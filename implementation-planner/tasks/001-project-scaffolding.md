# Task 001: Project Scaffolding

## Goal

Set up the Go project structure, build tooling, and container image definition so that all subsequent tasks have a working foundation to build on. This is the skeleton that everything else plugs into.

## Dependencies

None — this is the first task.

## Acceptance Criteria

- [ ] `go.mod` initialized with module path `github.com/openshift/lightspeed-agentic-alerts-adapter` and Go 1.25
- [ ] Directory structure matches the design doc: `cmd/`, `internal/alertmanager/`, `internal/proposal/`, `internal/poller/`
- [ ] `cmd/main.go` exists with a minimal `main()` that prints a startup message and exits (placeholder for real logic)
- [ ] `Makefile` with targets: `build`, `test`, `lint`, `image-build`
- [ ] `Dockerfile` with a multi-stage build using Red Hat Golang builder image (`registry.access.redhat.com/ubi9/go-toolset`) for the build stage and UBI minimal for the runtime stage
- [ ] `go build ./...` succeeds with no errors
- [ ] `go vet ./...` passes

## Test Plan

### Unit Tests
- No unit tests needed for scaffolding — validation is that the project builds.

### How to Validate
```bash
go build ./...
go vet ./...
make build
```
All commands should exit 0.

## Notes

- Key dependencies to add to `go.mod` (they'll be used in later tasks but should be resolvable now):
  - `github.com/openshift/lightspeed-agentic-operator/api` — Proposal CRD types
  - `k8s.io/client-go` — in-cluster config, ServiceAccount auth, typed Kubernetes client
  - `k8s.io/apimachinery` — Kubernetes API types and utilities
- The Dockerfile runtime stage should use `registry.access.redhat.com/ubi9/ubi-minimal` for OpenShift compatibility.
- See the "Project Structure" and "Dependencies" sections of the design doc.
