package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Goal is one standing objective a workspace agent works toward on its own —
// mirrors route/goals.py::GoalModel.
type Goal struct {
	ID          string  `json:"id"`
	WorkspaceID string  `json:"workspace_id"`
	AgentID     string  `json:"agent_id"`
	AgentSlug   string  `json:"agent_slug"`
	AgentName   *string `json:"agent_name"` // owner-chosen alias; nil = unnamed
	Objective   string  `json:"objective"`
	// Status is the goal's lifecycle state:
	// "active" | "paused" | "blocked" | "complete" | "canceled".
	Status           string      `json:"status"`
	Plan             *string     `json:"plan"`
	LatestSteering   *string     `json:"latest_steering"`
	BlockedReason    *string     `json:"blocked_reason"`
	CompletedSummary *string     `json:"completed_summary"`
	Evidence         []string    `json:"evidence"`
	Version          int         `json:"version"`
	NextWakeAt       *time.Time  `json:"next_wake_at"`
	TurnsStarted     int         `json:"turns_started"`
	CreatedAt        time.Time   `json:"created_at"`
	UpdatedAt        time.Time   `json:"updated_at"`
	RecentEvents     []GoalEvent `json:"recent_events"`
}

// GoalEvent is one line of a goal's audit trail — mirrors
// route/goals.py::GoalEvent. Actor is "user", "agent", or "system"; Kind names
// what happened (created, progress, steering, completed, blocked,
// turn_dispatched, turn_failed, rate_limited, …).
type GoalEvent struct {
	ID        int64          `json:"id"`
	GoalID    string         `json:"goal_id"`
	AgentID   string         `json:"agent_id"`
	Actor     string         `json:"actor"`
	Kind      string         `json:"kind"`
	Message   string         `json:"message"`
	Detail    map[string]any `json:"detail"`
	CreatedAt time.Time      `json:"created_at"`
}

// goalPath is the goal root for one member agent. wsID is the workspace UUID
// (the cmd layer resolves names); agentRef passes through verbatim — id, slug,
// or name, the backend resolves it.
func goalPath(wsID, agentRef string) string {
	return "/v1/workspaces/" + url.PathEscape(wsID) + "/agents/" + url.PathEscape(agentRef) + "/goal"
}

// SetGoal gives the agent a standing objective. With replace=false the backend
// answers 409 when an open goal already exists; replace=true supersedes it.
func (c *Client) SetGoal(ctx context.Context, wsID, agentRef, objective string, replace bool) (*Goal, error) {
	out := &Goal{}
	body := map[string]any{"objective": objective, "replace": replace}
	if _, err := c.do(ctx, http.MethodPost, goalPath(wsID, agentRef), body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetGoal reads the agent's current (most recent) goal. 404 means the agent
// never had one.
func (c *Client) GetGoal(ctx context.Context, wsID, agentRef string) (*Goal, error) {
	out := &Goal{}
	if _, err := c.do(ctx, http.MethodGet, goalPath(wsID, agentRef), nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// transitionGoal drives one lifecycle action (pause/resume/cancel).
// expectedVersion, when set, makes the backend 409 if the goal moved since it
// was read — nil skips the check.
func (c *Client) transitionGoal(ctx context.Context, wsID, agentRef, action string, expectedVersion *int) (*Goal, error) {
	out := &Goal{}
	body := map[string]any{"expected_version": expectedVersion}
	if _, err := c.do(ctx, http.MethodPost, goalPath(wsID, agentRef)+"/"+action, body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// PauseGoal stops the goal's autonomous turns until it is resumed.
func (c *Client) PauseGoal(ctx context.Context, wsID, agentRef string, expectedVersion *int) (*Goal, error) {
	return c.transitionGoal(ctx, wsID, agentRef, "pause", expectedVersion)
}

// ResumeGoal restarts a paused goal.
func (c *Client) ResumeGoal(ctx context.Context, wsID, agentRef string, expectedVersion *int) (*Goal, error) {
	return c.transitionGoal(ctx, wsID, agentRef, "resume", expectedVersion)
}

// CancelGoal ends the goal for good (idempotent — canceling a canceled goal
// returns it unchanged).
func (c *Client) CancelGoal(ctx context.Context, wsID, agentRef string, expectedVersion *int) (*Goal, error) {
	return c.transitionGoal(ctx, wsID, agentRef, "cancel", expectedVersion)
}

// GoalHistory lists the agent's goals, newest first.
func (c *Client) GoalHistory(ctx context.Context, wsID, agentRef string, limit int) ([]Goal, error) {
	out := struct {
		Goals []Goal `json:"goals"`
	}{}
	path := fmt.Sprintf("%s/history?limit=%d", goalPath(wsID, agentRef), limit)
	if _, err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Goals, nil
}
