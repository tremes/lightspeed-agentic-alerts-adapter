# Implementation Plan: lightspeed-agentic-alerts-adapter

Based on [.ai/spec/initial-design.md](../spec/initial-design.md).

## Overview

Build a stateless Go binary that polls AlertManager for firing alerts and creates Proposal CRs to trigger the Lightspeed Agentic operator's analysis/remediation workflows.

## Tech Stack

- **Language**: Go 1.26
- **Dependencies**: `k8s.io/client-go`, `github.com/openshift/lightspeed-agentic-operator/api`
- **Build**: `CGO_ENABLED=0` static binary, multi-stage Containerfile on UBI9
- **Logging**: `log/slog` (JSON)
- **No frameworks**: Standard library HTTP client for AlertManager, `client-go` dynamic or typed client for K8s

## Phases

The implementation is broken into 5 phases, each independently buildable and testable.

---

### Phase 1: Project Scaffolding & Go Module

**Goal**: Working Go module with directory structure, Makefile, and a "hello world" binary.

**Tasks**:
1. Initialize `go.mod` (`module github.com/openshift/lightspeed-agentic-alerts-adapter`)
2. Create directory structure:
   ```
   cmd/main.go
   internal/alertmanager/
   internal/proposal/
   internal/poller/
   ```
3. Create `Makefile` with targets: `build`, `test`, `lint`, `clean`
4. Create `cmd/main.go` with:
   - Signal handling (`SIGTERM`, `SIGINT`) via `context.WithCancel`
   - Placeholder for the poll loop
   - `slog` JSON logger setup
5. Verify: `make build` produces `bin/alerts-adapter`

**Files**: `go.mod`, `cmd/main.go`, `Makefile`

---

### Phase 2: AlertManager Client

**Goal**: HTTP client that fetches firing alerts from AlertManager's `GET /api/v2/alerts` endpoint.

**Tasks**:
1. Define alert response types in `internal/alertmanager/types.go`:
   - `Alert` struct matching AlertManager's v2 API response (labels, annotations, status, fingerprint, startsAt, endsAt, generatorURL)
   - `AlertStatus` struct (state, silencedBy, inhibitedBy)
2. Implement `internal/alertmanager/client.go`:
   - `Client` struct with base URL and `*http.Client`
   - `NewClient(baseURL string) *Client` — configures TLS using the in-cluster CA bundle (`/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`), sets `Authorization: Bearer <token>` from `/var/run/secrets/kubernetes.io/serviceaccount/token`
   - `GetFiringAlerts(ctx context.Context) ([]Alert, error)` — calls `GET {baseURL}/api/v2/alerts?active=true&silenced=false&inhibited=false`, decodes JSON response
3. Write unit tests with a test HTTP server returning canned AlertManager responses:
   - Happy path: returns multiple alerts
   - Empty response: no firing alerts
   - Error scenarios: 500, malformed JSON, network timeout

**Files**: `internal/alertmanager/types.go`, `internal/alertmanager/client.go`, `internal/alertmanager/client_test.go`

---

### Phase 3: Proposal Builder & Naming

**Goal**: Functions to map an alert to a Proposal CR, including deterministic naming and request template rendering.

**Tasks**:
1. Implement `internal/proposal/naming.go`:
   - `ProposalName(alertname, namespace, fingerprint string) string` — generates `{alertname}-{namespace}-{fingerprint[:8]}`, sanitized to DNS subdomain rules (RFC 1123: lowercase, replace non-alphanumeric with `-`, truncate to 253 chars)
   - Handle edge cases: empty namespace (cluster-scoped alerts), long alertnames, special characters
2. Write unit tests for naming:
   - Standard case: `KubePodCrashLooping`, namespace `production`, fingerprint `a1b2c3d4e5f6`
   - No namespace: cluster-scoped alert
   - Long alertname: truncation
   - Special characters: sanitization
