package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MCPSpec is an MCP server manifest loaded from config/mcps/*.yaml —
// either a stdio server (Command + Args + Env) or a streamable HTTP server
// (URL), never both.
type MCPSpec struct {
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	URL     string            `yaml:"url,omitempty"`

	Path string `yaml:"-"`
}

// loadMCPs loads config/mcps/*.yaml manifests keyed by file stem.
func loadMCPs(snap *Snapshot, dir string, errs *[]LoadError) {
	for _, e := range readKindDir(dir, KindMCP, errs) {
		if e.IsDir() || skipEntry(e.Name()) || !isYAML(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(path)
		if err != nil {
			addErr(errs, path, KindMCP, err)
			continue
		}
		var spec MCPSpec
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(&spec); err != nil {
			addErr(errs, path, KindMCP, fmt.Errorf("decode: %w", err))
			continue
		}
		if (spec.Command == "") == (spec.URL == "") {
			addErr(errs, path, KindMCP, fmt.Errorf("exactly one of command (stdio) or url (streamable http) is required"))
			continue
		}
		spec.Path = path
		key := stem(e.Name())
		if existing, dup := snap.MCPs[key]; dup {
			addErr(errs, path, KindMCP, fmt.Errorf("duplicate mcp %q (keeping %s)", key, existing.Path))
			continue
		}
		snap.MCPs[key] = spec
	}
}
