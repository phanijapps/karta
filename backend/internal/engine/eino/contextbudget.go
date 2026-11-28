// The context-budget building blocks (engine-adk-spine slice 1): what
// the daemon applies per run (RunOptions), the offload backend the
// reduction middleware writes oversized tool results to, the ranged
// reader the model re-reads them with, the skill backend scoped to the
// analysis's recommendations, and the model wrapper that carries a
// reasoning effort. The middlewares themselves are Eino's built-ins;
// this file is the glue that keeps their pointers actionable and their
// scope the operator's decisions (D3 skills, D4 summarizer, D5 effort).

package eino

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	einoopenai "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk/filesystem"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"zbot/backend/internal/registry"
	"zbot/backend/internal/tools"
)

// RunOptions are the daemon-applied context-budget knobs. The zero value
// keeps slice-0 behavior (no cap, no in-run summarization, skills in the
// prompt, provider-default effort).
type RunOptions struct {
	// ToolResultCap bounds one tool result's contribution to the
	// model-facing context, in BYTES; 0 disables the cap.
	ToolResultCap int
	// SummarizeAt is the in-run compaction budget (tokens, chars/4
	// estimate): the bridge asks the compactor once the context exceeds
	// it. 0 disables in-run compaction. Which model summarizes is the
	// policy's business (internal/compaction; `compaction.summarizer`).
	SummarizeAt int
	// ProgressiveSkills offers the recommended skills through the skill
	// tool instead of inlining their bodies (ADK path only).
	ProgressiveSkills bool
}

// SetRunOptions applies the daemon's context-budget configuration.
func (a *Adapter) SetRunOptions(o RunOptions) { a.opts = o }

// Names the context-budget tools carry on the wire. The `karta_` prefix is
// RESERVED for engine-provided tools: a user or MCP tool of the same name
// would be shadowed or break a notice's pointer (review F10).
const (
	reservedToolPrefix = "karta_"
	readFileToolName   = "karta_read_file"
	skillToolName      = "karta_skill"
)

// offloadRoot is a run's offload directory: canonical (symlinked temp
// ancestors resolved — macOS /tmp → /private/tmp) and held as an os.Root,
// whose methods can only reach files BENEATH the root (the kernel refuses
// escapes and symlinks out of the tree — no path-name races to win).
type offloadRoot struct {
	dir  string
	root *os.Root
}

func newOffloadRoot() (*offloadRoot, error) {
	dir, err := os.MkdirTemp("", "karta-tool-results-")
	if err != nil {
		return nil, err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err == nil {
		dir = real
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		_ = os.RemoveAll(dir) //nolint:errcheck // best-effort cleanup of a temp dir
		return nil, err
	}
	return &offloadRoot{dir: dir, root: root}, nil
}

// close releases the root handle; the driver removes the directory.
func (o *offloadRoot) close() { _ = o.root.Close() } //nolint:errcheck // read-only handle hygiene

// rel maps an absolute path the middleware produced to a root-relative
// name; anything not lexically under the root is refused here, and
// os.Root enforces the rest.
func (o *offloadRoot) rel(p string) (string, error) {
	rel, err := filepath.Rel(o.dir, filepath.Clean(p))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("%w: path outside this run's offloaded results", ErrToolNotOffered)
	}
	return rel, nil
}

// offloadBackend is the reduction middleware's Backend: oversized (or
// cleared) tool results land under the run's root — the ORIGINAL bytes,
// unchanged — readable only through the ranged reader.
type offloadBackend struct{ offload *offloadRoot }

func (b offloadBackend) Write(_ context.Context, req *filesystem.WriteRequest) error {
	rel, err := b.offload.rel(req.FilePath)
	if err != nil {
		return fmt.Errorf("tool-result offload: %w", err)
	}
	if err := b.offload.root.MkdirAll(filepath.Dir(rel), 0o700); err != nil {
		return fmt.Errorf("tool-result offload: %w", err)
	}
	f, err := b.offload.root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("tool-result offload: %w", err)
	}
	defer f.Close() //nolint:errcheck // write handle hygiene
	if _, err := f.WriteString(req.Content); err != nil {
		return fmt.Errorf("tool-result offload: %w", err)
	}
	return f.Close()
}

// readFileTool is the ranged reader the reduction notice points at. It
// serves BYTE ranges of the original content (the solar run's dumps were
// single 25–38 KB lines — line ranges would return them whole) and reads
// ONLY files beneath the run's offload root (os.Root).
type readFileTool struct{ offload *offloadRoot }

func (readFileTool) Def() tools.Def {
	return tools.Def{
		Name:        readFileToolName,
		Description: "Read a BYTE range of an offloaded tool result — the path from an \"Output too large … saved to\" notice. Start at offset 0; each answer says where to continue. Read a slice at a time; the full original is at that path.",
		ParamsJSON:  `{"type":"object","properties":{"path":{"type":"string","description":"path from the too-large notice"},"offset":{"type":"integer","description":"first byte to read, 0-based (default 0)"},"limit":{"type":"integer","description":"how many bytes (default 4096, max 16384)"}},"required":["path"]}`,
	}
}

