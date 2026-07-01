// Package api is the typed HTTP client for the Chariot backend.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client talks to the Chariot backend. Token is the session JWT (empty for the
// unauthenticated device-auth calls).
type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

// New builds a client with a sane timeout.
func New(baseURL, token string) *Client {
	return &Client{BaseURL: baseURL, Token: token, HTTP: &http.Client{Timeout: 30 * time.Second}}
}

// APIError is a non-2xx response from the backend.
type APIError struct {
	Status int
	Detail string
}

func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("%s (HTTP %d)", e.Detail, e.Status)
	}
	return fmt.Sprintf("HTTP %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) (int, error) {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return 0, err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return resp.StatusCode, &APIError{Status: resp.StatusCode, Detail: extractDetail(data)}
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return resp.StatusCode, fmt.Errorf("decoding response: %w", err)
		}
	}
	return resp.StatusCode, nil
}

func extractDetail(data []byte) string {
	var e struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(data, &e) == nil && e.Detail != "" {
		return e.Detail
	}
	return string(data)
}

// --- device auth -----------------------------------------------------------

type DeviceStart struct {
	DeviceCode            string `json:"device_code"`
	UserCode              string `json:"user_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval              int    `json:"interval"`
	ExpiresIn             int    `json:"expires_in"`
}

func (c *Client) StartDeviceAuth(ctx context.Context) (*DeviceStart, error) {
	out := &DeviceStart{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/auth/device/start", struct{}{}, out); err != nil {
		return nil, err
	}
	return out, nil
}

// PollDeviceAuth returns the session token once approved. It returns
// ("", nil) while still pending (HTTP 202), so the caller keeps polling.
func (c *Client) PollDeviceAuth(ctx context.Context, deviceCode string) (string, error) {
	out := struct {
		Token string `json:"token"`
	}{}
	status, err := c.do(ctx, http.MethodPost, "/v1/auth/device/token",
		map[string]string{"device_code": deviceCode}, &out)
	if status == http.StatusAccepted {
		return "", nil // still pending
	}
	if err != nil {
		return "", err
	}
	return out.Token, nil
}

// --- deploy / list / account ----------------------------------------------

type DeployResult struct {
	TokenSeed     string         `json:"token_seed"`
	Namespace     string         `json:"namespace"`
	Created       int            `json:"created"`
	Total         int            `json:"total"`
	AgentsByState map[string]int `json:"agents_by_state"`
}

func (c *Client) Deploy(ctx context.Context, count int, endpoint string) (*DeployResult, error) {
	out := &DeployResult{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/deploy",
		map[string]any{"count": count, "endpoint": endpoint}, out); err != nil {
		return nil, err
	}
	return out, nil
}

type Agent struct {
	ID    string `json:"id"`
	Slug  string `json:"slug"`
	State string `json:"state"`
}

type AgentPage struct {
	Agents     []Agent `json:"agents"`
	NextCursor string  `json:"next_cursor"`
}

func (c *Client) ListAgents(ctx context.Context, cursor string, limit int) (*AgentPage, error) {
	path := fmt.Sprintf("/v1/agents?limit=%d", limit)
	if cursor != "" {
		path += "&cursor=" + cursor
	}
	out := &AgentPage{}
	if _, err := c.do(ctx, http.MethodGet, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

type Account struct {
	Email         string         `json:"email"`
	Status        string         `json:"status"`
	CreditDollars float64        `json:"credit_dollars"`
	TokenPrefixes []string       `json:"token_prefixes"`
	AgentsByState map[string]int `json:"agents_by_state"`
}

func (c *Client) Account(ctx context.Context) (*Account, error) {
	out := &Account{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/account", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}
