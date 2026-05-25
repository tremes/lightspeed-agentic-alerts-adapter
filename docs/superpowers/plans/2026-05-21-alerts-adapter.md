# Alerts Adapter Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go binary that polls AlertManager for firing alerts and creates Proposal CRs to trigger automated remediation workflows in the Lightspeed Agentic system.

**Architecture:** A single poll loop runs every 30s: fetch firing alerts from AlertManager, list existing Proposals from the Kubernetes API, diff them with deduplication logic (initial delay + cooldown window), and create Proposals for new alerts. Stateless — all state is reconstructed from AlertManager + K8s API each cycle. Uses deterministic Proposal naming (`{alertname}-{namespace}-{fingerprint[:8]}`) to prevent duplicates under concurrent execution.

**Tech Stack:** Go 1.25, `sigs.k8s.io/controller-runtime/pkg/client` (typed K8s client), `github.com/openshift/lightspeed-agentic-operator/api` (Proposal CRD types), `text/template` (request rendering), `net/http` (AlertManager polling), standard library for health probes.

---

## File Structure

```
.
├── cmd/
│   └── alerts-adapter/
│       └── main.go                    # Entrypoint: parse flags, build dependencies, run
├── internal/
│   ├── alertmanager/
│   │   ├── client.go                  # HTTP client for AlertManager GET /api/v2/alerts
│   │   └── client_test.go
│   ├── alertmanager/
│   │   └── types.go                   # Alert response struct matching AM v2 API
│   ├── proposal/
│   │   ├── builder.go                 # Alert → Proposal mapping (name, labels, request template)
│   │   └── builder_test.go
│   ├── proposal/
│   │   └── template.go               # Request template constant and rendering
│   ├── adapter/
│   │   ├── adapter.go                 # Poll loop + deduplication logic
│   │   └── adapter_test.go
│   └── health/
│       ├── handler.go                 # /healthz and /readyz HTTP handlers
│       └── handler_test.go
├── go.mod
├── go.sum
├── Dockerfile
└── Makefile
```

Flattened view (merged split directories):

| File | Responsibility |
|------|---------------|
| `cmd/alerts-adapter/main.go` | Entrypoint: wire dependencies, start poll loop + health server |
| `internal/alertmanager/types.go` | Go structs for AlertManager v2 API response |
| `internal/alertmanager/client.go` | HTTP client: fetch firing alerts from AlertManager |
| `internal/alertmanager/client_test.go` | Tests for AlertManager client (httptest server) |
| `internal/proposal/template.go` | Request template constant + rendering function |
| `internal/proposal/builder.go` | Alert → Proposal CR mapping (name, namespace, labels, spec) |
| `internal/proposal/builder_test.go` | Tests for Proposal builder |
| `internal/adapter/adapter.go` | Poll loop orchestrator: fetch alerts, list proposals, dedup, create |
| `internal/adapter/adapter_test.go` | Tests for adapter poll logic with fake K8s client |
| `internal/health/handler.go` | Health/readiness HTTP handlers |
| `internal/health/handler_test.go` | Tests for health handlers |
| `Dockerfile` | Multi-stage build |
| `Makefile` | build, test, lint, image targets |

---

### Task 1: Go Module and Makefile

**Files:**
- Create: `go.mod`
- Create: `Makefile`

- [ ] **Step 1: Initialize Go module**

```bash
cd /home/tremes/GITHUB/lightspeed-agentic-alerts-adapter
go mod init github.com/openshift/lightspeed-agentic-alerts-adapter
```

- [ ] **Step 2: Add operator API dependency**

```bash
go get github.com/openshift/lightspeed-agentic-operator/api@v0.0.0-20260519124222-af313bbf25be
go get sigs.k8s.io/controller-runtime@v0.23.3
```

- [ ] **Step 3: Create Makefile**

Create `Makefile`:

```makefile
BINARY     := alerts-adapter
IMAGE      := quay.io/openshift-lightspeed/lightspeed-agentic-alerts-adapter
IMAGE_TAG  ?= latest
GO         := go
GOFLAGS    ?=

.PHONY: build test lint image clean

build:
	$(GO) build $(GOFLAGS) -o bin/$(BINARY) ./cmd/alerts-adapter

test:
	$(GO) test ./... -race -count=1

lint:
	golangci-lint run ./...

image:
	podman build -t $(IMAGE):$(IMAGE_TAG) .

clean:
	rm -rf bin/
```

