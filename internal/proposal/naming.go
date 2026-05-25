package proposal

import (
	"regexp"
	"strings"
)

const maxNameLength = 253

var nonAlphanumeric = regexp.MustCompile(`[^a-z0-9-]`)
var multiDash = regexp.MustCompile(`-{2,}`)

func ProposalName(alertname, namespace, fingerprint string) string {
	fp := fingerprint
	if len(fp) > 8 {
		fp = fp[:8]
	}

	parts := []string{sanitize(alertname)}
	if namespace != "" {
		parts = append(parts, sanitize(namespace))
	}
	parts = append(parts, sanitize(fp))

	name := strings.Join(parts, "-")
	if len(name) > maxNameLength {
		name = name[:maxNameLength]
	}
	name = strings.TrimRight(name, "-")
	return name
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	s = nonAlphanumeric.ReplaceAllString(s, "-")
	s = multiDash.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	return s
}
