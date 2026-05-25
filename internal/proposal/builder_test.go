package proposal

import (
	"strings"
	"testing"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

func TestBuildProposal_Complete(t *testing.T) {
	alert := alertmanager.Alert{
		Labels: map[string]string{
			"alertname": "KubePodCrashLooping",
			"namespace": "production",
			"severity":  "critical",
			"pod":       "my-pod-abc123",
		},
		Annotations: map[string]string{
			"summary":     "Pod is crash looping",
			"description": "Pod my-pod-abc123 is restarting frequently",
		},
		StartsAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Fingerprint: "a1b2c3d4e5f6",
	}

	p, err := BuildProposal(alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Name != "kubepodcrashlooping-production-a1b2c3d4" {
		t.Errorf("unexpected name: %s", p.Name)
	}
	if p.Namespace != "production" {
		t.Errorf("unexpected namespace: %s", p.Namespace)
	}
	if p.Labels[LabelSource] != "alertmanager" {
		t.Errorf("unexpected source label: %s", p.Labels[LabelSource])
	}
	if p.Labels[LabelAlertFingerprint] != "a1b2c3d4" {
		t.Errorf("unexpected fingerprint label: %s", p.Labels[LabelAlertFingerprint])
	}
	if p.Labels[LabelAlertName] != "kubepodcrashlooping" {
		t.Errorf("unexpected alert-name label: %s", p.Labels[LabelAlertName])
	}
	if p.Labels[LabelAlertSeverity] != "critical" {
		t.Errorf("unexpected severity label: %s", p.Labels[LabelAlertSeverity])
	}
	if p.Annotations[AnnotationAlertStartsAt] != "2025-01-01T00:00:00Z" {
		t.Errorf("unexpected starts-at annotation: %s", p.Annotations[AnnotationAlertStartsAt])
	}
	if !strings.Contains(p.Spec.Request, "KubePodCrashLooping") {
		t.Error("request should contain alertname")
	}
	if !strings.Contains(p.Spec.Request, "Pod is crash looping") {
		t.Error("request should contain summary")
	}
	if len(p.Spec.TargetNamespaces) != 1 || p.Spec.TargetNamespaces[0] != "production" {
		t.Errorf("unexpected targetNamespaces: %v", p.Spec.TargetNamespaces)
	}
	if p.Spec.Analysis.Agent != DefaultAgent {
		t.Errorf("unexpected analysis agent: %s", p.Spec.Analysis.Agent)
	}
	if p.Spec.Execution.Agent != DefaultAgent {
		t.Errorf("unexpected execution agent: %s", p.Spec.Execution.Agent)
	}
	if p.Spec.Verification.Agent != DefaultAgent {
		t.Errorf("unexpected verification agent: %s", p.Spec.Verification.Agent)
	}
	if p.Spec.AnalysisOutput.Mode != agenticv1alpha1.AnalysisOutputModeDefault {
		t.Errorf("unexpected analysisOutput mode: %s", p.Spec.AnalysisOutput.Mode)
	}
}

func TestBuildProposal_ClusterScoped(t *testing.T) {
	alert := alertmanager.Alert{
		Labels: map[string]string{
			"alertname": "EtcdHighFsyncDurations",
			"severity":  "warning",
		},
		Annotations: map[string]string{
			"summary": "Etcd fsync durations are high",
		},
		StartsAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Fingerprint: "f9e8d7c6b5a4",
	}

	p, err := BuildProposal(alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Namespace != DefaultNamespace {
		t.Errorf("expected fallback namespace %s, got %s", DefaultNamespace, p.Namespace)
	}
	if p.Spec.TargetNamespaces != nil {
		t.Errorf("expected nil targetNamespaces for cluster-scoped, got %v", p.Spec.TargetNamespaces)
	}
	if p.Name != "etcdhighfsyncdurations-f9e8d7c6" {
		t.Errorf("unexpected name: %s", p.Name)
	}
}

func TestBuildProposal_MissingAlertname(t *testing.T) {
	alert := alertmanager.Alert{
		Labels:      map[string]string{"severity": "critical"},
		Fingerprint: "abc123",
	}

	_, err := BuildProposal(alert)
	if err == nil {
		t.Fatal("expected error for missing alertname")
	}
}

func TestBuildProposal_MissingAnnotations(t *testing.T) {
	alert := alertmanager.Alert{
		Labels: map[string]string{
			"alertname": "TestAlert",
			"namespace": "test-ns",
		},
		Annotations: map[string]string{},
		StartsAt:    time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
		Fingerprint: "abcdef12",
	}

	p, err := BuildProposal(alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(p.Spec.Request, "TestAlert") {
		t.Error("request should contain alertname even without annotations")
	}
}

func TestBuildProposal_LongSummaryTruncated(t *testing.T) {
	alert := alertmanager.Alert{
		Labels: map[string]string{
			"alertname": "TestAlert",
		},
		Annotations: map[string]string{
			"summary": strings.Repeat("x", 500),
		},
		StartsAt:    time.Now(),
		Fingerprint: "12345678",
	}

	p, err := BuildProposal(alert)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(p.Annotations[AnnotationAlertSummary]) > maxAnnotationLength {
		t.Errorf("summary annotation not truncated: %d chars", len(p.Annotations[AnnotationAlertSummary]))
	}
}
