package alertmanager_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
)

func TestClient_FiringAlerts(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	alerts := []alertmanager.Alert{
		{
			Fingerprint: "abc123",
			Status:      alertmanager.AlertStatus{State: "active"},
			Labels:      map[string]string{"alertname": "KubePodCrashLooping", "namespace": "production", "severity": "warning"},
			Annotations: map[string]string{"summary": "Pod is crash looping", "description": "Pod web-abc is crash looping"},
			StartsAt:    now.Add(-10 * time.Minute),
		},
		{
			Fingerprint: "def456",
			Status:      alertmanager.AlertStatus{State: "suppressed"},
			Labels:      map[string]string{"alertname": "Watchdog", "severity": "none"},
			StartsAt:    now.Add(-1 * time.Hour),
		},
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/alerts" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("active") != "true" {
			t.Error("expected active=true query param")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(alerts)
	}))
	defer srv.Close()

	client := alertmanager.NewClient(srv.URL, srv.Client())
	got, err := client.FiringAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(got))
	}
	if got[0].Fingerprint != "abc123" {
		t.Errorf("expected fingerprint abc123, got %s", got[0].Fingerprint)
	}
	if got[0].Labels["alertname"] != "KubePodCrashLooping" {
		t.Errorf("expected alertname KubePodCrashLooping, got %s", got[0].Labels["alertname"])
	}
}

func TestClient_FiringAlerts_ServerError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	client := alertmanager.NewClient(srv.URL, srv.Client())
	_, err := client.FiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestClient_FiringAlerts_InvalidJSON(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	client := alertmanager.NewClient(srv.URL, srv.Client())
	_, err := client.FiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
