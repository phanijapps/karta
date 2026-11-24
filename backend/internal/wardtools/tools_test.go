package wardtools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"zbot/backend/internal/wards"
)

// newTestHost bootstraps a temp vault with the seeded templates.
func newTestHost(t *testing.T) (*Host, *wards.Vault) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "wards")
	if err := wards.Bootstrap(context.Background(), root); err != nil {
		t.Fatalf("vault bootstrap: %v", err)
	}
	vault, err := wards.Open(context.Background(), root)
	if err != nil {
		t.Fatalf("vault open: %v", err)
	}
	// One research ward to enter.
	ward := filepath.Join(root, "stock-analysis")
	if err := os.MkdirAll(ward, 0o755); err != nil {
		t.Fatal(err)
	}
	for rel, content := range map[string]string{
		"ward-conf.yaml": `
archetype: research
layout:
  kind: directory
  children:
    - { id: canonical, match: "{ward}.md", kind: file, format: markdown }
    - { id: log, match: log.md, kind: file, format: markdown }
    - id: tasks
      match: tasks
      kind: directory
      required: false
      children:
        - { id: task, match: "*.md", kind: file, required: false, repeat: true, format: markdown }
`,
		"stock-analysis.md": "# Stock Analysis\nthe canonical page\n",
		"AGENTS.md":         "# Working here\n\nsources/ is immutable intake.\n",
		"log.md":            "log\n",
	} {
		if err := os.WriteFile(filepath.Join(ward, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return NewHost(vault), vault
}

func TestUseTool(t *testing.T) {
	host, vault := newTestHost(t)
	use := UseTool{Host: host, Vault: vault}
	ctx := context.Background()

	t.Run("enters an existing ward with the packet", func(t *testing.T) {
		res := use.Exec(ctx, `{"name":"stock-analysis"}`)
		if res.Err != nil {
			t.Fatalf("Exec: %v", res.Err)
		}
		for _, want := range []string{"ACTIVE WARD: stock-analysis", "digest", "UNTRUSTED LAYOUT DATA", "sources/ is immutable intake", "file canonical ({ward}.md)"} {
			if !strings.Contains(res.Output, want) {
				t.Fatalf("packet missing %q:\n%s", want, res.Output)
			}
		}
		view := host.Active()
		if view == nil || view.Name != "stock-analysis" || view.Constitution == "" || view.Digest == "" {
			t.Fatalf("host state: %+v", view)
		}
	})

	t.Run("unknown ward refuses with the roster", func(t *testing.T) {
		res := use.Exec(ctx, `{"name":"nope"}`)
		if res.Err == nil || !strings.Contains(res.Err.Error(), "stock-analysis") {
			t.Fatalf("want roster in error, got %v", res.Err)
		}
	})

	t.Run("switching wards replaces without touching disk", func(t *testing.T) {
		before, _ := os.ReadFile(filepath.Join(vault.Root(), "stock-analysis", "AGENTS.md"))
		create := CreateTool{Host: host, Vault: vault}
		if res := create.Exec(ctx, `{"name":"india-analysis","archetype":"research"}`); res.Err != nil {
			t.Fatalf("create: %v", res.Err)
		}
		after, _ := os.ReadFile(filepath.Join(vault.Root(), "stock-analysis", "AGENTS.md"))
		if string(before) != string(after) {
			t.Fatal("switching wards disturbed the previous ward")
		}
		if view := host.Active(); view.Name != "india-analysis" {
			t.Fatalf("switch: %+v", view)
		}
	})
}

func TestCreateTool(t *testing.T) {
	host, vault := newTestHost(t)
	create := CreateTool{Host: host, Vault: vault}
	ctx := context.Background()

	t.Run("instantiates a lint-clean mini bot and enters it", func(t *testing.T) {
		res := create.Exec(ctx, `{"name":"market-research","archetype":"research"}`)
		if res.Err != nil {
			t.Fatalf("create: %v", res.Err)
		}
		dir := filepath.Join(vault.Root(), "market-research")
		for _, want := range []string{"market-research.md", "ward-conf.yaml", "AGENTS.md", "log.md", "spec.md", "plan.md"} {
			if _, err := os.Stat(filepath.Join(dir, want)); err != nil {
				t.Fatalf("created ward missing %s: %v", want, err)
			}
		}
		if _, err := os.Stat(filepath.Join(dir, "canonical.md")); !os.IsNotExist(err) {
			t.Fatal("canonical starter must be renamed to <name>.md")
		}
		if host.Active() == nil || host.Active().Name != "market-research" {
			t.Fatal("create must enter the ward")
		}
	})

	t.Run("existing ward refused", func(t *testing.T) {
		res := create.Exec(ctx, `{"name":"stock-analysis","archetype":"generic"}`)
		if res.Err == nil || !strings.Contains(res.Err.Error(), "already exists") {
			t.Fatalf("want exists error, got %v", res.Err)
		}
	})

	t.Run("task-specific name refused", func(t *testing.T) {
		for _, bad := range []string{"AMD-Stock", "spy_options", "x"} {
			res := create.Exec(ctx, `{"name":"`+bad+`","archetype":"generic"}`)
			if res.Err == nil || !strings.Contains(res.Err.Error(), "reusable domain category") {
				t.Fatalf("name %q must refuse, got %v", bad, res.Err)
			}
		}
	})

	t.Run("unknown archetype refused", func(t *testing.T) {
		res := create.Exec(ctx, `{"name":"fine-name","archetype":"ebook"}`)
		if res.Err == nil || !strings.Contains(res.Err.Error(), "unknown archetype") {
			t.Fatalf("want archetype error, got %v", res.Err)
		}
	})
}

func TestDigestChange(t *testing.T) {
	host, vault := newTestHost(t)
	use := UseTool{Host: host, Vault: vault}
	ctx := context.Background()
	if res := use.Exec(ctx, `{"name":"stock-analysis"}`); res.Err != nil {
		t.Fatal(res.Err)
	}
	if host.RefreshDigest() {
		t.Fatal("no edits — digest must match")
	}
	// The user edits the constitution concurrently.
	agentsMD := filepath.Join(vault.Root(), "stock-analysis", "AGENTS.md")
	if err := os.WriteFile(agentsMD, []byte("# changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !host.RefreshDigest() {
		t.Fatal("constitution edit must change the digest")
	}
	var _ = errors.New // reserved
}

// The ward-scoped file plane: bounded reads/writes/lists, typed
// escapes, no-ward refusal, lint-voiced write warnings.
func TestFileTools(t *testing.T) {
	host, vault := newTestHost(t)
	ctx := context.Background()
	use := UseTool{Host: host, Vault: vault}
	files := FileTools{Host: host}
	read, write, list := ReadTool{F: files}, WriteTool{F: files}, ListTool{F: files}

	t.Run("no active ward refuses typed", func(t *testing.T) {
		res := read.Exec(ctx, `{"path":"x"}`)
		if res.Err == nil || !strings.Contains(res.Err.Error(), "no active ward") {
			t.Fatalf("want no-ward error, got %v", res.Err)
		}
	})

	if res := use.Exec(ctx, `{"name":"stock-analysis"}`); res.Err != nil {
		t.Fatal(res.Err)
	}

	t.Run("read/write/list round-trip", func(t *testing.T) {
		if res := write.Exec(ctx, `{"path":"tasks/t1.md","content":"# Task one\ndo the thing\n"}`); res.Err != nil {
			t.Fatalf("write: %v", res.Err)
		}
		if res := read.Exec(ctx, `{"path":"tasks/t1.md"}`); res.Err != nil || !strings.Contains(res.Output, "do the thing") {
			t.Fatalf("read: %q %v", res.Output, res.Err)
		}
		if res := list.Exec(ctx, `{}`); res.Err != nil || !strings.Contains(res.Output, "tasks/t1.md") {
			t.Fatalf("list: %q %v", res.Output, res.Err)
		}
	})

	t.Run("layout-conforming write is silent; breaking write warns", func(t *testing.T) {
		if res := write.Exec(ctx, `{"path":"log.md","content":"log line\n"}`); res.Err != nil || strings.Contains(res.Output, "WARNING") {
			t.Fatalf("conforming write must not warn: %q %v", res.Output, res.Err)
		}
		if res := write.Exec(ctx, `{"path":"stray.txt","content":"x"}`); res.Err != nil {
			t.Fatalf("write stands: %v", res.Err)
		} else if !strings.Contains(res.Output, "unexpected_entry") || !strings.Contains(res.Output, "stray.txt") {
			t.Fatalf("breaking write must carry the lint warning: %q", res.Output)
		}
	})

	t.Run("escapes refused typed", func(t *testing.T) {
		for _, p := range []string{"../outside.md", "a/../../escape", "/etc/passwd"} {
			if res := write.Exec(ctx, `{"path":"`+p+`","content":"x"}`); res.Err == nil {
				t.Fatalf("path %q must refuse", p)
			}
		}
	})

	t.Run("symlinked directory refused", func(t *testing.T) {
		outside := t.TempDir()
		link := filepath.Join(vault.Root(), "stock-analysis", "linked")
		if err := os.Symlink(outside, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		if res := write.Exec(ctx, `{"path":"linked/x.md","content":"x"}`); res.Err == nil || !strings.Contains(res.Err.Error(), "symlink") {
			t.Fatalf("want symlink refusal, got %v", res.Err)
		}
	})
}

// TestOnEnterFires (add-chat-modes): every enter (ward_use,
// ward_create, and the adapter's EnsureWard restart recovery) fires
// the observer exactly once per successful enter — the session-row
// ward binding hangs off it. Failed enters never fire.
func TestOnEnterFires(t *testing.T) {
	host, vault := newTestHost(t)
	var mu sync.Mutex
	var fired []string
	host.SetOnEnter(func(ward string) {
		mu.Lock()
		defer mu.Unlock()
		fired = append(fired, ward)
	})
	ctx := context.Background()

	use := UseTool{Host: host, Vault: vault}
	if res := use.Exec(ctx, `{"name":"stock-analysis"}`); res.Err != nil {
		t.Fatalf("use: %v", res.Err)
	}
	// Reenter via the wiring path (daemon restart recovery).
	if _, err := host.Reenter(ctx, "stock-analysis"); err != nil {
		t.Fatalf("reenter: %v", err)
	}
	// A failed enter (unknown ward) fires nothing.
	if _, err := host.Reenter(ctx, "nope"); err == nil {
		t.Fatal("unknown ward reenter should fail")
	}

	mu.Lock()
	defer mu.Unlock()
	if len(fired) != 2 || fired[0] != "stock-analysis" || fired[1] != "stock-analysis" {
		t.Fatalf("fired: %v", fired)
	}
}

// TestShellTool (task 2.1): ward_shell runs bash pinned to the ward,
// scrubs env, caps output, times out, and refuses without a ward.
// Non-zero exits are RESULTS (model-visible), not tool errors.
func TestShellTool(t *testing.T) {
	host, vault := newTestHost(t)
	sh := ShellTool{Host: host}
	ctx := context.Background()

	t.Run("refuses without a ward", func(t *testing.T) {
		res := sh.Exec(ctx, `{"command":"pwd"}`)
		if res.Err == nil || !strings.Contains(res.Err.Error(), "ward_use") {
			t.Fatalf("want typed refusal naming ward_use, got %v", res.Err)
		}
	})

	enter := func() {
		use := UseTool{Host: host, Vault: vault}
		if res := use.Exec(ctx, `{"name":"stock-analysis"}`); res.Err != nil {
			t.Fatalf("enter: %v", res.Err)
		}
	}
	enter()

	t.Run("cwd pinned to the ward", func(t *testing.T) {
		res := sh.Exec(ctx, `{"command":"pwd"}`)
		if res.Err != nil {
			t.Fatalf("exec: %v", res.Err)
		}
		if !strings.Contains(res.Output, host.Active().Dir) {
			t.Fatalf("cwd not the ward: %s", res.Output)
		}
	})

	t.Run("env scrubbed + ward vars present", func(t *testing.T) {
		t.Setenv("SECRET_TOKEN", "leaky") //nolint:paralleltest // setenv in subtest
		res := sh.Exec(ctx, `{"command":"echo ${SECRET_TOKEN:-no-secret} ${WARD_NAME:-no-ward}"}`)
		if strings.Contains(res.Output, "leaky") {
			t.Fatalf("env leaked: %s", res.Output)
		}
		if !strings.Contains(res.Output, "stock-analysis") {
			t.Fatalf("WARD_NAME missing: %s", res.Output)
		}
	})

	t.Run("writes and runs code in apps/", func(t *testing.T) {
		res := sh.Exec(ctx, `{"command":"mkdir -p apps && printf 'print(6*7)\\n' > apps/x.py && python3 apps/x.py"}`)
		if res.Err != nil || !strings.Contains(res.Output, "42") {
			t.Fatalf("apps round-trip: %v %s", res.Err, res.Output)
		}
	})

	t.Run("non-zero exit is a result", func(t *testing.T) {
		res := sh.Exec(ctx, `{"command":"exit 3"}`)
		if res.Err != nil {
			t.Fatalf("exit code must not be a tool error: %v", res.Err)
		}
		if !strings.Contains(res.Output, "[exit:") {
			t.Fatalf("exit not reported: %s", res.Output)
		}
	})

	t.Run("timeout bounds the command", func(t *testing.T) {
		res := sh.Exec(ctx, `{"command":"sleep 5","timeout_seconds":1}`)
		if !strings.Contains(res.Output, "[timeout after 1s]") {
			t.Fatalf("timeout missing: %s", res.Output)
		}
	})

	t.Run("output cap truncates", func(t *testing.T) {
		res := sh.Exec(ctx, `{"command":"head -c 400000 /dev/zero | tr '\\0' 'x'"}`)
		if !strings.Contains(res.Output, "[output truncated at 256KB]") {
			t.Fatalf("truncation missing (len %d)", len(res.Output))
		}
		if len(res.Output) > shellOutputCap+1024 {
			t.Fatalf("cap exceeded: %d", len(res.Output))
		}
	})
}

// TestShellTimeoutKillsGroup (gate fix): a command that backgrounds a
// descendant cannot outlive the timeout — the process GROUP dies.
func TestShellTimeoutKillsGroup(t *testing.T) {
	host, vault := newTestHost(t)
	use := UseTool{Host: host, Vault: vault}
	if res := use.Exec(context.Background(), `{"name":"stock-analysis"}`); res.Err != nil {
		t.Fatalf("enter: %v", res.Err)
	}
	sh := ShellTool{Host: host}
	marker := filepath.Join(host.Active().Dir, "apps", ".group-marker")
	res := sh.Exec(context.Background(), fmt.Sprintf(
		`{"command":"rm -f %s && (sleep 2 && touch %s) & wait","timeout_seconds":1}`, marker, marker))
	if !strings.Contains(res.Output, "[timeout after 1s]") {
		t.Fatalf("timeout missing: %s", res.Output)
	}
	// The backgrounded touch must NOT survive: WITHOUT the group kill,
	// the descendant lands at ~2s — poll to 3s so a regression FAILS.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			t.Fatal("descendant survived the group kill")
		}
		time.Sleep(100 * time.Millisecond)
	}
}
