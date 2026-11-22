package registry

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MCPSpec is an MCP server manifest loaded from config/mcps/*.yaml —
// a stdio server (Command + Args + Env) and/or a streamable HTTP server
// (URL). Disabled marks `enabled: false` manifests; the zero value is
// an enabled server (an absent manifest field means enabled).
type MCPSpec struct {
	Command  string            `yaml:"command,omitempty"`
	Args     []string          `yaml:"args,omitempty"`
	Env      map[string]string `yaml:"env,omitempty"`
	URL      string            `yaml:"url,omitempty"`
	Disabled bool              `yaml:"-"`

	Path string `yaml:"-"`
}

// mcpFileSpec is the on-disk shape: `enabled` is tri-state on disk
// (absent = enabled) and normalized into MCPSpec.Disabled on load.
type mcpFileSpec struct {
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	URL     string            `yaml:"url,omitempty"`
	Enabled *bool             `yaml:"enabled,omitempty"`
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
		var file mcpFileSpec
		dec := yaml.NewDecoder(strings.NewReader(string(raw)))
		dec.KnownFields(true)
		if err := dec.Decode(&file); err != nil {
			addErr(errs, path, KindMCP, fmt.Errorf("decode: %w", err))
			continue
		}
		spec := MCPSpec{
			Command: file.Command, Args: file.Args, Env: file.Env, URL: file.URL,
			Disabled: file.Enabled != nil && !*file.Enabled,
		}
		// At-least-one transport: stdio (command) and/or streamable http
		// (url); declaring neither is the only invalid combination.
		if spec.Command == "" && spec.URL == "" {
			addErr(errs, path, KindMCP, fmt.Errorf("at least one of command (stdio) or url (streamable http) is required; both is valid"))
			continue
		}
		if len(snap.MCPs) >= mcpCapacity {
			addErr(errs, path, KindMCP, fmt.Errorf("mcp capacity %d exceeded; file overflows the managed list (sorted order decides the losers)", mcpCapacity))
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
