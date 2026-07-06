// Package scaffold embeds ready-to-build custom agent image templates for
// `chariot image init`.
package scaffold

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

//go:embed openclaw
var templates embed.FS

// Names returns the available template names, sorted.
func Names() []string {
	entries, err := templates.ReadDir(".")
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

// Files that must be executable in the written scaffold (embed does not
// preserve modes; the Dockerfile also chmods them at build as a belt-and-braces).
var executable = map[string]bool{
	"entrypoint.sh": true,
}

// Write materializes the named template into dir, creating it if needed.
// Refuses to overwrite existing files. Returns the written paths (relative).
func Write(name, dir string) ([]string, error) {
	sub, err := fs.Sub(templates, name)
	if err != nil {
		return nil, fmt.Errorf("unknown template %q (available: %v)", name, Names())
	}
	if _, err := fs.Stat(sub, "Dockerfile"); err != nil {
		return nil, fmt.Errorf("unknown template %q (available: %v)", name, Names())
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	var written []string
	err = fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path == "." {
				return nil
			}
			return os.MkdirAll(filepath.Join(dir, path), 0o755)
		}
		dest := filepath.Join(dir, path)
		if _, statErr := os.Stat(dest); statErr == nil {
			return fmt.Errorf("%s already exists — refusing to overwrite (use a fresh directory)", dest)
		}
		data, readErr := fs.ReadFile(sub, path)
		if readErr != nil {
			return readErr
		}
		mode := os.FileMode(0o644)
		if executable[filepath.Base(path)] {
			mode = 0o755
		}
		if writeErr := os.WriteFile(dest, data, mode); writeErr != nil {
			return writeErr
		}
		written = append(written, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(written)
	return written, nil
}
