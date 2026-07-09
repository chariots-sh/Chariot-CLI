package cmd

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

const skillBody = `{"image_name":"scraper","content":"# Setup\nSend hello first.\n",
	"updated_at":"2026-07-09T00:00:00Z"}`

func TestImageSkillShowOwnThenShareFallback(t *testing.T) {
	var paths []string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/v1/images/research/skill": // own image: served directly
			_, _ = w.Write([]byte(`{"image_name":"research","content":"# Mine\n",
				"updated_at":"2026-07-09T00:00:00Z"}`))
		case "/v1/images/scraper/skill", "/v1/images/ghost/skill":
			// not one of ours → the CLI falls back to the shares list
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"detail":"no skill attached"}`))
		case "/v1/images/shares":
			_, _ = w.Write([]byte(sharesFixture))
		case "/v1/images/shares/sh_pending/skill":
			_, _ = w.Write([]byte(skillBody))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	})

	// Own image: printed straight from the owner endpoint, raw markdown.
	got := runCLI(t, "", "image", "skill", "show", "research")
	if got.err != nil {
		t.Fatalf("skill show own: %v", got.err)
	}
	if got.stdout != "# Mine\n" {
		t.Errorf("stdout = %q", got.stdout)
	}

	// A pending offer's guide: own lookup 404s, the share lookup serves it.
	got = runCLI(t, "", "image", "skill", "show", "scraper")
	if got.err != nil {
		t.Fatalf("skill show shared: %v", got.err)
	}
	if got.stdout != "# Setup\nSend hello first.\n" {
		t.Errorf("stdout = %q", got.stdout)
	}

	// A name matching nothing errors cleanly.
	if got := runCLI(t, "", "image", "skill", "show", "ghost"); got.err == nil {
		t.Error("skill show of an unknown name must fail")
	}
}

func TestImageSkillSetReadsFileAndClearDeletes(t *testing.T) {
	var putBody map[string]any
	var deleted []string
	login(t, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/images/research/skill":
			_ = json.NewDecoder(r.Body).Decode(&putBody)
			_, _ = w.Write([]byte(`{"image_name":"research","content":"# Guide\n",
				"updated_at":"2026-07-09T00:00:00Z"}`))
		case r.Method == http.MethodDelete && r.URL.Path == "/v1/images/research/skill":
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	})

	dir := t.TempDir()
	file := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(file, []byte("# Guide\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := runCLI(t, "", "image", "skill", "set", "research", file)
	if got.err != nil {
		t.Fatalf("skill set: %v", got.err)
	}
	if putBody["content"] != "# Guide\n" {
		t.Errorf("PUT body = %v", putBody)
	}
	mustContain(t, got.stdout, "✓ setup guide attached to research", "stdout")

	got = runCLI(t, "", "image", "skill", "clear", "research")
	if got.err != nil {
		t.Fatalf("skill clear: %v", got.err)
	}
	if len(deleted) != 1 {
		t.Errorf("deleted = %v", deleted)
	}
	mustContain(t, got.stdout, "✓ setup guide removed from research", "stdout")
}
