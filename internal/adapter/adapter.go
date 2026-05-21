package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/proposal"
)

type AlertFetcher interface {
	FiringAlerts(ctx context.Context) ([]alertmanager.Alert, error)
}

type Config struct {
	InitialDelay   time.Duration
	CooldownWindow time.Duration
	AgentName      string
}

type Adapter struct {
	alerts AlertFetcher
	k8s    client.Client
	config Config
}

func New(alerts AlertFetcher, k8s client.Client, config Config) *Adapter {
	return &Adapter{
		alerts: alerts,
		k8s:    k8s,
		config: config,
	}
}

func (a *Adapter) Reconcile(ctx context.Context) error {
	alerts, err := a.alerts.FiringAlerts(ctx)
	if err != nil {
		return fmt.Errorf("fetching alerts: %w", err)
	}

	var existingProposals agenticv1alpha1.ProposalList
	if err := a.k8s.List(ctx, &existingProposals, client.MatchingLabels{
		proposal.LabelSource: proposal.SourceAlertManager,
	}); err != nil {
		return fmt.Errorf("listing proposals: %w", err)
	}

	proposalsByFingerprint := make(map[string][]agenticv1alpha1.Proposal)
	for _, p := range existingProposals.Items {
		fp := p.Labels[proposal.LabelAlertFingerprint]
		proposalsByFingerprint[fp] = append(proposalsByFingerprint[fp], p)
	}

	now := time.Now().UTC()

	for _, alert := range alerts {
		fp := proposal.TruncateFingerprint(alert.Fingerprint)

		if now.Sub(alert.StartsAt) < a.config.InitialDelay {
			slog.Debug("skipping alert within initial delay",
				"alertname", alert.Labels["alertname"],
				"fingerprint", fp,
				"age", now.Sub(alert.StartsAt).String())
			continue
		}

		if a.hasActiveProposal(proposalsByFingerprint[fp]) {
			slog.Debug("skipping alert with active proposal",
				"alertname", alert.Labels["alertname"],
				"fingerprint", fp)
			continue
		}

		if a.hasTerminalProposalInCooldown(proposalsByFingerprint[fp], now) {
			slog.Debug("skipping alert within cooldown window",
				"alertname", alert.Labels["alertname"],
				"fingerprint", fp)
			continue
		}

		p, err := proposal.BuildProposal(alert, a.config.AgentName)
		if err != nil {
			slog.Error("skipping invalid alert",
				"fingerprint", alert.Fingerprint,
				"error", err.Error())
			continue
		}

		if err := a.k8s.Create(ctx, p); err != nil {
			slog.Error("failed to create proposal",
				"proposal", p.Name,
				"namespace", p.Namespace,
				"error", err.Error())
			continue
		}

		slog.Info("created proposal",
			"proposal", p.Name,
			"namespace", p.Namespace,
			"alertname", alert.Labels["alertname"],
			"fingerprint", fp)
	}

	return nil
}

func (a *Adapter) hasActiveProposal(proposals []agenticv1alpha1.Proposal) bool {
	for _, p := range proposals {
		phase := agenticv1alpha1.DerivePhase(p.Status.Conditions)
		switch phase {
		case agenticv1alpha1.ProposalPhaseCompleted,
			agenticv1alpha1.ProposalPhaseFailed,
			agenticv1alpha1.ProposalPhaseEscalated,
			agenticv1alpha1.ProposalPhaseDenied:
			continue
		default:
			return true
		}
	}
	return false
}

func (a *Adapter) hasTerminalProposalInCooldown(proposals []agenticv1alpha1.Proposal, now time.Time) bool {
	for _, p := range proposals {
		phase := agenticv1alpha1.DerivePhase(p.Status.Conditions)
		switch phase {
		case agenticv1alpha1.ProposalPhaseCompleted,
			agenticv1alpha1.ProposalPhaseFailed,
			agenticv1alpha1.ProposalPhaseEscalated,
			agenticv1alpha1.ProposalPhaseDenied:
			terminalTime := a.terminalTransitionTime(p.Status.Conditions)
			if now.Sub(terminalTime) < a.config.CooldownWindow {
				return true
			}
		}
	}
	return false
}

func (a *Adapter) terminalTransitionTime(conditions []metav1.Condition) time.Time {
	var latest time.Time
	for _, c := range conditions {
		if c.Status == metav1.ConditionTrue && c.LastTransitionTime.After(latest) {
			latest = c.LastTransitionTime.Time
		}
	}
	return latest
}
