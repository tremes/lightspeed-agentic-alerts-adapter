package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

type fakeHealthChecker struct {
	healthy bool
}

func (f *fakeHealthChecker) Healthy() bool {
	return f.healthy
}

func TestHealthzHandler(t *testing.T) {
	handler := healthzHandler()

	tests := []struct {
		name     string
		wantCode int
	}{
		{
			name:     "always returns 200",
			wantCode: http.StatusOK,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

			if rec.Code != tt.wantCode {
				t.Errorf("GET /healthz = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestReadyzHandler(t *testing.T) {
	tests := []struct {
		name     string
		healthy  bool
		wantCode int
	}{
		{
			name:     "503 when no poll has completed",
			healthy:  false,
			wantCode: http.StatusServiceUnavailable,
		},
		{
			name:     "200 after successful poll",
			healthy:  true,
			wantCode: http.StatusOK,
		},
		{
			name:     "503 after failed poll",
			healthy:  false,
			wantCode: http.StatusServiceUnavailable,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checker := &fakeHealthChecker{healthy: tt.healthy}
			handler := readyzHandler(checker)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != tt.wantCode {
				t.Errorf("GET /readyz = %d, want %d", rec.Code, tt.wantCode)
			}
		})
	}
}

func TestReadyzTransitions(t *testing.T) {
	checker := &fakeHealthChecker{healthy: false}
	handler := readyzHandler(checker)

	steps := []struct {
		name     string
		healthy  bool
		wantCode int
	}{
		{"initially unhealthy", false, http.StatusServiceUnavailable},
		{"becomes healthy", true, http.StatusOK},
		{"becomes unhealthy again", false, http.StatusServiceUnavailable},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			checker.healthy = step.healthy
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))

			if rec.Code != step.wantCode {
				t.Errorf("GET /readyz = %d, want %d", rec.Code, step.wantCode)
			}
		})
	}
}
