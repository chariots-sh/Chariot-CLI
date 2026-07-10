package api

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// FleetPackItem is one image line of a fleet pack: an image (builtin or the
// owner's custom name) and how many agents run it.
type FleetPackItem struct {
	ImageName string `json:"image_name"`
	Kind      string `json:"kind"` // builtin | custom
	Count     int    `json:"count"`
	// The tier/fee the item runs at right now; nil while unresolvable (owner
	// mid re-push, builtin not published yet) — see Available.
	PodSize         *string  `json:"pod_size"`
	DailyFeeDollars *float64 `json:"daily_fee_dollars"`
	Available       bool     `json:"available"`
}

// FleetPack is one of the account's own packs — a named, shareable fleet
// recipe of images with counts plus an optional setup skill.
type FleetPack struct {
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Items       []FleetPackItem `json:"items"`
	TotalAgents int             `json:"total_agents"`
	HasSkill    bool            `json:"has_skill"`
	Published   bool            `json:"published"`
	PublishedAt *time.Time      `json:"published_at"`
	DeployCount int             `json:"deploy_count"`
}

// FleetPacks is the account's own packs (`chariot fleet list`).
type FleetPacks struct {
	Packs []FleetPack `json:"packs"`
}

// PublicFleetPack is a published pack as the public catalog shows it —
// always fully deployable (packs with an unresolvable image are hidden).
type PublicFleetPack struct {
	OwnerEmail  string          `json:"owner_email"`
	Name        string          `json:"name"`
	Description *string         `json:"description"`
	Items       []FleetPackItem `json:"items"`
	TotalAgents int             `json:"total_agents"`
	// Per-day cost of the whole pack once every agent is active.
	TotalDailyFeeDollars float64   `json:"total_daily_fee_dollars"`
	HasSkill             bool      `json:"has_skill"`
	PublishedAt          time.Time `json:"published_at"`
	DeployCount          int       `json:"deploy_count"`
}

// PublicFleetPacks is one page of the public pack catalog. A page may run
// short of the requested limit (packs whose owner is mid re-push are hidden)
// — follow NextCursor until nil.
type PublicFleetPacks struct {
	Packs      []PublicFleetPack `json:"packs"`
	NextCursor *string           `json:"next_cursor"`
}

// FleetSkill is a pack's setup skill — a markdown guide for the humans (and
// their coding agents) deploying the pack, not for the agent pods.
type FleetSkill struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// FleetPackItemSpec is one requested pack line for CreateFleetPack.
type FleetPackItemSpec struct {
	ImageName string `json:"image_name"`
	Count     int    `json:"count"`
}

// FleetDeployGroup is the agents one pack item produced.
type FleetDeployGroup struct {
	ImageName string `json:"image_name"`
	// The name stamped on the agents — the share alias when it had to differ
	// from ImageName (you already used the name).
	DeployName string   `json:"deploy_name"`
	Count      int      `json:"count"`
	Slugs      []string `json:"slugs"`
	PodSize    string   `json:"pod_size"`
}

// FleetDeployResult is the outcome of `chariot deploy-fleet`.
type FleetDeployResult struct {
	TokenSeed     string             `json:"token_seed"` // shown once
	Namespace     string             `json:"namespace"`
	Created       int                `json:"created"`
	Total         int                `json:"total"`
	AgentsByState map[string]int     `json:"agents_by_state"`
	Model         string             `json:"model"`
	PackName      string             `json:"pack_name"`
	OwnerEmail    *string            `json:"owner_email"` // nil for own packs / templates
	Groups        []FleetDeployGroup `json:"groups"`
	// The pack's setup skill, inline — nil when the pack ships without one.
	SkillContent *string `json:"skill_content"`
}

