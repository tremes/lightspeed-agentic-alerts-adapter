package poller

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/proposal"
)

type fakeProposalClient struct {
	proposals []agenticv1alpha1.Proposal
	created   []*agenticv1alpha1.Proposal
	createErr error
	listErr   error
}

func (f *fakeProposalClient) Create(_ context.Context, p *agenticv1alpha1.Proposal) (*agenticv1alpha1.Proposal, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, p.DeepCopy())
	return p, nil
}

func (f *fakeProposalClient) List(_ context.Context, _ metav1.ListOptions) (*agenticv1alpha1.ProposalList, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return &agenticv1alpha1.ProposalList{Items: f.proposals}, nil
}

func newTestPoller(serverURL string, pc ProposalClient) (*Poller, *atomic.Bool) {
	amClient := alertmanager.NewClientWithHTTP(serverURL, &http.Client{})
	ready := &atomic.Bool{}
	logger := slog.Default()
	return NewPoller(amClient, pc, logger, ready), ready
}

func alertJSON(name, ns, fingerprint string, startsAt time.Time) string {
	return fmt.Sprintf(`{
		"labels": {"alertname": %q, "namespace": %q, "severity": "critical"},
		"annotations": {"summary": "test summary"},
		"startsAt": %q,
		"fingerprint": %q,
		"status": {"state": "active", "silencedBy": [], "inhibitedBy": []}
	}`, name, ns, startsAt.Format(time.RFC3339), fingerprint)
}

func TestPoll_CreatesProposal(t *testing.T) {
	startsAt := time.Now().Add(-10 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", alertJSON("TestAlert", "test-ns", "aabb1122ccdd", startsAt))
	}))
	defer server.Close()

	pc := &fakeProposalClient{}
	p, ready := newTestPoller(server.URL, pc)

	if err := p.poll(context.Background()); err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	if len(pc.created) != 1 {
		t.Fatalf("expected 1 proposal created, got %d", len(pc.created))
	}
	if pc.created[0].Labels[proposal.LabelAlertFingerprint] != "aabb1122" {
		t.Errorf("unexpected fingerprint: %s", pc.created[0].Labels[proposal.LabelAlertFingerprint])
	}
	if !ready.Load() {
		t.Error("expected ready=true after successful poll")
	}
}

func TestPoll_SkipsAlertWithinInitialDelay(t *testing.T) {
	startsAt := time.Now().Add(-1 * time.Minute) // only 1 min ago
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", alertJSON("TestAlert", "test-ns", "aabb1122ccdd", startsAt))
	}))
	defer server.Close()

	pc := &fakeProposalClient{}
	p, _ := newTestPoller(server.URL, pc)

	if err := p.poll(context.Background()); err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	if len(pc.created) != 0 {
		t.Fatalf("expected no proposals created, got %d", len(pc.created))
	}
}

func TestPoll_SkipsActiveProposal(t *testing.T) {
	startsAt := time.Now().Add(-10 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", alertJSON("TestAlert", "test-ns", "aabb1122ccdd", startsAt))
	}))
	defer server.Close()

	pc := &fakeProposalClient{
		proposals: []agenticv1alpha1.Proposal{
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						proposal.LabelAlertFingerprint: "aabb1122",
					},
				},
				Status: agenticv1alpha1.ProposalStatus{
					Conditions: []metav1.Condition{
						{Type: agenticv1alpha1.ProposalConditionAnalyzed, Status: metav1.ConditionUnknown},
					},
				},
			},
		},
	}
	p, _ := newTestPoller(server.URL, pc)

	if err := p.poll(context.Background()); err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	if len(pc.created) != 0 {
		t.Fatalf("expected no proposals created (active exists), got %d", len(pc.created))
	}
}

func TestPoll_SkipsTerminalWithinCooldown(t *testing.T) {
	startsAt := time.Now().Add(-10 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", alertJSON("TestAlert", "test-ns", "aabb1122ccdd", startsAt))
	}))
	defer server.Close()

	pc := &fakeProposalClient{
		proposals: []agenticv1alpha1.Proposal{
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						proposal.LabelAlertFingerprint: "aabb1122",
					},
				},
				Status: agenticv1alpha1.ProposalStatus{
					Conditions: []metav1.Condition{
						{
							Type:               agenticv1alpha1.ProposalConditionAnalyzed,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-30 * time.Minute)),
						},
						{
							Type:               agenticv1alpha1.ProposalConditionVerified,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-20 * time.Minute)),
						},
					},
				},
			},
		},
	}
	p, _ := newTestPoller(server.URL, pc)

	if err := p.poll(context.Background()); err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	if len(pc.created) != 0 {
		t.Fatalf("expected no proposals created (cooldown), got %d", len(pc.created))
	}
}

