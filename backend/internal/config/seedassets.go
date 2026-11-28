package config

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"path/filepath"
	"zbot/backend/internal/noclobber"
)

// seedFS embeds the example roster (the same go:embed pattern as the
// ward vault templates): delegation-ready agent seeds and prompt-craft
// skill seeds tuned from the operator's ~/Documents/zbot set. Seeded
// write-once — user-owned after first write.
//
//go:embed seedassets/agents seedassets/skills seedassets/prompts seedassets/system-agents
var seedFS embed.FS

// intentAnalystSeedMarker gates the one-time intent-analyst seed
// (intent-analyst-agent S1): its OWN marker, separate from the example
// roster's, so homes already marked by the roster change receive the
// analyst on upgrade exactly once; afterwards edits and deletions stick.
const intentAnalystSeedMarker = ".intent-analyst-seeded"

// seedIntentAnalyst publishes the system agent once per home.
func seedIntentAnalyst(ctx context.Context, paths Paths) error {
	if paths.AgentsDir == "" || markerExists(paths.Home, intentAnalystSeedMarker) {
		return nil // no destination wired (unit helpers) / already seeded
	}
	if err := seedAsset(ctx, filepath.Join(paths.AgentsDir, "intent-analyst.md"), "seedassets/system-agents/intent-analyst.md"); err != nil {
		return err
	}
	return dropMarker(filepath.Join(paths.Home, intentAnalystSeedMarker))
}

// seedExamples seeds the example roster: agents into agents/, skills
// into the top-level skills/ root (relocate-skills-hooks). Every file is published
// crash-atomically via link (see seedAsset) — an existing file (a user
// edit, a concurrent writer) is left untouched, and the seedRoster
// marker keeps deleted examples from resurrecting.
func seedExamples(ctx context.Context, paths Paths) error {
	// The system agent first: it must land even when the roster/persona
	// branches below return early on an already-marked home.
	if err := seedIntentAnalyst(ctx, paths); err != nil {
		return err
	}
	// The honesty-contract upgrade of unmodified research/builder seeds:
	// once per home, before the early returns below.
	if !markerExists(paths.Home, agentHonestySeedMarker) {
		if err := upgradeAgentSeedsHonesty(ctx, paths); err != nil {
			return err
		}
	}
	// The reasoning-effort upgrade of unmodified builder seeds (D5).
	if !markerExists(paths.Home, agentEffortSeedMarker) {
		if err := upgradeAgentSeedsEffort(ctx, paths); err != nil {
			return err
		}
	}
	// Agents+skills ride the roster marker (seed-operator-roster):
	// one marker written after BOTH walks, so the pair seeds as a unit.
	if markerExists(paths.Home, exampleRosterMarker) {
		// already seeded — deletions stay deleted
	} else if err := seedWalk(ctx, "seedassets/agents", paths.AgentsDir); err != nil {
		return err
	} else if err := seedWalk(ctx, "seedassets/skills", paths.SkillsDir); err != nil {
		return err
	} else if err := dropMarker(filepath.Join(paths.Home, exampleRosterMarker)); err != nil {
		return err
	}
	// The persona seeds carry their OWN marker: homes already marked by
	// the earlier roster change still receive the persona once (upgrade
	// path); after this marker, deletions stick like everywhere else.
	if markerExists(paths.Home, personaSeedMarker) {
		// v2 (ward-v2-full-session): UPGRADE pre-v2 homes to the
		// operator's agentzero persona set — but only files that are
		// ABSENT (never shipped) or still EXACTLY the old neutral seed
		// (unmodified): user edits and deletions of PERSONALIZED files
		// are never clobbered or resurrected.
		if markerExists(paths.Home, personaSeedV2Marker) {
			return nil
		}
		if err := upgradePersonaSeeds(ctx, paths); err != nil {
			return err
		}
		return dropMarker(filepath.Join(paths.Home, personaSeedV2Marker))
	}
	if err := seedWalk(ctx, "seedassets/prompts", paths.ConfigDir("prompts")); err != nil {
		return err
	}
	// Fresh installs seed the v2 persona directly: both markers drop so
	// the v2 re-seed never resurrects a later deletion.
	if err := dropMarker(filepath.Join(paths.Home, personaSeedV2Marker)); err != nil {
		return err
	}
	return dropMarker(filepath.Join(paths.Home, personaSeedMarker))
}

