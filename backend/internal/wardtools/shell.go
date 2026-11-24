package wardtools

// ward_shell: bounded bash execution pinned to the ACTIVE ward
// (ward-v2's execution plane — agentzero's proven shape). The command
// runs with the ward root as its working directory for the whole
// invocation; env is scrubbed to a safe allowlist plus ward vars; the
// combined output is capped. A non-zero exit is a RESULT (the model
// reacts to it), not a tool error — only refusals and setup failures
// error.

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Shell execution bounds (recorded trust decision: unsandboxed,
// cwd-pinned — the operator's agentzero parity).
const (
	shellDefaultTimeout = 120 * time.Second
	shellMaxTimeout     = 300 * time.Second
	shellOutputCap      = 256 << 10 // 256KB combined stdout+stderr
)

// shellEnvAllow is the environment a ward command sees (everything
// else is scrubbed — no credentials leak into ward processes).
var shellEnvAllow = []string{"PATH", "HOME", "LANG", "LC_ALL", "TERM", "TMPDIR"}

// ShellTool is ward_shell(command, timeout_seconds?) — offered to
// hosted runs (root AND children: children execute inside the ward).
type ShellTool struct{ Host *Host }

// Name implements Tool.
func (t ShellTool) Name() string { return "ward_shell" }

// Def implements Tool.
func (t ShellTool) Def() Def {
	return Def{
		Name: "ward_shell",
		Description: "Run a bash command INSIDE the active ward (its directory is the working directory for the whole command). " +
			"Use it to execute the ward's apps/ scripts, install deps, transform data, and generate reports/. " +
			"Env is scrubbed; combined output caps at 256KB; default timeout 120s (max 300). " +
			"Requires an active ward — enter one with ward_use first.",
		ParamsJSON: `{"type":"object","properties":{
			"command":{"type":"string","description":"the bash command (runs with the ward root as cwd; prefer relative paths)"},
			"timeout_seconds":{"type":"integer","description":"optional timeout (default 120, max 300)"}
		},"required":["command"]}`,
	}
}

// limitWriter caps accumulated output and records truncation.
type limitWriter struct {
	buf       strings.Builder
	remaining int
	truncated bool
}

func (lw *limitWriter) Write(p []byte) (int, error) {
	n := len(p)
	if lw.remaining <= 0 {
		lw.truncated = true
		return n, nil // swallow but report success (bounded capture)
	}
	if len(p) > lw.remaining {
		p = p[:lw.remaining]
		lw.truncated = true
	}
	lw.buf.Write(p)
	lw.remaining -= len(p)
	return n, nil
}

// Exec runs the command in the ward.
func (t ShellTool) Exec(ctx context.Context, args string) Result {
	var parsed struct {
		Command        string `json:"command"`
		TimeoutSeconds *int   `json:"timeout_seconds"`
	}
	if err := decodeArgs(args, &parsed); err != nil {
		return Result{Err: fmt.Errorf("ward_shell: %w", err)}
	}
	command := strings.TrimSpace(parsed.Command)
	if command == "" {
		return Result{Err: fmt.Errorf("ward_shell: command must not be empty")}
	}
	view := t.Host.Active()
	if view == nil {
		return Result{Err: fmt.Errorf("ward_shell: no active ward — call ward_use(name=...) first")}
	}
	timeout := shellDefaultTimeout
	if parsed.TimeoutSeconds != nil {
		timeout = time.Duration(*parsed.TimeoutSeconds) * time.Second
		if timeout > shellMaxTimeout {
			timeout = shellMaxTimeout
		}
		if timeout <= 0 {
			timeout = shellDefaultTimeout
		}
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "bash", "-lc", command)
	cmd.Dir = view.Dir
	env := []string{
		"WARD_DIR=" + view.Dir,
		"WARD_NAME=" + view.Name,
	}
	for _, k := range shellEnvAllow {
		if v, ok := os.LookupEnv(k); ok {
			env = append(env, k+"="+v)
		}
	}
	cmd.Env = env
	// Process-group discipline: the command runs as its own group
	// leader; on timeout/cancel the WHOLE group is signaled — a
	// background descendant cannot outlive the bound and keep writing.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) //nolint:errcheck // best-effort group kill
		}
		return cmd.Process.Kill()
	}
	cmd.WaitDelay = 2 * time.Second

	lw := &limitWriter{remaining: shellOutputCap}
	cmd.Stdout, cmd.Stderr = lw, lw
	start := time.Now()
	err := cmd.Run()
	out := lw.buf.String()
	var b strings.Builder
	fmt.Fprintf(&b, "$ %s\n", command)
	b.WriteString(out)
	if lw.truncated {
		b.WriteString("\n[output truncated at 256KB]")
	}
	if runCtx.Err() == context.DeadlineExceeded {
		fmt.Fprintf(&b, "\n[timeout after %s]", timeout)
	} else if err != nil {
		fmt.Fprintf(&b, "\n[exit: %s]", err)
	}
	fmt.Fprintf(&b, "\n[%s in %s]", time.Since(start).Round(time.Millisecond), view.Name)
	return Result{Output: b.String()}
}
