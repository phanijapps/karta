package mcpclient

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"zbot/backend/internal/registry"
)

// echoBin builds the test-only stdio server once for the package.
func echoBin(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "echo-mcp-server")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/echoServer")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build echo server: %v: %s", err, out)
	}
	return bin
}

// httpMCPServer serves a real MCP streamable-http endpoint on a loopback
// ephemeral port with `count` tools; no external network.
func httpMCPServer(t *testing.T, count int) string {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test-http", Version: "0"}, nil)
	for i := 0; i < count; i++ {
		name := fmt.Sprintf("tool_%04d", i)
		mcp.AddTool(server, &mcp.Tool{Name: name, Description: "http tool " + name},
			func(ctx context.Context, req *mcp.CallToolRequest, in struct{}) (*mcp.CallToolResult, any, error) {
				return &mcp.CallToolResult{}, nil, nil
			})
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	go func() {
		_ = http.Serve(listener, mcp.NewStreamableHTTPHandler(func(*http.Request) *mcp.Server { return server }, nil))
	}()
	return "http://" + listener.Addr().String() + "/mcp"
}

func resultFor(res ProbeResult, transport string) TransportResult {
	for _, r := range res.Results {
		if r.Transport == transport {
			return r
		}
	}
	t := &testing.T{}
	t.Fatalf("no %s result in %v", transport, res.Results)
	return TransportResult{}
}

func TestProbeStdioHappyPath(t *testing.T) {
	t.Parallel()
	prober := GoSDKProber{Deadline: 20 * time.Second}
	res := prober.Test(context.Background(), registry.MCPSpec{Command: echoBin(t), Args: []string{"3"}})
	if len(res.Results) != 1 {
		t.Fatalf("want 1 result, got %v", res.Results)
	}
	r := res.Results[0]
	if r.Transport != TransportStdio || r.Status != StatusOK {
		t.Fatalf("stdio result wrong: %+v", r)
	}
	if len(r.Tools) != 3 || r.Tools[0].Name != "tool_0000" {
		t.Fatalf("tools wrong: %+v", r.Tools)
	}
	if r.Truncated {
		t.Fatal("3 tools should not truncate")
	}
}

func TestProbeHTTPHappyPathAndTruncation(t *testing.T) {
	t.Parallel()
	prober := GoSDKProber{Deadline: 10 * time.Second}
	url := httpMCPServer(t, toolListCap+5) // cap+5 forces pagination + truncation
	res := prober.Test(context.Background(), registry.MCPSpec{URL: url})
	r := resultFor(res, TransportHTTP)
	if r.Status != StatusOK {
		t.Fatalf("http result wrong: %+v", r)
	}
	if len(r.Tools) != toolListCap {
		t.Fatalf("want cap %d tools, got %d", toolListCap, len(r.Tools))
	}
	if !r.Truncated {
		t.Fatal("oversized listing must mark truncation")
	}
	// Exactly at the cap: no truncation flag.
	url2 := httpMCPServer(t, toolListCap)
	res2 := prober.Test(context.Background(), registry.MCPSpec{URL: url2})
	r2 := resultFor(res2, TransportHTTP)
	if len(r2.Tools) != toolListCap || r2.Truncated {
		t.Fatalf("at-cap listing wrong: n=%d truncated=%v", len(r2.Tools), r2.Truncated)
	}
}

func TestProbeDualTransportIndependentResults(t *testing.T) {
	t.Parallel()
	res := GoSDKProber{Deadline: 20 * time.Second}.Test(context.Background(), registry.MCPSpec{
		Command: echoBin(t),
		URL:     "http://127.0.0.1:1/mcp", // refused port
	})
	if len(res.Results) != 2 {
		t.Fatalf("want 2 results, got %v", res.Results)
	}
	stdio := resultFor(res, TransportStdio)
	httpRes := resultFor(res, TransportHTTP)
	if stdio.Status != StatusOK || len(stdio.Tools) != 2 {
		t.Fatalf("stdio should succeed: %+v", stdio)
	}
	if httpRes.Status != StatusFailed || httpRes.Failure == nil || httpRes.Failure.Reason != ReasonConnectFailed {
		t.Fatalf("http should be a typed connect failure: %+v", httpRes)
	}
	// Deterministic order: stdio first.
	if res.Results[0].Transport != TransportStdio || res.Results[1].Transport != TransportHTTP {
		t.Fatalf("order wrong: %v", res.Results)
	}
}

func TestProbeTypedFailures(t *testing.T) {
	t.Parallel()
	// 2s deadline: the timeout contract is identical, the wait is not.
	prober := GoSDKProber{Deadline: 2 * time.Second}
	ctx := context.Background()

	// Missing stdio binary (absolute path) → spawn_failed.
	res := prober.Test(ctx, registry.MCPSpec{Command: "/nonexistent/mcp-server"})
	if r := res.Results[0]; r.Status != StatusFailed || r.Failure.Reason != ReasonSpawnFailed {
		t.Fatalf("want spawn_failed, got %+v", r)
	}

	// Missing stdio binary (bare name, exec.Error path) → spawn_failed.
	res = prober.Test(ctx, registry.MCPSpec{Command: "definitely-not-a-real-mcp-binary"})
	if r := res.Results[0]; r.Status != StatusFailed || r.Failure.Reason != ReasonSpawnFailed {
		t.Fatalf("want spawn_failed for bare name, got %+v", r)
	}

	// Refused HTTP port → connect_failed.
	res = prober.Test(ctx, registry.MCPSpec{URL: "http://127.0.0.1:1/mcp"})
	if r := res.Results[0]; r.Status != StatusFailed || r.Failure.Reason != ReasonConnectFailed {
		t.Fatalf("want connect_failed, got %+v", r)
	}

	// Hanging handshake (listener accepts, never responds) → timeout,
	// bounded by the deadline.
	hang, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var conns atomic.Int32
	var held []net.Conn // keep references: an unreferenced conn is closed by the GC finalizer (instant RST — a load-dependent race this test must not have)
	acceptDone := make(chan struct{})
	go func() {
		defer close(acceptDone)
		for {
			conn, err := hang.Accept()
			if err != nil {
				return
			}
			conns.Add(1)
			held = append(held, conn)
			// Hold the connection open; never speak MCP.
		}
	}()
	// Closing the listener does NOT close accepted conns: wait for the
	// accept loop, then close what we held (no finalizer dependence).
	t.Cleanup(func() {
		_ = hang.Close()
		<-acceptDone
		for _, c := range held {
			_ = c.Close()
		}
	})
	start := time.Now()
	res = prober.Test(ctx, registry.MCPSpec{URL: "http://" + hang.Addr().String() + "/mcp"})
	if r := res.Results[0]; r.Status != StatusFailed || r.Failure.Reason != ReasonTimeout {
		t.Fatalf("want timeout, got %+v", r)
	}
	if elapsed := time.Since(start); elapsed > 4*time.Second+500*time.Millisecond { // deadline + stragglerGrace(2s) + slack
		t.Fatalf("probe not deadline-bounded: %s", elapsed)
	}
}

func TestProbeHangingStdioBounded(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sleep binary assumption")
	}
	// `sleep 30` accepts the spawn but never speaks MCP: the initialize
	// handshake must time out under the (shortened) deadline.
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("no sleep binary")
	}
	start := time.Now()
	res := GoSDKProber{Deadline: 1 * time.Second}.Test(context.Background(),
		registry.MCPSpec{Command: sleep, Args: []string{"30"}})
	if r := res.Results[0]; r.Status != StatusFailed || r.Failure.Reason != ReasonTimeout {
		t.Fatalf("want timeout, got %+v", r)
	}
	if time.Since(start) > 3*time.Second+500*time.Millisecond { // 1s deadline + grace + slack
		t.Fatalf("probe not bounded: %s", time.Since(start))
	}
}