func (r readFileTool) Exec(_ context.Context, argsJSON string) tools.Result {
	var args struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
		Limit  int    `json:"limit"`
	}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.Path == "" {
		return tools.Result{Err: fmt.Errorf("%s: arguments must be {path, offset?, limit?}: %w", readFileToolName, err)}
	}
	rel, err := r.offload.rel(args.Path)
	if err != nil {
		return tools.Result{Err: fmt.Errorf("%s: %w", readFileToolName, err)}
	}
	f, err := r.offload.root.Open(rel) // refuses symlinks and traversals out of the root
	if err != nil {
		return tools.Result{Err: fmt.Errorf("%s: %w", readFileToolName, err)}
	}
	defer f.Close() //nolint:errcheck // read-only handle hygiene
	raw, err := io.ReadAll(f)
	if err != nil {
		return tools.Result{Err: fmt.Errorf("%s: %w", readFileToolName, err)}
	}
	if args.Limit <= 0 {
		args.Limit = 4096
	}
	if args.Limit > 16384 {
		args.Limit = 16384
	}
	if args.Offset < 0 {
		args.Offset = 0
	}
	endWanted := len(raw)
	if args.Limit < len(raw)-args.Offset {
		endWanted = args.Offset + args.Limit // no overflow: bounded by len(raw)
	}
	start, end := clampRange(raw, args.Offset, endWanted)
	var b strings.Builder
	fmt.Fprintf(&b, "[bytes %d-%d of %d]", start, end, len(raw))
	if end < len(raw) {
		fmt.Fprintf(&b, " — continue with offset=%d", end)
	}
	b.WriteByte('\n')
	b.Write(raw[start:end])
	b.WriteByte('\n')
	return tools.Result{Output: b.String()}
}

// scopedSkillBackend is the skill middleware's Backend over the
// registry's provisioned skills, scoped to the analysis's
// recommendations (decision D3): a non-empty allowlist offers exactly
// those; an empty one offers every provisioned skill (the fallback).
type scopedSkillBackend struct {
	snap  func() registry.Snapshot
	allow []string
}

func (b *scopedSkillBackend) permitted() map[string]registry.SkillSpec {
	specs := b.snap().Skills
	if len(b.allow) == 0 {
		return specs
	}
	out := map[string]registry.SkillSpec{}
	for _, n := range b.allow {
		if s, ok := specs[n]; ok {
			out[n] = s
		}
	}
	return out
}

func (b *scopedSkillBackend) List(context.Context) ([]skill.FrontMatter, error) {
	permitted := b.permitted()
	names := make([]string, 0, len(permitted))
	for n := range permitted {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]skill.FrontMatter, 0, len(names))
	for _, n := range names {
		out = append(out, skill.FrontMatter{Name: permitted[n].Name, Description: permitted[n].Description})
	}
	return out, nil
}

func (b *scopedSkillBackend) Get(_ context.Context, name string) (skill.Skill, error) {
	spec, ok := b.permitted()[name]
	if !ok {
		return skill.Skill{}, fmt.Errorf("skill %q is not offered in this run", name)
	}
	return skill.Skill{FrontMatter: skill.FrontMatter{Name: spec.Name, Description: spec.Description}, Content: spec.Body, BaseDirectory: spec.Path}, nil
}

// EffortOption is the model option that carries a reasoning effort on a
// call — the OpenAI-compatible `reasoning_effort` field, which Ollama ≥
// 0.31 translates (`none` turns thinking off; the type is a plain string,
// so `none` rides it). The adapter holds it as a field (set in New) so a
// test can observe the hand-off on ITS adapter without a package global.
type EffortOption func(effort string) model.Option

// DefaultEffortOption is the production option.
func DefaultEffortOption(effort string) model.Option {
	return einoopenai.WithReasoningEffort(einoopenai.ReasoningEffortLevel(effort))
}

// withEffort returns a model whose every call carries the effort. Empty
// effort = the model unchanged (the provider's default).
func withEffort(m model.BaseModel[*schema.Message], effort string, opt EffortOption) model.BaseModel[*schema.Message] {
	if effort == "" || opt == nil {
		return m
	}
	return &effortModel{BaseModel: m, effort: effort, opt: opt}
}

type effortModel struct {
	model.BaseModel[*schema.Message]
	effort string
	opt    EffortOption
}

func (e *effortModel) Generate(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return e.BaseModel.Generate(ctx, msgs, append(opts, e.opt(e.effort))...)
}

func (e *effortModel) Stream(ctx context.Context, msgs []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return e.BaseModel.Stream(ctx, msgs, append(opts, e.opt(e.effort))...)
}

// clampRange bounds [start, end) to the content and to rune boundaries,
// and always makes progress: a limit shorter than the next rune still
// yields that one rune, so a continuation offset never repeats.
func clampRange(raw []byte, start, end int) (int, int) {
	if start < 0 {
		start = 0
	}
	if start > len(raw) {
		start = len(raw)
	}
	for start > 0 && start < len(raw) && !utf8.RuneStart(raw[start]) {
		start--
	}
	if end > len(raw) {
		end = len(raw)
	}
	if end < start {
		end = start
	}
	for end > start && end < len(raw) && !utf8.RuneStart(raw[end]) {
		end--
	}
	if end == start && start < len(raw) {
		_, width := utf8.DecodeRune(raw[start:]) // ≥ 1 byte: the whole rune at start
		end = start + width
		if end > len(raw) {
			end = len(raw)
		}
	}
	return start, end
}
