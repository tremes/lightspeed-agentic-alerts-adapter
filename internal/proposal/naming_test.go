package proposal

import (
	"strings"
	"testing"
)

func TestProposalName(t *testing.T) {
	tests := []struct {
		name        string
		alertname   string
		namespace   string
		fingerprint string
		want        string
	}{
		{
			name:        "standard case",
			alertname:   "KubePodCrashLooping",
			namespace:   "production",
			fingerprint: "a1b2c3d4e5f6",
			want:        "kubepodcrashlooping-production-a1b2c3d4",
		},
		{
			name:        "no namespace (cluster-scoped)",
			alertname:   "EtcdHighFsyncDurations",
			namespace:   "",
			fingerprint: "f9e8d7c6b5a4",
			want:        "etcdhighfsyncdurations-f9e8d7c6",
		},
		{
			name:        "short fingerprint",
			alertname:   "TestAlert",
			namespace:   "ns",
			fingerprint: "abc",
			want:        "testalert-ns-abc",
		},
		{
			name:        "special characters",
			alertname:   "Alert.With_Special/Chars",
			namespace:   "my-namespace",
			fingerprint: "1234567890ab",
			want:        "alert-with-special-chars-my-namespace-12345678",
		},
		{
			name:        "long alertname truncated",
			alertname:   strings.Repeat("a", 250),
			namespace:   "ns",
			fingerprint: "12345678",
			want:        truncatedTo253(strings.Repeat("a", 250) + "-ns-12345678"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ProposalName(tt.alertname, tt.namespace, tt.fingerprint)
			if got != tt.want {
				t.Errorf("ProposalName(%q, %q, %q) = %q, want %q",
					tt.alertname, tt.namespace, tt.fingerprint, got, tt.want)
			}
			if len(got) > 253 {
				t.Errorf("name exceeds 253 characters: %d", len(got))
			}
		})
	}
}

func truncatedTo253(s string) string {
	if len(s) > 253 {
		s = s[:253]
	}
	return strings.TrimRight(s, "-")
}
