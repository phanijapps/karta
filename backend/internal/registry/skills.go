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
	// Origin is derived at load: `marketplace`/`import` when a valid
	// .karta/provenance.yaml sidecar exists, `local` otherwise (local
	// is also the degradation for a malformed sidecar). Source is the
	// recorded owner/repo and Ref the recorded branch for marketplace
	// skills.
	Origin string
	Source string
	Ref    string
}

// ParseSkillMD parses one SKILL.md with the loader's EXACT rules
// (frontmatter name+description required, strict decode with the path
// sink). Preview and install share it so the registry can never drop
// a previewed skill.
func ParseSkillMD(raw string) (name, description, body string, err error) {
	fmRaw, body, ok := splitFrontmatter(raw)
	if !ok {
		return "", "", "", fmt.Errorf("no YAML frontmatter")
	}
	var fmSpec skillFrontmatter
	if err := decodeFrontmatter(fmRaw, &fmSpec); err != nil {
		return "", "", "", fmt.Errorf("frontmatter decode: %w", err)
	}
	if fmSpec.Name == "" || fmSpec.Description == "" {
		return "", "", "", fmt.Errorf("frontmatter requires name and description")
	}
	return fmSpec.Name, fmSpec.Description, body, nil
}

// skillFrontmatter is the decoded SKILL.md frontmatter. The Path sink
// keeps strict decoding tolerant of hand-authored path fields (they are
// ignored — the directory walk owns Path) without accepting arbitrary
// unknown fields.
type skillFrontmatter struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Path        string `yaml:"path"` // sink: decoded but never trusted
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
		_, _, ok := splitFrontmatter(string(raw))
		if !ok {
			addErr(errs, skillMD, KindSkill, fmt.Errorf("no YAML frontmatter"))
			continue
		}
		// Decode via the shared loader-exact parser (path sink inside).
		fmName, fmDesc, _, perr := ParseSkillMD(string(raw))
		if perr != nil {
			addErr(errs, skillMD, KindSkill, perr)
			continue
		}
		if len(snap.Skills) >= skillCapacity {
			addErr(errs, skillMD, KindSkill, fmt.Errorf("skill capacity %d exceeded; file overflows the managed list (sorted order decides the losers)", skillCapacity))
			continue
		}
		spec.Name = fmName
		spec.Description = fmDesc
		spec.Path = skillDir
		applyProvenance(&spec, skillDir)
		if existing, dup := snap.Skills[spec.Name]; dup {
			addErr(errs, skillMD, KindSkill, fmt.Errorf("duplicate skill name %q (keeping %s)", spec.Name, existing.Path))
			continue
		}
		snap.Skills[spec.Name] = spec
	}
}
