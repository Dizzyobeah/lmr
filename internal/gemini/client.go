// Package gemini is a minimal client for Google's OpenAI-compatible Gemini endpoint.
package gemini

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"time"
)

// Client posts OpenAI-shaped requests to Gemini. The API key is only ever
// sent in the Authorization header, never put in errors or logs.
type Client struct {
	endpoint string
	apiKey   string
	http     *http.Client
}

// NewClient creates a client. timeout bounds the wait for response headers
// only, so long streamed responses are not cut off.
func NewClient(endpoint, apiKey string, timeout time.Duration) *Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		IdleConnTimeout:       90 * time.Second,
		ForceAttemptHTTP2:     true,
		ResponseHeaderTimeout: timeout,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
	}
	return &Client{
		endpoint: endpoint,
		apiKey:   apiKey,
		http: &http.Client{
			Transport: transport,
			// Never follow redirects: Go would resend Authorization to a
			// same-host target, possibly over plain http. A 3xx is a failure.
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// ChatCompletions POSTs body to {endpoint}/chat/completions and returns the
// raw response. The caller must close resp.Body.
func (c *Client) ChatCompletions(ctx context.Context, body []byte) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")
	return c.http.Do(req)
}