3. Implement `internal/proposal/builder.go`:
   - `BuildProposal(alert alertmanager.Alert) (*unstructured.Unstructured, error)` — constructs the full Proposal CR:
     - Sets API version `agentic.openshift.io/v1alpha1`, kind `Proposal`
     - Deterministic name from `ProposalName()`
     - Namespace from alert's `namespace` label (fallback to `openshift-lightspeed`)
     - `spec.request` rendered from Go `text/template` with alert data
     - `spec.targetNamespaces` set to `[namespace]` or empty for cluster-scoped
     - `spec.analysis`, `spec.execution`, `spec.verification` all with `agent: default`
     - `spec.analysisOutput.mode: Default`
     - Labels: `agentic.openshift.io/source`, `alert-fingerprint`, `alert-name`, `alert-severity`
     - Annotations: `alert-starts-at`, `alert-summary` (truncated)
   - Uses typed Proposal objects from `github.com/openshift/lightspeed-agentic-operator/api/v1alpha1`. The typed client provides compile-time safety and direct access to `ProposalSpec`, `ProposalStatus`, and condition types.
4. Write unit tests for builder:
   - Complete alert → valid Proposal with all fields
   - Cluster-scoped alert (no namespace) → fallback namespace, empty targetNamespaces
   - Missing annotations (no summary, no description) → graceful handling
   - Template rendering with special characters

**Files**: `internal/proposal/naming.go`, `internal/proposal/naming_test.go`, `internal/proposal/builder.go`, `internal/proposal/builder_test.go`

**Decision**: Use typed Proposal objects from `github.com/openshift/lightspeed-agentic-operator/api/v1alpha1` for compile-time safety and direct access to spec/status types.

---

### Phase 4: Poll Loop & Deduplication

**Goal**: The core poll loop that fetches alerts, diffs against existing Proposals, and creates new ones.

**Tasks**:
1. Implement `internal/poller/poller.go`:
   - `Poller` struct with AlertManager client, typed K8s client (`k8s.io/client-go` with generated or hand-written clientset for Proposals), logger, and config constants
   - `NewPoller(amClient *alertmanager.Client, proposalClient ProposalClient, logger *slog.Logger) *Poller`
   - `Run(ctx context.Context)` — runs the poll loop on a `time.Ticker` (30s), exits on context cancellation
   - `poll(ctx context.Context) error` — single poll cycle:
     1. Fetch firing alerts from AlertManager
     2. List existing Proposals with label selector `agentic.openshift.io/source=alertmanager`
     3. For each alert, apply deduplication checks:
        - **Initial delay**: skip if `time.Now() - alert.StartsAt < InitialDelay` (5 min)
        - **Active Proposal exists**: skip if a non-terminal Proposal with matching fingerprint label exists. Terminal phases: `Completed`, `Failed`, `Escalated`, `Denied`. The phase is derived from status conditions (see below).
        - **Cooldown window**: skip if a terminal Proposal with matching fingerprint has a terminal condition timestamp within `CooldownWindow` (1 hour)
     4. Create Proposal for alerts that pass all checks
   - Phase derivation from conditions: The Proposal CRD doesn't store phase directly — it's derived from conditions. For the adapter's purposes, a Proposal is "terminal" when any of these conditions are True: `Analyzed` + `Executed` + `Verified` (= Completed), `Denied`, or `Executed/Verified` with Status=False and no retries remaining (= Failed), or `Escalated` with Status=True. A simpler heuristic: check if `status.conditions` contains a condition with type `Denied` and status `True`, OR type `Verified` with status `True` (= Completed), OR type `Verified` with status `False` (= Failed after retries), OR type `Escalated` with status `True`. This avoids reimplementing the operator's full phase derivation logic.
   - Track and log: proposals created count, alerts skipped (with reason), errors
2. Wire up the K8s dynamic client in `cmd/main.go`:
   - Use `rest.InClusterConfig()` for in-cluster auth
   - Create `dynamic.NewForConfig(config)`
   - Pass the Proposal GVR: `schema.GroupVersionResource{Group: "agentic.openshift.io", Version: "v1alpha1", Resource: "proposals"}`
3. Write unit tests for the poll function:
   - Alert passes all checks → Proposal created
   - Alert too new (initial delay) → skipped
   - Active Proposal exists → skipped
   - Terminal Proposal within cooldown → skipped
   - Terminal Proposal outside cooldown → new Proposal created
   - AlertManager error → poll cycle skipped, no crash
   - K8s API error → poll cycle skipped, no crash
   - 409 Conflict on create → logged and skipped (race condition, not an error)
   - Mixed: some alerts create Proposals, some are skipped

**Files**: `internal/poller/poller.go`, `internal/poller/poller_test.go`, `cmd/main.go` (updated)

---

### Phase 5: Health Probes, Containerfile & Manifests

