package proposal

import (
	"bytes"
	"fmt"
	"text/template"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

const (
	DefaultNamespace = "openshift-lightspeed"
	DefaultAgent     = "default"

	LabelSource           = "agentic.openshift.io/source"
	LabelAlertFingerprint = "agentic.openshift.io/alert-fingerprint"
	LabelAlertName        = "agentic.openshift.io/alert-name"
	LabelAlertSeverity    = "agentic.openshift.io/alert-severity"

	AnnotationAlertStartsAt = "agentic.openshift.io/alert-starts-at"
	AnnotationAlertSummary  = "agentic.openshift.io/alert-summary"

	maxAnnotationLength = 256
)

var requestTemplate = template.Must(template.New("request").Parse(
	`A Kubernetes alert is firing in the cluster.
Investigate the root cause and propose a remediation.

Alert: {{ .AlertName }}
Severity: {{ .Severity }}
Namespace: {{ .Namespace }}
Summary: {{ .Summary }}
Description: {{ .Description }}

Labels:
{{ range $k, $v := .Labels }}  {{ $k }}: {{ $v }}
{{ end }}`))

type requestData struct {
	AlertName   string
	Severity    string
	Namespace   string
	Summary     string
	Description string
	Labels      map[string]string
}

func BuildProposal(alert alertmanager.Alert) (*agenticv1alpha1.Proposal, error) {
	alertname := alert.Labels["alertname"]
	if alertname == "" {
		return nil, fmt.Errorf("alert missing alertname label")
	}

	namespace := alert.Labels["namespace"]
	fingerprint := alert.Fingerprint

	proposalNamespace := namespace
	if proposalNamespace == "" {
		proposalNamespace = DefaultNamespace
	}

	data := requestData{
		AlertName:   alertname,
		Severity:    alert.Labels["severity"],
		Namespace:   namespace,
		Summary:     alert.Annotations["summary"],
		Description: alert.Annotations["description"],
		Labels:      alert.Labels,
	}

	var buf bytes.Buffer
	if err := requestTemplate.Execute(&buf, data); err != nil {
		return nil, fmt.Errorf("rendering request template: %w", err)
	}

	fp := fingerprint
	if len(fp) > 8 {
		fp = fp[:8]
	}

	summary := alert.Annotations["summary"]
	if len(summary) > maxAnnotationLength {
		summary = summary[:maxAnnotationLength]
	}

	proposal := &agenticv1alpha1.Proposal{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "agentic.openshift.io/v1alpha1",
			Kind:       "Proposal",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ProposalName(alertname, namespace, fingerprint),
			Namespace: proposalNamespace,
			Labels: map[string]string{
				LabelSource:           "alertmanager",
				LabelAlertFingerprint: fp,
				LabelAlertName:        sanitize(alertname),
				LabelAlertSeverity:    alert.Labels["severity"],
			},
			Annotations: map[string]string{
				AnnotationAlertStartsAt: alert.StartsAt.Format("2006-01-02T15:04:05Z07:00"),
				AnnotationAlertSummary:  summary,
			},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request: buf.String(),
			Analysis: agenticv1alpha1.ProposalStep{
				Agent: DefaultAgent,
			},
			Execution: agenticv1alpha1.ProposalStep{
				Agent: DefaultAgent,
			},
			Verification: agenticv1alpha1.ProposalStep{
				Agent: DefaultAgent,
			},
			AnalysisOutput: agenticv1alpha1.AnalysisOutput{
				Mode: agenticv1alpha1.AnalysisOutputModeDefault,
			},
		},
	}

	if namespace != "" {
		proposal.Spec.TargetNamespaces = []string{namespace}
	}

	return proposal, nil
}
