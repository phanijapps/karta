package mcpclient

// The EXECUTION bridge (ward-v2's retrieval plane): configured stdio
// MCP servers stay connected and their tools run LIVE — hosted engine
// runs mount them (prefixed mcp_<server>_<tool>), so a ward can
// search the web (ddg-search) or call any configured server. Fail-soft
// throughout: a dead server's tools vanish and the daemon logs it.

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"zbot/backend/internal/registry"
	"zbot/backend/internal/tools"
)

// execClientName identifies Karta to live MCP servers.
const execClientName = "karta"

// callTimeout bounds one tool call.
const callTimeout = 60 * time.Second

// ServerSpec names one enabled stdio server to run.
type ServerSpec struct {
	Name string
	Spec registry.MCPSpec
}

// Executor keeps configured stdio MCP servers connected and executes
// their tools. Zero-value safe; Refresh reconciles the roster.
type Executor struct {
	// Specs supplies the desired roster each Refresh (registry names →
	// enabled stdio specs).
	Specs func() []ServerSpec
	// Log is the daemon's logger (fail-soft reporting).
	Log func(format string, args ...any)

	mu      sync.Mutex
	servers map[string]*managedServer
}

// managedServer is one live stdio session.
type managedServer struct {
	name    string
	spec    ServerSpec
	session *mcp.ClientSession
	tools   []*mcp.Tool
}

func (e *Executor) logf(format string, args ...any) {
	if e.Log != nil {
		e.Log("mcp exec: "+format, args...)
	}
}

// Refresh reconciles the live roster with the desired specs: starts
// missing servers, RESTARTS servers whose spec changed, drops
// deconfigured ones, and re-dials configured-but-dead servers (a dead
// server recovers for the NEXT run — no stale tool object needed).
// Safe to call repeatedly (daemon boot, registry changes, Tools()).
func (e *Executor) Refresh(ctx context.Context) {
	if e.Specs == nil {
		return
	}
	desired := e.Specs()
	e.mu.Lock()
	if e.servers == nil {
		e.servers = map[string]*managedServer{}
	}
	type liveEntry struct {
		spec ServerSpec
		dead bool // session dropped by a failed call
	}
	specKey := func(s ServerSpec) string {
		return strings.Join(append([]string{s.Spec.Command, s.Spec.URL}, append(s.Spec.Args, envPairs(s.Spec.Env)...)...), "|")
	}
	live := make(map[string]liveEntry, len(e.servers))
	for name, s := range e.servers {
		live[name] = liveEntry{spec: s.spec, dead: s.session == nil}
	}
	e.mu.Unlock()
	for _, want := range desired {
		cur, ok := live[want.Name]
		if ok && !cur.dead && specKey(cur.spec) == specKey(want) {
			continue // healthy + unchanged
		}
		if ok { // changed spec (or dead): drop, restart below
			e.mu.Lock()
			if s := e.servers[want.Name]; s != nil && s.session != nil {
				_ = s.session.Close()
			}
			delete(e.servers, want.Name)
			e.mu.Unlock()
		}
		if err := e.start(ctx, want); err != nil {
			e.logf("server %q failed to start: %v (tools absent)", want.Name, err)
		}
	}
	// Drop deconfigured servers.
	e.mu.Lock()
	for name := range e.servers {
		found := false
		for _, want := range desired {
			if want.Name == name {
				found = true
				break
			}
		}
		if !found {
			if s := e.servers[name]; s != nil && s.session != nil {
				_ = s.session.Close()
			}
			delete(e.servers, name)
		}
	}
	e.mu.Unlock()
}

// Tools returns the live roster adapted to Karta's tool plane —
// reconciling first so a server that died mid-run recovers for THIS
// run's next model round (bounded by Refresh's fail-soft start).