**Goal**: Production-ready packaging — health endpoints, container image, and Kubernetes manifests.

**Tasks**:
1. Add health probe HTTP server to `cmd/main.go`:
   - Start `net/http` server on `:8080` (or configurable port)
   - `/healthz` — always returns 200 (liveness)
   - `/readyz` — returns 200 if last poll cycle succeeded, 503 otherwise. Use an `atomic.Bool` or similar to track readiness state, set by the poller after each cycle.
2. Create `Containerfile` at repo root:
   - Builder stage: `registry.access.redhat.com/ubi9/go-toolset:latest`, `CGO_ENABLED=0 go build -o /alerts-adapter ./cmd/`
   - Runtime stage: `registry.access.redhat.com/ubi9/ubi-micro:latest`, copy binary, `ENTRYPOINT ["/alerts-adapter"]`
3. Create `manifests/` directory with deployable YAML:
   - `manifests/serviceaccount.yaml` — ServiceAccount `lightspeed-agentic-alerts-adapter` in `openshift-lightspeed`
   - `manifests/clusterrole.yaml` — ClusterRole with Proposal `create`, `list`, `get` on `agentic.openshift.io`
   - `manifests/clusterrolebinding.yaml` — binds ClusterRole to ServiceAccount
   - `manifests/clusterrolebinding-alertmanager.yaml` — binds `monitoring-alertmanager-view` to ServiceAccount
   - `manifests/deployment.yaml` — single-replica Deployment with health probes, resource requests/limits, ServiceAccount reference
4. Update `Makefile` with targets: `image-build` (podman build), `deploy` (kubectl apply -f manifests/)
5. Verify:
   - `make build` succeeds
   - `make test` passes all tests
   - `podman build -f Containerfile .` produces a valid image
   - `kubectl apply --dry-run=client -f manifests/` validates all YAML

**Files**: `cmd/main.go` (updated), `Containerfile`, `manifests/*.yaml`, `Makefile` (updated)

---

## Dependency Graph

```
Phase 1 (Scaffolding)
   │
   ├──► Phase 2 (AlertManager Client)
   │         │
   │         ▼
   │    Phase 3 (Proposal Builder)
   │         │
   │         ▼
   │    Phase 4 (Poll Loop)
   │
   └──► Phase 5 (Containerfile & Manifests)
        (can start in parallel with Phase 2-4 for manifests/Containerfile,
         but health probes depend on Phase 4)
```

Phases 2 and 3 are sequential (builder depends on alert types). Phase 5's manifests and Containerfile can be drafted in parallel with Phases 2-4, but the health probe wiring needs the poller from Phase 4.

## Decisions Made

1. **Typed Proposal objects** — use `github.com/openshift/lightspeed-agentic-operator/api/v1alpha1` for typed `Proposal` structs. K8s client via `k8s.io/client-go` (no `controller-runtime`).

## Open Questions

1. **Terminal phase detection heuristic** — the adapter needs to determine if a Proposal is "terminal" (Completed, Failed, Denied, Escalated) from its status conditions. The full phase derivation logic lives in the operator. The adapter can use a simplified heuristic (checking specific condition types and statuses). Is this acceptable, or should the adapter import the operator's `DerivePhase()` function?

2. **Go linter** — the spec doesn't specify a linter. Recommendation: `golangci-lint` with a reasonable default config. Confirm or specify preferences.

## Verification Checkpoints

| After Phase | Verify |
|-------------|--------|
| 1 | `make build` produces binary, `go vet ./...` passes |
| 2 | `make test` passes AlertManager client tests |
| 3 | `make test` passes naming + builder tests |
| 4 | `make test` passes poll loop tests (all dedup scenarios) |
| 5 | `podman build` succeeds, `kubectl apply --dry-run=client -f manifests/` validates, `make test` passes all |

## Success Criteria (from spec)

- [ ] Polls AlertManager for firing alerts at 30s interval
- [ ] Creates Proposals for alerts passing dedup checks
- [ ] Deterministic naming prevents duplicates under concurrent execution
- [ ] Initial delay (5 min) filters transient alerts
- [ ] Cooldown window (1 hour) prevents flooding for flapping alerts
- [ ] Handles restarts gracefully — no missed alerts, no duplicates
- [ ] Structured JSON logging via slog
- [ ] Health/readiness probes functional
- [ ] Containerfile builds a working image
- [ ] Manifests are valid and deployable
