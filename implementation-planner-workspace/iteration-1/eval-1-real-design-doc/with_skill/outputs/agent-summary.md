# Eval 1 With-Skill Output (from agent summary — agent was denied file write perms)

## Task Files (7 files)

Each follows the skill's prescribed format (Goal, Dependencies, Acceptance Criteria, Test Plan, Notes) and follows dependency order from foundations to integration.

### 001-project-scaffolding.md
Go module, directory structure, Makefile, Dockerfile

### 002-alertmanager-client.md
HTTP client for GET /api/v2/alerts with SA token auth and TLS

### 003-proposal-naming.md
Deterministic name generation and RFC 1123 sanitization

### 004-proposal-builder.md
Alert-to-Proposal CR mapping, template rendering, labels/annotations

### 005-poll-loop-and-deduplication.md
Core loop: fetch alerts, diff against existing Proposals, dedup, create

### 006-health-probes-and-entrypoint.md
/healthz, /readyz endpoints, signal handling, wiring components in main.go

### 007-deployment-manifests.md
ServiceAccount, ClusterRole, ClusterRoleBinding, Deployment YAML manifests
