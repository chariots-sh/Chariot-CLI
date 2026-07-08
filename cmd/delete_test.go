package cmd

import (
	"net/http"
	"testing"
)

// deleteServer counts DELETE calls so tests can assert the destructive request
// was (or crucially, was not) made.
func deleteServer(t *testing.T, calls *int) {
	t.Helper()
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("unexpected method %s", r.Method)
		}
		if r.URL.Path != "/v1/agents/agent-1" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		*calls++
		w.WriteHeader(http.StatusNoContent)
	})
}

func TestDeleteConfirmedProceeds(t *testing.T) {
	calls := 0
	deleteServer(t, &calls)

	got := runCLI(t, "y\n", "delete", "agent-1")
	if got.err != nil {
		t.Fatalf("delete: %v", got.err)
	}
	if calls != 1 {
		t.Errorf("want 1 DELETE, got %d", calls)
	}
	mustContain(t, got.stdout, "cannot be undone", "prompt")
	mustContain(t, got.stdout, "✓ deleted agent agent-1", "stdout")
}

// Deletion is irreversible: anything other than "y" must abort without
// touching the backend.
func TestDeleteAbortsWithoutRequest(t *testing.T) {
	for name, stdin := range map[string]string{
		"explicit no": "n\n",
		"bare enter":  "\n",
		"eof":         "",
		"typo":        "yes please\n",
	} {
		t.Run(name, func(t *testing.T) {
			calls := 0
			deleteServer(t, &calls)

			got := runCLI(t, stdin, "delete", "agent-1")
			if got.err != nil {
				t.Fatalf("abort should not error: %v", got.err)
			}
			if calls != 0 {
				t.Errorf("aborted delete still issued %d request(s)", calls)
			}
			mustContain(t, got.stdout, "Aborted.", "stdout")
		})
	}
}

// A bare "y" is accepted case-insensitively and with surrounding whitespace.
func TestDeleteConfirmationIsLenient(t *testing.T) {
	for _, stdin := range []string{"Y\n", " y \n", "y"} {
		t.Run(stdin, func(t *testing.T) {
			calls := 0
			deleteServer(t, &calls)

			if got := runCLI(t, stdin, "delete", "agent-1"); got.err != nil {
				t.Fatalf("delete: %v", got.err)
			}
			if calls != 1 {
				t.Errorf("want 1 DELETE, got %d", calls)
			}
		})
	}
}

// --yes is for scripts: no prompt, and nothing read from stdin.
func TestDeleteYesSkipsPrompt(t *testing.T) {
	calls := 0
	deleteServer(t, &calls)

	got := runCLI(t, "", "delete", "agent-1", "--yes")
	if got.err != nil {
		t.Fatalf("delete --yes: %v", got.err)
	}
	if calls != 1 {
		t.Errorf("want 1 DELETE, got %d", calls)
	}
	mustNotContain(t, got.stdout, "Continue?", "stdout")
}

func TestDeleteRequiresAgentID(t *testing.T) {
	logout(t)
	if got := runCLI(t, "", "delete"); got.err == nil {
		t.Fatal("want an arity error with no agent id")
	}
}
