package mcpimport

// Raw-text mcpServers import (design D1–D4 of refine-mcp-server-setup).
// The paste arrives as a string; this package is the ONLY parser: an
// order-preserving token walk keeps paste order, exact number literals,
// and detects duplicate keys (Unicode-escaped equivalents included) at
// every depth. Classification is by wrapper PRESENCE and is strictly
// separate from per-entry validation — a malformed entry is a per-entry
// failure, never a document rejection. Only command/args/env/url ever
// reach a manifest; every other key follows the normative field-policy
// table (mcp-import spec) with warnings that name keys, never values.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

// Document caps: max paste size and max top-level entries.
const (
	MaxBytes        = 256 * 1024
	MaxEntries      = 32
	ReasonDuplicate = "duplicate"
	ReasonDisabled  = "disabled"
	ReasonOtherOS   = "other_os"
	ReasonShape     = "shape"
	ReasonField     = "field"
	ReasonName      = "name"
	ReasonCapacity  = "capacity"
	ReasonIO        = "io_error"
	StatusCreated   = "created"
	StatusSkipped   = "skipped"
	StatusFailed    = "failed"
)

// DocError is a whole-document rejection (typed 400, nothing
// created). Every document-level check completes before the first create.
type DocError struct{ Msg string }

func (e *DocError) Error() string { return e.Msg }

// NewDocError builds a whole-document rejection (typed 400, nothing created).
func NewDocError(format string, args ...any) *DocError {
	return &DocError{Msg: fmt.Sprintf(format, args...)}
}

// KV is one ordered key/value pair from the paste.
type KV struct {
	Key string
	Raw json.RawMessage
}

// ParseDocument validates the raw text and returns the
// top-level object's pairs in paste order.
func ParseDocument(text string) ([]KV, error) {
	if strings.TrimSpace(text) == "" {
		return nil, NewDocError("the document is empty")
	}
	if len(text) > MaxBytes {
		return nil, NewDocError("the document exceeds %d bytes", MaxBytes)
	}
	if strings.HasPrefix(text, "\ufeff") {
		return nil, NewDocError("the document starts with a UTF-8 BOM; save without it")
	}
	if !utf8.ValidString(text) {
		return nil, NewDocError("the document is not valid UTF-8")
	}
	if err := scanDuplicateKeys(text); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	kvs, err := walkObject(dec)
	if err != nil {
		return nil, NewDocError("malformed JSON: %v", err)
	}
	// Complete-parse-through-EOF: anything after the object rejects.
	if tok, err := dec.Token(); err != io.EOF {
		return nil, NewDocError("trailing content after the document (%v)", tok)
	}
	return kvs, nil
}

// walkObject reads one object's pairs in order. The opening '{' has
// already been consumed by the caller in the single-shot paths; here we
// read it ourselves for nested walks.
func walkObject(dec *json.Decoder) ([]KV, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("expected an object, found %v", tok)
	}
	var kvs []KV
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		key, ok := keyTok.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected object key %v", keyTok)
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		kvs = append(kvs, KV{Key: key, Raw: raw})
	}
	if _, err := dec.Token(); err != nil { // closing '}'
		return nil, err
	}
	return kvs, nil
}

// scanDuplicateKeys token-walks the whole document (objects and arrays,
// every depth) and rejects any duplicated object key — the tokenizer
// decodes escapes, so `"fs"` and `"\u0066s"` collide as duplicates.
func scanDuplicateKeys(text string) error {
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	var scan func() error
	scan = func() error {
		tok, err := dec.Token()
		if err != nil {
			return err
		}
		if delim, ok := tok.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for dec.More() {
					keyTok, err := dec.Token()
					if err != nil {
						return err
					}
					key := keyTok.(string)
					if seen[key] {
						return NewDocError("duplicate key %q in the document", key)
					}
					seen[key] = true
					if err := scan(); err != nil { // value
						return err
					}
				}
				_, err = dec.Token() // '}'
				return err
			case '[':
				for dec.More() {
					if err := scan(); err != nil {
						return err
					}
				}
				_, err = dec.Token() // ']'
				return err
			}
		}
		return nil // scalar (decoded with UseNumber, discarded)
	}
	if err := scan(); err != nil {
		var ide *DocError
		if errors.As(err, &ide) {
			return err
		}
		return NewDocError("malformed JSON: %v", err)
	}
	if tok, err := dec.Token(); err != io.EOF {
		return NewDocError("trailing content after the document (%v)", tok)
	}
	return nil
}

// Classify resolves the document shape per the presence ladder.
// Returns the entries (paste order) and whether the paste was a single
// server object (whose name came from the request).
func Classify(kvs []KV, document string, singleName string) ([]KV, bool, error) {
	lookup := func(key string) (json.RawMessage, bool) {
		for _, kv := range kvs {
			if kv.Key == key {
				return kv.Raw, true
			}
		}
		return nil, false
	}
	// Wrapper presence is authoritative — no fallback to the alias.
	for _, wrapper := range []string{"mcpServers", "servers"} {
		if raw, present := lookup(wrapper); present {
			inner, err := walkObject(json.NewDecoder(strings.NewReader(string(raw))))
			if err != nil {
				return nil, false, NewDocError("%s must be an object", wrapper)
			}
			if len(inner) == 0 {
				return nil, false, NewDocError("%s contains no servers", wrapper)
			}
			return capEntries(inner)
		}
	}
	// Single-server document: a top-level command/url key wins — the
	// genuinely ambiguous bare-map-named-"command" case resolves here,
	// and the error text tells the user to wrap it.
	if _, hasCommand := lookup("command"); hasCommand {
		return singleEntry(document, singleName)
	}
	if _, hasURL := lookup("url"); hasURL {
		return singleEntry(document, singleName)
	}
	// Bare map iff at least one value is an object.
	anyObject := false
	for _, kv := range kvs {
		if strings.HasPrefix(strings.TrimSpace(string(kv.Raw)), "{") {
			anyObject = true
			break
		}
	}
	if !anyObject {
		return nil, false, NewDocError("no recognizable servers: expected an mcpServers wrapper, a name-to-server map, or a single server object")
	}
	return capEntries(kvs)
}

func singleEntry(document, singleName string) ([]KV, bool, error) {
	if strings.TrimSpace(singleName) == "" {
		return nil, false, NewDocError("a single-server paste needs a name (fill the name field)")
	}
	return []KV{{Key: singleName, Raw: json.RawMessage(document)}}, true, nil
}

func capEntries(kvs []KV) ([]KV, bool, error) {
	if len(kvs) > MaxEntries {
		return nil, false, NewDocError("the document holds %d servers; the import cap is %d", len(kvs), MaxEntries)
	}
	return kvs, false, nil
}

// rawIsNull reports a JSON null literal (Go's Unmarshal accepts null
// into strings silently — the field policy must not).
func rawIsNull(raw json.RawMessage) bool {
	return strings.TrimSpace(string(raw)) == "null"
}

// decodeString strictly decodes a string field; null and non-strings
// fail (never silently coerce to "").
func decodeString(raw json.RawMessage) (string, error) {
	if rawIsNull(raw) {
		return "", fmt.Errorf("must be a string (null is not)")
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("must be a string")
	}
	return s, nil
}
