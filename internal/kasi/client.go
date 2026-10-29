// Package kasi contains a constrained transport for independently querying the
// data.go.kr API. Request parameters and XML interpretation require verification
// against the official service documentation before a live comparison.
package kasi

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const MaxResponseBytes = 1 << 20

var ErrUnavailable = errors.New("KASI API request failed")

type Client struct {
	endpoint          *url.URL
	client            *http.Client
	keyParameter, key string
}

// New requires the documented HTTPS endpoint and key parameter to be supplied
// by trusted server configuration. It does not discover or invent an API schema.
func New(endpoint, keyParameter, key string, transport http.RoundTripper) (*Client, error) {
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host != "apis.data.go.kr" || u.User != nil || u.Path == "" || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("invalid KASI HTTPS endpoint")
	}
	if strings.TrimSpace(keyParameter) == "" || strings.TrimSpace(key) == "" {
		return nil, errors.New("KASI key configuration required")
	}
	if transport == nil {
		transport = http.DefaultTransport
	}
	return &Client{endpoint: u, client: &http.Client{Transport: transport, Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}, keyParameter: keyParameter, key: key}, nil
}

// Fetch accepts only trusted server-selected query parameters. It returns raw
// bytes so callers can save and independently parse genuine responses after
// confirming the official contract. Errors deliberately never expose the URL,
// query string, or key. A successful HTTP response is not proof of API success.
func (c *Client) Fetch(ctx context.Context, params url.Values) ([]byte, error) {
	query := make(url.Values, len(params)+1)
	for k, v := range params {
		if strings.EqualFold(k, c.keyParameter) {
			return nil, ErrUnavailable
		}
		query[k] = append([]string(nil), v...)
	}
	query.Set(c.keyParameter, c.key)
	u := *c.endpoint
	u.RawQuery = query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, ErrUnavailable
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, ErrUnavailable
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil || len(body) > MaxResponseBytes {
		return nil, ErrUnavailable
	}
	return body, nil
}
