package health_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/health"
)

func TestHealthz(t *testing.T) {
	h := health.NewHandler()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	h.Healthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestReadyz_InitiallyNotReady(t *testing.T) {
	h := health.NewHandler()
	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 initially, got %d", rec.Code)
	}
}

func TestReadyz_AfterSetReady(t *testing.T) {
	h := health.NewHandler()
	h.SetReady(true)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Readyz(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 after SetReady(true), got %d", rec.Code)
	}
}

func TestReadyz_SetNotReady(t *testing.T) {
	h := health.NewHandler()
	h.SetReady(true)
	h.SetReady(false)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()

	h.Readyz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503 after SetReady(false), got %d", rec.Code)
	}
}
