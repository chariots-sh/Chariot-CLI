// Package api is the typed HTTP client for the Chariot backend.
package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
	return c.doHeaders(ctx, method, path, nil, body, out)
}

func (c *Client) doHeaders(ctx context.Context, method, path string, headers map[string]string, body, out any) (int, error) {
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
	for k, v := range headers {
		req.Header.Set(k, v)
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
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	Interval                int    `json:"interval"`
	ExpiresIn               int    `json:"expires_in"`
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
	Model         string         `json:"model"`
	Image         string         `json:"image"` // built-in image name; "" = account default
	// Skills explicitly granted to the created agents (`--skills`); empty =
	// none granted at deploy.
	Skills []string `json:"skills"`
}

func (c *Client) Deploy(ctx context.Context, count int, endpoint, model, image string, skills []string) (*DeployResult, error) {
	body := map[string]any{"count": count}
	if endpoint != "" { // omitted → inbox-only, replies polled via ListReplies
		body["endpoint"] = endpoint
	}
	if model != "" { // omitted → the account's current model choice stands
		body["model"] = model
	}
	if image != "" { // omitted → the account default (custom image, else stock)
		body["image"] = image
	}
	if len(skills) > 0 { // omitted → no explicit grants (see `--skills` help)
		body["skills"] = skills
	}
	out := &DeployResult{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/deploy", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetModel chooses the model the account's fleet uses — any OpenRouter model
// id; an empty model resets to the server default. Returns the effective
// model after the change.
func (c *Client) SetModel(ctx context.Context, model string) (string, error) {
	body := map[string]any{"model": nil}
	if model != "" {
		body["model"] = model
	}
	out := struct {
		Model string `json:"model"`
	}{}
	if _, err := c.do(ctx, http.MethodPut, "/v1/account/model", body, &out); err != nil {
		return "", err
	}
	return out.Model, nil
}

// SetHibernateAfter chooses how long the account's agents may sit idle before
// they hibernate, in seconds; seconds <= 0 resets to the server default
// (48h). Returns the effective window after the change.
func (c *Client) SetHibernateAfter(ctx context.Context, seconds int64) (int64, error) {
	body := map[string]any{"seconds": nil}
	if seconds > 0 {
		body["seconds"] = seconds
	}
	out := struct {
		HibernateAfterSeconds int64 `json:"hibernate_after_seconds"`
	}{}
	if _, err := c.do(ctx, http.MethodPut, "/v1/account/hibernate-after", body, &out); err != nil {
		return 0, err
	}
	return out.HibernateAfterSeconds, nil
}

// SetDefaultImage chooses the image agents deployed without --image run — a
// built-in name or one of the account's verified custom image names; an empty
// image resets to the implicit default (the custom image named 'default' if
// one is verified, else stock). Returns the effective default after the
// change.
func (c *Client) SetDefaultImage(ctx context.Context, image string) (string, error) {
	body := map[string]any{"image": nil}
	if image != "" {
		body["image"] = image
	}
	out := struct {
		DefaultImage string `json:"default_image"`
	}{}
	if _, err := c.do(ctx, http.MethodPut, "/v1/account/default-image", body, &out); err != nil {
		return "", err
	}
	return out.DefaultImage, nil
}

type Agent struct {
	ID    string  `json:"id"`
	Slug  string  `json:"slug"`
	Name  *string `json:"name"` // owner-chosen alias (`chariot rename`); nil = unnamed
	State string  `json:"state"`
	Image *string `json:"image"` // built-in image name; nil = account default
	Model *string `json:"model"` // OpenRouter model id; nil = account default
	// Idle window (seconds) before this agent hibernates; nil = account default.
	HibernateAfterSeconds *int64 `json:"hibernate_after_seconds"`
}

// SetAgentName names (aliases) ONE agent; an empty name clears it back to
// unnamed. The name is accepted anywhere an agent id or slug is — the slug
// stays the stable identifier, so renaming changes no infrastructure. The
// returned Agent carries the canonical slug and the name after the change.
func (c *Client) SetAgentName(ctx context.Context, agentRef, name string) (*Agent, error) {
	body := map[string]any{"name": nil}
	if name != "" {
		body["name"] = name
	}
	out := &Agent{}
	if _, err := c.do(ctx, http.MethodPut, "/v1/agents/"+agentRef+"/name", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetAgentModel overrides ONE agent's model — any OpenRouter model id; an empty
// model clears the override back to the account default. Returns the agent's
// effective model after the change.
func (c *Client) SetAgentModel(ctx context.Context, agentID, model string) (string, error) {
	body := map[string]any{"model": nil}
	if model != "" {
		body["model"] = model
	}
	out := struct {
		Model string `json:"model"`
	}{}
	if _, err := c.do(ctx, http.MethodPut, "/v1/agents/"+agentID+"/model", body, &out); err != nil {
		return "", err
	}
	return out.Model, nil
}

// AgentSkills is one agent's skills state: the explicit grants vs. the
// effective (projected) set — the union with membership-implied grants, e.g.
// docs for agents in a shared-documents space.
type AgentSkills struct {
	Granted   []string `json:"granted"`
	Effective []string `json:"effective"`
}

// GetAgentSkills reads one agent's skills (id, slug, or name).
func (c *Client) GetAgentSkills(ctx context.Context, agentRef string) (*AgentSkills, error) {
	out := &AgentSkills{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/agents/"+url.PathEscape(agentRef)+"/skills", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// AddAgentSkill grants one skill to an existing agent (idempotent). A running
// agent picks the tool up within about a minute; a dormant one on next wake.
func (c *Client) AddAgentSkill(ctx context.Context, agentRef, skill string) (*AgentSkills, error) {
	out := &AgentSkills{}
	body := map[string]any{"skill": skill}
	if _, err := c.do(ctx, http.MethodPost, "/v1/agents/"+url.PathEscape(agentRef)+"/skills", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveAgentSkill revokes one explicit grant (idempotent). A skill the agent
// also holds through shared-documents membership stays effective.
func (c *Client) RemoveAgentSkill(ctx context.Context, agentRef, skill string) (*AgentSkills, error) {
	out := &AgentSkills{}
	path := "/v1/agents/" + url.PathEscape(agentRef) + "/skills/" + url.PathEscape(skill)
	if _, err := c.do(ctx, http.MethodDelete, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SetAgentHibernateAfter overrides ONE agent's idle→hibernate window, in
// seconds; seconds <= 0 clears the override back to the account window (else
// the server default, 48h). Returns the agent's effective window after the
// change.
func (c *Client) SetAgentHibernateAfter(ctx context.Context, agentID string, seconds int64) (int64, error) {
	body := map[string]any{"seconds": nil}
	if seconds > 0 {
		body["seconds"] = seconds
	}
	out := struct {
		HibernateAfterSeconds int64 `json:"hibernate_after_seconds"`
	}{}
	if _, err := c.do(ctx, http.MethodPut, "/v1/agents/"+agentID+"/hibernate-after", body, &out); err != nil {
		return 0, err
	}
	return out.HibernateAfterSeconds, nil
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

// DeleteAgent permanently deletes one agent (tears down its pod, PVC, and any
// other workload resources — unlike hibernation, session state is not kept).
func (c *Client) DeleteAgent(ctx context.Context, agentID string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/agents/"+agentID, nil, nil)
	return err
}

// HibernateAgent forces one of the caller's agents into hibernation right
// now, bypassing the backend's 48h idle wait. A no-op (still succeeds) if the
// agent is already hibernating or was never activated.
func (c *Client) HibernateAgent(ctx context.Context, slug string) (*Agent, error) {
	out := &Agent{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/agents/"+slug+"/hibernate", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// MessageAck is the backend's 202 response to an inbound agent message.
type MessageAck struct {
	Status  string `json:"status"`
	AgentID string `json:"agent_id"`
	State   string `json:"state"`
}

// SendMessage delivers a message to an agent. It authenticates with the
// token-seed printed by `chariot deploy` (X-Chariot-Token header), not the
// session JWT — this is the same call a customer backend makes.
func (c *Client) SendMessage(ctx context.Context, agentID, tokenSeed, message string) (*MessageAck, error) {
	out := &MessageAck{}
	if _, err := c.doHeaders(ctx, http.MethodPost, "/v1/agents/"+agentID+"/messages",
		map[string]string{"X-Chariot-Token": tokenSeed},
		map[string]string{"message": message}, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Reply is one stored agent reply from the account's inbox.
type Reply struct {
	ID        int64     `json:"id"`
	AgentID   string    `json:"agent_id"`
	Message   string    `json:"message"`
	ReplyTo   *string   `json:"reply_to"`
	CreatedAt time.Time `json:"created_at"`
}

type ReplyPage struct {
	Replies    []Reply `json:"replies"`
	NextCursor int64   `json:"next_cursor"`
}

// ListReplies pages the account's reply inbox: replies with id > after, oldest
// first. Token-seed auth (X-Chariot-Token), like SendMessage. Pass the returned
// NextCursor straight back as the next after.
func (c *Client) ListReplies(ctx context.Context, tokenSeed string, after int64, limit int) (*ReplyPage, error) {
	out := &ReplyPage{}
	path := fmt.Sprintf("/v1/replies?after=%d&limit=%d", after, limit)
	if _, err := c.doHeaders(ctx, http.MethodGet, path,
		map[string]string{"X-Chariot-Token": tokenSeed}, nil, out); err != nil {
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
	Model         string         `json:"model"`
	// Effective idle→hibernate window (the account's choice, or the 48h default).
	HibernateAfterSeconds int64 `json:"hibernate_after_seconds"`
	// Effective default image name for agents deployed without --image.
	DefaultImage string `json:"default_image"`
}

func (c *Client) Account(ctx context.Context) (*Account, error) {
	out := &Account{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/account", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}
