package wardtools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/tools"
	"zbot/backend/internal/wards"
)

// UseTool is ward_use(name): enter an existing ward (exact match) and
// set the session's sticky ward. Root-only (the engine's inventory
// decides who sees it). The tool RESULT carries the Active Ward packet
// (constitution + layout + digest) — mid-run entry can't retro-edit
// the system prompt, so the packet IS the injection.
type UseTool struct {
	Host  *Host
	Vault *wards.Vault
}

// Def describes ward_use.
func (t UseTool) Def() Def {
	return Def{
		Name:        "ward_use",
		Description: "Enter an existing ward and make it this session's active workspace. The result carries the ward's constitution and layout template (Active Ward packet). Structural and root-only.",
		ParamsJSON:  `{"type":"object","properties":{"name":{"type":"string","description":"exact ward name from the ward index"}},"required":["name"]}`,
	}
}

// Exec enters the ward or refuses with a typed error.
func (t UseTool) Exec(ctx context.Context, argsJSON string) Result {
	var args struct{ Name string }
	if err := decodeArgs(argsJSON, &args); err != nil {
		return Result{Err: err}
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return Result{Err: errors.New("ward_use: name is required")}
	}
	infos, ierr := t.Vault.Index(ctx)
	if ierr != nil {
		return Result{Err: fmt.Errorf("ward_use: index vault: %w", ierr)}
	}
	roster := wardRoster(infos)
	if !roster[name] {
		return Result{Err: fmt.Errorf("ward_use: no ward named %q (available: %s)", name, strings.Join(sortedNames(roster), ", "))}
	}
	view, err := t.Host.enter(ctx, name)
	if err != nil {
		return Result{Err: fmt.Errorf("ward_use: %w", err)}
	}
	return Result{Output: Packet(view)}
}

// CreateTool is ward_create(name, archetype): instantiate a mini bot
// from .templates/<archetype>/ and enter it. Existing names, invalid
// shapes, and unknown archetypes refuse typed; the created ward must
// lint clean (a broken template is a surfaced build invariant).
type CreateTool struct {
	Host  *Host
	Vault *wards.Vault
}

// Def describes ward_create.
func (CreateTool) Def() Def {
	return Def{
		Name:        "ward_create",
		Description: "Create a new ward from an archetype template (generic or research) and make it the session's active workspace, then enter it. The result carries the new ward's Active Ward packet. Structural and root-only.",
		ParamsJSON:  `{"type":"object","properties":{"name":{"type":"string","description":"reusable domain category, kebab-case (e.g. stock-analysis)"},"archetype":{"type":"string","enum":["generic","research"],"description":"the template to instantiate from"}},"required":["name","archetype"]}`,
	}
}

// Exec instantiates and enters the ward.
func (t CreateTool) Exec(ctx context.Context, argsJSON string) Result {
	var args struct {
		Name      string
		Archetype string
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return Result{Err: err}
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return Result{Err: errors.New("ward_create: name is required")}
	}
	if !domain.ValidWardName(name) {
		return Result{Err: fmt.Errorf("ward_create: name %q must be a reusable domain category (kebab-case, e.g. stock-analysis; never task-specific)", name)}
	}
	archetype := strings.TrimSpace(args.Archetype)
	if archetype == "" {
		archetype = "generic"
	}
	if !contains(wards.Archetypes(), archetype) {
		return Result{Err: fmt.Errorf("ward_create: unknown archetype %q (available: %s)", archetype, strings.Join(wards.Archetypes(), ", "))}
	}
	if err := t.instantiate(ctx, name, archetype); err != nil {
		return Result{Err: fmt.Errorf("ward_create: %w", err)}
	}
	view, err := t.Host.enter(ctx, name)
	if err != nil {
		return Result{Err: fmt.Errorf("ward_create: enter after create: %w", err)}
	}
	return Result{Output: "Created ward " + name + " (" + archetype + " archetype).\n\n" + Packet(view)}
}

