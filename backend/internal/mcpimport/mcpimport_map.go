package mcpimport

// The entry-mapping half: applies the normative field-policy table to
// one parsed entry (only command/args/env/url reach the spec; auth is
// never trusted) with skip/fail/warn actions.

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sort"
	"strings"

	"zbot/backend/internal/registry"
)

// Action is the per-entry field-policy verdict before any create.
type Action struct {
	Skip     string // skip reason (disabled, other_os) — "" when not skipping
	Fail     string // failure reason (shape, field) — "" when not failing
	FailMsg  string
	Warnings []string
}

// MapEntry applies the normative field-policy table to one entry.
// Only command/args/env/url can reach the spec.
func MapEntry(kv KV) (registry.MCPSpec, Action) {
	var spec registry.MCPSpec
	act := Action{}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(kv.Raw, &fields); err != nil {
		act.Fail, act.FailMsg = ReasonShape, "the entry is not a JSON object"
		return spec, act
	}
	warn := func(format string, args ...any) {
		act.Warnings = append(act.Warnings, fmt.Sprintf(format, args...))
	}

	// Skip controls run in a FIXED order (disabled, then os) so an
	// entry carrying both reports deterministically — never the map
	// iteration order.
	if raw, ok := fields["disabled"]; ok {
		var b bool
		if json.Unmarshal(raw, &b) == nil && b {
			act.Skip = ReasonDisabled
			return spec, act
		}
		if rawIsNull(raw) {
			warn("ignored disabled (null)")
		} else {
			warn("ignored disabled (not a boolean true)")
		}
	}
	if raw, ok := fields["os"]; ok {
		match, ok2 := osMatchesPlatform(raw, warn)
		if ok2 && !match {
			act.Skip = ReasonOtherOS
			return spec, act
		}
	}

	// Data fields — strictly typed; null never coerces.
	if raw, ok := fields["command"]; ok {
		cmd, err := decodeString(raw)
		if err != nil {
			act.Fail, act.FailMsg = ReasonField, "command "+err.Error()+" (if you meant a server NAMED \"command\", wrap it in an mcpServers block)"
			return spec, act
		}
		if cmd == "" {
			act.Fail, act.FailMsg = ReasonField, "command must not be empty"
			return spec, act
		}
		spec.Command = cmd
	}
	if raw, ok := fields["url"]; ok {
		url, err := decodeString(raw)
		if err != nil {
			act.Fail, act.FailMsg = ReasonField, "url "+err.Error()
			return spec, act
		}
		spec.URL = url
	}
	if raw, ok := fields["args"]; ok {
		if rawIsNull(raw) {
			act.Fail, act.FailMsg = ReasonField, "args must be an array of strings (null is not)"
			return spec, act
		}
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			act.Fail, act.FailMsg = ReasonField, "args must be an array of strings"
			return spec, act
		}
		args := make([]string, 0, len(arr))
		for _, item := range arr {
			arg, err := decodeString(item)
			if err != nil {
				act.Fail, act.FailMsg = ReasonField, "args "+err.Error()
				return spec, act
			}
			args = append(args, arg)
		}
		spec.Args = args
	}
	if raw, ok := fields["env"]; ok {
		env, warns, err := mapEnv(raw)
		if err != nil {
			act.Fail, act.FailMsg = ReasonField, err.Error()
			return spec, act
		}
		spec.Env = env
		act.Warnings = append(act.Warnings, warns...)
	}

	// At-least-one transport.
	if spec.Command == "" && spec.URL == "" {
		act.Fail, act.FailMsg = ReasonField, "at least one transport is required (command and/or url)"
		return spec, act
	}

	// Tolerance warnings in a FIXED key order (type first — it carries
	// the transport hint — then the known foreign keys, then unknown
	// keys sorted): values are never echoed, only key names.
	tolerated := []string{"type", "headers", "cwd", "autoApprove", "alwaysAllow", "path", "file"}
	var unknown []string
	for key := range fields {
		switch key {
		case "command", "args", "env", "url", "disabled", "os":
		case "type", "headers", "cwd", "autoApprove", "alwaysAllow", "path", "file":
		default:
			if strings.HasPrefix(key, "__") || key == "comment" || key == "_comment" {
				tolerated = append(tolerated, key)
			} else {
				unknown = append(unknown, key)
			}
		}
	}
	sort.Strings(unknown)
	tolerated = append(tolerated, unknown...)
	for _, key := range tolerated {
		raw, ok := fields[key]
		if !ok {
			continue
		}
		switch key {
		case "type":
			var t string
			if err := json.Unmarshal(raw, &t); err != nil {
				warn("ignored type (not a string); transport inferred from fields")
				continue
			}
			switch t {
			case "stdio", "http":
				warn("ignored type %q; transport inferred from fields", t)
			case "sse":
				warn("type sse is deprecated; the url imports as streamable HTTP — run Test connection to verify it")
			default:
				warn("ignored unknown type value; transport inferred from fields")
			}
			if (t == "http" && spec.URL == "") || (t == "stdio" && spec.Command == "") {
				warn("type %q conflicts with the declared fields; the fields win", t)
			}
		case "headers":
			warn("ignored headers (HTTP headers are not supported on managed manifests)")
		case "cwd", "autoApprove", "alwaysAllow":
			warn("ignored %s (not supported on managed manifests)", key)
		case "path", "file":
			warn("ignored %s (authored paths are never trusted; the manifest lives under config/mcps/)", key)
		default:
			if strings.HasPrefix(key, "__") || key == "comment" || key == "_comment" {
				warn("ignored comment key %q", key)
			} else {
				warn("ignored unknown key %q", key)
			}
		}
	}
	return spec, act
}

