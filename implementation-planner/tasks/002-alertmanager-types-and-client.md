# Task 002: AlertManager Types and Client

## Goal

Provide a client that fetches currently firing alerts from the in-cluster AlertManager. This is the adapter's primary data source — the poll loop (Task 005) calls this client every cycle to discover what's firing.

## Dependencies

- Task 001: Project Scaffolding (provides Go module, directory structure)

## Pre-Implementation Research

Before writing any code, explore existing Go client libraries for AlertManager (e.g., the official `github.com/prometheus/alertmanager` client package or generated OpenAPI clients). Evaluate whether an existing library provides the functionality needed (fetching firing alerts, authentication, TLS). Prefer adopting a well-maintained library over writing a custom HTTP client — only build from scratch if no suitable library exists or if the dependency cost is too high.

## Acceptance Criteria

- [ ] `internal/alertmanager/types.go` defines Go types for alerts (labels, annotations, status, fingerprint, startsAt, endsAt) — these may be thin wrappers or re-exports if using a library that provides its own types
- [ ] `internal/alertmanager/client.go` implements a client with a `FetchFiringAlerts(ctx context.Context) ([]Alert, error)` method
- [ ] The client fetches only firing alerts from the AlertManager API
- [ ] The client authenticates using the pod's ServiceAccount token (`/var/run/secrets/kubernetes.io/serviceaccount/token`)
- [ ] The client verifies TLS using the cluster CA bundle (`/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`)
- [ ] The AlertManager base URL is configurable via a constructor parameter (not hardcoded)
- [ ] Errors from the AlertManager API are surfaced with descriptive messages
- [ ] Unit tests cover: successful fetch, error responses, malformed data, empty alert list

## Test Plan

### Unit Tests
- Test cases:
  - Multiple alerts returned → parsed correctly
  - Empty alert list → returns empty slice, no error
  - AlertManager returns error → returns descriptive error
  - Malformed response data → returns error
  - Authentication credentials are included in requests

### How to Validate
```bash
go test ./internal/alertmanager/... -v
```

## Notes

- The AlertManager v2 API returns alerts with: `labels` (map), `annotations` (map), `status` (object with `state` field), `fingerprint` (string), `startsAt` (RFC3339 string), `endsAt` (RFC3339 string).
- The default AlertManager URL in OpenShift is `https://alertmanager-main.openshift-monitoring.svc:9093` but the client shouldn't hardcode it.
- The client constructor should support dependency injection for testability (e.g., swappable transport or base URL).
- See the "AlertManager Authentication" section of the design doc.
