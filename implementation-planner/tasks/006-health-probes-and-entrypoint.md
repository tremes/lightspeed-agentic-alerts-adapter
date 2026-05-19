# Task 006: Health Probes and Main Entrypoint

## Goal

Wire everything together in `cmd/main.go`: initialize the AlertManager client, Kubernetes client, and Poller, start the poll loop, serve health probes, and handle graceful shutdown on SIGTERM/SIGINT. After this task, the adapter is a runnable binary that does its full job.

## Dependencies

- Task 002: AlertManager Client (needed to construct the Poller)
- Task 004: Proposal Builder (needed to construct the Poller)
- Task 005: Poll Loop (the core logic this entrypoint starts)

## Acceptance Criteria

- [ ] `cmd/main.go` initializes the in-cluster Kubernetes client using `controller-runtime` (rest.InClusterConfig or equivalent)
- [ ] `cmd/main.go` initializes the AlertManager client with the configured URL and ServiceAccount token/CA paths
- [ ] `cmd/main.go` creates and starts the Poller in a goroutine
- [ ] An HTTP server on port 8081 serves `/healthz` (liveness) and `/readyz` (readiness)
- [ ] `/healthz` always returns 200 if the process is running
- [ ] `/readyz` returns 200 if the last poll cycle succeeded; 503 if the last poll cycle failed or no cycle has completed yet
- [ ] Graceful shutdown: SIGTERM/SIGINT cancels the context, stops the poll loop, and shuts down the HTTP server with a timeout
- [ ] Structured logging (e.g., `slog` or `klog`) is configured at startup
- [ ] All configurable constants from the design doc (PollInterval, InitialDelay, CooldownWindow, AlertManagerURL, DefaultNamespace, DefaultAgent) are defined and passed through
- [ ] `go build ./cmd/...` produces a working binary
- [ ] Unit tests for health probe handlers (200/503 behavior based on poll health state)

## Test Plan

### Unit Tests
- Test `/healthz` handler → always 200
- Test `/readyz` handler → 503 when no poll has completed, 200 after a successful poll, 503 after a failed poll

### How to Validate
```bash
go build -o adapter ./cmd/...
go test ./cmd/... -v
```

## Notes

- The binary doesn't need CLI flags in the initial implementation — all configuration is via Go constants. But structure the code so that adding flags or env vars later is straightforward (e.g., a config struct populated from constants).
- Use `context.WithCancel` for the main context and wire signal handling to the cancel function.
- The readiness state is a simple `atomic.Bool` or similar — set by the Poller after each cycle, read by the `/readyz` handler.
- Port 8081 matches the Deployment manifest in the design doc.
- See the "Health Probes" and "Configuration" sections of the design doc.
