// Package hooks runs the operator's hook manifests by the backend
// constitution's exit-code contract: a hook's command runs through a shell
// with the HookEvent as JSON on stdin; exit 0 allows, exit 2 blocks (stderr
// is the reason the agent sees as a recoverable error), any other non-zero
// exit or a timeout warns and continues. The runner is kernel-pure (domain +
// registry): callers emit decisions to the Timeline.
package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"zbot/backend/internal/domain"
	"zbot/backend/internal/registry"
)

// Defaults and bounds.
const (
	DefaultTimeout = 10 * time.Second
	MinTimeout     = time.Second
	MaxTimeout     = 60 * time.Second
	DefaultOutput  = 16 * 1024 // bytes captured per stream
	maxReason      = 2048      // bytes of the combined reason
)

// Runner implements domain.HookRunner over the registry's hook manifests.
type Runner struct {
	// Snapshot supplies the live roster (nil = no hooks, everything allows
	// — with an error, so a mis-wiring is visible to the caller).
	Snapshot func() registry.Snapshot
	// DefaultTimeout applies when a manifest sets no timeout_ms (0 = 10s).
	DefaultTimeout time.Duration
	// MaxOutput bounds the bytes captured from stdout and stderr each
	// (0 = 16 KB); the process keeps running, the excess is drained.
	MaxOutput int
}

// Subject returns what a matcher applies to for an event: the tool name for
// PreToolUse/PostToolUse, the agent name for SubagentStop, "" otherwise
// (subject-less events ignore matchers).
func Subject(ev domain.HookEvent) string {
	switch ev.Event {
	case domain.HookPreToolUse, domain.HookPostToolUse:
		return ev.ToolName
	case domain.HookSubagentStop:
		a, _ := ev.Payload["agent"].(string)
		return a
	}
	return ""
}

// Run executes every hook bound to the event (matcher permitting, in key
// order) and combines their decisions. A hook failure is a decision, never
// an error; the error is reserved for a missing roster.
func (r *Runner) Run(ctx context.Context, ev domain.HookEvent) (domain.HookDecision, error) {
	if r == nil || r.Snapshot == nil {
		return domain.HookDecision{Action: domain.HookAllow}, errors.New("hooks: no roster")
	}
	subject := Subject(ev)
	snap := r.Snapshot() // one roster read per event
	var keys []string
	for k, spec := range snap.Hooks {
		if spec.Event != ev.Event {
			continue
		}
		if spec.Matcher != nil && subject != "" && !spec.Matcher.MatchString(subject) {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	action := domain.HookAllow
	var reasons, ran []string
	for _, k := range keys {
		ran = append(ran, k)
		d := r.exec(ctx, k, snap.Hooks[k], ev)
		switch {
		case d.Action == domain.HookBlock:
			action = domain.HookBlock
		case d.Action == domain.HookWarn && action != domain.HookBlock:
			action = domain.HookWarn
		}
		if d.Reason != "" && d.Action != domain.HookAllow {
			reasons = append(reasons, k+": "+d.Reason)
		}
	}
	context := map[string]any{"hooks": ran, "event": string(ev.Event)}
	if subject != "" {
		context["subject"] = subject
	}
	return domain.HookDecision{Action: action, Reason: bound(strings.Join(reasons, "; "), maxReason), Context: context}, nil
}

// exec runs one hook and maps its outcome to a decision.
func (r *Runner) exec(ctx context.Context, key string, spec registry.HookSpec, ev domain.HookEvent) domain.HookDecision {
	timeout := r.timeoutFor(spec)
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	payload, _ := json.Marshal(ev)
	cmd := exec.CommandContext(cctx, "sh", "-c", spec.Command)
	cmd.Dir = filepath.Dir(spec.Path) // relative script paths resolve beside their manifest
	cmd.Stdin = bytes.NewReader(payload)
	cmd.WaitDelay = time.Second
	if attr := hookProcAttr(); attr != nil {
		cmd.SysProcAttr = attr
	}
	// On timeout (or a cancelled parent) the WHOLE process group dies — a
	// compound command's children must not outlive the hook.
	cmd.Cancel = func() error {
		killGroup(cmd)
		return cmd.Process.Kill()
	}
	max := r.MaxOutput
	if max <= 0 {
		max = DefaultOutput
	}
	var out, errOut boundedBuffer
	out.limit, errOut.limit = max, max
	cmd.Stdout, cmd.Stderr = &out, &errOut
	err := cmd.Run()
	stderr := strings.TrimSpace(errOut.String())
	switch {
	case cctx.Err() == context.DeadlineExceeded:
		return warn(fmt.Sprintf("timed out after %s", timeout))
	case err == nil:
		return domain.HookDecision{Action: domain.HookAllow}
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && exitErr.ExitCode() == 2 {
		if stderr == "" {
			stderr = "blocked by hook " + key
		}
		return domain.HookDecision{Action: domain.HookBlock, Reason: bound(stderr, maxReason)}
	}
	if stderr == "" {
		stderr = err.Error()
	}
	return warn(bound(stderr, maxReason))
}

func warn(reason string) domain.HookDecision {
	return domain.HookDecision{Action: domain.HookWarn, Reason: reason}
}

func (r *Runner) timeoutFor(spec registry.HookSpec) time.Duration {
	if spec.TimeoutMS > 0 {
		return clamp(time.Duration(spec.TimeoutMS)*time.Millisecond, MinTimeout, MaxTimeout)
	}
	if r.DefaultTimeout > 0 {
		return clamp(r.DefaultTimeout, MinTimeout, MaxTimeout)
	}
	return DefaultTimeout
}

func clamp(v, lo, hi time.Duration) time.Duration {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func bound(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// boundedBuffer keeps the first `limit` bytes and drains the rest, so a
// chatty hook never blocks on a full pipe.
type boundedBuffer struct {
	buf   bytes.Buffer
	limit int
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if remain := b.limit - b.buf.Len(); remain > 0 {
		if len(p) > remain {
			b.buf.Write(p[:remain])
		} else {
			b.buf.Write(p)
		}
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string { return b.buf.String() }
