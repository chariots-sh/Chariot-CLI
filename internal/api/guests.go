package api

import (
	"context"
	"net/http"
	"net/url"
	"time"
)

// GrantedAgent is one agent a guest may chat with.
type GrantedAgent struct {
	ID   string  `json:"id"`
	Slug string  `json:"slug"`
	Name *string `json:"name"`
}

// WorkspaceGuest is one invited guest's standing in a workspace: who they
// are and which member agents they may chat with.
type WorkspaceGuest struct {
	Email     string         `json:"email"`
	Agents    []GrantedAgent `json:"agents"`
	InvitedAt time.Time      `json:"invited_at"`
}

// ListWorkspaceGuests returns the workspace's invited guests with their
// granted agents.
func (c *Client) ListWorkspaceGuests(ctx context.Context, id string) ([]WorkspaceGuest, error) {
	out := struct {
		Guests []WorkspaceGuest `json:"guests"`
	}{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/workspaces/"+url.PathEscape(id)+"/guests", nil, &out); err != nil {
		return nil, err
	}
	return out.Guests, nil
}

// AddWorkspaceGuest grants a guest chat access to member agents (inviting
// them by email on their first grant in the workspace). Idempotent for
// already-granted agents.
func (c *Client) AddWorkspaceGuest(ctx context.Context, id, email string, agentRefs []string) ([]WorkspaceGuest, error) {
	out := struct {
		Guests []WorkspaceGuest `json:"guests"`
	}{}
	body := map[string]any{"email": email, "agent_refs": agentRefs}
	if _, err := c.do(ctx, http.MethodPost, "/v1/workspaces/"+url.PathEscape(id)+"/guests", body, &out); err != nil {
		return nil, err
	}
	return out.Guests, nil
}

// RemoveWorkspaceGuest revokes a guest's grants in the workspace — one
// agent's when agentRef is non-empty, all of them otherwise.
func (c *Client) RemoveWorkspaceGuest(ctx context.Context, id, email, agentRef string) ([]WorkspaceGuest, error) {
	out := struct {
		Guests []WorkspaceGuest `json:"guests"`
	}{}
	path := "/v1/workspaces/" + url.PathEscape(id) + "/guests/" + url.PathEscape(email)
	if agentRef != "" {
		path += "?agent_ref=" + url.QueryEscape(agentRef)
	}
	if _, err := c.do(ctx, http.MethodDelete, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Guests, nil
}
