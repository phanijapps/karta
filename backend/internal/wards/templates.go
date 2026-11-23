package wards

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// templateFS embeds the archetype templates (the same pattern as the
// example agents): generic (freeform knowledge) and research
// (spec/plan/tasks + material roles). Embedded so the daemon needs no
// external assets; seeded write-once — user-owned after first write.
//
//go:embed templates/generic/* templates/research/*
var templateFS embed.FS

// templatesDir is the vault's machinery directory holding the archetype
// templates. Dot-prefixed so Index skips it; wards.Bootstrap is its only
// writer, and only for files that do not exist yet.
const templatesDir = ".templates"

// gitignoreStarter lands at the vault root on first bootstrap: a hint
// for users who git-init their vault (wards are user data; versioning
// them is the user's choice).
const gitignoreStarter = `# Wards are user data, not daemon state (Karta vault).
# This vault is yours: git-init it, sync it, or leave it plain — Karta
# only reads and lints it.
`

// Archetypes lists the seeded archetype names (sorted). Ward creation
// (slice 3) copies from .templates/<name>/.
func Archetypes() []string { return []string{"generic", "research"} }

// Bootstrap creates the vault directory when missing and seeds the
// archetype templates plus the .gitignore starter. NEVER overwrites:
// files are created with O_EXCL — anything that already exists (a user
// edit, a concurrent writer, even a dangling symlink) is left untouched
// — so hand edits and customizations survive re-bootstrap forever
// (spec scenario).
func Bootstrap(ctx context.Context, vaultRoot string) error {
	tmplDir := filepath.Join(vaultRoot, templatesDir)
	if err := os.MkdirAll(tmplDir, 0o755); err != nil {
		return fmt.Errorf("wards: bootstrap vault %s: %w", vaultRoot, err)
	}
	if err := fs.WalkDir(templateFS, "templates", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("wards: walk templates: %w", err)
		}
		if d.IsDir() {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("wards: bootstrap: %w", err)
		}
		// templates/<archetype>/<file> → <vaultRoot>/.templates/<archetype>/<file>
		rel := filepath.ToSlash(path[len("templates/"):])
		dest := filepath.Join(tmplDir, filepath.FromSlash(rel))
		return seedFile(dest, path)
	}); err != nil {
		return err
	}
	return seedFile(filepath.Join(vaultRoot, ".gitignore"), "")
}

// seedFile writes one embedded template file to dest only when dest does
// not exist, atomically: O_CREATE|O_EXCL never follows an existing file
// or symlink and never truncates. src "" seeds the .gitignore starter
// instead of an embedded file (kept inline: one line of guidance, not a
// template).
func seedFile(dest, src string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("wards: seed %s: %w", dest, err)
	}
	content := []byte(gitignoreStarter)
	if src != "" {
		raw, err := templateFS.ReadFile(src)
		if err != nil {
			return fmt.Errorf("wards: seed %s: %w", dest, err)
		}
		content = raw
	}
	f, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil // user-owned after first write — never overwrite
		}
		return fmt.Errorf("wards: seed %s: %w", dest, err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close() // best-effort close on the error path; the file stays partial-but-owned
		return fmt.Errorf("wards: seed %s: %w", dest, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("wards: seed %s: %w", dest, err)
	}
	return nil
}