// instantiate copies .templates/<archetype>/ into <vault>/<name>/:
// canonical.md renames to <name>.md; every destination is written
// only when absent (the vault is user data; a name collision was
// already refused). Post-create lint must be clean.
func (t CreateTool) instantiate(ctx context.Context, name, archetype string) error {
	dest := filepath.Join(t.Vault.Root(), name)
	if _, err := os.Lstat(dest); err == nil {
		return fmt.Errorf("ward %q already exists", name)
	}
	tmplDir := filepath.Join(t.Vault.Root(), ".templates", archetype)
	entries, err := os.ReadDir(tmplDir)
	if err != nil {
		return fmt.Errorf("template %q unavailable: %w", archetype, err)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for _, e := range entries {
		src := filepath.Join(tmplDir, e.Name())
		target := e.Name()
		if target == "canonical.md" {
			target = name + ".md"
		}
		dst := filepath.Join(dest, target)
		if err := copyFileIfAbsent(src, dst); err != nil {
			return err
		}
	}
	// The created ward must lint clean against its own conf.
	raw, err := os.ReadFile(filepath.Join(dest, "ward-conf.yaml"))
	if err != nil {
		return fmt.Errorf("created ward missing conf: %w", err)
	}
	conf, perr := wards.ParseConf(raw, "ward-conf.yaml")
	if perr != nil {
		return fmt.Errorf("template conf invalid: %w", perr)
	}
	findings, lerr := wards.Lint(ctx, dest, name, &conf)
	if lerr != nil {
		return lerr
	}
	if len(findings) > 0 {
		return fmt.Errorf("created ward does not lint clean: %+v", findings)
	}
	return nil
}

// copyFileIfAbsent copies one file only when the destination does not
// exist (concurrent creation wins; we never overwrite user data).
func copyFileIfAbsent(src, dst string) error {
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	if _, err := f.Write(raw); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

func wardRoster(infos []wards.WardInfo) map[string]bool {
	set := map[string]bool{}
	for _, wi := range infos {
		set[wi.Name] = true
	}
	return set
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	if len(out) == 0 {
		return []string{"(none — vault empty)"}
	}
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// WardsTool is the read-only ward INDEX tool: lists the vault's wards
// (name — archetype — canonical title). The chat research protocol
// uses it to decide use-existing vs create-new before calling the
// structural tools.
type WardsTool struct {
	Vault *wards.Vault
}

// Def describes wards.
func (t WardsTool) Def() Def {
	return Def{
		Name:        "wards",
		Description: "List the ward vault's index: every ward with its name, archetype, and canonical-page title. Read-only. Use it to decide whether an existing ward covers a domain before creating a new one.",
		ParamsJSON:  `{"type":"object","properties":{}}`,
	}
}

// Exec lists the vault.
func (t WardsTool) Exec(ctx context.Context, _ string) Result {
	infos, err := t.Vault.Index(ctx)
	if err != nil {
		return Result{Err: fmt.Errorf("wards: index: %w", err)}
	}
	if len(infos) == 0 {
		return Result{Output: "(vault empty — no wards yet)"}
	}
	lines := make([]string, len(infos))
	for i, wi := range infos {
		line := wi.Name + " — " + wi.Archetype
		if wi.CanonicalTitle != "" {
			line += " — " + wi.CanonicalTitle
		}
		lines[i] = line
	}
	return Result{Output: strings.Join(lines, "\n")}
}

// ChatToolSet builds the ward tool plane for a CHAT session: the index
// tool, the structural tools, and the file tools, all over one sticky
// host. (The engine's root/child split does not apply here — the chat
// user IS the root.)
func ChatToolSet(host *Host) []tools.Tool {
	files := FileTools{Host: host}
	return []tools.Tool{
		WardsTool{Vault: host.Vault()},
		UseTool{Host: host, Vault: host.Vault()},
		CreateTool{Host: host, Vault: host.Vault()},
		ReadTool{F: files},
		WriteTool{F: files},
		ListTool{F: files},
	}
}
