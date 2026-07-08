package cmd

import (
	"net/http"
	"strings"
	"testing"
)

// An agent with no explicit image runs the account default; the column must
// say so rather than render an empty cell.
func TestListRendersDefaultImageForNilImage(t *testing.T) {
	login(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"agents":[
			{"id":"a1","slug":"agent-1","state":"active","image":"openclaw"},
			{"id":"a2","slug":"agent-2","state":"deactivated","image":null}
		],"next_cursor":""}`))
	})

	got := runCLI(t, "", "list")
	if got.err != nil {
		t.Fatalf("list: %v", got.err)
	}
	mustContain(t, got.stdout, "AGENT ID", "header")
	mustContain(t, got.stdout, "openclaw", "stdout")
	mustContain(t, got.stdout, "default", "stdout")
	mustContain(t, got.stderr, "2 agent(s) shown.", "stderr")
}

// Without --all, list stops after one page and says more is available.
func TestListStopsAtOnePageAndHintsAll(t *testing.T) {
	pages := 0
	login(t, func(w http.ResponseWriter, r *http.Request) {
		pages++
		_, _ = w.Write([]byte(`{"agents":[{"id":"a1","slug":"s1","state":"active"}],"next_cursor":"c1"}`))
	})

	got := runCLI(t, "", "list")
	if got.err != nil {
		t.Fatalf("list: %v", got.err)
	}
	if pages != 1 {
		t.Errorf("want 1 page fetched, got %d", pages)
	}
	mustContain(t, got.stderr, "use --all", "stderr")
}

// With --all, list follows next_cursor until it is empty.
func TestListAllPaginatesToEnd(t *testing.T) {
	var cursors []string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		cursor := r.URL.Query().Get("cursor")
		cursors = append(cursors, cursor)
		switch cursor {
		case "":
			_, _ = w.Write([]byte(`{"agents":[{"id":"a1","slug":"s1","state":"active"}],"next_cursor":"c1"}`))
		case "c1":
			_, _ = w.Write([]byte(`{"agents":[{"id":"a2","slug":"s2","state":"active"}],"next_cursor":""}`))
		default:
			t.Errorf("unexpected cursor %q", cursor)
		}
	})

	got := runCLI(t, "", "list", "--all")
	if got.err != nil {
		t.Fatalf("list --all: %v", got.err)
	}
	if strings.Join(cursors, ",") != ",c1" {
		t.Errorf("cursors = %v, want [\"\", \"c1\"]", cursors)
	}
	mustContain(t, got.stdout, "a1", "stdout")
	mustContain(t, got.stdout, "a2", "stdout")
	mustContain(t, got.stderr, "2 agent(s) shown.", "stderr")
	mustNotContain(t, got.stderr, "use --all", "stderr")
}

func TestListPassesLimit(t *testing.T) {
	var limit string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		limit = r.URL.Query().Get("limit")
		_, _ = w.Write([]byte(`{"agents":[],"next_cursor":""}`))
	})

	if got := runCLI(t, "", "list", "--limit", "7"); got.err != nil {
		t.Fatalf("list: %v", got.err)
	}
	if limit != "7" {
		t.Errorf("limit = %q, want 7", limit)
	}
}
