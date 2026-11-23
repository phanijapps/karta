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
//go:embed seedassets/agents seedassets/skills seedassets/prompts
var seedFS embed.FS

// seedExamples seeds the example roster: agents into agents/, skills
// into config/skills/ (the only tree the registry loads today; the
// deferred relocate-skills-hooks change carries them to a top-level
// skills/ via its one-time migration). Every file is published
// crash-atomically via link (see seedAsset) — an existing file (a user
// edit, a concurrent writer) is left untouched, and the seedRoster
// marker keeps deleted examples from resurrecting.
func seedExamples(ctx context.Context, paths Paths) error {
	// Agents+skills ride the roster marker (seed-operator-roster):
	// one marker written after BOTH walks, so the pair seeds as a unit.
	if markerExists(paths.Home, exampleRosterMarker) {
		// already seeded — deletions stay deleted
	} else if err := seedWalk(ctx, "seedassets/agents", paths.AgentsDir); err != nil {
		return err
	} else if err := seedWalk(ctx, "seedassets/skills", paths.ConfigDir("skills")); err != nil {
		return err
	} else if err := dropMarker(filepath.Join(paths.Home, exampleRosterMarker)); err != nil {
		return err
	}
	// The persona seeds carry their OWN marker: homes already marked by
	// the earlier roster change still receive the persona once (upgrade
	// path); after this marker, deletions stick like everywhere else.
	if markerExists(paths.Home, personaSeedMarker) {
		return nil
	}
	if err := seedWalk(ctx, "seedassets/prompts", paths.ConfigDir("prompts")); err != nil {
		return err
	}
	return dropMarker(filepath.Join(paths.Home, personaSeedMarker))
}

// personaSeedMarker gates the one-time persona seeding (a later addition
// than the example roster; separate so pre-persona installs upgrade).
const personaSeedMarker = ".persona-seeded"

func markerExists(home, name string) bool {
	_, err := os.Stat(filepath.Join(home, name))
	return err == nil
}

// dropMarker writes a seed marker idempotently (concurrent writer is fine).
func dropMarker(marker string) error {
	mf, err := os.OpenFile(marker, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return nil
		}
		return fmt.Errorf("config: seed marker %s: %w", marker, err)
	}
	return mf.Close()
}

// seedWalk walks one embed root into dstRoot, publishing each file
// crash-atomically (existing files untouched).
func seedWalk(ctx context.Context, srcRoot, dstRoot string) error {
	if dstRoot == "" {
		return nil // no destination wired (unit helpers)
	}
	return fs.WalkDir(seedFS, srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("config: walk seeds: %w", err)
		}
		if d.IsDir() {
			return nil
		}
		rel := filepath.ToSlash(p[len(srcRoot)+1:])
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("config: seed examples: %w", err)
		}
		return seedAsset(ctx, filepath.Join(dstRoot, filepath.FromSlash(rel)), p)
	})
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
