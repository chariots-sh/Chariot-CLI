package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const deployFleetResponse = `{"token_seed":"ts_secret","namespace":"cust-7",
	"created":6,"total":6,"agents_by_state":{"deactivated":6},
	"model":"anthropic/claude-opus-4.8","pack_name":"quant-firm","owner_email":"max@example.com",
	"groups":[{"image_name":"zeroclaw","deploy_name":"zeroclaw","count":5,
	           "slugs":["agent-000000","agent-000001","agent-000002","agent-000003","agent-000004"],"pod_size":"small"},
	          {"image_name":"brain","deploy_name":"max-brain","count":1,"slugs":["agent-000005"],"pod_size":"medium"}],
	"skill_content":null}`

const publicPackResponse = `{"owner_email":"max@example.com","name":"quant-firm","description":null,
	"items":[{"image_name":"zeroclaw","kind":"builtin","count":5,"pod_size":"small","daily_fee_dollars":1,"available":true},
	         {"image_name":"brain","kind":"custom","count":1,"pod_size":"medium","daily_fee_dollars":2,"available":true}],
	"total_agents":6,"total_daily_fee_dollars":7,"has_skill":true,
	"published_at":"2026-07-10T00:00:00Z","deploy_count":0}`

func TestDeployFleetAbortsOnNo(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/fleets/public/pack" {
			t.Errorf("only the confirmation fetch is expected, got %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(publicPackResponse))
	})

	got := runCLI(t, "n\n", "deploy-fleet", "quant-firm", "--from", "max@example.com")
	if got.err != nil {
		t.Fatalf("deploy-fleet: %v", got.err)
	}
	// The confirmation shows the money being consented to before asking.
	mustContain(t, got.stdout, "$7.00/day once fully active", "stdout")
	mustContain(t, got.stdout, "Aborted", "stdout")
}

func TestDeployFleetYesDeploysAndPrintsGroups(t *testing.T) {
	var body map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/fleets/deploy" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(deployFleetResponse))
	})

	got := runCLI(t, "", "deploy-fleet", "quant-firm", "--from", "max@example.com", "--yes")
	if got.err != nil {
		t.Fatalf("deploy-fleet: %v", got.err)
	}
	if body["name"] != "quant-firm" || body["owner_email"] != "max@example.com" {
		t.Errorf("body = %v", body)
	}
	mustContain(t, got.stdout, "6 agents live (total 6)", "stdout")
	mustContain(t, got.stdout, "agent-000000 … agent-000004", "stdout")
	// A share alias that differs from the pack-side name is surfaced.
	mustContain(t, got.stdout, "brain (as max-brain)", "stdout")
	mustContain(t, got.stdout, "token-seed  : ts_secret", "stdout")
	// No skill in the response: no menu.
	mustNotContain(t, got.stdout, "setup skill", "stdout")
}

func withSkill(t *testing.T) string {
	t.Helper()
	var res map[string]any
	if err := json.Unmarshal([]byte(deployFleetResponse), &res); err != nil {
		t.Fatal(err)
	}
	res["skill_content"] = "# Setup skill\nmessage agent-000005 first"
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestDeployFleetMenuDisplaysSkillThenQuits(t *testing.T) {
	response := withSkill(t)
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/fleets/public/pack" {
			_, _ = w.Write([]byte(publicPackResponse))
			return
		}
		_, _ = w.Write([]byte(response))
	})

	// No --yes: the confirm prompt and the menu must share one buffered
	// reader — a second bufio.Reader would lose the read-ahead menu input.
	got := runCLI(t, "y\n3\nq\n", "deploy-fleet", "quant-firm", "--from", "max@example.com")
	if got.err != nil {
		t.Fatalf("deploy-fleet: %v", got.err)
	}
	mustContain(t, got.stdout, "message agent-000005 first", "stdout")
	// Display loops back to the menu rather than exiting.
	mustContain(t, got.stdout, "1) run it with claude code", "stdout")
}

func TestDeployFleetMenuRunsClaudeWithHandoffFile(t *testing.T) {
	response := withSkill(t)
	login(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(response))
	})

	// A fake `claude` on PATH records the prompt it was launched with; the
	// handoff file is written to the (temp) working directory.
	bin := t.TempDir()
	promptFile := filepath.Join(bin, "prompt.txt")
	script := "#!/bin/sh\nprintf '%s' \"$1\" > " + promptFile + "\n"
	if err := os.WriteFile(filepath.Join(bin, "claude"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Chdir(t.TempDir())

	got := runCLI(t, "1\n", "deploy-fleet", "quant-firm", "--from", "max@example.com", "--yes")
	if got.err != nil {
		t.Fatalf("deploy-fleet: %v", got.err)
	}

	handoff := "chariot-fleet-quant-firm-setup.md"
	info, err := os.Stat(handoff)
	if err != nil {
		t.Fatalf("handoff file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("handoff file mode = %o, want 600", perm)
	}
	content, err := os.ReadFile(handoff)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"ts_secret",                     // the token-seed setup needs
		"agent-000005",                  // the fleet roster
		"brain (deployed as max-brain)", // alias surfaced for the coding agent
		"message agent-000005 first",    // the skill itself
		"Delete it once setup is done.", // the cleanup warning
	} {
		if !strings.Contains(string(content), want) {
			t.Errorf("handoff file missing %q", want)
		}
	}

	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatalf("fake claude was not launched: %v", err)
	}
	if !strings.Contains(string(prompt), handoff) {
		t.Errorf("prompt does not point at the handoff file: %q", prompt)
	}
}
