package poller

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"github.com/prometheus/alertmanager/api/v2/models"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func makeAlert(labels, annotations map[string]string, fingerprint string, startsAt time.Time) models.GettableAlert {
	fp := fingerprint
	st := strfmt.DateTime(startsAt)
	return models.GettableAlert{
		Alert: models.Alert{
			Labels: labels,
		},
		Annotations: annotations,
		Fingerprint: &fp,
		StartsAt:    &st,
	}
}

type fakeAlertClient struct {
	alerts []models.GettableAlert
	err    error
}

func (f *fakeAlertClient) FetchFiringAlerts(ctx context.Context) ([]models.GettableAlert, error) {
	return f.alerts, f.err
}

type fakeProposalClient struct {
	proposals []agenticv1alpha1.Proposal
	listErr   error
	createErr error
	created   []agenticv1alpha1.Proposal
}

func (f *fakeProposalClient) ListProposals(ctx context.Context, labelSelector string) ([]agenticv1alpha1.Proposal, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.proposals, nil
}

func (f *fakeProposalClient) CreateProposal(ctx context.Context, p *agenticv1alpha1.Proposal) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, *p)
	return nil
}

type countingProposalClient struct {
	proposals  []agenticv1alpha1.Proposal
	listErr    error
	createFunc func(p *agenticv1alpha1.Proposal) error
}

func (f *countingProposalClient) ListProposals(ctx context.Context, labelSelector string) ([]agenticv1alpha1.Proposal, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.proposals, nil
}

func (f *countingProposalClient) CreateProposal(ctx context.Context, p *agenticv1alpha1.Proposal) error {
	if f.createFunc != nil {
		return f.createFunc(p)
	}
	return nil
}