func TestPoll_CreatesAfterCooldownExpired(t *testing.T) {
	startsAt := time.Now().Add(-10 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", alertJSON("TestAlert", "test-ns", "aabb1122ccdd", startsAt))
	}))
	defer server.Close()

	pc := &fakeProposalClient{
		proposals: []agenticv1alpha1.Proposal{
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						proposal.LabelAlertFingerprint: "aabb1122",
					},
				},
				Status: agenticv1alpha1.ProposalStatus{
					Conditions: []metav1.Condition{
						{
							Type:               agenticv1alpha1.ProposalConditionVerified,
							Status:             metav1.ConditionTrue,
							LastTransitionTime: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
						},
					},
				},
			},
		},
	}
	p, _ := newTestPoller(server.URL, pc)

	if err := p.poll(context.Background()); err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	if len(pc.created) != 1 {
		t.Fatalf("expected 1 proposal created (cooldown expired), got %d", len(pc.created))
	}
}

func TestPoll_AlertManagerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	pc := &fakeProposalClient{}
	p, ready := newTestPoller(server.URL, pc)
	ready.Store(true)

	err := p.poll(context.Background())
	if err == nil {
		t.Fatal("expected error for AM failure")
	}
	if ready.Load() {
		t.Error("expected ready=false after AM error")
	}
}

func TestPoll_KubernetesListError(t *testing.T) {
	startsAt := time.Now().Add(-10 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", alertJSON("TestAlert", "test-ns", "aabb1122ccdd", startsAt))
	}))
	defer server.Close()

	pc := &fakeProposalClient{listErr: fmt.Errorf("k8s api unavailable")}
	p, ready := newTestPoller(server.URL, pc)
	ready.Store(true)

	err := p.poll(context.Background())
	if err == nil {
		t.Fatal("expected error for K8s list failure")
	}
	if ready.Load() {
		t.Error("expected ready=false after K8s error")
	}
}

func TestPoll_ConflictOnCreate(t *testing.T) {
	startsAt := time.Now().Add(-10 * time.Minute)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s]", alertJSON("TestAlert", "test-ns", "aabb1122ccdd", startsAt))
	}))
	defer server.Close()

	pc := &fakeProposalClient{
		createErr: &conflictError{},
	}
	p, _ := newTestPoller(server.URL, pc)

	if err := p.poll(context.Background()); err != nil {
		t.Fatalf("poll should not fail on conflict: %v", err)
	}

	if len(pc.created) != 0 {
		t.Error("no proposals should be tracked on conflict")
	}
}

type conflictError struct{}

func (e *conflictError) Error() string { return "conflict" }
func (e *conflictError) Status() metav1.Status {
	return metav1.Status{Code: 409}
}

func TestPoll_MixedAlerts(t *testing.T) {
	now := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, "[%s,%s,%s]",
			alertJSON("OldAlert", "ns1", "1111111111111", now.Add(-10*time.Minute)),
			alertJSON("NewAlert", "ns2", "2222222222222", now.Add(-1*time.Minute)),
			alertJSON("ActiveAlert", "ns3", "3333333333333", now.Add(-10*time.Minute)),
		)
	}))
	defer server.Close()

	pc := &fakeProposalClient{
		proposals: []agenticv1alpha1.Proposal{
			{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						proposal.LabelAlertFingerprint: "33333333",
					},
				},
				Status: agenticv1alpha1.ProposalStatus{
					Conditions: []metav1.Condition{
						{Type: agenticv1alpha1.ProposalConditionAnalyzed, Status: metav1.ConditionUnknown},
					},
				},
			},
		},
	}
	p, _ := newTestPoller(server.URL, pc)

	if err := p.poll(context.Background()); err != nil {
		t.Fatalf("poll failed: %v", err)
	}

	// OldAlert: should be created (passes all checks)
	// NewAlert: should be skipped (initial delay)
	// ActiveAlert: should be skipped (active proposal exists)
	if len(pc.created) != 1 {
		t.Fatalf("expected 1 proposal created, got %d", len(pc.created))
	}
	if pc.created[0].Labels[proposal.LabelAlertFingerprint] != "11111111" {
		t.Errorf("expected OldAlert proposal, got fingerprint %s",
			pc.created[0].Labels[proposal.LabelAlertFingerprint])
	}
}
