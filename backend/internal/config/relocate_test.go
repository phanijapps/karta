package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
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
		// The operator's real state: BOTH seed markers present, a REAL
		// user skill at the top level, an EMPTY legacy root — the
		// migration moves nothing and the user skill survives
		// byte-for-byte.
		if err := os.MkdirAll(paths.Home, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(paths.Home, exampleRosterMarker), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(paths.Home, personaSeedMarker), nil, 0o644); err != nil {
			t.Fatal(err)
		}
		userSkill := filepath.Join(paths.SkillsDir, "mine", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(userSkill), 0o755); err != nil {
			t.Fatal(err)
		}
		const userBody = "---\nname: mine\ndescription: mine\n---\nmy body"
		if err := os.WriteFile(userSkill, []byte(userBody), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(paths.ConfigDir("skills"), 0o755); err != nil { // empty legacy root
			t.Fatal(err)
		}
		if err := Bootstrap(ctxBG(t), paths); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(userSkill)
		if err != nil || string(got) != userBody {
			t.Fatalf("user skill disturbed: %q %v", got, err)
		}
		// No example seeds resurrected alongside the user tree.
		if _, err := os.Stat(filepath.Join(paths.SkillsDir, "clean-code")); !os.IsNotExist(err) {
			t.Fatal("marked hand-moved home must not gain example seeds")
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

}

func ctxBG(t *testing.T) context.Context {
	t.Helper()
	return context.Background()
}
