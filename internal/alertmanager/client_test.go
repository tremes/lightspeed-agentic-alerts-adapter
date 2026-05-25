package alertmanager

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestGetFiringAlerts_Success(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != alertsPath {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.RawQuery != alertsQuery {
			t.Errorf("unexpected query: %s", r.URL.RawQuery)
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[
			{
				"labels": {"alertname": "KubePodCrashLooping", "namespace": "production", "severity": "critical"},
				"annotations": {"summary": "Pod is crash looping", "description": "Pod xyz is restarting"},
				"startsAt": "2025-01-01T00:00:00Z",
				"endsAt": "0001-01-01T00:00:00Z",
				"generatorURL": "http://prometheus:9090/graph",
				"fingerprint": "a1b2c3d4e5f6",
				"status": {"state": "active", "silencedBy": [], "inhibitedBy": []}
			},
			{
				"labels": {"alertname": "EtcdHighFsyncDurations", "severity": "warning"},
				"annotations": {"summary": "Etcd fsync durations are high"},
				"startsAt": "2025-01-01T01:00:00Z",
				"endsAt": "0001-01-01T00:00:00Z",
				"fingerprint": "f9e8d7c6b5a4",
				"status": {"state": "active", "silencedBy": [], "inhibitedBy": []}
			}
		]`))
	}))
	defer server.Close()

	client := NewClientWithHTTP(server.URL, server.Client())
	alerts, err := client.GetFiringAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(alerts) != 2 {
		t.Fatalf("expected 2 alerts, got %d", len(alerts))
	}

	if alerts[0].Labels["alertname"] != "KubePodCrashLooping" {
		t.Errorf("expected alertname KubePodCrashLooping, got %s", alerts[0].Labels["alertname"])
	}
	if alerts[0].Fingerprint != "a1b2c3d4e5f6" {
		t.Errorf("expected fingerprint a1b2c3d4e5f6, got %s", alerts[0].Fingerprint)
	}
	expected := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	if !alerts[0].StartsAt.Equal(expected) {
		t.Errorf("expected startsAt %v, got %v", expected, alerts[0].StartsAt)
	}

	if alerts[1].Labels["alertname"] != "EtcdHighFsyncDurations" {
		t.Errorf("expected alertname EtcdHighFsyncDurations, got %s", alerts[1].Labels["alertname"])
	}
	if alerts[1].Labels["namespace"] != "" {
		t.Errorf("expected no namespace, got %s", alerts[1].Labels["namespace"])
	}
}

func TestGetFiringAlerts_Empty(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`[]`))
	}))
	defer server.Close()

	client := NewClientWithHTTP(server.URL, server.Client())
	alerts, err := client.GetFiringAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestGetFiringAlerts_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	client := NewClientWithHTTP(server.URL, server.Client())
	_, err := client.GetFiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
}

func TestGetFiringAlerts_MalformedJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not json`))
	}))
	defer server.Close()

	client := NewClientWithHTTP(server.URL, server.Client())
	_, err := client.GetFiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
}

func TestGetFiringAlerts_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer server.Close()

	client := NewClientWithHTTP(server.URL, &http.Client{Timeout: 100 * time.Millisecond})
	_, err := client.GetFiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for timeout")
	}
}
