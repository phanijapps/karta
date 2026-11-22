package chitta

import (
	"context"
	"encoding/json"
	"errors"

	"zbot/backend/internal/domain"
)

// Tool names (closed set; underscore namespaces, no dots).
const (
	ToolRemember     = "memory_remember"
	ToolRecall       = "memory_recall"
	ToolRevise       = "memory_revise"
	ToolHistory      = "memory_history"
	ToolConnect      = "knowledge_connect"
	ToolFindEntities = "knowledge_find_entities"
	ToolAbout        = "knowledge_about"
)

// reservedFields are host-assigned; supplying any of them is
// invalid_input (contracts "Host-stamped trust boundary").
var reservedFields = []string{
	"id", "provenance", "status", "created_at", "updated_at",
	"mention_count", "last_confirmed_at", "embedding",
}

// ToolDef is one tool: name, machine-readable input schema, handler.
type ToolDef struct {
	Name   string
	Schema map[string]any
}

// toolDef builds one tool definition from a property set.
func toolDef(name, desc string, props map[string]any, required ...string) ToolDef {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"description":          desc,
		"properties":           props,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	return ToolDef{Name: name, Schema: schema}
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }
func num(desc string) map[string]any {
	return map[string]any{"type": "number", "minimum": 0.0, "maximum": 1.0, "description": desc}
}
func intp(desc string) map[string]any {
	return map[string]any{"type": "integer", "minimum": 1, "maximum": 100, "description": desc}
}
func arr(item map[string]any, desc string) map[string]any {
	return map[string]any{"type": "array", "items": item, "description": desc}
}
func kindEnum() map[string]any {
	return map[string]any{"type": "string", "enum": []string{"fact", "preference", "correction", "observation", "procedure"}}
}

// Tools returns the closed tool inventory with input schemas derived
// from the transfer objects.
func Tools() []ToolDef {
	observation := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"properties": map[string]any{
			"kind":       kindEnum(),
			"content":    str("the distilled statement"),
			"subject":    str("who/what this is about (default: user)"),
			"tags":       arr(str("tag"), "optional tags"),
			"confidence": num("suggested confidence, clamped to [0,1]"),
			"entities":   arr(str("entity name or id"), "entity references to link"),
		},
		"required": []string{"kind", "content"},
	}
	return []ToolDef{
		toolDef(ToolRemember, "Remember a bounded batch of observations about the user or their world; merges exact duplicates, never resurrects corrections.",
			map[string]any{"observations": arr(observation, "batch, all-or-nothing")},
			"observations"),
		toolDef(ToolRecall, "Ranked hybrid retrieval over memory records with lane attribution (keyword/vector/graph).",
			map[string]any{
				"query":              str("textual query"),
				"kinds":              arr(kindEnum(), "kind filter"),
				"tags":               arr(str("tag"), "tag filter"),
				"subject":            str("subject filter"),
				"entity_ids":         arr(str("id"), "entity filter"),
				"since":              str("RFC3339 lower bound"),
				"until":              str("RFC3339 upper bound"),
				"limit":              intp("page size (default 10, max 100)"),
				"include_superseded": map[string]any{"type": "boolean"},
			},
			"query"),
		toolDef(ToolRevise, "Replace one active record; the revision inherits its kind unless an explicit kind is given.",
			map[string]any{
				"supersedes": str("id of the active record to supersede"),
				"kind":       kindEnum(),
				"content":    str("replacement statement"),
				"tags":       arr(str("tag"), "tags"),
				"confidence": num("replacement confidence"),
			},
			"supersedes", "content"),
		toolDef(ToolHistory, "Chronological audit view of records (superseded visible by default); ids filter retrieves directly.",
			map[string]any{
				"subject":     str("subject filter"),
				"kinds":       arr(kindEnum(), "kind filter"),
				"tags":        arr(str("tag"), "tag filter"),
				"ids":         arr(str("id"), "direct-id retrieval"),
				"active_only": map[string]any{"type": "boolean", "description": "hide superseded records"},
				"limit":       intp("page size"),
				"cursor":      str("pagination cursor"),
			}),
		toolDef(ToolConnect, "Record one typed, directed relationship between two entities (names or ids); idempotent.",
			map[string]any{
				"from":       str("entity name or id"),
				"to":         str("entity name or id"),
				"type":       str("snake_case verb, e.g. works_on"),
				"confidence": num("suggested confidence"),
			},
			"from", "to", "type"),
		toolDef(ToolFindEntities, "Search entity names and aliases by prefix and optional type.",
			map[string]any{
				"prefix": str("prefix query over names and aliases"),
				"type":   str("entity type filter"),
				"limit":  intp("page size"),
			}),
		toolDef(ToolAbout, "Dossier for one target (entity name/alias/id, else subject): aliases, referencing records, bounded graph neighborhood.",
			map[string]any{
				"target": str("entity name, alias, or id — else subject"),
				"depth":  map[string]any{"type": "integer", "minimum": 1, "maximum": 3, "description": "neighborhood depth (default 1)"},
				"types":  arr(str("verb"), "relationship type filter"),
				"limit":  intp("page size"),
			},
			"target"),
	}
}

