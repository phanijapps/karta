// Package tools defines Karta's tool seam: small named units with a
// description, a parameters schema, and an exec function. The chat and
// engine layers consume tools exclusively through Provider
// implementations; skills, MCP servers, and subagent tools register
// additional providers later without consumer changes. This package
// ships the two builtins (clock.now, echo) — primitive functions of the
// daemon, not registry content.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// Def is one tool's static description (offered to models).
type Def struct {
	Name        string
	Description string
	// ParamsJSON is an OpenAI-style JSON-schema object for arguments.
	ParamsJSON string
}

// Result is one tool execution outcome; Err carries a typed failure the
// model can see (never panic, never secrets).
type Result struct {
	Output string
	Err    error
}

// Tool is one executable unit.
type Tool interface {
	Def() Def
	Exec(ctx context.Context, argsJSON string) Result
}

// Provider assembles the tools a consumer may offer and executes them
// by name. Unknown names return a typed error result.
type Provider interface {
	Tools() []Tool
}

// BuiltinProvider serves the compiled-in tools.
type BuiltinProvider struct{}

// Tools — Tools lists the builtin tools (clock_now, echo).
func (BuiltinProvider) Tools() []Tool { return []Tool{ClockNow{}, Echo{}} }

// ClockNow reports the current UTC time.
type ClockNow struct{}

// Def — Def describes the clock_now tool.
func (ClockNow) Def() Def {
	return Def{Name: "clock.now", Description: "Returns the current UTC time as RFC3339.",
		ParamsJSON: `{"type":"object","properties":{}}`}
}

// Exec — Exec returns the current wall-clock time.
func (ClockNow) Exec(_ context.Context, _ string) Result {
	return Result{Output: time.Now().UTC().Format(time.RFC3339)}
}

// Echo returns its input (bounded).
type Echo struct{}

// Def — Def describes the echo tool.
func (Echo) Def() Def {
	return Def{Name: "echo", Description: "Returns the provided text (bounded length).",
		ParamsJSON: `{"type":"object","properties":{"text":{"type":"string","maxLength":4096}},"required":["text"]}`}
}

// Exec — Exec returns the input arguments unchanged.
func (Echo) Exec(_ context.Context, argsJSON string) Result {
	var args struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return Result{Err: fmt.Errorf("echo: invalid args: %w", err)}
	}
	if args.Text == "" {
		return Result{Err: fmt.Errorf("echo: args require text")}
	}
	if len(args.Text) > 4096 {
		args.Text = args.Text[:4096]
	}
	return Result{Output: args.Text}
}

// Execute runs the named tool across a provider; unknown tools return a
// typed error result the model can see.
func Execute(ctx context.Context, p Provider, name, argsJSON string) Result {
	for _, tool := range p.Tools() {
		if tool.Def().Name == name {
			return tool.Exec(ctx, argsJSON)
		}
	}
	return Result{Err: fmt.Errorf("unknown tool %q", name)}
}

// Lookup finds a tool def by name (nil when absent).
func Lookup(p Provider, name string) *Def {
	for _, tool := range p.Tools() {
		if d := tool.Def(); d.Name == name {
			return &d
		}
	}
	return nil
}

// Multi merges providers into one tool surface. Duplicate tool names
// panic at composition time (a startup error by design — never a
// mid-run ambiguity); execution keeps the typed unknown-tool error.
type multiProvider struct{ providers []Provider }

// Multi returns a Provider serving every tool of every provider.
// Duplicate names panic HERE — at composition time (startup), never
// mid-run (final-gate finding: the panic must not lurk in Tools()).
func Multi(providers ...Provider) Provider {
	m := multiProvider{providers}
	_ = m.Tools() // eager duplicate-name validation
	return m
}

func (m multiProvider) Tools() []Tool {
	seen := map[string]bool{}
	var out []Tool
	for _, p := range m.providers {
		for _, t := range p.Tools() {
			name := t.Def().Name
			if seen[name] {
				panic("tools: duplicate tool name in Multi: " + name)
			}
			seen[name] = true
			out = append(out, t)
		}
	}
	return out
}