func TestPollOnce(t *testing.T) {
	now := time.Now().UTC()

	tests := []struct {
		name        string
		alerts      []models.GettableAlert
		alertErr    error
		proposals   []agenticv1alpha1.Proposal
		listErr     error
		createErr   error
		wantCreated int
		wantHealthy bool
	}{
		{
			name:        "no firing alerts creates nothing",
			wantCreated: 0,
			wantHealthy: true,
		},
		{
			name: "new alert with no existing proposals creates proposal",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"alertname": "KubePodCrashLooping", "severity": "critical", "namespace": "production"},
					map[string]string{"summary": "Pod is crash looping"},
					"a1b2c3d4e5f6", now.Add(-10*time.Minute),
				),
			},
			wantCreated: 1,
			wantHealthy: true,
		},
		{
			name: "alert within initial delay is skipped",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"alertname": "TestAlert", "severity": "warning", "namespace": "test"},
					map[string]string{}, "abc123", now.Add(-2*time.Minute),
				),
			},
			wantCreated: 0,
			wantHealthy: true,
		},
		{
			name: "alert with active non-terminal proposal is skipped",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"alertname": "TestAlert", "severity": "warning", "namespace": "test"},
					map[string]string{}, "abc12345dead", now.Add(-10*time.Minute),
				),
			},
			proposals: []agenticv1alpha1.Proposal{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testalert-test-abc12345", Namespace: "test",
					Labels: map[string]string{
						"agentic.openshift.io/source":            "alertmanager",
						"agentic.openshift.io/alert-fingerprint": "abc12345",
					},
				},
			}},
			wantCreated: 0,
			wantHealthy: true,
		},
		{
			name: "alert with terminal proposal within cooldown is skipped",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"alertname": "TestAlert", "severity": "warning", "namespace": "test"},
					map[string]string{}, "abc12345dead", now.Add(-10*time.Minute),
				),
			},
			proposals: []agenticv1alpha1.Proposal{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testalert-test-abc12345", Namespace: "test",
					Labels: map[string]string{
						"agentic.openshift.io/source":            "alertmanager",
						"agentic.openshift.io/alert-fingerprint": "abc12345",
					},
				},
				Status: agenticv1alpha1.ProposalStatus{
					Conditions: []metav1.Condition{{
						Type: "Completed", Status: metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(now.Add(-30 * time.Minute)),
					}},
				},
			}},
			wantCreated: 0,
			wantHealthy: true,
		},
		{
			name: "alert with terminal proposal outside cooldown creates new proposal",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"alertname": "TestAlert", "severity": "warning", "namespace": "test"},
					map[string]string{}, "abc12345dead", now.Add(-10*time.Minute),
				),
			},
			proposals: []agenticv1alpha1.Proposal{{
				ObjectMeta: metav1.ObjectMeta{
					Name: "testalert-test-abc12345", Namespace: "test",
					Labels: map[string]string{
						"agentic.openshift.io/source":            "alertmanager",
						"agentic.openshift.io/alert-fingerprint": "abc12345",
					},
				},
				Status: agenticv1alpha1.ProposalStatus{
					Conditions: []metav1.Condition{{
						Type: "Completed", Status: metav1.ConditionTrue,
						LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Hour)),
					}},
				},
			}},
			wantCreated: 1,
			wantHealthy: true,
		},
		{
			name: "multiple alerts with mix of skip and create",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"alertname": "AlertA", "severity": "critical", "namespace": "ns1"},
					map[string]string{}, "fp_a_12345678", now.Add(-10*time.Minute),
				),
				makeAlert(
					map[string]string{"alertname": "AlertB", "severity": "warning", "namespace": "ns2"},
					map[string]string{}, "fp_b_12345678", now.Add(-2*time.Minute),
				),
				makeAlert(
					map[string]string{"alertname": "AlertC", "severity": "info", "namespace": "ns3"},
					map[string]string{}, "fp_c_12345678", now.Add(-10*time.Minute),
				),
				makeAlert(
					map[string]string{"alertname": "AlertD", "severity": "critical", "namespace": "ns4"},
					map[string]string{}, "fp_d_12345678", now.Add(-10*time.Minute),
				),
			},
			proposals: []agenticv1alpha1.Proposal{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "alertc-ns3-fp_c_123", Namespace: "ns3",
						Labels: map[string]string{
							"agentic.openshift.io/source":            "alertmanager",
							"agentic.openshift.io/alert-fingerprint": "fp_c_123",
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "alertd-ns4-fp_d_123", Namespace: "ns4",
						Labels: map[string]string{
							"agentic.openshift.io/source":            "alertmanager",
							"agentic.openshift.io/alert-fingerprint": "fp_d_123",
						},
					},
					Status: agenticv1alpha1.ProposalStatus{
						Conditions: []metav1.Condition{{
							Type: "Completed", Status: metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(now.Add(-3 * time.Hour)),
						}},
					},
				},
			},
			wantCreated: 2,
			wantHealthy: true,
		},
		{
			name:        "alertmanager error skips cycle and reports unhealthy",
			alertErr:    fmt.Errorf("connection refused"),
			wantCreated: 0,
			wantHealthy: false,
		},
		{
			name: "k8s list error skips cycle and reports unhealthy",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"alertname": "TestAlert", "severity": "warning", "namespace": "test"},
					map[string]string{}, "abc123", now.Add(-10*time.Minute),
				),
			},
			listErr:     fmt.Errorf("k8s api unreachable"),
			wantCreated: 0,
			wantHealthy: false,
		},
		{
			name: "409 conflict on create is treated as success",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"alertname": "TestAlert", "severity": "warning", "namespace": "test"},
					map[string]string{}, "abc12345dead", now.Add(-10*time.Minute),
				),
			},
			createErr:   kerrors.NewAlreadyExists(schema.GroupResource{Group: "agentic.openshift.io", Resource: "proposals"}, "testalert-test-abc12345"),
			wantCreated: 0,
			wantHealthy: true,
		},
		{
			name: "alert missing alertname is skipped but other alerts still processed",
			alerts: []models.GettableAlert{
				makeAlert(
					map[string]string{"severity": "warning", "namespace": "test"},
					map[string]string{}, "abc123", now.Add(-10*time.Minute),
				),
				makeAlert(
					map[string]string{"alertname": "ValidAlert", "severity": "warning", "namespace": "test"},
					map[string]string{}, "def456789012", now.Add(-10*time.Minute),
				),
			},
			wantCreated: 1,
			wantHealthy: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ac := &fakeAlertClient{alerts: tt.alerts, err: tt.alertErr}
			pc := &fakeProposalClient{proposals: tt.proposals, listErr: tt.listErr, createErr: tt.createErr}
			p := New(ac, pc,
				WithInitialDelay(5*time.Minute),
				WithCooldownWindow(time.Hour),
				withNowFunc(func() time.Time { return now }),
			)

			p.pollOnce(t.Context())

			if got := len(pc.created); got != tt.wantCreated {
				t.Errorf("created %d proposals, want %d", got, tt.wantCreated)
			}
			if got := p.Healthy(); got != tt.wantHealthy {
				t.Errorf("Healthy() = %v, want %v", got, tt.wantHealthy)
			}
		})
	}
}