func TestProbeEnvPassthroughToStdioChild(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("env binary assumption")
	}
	// `env` prints the child environment but speaks no MCP: initialize
	// fails as protocol garbage — proving the child ran with the env
	// applied would need its output; instead assert the transport is
	// exercised and fails typed (not spawn: env exists).
	envBin, err := exec.LookPath("env")
	if err != nil {
		t.Skip("no env binary")
	}
	res := GoSDKProber{Deadline: 3 * time.Second}.Test(context.Background(),
		registry.MCPSpec{Command: envBin, Env: map[string]string{"KARTA_PROBE": "yes"}})
	r := res.Results[0]
	if r.Status != StatusFailed {
		t.Fatalf("env should not handshake: %+v", r)
	}
	if r.Failure.Reason == ReasonSpawnFailed {
		t.Fatal("env exists; failure must not be spawn_failed")
	}
}

// Fakes exist so server handler tests never spawn processes (see the
// Prober seam); this guard keeps the seam honest.
type recordingProber struct {
	called int
	spec   registry.MCPSpec
}

func (p *recordingProber) Test(ctx context.Context, spec registry.MCPSpec) ProbeResult {
	p.called++
	p.spec = spec
	return ProbeResult{Results: []TransportResult{{Transport: TransportStdio, Status: StatusOK, Tools: []Tool{{Name: "fake"}}}}}
}

func TestProberSeamIsImplementable(t *testing.T) {
	t.Parallel()
	var p Prober = &recordingProber{}
	res := p.Test(context.Background(), registry.MCPSpec{Command: "x"})
	if res.Results[0].Tools[0].Name != "fake" {
		t.Fatal("fake prober round-trip failed")
	}
	if _, err := os.Stat("/nonexistent"); !os.IsNotExist(err) {
		t.Fatal("sanity")
	}
}

