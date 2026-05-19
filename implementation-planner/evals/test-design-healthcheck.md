# go-healthcheck - HTTP Health Check Library

## Overview

A small Go library that provides a standard `/healthz` and `/readyz` HTTP handler pair for Kubernetes workloads. It supports registering named checks that run on each request and returns structured JSON responses.

## API

```go
func NewHandler() *HealthHandler
func (h *HealthHandler) AddLivenessCheck(name string, check CheckFunc)
func (h *HealthHandler) AddReadinessCheck(name string, check CheckFunc)
func (h *HealthHandler) LivenessHandler() http.HandlerFunc
func (h *HealthHandler) ReadinessHandler() http.HandlerFunc

type CheckFunc func(ctx context.Context) error
```

## Response Format

```json
{
  "status": "ok" | "fail",
  "checks": {
    "database": {"status": "ok", "duration_ms": 12},
    "cache": {"status": "fail", "error": "connection refused", "duration_ms": 5003}
  }
}
```

HTTP 200 when all checks pass, HTTP 503 when any check fails.

## Requirements

- Individual check timeout: 5 seconds (configurable per check)
- All checks run concurrently
- Thread-safe: checks can be added after the handler is serving
- No external dependencies beyond the Go standard library
