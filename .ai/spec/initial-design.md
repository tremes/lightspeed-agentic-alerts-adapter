# lightspeed-agentic-alerts-adapter

## Overview

The **lightspeed-agentic-alerts-adapter** is a standalone component that bridges OpenShift cluster alerts into the Lightspeed Agentic system. It polls the in-cluster AlertManager API for firing alerts and creates `Proposal` CRs (`agentic.openshift.io/v1alpha1`) to trigger automated analysis, remediation, and verification workflows.

The adapter is a stateless, single-purpose binary written in Go 1.26. It runs as a Deployment in the same namespace as the Lightspeed Agentic operator (`openshift-lightspeed`).

## Requirements

### Functional

1. **Poll AlertManager** for currently firing alerts at a configurable interval.
2. **Create Proposals** for firing alerts that pass deduplication checks.
3. **Deduplicate** to avoid creating multiple Proposals for the same alert or for intermittent/flapping alerts.
4. **Map alert data** into Proposal spec fields using a structured request template.
5. **Handle restarts gracefully** — no missed alerts, no duplicates after restart.

### Non-functional

1. **Stateless** — no persistent storage, no in-memory state required for correctness.
2. **Idempotent** — safe to restart at any time; safe under concurrent execution.
3. **Create-only** — the adapter creates Proposals but never modifies or deletes them. The operator owns the Proposal lifecycle.
4. **Observable** — structured logging and health/readiness probes. Prometheus metrics deferred to a future iteration.

## Architecture

### Component Placement

```
                          openshift-monitoring
                         ┌──────────────────────────────┐
  Prometheus/Thanos      │                              │
  evaluates rules ──────►│  AlertManager                │
                         │  (grouping, silencing,       │
                         │   inhibition, dedup)         │
                         └──────────┬───────────────────┘
                                    │
                      GET /api/v2/alerts (poll every 30s)
                                    │
                          openshift-lightspeed
                         ┌──────────┼───────────────────┐
                         │          ▼                   │
                         │  alerts-adapter              │
                         │  (diff firing vs existing)   │
                         │          │                   │
                         │     CREATE Proposal CR       │
                         │          │                   │
                         │          ▼                   │
                         │  Lightspeed Agentic Operator │
                         │  (reconcile → agents)        │
                         └──────────────────────────────┘
```

### Why AlertManager (not Thanos Ruler)

