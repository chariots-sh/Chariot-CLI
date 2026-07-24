package cmd

import (
	"net/http"
	"testing"
)

// The happy path prints the link (the reason the command exists) alongside
// enough context to know which page it is.
func TestPageShowsLink(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method+" "+r.URL.Path != "GET /v1/agents/analyst/page" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(`{"title":"Weekly numbers","html":"<h1>Q3</h1>",` +
			`"agent":"analyst","share_url":"https://app.chariots.sh/p/#abc.def"}`))
	})

	got := runCLI(t, "", "page", "--agent", "analyst")
	if got.err != nil {
		t.Fatalf("page: %v", got.err)
	}
	mustContain(t, got.stdout, "title : Weekly numbers", "stdout")
	mustContain(t, got.stdout, "agent : analyst", "stdout")
	mustContain(t, got.stdout, "link  : https://app.chariots.sh/p/#abc.def", "stdout")
	mustContain(t, got.stdout, "without signing in", "stdout")
}

// A 404 is the ordinary "nothing published, or asleep" answer to a query, so
// it must read as an answer and exit 0 — not fail the command.
func TestPageMissingIsNotAnError(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"detail":"no page — the agent hasn't published one, or it's asleep"}`))
	})

	got := runCLI(t, "", "page", "--agent", "analyst")
	if got.err != nil {
		t.Fatalf("page (missing) should not error: %v", got.err)
	}
	mustContain(t, got.stdout, "no page", "stdout")
	mustContain(t, got.stdout, "asleep", "stdout")
}

func TestPageRequiresAgent(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("should not call the API: %s %s", r.Method, r.URL.Path)
	})

	got := runCLI(t, "", "page")
	if got.err == nil {
		t.Fatal("expected an error without --agent")
	}
}
