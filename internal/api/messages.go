package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
)

// AgentStatus is one agent's control-plane lifecycle snapshot, read without
// waking it. Activity ("idle"/"working") is turn-level readiness.
type AgentStatus struct {
	AgentID  string  `json:"agent_id"`
	Slug     string  `json:"slug"`
	Name     *string `json:"name"`
	State    string  `json:"state"`
	Activity string  `json:"activity"`
}

// MessageAgent sends a message to one of the caller's agents (id, slug, or
// name), authenticating with the session token from `chariot login` — no
// token-seed needed. replyTo is the caller's correlation id, echoed back on the
// agent's reply so a poll can pick out this exchange; pass "" to omit it.
//
// The 202 only means the pod took delivery; the reply arrives asynchronously
// in the inbox (Replies) and on the account's webhook if one is configured.
func (c *Client) MessageAgent(ctx context.Context, ref, message, replyTo string) (*MessageAck, error) {
	body := map[string]string{"message": message}
	if replyTo != "" {
		body["reply_to"] = replyTo
	}
	out := &MessageAck{}
	path := "/v1/agents/" + url.PathEscape(ref) + "/messages"
	if _, err := c.do(ctx, http.MethodPost, path, body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Replies pages the account's reply inbox with the session token: replies with
// id > after, oldest first. Same endpoint as ListReplies, which authenticates
// with a token-seed instead.
func (c *Client) Replies(ctx context.Context, after int64, limit int) (*ReplyPage, error) {
	out := &ReplyPage{}
	path := fmt.Sprintf("/v1/replies?after=%d&limit=%d", after, limit)
	if _, err := c.do(ctx, http.MethodGet, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// AgentStatus reads one agent's lifecycle state without waking it.
func (c *Client) AgentStatus(ctx context.Context, ref string) (*AgentStatus, error) {
	out := &AgentStatus{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/agents/"+url.PathEscape(ref)+"/status", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}
