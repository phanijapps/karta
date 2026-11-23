package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"zbot/backend/internal/registry"
)

// The hand-move-aware migration (relocate-skills-hooks).
func TestMigrateLegacySkillsHooks(t *testing.T) {
	t.Run("classic install migrates once", func(t *testing.T) {
		_, paths := tempHome(t)
		legacySkill := filepath.Join(paths.ConfigDir("skills"), "mine", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(legacySkill), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(legacySkill, []byte("---\nname: mine\ndescription: d\n---\nbody"), 0o644); err != nil {
			t.Fatal(err)
		}
		legacyHook := filepath.Join(paths.ConfigDir("hooks"), "h.yaml")
		if err := os.MkdirAll(filepath.Dir(legacyHook), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(legacyHook, []byte("kind: hook"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Bootstrap(ctxBG(t), paths); err != nil {
			t.Fatal(err)
		}
		for _, moved := range []string{filepath.Join(paths.SkillsDir, "mine", "SKILL.md"), filepath.Join(paths.HooksDir, "h.yaml")} {
			if _, err := os.Stat(moved); err != nil {
				t.Fatalf("migration missing %s: %v", moved, err)
			}
		}
		// Idempotent: a second bootstrap moves nothing and touches nothing.
		if err := Bootstrap(ctxBG(t), paths); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("hand-moved home is a no-op", func(t *testing.T) {
		_, paths := tempHome(t)
		// Top-level populated by the user, legacy empty — nothing to do.
		if err := os.MkdirAll(filepath.Join(paths.SkillsDir, "operators"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := Bootstrap(ctxBG(t), paths); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(filepath.Join(paths.SkillsDir, "operators")); err != nil {
			t.Fatalf("user tree disturbed: %v", err)
		}
	})

	t.Run("conflict never overwrites", func(t *testing.T) {
		_, paths := tempHome(t)
		// Top-level copy exists; legacy copy stays inert.
		if err := os.MkdirAll(filepath.Join(paths.SkillsDir, "dupe"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(paths.SkillsDir, "dupe", "SKILL.md"), []byte("top"), 0o644); err != nil {
			t.Fatal(err)
		}
		legacy := filepath.Join(paths.ConfigDir("skills"), "dupe", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(legacy), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(legacy, []byte("legacy"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := Bootstrap(ctxBG(t), paths); err != nil {
			t.Fatal(err)
		}
		got, _ := os.ReadFile(filepath.Join(paths.SkillsDir, "dupe", "SKILL.md"))
		if string(got) != "top" {
			t.Fatalf("top-level copy overwritten: %q", got)
		}
		if _, err := os.Stat(legacy); err != nil {
			t.Fatalf("legacy copy should stay inert: %v", err)
		}
	})

	t.Run("registry loads the top-level root", func(t *testing.T) {
		_, paths := tempHome(t)
		if err := Bootstrap(ctxBG(t), paths); err != nil {
			t.Fatal(err)
		}
		reg := registry.New()
		snap, errs := reg.LoadAll(registry.Dirs{Agents: paths.AgentsDir, Skills: paths.SkillsDir, Hooks: paths.HooksDir})
		if len(errs) > 0 {
			t.Fatalf("load errors: %+v", errs)
		}
		if len(snap.Skills) != 5 {
			t.Fatalf("seeded skills at the new root: %d", len(snap.Skills))
		}
	})
}

func ctxBG(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