// upgradePersonaSeeds carries the three persona files to the operator
// set. In a MARKED home (the only homes that reach this path) all
// three were shipped once, so: present-but-UNMODIFIED neutral v1 seed
// → replace (crash-atomic temp+rename); present-and-user-modified →
// untouched; ABSENT → the user deleted it — deletions stick (D7).
func upgradePersonaSeeds(ctx context.Context, paths Paths) error {
	for name, oldBody := range personaV1Bodies {
		dst := filepath.Join(paths.ConfigDir("prompts"), name)
		raw, err := os.ReadFile(dst)
		if err != nil {
			continue // absent (deleted — sticks) or unreadable: leave alone
		}
		if string(raw) != oldBody {
			continue // user-modified: untouched
		}
		// Unmodified neutral seed: replace with the operator's,
		// crash-atomically (temp file + rename — an interrupted process
		// never strands a partial that later runs treat as user-owned).
		newBody, rerr := seedAssetFSRead(ctx, "seedassets/prompts/"+name)
		if rerr != nil {
			return rerr
		}
		tmp := dst + ".upgrade-tmp"
		if werr := os.WriteFile(tmp, []byte(newBody), 0o644); werr != nil {
			return werr
		}
		if rerr := os.Rename(tmp, dst); rerr != nil {
			_ = os.Remove(tmp)
			return rerr
		}
	}
	return nil
}

// agentHonestySeedMarker gates the one-time honesty-contract upgrade of
// the research and builder agent seeds (delegation-truthfulness): a
// home seeded before it receives the new seeds ONCE, and only when the
// user's copy is still byte-identical to the earlier seed — an edited
// copy is the user's and is never touched.
const agentHonestySeedMarker = ".agent-seeds-honesty"

// agentHonestyV1FS embeds the PRE-honesty research and builder seeds
// (the exact bytes earlier bootstraps wrote) for the byte-identical
// comparison.
//
//go:embed seedassets/agents_v1/research-agent.example.md seedassets/agents_v1/builder-agent.example.md
var agentHonestyV1FS embed.FS

var agentHonestyV1Bodies = map[string]string{
	"research-agent.example.md": mustReadSeedV1("seedassets/agents_v1/research-agent.example.md"),
	"builder-agent.example.md":  mustReadSeedV1("seedassets/agents_v1/builder-agent.example.md"),
}

func mustReadSeedV1(path string) string {
	raw, err := agentHonestyV1FS.ReadFile(path)
	if err != nil {
		panic("config: embedded pre-honesty agent seed missing: " + err.Error())
	}
	return string(raw)
}

const agentEffortSeedMarker = ".agent-seeds-effort"

// agentEffortV2FS embeds the pre-effort builder seed (the exact bytes the
// honesty upgrade wrote) for the byte-identical comparison.
//
//go:embed seedassets/agents_v2/builder-agent.example.md
var agentEffortV2FS embed.FS

var agentEffortV2Bodies = map[string]string{
	"builder-agent.example.md": mustReadSeedV2("seedassets/agents_v2/builder-agent.example.md"),
}

func mustReadSeedV2(path string) string {
	raw, err := agentEffortV2FS.ReadFile(path)
	if err != nil {
		panic("config: embedded pre-effort agent seed missing: " + err.Error())
	}
	return string(raw)
}

// upgradeAgentSeedsEffort replaces unmodified pre-effort builder seeds
// with the reasoning_effort: low seed (engine-adk-spine, decision D5),
// once per home.
func upgradeAgentSeedsEffort(ctx context.Context, paths Paths) error {
	return upgradeAgentSeeds(ctx, paths, agentEffortV2Bodies, agentEffortSeedMarker)
}

// upgradeAgentSeedsHonesty replaces unmodified pre-honesty research and
// builder seeds with the honesty-contract seeds, crash-atomically
// (temp + rename), once per home.
func upgradeAgentSeedsHonesty(ctx context.Context, paths Paths) error {
	return upgradeAgentSeeds(ctx, paths, agentHonestyV1Bodies, agentHonestySeedMarker)
}

