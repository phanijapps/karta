package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AgentSpec is a declarative sub-agent definition loaded from
// config/agents. The system prompt is either literal (SystemPrompt) or a
// reference to a prompt template by name (Prompt).
type AgentSpec struct {
	Name         string   `yaml:"name"`
	SystemPrompt string   `yaml:"system_prompt"`
	Prompt       string   `yaml:"prompt"`
	Model        string   `yaml:"model,omitempty"`
	Tools        []string `yaml:"tools,omitempty"`
	// Description is the when-to-delegate hint surfaced in the delegate
	// tool's description; an agent without one is not offered for
	// delegation (it stays usable via goal requests).
	Description string `yaml:"description,omitempty"`
	// DisplayName is an optional UI label.
	DisplayName string `yaml:"display_name,omitempty"`
	// MaxDepth grants delegation: the agent MAY spawn children while its
	// depth stays below MaxDepth (0 = may not delegate). A config hard cap
	// bounds the tree regardless.
	MaxDepth int `yaml:"max_depth,omitempty"`

	Path string `yaml:"-"`
}

// loadAgents loads config/agents/*.yaml plus *.md files whose YAML
// frontmatter carries the spec (the markdown body becomes the system
// prompt). The first spec (lexicographic file order) wins a duplicate-name
// conflict; the conflict is reported with both paths.
func loadAgents(snap *Snapshot, dir string, errs *[]LoadError) {
	for _, e := range readKindDir(dir, KindAgent, errs) {
		if e.IsDir() || skipEntry(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		var spec AgentSpec
		switch {
		case isYAML(e.Name()):
			raw, err := os.ReadFile(path)
			if err != nil {
				addErr(errs, path, KindAgent, err)
				continue
			}
			dec := yaml.NewDecoder(strings.NewReader(string(raw)))
			dec.KnownFields(true)
			if err := dec.Decode(&spec); err != nil {
				addErr(errs, path, KindAgent, fmt.Errorf("decode: %w", err))
				continue
			}
		case filepath.Ext(e.Name()) == ".md":
			raw, err := os.ReadFile(path)
			if err != nil {
				addErr(errs, path, KindAgent, err)
				continue
			}
			fm, body, ok := splitFrontmatter(string(raw))
			if !ok {
				addErr(errs, path, KindAgent, fmt.Errorf("no YAML frontmatter"))
				continue
			}
			if err := decodeFrontmatter(fm, &spec); err != nil {
				addErr(errs, path, KindAgent, fmt.Errorf("frontmatter decode: %w", err))
				continue
			}
			if spec.SystemPrompt == "" {
				spec.SystemPrompt = strings.TrimSpace(body)
			}
		default:
			continue
		}
		if spec.Name == "" {
			addErr(errs, path, KindAgent, fmt.Errorf("agent name is required"))
			continue
		}
		spec.Path = path
		if existing, dup := snap.Agents[spec.Name]; dup {
			addErr(errs, path, KindAgent, fmt.Errorf("duplicate agent name %q (keeping %s)", spec.Name, existing.Path))
			continue
		}
		snap.Agents[spec.Name] = spec
	}
}
