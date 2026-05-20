package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/poller"
)

// config holds all configurable constants for the adapter.
type config struct {
	AlertManagerURL  string
	PollInterval     time.Duration
	InitialDelay     time.Duration
	CooldownWindow   time.Duration
	DefaultNamespace string
	DefaultAgent     string
	HealthPort       string
}

func defaultConfig() config {
	return config{
		AlertManagerURL:  envOrDefault("ALERTMANAGER_URL", "https://alertmanager-main.openshift-monitoring.svc:9094"),
		PollInterval:     30 * time.Second,
		InitialDelay:     5 * time.Minute,
		CooldownWindow:   1 * time.Hour,
		DefaultNamespace: "openshift-lightspeed",
		DefaultAgent:     "default",
		HealthPort:       ":8081",
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// proposalClient implements poller.ProposalClient using a client-go REST client.
type proposalClient struct {
	client rest.Interface
}

func (c *proposalClient) ListProposals(ctx context.Context, labelSelector string) ([]agenticv1alpha1.Proposal, error) {
	var list agenticv1alpha1.ProposalList
	err := c.client.Get().
		Resource("proposals").
		Param("labelSelector", labelSelector).
		Do(ctx).
		Into(&list)
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

func (c *proposalClient) CreateProposal(ctx context.Context, p *agenticv1alpha1.Proposal) error {
	return c.client.Post().
		Namespace(p.Namespace).
		Resource("proposals").
		Body(p).
		Do(ctx).
		Into(p)
}

func newProposalRESTClient(restCfg *rest.Config) (rest.Interface, error) {
	scheme := runtime.NewScheme()
	if err := agenticv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("registering agentic scheme: %w", err)
	}

	cfg := *restCfg
	cfg.GroupVersion = &agenticv1alpha1.GroupVersion
	cfg.APIPath = "/apis"
	cfg.NegotiatedSerializer = serializer.NewCodecFactory(scheme)

	return rest.RESTClientFor(&cfg)
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg := defaultConfig()
	slog.Info("starting lightspeed-agentic-alerts-adapter",
		"alertManagerURL", cfg.AlertManagerURL,
		"pollInterval", cfg.PollInterval,
		"initialDelay", cfg.InitialDelay,
		"cooldownWindow", cfg.CooldownWindow,
	)

	restCfg, err := rest.InClusterConfig()
	if err != nil {
		slog.Error("failed to get in-cluster config", "error", err)
		os.Exit(1)
	}

	restClient, err := newProposalRESTClient(restCfg)
	if err != nil {
		slog.Error("failed to create kubernetes REST client", "error", err)
		os.Exit(1)
	}

	amClient, err := alertmanager.NewInClusterClient(cfg.AlertManagerURL)
	if err != nil {
		slog.Error("failed to create alertmanager client", "error", err)
		os.Exit(1)
	}

	pc := &proposalClient{client: restClient}
	p := poller.New(amClient, pc,
		poller.WithPollInterval(cfg.PollInterval),
		poller.WithInitialDelay(cfg.InitialDelay),
		poller.WithCooldownWindow(cfg.CooldownWindow),
	)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)

	go p.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("/healthz", healthzHandler())
	mux.Handle("/readyz", readyzHandler(p))

	srv := &http.Server{
		Addr:    cfg.HealthPort,
		Handler: mux,
	}

	go func() {
		slog.Info("health probe server starting", "addr", cfg.HealthPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("health probe server error", "error", err)
		}
	}()

	sig := <-sigCh
	slog.Info("received signal, shutting down", "signal", sig)
	cancel()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("health probe server shutdown error", "error", err)
	}

	slog.Info("shutdown complete")
}
