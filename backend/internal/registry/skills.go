package registry

import (
	"fmt"
	"os"
	"path/filepath"
)

// SkillSpec is a skill directory (Agent Skills layout) loaded from
// <skills-root>/<name>/SKILL.md (top-level ~/.zbot/skills) with
// optional bundled files.
type SkillSpec struct {
	Name        string
	Description string
	// Body is the SKILL.md content after the frontmatter — what runtime
	// provisioning inlines into ## Skills (intent middleware).
	Body string
	Path string // the skill directory
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

// skillFrontmatter is the decoded SKILL.md frontmatter. The sink
// fields (Path, License, Metadata, AllowedTools) keep strict decoding
// tolerant of PACKAGING keys common in hand-moved/marketplace skills —
// they are decoded and ignored. Everything else unknown still errors.
type skillFrontmatter struct {
	Name         string `yaml:"name"`
	Description  string `yaml:"description"`
	Path         string `yaml:"path"`          // sink: decoded but never trusted
	License      string `yaml:"license"`       // sink: packaging key, ignored
	Metadata     any    `yaml:"metadata"`      // sink: packaging key, ignored
	AllowedTools any    `yaml:"allowed-tools"` // sink: packaging key, ignored
}

// loadSkills loads <skills-root>/<name>/SKILL.md directories (the
// top-level ~/.zbot/skills since relocate-skills-hooks). SKILL.md
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
		_, _, spec.Body, _ = ParseSkillMD(string(raw)) //nolint:errcheck // perr already checked above on the same raw
		spec.Path = skillDir
		applyProvenance(&spec, skillDir)
		if existing, dup := snap.Skills[spec.Name]; dup {
			addErr(errs, skillMD, KindSkill, fmt.Errorf("duplicate skill name %q (keeping %s)", spec.Name, existing.Path))
			continue
		}
		snap.Skills[spec.Name] = spec
	}
}