// CreateFleetPack creates the pack, or replaces an existing one's
// items/description wholesale (publication state and skill survive).
func (c *Client) CreateFleetPack(ctx context.Context, name, description string, items []FleetPackItemSpec) (*FleetPack, error) {
	body := map[string]any{"name": name, "items": items}
	if description != "" {
		body["description"] = description
	}
	out := &FleetPack{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/fleets", body, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListFleetPacks fetches the account's own packs.
func (c *Client) ListFleetPacks(ctx context.Context) (*FleetPacks, error) {
	out := &FleetPacks{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/fleets", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetFleetPack fetches one of the account's own packs.
func (c *Client) GetFleetPack(ctx context.Context, name string) (*FleetPack, error) {
	out := &FleetPack{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/fleets/"+name, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeleteFleetPack deletes a pack. Agents and shares created by past deploys
// are not touched.
func (c *Client) DeleteFleetPack(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/fleets/"+name, nil, nil)
	return err
}

// PublishFleetPack lists the pack in the public catalog — a standing offer
// any account can deploy. Publishing implies consent to share the pack's
// custom images with accounts that deploy it.
func (c *Client) PublishFleetPack(ctx context.Context, name string) (*FleetPack, error) {
	out := &FleetPack{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/fleets/"+name+"/publish", struct{}{}, out); err != nil {
		return nil, err
	}
	return out, nil
}

// UnpublishFleetPack stops discovery and new deploys. Fleets and shares
// already created from the pack keep working.
func (c *Client) UnpublishFleetPack(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/fleets/"+name+"/publish", nil, nil)
	return err
}

// SetFleetSkill attaches (or replaces) the pack's setup skill.
func (c *Client) SetFleetSkill(ctx context.Context, name, content string) (*FleetSkill, error) {
	out := &FleetSkill{}
	if _, err := c.do(ctx, http.MethodPut, "/v1/fleets/"+name+"/skill", map[string]any{"content": content}, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetFleetSkill fetches the setup skill on one of YOUR OWN packs.
func (c *Client) GetFleetSkill(ctx context.Context, name string) (*FleetSkill, error) {
	out := &FleetSkill{}
	if _, err := c.do(ctx, http.MethodGet, "/v1/fleets/"+name+"/skill", nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// ClearFleetSkill removes the pack's setup skill.
func (c *Client) ClearFleetSkill(ctx context.Context, name string) error {
	_, err := c.do(ctx, http.MethodDelete, "/v1/fleets/"+name+"/skill", nil, nil)
	return err
}

// BrowseFleetPacks fetches one page of the public pack catalog.
func (c *Client) BrowseFleetPacks(ctx context.Context, cursor string, limit int) (*PublicFleetPacks, error) {
	out := &PublicFleetPacks{}
	path := fmt.Sprintf("/v1/fleets/public?limit=%d&cursor=%s", limit, url.QueryEscape(cursor))
	if _, err := c.do(ctx, http.MethodGet, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPublicFleetPack fetches one published pack — what the deploy-fleet
// confirmation screen shows (items, tiers, the daily fee being consented to).
func (c *Client) GetPublicFleetPack(ctx context.Context, ownerEmail, name string) (*PublicFleetPack, error) {
	out := &PublicFleetPack{}
	path := "/v1/fleets/public/pack?owner_email=" + url.QueryEscape(ownerEmail) + "&name=" + url.QueryEscape(name)
	if _, err := c.do(ctx, http.MethodGet, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// GetPublicFleetSkill fetches a published pack's setup skill — readable
// before deploying.
func (c *Client) GetPublicFleetSkill(ctx context.Context, ownerEmail, name string) (*FleetSkill, error) {
	out := &FleetSkill{}
	path := "/v1/fleets/public/skill?owner_email=" + url.QueryEscape(ownerEmail) + "&name=" + url.QueryEscape(name)
	if _, err := c.do(ctx, http.MethodGet, path, nil, out); err != nil {
		return nil, err
	}
	return out, nil
}

// DeployFleetPack deploys a pack in one shot: every item's agents, one token
// seed. name alone deploys your own pack (or a builtin template as a pack of
// one); with ownerEmail it deploys that account's published pack, consenting
// to its custom images as accepted shares.
func (c *Client) DeployFleetPack(ctx context.Context, name, ownerEmail, endpoint, model string) (*FleetDeployResult, error) {
	body := map[string]any{"name": name}
	if ownerEmail != "" {
		body["owner_email"] = ownerEmail
	}
	if endpoint != "" {
		body["endpoint"] = endpoint
	}
	if model != "" {
		body["model"] = model
	}
	out := &FleetDeployResult{}
	if _, err := c.do(ctx, http.MethodPost, "/v1/fleets/deploy", body, out); err != nil {
		return nil, err
	}
	return out, nil
}
