package cmd

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestModelsShowsFleetModel(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"email":"a@b.c","model":"anthropic/claude-opus-4.8"}`))
	})

	got := runCLI(t, "", "models")
	if got.err != nil {
		t.Fatalf("models: %v", got.err)
	}
	mustContain(t, got.stdout, "fleet model : anthropic/claude-opus-4.8", "stdout")
}

func TestModelsSetSendsModelID(t *testing.T) {
	var body map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v1/account/model" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"model":"openai/gpt-5"}`))
	})

	got := runCLI(t, "", "models", "set", "openai/gpt-5")
	if got.err != nil {
		t.Fatalf("models set: %v", got.err)
	}
	if body["model"] != "openai/gpt-5" {
		t.Errorf("model = %v", body["model"])
	}
	mustContain(t, got.stdout, "✓ fleet model: openai/gpt-5", "stdout")
}

// `models set default` is the documented way to reset; it must send a JSON
// null (not the literal string "default") so the backend restores its default.
func TestModelsSetDefaultSendsNull(t *testing.T) {
	var body map[string]any
	login(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = w.Write([]byte(`{"model":"server/default"}`))
	})

	got := runCLI(t, "", "models", "set", "default")
	if got.err != nil {
		t.Fatalf("models set default: %v", got.err)
	}
	raw, present := body["model"]
	if !present {
		t.Fatal("body must carry an explicit model key")
	}
	if raw != nil {
		t.Errorf("model = %v, want JSON null", raw)
	}
	mustContain(t, got.stdout, "✓ fleet model: server/default", "stdout")
}

func TestModelsSetRequiresExactlyOneArg(t *testing.T) {
	logout(t)
	if got := runCLI(t, "", "models", "set"); got.err == nil {
		t.Fatal("want an arity error with no model id")
	}
}
