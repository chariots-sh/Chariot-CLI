package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

const testWorkspaceID = "11111111-1111-4111-8111-111111111111"

// listWorkspaces is the one call every workspace command makes first: it turns
// the name the user typed into the id the API's paths take.
func listWorkspaces(w http.ResponseWriter) {
	_, _ = w.Write([]byte(`{"workspaces":[{"id":"` + testWorkspaceID + `","name":"research","agent_count":2,"created_at":"2026-07-28T10:00:00Z"}]}`))
}

const testWorkspaceDetail = `{"id":"` + testWorkspaceID + `","name":"research","created_at":"2026-07-28T10:00:00Z","agents":[
	{"id":"agent-0001","slug":"agent-000001","name":"scout","state":"active","activity":"idle"}
]}`

// Create → add a member → show, addressing the workspace by name throughout.
func TestWorkspaceCreateAddShow(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "POST /v1/workspaces":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["name"] != "research" {
				t.Errorf("create body = %v (err %v)", body, err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"` + testWorkspaceID + `","name":"research","agent_count":0,"created_at":"2026-07-28T10:00:00Z"}`))
		case "POST /v1/workspaces/" + testWorkspaceID + "/agents":
			var body map[string][]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding add body: %v", err)
			}
			if len(body["refs"]) != 2 {
				t.Errorf("refs = %v, want both agents", body["refs"])
			}
			_, _ = w.Write([]byte(testWorkspaceDetail))
		case "GET /v1/workspaces/" + testWorkspaceID:
			_, _ = w.Write([]byte(testWorkspaceDetail))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "workspace", "create", "research")
	if got.err != nil {
		t.Fatalf("create: %v", got.err)
	}
	mustContain(t, got.stdout, "✓ created workspace research", "stdout")

	got = runCLI(t, "", "workspace", "add", "research", "agent-000001", "agent-000002")
	if got.err != nil {
		t.Fatalf("add: %v", got.err)
	}
	mustContain(t, got.stdout, "research now has 1 member(s)", "stdout")

	got = runCLI(t, "", "workspace", "show", "research")
	if got.err != nil {
		t.Fatalf("show: %v", got.err)
	}
	mustContain(t, got.stdout, "agent-000001", "stdout")
	mustContain(t, got.stdout, "scout", "stdout")
}

func TestWorkspaceUnknownNameIsARealError(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/workspaces" {
			listWorkspaces(w)
			return
		}
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	})

	got := runCLI(t, "", "workspace", "show", "marketing")
	if got.err == nil {
		t.Fatal("want an error for a workspace that doesn't exist")
	}
	mustContain(t, got.err.Error(), "workspace not found: marketing", "error")
}

// Sending into the broadcast thread, then printing the member's reply.
func TestWorkspaceChatSendsAndPrintsReply(t *testing.T) {
	sent := false
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "GET /v1/workspaces/" + testWorkspaceID:
			_, _ = w.Write([]byte(testWorkspaceDetail))
		case "POST /v1/workspaces/" + testWorkspaceID + "/chat":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding chat body: %v", err)
			}
			if _, ok := body["agent_ref"]; ok {
				t.Errorf("body carried agent_ref %v; no --agent means broadcast", body["agent_ref"])
			}
			sent = true
			w.WriteHeader(http.StatusAccepted)
			_, _ = w.Write([]byte(`{"user_message":{"id":5,"sender":"user","agent_id":null,"broadcast":true,"message":"who has capacity?","created_at":"2026-07-28T10:00:00Z"}}`))
		case "GET /v1/workspaces/" + testWorkspaceID + "/chat":
			// Honour the id cursor like the backend does: the reply is served
			// once, to the poll that follows the send.
			if !sent || r.URL.Query().Get("after") != "5" {
				_, _ = w.Write([]byte(`{"messages":[],"next_cursor":` + r.URL.Query().Get("after") + `}`))
				return
			}
			_, _ = w.Write([]byte(`{"messages":[{"id":6,"sender":"agent","agent_id":"agent-0001","broadcast":true,"message":"I do","created_at":"2026-07-28T10:00:05Z"}],"next_cursor":6}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "workspace", "chat", "research", "who has capacity?")
	if got.err != nil {
		t.Fatalf("chat: %v", got.err)
	}
	mustContain(t, got.stdout, "scout", "stdout") // named member, not its raw id
	mustContain(t, got.stdout, "I do", "stdout")
}

func TestWorkspaceDocsWriteFromStdin(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "GET /v1/workspaces/" + testWorkspaceID + "/documents/brief":
			w.WriteHeader(http.StatusNotFound) // no such document yet → create
			_, _ = w.Write([]byte(`{"detail":"document not found: brief"}`))
		case "POST /v1/workspaces/" + testWorkspaceID + "/documents":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding document body: %v", err)
			}
			if body["title"] != "brief" || body["content"] != "focus on margins\n" {
				t.Errorf("document body = %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"doc-1","title":"brief","author":"user","author_agent_id":null,"content_chars":17,"content":"focus on margins\n","created_at":"2026-07-28T10:00:00Z","updated_at":"2026-07-28T10:00:00Z"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "focus on margins\n", "workspace", "docs", "write", "research", "brief")
	if got.err != nil {
		t.Fatalf("docs write: %v", got.err)
	}
	mustContain(t, got.stdout, `✓ wrote "brief"`, "stdout")
}

// Only a 404 means "no such document yet". Any other lookup failure is the
// real error and must not be papered over by creating a second document.
func TestWorkspaceDocsWriteSurfacesLookupFailure(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "GET /v1/workspaces/" + testWorkspaceID + "/documents/brief":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"detail":"database unavailable"}`))
		default:
			t.Errorf("unexpected request: %s %s — a failed lookup must not create", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "focus on margins\n", "workspace", "docs", "write", "research", "brief")
	if got.err == nil {
		t.Fatal("want the lookup failure surfaced")
	}
	mustContain(t, got.err.Error(), "database unavailable", "error")
}
