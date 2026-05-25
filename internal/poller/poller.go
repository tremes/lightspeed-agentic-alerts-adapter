package poller

import (
	"context"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	agenticv1alpha1 "github.com/openshift/lightspeed-agentic-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/rest"

	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/alertmanager"
	"github.com/openshift/lightspeed-agentic-alerts-adapter/internal/proposal"
)

const (
	PollInterval  = 30 * time.Second
	InitialDelay  = 5 * time.Minute
	CooldownWindow = 1 * time.Hour
)

type ProposalClient interface {
	Create(ctx context.Context, p *agenticv1alpha1.Proposal) (*agenticv1alpha1.Proposal, error)
	List(ctx context.Context, opts metav1.ListOptions) (*agenticv1alpha1.ProposalList, error)
}

type Poller struct {
	amClient       *alertmanager.Client
	proposalClient ProposalClient
	logger         *slog.Logger
	ready          *atomic.Bool
}

func NewPoller(amClient *alertmanager.Client, proposalClient ProposalClient, logger *slog.Logger, ready *atomic.Bool) *Poller {
	return &Poller{
		amClient:       amClient,
		proposalClient: proposalClient,
		logger:         logger,
		ready:          ready,
	}
}

func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()

	p.logger.Info("poll loop started", "interval", PollInterval)

	if err := p.poll(ctx); err != nil {
		p.logger.Error("poll cycle failed", "error", err)
	}

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("poll loop stopped")
			return
		case <-ticker.C:
			if err := p.poll(ctx); err != nil {
				p.logger.Error("poll cycle failed", "error", err)
			}
		}
	}
}

func (p *Poller) poll(ctx context.Context) error {
	p.logger.Info("poll cycle starting")

	alerts, err := p.amClient.GetFiringAlerts(ctx)
	if err != nil {
		p.ready.Store(false)
		return fmt.Errorf("fetching alerts: %w", err)
	}

	existing, err := p.proposalClient.List(ctx, metav1.ListOptions{
		LabelSelector: proposal.LabelSource + "=alertmanager",
	})
	if err != nil {
		p.ready.Store(false)
		return fmt.Errorf("listing proposals: %w", err)
	}

	proposalsByFingerprint := indexByFingerprint(existing.Items)

	var created int
	for _, alert := range alerts {
		alertname := alert.Labels["alertname"]
		if alertname == "" {
			p.logger.Debug("skipping alert without alertname")
			continue
		}

		fp := alert.Fingerprint
		if len(fp) > 8 {
			fp = fp[:8]
		}

		if time.Since(alert.StartsAt) < InitialDelay {
			p.logger.Debug("skipping alert: initial delay not met",
				"alertname", alertname, "fingerprint", fp,
				"firingFor", time.Since(alert.StartsAt))
			continue
		}

		proposals := proposalsByFingerprint[fp]
		if hasActiveProposal(proposals) {
			p.logger.Debug("skipping alert: active proposal exists",
				"alertname", alertname, "fingerprint", fp)
			continue
		}

		if inCooldown(proposals) {
			p.logger.Debug("skipping alert: within cooldown window",
				"alertname", alertname, "fingerprint", fp)
			continue
		}

		prop, err := proposal.BuildProposal(alert)
		if err != nil {
			p.logger.Error("building proposal failed",
				"alertname", alertname, "error", err)
			continue
		}

		_, err = p.proposalClient.Create(ctx, prop)
		if err != nil {
			if isConflict(err) {
				p.logger.Debug("proposal already exists (conflict)",
					"alertname", alertname, "fingerprint", fp)
				continue
			}
			p.logger.Error("creating proposal failed",
				"alertname", alertname, "fingerprint", fp, "error", err)
			continue
		}

		p.logger.Info("proposal created",
			"name", prop.Name, "namespace", prop.Namespace,
			"alertname", alertname, "fingerprint", fp)
		created++
	}

	p.ready.Store(true)
	p.logger.Info("poll cycle complete",
		"alertsTotal", len(alerts), "proposalsCreated", created)
	return nil
}

