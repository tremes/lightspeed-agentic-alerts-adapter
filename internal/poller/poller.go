package poller

import (
	"context"
	"log/slog"
	"sync/atomic"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/proposal"
)

const (
	defaultPollInterval  = 30 * time.Second
	defaultInitialDelay  = 5 * time.Minute
	defaultCooldownWindow = 1 * time.Hour

	labelSource           = "agentic.openshift.io/source"
	labelAlertFingerprint = "agentic.openshift.io/alert-fingerprint"
	sourceValue           = "alertmanager"
	labelSelectorSource   = labelSource + "=" + sourceValue
)

var terminalConditionTypes = map[string]bool{
	"Completed": true,
	"Failed":    true,
	"Escalated": true,
	"Denied":    true,
}

// AlertFetcher retrieves currently firing alerts from AlertManager.
type AlertFetcher interface {
	FetchFiringAlerts(ctx context.Context) ([]alertmanager.Alert, error)
}

// ProposalClient lists and creates Proposal CRs in the Kubernetes API.
type ProposalClient interface {
	ListProposals(ctx context.Context, labelSelector string) ([]agenticv1alpha1.Proposal, error)
	CreateProposal(ctx context.Context, p *agenticv1alpha1.Proposal) error
}

// Poller implements the core reconciliation loop: fetch firing alerts,
// compare against existing Proposals, and create new Proposals for
// alerts that pass deduplication checks.
type Poller struct {
	alerts         AlertFetcher
	proposals      ProposalClient
	pollInterval   time.Duration
	initialDelay   time.Duration
	cooldownWindow time.Duration
	nowFunc        func() time.Time
	healthy        atomic.Bool
}

// Option configures the Poller during construction.
type Option func(*Poller)

// WithPollInterval sets how often the poller fetches alerts and reconciles Proposals.
func WithPollInterval(d time.Duration) Option {
	return func(p *Poller) { p.pollInterval = d }
}

// WithInitialDelay sets the minimum time an alert must be firing before a Proposal is created.
func WithInitialDelay(d time.Duration) Option {
	return func(p *Poller) { p.initialDelay = d }
}

// WithCooldownWindow sets the minimum time after a terminal Proposal before re-proposing for the same alert.
func WithCooldownWindow(d time.Duration) Option {
	return func(p *Poller) { p.cooldownWindow = d }
}

func withNowFunc(f func() time.Time) Option {
	return func(p *Poller) { p.nowFunc = f }
}

// New creates a Poller with the given alert and proposal clients.
func New(alerts AlertFetcher, proposals ProposalClient, opts ...Option) *Poller {
	p := &Poller{
		alerts:         alerts,
		proposals:      proposals,
		pollInterval:   defaultPollInterval,
		initialDelay:   defaultInitialDelay,
		cooldownWindow: defaultCooldownWindow,
		nowFunc:        time.Now,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Healthy reports whether the last poll cycle completed without critical errors.
func (p *Poller) Healthy() bool {
	return p.healthy.Load()
}

// Run executes the poll loop until the context is cancelled.
// Poll cycle errors are logged and retried on the next interval.
func (p *Poller) Run(ctx context.Context) {
	slog.Info("poller starting", "interval", p.pollInterval.String(), "initialDelay", p.initialDelay.String(), "cooldownWindow", p.cooldownWindow.String())
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()

	p.pollOnce(ctx)

	for {
		select {
		case <-ctx.Done():
			slog.Info("poller shutting down")
			return
		case <-ticker.C:
			p.pollOnce(ctx)
		}
	}
}

func (p *Poller) pollOnce(ctx context.Context) {
	now := p.nowFunc().UTC()

	firingAlerts, err := p.alerts.FetchFiringAlerts(ctx)
	if err != nil {
		slog.Error("failed to fetch firing alerts", "error", err)
		p.healthy.Store(false)
		return
	}

	existingProposals, err := p.proposals.ListProposals(ctx, labelSelectorSource)
	if err != nil {
		slog.Error("failed to list proposals", "error", err)
		p.healthy.Store(false)
		return
	}

	proposalsByFingerprint := indexProposalsByFingerprint(existingProposals)

	for _, alert := range firingAlerts {
		p.processAlert(ctx, alert, proposalsByFingerprint, now)
	}

	p.healthy.Store(true)
}

func (p *Poller) processAlert(ctx context.Context, alert alertmanager.Alert, proposalsByFP map[string][]agenticv1alpha1.Proposal, now time.Time) {
	fingerprint := ""
	if alert.Fingerprint != nil {
		fingerprint = *alert.Fingerprint
	}
	fp := fingerprint
	if len(fp) > 8 {
		fp = fp[:8]
	}

	logger := slog.With("fingerprint", fingerprint, "alertname", alert.Labels["alertname"])

	if alert.StartsAt != nil {
		startsAt := time.Time(*alert.StartsAt)
		if now.Sub(startsAt) < p.initialDelay {
			logger.Debug("alert within initial delay, skipping")
			return
		}
	}

	built, err := proposal.BuildProposal(alert)
	if err != nil {
		logger.Warn("failed to build proposal from alert, skipping", "error", err)
		return
	}

	matching := proposalsByFP[fp]
	for _, existing := range matching {
		terminal, terminalTime := terminalState(existing)
		if !terminal {
			logger.Debug("active proposal exists, skipping")
			return
		}
		if now.Sub(terminalTime) < p.cooldownWindow {
			logger.Debug("terminal proposal within cooldown, skipping", "terminalTime", terminalTime)
			return
		}
	}

	err = p.proposals.CreateProposal(ctx, built)
	if err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Debug("proposal already exists (409), treating as success")
			return
		}
		logger.Error("failed to create proposal", "error", err)
		return
	}

	logger.Info("created proposal", "name", built.Name, "namespace", built.Namespace)
}

func terminalState(p agenticv1alpha1.Proposal) (bool, time.Time) {
	for _, c := range p.Status.Conditions {
		if terminalConditionTypes[c.Type] && c.Status == metav1.ConditionTrue {
			return true, c.LastTransitionTime.Time
		}
	}
	return false, time.Time{}
}

func indexProposalsByFingerprint(proposals []agenticv1alpha1.Proposal) map[string][]agenticv1alpha1.Proposal {
	m := make(map[string][]agenticv1alpha1.Proposal)
	for _, p := range proposals {
		fp := p.Labels[labelAlertFingerprint]
		if fp != "" {
			m[fp] = append(m[fp], p)
		}
	}
	return m
}