// start dials one server and lists its tools (bounded by the probe
// deadline discipline).
func (e *Executor) start(ctx context.Context, want ServerSpec) error {
	dialCtx, cancel := context.WithTimeout(ctx, DefaultDeadline)
	defer cancel()
	cmd := exec.Command(want.Spec.Command, want.Spec.Args...)
	cmd.Env = append(cmd.Environ(), envPairs(want.Spec.Env)...)
	client := mcp.NewClient(&mcp.Implementation{Name: execClientName, Version: "0.0"}, nil)
	sess, err := client.Connect(dialCtx, &mcp.CommandTransport{Command: cmd}, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	var live []*mcp.Tool
	cursor := ""
	for {
		res, lerr := sess.ListTools(dialCtx, &mcp.ListToolsParams{Cursor: cursor})
		if lerr != nil {
			_ = sess.Close()
			return fmt.Errorf("tools/list: %w", lerr)
		}
		live = append(live, res.Tools...)
		if res.NextCursor == "" || len(live) >= toolListCap {
			break
		}
		cursor = res.NextCursor
	}
	e.mu.Lock()
	e.servers[want.Name] = &managedServer{name: want.Name, spec: want, session: sess, tools: live}
	e.mu.Unlock()
	e.logf("server %q live (%d tools)", want.Name, len(live))
	return nil
}

func envPairs(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	sort.Strings(out)
	return out
}

// sanitizeName makes an MCP tool's external name safe for provider
// function-name rules ([a-zA-Z0-9_-], ≤64).
func sanitizeName(parts ...string) string {
	var b strings.Builder
	for _, p := range parts {
		for _, r := range p {
			switch {
			case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
				b.WriteRune(r)
			default:
				b.WriteRune('_')
			}
		}
		b.WriteRune('_')
	}
	name := strings.TrimSuffix(b.String(), "_")
	if len(name) > 64 {
		// 55 + 1 + 8 = 64 exactly.
		name = name[:55] + "_" + fmt.Sprintf("%08x", fnv32(name))
	}
	return name
}

func fnv32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// mcpTool adapts one live MCP tool to Karta's tool plane.
type mcpTool struct {
	exec    *Executor
	server  string
	tool    *mcp.Tool
	extName string
}

func (t *mcpTool) Def() tools.Def {
	desc := "MCP tool " + t.tool.Name + " (server " + t.server + ")."
	if t.tool.Description != "" {
		desc = "[" + t.server + "] " + clampUTF8(t.tool.Description, 1024)
	}
	params := "{}"
	if raw, err := json.Marshal(t.tool.InputSchema); err == nil && len(raw) > 2 {
		params = string(raw)
	}
	return tools.Def{Name: t.extName, Description: desc, ParamsJSON: params}
}

func (t *mcpTool) Exec(ctx context.Context, argsJSON string) tools.Result {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	var args map[string]any
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return tools.Result{Err: fmt.Errorf("mcp %s.%s: bad arguments: %w", t.server, t.tool.Name, err)}
	}
	e := t.exec
	e.mu.Lock()
	srv := e.servers[t.server]
	e.mu.Unlock()
	if srv == nil || srv.session == nil {
		// Fail-soft: re-dial once (the server may have died).
		if e.Specs != nil {
			for _, want := range e.Specs() {
				if want.Name == t.server {
					if err := e.start(ctx, want); err != nil {
						return tools.Result{Err: fmt.Errorf("mcp %s unavailable: %w", t.server, err)}
					}
					e.mu.Lock()
					srv = e.servers[t.server]
					e.mu.Unlock()
					break
				}
			}
		}
		if srv == nil {
			return tools.Result{Err: fmt.Errorf("mcp %s unavailable", t.server)}
		}
	}
	res, err := srv.session.CallTool(callCtx, &mcp.CallToolParams{Name: t.tool.Name, Arguments: args})
	if err != nil {
		// The session may be dead — drop it so the next call re-dials.
		e.mu.Lock()
		if cur := e.servers[t.server]; cur == srv {
			_ = srv.session.Close()
			delete(e.servers, t.server)
		}
		e.mu.Unlock()
		return tools.Result{Err: fmt.Errorf("mcp %s.%s: %w", t.server, t.tool.Name, err)}
	}
	return tools.Result{Output: contentText(res)}
}

// contentText renders an MCP tool result's content blocks to text
// (text blocks verbatim, others as JSON summaries).
func contentText(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tx, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tx.Text)
			b.WriteByte('\n')
			continue
		}
		raw, _ := json.Marshal(c)
		b.Write(raw)
		b.WriteByte('\n')
	}
	if res.IsError {
		return "error: " + strings.TrimSpace(b.String())
	}
	return strings.TrimSpace(b.String())
}

// Tools returns the live roster adapted to Karta's tool plane
// (mcp_<server>_<tool>, sanitized; collisions impossible by hash
// suffix).
func (e *Executor) Tools() []tools.Tool {
	e.Refresh(context.Background())
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []tools.Tool
	names := make([]string, 0, len(e.servers))
	for name := range e.servers {
		names = append(names, name)
	}
	sort.Strings(names)
	seen := map[string]string{} // extName → "server/tool" owner
	for _, name := range names {
		srv := e.servers[name]
		for _, tl := range srv.tools {
			ext := sanitizeName("mcp", name, tl.Name)
			if owner, clash := seen[ext]; clash {
				// Normalization collision (e.g. "a b" vs "a_b"): suffix
				// a stable hash of the ORIGINAL pair.
				ext = ext[:55] + "_" + fmt.Sprintf("%08x", fnv32(name+"/"+tl.Name))
				if _, again := seen[ext]; again {
					continue // pathological: drop rather than alias
				}
				e.logf("tool name collision resolved: %s (%s vs %s)", ext, owner, name+"/"+tl.Name)
			}
			seen[ext] = name + "/" + tl.Name
			out = append(out, &mcpTool{exec: e, server: name, tool: tl, extName: ext})
		}
	}
	return out
}

var _ tools.Tool = (*mcpTool)(nil)
