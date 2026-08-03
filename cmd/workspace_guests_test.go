package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

const testGuestList = `{"guests":[{"email":"alice@example.com","agents":[
	{"id":"agent-0001","slug":"agent-000001","name":"scout"}
],"invited_at":"2026-08-01T10:00:00Z"}]}`

// Invite a guest, addressing the workspace by name and the agent by slug.
func TestWorkspaceGuestsAddAndList(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "POST /v1/workspaces/" + testWorkspaceID + "/guests":
			var body struct {
				Email     string   `json:"email"`
				AgentRefs []string `json:"agent_refs"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding invite body: %v", err)
			}
			if body.Email != "alice@example.com" || len(body.AgentRefs) != 1 || body.AgentRefs[0] != "agent-000001" {
				t.Errorf("invite body = %+v", body)
			}
			_, _ = w.Write([]byte(testGuestList))
		case "GET /v1/workspaces/" + testWorkspaceID + "/guests":
			_, _ = w.Write([]byte(testGuestList))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "workspace", "guests", "add", "research", "alice@example.com", "agent-000001")
	if got.err != nil {
		t.Fatalf("add: %v", got.err)
	}
	mustContain(t, got.stdout, "✓ granted alice@example.com chat access to agent-000001", "stdout")
	// tabwriter pads the header with spaces, so match the columns separately.
	mustContain(t, got.stdout, "EMAIL", "stdout")
	mustContain(t, got.stdout, "INVITED", "stdout")
	mustContain(t, got.stdout, "scout", "stdout")

	got = runCLI(t, "", "workspace", "guests", "research")
	if got.err != nil {
		t.Fatalf("list: %v", got.err)
	}
	mustContain(t, got.stdout, "alice@example.com", "stdout")
}

// Removing everything prompts; "n" aborts without a DELETE, --yes goes through.
func TestWorkspaceGuestsRemoveAllConfirms(t *testing.T) {
	deleted := false
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "DELETE /v1/workspaces/" + testWorkspaceID + "/guests/alice@example.com":
			if r.URL.Query().Get("agent_ref") != "" {
				t.Errorf("remove-all must not send agent_ref, got %q", r.URL.Query().Get("agent_ref"))
			}
			deleted = true
			_, _ = w.Write([]byte(`{"guests":[]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "n\n", "workspace", "guests", "remove", "research", "alice@example.com")
	if got.err != nil {
		t.Fatalf("aborted remove: %v", got.err)
	}
	mustContain(t, got.stdout, "Aborted.", "stdout")
	if deleted {
		t.Fatal("answering n must not delete")
	}

	got = runCLI(t, "", "workspace", "guests", "remove", "research", "alice@example.com", "--yes")
	if got.err != nil {
		t.Fatalf("remove: %v", got.err)
	}
	if !deleted {
		t.Fatal("expected the DELETE to have happened")
	}
	mustContain(t, got.stdout, "✓ revoked all of alice@example.com's access in research", "stdout")
	mustContain(t, got.stderr, "No guests in research", "stderr")
}

// Removing one agent's grant needs no confirmation and passes agent_ref.
func TestWorkspaceGuestsRemoveOneAgent(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "DELETE /v1/workspaces/" + testWorkspaceID + "/guests/alice@example.com":
			if got := r.URL.Query().Get("agent_ref"); got != "scout" {
				t.Errorf("agent_ref = %q, want scout", got)
			}
			_, _ = w.Write([]byte(`{"guests":[]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "workspace", "guests", "remove", "research", "alice@example.com", "scout")
	if got.err != nil {
		t.Fatalf("remove: %v", got.err)
	}
	mustContain(t, got.stdout, "✓ revoked alice@example.com's access to scout", "stdout")
}

// A backend rejection (unknown agent) surfaces the API detail.
func TestWorkspaceGuestsAddUnknownAgentFails(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "POST /v1/workspaces/" + testWorkspaceID + "/guests":
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"detail":"agent not found: nope"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "", "workspace", "guests", "add", "research", "alice@example.com", "nope")
	if got.err == nil {
		t.Fatal("want an error when the backend rejects the agent ref")
	}
	mustContain(t, got.err.Error(), "agent not found: nope", "error")
}