- [ ] **Step 4: Commit**

```bash
git add go.mod go.sum Makefile
git commit -m "feat: initialize Go module with operator API dependency"
```

---

### Task 2: AlertManager Types

**Files:**
- Create: `internal/alertmanager/types.go`

- [ ] **Step 1: Create directory**

```bash
mkdir -p internal/alertmanager
```

- [ ] **Step 2: Write AlertManager v2 API response types**

Create `internal/alertmanager/types.go`:

```go
package alertmanager

import "time"

type AlertStatus struct {
	State       string   `json:"state"`
	SilencedBy  []string `json:"silencedBy"`
	InhibitedBy []string `json:"inhibitedBy"`
}

type Alert struct {
	Fingerprint  string            `json:"fingerprint"`
	Status       AlertStatus       `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	UpdatedAt    time.Time         `json:"updatedAt"`
}
```

- [ ] **Step 3: Commit**

```bash
git add internal/alertmanager/types.go
git commit -m "feat: add AlertManager v2 API response types"
```

---

### Task 3: AlertManager Client

**Files:**
- Create: `internal/alertmanager/client.go`
- Create: `internal/alertmanager/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/alertmanager/client_test.go`:

```go
package alertmanager_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

func TestClient_FiringAlerts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	alerts := []alertmanager.Alert{
		{
			Fingerprint: "abc123",
			Status:      alertmanager.AlertStatus{State: "active"},
			Labels:      map[string]string{"alertname": "KubePodCrashLooping", "namespace": "production", "severity": "warning"},
			Annotations: map[string]string{"summary": "Pod is crash looping", "description": "Pod web-abc is crash looping"},
			StartsAt:    now.Add(-10 * time.Minute),
		},
		{
			Fingerprint: "def456",
			Status:      alertmanager.AlertStatus{State: "suppressed"},
			Labels:      map[string]string{"alertname": "Watchdog", "severity": "none"},
			StartsAt:    now.Add(-1 * time.Hour),
		},
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/alerts" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("active") != "true" {
			t.Error("expected active=true query param")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(alerts)
	}))
	defer srv.Close()

	client := alertmanager.NewClient(srv.URL, srv.Client())
	got, err := client.FiringAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(got))
	}
	if got[0].Fingerprint != "abc123" {
		t.Errorf("expected fingerprint abc123, got %s", got[0].Fingerprint)
	}
	if got[0].Labels["alertname"] != "KubePodCrashLooping" {
		t.Errorf("expected alertname KubePodCrashLooping, got %s", got[0].Labels["alertname"])
	}
}

