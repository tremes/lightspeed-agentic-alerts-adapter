package proposal

import (
	"strings"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/prometheus/alertmanager/api/v2/models"
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

var defaultStartsAt = time.Date(2025, 3, 15, 10, 30, 0, 0, time.UTC)

func TestBuildProposal(t *testing.T) {
	tests := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		fingerprint string
		startsAt    time.Time

		wantErr       bool
		wantName      string
		wantNamespace string
		wantTargetNS  []string

		checkLabels      map[string]string
		checkAnnotations map[string]string

		maxSummaryLen    int
		maxAlertNameLen  int
		checkRequestHas  []string
		checkRequestMiss []string
	}{
		{
			name: "standard alert with all fields",
			labels: map[string]string{
				"alertname": "KubePodCrashLooping",
				"severity":  "critical",
				"namespace": "production",
				"pod":       "web-abc123",
			},
			annotations: map[string]string{
				"summary":     "Pod is crash looping",
				"description": "Pod web-abc123 in namespace production has restarted 5 times",
			},
			fingerprint:   "a1b2c3d4e5f6",
			startsAt:      defaultStartsAt,
			wantName:      "kubepodcrashlooping-production-a1b2c3d4",
			wantNamespace: "production",
			wantTargetNS:  []string{"production"},
			checkLabels: map[string]string{
				"agentic.openshift.io/source":            "alertmanager",
				"agentic.openshift.io/alert-fingerprint": "a1b2c3d4",
				"agentic.openshift.io/alert-name":        "kubepodcrashlooping",
				"agentic.openshift.io/alert-severity":    "critical",
			},
			checkAnnotations: map[string]string{
				"agentic.openshift.io/alert-starts-at": defaultStartsAt.Format(time.RFC3339),
				"agentic.openshift.io/alert-summary":   "Pod is crash looping",
			},
			checkRequestHas: []string{
				"KubePodCrashLooping", "critical", "production",
				"Pod is crash looping", "Pod web-abc123 in namespace production",
			},
		},
		{
			name: "cluster-scoped alert with no namespace",
			labels: map[string]string{
				"alertname": "etcdHighFsyncDurations",
				"severity":  "warning",
			},
			annotations: map[string]string{
				"summary": "etcd is slow",
			},
			fingerprint:   "f9e8d7c6abcd",
			startsAt:      defaultStartsAt,
			wantName:      "etcdhighfsyncdurations--f9e8d7c6",
			wantNamespace: "openshift-lightspeed",
			wantTargetNS:  nil,
		},
		{
			name: "long summary truncated to 256 chars",
			labels: map[string]string{
				"alertname": "TestAlert",
				"severity":  "info",
				"namespace": "default",
			},
			annotations: map[string]string{
				"summary": strings.Repeat("x", 300),
			},
			fingerprint:   "abcdef123456",
			startsAt:      defaultStartsAt,
			maxSummaryLen: 256,
		},
		{
			name: "long alertname label truncated to 63 chars",
			labels: map[string]string{
				"alertname": strings.Repeat("A", 100),
				"severity":  "info",
				"namespace": "default",
			},
			annotations:     map[string]string{},
			fingerprint:     "abcdef123456",
			startsAt:        defaultStartsAt,
			maxAlertNameLen: 63,
		},
		{
			name: "missing alertname returns error",
			labels: map[string]string{
				"severity":  "info",
				"namespace": "default",
			},
			annotations: map[string]string{},
			fingerprint: "abcdef123456",
			startsAt:    defaultStartsAt,
			wantErr:     true,
		},
		{
			name: "empty annotations render without error",
			labels: map[string]string{
				"alertname": "TestAlert",
				"severity":  "info",
				"namespace": "default",
			},
			annotations: map[string]string{},
			fingerprint: "abcdef123456",
			startsAt:    defaultStartsAt,
			checkAnnotations: map[string]string{
				"agentic.openshift.io/alert-summary": "",
			},
			checkRequestHas: []string{"TestAlert"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := makeAlert(tt.labels, tt.annotations, tt.fingerprint, tt.startsAt)

			p, err := BuildProposal(a)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if tt.wantName != "" && p.Name != tt.wantName {
				t.Errorf("name = %q, want %q", p.Name, tt.wantName)
			}
			if tt.wantNamespace != "" && p.Namespace != tt.wantNamespace {
				t.Errorf("namespace = %q, want %q", p.Namespace, tt.wantNamespace)
			}

			if tt.wantTargetNS != nil {
				if len(p.Spec.TargetNamespaces) != len(tt.wantTargetNS) {
					t.Errorf("targetNamespaces = %v, want %v", p.Spec.TargetNamespaces, tt.wantTargetNS)
				}
				for i, ns := range tt.wantTargetNS {
					if i < len(p.Spec.TargetNamespaces) && p.Spec.TargetNamespaces[i] != ns {
						t.Errorf("targetNamespaces[%d] = %q, want %q", i, p.Spec.TargetNamespaces[i], ns)
					}
				}
			} else if tt.wantNamespace != "" && len(p.Spec.TargetNamespaces) != 0 {
				t.Errorf("targetNamespaces = %v, want empty", p.Spec.TargetNamespaces)
			}

			for k, want := range tt.checkLabels {
				if got := p.Labels[k]; got != want {
					t.Errorf("label %q = %q, want %q", k, got, want)
				}
			}
			for k, want := range tt.checkAnnotations {
				if got := p.Annotations[k]; got != want {
					t.Errorf("annotation %q = %q, want %q", k, got, want)
				}
			}

			if tt.maxSummaryLen > 0 {
				got := p.Annotations["agentic.openshift.io/alert-summary"]
				if len(got) > tt.maxSummaryLen {
					t.Errorf("summary annotation length = %d, want <= %d", len(got), tt.maxSummaryLen)
				}
			}
			if tt.maxAlertNameLen > 0 {
				got := p.Labels["agentic.openshift.io/alert-name"]
				if len(got) > tt.maxAlertNameLen {
					t.Errorf("alert-name label length = %d, want <= %d", len(got), tt.maxAlertNameLen)
				}
			}

			for _, s := range tt.checkRequestHas {
				if !strings.Contains(p.Spec.Request, s) {
					t.Errorf("request missing %q", s)
				}
			}

			// Verify common fields on all successful builds
			if p.APIVersion != "agentic.openshift.io/v1alpha1" {
				t.Errorf("apiVersion = %q, want %q", p.APIVersion, "agentic.openshift.io/v1alpha1")
			}
			if p.Kind != "Proposal" {
				t.Errorf("kind = %q, want %q", p.Kind, "Proposal")
			}
			if p.Spec.Analysis.Agent != "default" {
				t.Errorf("analysis.agent = %q, want %q", p.Spec.Analysis.Agent, "default")
			}
			if p.Spec.Execution.Agent != "default" {
				t.Errorf("execution.agent = %q, want %q", p.Spec.Execution.Agent, "default")
			}
			if p.Spec.Verification.Agent != "default" {
				t.Errorf("verification.agent = %q, want %q", p.Spec.Verification.Agent, "default")
			}
			if string(p.Spec.AnalysisOutput.Mode) != "Default" {
				t.Errorf("analysisOutput.mode = %q, want %q", p.Spec.AnalysisOutput.Mode, "Default")
			}
		})
	}
}

func TestBuildProposal_RequestTemplateContainsAllLabels(t *testing.T) {
	a := makeAlert(
		map[string]string{
			"alertname":  "KubePodCrashLooping",
			"severity":   "critical",
			"namespace":  "production",
			"custom_key": "custom_value",
		},
		map[string]string{},
		"a1b2c3d4e5f6",
		defaultStartsAt,
	)

	p, err := BuildProposal(a)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(p.Spec.Request, "custom_key") || !strings.Contains(p.Spec.Request, "custom_value") {
		t.Error("request should contain all alert labels including custom ones")
	}
}
