package registry

import (
	"fmt"
	"os"
	"path/filepath"
)

// SkillSpec is a skill directory (Agent Skills layout) loaded from
// config/skills/<name>/SKILL.md with optional bundled files.
type SkillSpec struct {
	Name        string
	Description string
	Path        string // the skill directory
}

// loadSkills loads config/skills/<name>/SKILL.md directories. SKILL.md
// must carry Agent Skills frontmatter (name, description); directories
// without a SKILL.md are skipped (they may hold bundled assets).
func loadSkills(snap *Snapshot, dir string, errs *[]LoadError) {
	for _, e := range readKindDir(dir, KindSkill, errs) {
		if !e.IsDir() || skipEntry(e.Name()) {
			continue
		}
		skillDir := filepath.Join(dir, e.Name())
		skillMD := filepath.Join(skillDir, "SKILL.md")
		raw, err := os.ReadFile(skillMD)
		if err != nil {
			if os.IsNotExist(err) {
				continue // no SKILL.md: not a skill (assets-only directory)
			}
			addErr(errs, skillMD, KindSkill, err)
			continue
		}
		var spec SkillSpec
		spec.Path = skillDir
		fm, _, ok := splitFrontmatter(string(raw))
		if !ok {
			addErr(errs, skillMD, KindSkill, fmt.Errorf("no YAML frontmatter"))
			continue
		}
		if err := decodeFrontmatter(fm, &spec); err != nil {
			addErr(errs, skillMD, KindSkill, fmt.Errorf("frontmatter decode: %w", err))
			continue
		}
		if spec.Name == "" || spec.Description == "" {
			addErr(errs, skillMD, KindSkill, fmt.Errorf("frontmatter requires name and description"))
			continue
		}
		if existing, dup := snap.Skills[spec.Name]; dup {
			addErr(errs, skillMD, KindSkill, fmt.Errorf("duplicate skill name %q (keeping %s)", spec.Name, existing.Path))
			continue
		}
		snap.Skills[spec.Name] = spec
	}
}