func TestClient_FiringAlerts_ServerError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := alertmanager.NewClient(srv.URL, srv.Client())
	_, err := client.FiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestClient_FiringAlerts_InvalidJSON(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := alertmanager.NewClient(srv.URL, srv.Client())
	_, err := client.FiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

```bash
go test ./internal/alertmanager/... -v
```

Expected: FAIL — `alertmanager.NewClient` is not defined.

- [ ] **Step 3: Write the AlertManager client**

Create `internal/alertmanager/client.go`:

```go
package alertmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
}

func NewClient(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (c *Client) FiringAlerts(ctx context.Context) ([]Alert, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v2/alerts?active=true", nil)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching alerts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d from AlertManager", resp.StatusCode)
	}

	var alerts []Alert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, fmt.Errorf("decoding alerts: %w", err)
	}

	return alerts, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/alertmanager/... -v -count=1
```

Expected: PASS (all 3 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/alertmanager/client.go internal/alertmanager/client_test.go
git commit -m "feat: add AlertManager HTTP client for fetching firing alerts"
```

---

### Task 4: Request Template

**Files:**
- Create: `internal/proposal/template.go`

- [ ] **Step 1: Create directory**

```bash
mkdir -p internal/proposal
```

- [ ] **Step 2: Write the request template**

Create `internal/proposal/template.go`:

```go
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
```

- [ ] **Step 3: Commit**

```bash
git add internal/proposal/template.go
git commit -m "feat: add request template for alert-to-Proposal mapping"
```

---

### Task 5: Proposal Builder

**Files:**
- Create: `internal/proposal/builder.go`
- Create: `internal/proposal/builder_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/proposal/builder_test.go`:

```go
package proposal_test

import (
	"strings"
	"testing"
	"time"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/proposal"
)

func TestBuildProposalName(t *testing.T) {
	tests := []struct {
		name        string
		alertName   string
		namespace   string
		fingerprint string
		want        string
	}{
		{
			name:        "namespaced alert",
			alertName:   "KubePodCrashLooping",
			namespace:   "production",
			fingerprint: "a1b2c3d4e5f6",
			want:        "kubepodcrashlooping-production-a1b2c3d4",
		},
		{
			name:        "cluster-scoped alert (no namespace)",
			alertName:   "etcdHighFsyncDurations",
			namespace:   "",
			fingerprint: "f9e8d7c6b5a4",
			want:        "etcdhighfsyncdurations--f9e8d7c6",
		},
		{
			name:        "special characters sanitized",
			alertName:   "My_Alert.Name",
			namespace:   "my-ns",
			fingerprint: "1234567890ab",
			want:        "my-alert-name-my-ns-12345678",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := proposal.BuildProposalName(tt.alertName, tt.namespace, tt.fingerprint)
			if got != tt.want {
				t.Errorf("BuildProposalName() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildProposal(t *testing.T) {
	alert := alertmanager.Alert{
		Fingerprint: "abc12345def6",
		Labels: map[string]string{
			"alertname": "KubePodCrashLooping",
			"namespace": "production",
			"severity":  "warning",
		},
		Annotations: map[string]string{
			"summary":     "Pod is crash looping",
			"description": "Pod web-abc is crash looping in namespace production",
		},
		StartsAt: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	}

	p, err := proposal.BuildProposal(alert, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Name != "kubepodcrashlooping-production-abc12345" {
		t.Errorf("unexpected name: %s", p.Name)
	}
	if p.Namespace != "production" {
		t.Errorf("unexpected namespace: %s", p.Namespace)
	}
	if p.Labels["agentic.openshift.io/source"] != "alertmanager" {
		t.Errorf("missing source label")
	}
	if p.Labels["agentic.openshift.io/alert-fingerprint"] != "abc12345" {
		t.Errorf("unexpected fingerprint label: %s", p.Labels["agentic.openshift.io/alert-fingerprint"])
	}
	if p.Labels["agentic.openshift.io/alert-name"] != "kubepodcrashlooping" {
		t.Errorf("unexpected alert-name label: %s", p.Labels["agentic.openshift.io/alert-name"])
	}
	if p.Labels["agentic.openshift.io/alert-severity"] != "warning" {
		t.Errorf("unexpected severity label: %s", p.Labels["agentic.openshift.io/alert-severity"])
	}
	if p.Annotations["agentic.openshift.io/alert-starts-at"] != "2026-05-21T10:00:00Z" {
		t.Errorf("unexpected starts-at annotation: %s", p.Annotations["agentic.openshift.io/alert-starts-at"])
	}
	if !strings.Contains(p.Annotations["agentic.openshift.io/alert-summary"], "Pod is crash looping") {
		t.Errorf("unexpected summary annotation: %s", p.Annotations["agentic.openshift.io/alert-summary"])
	}
	if !strings.Contains(p.Spec.Request, "KubePodCrashLooping") {
		t.Errorf("request should contain alert name")
	}
	if len(p.Spec.TargetNamespaces) != 1 || p.Spec.TargetNamespaces[0] != "production" {
		t.Errorf("unexpected target namespaces: %v", p.Spec.TargetNamespaces)
	}
	if p.Spec.Analysis.Agent != "default" {
		t.Errorf("unexpected analysis agent: %s", p.Spec.Analysis.Agent)
	}
	if p.Spec.Execution.Agent != "default" {
		t.Errorf("unexpected execution agent: %s", p.Spec.Execution.Agent)
	}
	if p.Spec.Verification.Agent != "default" {
		t.Errorf("unexpected verification agent: %s", p.Spec.Verification.Agent)
	}
}

func TestBuildProposal_ClusterScoped(t *testing.T) {
	alert := alertmanager.Alert{
		Fingerprint: "ff11223344",
		Labels: map[string]string{
			"alertname": "etcdHighFsyncDurations",
			"severity":  "critical",
		},
		Annotations: map[string]string{
			"summary": "etcd fsync durations are high",
		},
		StartsAt: time.Date(2026, 5, 21, 10, 0, 0, 0, time.UTC),
	}

	p, err := proposal.BuildProposal(alert, "default")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Namespace != "openshift-lightspeed" {
		t.Errorf("cluster-scoped alert should use default namespace, got: %s", p.Namespace)
	}
	if len(p.Spec.TargetNamespaces) != 0 {
		t.Errorf("cluster-scoped alert should have empty target namespaces, got: %v", p.Spec.TargetNamespaces)
	}
}

func TestBuildProposal_MissingAlertName(t *testing.T) {
	alert := alertmanager.Alert{
		Fingerprint: "abc123",
		Labels:      map[string]string{"severity": "warning"},
		StartsAt:    time.Now(),
	}

	_, err := proposal.BuildProposal(alert, "default")
	if err == nil {
		t.Fatal("expected error for missing alertname")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/proposal/... -v
```

Expected: FAIL — `proposal.BuildProposalName` and `proposal.BuildProposal` are not defined.

- [ ] **Step 3: Write the Proposal builder**

Create `internal/proposal/builder.go`:

```go
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

func truncateFingerprint(fingerprint string) string {
	if len(fingerprint) > 8 {
		return fingerprint[:8]
	}
	return fingerprint
}

func BuildProposalName(alertName, namespace, fingerprint string) string {
	name := sanitizeDNS(alertName) + "-" + sanitizeDNS(namespace) + "-" + truncateFingerprint(fingerprint)
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
	fp := truncateFingerprint(alert.Fingerprint)

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
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/proposal/... -v -count=1
```

Expected: PASS (all 4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/proposal/builder.go internal/proposal/builder_test.go
git commit -m "feat: add Proposal builder for alert-to-Proposal CR mapping"
```

---

### Task 6: Health Handlers

**Files:**
- Create: `internal/health/handler.go`
- Create: `internal/health/handler_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/health/handler_test.go`:

```go
package health_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/health"
)

func TestHealthz(t *testing.T) {
	h := health.NewHandler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	h.Healthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestReadyz_InitiallyNotReady(t *testing.T) {
	h := health.NewHandler()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 initially, got %d", rec.Code)
	}
}

func TestReadyz_AfterSetReady(t *testing.T) {
	h := health.NewHandler()
	h.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Readyz(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after SetReady(true), got %d", rec.Code)
	}
}

func TestReadyz_SetNotReady(t *testing.T) {
	h := health.NewHandler()
	h.SetReady(true)
	h.SetReady(false)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 after SetReady(false), got %d", rec.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/health/... -v
```

Expected: FAIL — `health.NewHandler` is not defined.

- [ ] **Step 3: Write the health handler**

Create `internal/health/handler.go`:

```go
package health

import (
	"net/http"
	"sync/atomic"
)

type Handler struct {
	ready atomic.Bool
}

func NewHandler() *Handler {
	return &Handler{}
}

func (h *Handler) SetReady(ready bool) {
	h.ready.Store(ready)
}

func (h *Handler) Healthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (h *Handler) Readyz(w http.ResponseWriter, _ *http.Request) {
	if h.ready.Load() {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("not ready"))
}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/health/... -v -count=1
```

Expected: PASS (all 4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/health/handler.go internal/health/handler_test.go
git commit -m "feat: add health and readiness probe handlers"
```

---

### Task 7: Adapter (Poll Loop + Deduplication)

**Files:**
- Create: `internal/adapter/adapter.go`
- Create: `internal/adapter/adapter_test.go`

This is the core logic: the poll loop orchestrator with deduplication.

- [ ] **Step 1: Write the failing tests**

Create `internal/adapter/adapter_test.go`:

```go
package adapter_test

import (
	"context"
	"testing"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

func newScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := agenticv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("failed to add scheme: %v", err)
	}
	return s
}

type fakeAMClient struct {
	alerts []alertmanager.Alert
	err    error
}

func (f *fakeAMClient) FiringAlerts(_ context.Context) ([]alertmanager.Alert, error) {
	return f.alerts, f.err
}

func TestReconcile_CreatesProposalForNewAlert(t *testing.T) {
	scheme := newScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				Annotations: map[string]string{
					"summary": "Pod is crash looping",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	err := a.Reconcile(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Fatalf("expected 1 proposal, got %d", len(proposals.Items))
	}
	if proposals.Items[0].Name != "kubepodcrashlooping-production-abc12345" {
		t.Errorf("unexpected proposal name: %s", proposals.Items[0].Name)
	}
}

func TestReconcile_SkipsAlertWithinInitialDelay(t *testing.T) {
	scheme := newScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				StartsAt: now.Add(-2 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 0 {
		t.Fatalf("expected 0 proposals (alert within initial delay), got %d", len(proposals.Items))
	}
}

func TestReconcile_SkipsAlertWithExistingActiveProposal(t *testing.T) {
	scheme := newScheme(t)

	existingProposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubepodcrashlooping-production-abc12345",
			Namespace: "production",
			Labels: map[string]string{
				"agentic.openshift.io/source":            "alertmanager",
				"agentic.openshift.io/alert-fingerprint": "abc12345",
			},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:  "test",
			Analysis: agenticv1alpha1.ProposalStep{Agent: "default"},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingProposal).
		Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Errorf("expected 1 proposal (existing, no new one), got %d", len(proposals.Items))
	}
}

func TestReconcile_SkipsAlertWithTerminalProposalInCooldown(t *testing.T) {
	scheme := newScheme(t)

	now := time.Now().UTC()
	terminalProposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubepodcrashlooping-production-abc12345",
			Namespace: "production",
			Labels: map[string]string{
				"agentic.openshift.io/source":            "alertmanager",
				"agentic.openshift.io/alert-fingerprint": "abc12345",
			},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:  "test",
			Analysis: agenticv1alpha1.ProposalStep{Agent: "default"},
		},
		Status: agenticv1alpha1.ProposalStatus{
			Conditions: []metav1.Condition{
				{
					Type:               agenticv1alpha1.ProposalConditionVerified,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(now.Add(-30 * time.Minute)),
					Reason:             "Completed",
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(terminalProposal).
		WithStatusSubresource(&agenticv1alpha1.Proposal{}).
		Build()

	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Errorf("expected 1 proposal (terminal in cooldown, no new one), got %d", len(proposals.Items))
	}
}

func TestReconcile_CreatesProposalAfterCooldownExpired(t *testing.T) {
	scheme := newScheme(t)

	now := time.Now().UTC()
	terminalProposal := &agenticv1alpha1.Proposal{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "kubepodcrashlooping-production-abc12345",
			Namespace: "production",
			Labels: map[string]string{
				"agentic.openshift.io/source":            "alertmanager",
				"agentic.openshift.io/alert-fingerprint": "abc12345",
			},
		},
		Spec: agenticv1alpha1.ProposalSpec{
			Request:  "test",
			Analysis: agenticv1alpha1.ProposalStep{Agent: "default"},
		},
		Status: agenticv1alpha1.ProposalStatus{
			Conditions: []metav1.Condition{
				{
					Type:               agenticv1alpha1.ProposalConditionVerified,
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.NewTime(now.Add(-2 * time.Hour)),
					Reason:             "Completed",
				},
			},
		},
	}

	k8sClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(terminalProposal).
		WithStatusSubresource(&agenticv1alpha1.Proposal{}).
		Build()

	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "abc12345def6",
				Labels: map[string]string{
					"alertname": "KubePodCrashLooping",
					"namespace": "production",
					"severity":  "warning",
				},
				Annotations: map[string]string{
					"summary": "Pod is crash looping",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 2 {
		t.Fatalf("expected 2 proposals (old terminal + new), got %d", len(proposals.Items))
	}
}

func TestReconcile_ContinuesOnInvalidAlert(t *testing.T) {
	scheme := newScheme(t)
	k8sClient := fake.NewClientBuilder().WithScheme(scheme).Build()

	now := time.Now().UTC()
	amClient := &fakeAMClient{
		alerts: []alertmanager.Alert{
			{
				Fingerprint: "bad1",
				Labels:      map[string]string{"severity": "warning"},
				StartsAt:    now.Add(-10 * time.Minute),
			},
			{
				Fingerprint: "good1234abcd",
				Labels: map[string]string{
					"alertname": "ValidAlert",
					"namespace": "test-ns",
					"severity":  "info",
				},
				Annotations: map[string]string{
					"summary": "A valid alert",
				},
				StartsAt: now.Add(-10 * time.Minute),
			},
		},
	}

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   5 * time.Minute,
		CooldownWindow: 1 * time.Hour,
		AgentName:      "default",
	})

	if err := a.Reconcile(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var proposals agenticv1alpha1.ProposalList
	if err := k8sClient.List(context.Background(), &proposals); err != nil {
		t.Fatalf("failed to list proposals: %v", err)
	}
	if len(proposals.Items) != 1 {
		t.Fatalf("expected 1 proposal (skipped invalid, created valid), got %d", len(proposals.Items))
	}
	if proposals.Items[0].Labels["agentic.openshift.io/alert-name"] != "validalert" {
		t.Errorf("unexpected proposal: %s", proposals.Items[0].Name)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/adapter/... -v
```

Expected: FAIL — `adapter` package does not exist.

- [ ] **Step 3: Write the adapter**

Create `internal/adapter/adapter.go`:

```go
package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/proposal"
)

type AlertFetcher interface {
	FiringAlerts(ctx context.Context) ([]alertmanager.Alert, error)
}

type Config struct {
	InitialDelay   time.Duration
	CooldownWindow time.Duration
	AgentName      string
}

type Adapter struct {
	alerts    AlertFetcher
	k8s       client.Client
	config    Config
}

func New(alerts AlertFetcher, k8s client.Client, config Config) *Adapter {
	return &Adapter{
		alerts: alerts,
		k8s:    k8s,
		config: config,
	}
}

func (a *Adapter) Reconcile(ctx context.Context) error {
	alerts, err := a.alerts.FiringAlerts(ctx)
	if err != nil {
		return fmt.Errorf("fetching alerts: %w", err)
	}

	var existingProposals agenticv1alpha1.ProposalList
	if err := a.k8s.List(ctx, &existingProposals, client.MatchingLabels{
		proposal.LabelSource: proposal.SourceAlertManager,
	}); err != nil {
		return fmt.Errorf("listing proposals: %w", err)
	}

	proposalsByFingerprint := make(map[string][]agenticv1alpha1.Proposal)
	for _, p := range existingProposals.Items {
		fp := p.Labels[proposal.LabelAlertFingerprint]
		proposalsByFingerprint[fp] = append(proposalsByFingerprint[fp], p)
	}

	now := time.Now().UTC()

	for _, alert := range alerts {
		fp := proposal.TruncateFingerprint(alert.Fingerprint)

		if now.Sub(alert.StartsAt) < a.config.InitialDelay {
			slog.Debug("skipping alert within initial delay",
				"alertname", alert.Labels["alertname"],
				"fingerprint", fp,
				"age", now.Sub(alert.StartsAt).String())
			continue
		}

		if a.hasActiveProposal(proposalsByFingerprint[fp]) {
			slog.Debug("skipping alert with active proposal",
				"alertname", alert.Labels["alertname"],
				"fingerprint", fp)
			continue
		}

		if a.hasTerminalProposalInCooldown(proposalsByFingerprint[fp], now) {
			slog.Debug("skipping alert within cooldown window",
				"alertname", alert.Labels["alertname"],
				"fingerprint", fp)
			continue
		}

		p, err := proposal.BuildProposal(alert, a.config.AgentName)
		if err != nil {
			slog.Error("skipping invalid alert",
				"fingerprint", alert.Fingerprint,
				"error", err.Error())
			continue
		}

		if err := a.k8s.Create(ctx, p); err != nil {
			slog.Error("failed to create proposal",
				"proposal", p.Name,
				"namespace", p.Namespace,
				"error", err.Error())
			continue
		}

		slog.Info("created proposal",
			"proposal", p.Name,
			"namespace", p.Namespace,
			"alertname", alert.Labels["alertname"],
			"fingerprint", fp)
	}

	return nil
}

func (a *Adapter) hasActiveProposal(proposals []agenticv1alpha1.Proposal) bool {
	for _, p := range proposals {
		phase := agenticv1alpha1.DerivePhase(p.Status.Conditions)
		switch phase {
		case agenticv1alpha1.ProposalPhaseCompleted,
			agenticv1alpha1.ProposalPhaseFailed,
			agenticv1alpha1.ProposalPhaseEscalated,
			agenticv1alpha1.ProposalPhaseDenied:
			continue
		default:
			return true
		}
	}
	return false
}

func (a *Adapter) hasTerminalProposalInCooldown(proposals []agenticv1alpha1.Proposal, now time.Time) bool {
	for _, p := range proposals {
		phase := agenticv1alpha1.DerivePhase(p.Status.Conditions)
		switch phase {
		case agenticv1alpha1.ProposalPhaseCompleted,
			agenticv1alpha1.ProposalPhaseFailed,
			agenticv1alpha1.ProposalPhaseEscalated,
			agenticv1alpha1.ProposalPhaseDenied:
			terminalTime := a.terminalTransitionTime(p.Status.Conditions)
			if now.Sub(terminalTime) < a.config.CooldownWindow {
				return true
			}
		}
	}
	return false
}

func (a *Adapter) terminalTransitionTime(conditions []metav1.Condition) time.Time {
	var latest time.Time
	for _, c := range conditions {
		if c.Status == metav1.ConditionTrue && c.LastTransitionTime.After(latest) {
			latest = c.LastTransitionTime.Time
		}
	}
	return latest
}
```

Note: This requires exporting `TruncateFingerprint` from `internal/proposal/builder.go`. Update `builder.go` to rename `truncateFingerprint` → `TruncateFingerprint`:

In `internal/proposal/builder.go`, change:
- `func truncateFingerprint(fingerprint string) string {` → `func TruncateFingerprint(fingerprint string) string {`
- All call sites within `builder.go` that use `truncateFingerprint` → `TruncateFingerprint`

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/adapter/... -v -count=1
```

Expected: PASS (all 6 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/adapter.go internal/adapter/adapter_test.go internal/proposal/builder.go
git commit -m "feat: add adapter poll loop with deduplication logic"
```

---

### Task 8: Main Entrypoint

**Files:**
- Create: `cmd/alerts-adapter/main.go`

- [ ] **Step 1: Create directory**

```bash
mkdir -p cmd/alerts-adapter
```

- [ ] **Step 2: Write the main entrypoint**

Create `cmd/alerts-adapter/main.go`:

```go
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/health"
)

const (
	PollInterval    = 30 * time.Second
	InitialDelay    = 5 * time.Minute
	CooldownWindow  = 1 * time.Hour
	AlertManagerURL = "https://alertmanager-main.openshift-monitoring.svc:9093"
	DefaultAgent    = "default"
	HealthAddr      = ":8080"
	ServiceCAPath   = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"
	SATokenPath     = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		slog.Error("fatal error", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	scheme := runtime.NewScheme()
	if err := agenticv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("adding agentic scheme: %w", err)
	}

	restConfig, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("getting in-cluster config: %w", err)
	}

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	httpClient, err := buildAlertManagerHTTPClient()
	if err != nil {
		return fmt.Errorf("building AlertManager HTTP client: %w", err)
	}

	amClient := alertmanager.NewClient(AlertManagerURL, httpClient)

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   InitialDelay,
		CooldownWindow: CooldownWindow,
		AgentName:      DefaultAgent,
	})

	healthHandler := health.NewHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler.Healthz)
	mux.HandleFunc("/readyz", healthHandler.Readyz)

	healthServer := &http.Server{
		Addr:    HealthAddr,
		Handler: mux,
	}

	go func() {
		slog.Info("starting health server", "addr", HealthAddr)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "error", err.Error())
		}
	}()

	slog.Info("starting poll loop",
		"interval", PollInterval.String(),
		"initialDelay", InitialDelay.String(),
		"cooldownWindow", CooldownWindow.String(),
		"alertManagerURL", AlertManagerURL)

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	for {
		if err := a.Reconcile(ctx); err != nil {
			slog.Error("poll cycle failed", "error", err.Error())
			healthHandler.SetReady(false)
		} else {
			healthHandler.SetReady(true)
		}

		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			healthServer.Shutdown(shutdownCtx)
			return nil
		case <-ticker.C:
		}
	}
}

func buildAlertManagerHTTPClient() (*http.Client, error) {
	caCert, err := os.ReadFile(ServiceCAPath)
	if err != nil {
		return nil, fmt.Errorf("reading service CA: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse service CA certificate")
	}

	token, err := os.ReadFile(SATokenPath)
	if err != nil {
		return nil, fmt.Errorf("reading service account token: %w", err)
	}

	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &bearerTokenTransport{
			token: string(token),
			base: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: pool,
				},
			},
		},
	}, nil
}

type bearerTokenTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}
```

- [ ] **Step 3: Verify compilation**

```bash
go build ./cmd/alerts-adapter/
```

Expected: Build succeeds (binary won't run outside a cluster, but compilation validates correctness).

- [ ] **Step 4: Commit**

```bash
git add cmd/alerts-adapter/main.go
git commit -m "feat: add main entrypoint with poll loop, health server, and AM auth"
```

---

### Task 9: Dockerfile

**Files:**
- Create: `Dockerfile`

- [ ] **Step 1: Write the Dockerfile**

Create `Dockerfile`:

```dockerfile
FROM registry.access.redhat.com/ubi9/go-toolset:1.22 AS builder
WORKDIR /opt/app-root/src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /opt/app-root/alerts-adapter ./cmd/alerts-adapter

FROM registry.access.redhat.com/ubi9-micro:latest
COPY --from=builder /opt/app-root/alerts-adapter /usr/local/bin/alerts-adapter
USER 65534:65534
ENTRYPOINT ["/usr/local/bin/alerts-adapter"]
```

- [ ] **Step 2: Commit**

```bash
git add Dockerfile
git commit -m "feat: add multi-stage Dockerfile"
```

---

### Task 10: Run Full Test Suite and Final Verification

- [ ] **Step 1: Run all tests**

```bash
make test
```

Expected: All tests pass.

- [ ] **Step 2: Run build**

```bash
make build
```

Expected: Binary created at `bin/alerts-adapter`.

- [ ] **Step 3: Verify go vet**

```bash
go vet ./...
```

Expected: No issues.

- [ ] **Step 4: Commit any fixes if needed**

Only if steps 1-3 revealed issues.

---

## Self-Review Notes

**Spec coverage:**
- Poll AlertManager: Task 3 (client), Task 7 (poll loop in adapter), Task 8 (ticker in main).
- Create Proposals: Task 5 (builder), Task 7 (adapter creates via k8s client).
- Deduplication (initial delay + cooldown): Task 7 (adapter.Reconcile logic + tests).
- Alert-to-Proposal mapping: Task 4 (template), Task 5 (builder with all labels/annotations/spec fields).
- Deterministic naming: Task 5 (BuildProposalName + tests).
- Stateless design: Task 7 (adapter rebuilds state each cycle from AM + K8s).
- Health probes: Task 6 (handler), Task 8 (wired in main).
- Error handling (AM unreachable, K8s unreachable, invalid alerts): Task 7 (adapter continues on errors, tests verify).
- AlertManager auth (SA token + TLS): Task 8 (bearerTokenTransport + service CA).
- RBAC manifests: Documented in spec but not code — these are static YAML applied by the operator, not generated by the adapter. Out of scope for the Go binary.
- Deployment YAML: Same — static manifest, not generated code.

**Placeholder scan:** No TBD, TODO, or "implement later" found. All code steps include complete code.

**Type consistency:**
- `TruncateFingerprint` is used in both `builder.go` and `adapter.go` consistently.
- `AlertFetcher` interface in adapter matches `Client.FiringAlerts` signature.
- Label constants (`LabelSource`, `LabelAlertFingerprint`, etc.) used in both builder and adapter.
- `proposal.BuildProposal` returns `*agenticv1alpha1.Proposal` — consistent with `k8sClient.Create`.
