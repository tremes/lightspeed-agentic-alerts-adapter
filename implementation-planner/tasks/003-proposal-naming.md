# Task 003: Proposal Naming

## Goal

Implement deterministic Proposal name generation from alert metadata. This naming scheme is critical for deduplication and race condition prevention — two poll cycles seeing the same alert must produce the same Proposal name, and the name must be a valid Kubernetes resource name.

## Dependencies

- Task 001: Project Scaffolding (provides directory structure)
- Task 002: AlertManager Types (provides the Alert type with labels and fingerprint fields)

## Acceptance Criteria

- [ ] `internal/proposal/naming.go` implements a function that takes alert metadata (alertname, namespace, fingerprint) and returns a deterministic Proposal name
- [ ] Name format: `{alertname}-{namespace}-{fingerprint[:8]}` — lowercased, with non-alphanumeric characters (except hyphens) replaced
- [ ] Cluster-scoped alerts (empty namespace) produce names like `{alertname}--{fingerprint[:8]}` (double hyphen where namespace would be)
- [ ] Names conform to DNS subdomain rules (RFC 1123): lowercase, alphanumeric and hyphens only, no leading/trailing hyphens, max 253 characters
- [ ] Truncation handles the 253-character limit by truncating the alertname component (preserving namespace and fingerprint for uniqueness)
- [ ] Unit tests cover: normal case, no namespace, special characters in alertname, very long alertname, fingerprint shorter than 8 chars (defensive)

## Test Plan

### Unit Tests
- Test cases:
  - `KubePodCrashLooping` + `production` + `a1b2c3d4e5f6` → `kubepodcrashlooping-production-a1b2c3d4`
  - `etcdHighFsyncDurations` + `` (empty namespace) + `f9e8d7c6abcd` → `etcdhighfsyncdurations--f9e8d7c6`
  - Alert name with dots/underscores: `kube_pod.restart` → sanitized to `kube-pod-restart`
  - Alert name > 240 chars → truncated, total name ≤ 253 chars, fingerprint suffix preserved
  - Leading/trailing special chars stripped
  - Result matches DNS subdomain regex: `[a-z0-9]([a-z0-9-]*[a-z0-9])?`

### How to Validate
```bash
go test ./internal/proposal/... -v -run TestNaming
```

## Notes

- The fingerprint comes from AlertManager — it's a hex string hash of the alert's label set. It's always present and at least 8 chars in practice, but handle the edge case defensively.
- Sanitization rules: lowercase everything, replace `[^a-z0-9-]` with `-`, collapse consecutive hyphens, trim leading/trailing hyphens.
- See the "Race Condition Prevention" and "Proposal Name" sections of the design doc.
