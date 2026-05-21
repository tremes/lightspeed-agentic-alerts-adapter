package adapter_test

import (
	"context"
	"testing"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := agenticv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return s
}

type fakeAMClient struct {
	alerts []alertmanager.Alert
	err    error
}

func (f *fakeAMClient) FiringAlerts(_ context.Context) ([]alertmanager.Alert, error) {
	return f.alerts, f.err
}

func TestReconcile_CreatesProposalForNewAlert(t *testing.T) {
	scheme := newScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				Annotations: map[string]string{
					"summary": "Pod is crash looping",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	err := a.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals.Items))
	}
	if proposals.Items[0].Name != "kubepodcrashlooping-production-abc12345" {
		t.Errorf("unexpected proposal name: %s", proposals.Items[0].Name)
	}
}

func TestReconcile_SkipsAlertWithinInitialDelay(t *testing.T) {
	scheme := newScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				StartsAt: now.Add(-2 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 0 {
		t.Fatalf("expected 0 proposals (alert within initial delay), got %d", len(proposals.Items))
	}
}

func TestReconcile_SkipsAlertWithExistingActiveProposal(t *testing.T) {
	scheme := newScheme(t)

	existingProposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubepodcrashlooping-production-abc12345",
			Namespace: "production",
			Labels: map[string]string{
				"agentic.openshift.io/source":            "alertmanager",
				"agentic.openshift.io/alert-fingerprint": "abc12345",
			},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:  "test",
			Analysis: agenticv1alpha1.ProposalStep{Agent: "default"},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingProposal).
		Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Errorf("expected 1 proposal (existing, no new one), got %d", len(proposals.Items))
	}
}

func TestReconcile_SkipsAlertWithTerminalProposalInCooldown(t *testing.T) {
	scheme := newScheme(t)

	now := time.Now().UTC()
	terminalProposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubepodcrashlooping-production-abc12345",
			Namespace: "production",
			Labels: map[string]string{
				"agentic.openshift.io/source":            "alertmanager",
				"agentic.openshift.io/alert-fingerprint": "abc12345",
			},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:  "test",
			Analysis: agenticv1alpha1.ProposalStep{Agent: "default"},
		},
		Status: agenticv1alpha1.ProposalStatus{
			Conditions: []metav1.Condition{
				{
					Type:               agenticv1alpha1.ProposalConditionVerified,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(now.Add(-30 * time.Minute)),
					Reason:             "Completed",
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(terminalProposal).
		WithStatusSubresource(&agenticv1alpha1.Proposal{}).
		Build()

	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Errorf("expected 1 proposal (terminal in cooldown, no new one), got %d", len(proposals.Items))
	}
}

func TestReconcile_CreatesProposalAfterCooldownExpired(t *testing.T) {
	scheme := newScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				Annotations: map[string]string{
					"summary": "Pod is crash looping",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals.Items))
	}
	if proposals.Items[0].Name != "kubepodcrashlooping-production-abc12345" {
		t.Errorf("unexpected proposal name: %s", proposals.Items[0].Name)
	}
}

func TestReconcile_ContinuesOnInvalidAlert(t *testing.T) {
	scheme := newScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "bad1",
				Labels:      map[string]string{"severity": "warning"},
				StartsAt:    now.Add(-10 * time.Minute),
			},
			{
				Fingerprint: "good1234abcd",
				Labels: map[string]string{
					"alertname": "ValidAlert",
					"namespace": "test-ns",
					"severity":  "info",
				},
				Annotations: map[string]string{
					"summary": "A valid alert",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Fatalf("expected 1 proposal (skipped invalid, created valid), got %d", len(proposals.Items))
	}
	if proposals.Items[0].Labels["agentic.openshift.io/alert-name"] != "validalert" {
		t.Errorf("unexpected proposal: %s", proposals.Items[0].Name)
	}
}