// mapEnv strictly maps env: strings pass; number/boolean literals
// coerce from their exact source text (json.Number) with a warning
// that names the key only — values never appear in warnings. Null,
// arrays, and objects fail the entry.
func mapEnv(raw json.RawMessage) (map[string]string, []string, error) {
	if rawIsNull(raw) {
		return nil, nil, fmt.Errorf("env must be an object (null is not)")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, nil, fmt.Errorf("env must be an object")
	}
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys) // deterministic warning order
	env := make(map[string]string, len(keys))
	var warns []string
	for _, k := range keys {
		v := fields[k]
		if rawIsNull(v) {
			return nil, nil, fmt.Errorf("env %s must be a string, number, or boolean (null is not)", k)
		}
		var s string
		if err := json.Unmarshal(v, &s); err == nil {
			env[k] = s
			continue
		}
		var num json.Number
		if err := json.Unmarshal(v, &num); err == nil {
			env[k] = num.String() // exact literal into the manifest
			warns = append(warns, fmt.Sprintf("env %s: number coerced to string", k))
			continue
		}
		var b bool
		if err := json.Unmarshal(v, &b); err == nil {
			env[k] = fmt.Sprintf("%t", b)
			warns = append(warns, fmt.Sprintf("env %s: boolean coerced to string", k))
			continue
		}
		return nil, nil, fmt.Errorf("env %s must be a string, number, or boolean", k)
	}
	return env, warns, nil
}

// osMatchesPlatform normalizes os selectors (win32/win/windows→windows,
// darwin/mac/macos/apple→darwin, linux→linux; tokens are never echoed).
// Absent-like values (null, empty) match. A present, non-empty selector
// with no recognized token does NOT match (skip); recognized tokens
// match only when one equals the platform. Malformed selectors warn and
// match.
func osMatchesPlatform(raw json.RawMessage, warn func(string, ...any)) (match bool, ok bool) {
	if rawIsNull(raw) {
		return true, true // null: treat as absent
	}
	var tokens []string
	var one string
	if err := json.Unmarshal(raw, &one); err == nil {
		tokens = []string{one}
	} else {
		var arr []json.RawMessage
		if err := json.Unmarshal(raw, &arr); err != nil {
			warn("ignored os (not a string or array); importing")
			return true, true
		}
		for _, item := range arr {
			var str string
			if err := json.Unmarshal(item, &str); err == nil && !rawIsNull(item) {
				tokens = append(tokens, str)
			}
		}
	}
	if len(tokens) == 0 {
		return true, true // empty: no constraint
	}
	normalize := func(t string) string {
		switch strings.ToLower(t) {
		case "win32", "win", "windows":
			return "windows"
		case "darwin", "mac", "macos", "apple":
			return "darwin"
		case "linux":
			return "linux"
		}
		return ""
	}
	matched := false
	recognized := false
	for _, t := range tokens {
		n := normalize(t)
		if n == "" {
			warn("ignored unrecognized os value")
			continue
		}
		recognized = true
		if n == runtime.GOOS {
			matched = true
		}
	}
	if !recognized {
		// Present, non-empty, nothing recognized: does not match.
		return false, true
	}
	return matched, true
}
