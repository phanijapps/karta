package config

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// seedFS embeds the example roster (the same go:embed pattern as the
// ward vault templates): delegation-ready agent seeds and prompt-craft
// skill seeds tuned from the operator's ~/Documents/zbot set. Seeded
// write-once — user-owned after first write.
//
//go:embed seedassets/agents seedassets/skills
var seedFS embed.FS

// seedExamples seeds the example roster: agents into agents/, skills
// into config/skills/ (the only tree the registry loads today; the
// deferred relocate-skills-hooks change carries them to a top-level
// skills/ via its one-time migration). Every file is published
// crash-atomically via link (see seedAsset) — an existing file (a user
// edit, a concurrent writer) is left untouched, and the seedRoster
// marker keeps deleted examples from resurrecting.
func seedExamples(ctx context.Context, paths Paths) error {
	targets := []struct{ srcRoot, dstRoot string }{
		{"seedassets/agents", paths.AgentsDir},
		{"seedassets/skills", paths.ConfigDir("skills")},
	}
	for _, t := range targets {
		if t.dstRoot == "" {
			continue // no destination wired (unit helpers)
		}
		if err := fs.WalkDir(seedFS, t.srcRoot, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return fmt.Errorf("config: walk seeds: %w", err)
			}
			if d.IsDir() {
				return nil
			}
			rel := filepath.ToSlash(p[len(t.srcRoot)+1:])
			if err := ctx.Err(); err != nil {
				return fmt.Errorf("config: seed examples: %w", err)
			}
			return seedAsset(ctx, filepath.Join(t.dstRoot, filepath.FromSlash(rel)), p)
		}); err != nil {
			return err
		}
	}
	return nil
}

// seedAsset publishes one embedded seed file at dest only when dest
// does not exist — crash-atomically: content is written to a temporary
// dotfile in the destination directory and published with os.Link
// (link(2) never replaces an existing file), so an interrupted process
// can never strand a partial destination that later runs would treat
// as user-owned. A crashed writer's leftover `.seed-*.tmp` dotfile is
// inert (the registry skips dotfiles) and unique per writer.
func seedAsset(ctx context.Context, dest, src string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("config: seed %s: %w", dest, err)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("config: seed %s: %w", dest, err)
	}
	content, err := seedFS.ReadFile(src)
	if err != nil {
		return fmt.Errorf("config: seed %s: %w", dest, err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".seed-*.tmp")
	if err != nil {
		return fmt.Errorf("config: seed %s: %w", dest, err)
	}
	tmpName := tmp.Name()
	published := false
	defer func() {
		if !published {
			_ = os.Remove(tmpName) // our temp, never the user's — always safe
		}
	}()
	if _, err := tmp.Write(content); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("config: seed %s: %w", dest, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: seed %s: %w", dest, err)
	}
	// Publish without replacing: link fails with ErrExist when dest is
	// already there (a user-owned file or a concurrent writer's publish).
	if err := os.Link(tmpName, dest); err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil // user-owned after first write — never overwrite
		}
		return fmt.Errorf("config: seed %s: %w", dest, err)
	}
	published = true
	_ = os.Remove(tmpName) // the content lives at dest now
	return nil
}
