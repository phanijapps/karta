// Package mcpclient probes configured MCP servers for Settings
// verification: per declared transport it performs a real MCP client
// handshake (initialize → tools/list → shutdown) under one shared
// deadline and returns the discovered tools or a typed failure. It is
// settings-plane only — no runtime agent wiring, no tool execution.
//
// Graph placement: imports `registry` (only the MCPSpec type) plus
// stdlib and the official go-sdk; sits beside `tools`, below `server`,
// never touching `engine/*` (the eino boundary).
package mcpclient

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"zbot/backend/internal/registry"
)

// Failure reasons, stable API surface (contract enum).
const (
	ReasonSpawnFailed   = "spawn_failed"
	ReasonConnectFailed = "connect_failed"
	ReasonTimeout       = "timeout"
	ReasonProtocolError = "protocol_error"
	StatusOK            = "ok"
	StatusFailed        = "failed"
	TransportStdio      = "stdio"
	TransportHTTP       = "http"
	clientName          = "karta-probe"
	toolListCap         = 256
	DefaultDeadline     = 10 * time.Second
)

// Tool is one discovered tool's static description.
type Tool struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// Failure is a typed probe failure for one transport.
type Failure struct {
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// TransportResult is the probe outcome for one declared transport.
type TransportResult struct {
	Transport string   `json:"transport"`
	Status    string   `json:"status"`
	Tools     []Tool   `json:"tools,omitempty"`
	Truncated bool     `json:"truncated,omitempty"`
	Failure   *Failure `json:"failure,omitempty"`
}

// ProbeResult carries one result per declared transport (stdio first).
type ProbeResult struct {
	Results []TransportResult
}

// stragglerGrace is the slack beyond the deadline before a probe that
// has not returned is abandoned (the go-sdk's connect error path can
// drain for a few seconds after ctx cancellation).
const stragglerGrace = 2 * time.Second

// Prober verifies MCP server manifests. Server handlers take this seam;
// tests inject fakes (zero processes, zero network).
type Prober interface {
	Test(ctx context.Context, spec registry.MCPSpec) ProbeResult
}

// GoSDKProber implements Prober over the official modelcontextprotocol
// go-sdk. Declared transports are probed concurrently under one shared
// deadline, so worst-case wall time stays one deadline.
type GoSDKProber struct {
	// Deadline bounds the whole probe (all transports). Zero means
	// DefaultDeadline. Tests shrink it.
	Deadline time.Duration
}

// Test probes every declared transport of spec concurrently under one
// shared deadline. The envelope is the deadline plus a small straggler
// grace: the go-sdk's connect error path can take a few extra seconds
// to drain after ctx cancellation, and a hanging server must not hold
// the caller past the envelope — a straggler is abandoned and its
// result synthesized as the timeout failure (identical to what it
// would have reported; its goroutine exits shortly after).
func (p GoSDKProber) Test(ctx context.Context, spec registry.MCPSpec) ProbeResult {
	deadline := p.Deadline
	if deadline <= 0 {
		deadline = DefaultDeadline
	}
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	// Deterministic order: stdio first, then http.
	var order []string
	probes := map[string]func() TransportResult{}
	if spec.Command != "" {
		order = append(order, TransportStdio)
		cmd := spec.Command
		if len(spec.Args) > 0 {
			probes[TransportStdio] = func() TransportResult {
				return probeOne(ctx, &mcp.CommandTransport{Command: exec.Command(cmd, spec.Args...)}, transportEnv(spec))
			}
		} else {
			probes[TransportStdio] = func() TransportResult {
				return probeOne(ctx, &mcp.CommandTransport{Command: exec.Command(cmd)}, transportEnv(spec))
			}
		}
	}
	if spec.URL != "" {
		order = append(order, TransportHTTP)
		url := spec.URL
		probes[TransportHTTP] = func() TransportResult {
			return probeOne(ctx, &mcp.StreamableClientTransport{Endpoint: url}, nil)
		}
	}

	done := make(chan indexedResult, len(order))
	for i, transport := range order {
		go func(i int, transport string) { done <- indexedResult{i, probes[transport]()} }(i, transport)
	}

	// Envelope: after deadline+grace, abandon stragglers.
	abandon := make(chan struct{})
	time.AfterFunc(deadline+stragglerGrace, func() { close(abandon) })

	results := make([]TransportResult, len(order))
	filled := 0
	for filled < len(order) {
		select {
		case r := <-done:
			results[r.index] = r.result
			filled++
		case <-abandon:
			for i, transport := range order {
				if results[i].Transport == "" {
					results[i] = TransportResult{Transport: transport, Status: StatusFailed,
						Failure: &Failure{Reason: ReasonTimeout, Message: "probe deadline exceeded (straggler abandoned)"}}
					filled++
				}
			}
		}
	}
	return ProbeResult{Results: results}
}

// indexedResult keeps the deterministic stdio-then-http ordering while
// collecting concurrently produced results.
type indexedResult struct {
	index  int
	result TransportResult
}

// transportEnv applies the manifest's env to stdio children (that is
// what the env map is for); http transports need none.
func transportEnv(spec registry.MCPSpec) []string {
	if len(spec.Env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(spec.Env))
	for k := range spec.Env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	env := make([]string, 0, len(keys))
	for _, k := range keys {
		env = append(env, k+"="+spec.Env[k])
	}
	return env
}

// probeOne runs connect (initialize) → ListTools (paginated up to the
// cap) → Close for one transport and maps failures to typed reasons.
func probeOne(ctx context.Context, transport mcp.Transport, childEnv []string) TransportResult {
	label := transportLabel(transport)
	if ct, ok := transport.(*mcp.CommandTransport); ok && childEnv != nil {
		ct.Command.Env = append(ct.Command.Environ(), childEnv...)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: clientName, Version: "0.0"}, nil)
	sess, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return TransportResult{Transport: label, Status: StatusFailed, Failure: mapConnectFailure(err)}
	}
	defer func() { _ = sess.Close() }()

	var tools []Tool
	truncated := false
	cursor := ""
	for {
		res, err := sess.ListTools(ctx, &mcp.ListToolsParams{Cursor: cursor})
		if err != nil {
			return TransportResult{Transport: label, Status: StatusFailed, Failure: mapCallFailure(err)}
		}
		for _, tool := range res.Tools {
			if len(tools) >= toolListCap {
				truncated = true
				break
			}
			// Externally sourced metadata is clamped to the contract's
			// response bounds (name 128, description 1024) — a hostile
			// peer cannot smuggle oversized strings into responses.
			tools = append(tools, Tool{Name: clampUTF8(tool.Name, 128), Description: clampUTF8(tool.Description, 1024)})
		}
		if res.NextCursor == "" || truncated {
			break
		}
		cursor = res.NextCursor
	}
	return TransportResult{Transport: label, Status: StatusOK, Tools: tools, Truncated: truncated}
}

