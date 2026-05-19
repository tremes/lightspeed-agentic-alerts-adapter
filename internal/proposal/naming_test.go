package proposal

import (
	"regexp"
	"strings"
	"testing"
)

var dnsSubdomainRegex = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

func TestProposalName(t *testing.T) {
	tests := []struct {
		name        string
		alertname   string
		namespace   string
		fingerprint string
		want        string
	}{
		{
			name:        "normal case",
			alertname:   "KubePodCrashLooping",
			namespace:   "production",
			fingerprint: "a1b2c3d4e5f6",
			want:        "kubepodcrashlooping-production-a1b2c3d4",
		},
		{
			name:        "empty namespace produces double hyphen",
			alertname:   "etcdHighFsyncDurations",
			namespace:   "",
			fingerprint: "f9e8d7c6abcd",
			want:        "etcdhighfsyncdurations--f9e8d7c6",
		},
		{
			name:        "dots and underscores in alertname are sanitized to hyphens",
			alertname:   "kube_pod.restart",
			namespace:   "default",
			fingerprint: "abcdef123456",
			want:        "kube-pod-restart-default-abcdef12",
		},
		{
			name:        "leading and trailing special chars stripped",
			alertname:   "..._AlertName_...",
			namespace:   "ns",
			fingerprint: "1234567890ab",
			want:        "alertname-ns-12345678",
		},
		{
			name:        "consecutive special chars collapsed to single hyphen",
			alertname:   "alert___name...test",
			namespace:   "ns",
			fingerprint: "aabbccddee11",
			want:        "alert-name-test-ns-aabbccdd",
		},
		{
			name:        "fingerprint shorter than 8 chars used as-is",
			alertname:   "TestAlert",
			namespace:   "ns",
			fingerprint: "abc",
			want:        "testalert-ns-abc",
		},
		{
			name:        "fingerprint exactly 8 chars",
			alertname:   "TestAlert",
			namespace:   "ns",
			fingerprint: "12345678",
			want:        "testalert-ns-12345678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProposalName(tt.alertname, tt.namespace, tt.fingerprint)
			if got != tt.want {
				t.Errorf("ProposalName(%q, %q, %q) = %q, want %q",
					tt.alertname, tt.namespace, tt.fingerprint, got, tt.want)
			}
		})
	}
}

func TestProposalName_LongAlertname(t *testing.T) {
	longName := strings.Repeat("A", 300)
	namespace := "production"
	fingerprint := "a1b2c3d4e5f6"

	got := ProposalName(longName, namespace, fingerprint)

	if len(got) > 253 {
		t.Errorf("name length %d exceeds 253 character limit", len(got))
	}

	// Fingerprint suffix must be preserved
	if !strings.HasSuffix(got, "-a1b2c3d4") {
		t.Errorf("fingerprint suffix not preserved: got %q", got)
	}

	// Namespace must be preserved
	if !strings.Contains(got, "-production-") {
		t.Errorf("namespace not preserved in name: got %q", got)
	}
}

func TestProposalName_DNSCompliance(t *testing.T) {
	inputs := []struct {
		alertname   string
		namespace   string
		fingerprint string
	}{
		{"KubePodCrashLooping", "production", "a1b2c3d4e5f6"},
		{"etcdHighFsyncDurations", "", "f9e8d7c6abcd"},
		{"kube_pod.restart", "default", "abcdef123456"},
		{"..._AlertName_...", "ns", "1234567890ab"},
		{strings.Repeat("x", 300), "ns", "aabbccddee11"},
		{"TestAlert", "ns", "abc"},
	}

	for _, in := range inputs {
		name := ProposalName(in.alertname, in.namespace, in.fingerprint)
		if !dnsSubdomainRegex.MatchString(name) {
			t.Errorf("ProposalName(%q, %q, %q) = %q does not match DNS subdomain regex",
				in.alertname, in.namespace, in.fingerprint, name)
		}
		if len(name) > 253 {
			t.Errorf("ProposalName(%q, %q, %q) = %q exceeds 253 chars (len=%d)",
				in.alertname, in.namespace, in.fingerprint, name, len(name))
		}
	}
}

func TestProposalName_Deterministic(t *testing.T) {
	a := ProposalName("KubePodCrashLooping", "production", "a1b2c3d4e5f6")
	b := ProposalName("KubePodCrashLooping", "production", "a1b2c3d4e5f6")
	if a != b {
		t.Errorf("non-deterministic: %q != %q", a, b)
	}
}
