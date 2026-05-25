package alertmanager

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

const (
	alertsPath   = "/api/v2/alerts"
	alertsQuery  = "active=true&silenced=false&inhibited=false"
	caPath       = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	tokenPath    = "/var/run/secrets/kubernetes.io/serviceaccount/token"
)

type Client struct {
	baseURL    string
	httpClient *http.Client
	token      string
}

func NewClient(baseURL string) (*Client, error) {
	tlsConfig := &tls.Config{}

	if caCert, err := os.ReadFile(caPath); err == nil {
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caCert)
		tlsConfig.RootCAs = pool
	}

	token, _ := os.ReadFile(tokenPath)

	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: tlsConfig,
			},
		},
		token: string(token),
	}, nil
}

func NewClientWithHTTP(baseURL string, httpClient *http.Client) *Client {
	return &Client{
		baseURL:    baseURL,
		httpClient: httpClient,
	}
}

func (c *Client) GetFiringAlerts(ctx context.Context) ([]Alert, error) {
	url := fmt.Sprintf("%s%s?%s", c.baseURL, alertsPath, alertsQuery)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching alerts: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	var alerts []Alert
	if err := json.NewDecoder(resp.Body).Decode(&alerts); err != nil {
		return nil, fmt.Errorf("decoding alerts: %w", err)
	}

	return alerts, nil
}
