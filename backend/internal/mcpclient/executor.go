package mcpclient

// The EXECUTION bridge (ward-v2's retrieval plane): configured stdio
// MCP servers stay connected and their tools run LIVE — hosted engine
// runs mount them (prefixed mcp_<server>_<tool>), so a ward can
// search the web (ddg-search) or call any configured server. Fail-soft
// throughout: a dead server's tools vanish and the daemon logs it.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"zbot/backend/internal/tools"
)

// execClientName identifies Karta to live MCP servers.
const execClientName = "karta"

// callTimeout bounds one tool call.
const callTimeout = 60 * time.Second

// Failure kinds (the Timeline shows WHY an MCP call failed).
const (
	FailTimeout     = "timeout"       // the call exceeded callTimeout
	FailUnavailable = "unavailable"   // the server cannot start / re-dial
	FailProtocol    = "protocol"      // the server answered a protocol error
	FailServer      = "server_error"  // the tool reported isError
	FailBadArgs     = "bad_arguments" // the model's arguments did not decode
)

// ExecFailure is the typed EXECUTION failure (the probe Failure is the
// settings-plane JSON shape; this one rides tool results — the engine
// maps Kind → Timeline text).
type ExecFailure struct {
	Kind   string
	Server string
	Tool   string
	Err    error
}

func (f *ExecFailure) Error() string {
	return fmt.Sprintf("mcp %s.%s: %s: %v", f.Server, f.Tool, f.Kind, f.Err)
}

func (f *ExecFailure) Unwrap() error { return f.Err }

// AsFailure reports the typed MCP execution failure behind err, if any.
func AsFailure(err error) (*ExecFailure, bool) {
	var f *ExecFailure
	if errors.As(err, &f) {
		return f, true
	}
	return nil, false
}

// Executor keeps configured stdio MCP servers connected and executes
// their tools. Zero-value safe; Refresh reconciles the roster.
type Executor struct {
	// Specs supplies the desired roster each Refresh (registry names →
	// enabled stdio specs).
	Specs func() []ServerSpec
	// Log is the daemon's logger (fail-soft reporting).
	Log func(format string, args ...any)
	// BaseCtx backs the tool-plane reconciliation (Tools() has no
	// caller ctx; the daemon wires context.WithoutCancel(app ctx) —
	// a server START is bounded by DefaultDeadline inside start).
	BaseCtx context.Context // ctx-ok: wv2-tools-reconcile (daemon-lifetime reconcile)

	mu      sync.Mutex
	servers map[string]*managedServer
}

