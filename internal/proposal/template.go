package proposal

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

var requestTmpl = template.Must(template.New("request").Parse(`A Kubernetes alert is firing in the cluster.
Investigate the root cause and propose a remediation.

Alert: {{ .AlertName }}
Severity: {{ .Severity }}
Namespace: {{ .Namespace }}
Summary: {{ .Summary }}
Description: {{ .Description }}

Labels:
{{ range $k, $v := .Labels }}  {{ $k }}: {{ $v }}
{{ end }}`))

type templateData struct {
	AlertName   string
	Severity    string
	Namespace   string
	Summary     string
	Description string
	Labels      map[string]string
}

func RenderRequest(alert alertmanager.Alert) (string, error) {
	data := templateData{
		AlertName:   alert.Labels["alertname"],
		Severity:    alert.Labels["severity"],
		Namespace:   alert.Labels["namespace"],
		Summary:     alert.Annotations["summary"],
		Description: alert.Annotations["description"],
		Labels:      alert.Labels,
	}

	var buf bytes.Buffer
	if err := requestTmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering request template: %w", err)
	}
	return buf.String(), nil
}
