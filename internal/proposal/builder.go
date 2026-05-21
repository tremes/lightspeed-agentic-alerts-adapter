package proposal

import (
	"fmt"
	"regexp"
	"strings"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

const (
	DefaultNamespace = "openshift-lightspeed"

	LabelSource           = "agentic.openshift.io/source"
	LabelAlertFingerprint = "agentic.openshift.io/alert-fingerprint"
	LabelAlertName        = "agentic.openshift.io/alert-name"
	LabelAlertSeverity    = "agentic.openshift.io/alert-severity"

	AnnotationAlertStartsAt = "agentic.openshift.io/alert-starts-at"
	AnnotationAlertSummary  = "agentic.openshift.io/alert-summary"

	SourceAlertManager = "alertmanager"
)

var nonAlphanumericRe = regexp.MustCompile(`[^a-z0-9-]`)

func sanitizeDNS(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, ".", "-")
	s = nonAlphanumericRe.ReplaceAllString(s, "")
	s = strings.Trim(s, "-")
	return s
}

func TruncateFingerprint(fingerprint string) string {
	if len(fingerprint) > 8 {
		return fingerprint[:8]
	}
	return fingerprint
}

func BuildProposalName(alertName, namespace, fingerprint string) string {
	name := sanitizeDNS(alertName) + "-" + sanitizeDNS(namespace) + "-" + TruncateFingerprint(fingerprint)
	if len(name) > 253 {
		name = name[:253]
	}
	return name
}

func BuildProposal(alert alertmanager.Alert, agentName string) (*agenticv1alpha1.Proposal, error) {
	alertName := alert.Labels["alertname"]
	if alertName == "" {
		return nil, fmt.Errorf("alert missing alertname label (fingerprint: %s)", alert.Fingerprint)
	}

	namespace := alert.Labels["namespace"]
	fp := TruncateFingerprint(alert.Fingerprint)

	proposalName := BuildProposalName(alertName, namespace, alert.Fingerprint)

	proposalNamespace := namespace
	if proposalNamespace == "" {
		proposalNamespace = DefaultNamespace
	}

	request, err := RenderRequest(alert)
	if err != nil {
		return nil, fmt.Errorf("rendering request for alert %s: %w", alertName, err)
	}

	summary := alert.Annotations["summary"]
	if len(summary) > 256 {
		summary = summary[:256]
	}

	p := &agenticv1alpha1.Proposal{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "agentic.openshift.io/v1alpha1",
			Kind:       "Proposal",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      proposalName,
			Namespace: proposalNamespace,
			Labels: map[string]string{
				LabelSource:           SourceAlertManager,
				LabelAlertFingerprint: fp,
				LabelAlertName:        sanitizeDNS(alertName),
				LabelAlertSeverity:    strings.ToLower(alert.Labels["severity"]),
			},
			Annotations: map[string]string{
				AnnotationAlertStartsAt: alert.StartsAt.UTC().Format("2006-01-02T15:04:05Z"),
				AnnotationAlertSummary:  summary,
			},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request: request,
			Analysis: agenticv1alpha1.ProposalStep{
				Agent: agentName,
			},
			Execution: agenticv1alpha1.ProposalStep{
				Agent: agentName,
			},
			Verification: agenticv1alpha1.ProposalStep{
				Agent: agentName,
			},
			AnalysisOutput: agenticv1alpha1.AnalysisOutput{
				Mode: agenticv1alpha1.AnalysisOutputModeDefault,
			},
		},
	}

	if namespace != "" {
		p.Spec.TargetNamespaces = []string{namespace}
	}

	return p, nil
}
