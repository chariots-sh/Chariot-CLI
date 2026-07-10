package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func TestFleetCreateParsesImageSpecsAndSendsThem(t *testing.T) {
	var body map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/fleets" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"name":"quant-firm","description":"runs a quant firm",
			"items":[{"image_name":"zeroclaw","kind":"builtin","count":5,"pod_size":"small","daily_fee_dollars":1,"available":true},
			         {"image_name":"brain","kind":"custom","count":1,"pod_size":"medium","daily_fee_dollars":2,"available":true}],
			"total_agents":6,"has_skill":false,"published":false,"deploy_count":0}`))
	})

	got := runCLI(t, "", "fleet", "create", "quant-firm",
		"--image", "zeroclaw:5", "--image", "brain",
		"--description", "runs a quant firm")
	if got.err != nil {
		t.Fatalf("fleet create: %v", got.err)
	}

	items := body["items"].([]any)
	first := items[0].(map[string]any)
	second := items[1].(map[string]any)
	if first["image_name"] != "zeroclaw" || first["count"].(float64) != 5 {
		t.Errorf("first item = %v", first)
	}
	// A spec without :count defaults to 1.
	if second["image_name"] != "brain" || second["count"].(float64) != 1 {
		t.Errorf("second item = %v", second)
	}
	if body["description"] != "runs a quant firm" {
		t.Errorf("description = %v", body["description"])
	}
	mustContain(t, got.stdout, "5× zeroclaw + 1× brain", "stdout")
	mustContain(t, got.stdout, "6 agents", "stdout")
}

func TestFleetCreateRejectsBadImageSpecs(t *testing.T) {
	for _, spec := range []string{"brain:0", "brain:lots", ":3"} {
		t.Run(spec, func(t *testing.T) {
			// No login: specs are validated before the client is built.
			logout(t)
			got := runCLI(t, "", "fleet", "create", "pack", "--image", spec)
			if got.err == nil {
				t.Fatalf("want an error for --image %q", spec)
			}
		})
	}
}

func TestFleetDeleteConfirmAborts(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("no request expected on abort, got %s %s", r.Method, r.URL.Path)
	})
	got := runCLI(t, "n\n", "fleet", "delete", "quant-firm")
	if got.err != nil {
		t.Fatalf("fleet delete: %v", got.err)
	}
	mustContain(t, got.stdout, "Aborted", "stdout")
}

func TestFleetBrowseRendersCatalog(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/fleets/public" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"packs":[{"owner_email":"max@example.com","name":"quant-firm",
			"description":"runs a quant firm",
			"items":[{"image_name":"zeroclaw","kind":"builtin","count":5,"pod_size":"small","daily_fee_dollars":1,"available":true}],
			"total_agents":5,"total_daily_fee_dollars":5,"has_skill":true,
			"published_at":"2026-07-10T00:00:00Z","deploy_count":3}],"next_cursor":null}`))
	})

	got := runCLI(t, "", "fleet", "browse")
	if got.err != nil {
		t.Fatalf("fleet browse: %v", got.err)
	}
	mustContain(t, got.stdout, "quant-firm", "stdout")
	mustContain(t, got.stdout, "max@example.com", "stdout")
	mustContain(t, got.stdout, "$5.00", "stdout")
	mustContain(t, got.stdout, "deploy-fleet <pack> --from <owner-email>", "stdout")
}

func TestFleetSkillSetReadsFileAndShowPrintsMarkdown(t *testing.T) {
	skillPath := filepath.Join(t.TempDir(), "SETUP.md")
	if err := os.WriteFile(skillPath, []byte("# Setup\nwire the fleet"), 0o644); err != nil {
		t.Fatal(err)
	}
	var putBody map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/fleets/quant-firm/skill":
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			_, _ = w.Write([]byte(`{"name":"quant-firm","content":"# Setup\nwire the fleet"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/fleets/public/skill":
			if r.URL.Query().Get("owner_email") != "max@example.com" || r.URL.Query().Get("name") != "quant-firm" {
				t.Errorf("unexpected query: %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"name":"quant-firm","content":"# Published setup"}`))
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	got := runCLI(t, "", "fleet", "skill", "set", "quant-firm", skillPath)
	if got.err != nil {
		t.Fatalf("fleet skill set: %v", got.err)
	}
	if putBody["content"] != "# Setup\nwire the fleet" {
		t.Errorf("content = %v", putBody["content"])
	}

	got = runCLI(t, "", "fleet", "skill", "show", "quant-firm", "--from", "max@example.com")
	if got.err != nil {
		t.Fatalf("fleet skill show: %v", got.err)
	}
	mustContain(t, got.stdout, "# Published setup", "stdout")
}
