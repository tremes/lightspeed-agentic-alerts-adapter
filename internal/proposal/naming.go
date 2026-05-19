package proposal

import (
	"regexp"
	"strings"
)

const maxNameLength = 253

var nonAlphanumHyphen = regexp.MustCompile(`[^a-z0-9-]`)
var consecutiveHyphens = regexp.MustCompile(`-{2,}`)

// ProposalName produces a deterministic, DNS-compliant Kubernetes resource name
// from alert metadata. Format: {alertname}-{namespace}-{fingerprint[:8]}.
func ProposalName(alertname, namespace, fingerprint string) string {
	fp := fingerprint
	if len(fp) > 8 {
		fp = fp[:8]
	}

	alertname = sanitize(alertname)
	namespace = sanitize(namespace)

	suffix := "-" + namespace + "-" + fp
	if namespace == "" {
		suffix = "--" + fp
	}

	maxAlertLen := maxNameLength - len(suffix)
	if len(alertname) > maxAlertLen {
		alertname = alertname[:maxAlertLen]
	}

	name := alertname + suffix
	name = strings.TrimRight(name, "-")
	name = strings.TrimLeft(name, "-")
	return name
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumHyphen.ReplaceAllString(s, "-")
	s = consecutiveHyphens.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
