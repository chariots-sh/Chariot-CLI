package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// Workspace is one grouping of the account's agents — mirrors
// route/workspaces.py::WorkspaceSummaryModel.
type Workspace struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	AgentCount int       `json:"agent_count"`
	CreatedAt  time.Time `json:"created_at"`
}

// WorkspaceMember is one agent in a workspace. Activity ("idle"/"working") is
// turn-level readiness, orthogonal to the lifecycle State.
type WorkspaceMember struct {
	ID       string  `json:"id"`
	Slug     string  `json:"slug"`
	Name     *string `json:"name"` // owner-chosen alias; nil = never named
	State    string  `json:"state"`
	Activity string  `json:"activity"`
}

type WorkspaceDetail struct {
	ID        string            `json:"id"`
	Name      string            `json:"name"`
	CreatedAt time.Time         `json:"created_at"`
	Agents    []WorkspaceMember `json:"agents"`
}

// ChatLine is one line of a workspace thread. Sender is "user" or "agent";
// AgentID is nil only on a broadcast line addressed to every member.
type ChatLine struct {
	ID        int64     `json:"id"`
	Sender    string    `json:"sender"`
	AgentID   *string   `json:"agent_id"`
	Broadcast bool      `json:"broadcast"`
	Message   string    `json:"message"`
	CreatedAt time.Time `json:"created_at"`
}

type ChatPage struct {
	Messages   []ChatLine `json:"messages"`
	NextCursor int64      `json:"next_cursor"`
}

// AgentSkillRow is one member's skills in the workspace skills matrix.
type AgentSkillRow struct {
	ID     string   `json:"id"`
	Slug   string   `json:"slug"`
	Name   *string  `json:"name"`
	State  string   `json:"state"`
	Skills []string `json:"skills"`
}

// SkillCoverage is one skill's reach across the workspace: how many members
// hold it, and which slugs are missing it.
type SkillCoverage struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Holders     int      `json:"holders"`
	Total       int      `json:"total"`
	Missing     []string `json:"missing"`
}

type WorkspaceSkills struct {
	WorkspaceID string          `json:"workspace_id"`
	Name        string          `json:"name"`
	Agents      []AgentSkillRow `json:"agents"`
	Coverage    []SkillCoverage `json:"coverage"`
}

