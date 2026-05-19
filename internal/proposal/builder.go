package proposal

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

const (
	// DefaultNamespace is the fallback namespace for Proposals created from
	// cluster-scoped alerts that have no namespace label.
	DefaultNamespace = "openshift-lightspeed"

	// DefaultAgent is the agent name used for analysis, execution, and
	// verification steps.
	DefaultAgent = "default"

	maxLabelValueLen      = 63
	maxAnnotationValueLen = 256

	labelSource           = "agentic.openshift.io/source"
	labelAlertFingerprint = "agentic.openshift.io/alert-fingerprint"
	labelAlertName        = "agentic.openshift.io/alert-name"
	labelAlertSeverity    = "agentic.openshift.io/alert-severity"

	annotationStartsAt = "agentic.openshift.io/alert-starts-at"
	annotationSummary  = "agentic.openshift.io/alert-summary"
)

//go:embed request.tmpl
var templateFS embed.FS

var requestTmpl = template.Must(template.ParseFS(templateFS, "request.tmpl"))

// requestData holds the values injected into the request template.
type requestData struct {
	AlertName   string
	Severity    string
	Namespace   string
	Summary     string
	Description string
	Labels      map[string]string
}

// BuildProposal maps an AlertManager alert to a fully populated Proposal CR.
// It returns an error if the alert is missing the required "alertname" label
// or if the request template fails to render.
func BuildProposal(a alertmanager.Alert) (*agenticv1alpha1.Proposal, error) {
	alertname := a.Labels["alertname"]
	if alertname == "" {
		return nil, fmt.Errorf("alert missing required label \"alertname\"")
	}

	severity := a.Labels["severity"]
	namespace := a.Labels["namespace"]

	fingerprint := ""
	if a.Fingerprint != nil {
		fingerprint = *a.Fingerprint
	}

	var startsAt time.Time
	if a.StartsAt != nil {
		startsAt = time.Time(*a.StartsAt)
	}

	summary := a.Annotations["summary"]
	description := a.Annotations["description"]

	var buf bytes.Buffer
	err := requestTmpl.Execute(&buf, requestData{
		AlertName:   alertname,
		Severity:    severity,
		Namespace:   namespace,
		Summary:     summary,
		Description: description,
		Labels:      a.Labels,
	})
	if err != nil {
		return nil, fmt.Errorf("rendering request template: %w", err)
	}

	fp := fingerprint
	if len(fp) > 8 {
		fp = fp[:8]
	}

	proposalNamespace := namespace
	if proposalNamespace == "" {
		proposalNamespace = DefaultNamespace
	}

	var targetNamespaces []string
	if namespace != "" {
		targetNamespaces = []string{namespace}
	}

	return &agenticv1alpha1.Proposal{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "agentic.openshift.io/v1alpha1",
			Kind:       "Proposal",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ProposalName(alertname, namespace, fingerprint),
			Namespace: proposalNamespace,
			Labels: map[string]string{
				labelSource:           "alertmanager",
				labelAlertFingerprint: fp,
				labelAlertName:        truncate(strings.ToLower(alertname), maxLabelValueLen),
				labelAlertSeverity:    truncate(strings.ToLower(severity), maxLabelValueLen),
			},
			Annotations: map[string]string{
				annotationStartsAt: startsAt.Format(time.RFC3339),
				annotationSummary:  truncate(summary, maxAnnotationValueLen),
			},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:          buf.String(),
			TargetNamespaces: targetNamespaces,
			AnalysisOutput: agenticv1alpha1.AnalysisOutput{
				Mode: agenticv1alpha1.AnalysisOutputModeDefault,
			},
			Analysis:     agenticv1alpha1.ProposalStep{Agent: DefaultAgent},
			Execution:    agenticv1alpha1.ProposalStep{Agent: DefaultAgent},
			Verification: agenticv1alpha1.ProposalStep{Agent: DefaultAgent},
		},
	}, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