// clampUTF8 truncates s to at most max BYTES without splitting a rune
// (the contract bounds are byte lengths).
func clampUTF8(s string, max int) string {
	if len(s) <= max {
		return s
	}
	cut := s[:max]
	for i := max - 1; i >= 0 && !utf8.RuneStart(cut[i]); i-- {
		cut = cut[:i]
	}
	return cut
}

func transportLabel(transport mcp.Transport) string {
	if _, ok := transport.(*mcp.CommandTransport); ok {
		return TransportStdio
	}
	return TransportHTTP
}

// mapConnectFailure classifies a connect/initialize failure: missing
// stdio binaries (fork/exec), unreachable endpoints (dial), deadline
// expiry, and everything protocol-level.
func mapConnectFailure(err error) *Failure {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
		return &Failure{Reason: ReasonTimeout, Message: err.Error()}
	}
	var pathErr *fs.PathError
	if errors.As(err, &pathErr) {
		return &Failure{Reason: ReasonSpawnFailed, Message: err.Error()}
	}
	var execErr *exec.Error
	if errors.As(err, &execErr) {
		return &Failure{Reason: ReasonSpawnFailed, Message: err.Error()}
	}
	// exec.Start failures for bare command names surface as exec.Error
	// (“executable file not found”); fork/exec path failures as
	// fs.PathError. Both are spawn failures, never protocol errors.
	if strings.Contains(err.Error(), "executable file not found") || strings.Contains(err.Error(), "fork/exec") {
		return &Failure{Reason: ReasonSpawnFailed, Message: err.Error()}
	}
	if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "dial tcp") {
		return &Failure{Reason: ReasonConnectFailed, Message: err.Error()}
	}
	return &Failure{Reason: ReasonProtocolError, Message: err.Error()}
}

// mapCallFailure classifies a tools/list failure after a successful
// handshake (timeout or protocol violation; the endpoint was reachable).
func mapCallFailure(err error) *Failure {
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "context deadline exceeded") {
		return &Failure{Reason: ReasonTimeout, Message: err.Error()}
	}
	return &Failure{Reason: ReasonProtocolError, Message: err.Error()}
}

var _ Prober = GoSDKProber{}

// String keeps Failure readable in logs.
func (f *Failure) String() string {
	return fmt.Sprintf("%s: %s", f.Reason, f.Message)
}