// upgradeAgentSeeds rewrites every installed copy that still matches its
// embedded previous body (user-modified copies are untouched), then drops
// the marker — once per home.
func upgradeAgentSeeds(ctx context.Context, paths Paths, previous map[string]string, marker string) error {
	if paths.AgentsDir == "" {
		return nil
	}
	for name, oldBody := range previous {
		dst := filepath.Join(paths.AgentsDir, name)
		raw, err := os.ReadFile(dst)
		if os.IsNotExist(err) {
			continue // absent (user deleted it): untouched
		}
		if err != nil {
			return err // unreadable ≠ absent: no marker, retry next bootstrap
		}
		if string(raw) != oldBody {
			continue // user-modified: untouched
		}
		newBody, rerr := seedAssetFSRead(ctx, "seedassets/agents/"+name)
		if rerr != nil {
			return rerr
		}
		if err := replaceFileAtomically(dst, []byte(newBody)); err != nil {
			return err
		}
	}
	return dropMarker(filepath.Join(paths.Home, marker))
}

// replaceFileAtomically writes body to a temp file beside dst and
// renames it over dst (an interrupted process never strands a partial
// that later runs would treat as user-owned).
func replaceFileAtomically(dst string, body []byte) error {
	tmp := dst + ".upgrade-tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// personaSeedMarker gates the one-time persona seeding (a later addition
// than the example roster; separate so pre-persona installs upgrade).
const personaSeedMarker = ".persona-seeded"

// personaSeedV2Marker gates the operator-persona re-seed (ward-v2).
const personaSeedV2Marker = ".persona-seeded-v2"

// personaV1Seeds embeds the ORIGINAL neutral persona bodies (pre-v2):
// the upgrade replaces them only when byte-identical (unmodified).
//
//go:embed seedassets/persona_v1/role.md seedassets/persona_v1/soul.md seedassets/persona_v1/os-linux.md
var personaV1FS embed.FS

var personaV1Bodies = map[string]string{
	"role.md":     mustReadSeed("seedassets/persona_v1/role.md"),
	"soul.md":     mustReadSeed("seedassets/persona_v1/soul.md"),
	"os-linux.md": mustReadSeed("seedassets/persona_v1/os-linux.md"),
}

func mustReadSeed(path string) string {
	raw, err := personaV1FS.ReadFile(path)
	if err != nil {
		panic("config: embedded v1 persona missing: " + err.Error())
	}
	return string(raw)
}

// seedAssetFSRead reads one embedded seed file's body.
func seedAssetFSRead(_ context.Context, path string) (string, error) {
	raw, err := seedFS.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("config: seed %s: %w", path, err)
	}
	return string(raw), nil
}

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

// migrateLegacySkillsHooks relocates legacy config/{skills,hooks}/
// entries into the top-level roots (relocate-skills-hooks), one entry
// at a time: an entry whose destination exists is LEFT in place
// (never overwritten — the top-level copy stands); otherwise it moves
// up. Naturally idempotent: empty/absent legacy dirs are a no-op, and
// a hand-moved home (top-level populated, legacy empty) migrates
// nothing. Empty legacy directories are left in place (inert).
func migrateLegacySkillsHooks(paths Paths) error {
	pairs := []struct{ legacy, dest string }{
		{paths.ConfigDir("skills"), paths.SkillsDir},
		{paths.ConfigDir("hooks"), paths.HooksDir},
	}
	for _, p := range pairs {
		if p.legacy == "" || p.dest == "" || p.legacy == p.dest {
			continue
		}
		entries, err := os.ReadDir(p.legacy)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // absent legacy dir: nothing to migrate
			}
			return fmt.Errorf("config: migrate legacy %s: %w", p.legacy, err) // unreadable ≠ absent
		}
		if err := os.MkdirAll(p.dest, 0o755); err != nil {
			return fmt.Errorf("config: migrate %s: %w", p.dest, err)
		}
		for _, e := range entries {
			src := filepath.Join(p.legacy, e.Name())
			dst := filepath.Join(p.dest, e.Name())
			// No-clobber move: a destination that appears between the
			// listing and the move is a conflict, never an overwrite
			// (phase-1 gate finding — the registry's noclobber, shared).
			if err := noclobber.Move(src, dst); err != nil {
				if noclobber.IsExist(err) {
					continue // destination exists: never overwrite, legacy copy stays inert
				}
				return fmt.Errorf("config: migrate %s -> %s: %w", src, dst, err)
			}
		}
	}
	return nil
}
