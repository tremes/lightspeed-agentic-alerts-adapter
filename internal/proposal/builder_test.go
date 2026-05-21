package proposal_test

import (
	"strings"
	"testing"
	"time"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/proposal"
)

func TestBuildProposalName(t *testing.T) {
	tests := []struct {
		name        string
		alertName   string
		namespace   string
		fingerprint string
		want        string
	}{
		{
			name:        "namespaced alert",
			alertName:   "KubePodCrashLooping",
			namespace:   "production",
			fingerprint: "a1b2c3d4e5f6",
			want:        "kubepodcrashlooping-production-a1b2c3d4",
		},
		{
			name:        "cluster-scoped alert (no namespace)",
			alertName:   "etcdHighFsyncDurations",
			namespace:   "",
			fingerprint: "f9e8d7c6b5a4",
			want:        "etcdhighfsyncdurations--f9e8d7c6",
		},
		{
			name:        "special characters sanitized",
			alertName:   "My_Alert.Name",
			namespace:   "my-ns",
			fingerprint: "1234567890ab",
			want:        "my-alert-name-my-ns-12345678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := proposal.BuildProposalName(tt.alertName, tt.namespace, tt.fingerprint)
			if got != tt.want {
				t.Errorf("BuildProposalName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildProposal(t *testing.T) {
	alert := alertmanager.Alert{
		Fingerprint: "abc12345def6",
		Labels: map[string]string{
			"alertname": "KubePodCrashLooping",
			"namespace": "production",
			"severity":  "warning",
		},
		Annotations: map[string]string{
			"summary":     "Pod is crash looping",
			"description": "Pod web-abc is crash looping in namespace production",
		},
		StartsAt: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	}

	p, err := proposal.BuildProposal(alert, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Name != "kubepodcrashlooping-production-abc12345" {
		t.Errorf("unexpected name: %s", p.Name)
	}
	if p.Namespace != "production" {
		t.Errorf("unexpected namespace: %s", p.Namespace)
	}
	if p.Labels["agentic.openshift.io/source"] != "alertmanager" {
		t.Errorf("missing source label")
	}
	if p.Labels["agentic.openshift.io/alert-fingerprint"] != "abc12345" {
		t.Errorf("unexpected fingerprint label: %s", p.Labels["agentic.openshift.io/alert-fingerprint"])
	}
	if p.Labels["agentic.openshift.io/alert-name"] != "kubepodcrashlooping" {
		t.Errorf("unexpected alert-name label: %s", p.Labels["agentic.openshift.io/alert-name"])
	}
	if p.Labels["agentic.openshift.io/alert-severity"] != "warning" {
		t.Errorf("unexpected severity label: %s", p.Labels["agentic.openshift.io/alert-severity"])
	}
	if p.Annotations["agentic.openshift.io/alert-starts-at"] != "2026-05-21T10:00:00Z" {
		t.Errorf("unexpected starts-at annotation: %s", p.Annotations["agentic.openshift.io/alert-starts-at"])
	}
	if !strings.Contains(p.Annotations["agentic.openshift.io/alert-summary"], "Pod is crash looping") {
		t.Errorf("unexpected summary annotation: %s", p.Annotations["agentic.openshift.io/alert-summary"])
	}
	if !strings.Contains(p.Spec.Request, "KubePodCrashLooping") {
		t.Errorf("request should contain alert name")
	}
	if len(p.Spec.TargetNamespaces) != 1 || p.Spec.TargetNamespaces[0] != "production" {
		t.Errorf("unexpected target namespaces: %v", p.Spec.TargetNamespaces)
	}
	if p.Spec.Analysis.Agent != "default" {
		t.Errorf("unexpected analysis agent: %s", p.Spec.Analysis.Agent)
	}
	if p.Spec.Execution.Agent != "default" {
		t.Errorf("unexpected execution agent: %s", p.Spec.Execution.Agent)
	}
	if p.Spec.Verification.Agent != "default" {
		t.Errorf("unexpected verification agent: %s", p.Spec.Verification.Agent)
	}
}

func TestBuildProposal_ClusterScoped(t *testing.T) {
	alert := alertmanager.Alert{
		Fingerprint: "ff11223344",
		Labels: map[string]string{
			"alertname": "etcdHighFsyncDurations",
			"severity":  "critical",
		},
		Annotations: map[string]string{
			"summary": "etcd fsync durations are high",
		},
		StartsAt: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	}

	p, err := proposal.BuildProposal(alert, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Namespace != "openshift-lightspeed" {
		t.Errorf("cluster-scoped alert should use default namespace, got: %s", p.Namespace)
	}
	if len(p.Spec.TargetNamespaces) != 0 {
		t.Errorf("cluster-scoped alert should have empty target namespaces, got: %v", p.Spec.TargetNamespaces)
	}
}

func TestBuildProposal_MissingAlertName(t *testing.T) {
	alert := alertmanager.Alert{
		Fingerprint: "abc123",
		Labels:      map[string]string{"severity": "warning"},
		StartsAt:    time.Now(),
	}

	_, err := proposal.BuildProposal(alert, "default")
	if err == nil {
		t.Fatal("expected error for missing alertname")
	}
}