// managedServer is one live stdio session (or a dead one awaiting
// backoff).
type managedServer struct {
	name    string
	spec    ServerSpec
	session *mcp.ClientSession
	tools   []*mcp.Tool
	// startFailures counts consecutive start failures (backoff); a
	// healthy start resets it. notBefore gates Refresh re-dials.
	startFailures int
	notBefore     time.Time
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
		spec      ServerSpec
		dead      bool // session dropped by a failed call
		notBefore time.Time
	}
	specKey := func(s ServerSpec) string {
		return strings.Join(append([]string{s.Spec.Command, s.Spec.URL}, append(s.Spec.Args, envPairs(s.Spec.Env)...)...), "|")
	}
	live := make(map[string]liveEntry, len(e.servers))
	for name, s := range e.servers {
		live[name] = liveEntry{spec: s.spec, dead: s.session == nil, notBefore: s.notBefore}
	}
	e.mu.Unlock()
	for _, want := range desired {
		cur, ok := live[want.Name]
		if ok && !cur.dead && specKey(cur.spec) == specKey(want) {
			continue // healthy + unchanged
		}
		// Backoff FIRST: a crash-looping server (dead, unchanged spec)
		// is retried on its schedule, not on every Refresh (which
		// Tools() calls per model round). A spec CHANGE resets it.
		if ok && cur.dead && specKey(cur.spec) == specKey(want) && time.Now().Before(cur.notBefore) {
			continue
		}
		if ok && !cur.dead { // live but changed spec: close, restart below
			e.mu.Lock()
			if s := e.servers[want.Name]; s != nil && s.session != nil {
				_ = s.session.Close()
			}
			delete(e.servers, want.Name)
			e.mu.Unlock()
		}
		// A DEAD entry (unchanged spec, window expired) is kept in place:
		// recordStartFailure below bumps its count so the backoff GROWS
		// (2s → 4s → 8s…) across successive failed retries.
		if ok && cur.dead && specKey(cur.spec) != specKey(want) {
			e.mu.Lock() // spec changed: reset the failure history
			delete(e.servers, want.Name)
			e.mu.Unlock()
		}
		if err := e.start(ctx, want); err != nil {
			e.recordStartFailure(want, err)
			e.logf("server %q failed to start (%d in a row): %v (tools absent; retry in %s)", want.Name, e.failuresOf(want.Name), err, backoffFor(e.failuresOf(want.Name)))
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

// start dials one server and lists its tools (bounded by the probe
// deadline discipline).
func (e *Executor) start(ctx context.Context, want ServerSpec) error {
	dialCtx, cancel := context.WithTimeout(ctx, DefaultDeadline)
	defer cancel()
	cmd := exec.Command(resolveBinary(want.Spec.Command), want.Spec.Args...)
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
	if cur := e.servers[want.Name]; cur != nil && cur.session != nil {
		// A concurrent Refresh/start won: discard THIS session (its
		// process closes) rather than stranding it as a leaked child.
		e.mu.Unlock()
		_ = sess.Close()
		e.logf("server %q: concurrent start discarded", want.Name)
		return nil
	}
	e.servers[want.Name] = &managedServer{name: want.Name, spec: want, session: sess, tools: live}
	e.mu.Unlock()
	e.logf("server %q live (%d tools)", want.Name, len(live))
	return nil
}

// recordStartFailure bumps a server's consecutive-failure count and
// schedules its retry (backoff). A dead entry (no session) is kept so
// the backoff state survives between Refresh calls.
func (e *Executor) recordStartFailure(want ServerSpec, err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	s := e.servers[want.Name]
	if s == nil {
		s = &managedServer{name: want.Name, spec: want}
		e.servers[want.Name] = s
	}
	s.startFailures++
	s.notBefore = time.Now().Add(backoffFor(s.startFailures))
}

func (e *Executor) failuresOf(name string) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s := e.servers[name]; s != nil {
		return s.startFailures
	}
	return 0
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
		return tools.Result{Err: &ExecFailure{Kind: FailBadArgs, Server: t.server, Tool: t.tool.Name, Err: err}}
	}
	e := t.exec
	e.mu.Lock()
	srv := e.servers[t.server]
	e.mu.Unlock()
	if srv == nil || srv.session == nil {
		// Fail-soft: re-dial once (the server may have died) — honoring
		// the restart backoff (a crash-looping server is not re-dialed
		// from every stale tool call inside its window).
		if srv != nil && time.Now().Before(srv.notBefore) {
			return tools.Result{Err: &ExecFailure{Kind: FailUnavailable, Server: t.server, Tool: t.tool.Name,
				Err: fmt.Errorf("server down; retry in %s", time.Until(srv.notBefore).Round(time.Second))}}
		}
		started := false
		if e.Specs != nil {
			for _, want := range e.Specs() {
				if want.Name == t.server {
					if err := e.start(ctx, want); err != nil {
						e.recordStartFailure(want, err)
						return tools.Result{Err: &ExecFailure{Kind: FailUnavailable, Server: t.server, Tool: t.tool.Name, Err: err}}
					}
					started = true
					break
				}
			}
		}
		if started {
			e.mu.Lock()
			srv = e.servers[t.server]
			e.mu.Unlock()
		}
		// Re-check BOTH: a deconfigured server (Specs no longer lists it)
		// leaves a dead entry whose session is nil — typed, never a panic.
		if srv == nil || srv.session == nil {
			return tools.Result{Err: &ExecFailure{Kind: FailUnavailable, Server: t.server, Tool: t.tool.Name, Err: errors.New("server not running")}}
		}
	}
	res, err := srv.session.CallTool(callCtx, &mcp.CallToolParams{Name: t.tool.Name, Arguments: args})
	if err != nil {
		kind := FailProtocol
		if callCtx.Err() == context.DeadlineExceeded {
			kind = FailTimeout
		}
		// The session may be dead — drop it so the next call re-dials.
		e.mu.Lock()
		if cur := e.servers[t.server]; cur == srv {
			_ = srv.session.Close()
			delete(e.servers, t.server)
		}
		e.mu.Unlock()
		return tools.Result{Err: &ExecFailure{Kind: kind, Server: t.server, Tool: t.tool.Name, Err: err}}
	}
	if res != nil && res.IsError {
		return tools.Result{Err: &ExecFailure{Kind: FailServer, Server: t.server, Tool: t.tool.Name, Err: errors.New(strings.TrimSpace(contentText(res)))}}
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
	ctx := e.BaseCtx
	if ctx == nil {
		ctx = context.Background() // ctx-ok: wv2-tools-reconcile (zero-value Executor: tests; daemon always wires BaseCtx)
	}
	e.Refresh(ctx)
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
				// a stable hash of the ORIGINAL pair (bounded to 64).
				suffix := "_" + fmt.Sprintf("%08x", fnv32(name+"/"+tl.Name))
				cut := 64 - len(suffix)
				if cut > len(ext) {
					cut = len(ext)
				}
				ext = ext[:cut] + suffix
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
