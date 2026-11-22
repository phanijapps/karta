package chitta

import (
	"context"
	"encoding/json"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/tools"
)

// HostContextKey is the context key the calling layer uses to stamp
// execution identity (session, execution, agent) onto a tool call. The
// binding fills missing fields from its configured fallback; tool
// arguments never carry provenance (Dispatch rejects reserved fields).
type HostContextKey struct{}

// ToolProvider exposes the seven chitta tools on the kernel tool plane
// (tools.Provider). The chitta package implements the kernel interface
// — the plugin zone imports downward; nothing in the kernel imports
// chitta. Provenance comes from context values under HostContextKey
// plus the fallback supplied here (typically source: agent, root).
func (s *System) ToolProvider(fallback domain.ChittaHostContext) tools.Provider {
	return &chittaProvider{sys: s, fallback: fallback}
}

type chittaProvider struct {
	sys      *System
	fallback domain.ChittaHostContext
}

func (p *chittaProvider) Tools() []tools.Tool {
	out := make([]tools.Tool, 0, len(Tools()))
	for _, def := range Tools() {
		out = append(out, &chittaTool{sys: p.sys, def: def, fallback: p.fallback})
	}
	return out
}

type chittaTool struct {
	sys      *System
	def      ToolDef
	fallback domain.ChittaHostContext
}

func (t *chittaTool) Def() tools.Def {
	schema, _ := json.Marshal(t.def.Schema)
	return tools.Def{
		Name:        t.def.Name,
		Description: descriptionOf(t.def.Schema),
		ParamsJSON:  string(schema),
	}
}

func (t *chittaTool) Exec(ctx context.Context, argsJSON string) tools.Result {
	host := t.fallback
	if stamped, ok := ctx.Value(HostContextKey{}).(domain.ChittaHostContext); ok {
		if stamped.Source != "" {
			host.Source = stamped.Source
		}
		if stamped.SessionID != "" {
			host.SessionID = stamped.SessionID
		}
		if stamped.ExecutionID != "" {
			host.ExecutionID = stamped.ExecutionID
		}
		if stamped.AgentID != "" {
			host.AgentID = stamped.AgentID
		}
	}
	out, err := t.sys.Dispatch(ctx, t.def.Name, json.RawMessage(argsJSON), host)
	if err != nil {
		return tools.Result{Err: err}
	}
	return tools.Result{Output: string(out)}
}

// descriptionOf pulls the schema's description for the tool Def.
func descriptionOf(schema map[string]any) string {
	if d, ok := schema["description"].(string); ok {
		return d
	}
	return ""
}
