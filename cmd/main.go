package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"

	"k8s.io/client-go/rest"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/poller"
)

const alertManagerURL = "https://alertmanager-main.openshift-monitoring.svc:9094"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	slog.SetDefault(logger)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	logger.Info("starting alerts-adapter")

	amClient, err := alertmanager.NewClient(alertManagerURL)
	if err != nil {
		logger.Error("creating alertmanager client", "error", err)
		os.Exit(1)
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		logger.Error("loading in-cluster config", "error", err)
		os.Exit(1)
	}

	restClient, err := poller.NewProposalRESTClient(config)
	if err != nil {
		logger.Error("creating proposal REST client", "error", err)
		os.Exit(1)
	}

	proposalClient := poller.NewRESTProposalClient(restClient)
	ready := &atomic.Bool{}

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) {
		if ready.Load() {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("ok"))
		} else {
			w.WriteHeader(http.StatusServiceUnavailable)
			w.Write([]byte("not ready"))
		}
	})

	healthServer := &http.Server{Addr: ":8080", Handler: mux}
	go func() {
		logger.Info("health server listening", "addr", ":8080")
		if err := healthServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("health server failed", "error", err)
		}
	}()

	p := poller.NewPoller(amClient, proposalClient, logger, ready)
	p.Run(ctx)

	healthServer.Close()
	logger.Info("shutting down alerts-adapter")
}
