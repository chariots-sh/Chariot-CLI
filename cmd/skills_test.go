package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

// One round-trip per verb: show, grant, revoke — including the
// granted-vs-effective split that explains membership-implied skills.
func TestSkillsShowAddRemove(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "GET /v1/agents/agent-0001/skills":
			_, _ = w.Write([]byte(`{"granted":[],"effective":["docs"]}`))
		case "POST /v1/agents/agent-0001/skills":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["skill"] != "docs" {
				t.Errorf("grant body = %v (err %v)", body, err)
			}
			_, _ = w.Write([]byte(`{"granted":["docs"],"effective":["docs"]}`))
		case "DELETE /v1/agents/agent-0001/skills/docs":
			_, _ = w.Write([]byte(`{"granted":[],"effective":[]}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	})

	got := runCLI(t, "", "skills", "--agent", "agent-0001")
	if got.err != nil {
		t.Fatalf("skills: %v", got.err)
	}
	mustContain(t, got.stdout, "granted    : none", "stdout")
	mustContain(t, got.stdout, "effective  : docs", "stdout")

	got = runCLI(t, "", "skills", "add", "docs", "--agent", "agent-0001")
	if got.err != nil {
		t.Fatalf("skills add: %v", got.err)
	}
	mustContain(t, got.stdout, "✓ granted docs", "stdout")
	mustContain(t, got.stdout, "granted    : docs", "stdout")

	got = runCLI(t, "", "skills", "remove", "docs", "--agent", "agent-0001")
	if got.err != nil {
		t.Fatalf("skills remove: %v", got.err)
	}
	mustContain(t, got.stdout, "✓ revoked docs", "stdout")
	mustContain(t, got.stdout, "effective  : none", "stdout")
}

func TestSkillsRequiresAgentFlag(t *testing.T) {
	logout(t)
	got := runCLI(t, "", "skills")
	if got.err == nil {
		t.Fatal("want an error without --agent")
	}
	mustContain(t, got.err.Error(), "--agent is required", "error")
}
