package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestDeployRejectsNonPositiveCount(t *testing.T) {
	for _, count := range []string{"0", "-1"} {
		t.Run(count, func(t *testing.T) {
			// No login: --count is validated before the client is built, so a
			// bad count must never reach the network.
			logout(t)
			got := runCLI(t, "", "deploy", "--count", count)
			if got.err == nil {
				t.Fatal("want an error for non-positive --count")
			}
			mustContain(t, got.err.Error(), "--count must be positive", "error")
		})
	}
}

func TestDeploySendsFlagsAndPrintsTokenSeed(t *testing.T) {
	var body map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/deploy" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decoding body: %v", err)
		}
		_, _ = w.Write([]byte(`{"token_seed":"ts_secret","namespace":"cust-7",
			"created":10,"total":10,"model":"anthropic/claude-opus-4.8","image":"openclaw",
			"skills":["docs"]}`))
	})

	got := runCLI(t, "", "deploy", "--count", "10",
		"--endpoint", "https://hooks.example/agent",
		"--model", "anthropic/claude-opus-4.8", "--image", "openclaw",
		"--skills", "docs")
	if got.err != nil {
		t.Fatalf("deploy: %v", got.err)
	}

	if body["count"].(float64) != 10 {
		t.Errorf("count = %v", body["count"])
	}
	if body["endpoint"] != "https://hooks.example/agent" {
		t.Errorf("endpoint = %v", body["endpoint"])
	}
	if body["model"] != "anthropic/claude-opus-4.8" {
		t.Errorf("model = %v", body["model"])
	}
	if body["image"] != "openclaw" {
		t.Errorf("image = %v", body["image"])
	}
	if skills, ok := body["skills"].([]any); !ok || len(skills) != 1 || skills[0] != "docs" {
		t.Errorf("skills = %v", body["skills"])
	}

	mustContain(t, got.stdout, "10 agents live (total 10)", "stdout")
	mustContain(t, got.stdout, "endpoint    : https://hooks.example/agent", "stdout")
	mustContain(t, got.stdout, "image       : openclaw", "stdout")
	mustContain(t, got.stdout, "skills      : docs", "stdout")
	// The token-seed is shown exactly once, so the command must print it.
	mustContain(t, got.stdout, "token-seed  : ts_secret", "stdout")
	mustContain(t, got.stdout, "X-Chariot-Token: ts_secret", "stdout")
}

// Optional flags are omitted from the request body entirely (rather than sent
// as ""), so the backend applies its own defaults.
func TestDeployOmitsUnsetOptionalFlags(t *testing.T) {
	var body map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"token_seed":"ts","namespace":"n","created":1,"total":1,"model":"m"}`))
	})

	got := runCLI(t, "", "deploy", "--count", "1")
	if got.err != nil {
		t.Fatalf("deploy: %v", got.err)
	}

	for _, key := range []string{"endpoint", "model", "image"} {
		if _, present := body[key]; present {
			t.Errorf("body should omit %q, got %v", key, body[key])
		}
	}
	// Without --endpoint the user is told replies land in the inbox.
	mustContain(t, got.stdout, "endpoint    : none", "stdout")
	// An empty image means "account default" — don't print a blank image line.
	mustNotContain(t, got.stdout, "image       :", "stdout")
}