AlertManager is the alert **notification router** in the OpenShift monitoring stack. It sits downstream of Prometheus/Thanos Ruler. It provides a stable API (`GET /api/v2/alerts`) that returns all alerts with their metadata (labels, annotations, status, timestamps). AlertManager also offers features like grouping, silencing, and inhibition — the adapter does not leverage these in the initial implementation, but they provide a path for future refinement (see [Future Work](#future-work)).

Thanos Ruler evaluates alerting rules and forwards firing alerts *to* AlertManager. Integrating at the Thanos Ruler level would require reimplementing AlertManager's notification logic.

### Why Polling (not Webhooks)

The adapter polls AlertManager's `GET /api/v2/alerts` endpoint rather than receiving webhooks. This choice is driven by **resilience to downtime**:

- **Webhook risk**: If the adapter is down when AlertManager delivers a webhook, the notification is lost. AlertManager retries with backoff but has a finite retry window. Recovery depends on `repeat_interval` (typically 1–4 hours).
- **Polling resilience**: When the adapter restarts, the next poll immediately sees all currently firing alerts. Zero missed alerts, zero catch-up delay.

The polling approach also requires no AlertManager configuration changes (no `AlertmanagerConfig` CR, no receiver setup).

### Stateless Design

The adapter maintains **no internal state**. On every poll cycle, it computes a fresh diff between two authoritative sources:

1. **AlertManager API** — what's currently firing.
2. **Kubernetes API** — what Proposals already exist (filtered by labels).

This means restarts, upgrades, and pod rescheduling are inherently safe. The adapter rebuilds its understanding of the world on every poll cycle.

## Design

### Alert-to-Proposal Cardinality

The adapter maintains a **1:1 relationship** between alerts and Proposals. Each unique firing alert (identified by its AlertManager fingerprint) maps to exactly one Proposal. There is no alert grouping — if 10 alerts are firing, 10 Proposals are created (subject to deduplication checks).

### Poll Loop

The adapter runs a single loop:

```
every 30 seconds:
    1. GET /api/v2/alerts → firing alerts
    2. LIST Proposals (label: source=alertmanager) → existing proposals
    3. For each firing alert:
        a. now - alert.startsAt < INITIAL_DELAY?        → skip (too transient)
        b. Active Proposal with same fingerprint?        → skip (already handling)
        c. Terminal Proposal within COOLDOWN_WINDOW?     → skip (too soon to retry)
        d. Else → CREATE Proposal
```

**Poll interval**: 30 seconds (constant). The initial delay dominates response latency, so the poll interval doesn't need to be aggressive.

### Deduplication

Two configurable parameters control deduplication. Both are defined as Go constants initially, with a path to make them configurable via CR or ConfigMap.

| Parameter | Description | Default |
|-----------|-------------|---------|
| `InitialDelay` | Minimum time an alert must be firing before the adapter creates a Proposal. Filters transient alerts that resolve on their own. Uses the alert's `startsAt` field from AlertManager — no in-memory tracking needed. | 5 minutes |
| `CooldownWindow` | After a Proposal reaches a terminal phase (Completed, Failed, Escalated, Denied), minimum time before the adapter creates a new Proposal for the same alert fingerprint. Prevents flooding for flapping alerts. Uses the terminal Proposal's condition timestamps. | 1 hour |

### Race Condition Prevention

The adapter uses **deterministic Proposal naming** derived from alert metadata. The fingerprint is not computed by the adapter — AlertManager computes it as a hash of the alert's label set and returns it in the `GET /api/v2/alerts` response.

```
{alertname}-{namespace}-{fingerprint[:8]}
```

Examples:
- `kubepodcrashlooping-production-a1b2c3d4`
- `etcdhighfsyncdurations--f9e8d7c6` (no namespace for cluster-scoped alerts)

Components are sanitized to conform to DNS subdomain rules (RFC 1123): lowercased, non-alphanumeric characters replaced, truncated to fit the 253-character limit.

If two poll cycles (or replicas in a future multi-replica setup) attempt to create the same Proposal concurrently, one succeeds and the other receives `409 Conflict (AlreadyExists)`. The adapter treats 409 as success.

### Alert to Proposal Mapping

#### Proposal Name

Deterministic: `{alertname}-{namespace}-{fingerprint[:8]}` (see above).

#### Namespace

Proposals are created in the alert's source namespace — i.e., the namespace from the alert's `namespace` label. For cluster-scoped alerts with no namespace label, Proposals are created in the operator namespace (`openshift-lightspeed`) as a fallback. The operator controller watches Proposals across all namespaces, so this works without additional configuration. The adapter's ServiceAccount needs Proposal create/list/get RBAC across namespaces (ClusterRole instead of a namespace-scoped Role).

#### spec.targetNamespaces

Set to the alert's `namespace` label (matching the namespace where the Proposal is created). If the alert has no namespace label (cluster-scoped alerts), `targetNamespaces` is left empty. The operator grants cluster-scoped RBAC based on the analysis agent's output.

#### spec.request

Built from a Go `text/template` that combines English instructions with structured alert data:

```go
const requestTemplate = `
A Kubernetes alert is firing in the cluster.
Investigate the root cause and propose a remediation.

Alert: {{ .AlertName }}
Severity: {{ .Severity }}
Namespace: {{ .Namespace }}
Summary: {{ .Summary }}
Description: {{ .Description }}

Labels:
{{ range $k, $v := .Labels }}  {{ $k }}: {{ $v }}
{{ end }}
`
```

The template input is populated from the AlertManager alert payload:
- `AlertName`: `alert.labels["alertname"]`
- `Severity`: `alert.labels["severity"]`
- `Namespace`: `alert.labels["namespace"]` (may be empty)
- `Summary`: `alert.annotations["summary"]`
- `Description`: `alert.annotations["description"]`
- `Labels`: all `alert.labels`

#### spec.analysis / execution / verification

All three steps configured with the `default` agent:

```yaml
spec:
  analysis:
    agent: default
  execution:
    agent: default
  verification:
    agent: default
```

The `default` Agent CR is expected to exist in the cluster, configured by the operator installation.

#### spec.analysisOutput

Default mode, no adapter-specific schema:

```yaml
spec:
  analysisOutput:
    mode: Default
```

#### Labels and Annotations

```yaml
metadata:
  labels:
    agentic.openshift.io/source: alertmanager
    agentic.openshift.io/alert-fingerprint: <fingerprint[:8]>
    agentic.openshift.io/alert-name: <alertname, lowercased>
    agentic.openshift.io/alert-severity: <severity>
  annotations:
    agentic.openshift.io/alert-starts-at: "<RFC3339 timestamp>"
    agentic.openshift.io/alert-summary: "<summary annotation, truncated>"
```

Labels are used for filtering and dedup queries. Annotations carry non-selectable metadata for UI and debugging.

### Alert Resolution Behavior

When an alert resolves while its Proposal is still active (Analyzing, Executing, Verifying), the adapter does nothing. The Proposal continues to completion. Rationale:

- A self-resolved alert doesn't mean the underlying issue is fixed.
- The analysis and remediation may still be valuable.
- Keeping the adapter create-only simplifies the design and avoids lifecycle coupling with the operator.

### AlertManager Authentication

The adapter authenticates to the AlertManager API using the pod's auto-mounted ServiceAccount token:

- **Endpoint**: `https://alertmanager-main.openshift-monitoring.svc:9093/api/v2/alerts`
- **Authentication**: `Authorization: Bearer <ServiceAccount token>`
- **TLS**: Verified against the cluster CA bundle (`/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`)

The AlertManager URL is defined as a constant, with a path to make it configurable.

### Error Handling

- **AlertManager unreachable**: Log the error, skip the poll cycle, retry on the next interval.
- **Kubernetes API unreachable**: Log the error, skip Proposal creation, retry on the next interval.
- **Proposal creation fails (non-409)**: Log the error with alert details. The alert will be retried on the next poll cycle since no Proposal exists for it.
- **Invalid alert data** (missing alertname, template rendering failure): Log and skip the individual alert. Do not block processing of other alerts.

## Deployment

### Kubernetes Resources

**Deployment** — single replica in `openshift-lightspeed`:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: lightspeed-agentic-alerts-adapter
  namespace: openshift-lightspeed
spec:
  replicas: 1
  selector:
    matchLabels:
      app: lightspeed-agentic-alerts-adapter
  template:
    metadata:
      labels:
        app: lightspeed-agentic-alerts-adapter
    spec:
      serviceAccountName: lightspeed-agentic-alerts-adapter
      containers:
        - name: adapter
          image: quay.io/openshift-lightspeed/lightspeed-agentic-alerts-adapter:latest
          livenessProbe:
            httpGet:
              path: /healthz
              port: 8081
          readinessProbe:
            httpGet:
              path: /readyz
              port: 8081
```

Single replica is sufficient because:
- Stateless design handles restarts gracefully.
- Polling catches up immediately after downtime.
- Deterministic naming prevents duplicates even under concurrent execution.

### RBAC

The adapter's ServiceAccount needs two sets of permissions:

**1. AlertManager access** — read alerts from `openshift-monitoring`:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: lightspeed-agentic-alerts-adapter-alertmanager
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: monitoring-alertmanager-view
subjects:
  - kind: ServiceAccount
    name: lightspeed-agentic-alerts-adapter
    namespace: openshift-lightspeed
```

**2. Proposal management** — create and list Proposals across namespaces:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: lightspeed-agentic-alerts-adapter-proposals
rules:
  - apiGroups: ["agentic.openshift.io"]
    resources: ["proposals"]
    verbs: ["create", "list", "get"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: lightspeed-agentic-alerts-adapter-proposals
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: lightspeed-agentic-alerts-adapter-proposals
subjects:
  - kind: ServiceAccount
    name: lightspeed-agentic-alerts-adapter
    namespace: openshift-lightspeed
```

### Health Probes

| Probe | Path | Behavior |
|-------|------|----------|
| Liveness | `/healthz` | Always returns 200 if the process is running. |
| Readiness | `/readyz` | Returns 200 if the last poll cycle completed without critical errors (AlertManager reachable, K8s API reachable). |

## Project Structure

```
lightspeed-agentic-alerts-adapter/
├── cmd/
│   └── main.go                      # Entrypoint: flags, signal handling, run loop
├── internal/
│   ├── alertmanager/
│   │   ├── client.go                # AlertManager HTTP client (GET /api/v2/alerts)
│   │   └── types.go                 # Alert response types
│   ├── proposal/
│   │   ├── builder.go               # Alert → Proposal mapping, template rendering
│   │   └── naming.go                # Deterministic name generation, sanitization
│   └── poller/
│       └── poller.go                # Poll loop: fetch alerts, diff, create proposals
├── Dockerfile
├── Makefile
├── go.mod
├── go.sum
└── OWNERS
```

### Dependencies

| Dependency | Purpose |
|-----------|---------|
| `github.com/openshift/lightspeed-agentic-operator/api` | Typed Proposal CRD Go types |
| `sigs.k8s.io/controller-runtime/pkg/client` | Typed Kubernetes client for Proposal CRUD |
| `k8s.io/client-go` | In-cluster config, ServiceAccount auth |

## Configuration

All configurable values are Go constants in the initial implementation. Future iterations will move them to a CR or ConfigMap.

| Constant | Value | Description |
|----------|-------|-------------|
| `PollInterval` | `30 * time.Second` | How often to poll AlertManager |
| `InitialDelay` | `5 * time.Minute` | Alert must fire this long before creating a Proposal |
| `CooldownWindow` | `1 * time.Hour` | Minimum time after a terminal Proposal before re-proposing for the same alert |
| `AlertManagerURL` | `https://alertmanager-main.openshift-monitoring.svc:9093` | AlertManager API base URL |
| `DefaultNamespace` | `openshift-lightspeed` | Namespace for Proposals from cluster-scoped alerts (no namespace label) |
| `DefaultAgent` | `default` | Agent name for analysis, execution, and verification steps |

## Future Work

- **AlertManager-aware filtering**: Leverage AlertManager's silencing, inhibition, and grouping features to reduce noise. The adapter currently reads all firing alerts regardless of their AM status. Future iterations could filter by alert state (`active` vs `suppressed`), respect silence rules, and use grouping metadata to create a single Proposal per alert group instead of per individual alert.
- **Custom fingerprinting and alert grouping**: Consider replacing AlertManager's fingerprint (hash of all labels) with a custom fingerprint computed from a chosen subset of labels. This could potentially enable custom alert grouping to handle alert storms — e.g., multiple related alerts might map to a single Proposal instead of creating one per alert. A custom fingerprint would also decouple the adapter from AlertManager's fingerprint format, making it easier to switch to other alert sources (e.g., Thanos Ruler) in the future. To be evaluated based on real-world usage patterns.
- **Configurable parameters**: Move constants to a CRD or ConfigMap (poll interval, initial delay, cooldown, AlertManager URL, agent names).
- **Alert filtering**: Opt-in via alert labels, severity-based filtering, or configurable label selectors.
- **Prometheus metrics**: `alerts_adapter_polls_total`, `proposals_created_total`, `errors_total`, `poll_duration_seconds`.
- **Adapter-specific analysis output schema**: Inject custom fields into `analysisOutput.schema` for alert correlation, affected services topology.
- **Workflow selection**: Choose different workflow patterns (advisory, assisted, full remediation) based on alert labels.
- **Multi-replica support**: Leader election or sharded alert processing for high availability.
