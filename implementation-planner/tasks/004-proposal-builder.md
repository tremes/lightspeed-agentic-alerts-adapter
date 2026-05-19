# Task 004: Proposal Builder

## Goal

Implement the mapping from an AlertManager alert to a fully populated Proposal CR. This is where alert data gets transformed into the structured Proposal spec that the Lightspeed Agentic operator reconciles. The builder uses the naming function from Task 003 and the alert types from Task 002.

## Dependencies

- Task 002: AlertManager Types (provides the Alert struct)
- Task 003: Proposal Naming (provides the name generation function)

## Acceptance Criteria

- [ ] `internal/proposal/builder.go` implements a function that takes an Alert and returns a `*Proposal` CR (using the typed API from the operator)
- [ ] Proposal namespace is set to the alert's `namespace` label; falls back to `openshift-lightspeed` for cluster-scoped alerts
- [ ] `spec.request` is rendered from the Go `text/template` defined in the design doc, populated with alert data
- [ ] `spec.targetNamespaces` is set to the alert's namespace (empty for cluster-scoped alerts)
- [ ] `spec.analysis`, `spec.execution`, `spec.verification` all reference the `default` agent
- [ ] `spec.analysisOutput.mode` is set to `Default`
- [ ] Labels are set: `agentic.openshift.io/source`, `agentic.openshift.io/alert-fingerprint`, `agentic.openshift.io/alert-name`, `agentic.openshift.io/alert-severity`
- [ ] Annotations are set: `agentic.openshift.io/alert-starts-at` (RFC3339), `agentic.openshift.io/alert-summary` (truncated to 256 chars)
- [ ] Label values are truncated to 63 characters per Kubernetes limits
- [ ] Template rendering errors (missing alertname) return an error rather than creating a malformed Proposal
- [ ] Unit tests verify the full Proposal output for representative alert inputs

## Test Plan

### Unit Tests
- Test cases:
  - Standard alert with all fields populated → verify every Proposal field
  - Cluster-scoped alert (no namespace label) → Proposal in `openshift-lightspeed`, empty `targetNamespaces`
  - Alert with very long summary → annotation truncated to 256 chars
  - Alert with very long alertname → label truncated to 63 chars
  - Alert missing `alertname` label → returns error
  - Alert with empty annotations → template renders with empty summary/description fields (not an error)
  - Verify all labels and annotations are set correctly

### How to Validate
```bash
go test ./internal/proposal/... -v -run TestBuilder
```

## Notes

- The Proposal type comes from `github.com/openshift/lightspeed-agentic-operator/api`. You'll need to study the actual type definition to understand the exact field names and structure. The design doc describes the intended mapping; the API types define the actual Go struct.
- The request template is defined as a Go constant. Parse it once (e.g., in an `init()` or constructor) rather than on every call.
- The `default` agent name and `openshift-lightspeed` fallback namespace are Go constants as specified in the design doc's Configuration section.
- See the "Alert to Proposal Mapping" section of the design doc for the full field mapping.
