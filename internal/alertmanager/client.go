package alertmanager

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/url"
	"os"

	openapiclient "github.com/go-openapi/runtime/client"
	"github.com/prometheus/alertmanager/api/v2/client"
	"github.com/prometheus/alertmanager/api/v2/client/alert"
)

const (
	saTokenPath = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	saCAPath    = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
)

// Client wraps the AlertManager v2 API client to fetch firing alerts.
type Client struct {
	amClient *client.AlertmanagerAPI
}

// Option configures the Client during construction.
type Option func(*clientConfig)

type clientConfig struct {
	httpClient *http.Client
	token      string
}

// WithHTTPClient sets the underlying HTTP client used for AlertManager requests.
func WithHTTPClient(c *http.Client) Option {
	return func(cfg *clientConfig) {
		cfg.httpClient = c
	}
}

// WithToken sets the Bearer token used to authenticate against AlertManager.
func WithToken(token string) Option {
	return func(cfg *clientConfig) {
		cfg.token = token
	}
}

// NewClient creates an AlertManager client for the given base URL.
// Use Option values to configure authentication and transport.
func NewClient(baseURL string, opts ...Option) *Client {
	cfg := &clientConfig{}
	for _, opt := range opts {
		opt(cfg)
	}

	parsed, _ := url.Parse(baseURL)
	schemes := []string{parsed.Scheme}
	if parsed.Scheme == "" {
		schemes = []string{"https"}
	}

	transport := openapiclient.New(parsed.Host, client.DefaultBasePath, schemes)
	if cfg.httpClient != nil {
		transport.Transport = cfg.httpClient.Transport
	}
	if cfg.token != "" {
		transport.DefaultAuthentication = openapiclient.BearerToken(cfg.token)
	}

	return &Client{
		amClient: client.New(transport, nil),
	}
}

// NewInClusterClient creates a Client configured for in-cluster use,
// reading the ServiceAccount token and CA bundle from the pod filesystem.
func NewInClusterClient(baseURL string) (*Client, error) {
	token, err := os.ReadFile(saTokenPath)
	if err != nil {
		return nil, fmt.Errorf("reading service account token: %w", err)
	}

	caCert, err := os.ReadFile(saCAPath)
	if err != nil {
		return nil, fmt.Errorf("reading cluster CA bundle: %w", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCert) {
		return nil, fmt.Errorf("failed to parse cluster CA bundle")
	}

	httpClient := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caPool,
			},
		},
	}

	return NewClient(baseURL, WithHTTPClient(httpClient), WithToken(string(token))), nil
}

// FetchFiringAlerts retrieves all currently firing (active, non-silenced, non-inhibited)
// alerts from AlertManager.
func (c *Client) FetchFiringAlerts(ctx context.Context) ([]Alert, error) {
	active := true
	silenced := false
	inhibited := false

	params := alert.NewGetAlertsParamsWithContext(ctx)
	params.SetActive(&active)
	params.SetSilenced(&silenced)
	params.SetInhibited(&inhibited)

	resp, err := c.amClient.Alert.GetAlerts(params)
	if err != nil {
		return nil, fmt.Errorf("fetching alerts from alertmanager: %w", err)
	}

	result := make([]Alert, len(resp.Payload))
	for i, a := range resp.Payload {
		result[i] = *a
	}
	return result, nil
}
