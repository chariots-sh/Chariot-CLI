package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteOpenclawScaffold(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "img")
	written, err := Write("openclaw", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, required := range []string{"Dockerfile", "entrypoint.sh", "zeroclaw", "turn.mjs", "render-config.mjs", "health-server.mjs", "README.md"} {
		found := false
		for _, w := range written {
			if w == required {
				found = true
			}
		}
		if !found {
			t.Errorf("missing scaffold file %s (wrote %v)", required, written)
		}
	}
	info, err := os.Stat(filepath.Join(dir, "zeroclaw"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("zeroclaw shim not executable: %v", info.Mode())
	}
	// A second write into the same directory must refuse to overwrite.
	if _, err := Write("openclaw", dir); err == nil {
		t.Error("expected overwrite refusal, got nil error")
	}
	// Unknown templates error cleanly.
	if _, err := Write("nope", t.TempDir()); err == nil {
		t.Error("expected unknown-template error, got nil")
	}
}