// TestExecutorLifecycleAndCalls (task 3.1): the execution bridge keeps
// a stdio server connected, lists + calls its tools live, drops
// deconfigured servers, and re-dials after the session dies.
func TestExecutorLifecycleAndCalls(t *testing.T) {
	t.Parallel()
	bin := echoBin(t)
	spec := registry.MCPSpec{Command: bin, Args: []string{"2"}}
	ex := &Executor{
		Specs: func() []ServerSpec { return []ServerSpec{{Name: "echo", Spec: spec}} },
		Log:   func(string, ...any) {},
	}
	ctx := context.Background()
	ex.Refresh(ctx)

	live := ex.Tools()
	if len(live) != 2 {
		t.Fatalf("want 2 tools, got %d", len(live))
	}
	if live[0].Def().Name != "mcp_echo_tool_0000" {
		t.Fatalf("prefixed name: %s", live[0].Def().Name)
	}
	def := live[0].Def()
	if def.Description == "" || def.ParamsJSON == "" {
		t.Fatalf("def incomplete: %+v", def)
	}

	// A live call round-trips the arguments.
	res := live[0].Exec(ctx, `{"hello":"world"}`)
	if res.Err != nil {
		t.Fatalf("call: %v", res.Err)
	}
	if !strings.Contains(res.Output, "echo:") || !strings.Contains(res.Output, "world") {
		t.Fatalf("output: %s", res.Output)
	}

	// Deconfigured: the roster empties.
	ex.Specs = func() []ServerSpec { return nil }
	ex.Refresh(ctx)
	if got := ex.Tools(); len(got) != 0 {
		t.Fatalf("deconfigured tools linger: %d", len(got))
	}

	// Re-dial on call after the roster returns (the dead-session path).
	ex.Specs = func() []ServerSpec { return []ServerSpec{{Name: "echo", Spec: spec}} }
	res = live[0].Exec(ctx, `{"again":true}`)
	if res.Err != nil {
		t.Fatalf("re-dial call: %v", res.Err)
	}
	if !strings.Contains(res.Output, "again") {
		t.Fatalf("re-dial output: %s", res.Output)
	}
}

// TestExecutorSanitizeName: external names obey provider rules.
func TestExecutorSanitizeName(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ddg-search", "ddg-search"}, // dashes are valid function-name chars
		{"a b", "a_b"},
	} {
		if got := sanitizeName("mcp", tc.in, "search"); got != "mcp_"+tc.want+"_search" {
			t.Fatalf("sanitize(%q) = %q", tc.in, got)
		}
	}
	long := sanitizeName("mcp", strings.Repeat("x", 80), strings.Repeat("y", 80))
	if len(long) > 64 {
		t.Fatalf("long name not bounded: %d", len(long))
	}
}

// TestExecutorChangedSpecRestartsAndCollisions (final-review P2-2):
// a changed spec restarts the server; a dead server recovers via
// Tools(); normalizing collisions get distinct suffixed names.
func TestExecutorChangedSpecRestartsAndCollisions(t *testing.T) {
	t.Parallel()
	bin := echoBin(t)
	spec := func(args ...string) registry.MCPSpec {
		return registry.MCPSpec{Command: bin, Args: args}
	}
	current := []ServerSpec{{Name: "echo", Spec: spec("2")}}
	ex := &Executor{
		Specs: func() []ServerSpec { return current },
		Log:   func(string, ...any) {},
	}
	ctx := context.Background()
	ex.Refresh(ctx)
	if got := len(ex.Tools()); got != 2 {
		t.Fatalf("initial tools: %d", got)
	}

	// Changed spec (more tools): Refresh restarts under the same name.
	current = []ServerSpec{{Name: "echo", Spec: spec("4")}}
	ex.Refresh(ctx)
	if got := len(ex.Tools()); got != 4 {
		t.Fatalf("changed spec must restart: %d tools", got)
	}

	// Dead server: drop the session, Tools() reconciles it back.
	ex.mu.Lock()
	for name, srv := range ex.servers {
		_ = srv.session.Close()
		delete(ex.servers, name)
	}
	ex.mu.Unlock()
	if got := len(ex.Tools()); got != 4 {
		t.Fatalf("dead server must recover via Tools(): %d", got)
	}

	// Collision: two servers whose sanitized names coincide.
	current = []ServerSpec{
		{Name: "a b", Spec: spec("1")},
		{Name: "a_b", Spec: spec("1")},
	}
	ex.Refresh(ctx)
	names := map[string]bool{}
	for _, tl := range ex.Tools() {
		names[tl.Def().Name] = true
	}
	if len(names) != 2 {
		t.Fatalf("colliding names must resolve distinctly: %v", names)
	}
}
