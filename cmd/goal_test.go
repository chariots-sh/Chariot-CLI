package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

const goalBasePath = "/v1/workspaces/" + testWorkspaceID + "/agents/agent-000001/goal"

const testGoalJSON = `{
	"id": "goal-1",
	"workspace_id": "` + testWorkspaceID + `",
	"agent_id": "agent-0001",
	"agent_slug": "agent-000001",
	"agent_name": "scout",
	"objective": "ship the Q3 report",
	"status": "active",
	"plan": "gather filings, then draft",
	"latest_steering": null,
	"blocked_reason": null,
	"completed_summary": null,
	"evidence": [],
	"version": 3,
	"next_wake_at": null,
	"turns_started": 7,
	"created_at": "2026-07-28T10:00:00Z",
	"updated_at": "2026-07-28T11:00:00Z",
	"recent_events": [
		{"id": 1, "goal_id": "goal-1", "agent_id": "agent-0001", "actor": "user", "kind": "created",
		 "message": "goal created", "detail": {}, "created_at": "2026-07-28T10:00:00Z"}
	]
}`

func TestGoalSetHappyPath(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "POST " + goalBasePath:
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding set body: %v", err)
			}
			if body["objective"] != "ship the Q3 report" || body["replace"] != false {
				t.Errorf("set body = %v", body)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(testGoalJSON))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "goal", "set", "agent-000001", "ship the Q3 report", "--workspace", "research")
	if got.err != nil {
		t.Fatalf("goal set: %v", got.err)
	}
	mustContain(t, got.stdout, "✓ goal set", "stdout")
	mustContain(t, got.stdout, "ship the Q3 report", "stdout")
}

// A 409 without --replace must not clobber the open goal: the CLI surfaces the
// backend's detail and names both ways out.
func TestGoalSetConflictWithoutReplaceSuggestsFix(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "POST " + goalBasePath:
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"detail":"agent already has an open goal"}`))
		default:
			t.Errorf("unexpected request: %s %s — a plain 409 must not retry", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "goal", "set", "agent-000001", "new objective", "--workspace", "research")
	if got.err == nil {
		t.Fatal("want a non-zero exit on the goal conflict")
	}
	mustContain(t, got.err.Error(), "agent already has an open goal", "error")
	mustContain(t, got.stderr, "--replace", "stderr")
	mustContain(t, got.stderr, "chariot goal cancel", "stderr")
}

func TestGoalStatusRendering(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "GET " + goalBasePath:
			_, _ = w.Write([]byte(testGoalJSON))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "goal", "status", "agent-000001", "--workspace", "research")
	if got.err != nil {
		t.Fatalf("goal status: %v", got.err)
	}
	mustContain(t, got.stdout, "objective : ship the Q3 report", "stdout")
	mustContain(t, got.stdout, "status    : active", "stdout")
	mustContain(t, got.stdout, "agent     : scout", "stdout") // named member, not its raw slug
	mustContain(t, got.stdout, "plan      : gather filings, then draft", "stdout")
	mustContain(t, got.stdout, "steering  : -", "stdout")
	mustContain(t, got.stdout, "turns     : 7", "stdout")
	mustContain(t, got.stdout, "Recent events:", "stdout")
	mustContain(t, got.stdout, "user/created", "stdout")
	mustContain(t, got.stdout, "goal created", "stdout")
}

// Cancel reads the goal first and sends the version it saw, so a concurrent
// edit 409s instead of being silently canceled.
func TestGoalCancelSendsExpectedVersionFromPriorGet(t *testing.T) {
	fetched := false
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "GET " + goalBasePath:
			fetched = true
			_, _ = w.Write([]byte(testGoalJSON))
		case "POST " + goalBasePath + "/cancel":
			if !fetched {
				t.Error("cancel posted before the goal was read")
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatalf("decoding cancel body: %v", err)
			}
			if body["expected_version"] != float64(3) {
				t.Errorf("expected_version = %v, want 3 (from the prior GET)", body["expected_version"])
			}
			_, _ = w.Write([]byte(`{"id":"goal-1","workspace_id":"` + testWorkspaceID + `","agent_id":"agent-0001","agent_slug":"agent-000001","agent_name":"scout","objective":"ship the Q3 report","status":"canceled","plan":null,"latest_steering":null,"blocked_reason":null,"completed_summary":null,"evidence":[],"version":4,"next_wake_at":null,"turns_started":7,"created_at":"2026-07-28T10:00:00Z","updated_at":"2026-07-28T12:00:00Z","recent_events":[]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "goal", "cancel", "agent-000001", "--workspace", "research", "-y")
	if got.err != nil {
		t.Fatalf("goal cancel: %v", got.err)
	}
	mustContain(t, got.stdout, "✓ canceled", "stdout")
	mustContain(t, got.stdout, "status    : canceled", "stdout")
}

func TestGoalHistoryTable(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/workspaces":
			listWorkspaces(w)
		case "GET " + goalBasePath + "/history":
			if r.URL.Query().Get("limit") != "2" {
				t.Errorf("limit = %q, want 2", r.URL.Query().Get("limit"))
			}
			_, _ = w.Write([]byte(`{"goals":[` + testGoalJSON + `,
				{"id":"goal-0","workspace_id":"` + testWorkspaceID + `","agent_id":"agent-0001","agent_slug":"agent-000001","agent_name":"scout","objective":"stand up the data room","status":"complete","plan":null,"latest_steering":null,"blocked_reason":null,"completed_summary":"done","evidence":[],"version":9,"next_wake_at":null,"turns_started":12,"created_at":"2026-07-20T10:00:00Z","updated_at":"2026-07-21T10:00:00Z","recent_events":[]}]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "goal", "history", "agent-000001", "--workspace", "research", "--limit", "2")
	if got.err != nil {
		t.Fatalf("goal history: %v", got.err)
	}
	mustContain(t, got.stdout, "CREATED", "stdout")
	mustContain(t, got.stdout, "OBJECTIVE", "stdout")
	mustContain(t, got.stdout, "ship the Q3 report", "stdout")
	mustContain(t, got.stdout, "stand up the data room", "stdout")
	mustContain(t, got.stdout, "complete", "stdout")
}

// The workspace flag is validated before anything hits the network.
func TestGoalMissingWorkspaceRejected(t *testing.T) {
	logout(t)
	got := runCLI(t, "", "goal", "status", "agent-000001")
	if got.err == nil {
		t.Fatal("want an error when --workspace is missing")
	}
	mustContain(t, got.err.Error(), "--workspace is required", "error")
}
