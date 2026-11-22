package mcpimport

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// --- parser and classifier (moved from internal/server; behavior unchanged) --

// --- parser and classifier (design D1–D2) ---------------------------------

func TestParseImportOfficialQuickstart(t *testing.T) {
	doc := `{"mcpServers": {"filesystem": {"command": "npx", "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"], "env": {"KEY": "v"}}}}`
	kvs, err := ParseDocument(doc)
	if err != nil {
		t.Fatal(err)
	}
	entries, _, err := Classify(kvs, doc, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Key != "filesystem" {
		t.Fatalf("entries wrong: %+v", entries)
	}
	spec, act := MapEntry(entries[0])
	if act.Skip != "" || act.Fail != "" {
		t.Fatalf("action wrong: %+v", act)
	}
	if spec.Command != "npx" || len(spec.Args) != 3 || spec.Env["KEY"] != "v" {
		t.Fatalf("spec wrong: %+v", spec)
	}
	if len(act.Warnings) != 0 {
		t.Fatalf("clean entry warned: %v", act.Warnings)
	}
}

func TestParseImportPreservesPasteOrderIncludingIntegerKeys(t *testing.T) {
	doc := `{"mcpServers": {"2": {"command": "two"}, "1": {"command": "one"}}}`
	kvs, _ := ParseDocument(doc)
	entries, _, err := Classify(kvs, doc, "")
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Key != "2" || entries[1].Key != "1" {
		t.Fatalf("paste order lost: %+v", entries)
	}
}

func TestParseImportDocumentRejections(t *testing.T) {
	cases := map[string]string{
		"empty":                    "   ",
		"not json":                 "mcpServers: {}",
		"array":                    `[{"command": "x"}]`,
		"scalar":                   `"hello"`,
		"trailing content":         `{"mcpServers": {}} trailing`,
		"bom":                      "\ufeff" + `{"mcpServers": {}}`,
		"wrapper array":            `{"mcpServers": []}`,
		"empty wrapper with alias": `{"mcpServers": {}, "servers": {"a": {"command": "x"}}}`,
		"no recognizable servers":  `{"a": 42}`,
		"duplicate keys":           `{"mcpServers": {"fs": {"command": "x"}, "fs": {"command": "y"}}}`,
		"escaped duplicate keys":   `{"mcpServers": {"fs": {"command": "x"}, "\u0066s": {"command": "y"}}}`,
		"nested duplicate keys":    `{"mcpServers": {"fs": {"command": "x", "env": {"A": "1", "A": "2"}}}}`,
	}
	for name, doc := range cases {
		if _, err := ParseDocument(doc); err == nil {
			// The bare `{"mcpServers": {}}` (empty, no alias) and doc-level
			// shape checks happen across parse+classify; run both here.
			kvs, perr := ParseDocument(doc)
			if perr != nil {
				t.Errorf("%s: parse passed unexpectedly", name)
				continue
			}
			if _, _, cerr := Classify(kvs, doc, ""); cerr == nil {
				t.Errorf("%s: expected a document rejection, got none", name)
			}
		}
	}
}

func TestParseImportSizeAndCountCaps(t *testing.T) {
	big := `{"mcpServers": {"x": {"command": "` + strings.Repeat("a", 256*1024) + `"}}}`
	if _, err := ParseDocument(big); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("size cap: %v", err)
	}
	var sb strings.Builder
	sb.WriteString(`{"mcpServers": {`)
	for i := 0; i < 33; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"s` + string(rune('a'+i%26)) + string(rune('0'+i/26)) + `": {"command": "x"}`)
	}
	sb.WriteString(`}}`)
	kvs, err := ParseDocument(sb.String())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := Classify(kvs, sb.String(), ""); err == nil || !strings.Contains(err.Error(), "cap is 32") {
		t.Fatalf("count cap: %v", err)
	}
	// Exactly 32 passes.
	sb.Reset()
	sb.WriteString(`{"mcpServers": {`)
	for i := 0; i < 32; i++ {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`"s` + string(rune('a'+i%26)) + string(rune('0'+i/26)) + `": {"command": "x"}`)
	}
	sb.WriteString(`}}`)
	kvs, err = ParseDocument(sb.String())
	if err != nil {
		t.Fatal(err)
	}
	if entries, _, err := Classify(kvs, sb.String(), ""); err != nil || len(entries) != 32 {
		t.Fatalf("32 entries should pass: %d %v", len(entries), err)
	}
}

func TestParseImportBoundaryAndEncoding(t *testing.T) {
	// Exactly at the byte cap parses; one over rejects.
	exact := `{"mcpServers": {"x": {"command": "` + strings.Repeat("a", MaxBytes-len(`{"mcpServers": {"x": {"command": "`)-len(`"}}}`)) + `"}}}`
	if len(exact) != MaxBytes {
		t.Fatalf("fixture sizing: %d", len(exact))
	}
	if _, err := ParseDocument(exact); err != nil {
		t.Fatalf("exact-cap document should parse: %v", err)
	}
	if _, err := ParseDocument(exact + " "); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("over-cap should reject: %v", err)
	}
	// Invalid UTF-8 rejects.
	if _, err := ParseDocument("{\xff}"); err == nil || !strings.Contains(err.Error(), "UTF-8") {
		t.Fatalf("invalid UTF-8 should reject: %v", err)
	}
}

func TestClassifyImportShapes(t *testing.T) {
	// Wrapper presence: malformed primary wrapper never falls back.
	kvs, _ := ParseDocument(`{"mcpServers": [], "servers": {"a": {"command": "x"}}}`)
	if _, _, err := Classify(kvs, `{"mcpServers": [], "servers": {"a": {"command": "x"}}}`, ""); err == nil {
		t.Fatal("malformed wrapper must reject with no fallback")
	}
	// servers alias alone works.
	kvs, _ = ParseDocument(`{"servers": {"a": {"command": "x"}}}`)
	entries, _, err := Classify(kvs, `{"servers": {"a": {"command": "x"}}}`, "")
	if err != nil || len(entries) != 1 || entries[0].Key != "a" {
		t.Fatalf("servers alias: %+v %v", entries, err)
	}
	// Single-server document with a name.
	kvs, _ = ParseDocument(`{"command": "x"}`)
	entries, wasSingle, err := Classify(kvs, `{"command": "x"}`, "solo")
	if err != nil || !wasSingle || entries[0].Key != "solo" {
		t.Fatalf("single: %+v %v", entries, err)
	}
	// Single without a name rejects.
	if _, _, err := Classify(kvs, `{"command": "x"}`, ""); err == nil {
		t.Fatal("single without name must reject")
	}
	// Bare map with one bad scalar entry still classifies; the bad entry
	// fails per-entry (isolation).
	kvs, _ = ParseDocument(`{"good": {"command": "x"}, "bad": 42}`)
	entries, _, err = Classify(kvs, `{"good": {"command": "x"}, "bad": 42}`, "")
	if err != nil || len(entries) != 2 {
		t.Fatalf("mixed bare map: %+v %v", entries, err)
	}
	if _, act := MapEntry(entries[1]); act.Fail != ReasonShape {
		t.Fatalf("bad entry should fail shape: %+v", act)
	}
	// Top-level command resolves the ambiguous shape toward single-server:
	// the DOCUMENT is the entry, so its object-valued command fails.
	docAmbiguous := `{"command": {"command": "x"}}`
	kvs, _ = ParseDocument(docAmbiguous)
	entriesAmb, _, errAmb := Classify(kvs, docAmbiguous, "k")
	if errAmb != nil || len(entriesAmb) != 1 {
		t.Fatalf("ambiguous classification: %+v %v", entries, err)
	}
	if _, act := MapEntry(entriesAmb[0]); act.Fail != ReasonField || !strings.Contains(act.FailMsg, "wrap it in an mcpServers block") {
		t.Fatalf("ambiguous single needs wrapping guidance: %+v", act)
	}
}

// --- field policy (design D3) ----------------------------------------------

func warningJoin(w []string) string { return strings.Join(w, "\n") }

func TestMapImportEntryFieldPolicy(t *testing.T) {
	// Foreign keys warn, naming keys, NEVER echoing values.
	raw := `{"url": "http://127.0.0.1:1/mcp", "headers": {"Authorization": "Bearer SECRET"}, "autoApprove": true}`
	spec, act := MapEntry(KV{Key: "x", Raw: json.RawMessage(raw)})
	if act.Skip != "" || act.Fail != "" {
		t.Fatalf("foreign keys blocked: %+v", act)
	}
	if spec.URL == "" {
		t.Fatal("url not mapped")
	}
	w := warningJoin(act.Warnings)
	if !strings.Contains(w, "headers") || !strings.Contains(w, "autoApprove") {
		t.Fatalf("warnings must name keys: %q", w)
	}
	if strings.Contains(w, "SECRET") {
		t.Fatalf("warnings must not echo values: %q", w)
	}
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "type": "carrier-pigeon", "os": ["beos"]}`)})
	w2 := warningJoin(act.Warnings)
	if strings.Contains(w2, "carrier-pigeon") || strings.Contains(w2, "beos") {
		t.Fatalf("type/os warnings must not echo values: %q", w2)
	}
	if act.Skip != ReasonOtherOS {
		t.Fatalf("unrecognized os should skip: %+v", act)
	}

	// sse deprecation; type conflict with fields.
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"url": "http://x/mcp", "type": "sse"}`)})
	if !strings.Contains(warningJoin(act.Warnings), "sse is deprecated") {
		t.Fatalf("sse warning missing: %v", act.Warnings)
	}
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "type": "http"}`)})
	if !strings.Contains(warningJoin(act.Warnings), "fields win") {
		t.Fatalf("conflict warning missing: %v", act.Warnings)
	}

	// disabled: true skips; "true" (string) warns and imports.
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "disabled": true}`)})
	if act.Skip != ReasonDisabled {
		t.Fatalf("disabled true should skip: %+v", act)
	}
	spec, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "disabled": "true"}`)})
	if act.Skip != "" || spec.Command != "c" {
		t.Fatalf("disabled string should import: %+v", act)
	}
	if !strings.Contains(warningJoin(act.Warnings), "disabled") {
		t.Fatalf("disabled string should warn: %v", act.Warnings)
	}

	// os policy: non-matching platform skips; null/empty import; win32
	// normalizes to windows; unrecognized warns and imports.
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "os": ["win32"]}`)})
	if act.Skip != ReasonOtherOS {
		t.Fatalf("os win32 on linux should skip: %+v", act)
	}
	spec, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "os": null}`)})
	if act.Skip != "" || spec.Command != "c" {
		t.Fatalf("os null should import: %+v", act)
	}
	if _, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "os": ["` + goosString() + `"]}`)}); act.Skip != "" {
		t.Fatalf("os matching should import: %+v", act)
	}
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "os": ["plan9"]}`)})
	if act.Skip != ReasonOtherOS || !strings.Contains(warningJoin(act.Warnings), "unrecognized os value") {
		t.Fatalf("unrecognized os should warn and skip (present, non-empty, not matching): %+v", act)
	}

	// Env coercion: exact number literal; bool; structured fails.
	spec, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "env": {"BIG": 9007199254740993, "FLAG": true}}`)})
	if act.Fail != "" {
		t.Fatalf("scalar env should coerce: %+v", act)
	}
	if spec.Env["BIG"] != "9007199254740993" || spec.Env["FLAG"] != "true" {
		t.Fatalf("coercion wrong: %+v", spec.Env)
	}
	if !strings.Contains(warningJoin(act.Warnings), "BIG") {
		t.Fatalf("coercion warning missing: %v", act.Warnings)
	}
	if strings.Contains(warningJoin(act.Warnings), "9007199254740993") || strings.Contains(warningJoin(act.Warnings), "true") {
		t.Fatalf("coercion warnings must not echo values: %v", act.Warnings)
	}
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "env": {"BAD": [1]}}`)})
	if act.Fail != ReasonField {
		t.Fatalf("structured env should fail: %+v", act)
	}

	// Field failures: non-string command, non-string args, empty
	// command, neither transport.
	for name, doc := range map[string]string{
		"command object":  `{"command": {"command": "x"}}`,
		"args non-string": `{"command": "c", "args": [1]}`,
		"empty command":   `{"command": "", "url": "http://x/mcp"}`,
		"no transport":    `{"args": ["x"]}`,
	} {
		_, act := MapEntry(KV{Key: "x", Raw: json.RawMessage(doc)})
		if act.Fail != ReasonField {
			t.Errorf("%s: want field failure, got %+v", name, act)
		}
	}

	// Skip controls are deterministic: disabled outranks os regardless
	// of map iteration order.
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "os": ["win32"], "disabled": true}`)})
	if act.Skip != ReasonDisabled {
		t.Fatalf("disabled must outrank os: %+v", act)
	}

	// Null never coerces: command/url/args/env nulls fail the entry.
	for name, doc := range map[string]string{
		"null command": `{"command": null, "url": "http://x/mcp"}`,
		"null url":     `{"command": "c", "url": null}`,
		"null args":    `{"command": "c", "args": null}`,
		"null arg":     `{"command": "c", "args": ["a", null]}`,
		"null env":     `{"command": "c", "env": null}`,
		"null value":   `{"command": "c", "env": {"TOKEN": null}}`,
	} {
		_, act := MapEntry(KV{Key: "x", Raw: json.RawMessage(doc)})
		if act.Fail != ReasonField {
			t.Errorf("%s: want field failure, got %+v", name, act)
		}
	}

	// Authored path/file keys warn and never reach the spec.
	_, act = MapEntry(KV{Key: "x", Raw: json.RawMessage(`{"command": "c", "path": "/etc"}`)})
	if act.Fail != "" || !strings.Contains(warningJoin(act.Warnings), "never trusted") {
		t.Fatalf("authored path handling wrong: %+v", act)
	}
}

func goosString() string { return runtime.GOOS }
