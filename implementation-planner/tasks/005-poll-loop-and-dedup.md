# Task 005: Poll Loop and Deduplication

## Goal

Implement the core reconciliation loop that ties everything together: fetch firing alerts from AlertManager, list existing Proposals from the Kubernetes API, apply deduplication rules, and create Proposals for new alerts. This is the adapter's main behavior — everything else exists to support this loop.

## Dependencies

- Task 002: AlertManager Types and Client (provides `FetchFiringAlerts`)
- Task 003: Proposal Naming (provides deterministic name generation)
- Task 004: Proposal Builder (provides alert → Proposal mapping)

## Acceptance Criteria

- [ ] `internal/poller/poller.go` implements a `Poller` struct with a `Run(ctx context.Context) error` method that executes the poll loop until the context is cancelled
- [ ] Each poll cycle: fetches firing alerts, lists existing Proposals (filtered by `agentic.openshift.io/source=alertmanager` label), and processes each alert through the dedup checks
- [ ] **Initial delay check**: alerts where `now - alert.startsAt < InitialDelay (5 min)` are skipped
- [ ] **Active Proposal check**: alerts with a matching fingerprint label on a non-terminal Proposal are skipped
- [ ] **Cooldown check**: alerts with a matching fingerprint label on a terminal Proposal whose terminal condition timestamp is within `CooldownWindow (1 hour)` of now are skipped
- [ ] Terminal state is determined from `.status.conditions` — a Proposal is terminal if it has a condition with type Completed, Failed, Escalated, or Denied with status True
- [ ] Proposals that pass all dedup checks are created via the Kubernetes API
- [ ] `409 Conflict (AlreadyExists)` on create is treated as success (logged at debug level, not error)
- [ ] Errors during a poll cycle (AlertManager unreachable, K8s API error) are logged and the cycle is skipped — the next cycle retries
- [ ] Individual alert processing errors (invalid data, template failure) are logged and the alert is skipped — other alerts in the same cycle continue
- [ ] The Poller exposes a method or channel to report poll cycle health (success/failure) for the readiness probe (Task 006)
- [ ] Unit tests cover all dedup scenarios and error handling paths

## Test Plan

### Unit Tests
Use interfaces for the AlertManager client and Kubernetes client so they can be mocked in tests.
- Test cases:
  - No firing alerts → no Proposals created
  - New alert, no existing Proposals → Proposal created
  - Alert within initial delay window → skipped
  - Alert with active (non-terminal) Proposal → skipped
  - Alert with terminal Proposal within cooldown → skipped
  - Alert with terminal Proposal outside cooldown → new Proposal created
  - Multiple alerts in one cycle, mix of skip and create → correct subset created
  - AlertManager returns error → cycle skipped, no Proposals created, poll health reports failure
  - K8s API list returns error → cycle skipped
  - Proposal create returns 409 → treated as success
  - Proposal create returns other error → logged, other alerts still processed
  - Alert missing alertname → skipped with log, other alerts still processed

### Integration Tests
- If feasible, use `envtest` (from controller-runtime) to test against a real API server with the Proposal CRD registered. Create a Proposal, verify it appears in the list, check dedup logic works end-to-end.

### How to Validate
```bash
go test ./internal/poller/... -v
```

## Notes

- The Poller needs both an AlertManager client and a Kubernetes client (controller-runtime `client.Client`). Accept these as constructor parameters for testability.
- For terminal state detection: iterate over `.status.conditions` and check for any condition where `Type` is one of `Completed`, `Failed`, `Escalated`, `Denied` and `Status` is `True`. The condition's `LastTransitionTime` is the timestamp to compare against for cooldown.
- The poll interval (30s) should be a configurable parameter on the Poller, not a global constant, so tests can use a shorter interval.
- The design doc says "stateless" — the Poller doesn't cache state between cycles. Each cycle does a fresh fetch + list. The only in-memory state is the poll health flag for the readiness probe.
- See the "Poll Loop", "Deduplication", and "Error Handling" sections of the design doc.
