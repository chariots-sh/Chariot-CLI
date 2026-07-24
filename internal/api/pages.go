package api

import (
	"context"
	"fmt"
	"net/url"
)

// PublishedPage mirrors route/agent_pages.py::AgentPageModel — the one HTML page an
// agent has published, read live off its pod.
//
// HTML is UNTRUSTED: an LLM wrote it. Anything rendering it must use a
// sandboxed iframe without allow-same-origin, never inject it into a trusted
// document, and never re-serve it as text/html from an origin holding a
// session token.
type PublishedPage struct {
	Title    string `json:"title"`
	HTML     string `json:"html"`
	Agent    string `json:"agent"`
	ShareURL string `json:"share_url"`
}

// GetAgentPage fetches one agent's published page.
//
// A 404 comes back as an *APIError and means "nothing to show" — the agent
// never published, or it is asleep (an idle agent is scaled to zero, and the
// page lives on its pod). Callers that want to distinguish those cannot: the
// backend deliberately does not.
func (c *Client) GetAgentPage(ctx context.Context, ref string) (*PublishedPage, error) {
	var out PublishedPage
	path := fmt.Sprintf("/v1/agents/%s/page", url.PathEscape(ref))
	if _, err := c.do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
