# Implementation Tasks

Based on [implementation-plan.md](./implementation-plan.md).

## Task 1: Project scaffolding & Go module

**Status**: completed
**Blocked by**: —

- Initialize `go.mod` (`module github.com/openshift/lightspeed-agentic-alerts-adapter`)
- Add dependencies: `github.com/openshift/lightspeed-agentic-operator/api`, `k8s.io/client-go`
- Create directory structure: `cmd/`, `internal/alertmanager/`, `internal/proposal/`, `internal/poller/`
- `Makefile` with targets: `build`, `test`, `lint`, `clean`
- `cmd/main.go` with signal handling (SIGTERM/SIGINT), context cancellation, slog JSON logger, placeholder poll loop

**Acceptance**: `make build` produces `bin/alerts-adapter`, `go vet ./...` passes
**Files**: `go.mod`, `cmd/main.go`, `Makefile`

---

## Task 2: AlertManager HTTP client

**Status**: completed
**Blocked by**: Task 1

- `internal/alertmanager/types.go` — Alert response types matching AM v2 API (labels, annotations, status, fingerprint, startsAt, endsAt, generatorURL)
- `internal/alertmanager/client.go` — Client struct, `NewClient(baseURL)`, `GetFiringAlerts(ctx)`. TLS via in-cluster CA bundle, Bearer token auth
- `internal/alertmanager/client_test.go` — unit tests with httptest server: happy path, empty response, error scenarios (500, malformed JSON, timeout)

**Acceptance**: `make test` passes AlertManager client tests
**Files**: `internal/alertmanager/types.go`, `internal/alertmanager/client.go`, `internal/alertmanager/client_test.go`

---

## Task 3: Proposal builder & deterministic naming

**Status**: completed
**Blocked by**: Task 2

- `internal/proposal/naming.go` — `ProposalName(alertname, namespace, fingerprint)`, DNS subdomain sanitization (RFC 1123), handle empty namespace, long names, special chars
- `internal/proposal/naming_test.go` — unit tests for naming edge cases
- `internal/proposal/builder.go` — `BuildProposal(alert) (*v1alpha1.Proposal, error)` using typed Proposal from operator API. Sets deterministic name, namespace (fallback `openshift-lightspeed`), `spec.request` (Go text/template), `spec.targetNamespaces`, `spec.analysis/execution/verification` (agent: default), `spec.analysisOutput` (mode: Default), labels, annotations
- `internal/proposal/builder_test.go` — unit tests: complete alert, cluster-scoped alert, missing annotations, special characters

**Acceptance**: `make test` passes naming + builder tests
**Files**: `internal/proposal/naming.go`, `internal/proposal/naming_test.go`, `internal/proposal/builder.go`, `internal/proposal/builder_test.go`

---

## Task 4: Poll loop & deduplication logic

**Status**: completed
**Blocked by**: Task 3

- `internal/poller/poller.go` — Poller struct, `Run(ctx)` with 30s ticker, `poll(ctx)` single cycle: fetch alerts, list existing Proposals (label selector `agentic.openshift.io/source=alertmanager`), dedup checks (initial delay 5min, active Proposal exists, cooldown 1hr), create Proposal
- Terminal detection from status conditions: `Denied=True`, `Verified=True/False`, `Escalated=True`
- Wire up typed K8s client in `cmd/main.go` — `rest.InClusterConfig()`, typed client for Proposals (`k8s.io/client-go`, no `controller-runtime`)
- `internal/poller/poller_test.go` — unit tests: passes all checks, too new, active exists, cooldown, outside cooldown, AM/K8s errors, 409 Conflict

**Acceptance**: `make test` passes all poll loop tests
**Files**: `internal/poller/poller.go`, `internal/poller/poller_test.go`, `cmd/main.go`

---

## Task 5: Health probes, Containerfile & manifests

**Status**: completed
**Blocked by**: Task 4

- Health probe HTTP server in `cmd/main.go`: `/healthz` (always 200), `/readyz` (200 if last poll succeeded, 503 otherwise)
- `Containerfile`: builder stage (`ubi9/go-toolset`, `CGO_ENABLED=0`), runtime stage (`ubi9/ubi-micro`)
- `manifests/serviceaccount.yaml`, `manifests/clusterrole.yaml`, `manifests/clusterrolebinding.yaml`, `manifests/clusterrolebinding-alertmanager.yaml`, `manifests/deployment.yaml`
- Update `Makefile`: `image-build`, `deploy` targets

**Acceptance**:
- `make build` succeeds
- `make test` passes all tests
- `podman build -f Containerfile .` produces valid image
- `kubectl apply --dry-run=client -f manifests/` validates all YAML

**Files**: `cmd/main.go`, `Containerfile`, `manifests/*.yaml`, `Makefile`
