# Task 007: Deployment Manifests

## Goal

Create the Kubernetes resource manifests (Deployment, ServiceAccount, RBAC) so the adapter can be deployed to an OpenShift cluster. These are the final artifacts needed to run the adapter in its target environment.

## Dependencies

- Task 006: Health Probes and Entrypoint (the binary that the Deployment runs)

## Acceptance Criteria

- [ ] `deploy/` directory contains all manifests
- [ ] `ServiceAccount` named `lightspeed-agentic-alerts-adapter` in namespace `openshift-lightspeed`
- [ ] `Deployment` with single replica, referencing the ServiceAccount, with liveness and readiness probes on port 8081 at `/healthz` and `/readyz`
- [ ] `ClusterRole` for Proposal management: `create`, `list`, `get` on `proposals.agentic.openshift.io`
- [ ] `ClusterRoleBinding` binding the Proposal ClusterRole to the adapter ServiceAccount
- [ ] `ClusterRoleBinding` binding the existing `monitoring-alertmanager-view` ClusterRole to the adapter ServiceAccount
- [ ] All manifests use consistent labels (`app: lightspeed-agentic-alerts-adapter`)
- [ ] Manifests pass `kubectl apply --dry-run=client` validation

## Test Plan

### Unit Tests
- N/A — these are declarative YAML manifests.

### How to Validate
```bash
kubectl apply --dry-run=client -f deploy/
```
All manifests should validate without errors.

## Notes

- The image reference in the Deployment should use a placeholder (`quay.io/openshift-lightspeed/lightspeed-agentic-alerts-adapter:latest`) — actual image tags are set during CI/CD.
- The Deployment and RBAC specs in the design doc have the complete structure — replicate them.
- Consider adding resource requests/limits to the container spec (not in the design doc, but good practice). Reasonable defaults: 50m CPU / 64Mi memory request, 200m CPU / 128Mi memory limit.
- See the "Deployment" and "RBAC" sections of the design doc.