// Document is one shared document's metadata. Author is "user" for documents
// you wrote, else the authoring agent's alias.
type Document struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	Author       string    `json:"author"`
	ContentChars int       `json:"content_chars"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// DocumentDetail is a document with its body.
type DocumentDetail struct {
	Document
	Content string `json:"content"`
}

// CrosstalkLine is one agent-to-agent message inside a workspace. Status is
// the delivery outcome ("delivered", "failed", …).
type CrosstalkLine struct {
	ID        int64     `json:"id"`
	FromAgent string    `json:"from_agent"`
	ToAgent   string    `json:"to_agent"`
	Message   string    `json:"message"`
	Status    string    `json:"status"`
	Detail    *string   `json:"detail"`
	CreatedAt time.Time `json:"created_at"`
}

func (c *Client) ListWorkspaces(ctx context.Context) ([]Workspace, error) {
	out := struct {
		Workspaces []Workspace `json:"workspaces"`
	}{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/workspaces", nil, &out); err != nil {
		return nil, err
	}
	return out.Workspaces, nil
}

// CreateWorkspace makes an empty workspace. Names are DNS-label shaped
// (a-z, 0-9, '-') and unique per account.
func (c *Client) CreateWorkspace(ctx context.Context, name string) (*Workspace, error) {
	out := &Workspace{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/workspaces", map[string]string{"name": name}, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetWorkspace reads one workspace and its members. id is the workspace UUID —
// the backend's workspace paths take ids, not names (see cmd's resolver).
func (c *Client) GetWorkspace(ctx context.Context, id string) (*WorkspaceDetail, error) {
	out := &WorkspaceDetail{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/workspaces/"+url.PathEscape(id), nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteWorkspace removes the workspace, its membership and its chat threads.
// The member agents themselves keep running (and keep their skills).
func (c *Client) DeleteWorkspace(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/workspaces/"+url.PathEscape(id), nil, nil)
	return err
}

// AddWorkspaceAgents adds agents by id, slug, or name (idempotent). Joining
// grants the workspace skills — shared documents and crosstalk.
func (c *Client) AddWorkspaceAgents(ctx context.Context, id string, refs []string) (*WorkspaceDetail, error) {
	out := &WorkspaceDetail{}
	body := map[string][]string{"refs": refs}
	if _, err := c.do(ctx, http.MethodPost, "/v1/workspaces/"+url.PathEscape(id)+"/agents", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// RemoveWorkspaceAgent drops one member (its chat lines stay). The agent keeps
// its skills — they are its own grants, revocable from `chariot skills`.
func (c *Client) RemoveWorkspaceAgent(ctx context.Context, id, ref string) (*WorkspaceDetail, error) {
	out := &WorkspaceDetail{}
	path := "/v1/workspaces/" + url.PathEscape(id) + "/agents/" + url.PathEscape(ref)
	if _, err := c.do(ctx, http.MethodDelete, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// WorkspaceChat pages one thread, oldest first: agentRef selects that member's
// 1:1 thread, an empty agentRef the broadcast thread. Pass the returned
// NextCursor back as after.
func (c *Client) WorkspaceChat(ctx context.Context, id, agentRef string, after int64, limit int) (*ChatPage, error) {
	query := url.Values{}
	query.Set("after", fmt.Sprintf("%d", after))
	query.Set("limit", fmt.Sprintf("%d", limit))
	if agentRef != "" {
		query.Set("agent_ref", agentRef)
	}
	out := &ChatPage{}
	path := "/v1/workspaces/" + url.PathEscape(id) + "/chat?" + query.Encode()
	if _, err := c.do(ctx, http.MethodGet, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// SendWorkspaceChat posts into a thread: with agentRef a 1:1 message, without
// it a broadcast to every member. Delivery is fire-and-forget — the returned
// line is the stored user message; replies arrive in the thread.
func (c *Client) SendWorkspaceChat(ctx context.Context, id, agentRef, message string) (*ChatLine, error) {
	body := map[string]any{"message": message}
	if agentRef != "" {
		body["agent_ref"] = agentRef
	}
	out := struct {
		UserMessage ChatLine `json:"user_message"`
	}{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/workspaces/"+url.PathEscape(id)+"/chat", body, &out); err != nil {
		return nil, err
	}
	return &out.UserMessage, nil
}

// SkillCatalog lists every grantable skill, served from the backend's tool
// registry so it can never drift from what agents can actually be given.
func (c *Client) SkillCatalog(ctx context.Context) ([]SkillCatalogEntry, error) {
	out := struct {
		Skills []SkillCatalogEntry `json:"skills"`
	}{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/skills", nil, &out); err != nil {
		return nil, err
	}
	return out.Skills, nil
}

// SkillCatalogEntry is one grantable skill and what it does.
type SkillCatalogEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (c *Client) GetWorkspaceSkills(ctx context.Context, id string) (*WorkspaceSkills, error) {
	out := &WorkspaceSkills{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/workspaces/"+url.PathEscape(id)+"/skills", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// EnableWorkspaceSkill grants one skill to every member (idempotent).
func (c *Client) EnableWorkspaceSkill(ctx context.Context, id, skill string) (*WorkspaceSkills, error) {
	out := &WorkspaceSkills{}
	path := "/v1/workspaces/" + url.PathEscape(id) + "/skills/" + url.PathEscape(skill)
	if _, err := c.do(ctx, http.MethodPost, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DisableWorkspaceSkill revokes one skill from every member (idempotent),
// scoped to this workspace's members.
func (c *Client) DisableWorkspaceSkill(ctx context.Context, id, skill string) (*WorkspaceSkills, error) {
	out := &WorkspaceSkills{}
	path := "/v1/workspaces/" + url.PathEscape(id) + "/skills/" + url.PathEscape(skill)
	if _, err := c.do(ctx, http.MethodDelete, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListWorkspaceDocuments(ctx context.Context, id string) ([]Document, error) {
	out := struct {
		Documents []Document `json:"documents"`
	}{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/workspaces/"+url.PathEscape(id)+"/documents", nil, &out); err != nil {
		return nil, err
	}
	return out.Documents, nil
}

// GetWorkspaceDocument reads one document by id or title.
func (c *Client) GetWorkspaceDocument(ctx context.Context, id, docRef string) (*DocumentDetail, error) {
	out := &DocumentDetail{}
	path := "/v1/workspaces/" + url.PathEscape(id) + "/documents/" + url.PathEscape(docRef)
	if _, err := c.do(ctx, http.MethodGet, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// CreateWorkspaceDocument adds a document every member agent can read, append
// to, and rewrite.
func (c *Client) CreateWorkspaceDocument(ctx context.Context, id, title, content string) (*DocumentDetail, error) {
	out := &DocumentDetail{}
	body := map[string]string{"title": title, "content": content}
	if _, err := c.do(ctx, http.MethodPost, "/v1/workspaces/"+url.PathEscape(id)+"/documents", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpdateWorkspaceDocument replaces a document's whole body.
func (c *Client) UpdateWorkspaceDocument(ctx context.Context, id, docRef, content string) (*DocumentDetail, error) {
	out := &DocumentDetail{}
	path := "/v1/workspaces/" + url.PathEscape(id) + "/documents/" + url.PathEscape(docRef)
	if _, err := c.do(ctx, http.MethodPut, path, map[string]string{"content": content}, out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) DeleteWorkspaceDocument(ctx context.Context, id, docRef string) error {
	path := "/v1/workspaces/" + url.PathEscape(id) + "/documents/" + url.PathEscape(docRef)
	_, err := c.do(ctx, http.MethodDelete, path, nil, nil)
	return err
}

// WorkspaceCrosstalk reads what the workspace's agents have said to each other.
func (c *Client) WorkspaceCrosstalk(ctx context.Context, id string) ([]CrosstalkLine, error) {
	out := struct {
		Messages []CrosstalkLine `json:"messages"`
	}{}
	path := "/v1/workspaces/" + url.PathEscape(id) + "/agent-messages"
	if _, err := c.do(ctx, http.MethodGet, path, nil, &out); err != nil {
		return nil, err
	}
	return out.Messages, nil
}