// ToolNames returns the closed inventory.
func ToolNames() []string {
	defs := Tools()
	names := make([]string, 0, len(defs))
	for _, d := range defs {
		names = append(names, d.Name)
	}
	return names
}

// Dispatch executes one tool call. args is raw JSON input; host is the
// ONLY provenance source. Every invocation emits one observable event.
// The result is the tool's output transfer object as JSON.
func (s *System) Dispatch(ctx context.Context, tool string, args json.RawMessage, host domain.ChittaHostContext) (json.RawMessage, error) {
	s.mu.Lock()
	storeSnap := s.store
	s.mu.Unlock()
	if storeSnap == nil {
		return nil, closedErr
	}
	outcome := "success"
	var errCode string
	affected := []string{}
	defer func() {
		s.emit(ctx, tool, outcome, errCode, affected, host)
	}()

	fail := func(err error) (json.RawMessage, error) {
		outcome = "error"
		var ce *domain.ChittaError
		if errors.As(err, &ce) {
			errCode = string(ce.Code)
		}
		return nil, err
	}

	if err := rejectReserved(args); err != nil {
		return fail(err)
	}

	switch tool {
	case ToolRemember:
		var in struct {
			Observations []domain.Observation `json:"observations"`
		}
		if err := domain.DecodeChittaWrite(args, &in); err != nil {
			return fail(err)
		}
		results, err := s.Remember(ctx, in.Observations, host)
		if err != nil {
			return fail(err)
		}
		for _, r := range results {
			if r.Record != nil {
				affected = append(affected, r.Record.ID)
			}
		}
		return marshal(map[string]any{"results": results})

	case ToolRecall:
		var req domain.RecallRequest
		if err := domain.DecodeChittaWrite(args, &req); err != nil {
			return fail(err)
		}
		page, err := s.Recall(ctx, req, host)
		if err != nil {
			return fail(err)
		}
		return marshal(page)

	case ToolRevise:
		var req domain.ReviseRequest
		if err := domain.DecodeChittaWrite(args, &req); err != nil {
			return fail(err)
		}
		rec, err := s.Revise(ctx, req, host)
		if err != nil {
			return fail(err)
		}
		affected = []string{rec.ID, req.Supersedes}
		return marshal(rec)

	case ToolHistory:
		var req domain.HistoryRequest
		if err := domain.DecodeChittaWrite(args, &req); err != nil {
			return fail(err)
		}
		page, err := s.History(ctx, req)
		if err != nil {
			return fail(err)
		}
		return marshal(page)

	case ToolConnect:
		var req domain.ConnectRequest
		if err := domain.DecodeChittaWrite(args, &req); err != nil {
			return fail(err)
		}
		rel, err := s.Connect(ctx, req, host)
		if err != nil {
			return fail(err)
		}
		affected = []string{rel.ID}
		return marshal(rel)

	case ToolFindEntities:
		var req domain.EntitySearchRequest
		if err := domain.DecodeChittaWrite(args, &req); err != nil {
			return fail(err)
		}
		page, err := s.FindEntities(ctx, req)
		if err != nil {
			return fail(err)
		}
		return marshal(page)

	case ToolAbout:
		var req domain.ExploreRequest
		if err := domain.DecodeChittaWrite(args, &req); err != nil {
			return fail(err)
		}
		dossier, err := s.About(ctx, req)
		if err != nil {
			return fail(err)
		}
		if dossier.Entity != nil {
			affected = []string{dossier.Entity.ID}
		}
		return marshal(dossier)
	}
	return fail(domain.NewChittaError(domain.ErrInvalidInput, "unknown tool %q", tool))
}

// rejectReserved walks the raw args (two levels deep: object and array
// items) rejecting host-reserved field names.
func rejectReserved(args json.RawMessage) error {
	if len(args) == 0 {
		return nil
	}
	var top any
	if err := json.Unmarshal(args, &top); err != nil {
		return domain.NewChittaError(domain.ErrInvalidInput, "malformed arguments: %v", err)
	}
	check := func(m map[string]any) error {
		for _, f := range reservedFields {
			if _, ok := m[f]; ok {
				return domain.NewChittaError(domain.ErrInvalidInput, "reserved field %q is host-assigned", f)
			}
		}
		return nil
	}
	switch v := top.(type) {
	case map[string]any:
		if err := check(v); err != nil {
			return err
		}
		for _, child := range v {
			if cm, ok := child.(map[string]any); ok {
				if err := check(cm); err != nil {
					return err
				}
			}
			if arr, ok := child.([]any); ok {
				for _, item := range arr {
					if im, ok := item.(map[string]any); ok {
						if err := check(im); err != nil {
							return err
						}
					}
				}
			}
		}
	}
	return nil
}

func marshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, domain.NewChittaError(domain.ErrUnavailable, "marshal: %v", err)
	}
	return b, nil
}

// ensure System satisfies both seams at compile time.
var (
	_ domain.MemoryStore    = (*System)(nil)
	_ domain.KnowledgeGraph = (*System)(nil)
)
