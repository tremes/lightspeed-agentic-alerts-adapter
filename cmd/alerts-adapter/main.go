package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/adapter"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/health"
)

const (
	PollInterval    = 30 * time.Second
	InitialDelay    = 5 * time.Minute
	CooldownWindow  = 1 * time.Hour
	AlertManagerURL = "https://alertmanager-main.openshift-monitoring.svc:9093"
	DefaultAgent    = "default"
	HealthAddr      = ":8080"
	ServiceCAPath   = "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"
	SATokenPath     = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := run(ctx); err != nil {
		slog.Error("fatal error", "error", err.Error())
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	scheme := runtime.NewScheme()
	if err := agenticv1alpha1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("adding agentic scheme: %w", err)
	}

	restConfig, err := config.GetConfig()
	if err != nil {
		return fmt.Errorf("getting in-cluster config: %w", err)
	}

	k8sClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	httpClient, err := buildAlertManagerHTTPClient()
	if err != nil {
		return fmt.Errorf("building AlertManager HTTP client: %w", err)
	}

	amClient := alertmanager.NewClient(AlertManagerURL, httpClient)

	a := adapter.New(amClient, k8sClient, adapter.Config{
		InitialDelay:   InitialDelay,
		CooldownWindow: CooldownWindow,
		AgentName:      DefaultAgent,
	})

	healthHandler := health.NewHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthHandler.Healthz)
	mux.HandleFunc("/readyz", healthHandler.Readyz)

	healthServer := &http.Server{
		Addr:    HealthAddr,
		Handler: mux,
	}

	go func() {
		slog.Info("starting health server", "addr", HealthAddr)
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("health server error", "error", err.Error())
		}
	}()

	slog.Info("starting poll loop",
		"interval", PollInterval.String(),
		"initialDelay", InitialDelay.String(),
		"cooldownWindow", CooldownWindow.String(),
		"alertManagerURL", AlertManagerURL)

	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	for {
		if err := a.Reconcile(ctx); err != nil {
			slog.Error("poll cycle failed", "error", err.Error())
			healthHandler.SetReady(false)
		} else {
			healthHandler.SetReady(true)
		}

		select {
		case <-ctx.Done():
			slog.Info("shutting down")
			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer shutdownCancel()
			healthServer.Shutdown(shutdownCtx)
			return nil
		case <-ticker.C:
		}
	}
}

func buildAlertManagerHTTPClient() (*http.Client, error) {
	caCert, err := os.ReadFile(ServiceCAPath)
	if err != nil {
		return nil, fmt.Errorf("reading service CA: %w", err)
	}

	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse service CA certificate")
	}

	token, err := os.ReadFile(SATokenPath)
	if err != nil {
		return nil, fmt.Errorf("reading service account token: %w", err)
	}

	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &bearerTokenTransport{
			token: string(token),
			base: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: pool,
				},
			},
		},
	}, nil
}

type bearerTokenTransport struct {
	token string
	base  http.RoundTripper
}

func (t *bearerTokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("Authorization", "Bearer "+t.token)
	return t.base.RoundTrip(req)
}