func indexByFingerprint(proposals []agenticv1alpha1.Proposal) map[string][]agenticv1alpha1.Proposal {
	m := make(map[string][]agenticv1alpha1.Proposal)
	for _, p := range proposals {
		fp := p.Labels[proposal.LabelAlertFingerprint]
		if fp != "" {
			m[fp] = append(m[fp], p)
		}
	}
	return m
}

func hasActiveProposal(proposals []agenticv1alpha1.Proposal) bool {
	for _, p := range proposals {
		if !isTerminal(p) {
			return true
		}
	}
	return false
}

func isTerminal(p agenticv1alpha1.Proposal) bool {
	phase := agenticv1alpha1.DerivePhase(p.Status.Conditions)
	switch phase {
	case agenticv1alpha1.ProposalPhaseCompleted,
		agenticv1alpha1.ProposalPhaseFailed,
		agenticv1alpha1.ProposalPhaseDenied,
		agenticv1alpha1.ProposalPhaseEscalated:
		return true
	}
	return false
}

func inCooldown(proposals []agenticv1alpha1.Proposal) bool {
	for _, p := range proposals {
		if !isTerminal(p) {
			continue
		}
		terminalTime := terminalTimestamp(p)
		if terminalTime.IsZero() {
			continue
		}
		if time.Since(terminalTime) < CooldownWindow {
			return true
		}
	}
	return false
}

func terminalTimestamp(p agenticv1alpha1.Proposal) time.Time {
	var latest time.Time
	for _, c := range p.Status.Conditions {
		switch c.Type {
		case agenticv1alpha1.ProposalConditionVerified,
			agenticv1alpha1.ProposalConditionDenied,
			agenticv1alpha1.ProposalConditionEscalated:
			if c.Status != metav1.ConditionUnknown && c.LastTransitionTime.After(latest) {
				latest = c.LastTransitionTime.Time
			}
		}
	}
	return latest
}

func isConflict(err error) bool {
	if err == nil {
		return false
	}
	// k8s.io/apimachinery status errors contain the status code
	type statusError interface {
		Status() metav1.Status
	}
	if se, ok := err.(statusError); ok {
		return se.Status().Code == 409
	}
	return false
}

func NewProposalRESTClient(config *rest.Config) (*rest.RESTClient, error) {
	scheme := runtime.NewScheme()
	if err := agenticv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("adding agentic scheme: %w", err)
	}
	if err := metav1.AddMetaToScheme(scheme); err != nil {
		return nil, fmt.Errorf("adding meta scheme: %w", err)
	}

	config.GroupVersion = &agenticv1alpha1.GroupVersion
	config.APIPath = "/apis"
	config.NegotiatedSerializer = serializer.NewCodecFactory(scheme)

	return rest.RESTClientFor(config)
}

type restProposalClient struct {
	client *rest.RESTClient
}

func NewRESTProposalClient(client *rest.RESTClient) ProposalClient {
	return &restProposalClient{client: client}
}

func (c *restProposalClient) Create(ctx context.Context, p *agenticv1alpha1.Proposal) (*agenticv1alpha1.Proposal, error) {
	result := &agenticv1alpha1.Proposal{}
	err := c.client.Post().
		Namespace(p.Namespace).
		Resource("proposals").
		Body(p).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *restProposalClient) List(ctx context.Context, opts metav1.ListOptions) (*agenticv1alpha1.ProposalList, error) {
	result := &agenticv1alpha1.ProposalList{}
	err := c.client.Get().
		Resource("proposals").
		VersionedParams(&opts, runtime.NewParameterCodec(c.scheme())).
		Do(ctx).
		Into(result)
	return result, err
}

func (c *restProposalClient) scheme() *runtime.Scheme {
	s := runtime.NewScheme()
	_ = agenticv1alpha1.AddToScheme(s)
	_ = metav1.AddMetaToScheme(s)
	return s
}