func TestTerminalConditionTypes(t *testing.T) {
	now := time.Now().UTC()
	terminalTypes := []string{"Completed", "Failed", "Escalated", "Denied"}

	for _, condType := range terminalTypes {
		t.Run(condType+"_within_cooldown_is_skipped", func(t *testing.T) {
			ac := &fakeAlertClient{
				alerts: []models.GettableAlert{
					makeAlert(
						map[string]string{"alertname": "TestAlert", "severity": "warning", "namespace": "test"},
						map[string]string{}, "abc12345dead", now.Add(-10*time.Minute),
					),
				},
			}
			pc := &fakeProposalClient{
				proposals: []agenticv1alpha1.Proposal{{
					ObjectMeta: metav1.ObjectMeta{
						Name: "testalert-test-abc12345", Namespace: "test",
						Labels: map[string]string{
							"agentic.openshift.io/source":            "alertmanager",
							"agentic.openshift.io/alert-fingerprint": "abc12345",
						},
					},
					Status: agenticv1alpha1.ProposalStatus{
						Conditions: []metav1.Condition{{
							Type: condType, Status: metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(now.Add(-30 * time.Minute)),
						}},
					},
				}},
			}
			p := New(ac, pc,
				WithInitialDelay(5*time.Minute),
				WithCooldownWindow(time.Hour),
				withNowFunc(func() time.Time { return now }),
			)

			p.pollOnce(t.Context())

			if got := len(pc.created); got != 0 {
				t.Errorf("expected skipped for terminal %s within cooldown, got %d proposals", condType, got)
			}
		})
	}
}

func TestTerminalConditionStatusFalseIsNotTerminal(t *testing.T) {
	now := time.Now().UTC()
	ac := &fakeAlertClient{
		alerts: []models.GettableAlert{
			makeAlert(
				map[string]string{"alertname": "TestAlert", "severity": "warning", "namespace": "test"},
				map[string]string{}, "abc12345dead", now.Add(-10*time.Minute),
			),
		},
	}
	pc := &fakeProposalClient{
		proposals: []agenticv1alpha1.Proposal{{
			ObjectMeta: metav1.ObjectMeta{
				Name: "testalert-test-abc12345", Namespace: "test",
				Labels: map[string]string{
					"agentic.openshift.io/source":            "alertmanager",
					"agentic.openshift.io/alert-fingerprint": "abc12345",
				},
			},
			Status: agenticv1alpha1.ProposalStatus{
				Conditions: []metav1.Condition{{
					Type: "Failed", Status: metav1.ConditionFalse,
				}},
			},
		}},
	}
	p := New(ac, pc,
		WithInitialDelay(5*time.Minute),
		WithCooldownWindow(time.Hour),
		withNowFunc(func() time.Time { return now }),
	)

	p.pollOnce(t.Context())

	if got := len(pc.created); got != 0 {
		t.Errorf("expected active proposal (Failed=False) to block creation, got %d", got)
	}
}

func TestCreateErrorDoesNotBlockOtherAlerts(t *testing.T) {
	now := time.Now().UTC()
	callCount := 0
	ac := &fakeAlertClient{
		alerts: []models.GettableAlert{
			makeAlert(
				map[string]string{"alertname": "AlertFail", "severity": "warning", "namespace": "ns1"},
				map[string]string{}, "fail12345678", now.Add(-10*time.Minute),
			),
			makeAlert(
				map[string]string{"alertname": "AlertOK", "severity": "warning", "namespace": "ns2"},
				map[string]string{}, "ok_123456789", now.Add(-10*time.Minute),
			),
		},
	}
	pc := &countingProposalClient{
		createFunc: func(p *agenticv1alpha1.Proposal) error {
			callCount++
			if callCount == 1 {
				return fmt.Errorf("internal server error")
			}
			return nil
		},
	}
	p := New(ac, pc,
		WithInitialDelay(5*time.Minute),
		WithCooldownWindow(time.Hour),
		withNowFunc(func() time.Time { return now }),
	)

	p.pollOnce(t.Context())

	if callCount != 2 {
		t.Errorf("expected create called 2 times (both alerts attempted), got %d", callCount)
	}
}

func TestRunContextCancellation(t *testing.T) {
	ac := &fakeAlertClient{}
	pc := &fakeProposalClient{}
	p := New(ac, pc, WithPollInterval(50*time.Millisecond))

	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestHealthyFlipsOnError(t *testing.T) {
	ac := &fakeAlertClient{}
	pc := &fakeProposalClient{}
	p := New(ac, pc)

	if p.Healthy() {
		t.Error("expected unhealthy before first cycle")
	}

	p.pollOnce(t.Context())
	if !p.Healthy() {
		t.Error("expected healthy after successful cycle")
	}

	ac.err = fmt.Errorf("connection refused")
	p.pollOnce(t.Context())
	if p.Healthy() {
		t.Error("expected unhealthy after failed cycle")
	}

	ac.err = nil
	p.pollOnce(t.Context())
	if !p.Healthy() {
		t.Error("expected healthy after recovery")
	}
}
