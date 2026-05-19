package alertmanager

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-openapi/strfmt"
	"github.com/prometheus/alertmanager/api/v2/models"
)

func strPtr(s string) *string { return &s }

func makeTestAlerts(count int) models.GettableAlerts {
	alerts := make(models.GettableAlerts, count)
	now := time.Now().UTC()
	for i := range count {
		fingerprint := "abc123"
		state := "active"
		startsAt := strfmt.DateTime(now.Add(-time.Duration(i+1) * time.Minute))
		endsAt := strfmt.DateTime(now.Add(time.Hour))
		alerts[i] = &models.GettableAlert{
			Alert: models.Alert{
				Labels: models.LabelSet{
					"alertname": "TestAlert",
					"severity":  "warning",
					"namespace": "test-ns",
				},
			},
			Annotations: models.LabelSet{
				"summary":     "Test alert summary",
				"description": "Test alert description",
			},
			Fingerprint: &fingerprint,
			Status: &models.AlertStatus{
				State:       &state,
				InhibitedBy: []string{},
				SilencedBy:  []string{},
				MutedBy:     []string{},
			},
			StartsAt:  &startsAt,
			EndsAt:    &endsAt,
			UpdatedAt: &startsAt,
			Receivers: []*models.Receiver{{Name: strPtr("default")}},
		}
	}
	return alerts
}

func TestFetchFiringAlerts_MultipleAlerts(t *testing.T) {
	expected := makeTestAlerts(3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(expected)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithHTTPClient(server.Client()))
	alerts, err := c.FetchFiringAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 3 {
		t.Fatalf("expected 3 alerts, got %d", len(alerts))
	}
}

func TestFetchFiringAlerts_EmptyList(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.GettableAlerts{})
	}))
	defer server.Close()

	c := NewClient(server.URL, WithHTTPClient(server.Client()))
	alerts, err := c.FetchFiringAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(alerts) != 0 {
		t.Fatalf("expected 0 alerts, got %d", len(alerts))
	}
}

func TestFetchFiringAlerts_ErrorResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewClient(server.URL, WithHTTPClient(server.Client()))
	_, err := c.FetchFiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestFetchFiringAlerts_MalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{not json`))
	}))
	defer server.Close()

	c := NewClient(server.URL, WithHTTPClient(server.Client()))
	_, err := c.FetchFiringAlerts(context.Background())
	if err == nil {
		t.Fatal("expected error for malformed response, got nil")
	}
}

func TestFetchFiringAlerts_AuthIncluded(t *testing.T) {
	var receivedAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.GettableAlerts{})
	}))
	defer server.Close()

	token := "test-service-account-token"
	c := NewClient(server.URL, WithHTTPClient(server.Client()), WithToken(token))
	_, err := c.FetchFiringAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedAuth := "Bearer " + token
	if receivedAuth != expectedAuth {
		t.Errorf("expected Authorization=%q, got %q", expectedAuth, receivedAuth)
	}
}

func TestFetchFiringAlerts_RequestsOnlyFiringAlerts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if got := q.Get("active"); got != "true" {
			t.Errorf("expected active=true, got %q", got)
		}
		if got := q.Get("silenced"); got != "false" {
			t.Errorf("expected silenced=false, got %q", got)
		}
		if got := q.Get("inhibited"); got != "false" {
			t.Errorf("expected inhibited=false, got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(models.GettableAlerts{})
	}))
	defer server.Close()

	c := NewClient(server.URL, WithHTTPClient(server.Client()))
	_, err := c.FetchFiringAlerts(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
