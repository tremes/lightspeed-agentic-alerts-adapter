# Task 002: AlertManager Types and Client

## Goal

Implement the AlertManager HTTP client that fetches currently firing alerts from the `GET /api/v2/alerts` endpoint. This is the adapter's primary data source — the poll loop (Task 005) calls this client every cycle to discover what's firing.

## Dependencies

- Task 001: Project Scaffolding (provides Go module, directory structure)

## Acceptance Criteria

- [ ] `internal/alertmanager/types.go` defines Go structs for the AlertManager v2 alert response (labels, annotations, status, fingerprint, startsAt, endsAt)
- [ ] `internal/alertmanager/client.go` implements a client with a `FetchFiringAlerts(ctx context.Context) ([]Alert, error)` method
- [ ] The client sends `GET /api/v2/alerts?filter=status=firing` (or equivalent query to get only firing alerts)
- [ ] The client authenticates using a Bearer token (ServiceAccount token read from `/var/run/secrets/kubernetes.io/serviceaccount/token`)
- [ ] The client verifies TLS using the cluster CA bundle (`/var/run/secrets/kubernetes.io/serviceaccount/ca.crt`)
- [ ] The AlertManager base URL is configurable via a constructor parameter (not hardcoded in the client)
- [ ] The client handles HTTP errors gracefully: non-2xx responses return a descriptive error
- [ ] Unit tests cover: successful fetch, HTTP error responses, malformed JSON, empty alert list

## Test Plan

### Unit Tests
- Use `httptest.NewServer` to simulate the AlertManager API.
- Test cases:
  - 200 response with multiple alerts → returns parsed alerts
  - 200 response with empty array → returns empty slice, no error
  - 500 response → returns error
  - Malformed JSON body → returns error
  - Verify the request includes `Authorization: Bearer <token>` header

### How to Validate
```bash
go test ./internal/alertmanager/... -v
```

## Notes

- The AlertManager v2 API returns alerts as a JSON array. Each alert has: `labels` (map), `annotations` (map), `status` (object with `state` field), `fingerprint` (string), `startsAt` (RFC3339 string), `endsAt` (RFC3339 string).
- The client should accept an `*http.Client` or token/CA path in its constructor for testability — tests can inject an `httptest` server and skip real TLS/auth.
- The default AlertManager URL is `https://alertmanager-main.openshift-monitoring.svc:9093` but the client shouldn't hardcode it.
- See the "AlertManager Authentication" section of the design doc.
