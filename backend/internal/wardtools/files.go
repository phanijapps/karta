package wardtools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zbot/backend/internal/wards"
)

// FileTools are the ward-scoped file plane: read, write,
// ward_list — offered to root runs AND delegated children (children act
// through the same bounded plane; STRUCTURAL ward actions are the
// root-only guard). Every path resolves inside the ACTIVE ward:
// traversal above it, symlinked directories, and calls without an
// active ward are typed errors. write reports lint findings for
// the touched path as a WARNING line (the write stands; the conf
// speaks through lint).
type FileTools struct {
	Host *Host
}

// Def describes the read tool.
func (f FileTools) readDef() Def {
	return Def{
		Name:        "read",
		Description: "Read a file inside the ACTIVE ward (path relative to the ward root). Bounded: paths outside the ward are refused.",
		ParamsJSON:  `{"type":"object","properties":{"path":{"type":"string","description":"ward-relative path"}},"required":["path"]}`,
	}
}

// Def describes the write tool.
func (f FileTools) writeDef() Def {
	return Def{
		Name:        "write",
		Description: "Write a file inside the ACTIVE ward (path relative to the ward root; parent directories are created as the layout allows). The result notes layout-lint findings as warnings — the write stands.",
		ParamsJSON:  `{"type":"object","properties":{"path":{"type":"string","description":"ward-relative path"},"content":{"type":"string","description":"full file content"}},"required":["path","content"]}`,
	}
}

// Def describes the list tool.
func (f FileTools) listDef() Def {
	return Def{
		Name:        "ward_list",
		Description: "List files under a directory of the ACTIVE ward (recursive, ward-relative paths).",
		ParamsJSON:  `{"type":"object","properties":{"path":{"type":"string","description":"ward-relative directory (default: the ward root)"}}}`,
	}
}

// ReadTool is the exposed read (ward-scoped).
type ReadTool struct{ F FileTools }

// Def describes read.
func (t ReadTool) Def() Def { return t.F.readDef() }

// Exec reads one bounded file.
func (t ReadTool) Exec(ctx context.Context, argsJSON string) Result {
	var args struct{ Path string }
	if err := decodeArgs(argsJSON, &args); err != nil {
		return Result{Err: err}
	}
	full, err := t.F.resolve(args.Path)
	if err != nil {
		return Result{Err: err}
	}
	raw, err := readNoFollow(full)
	if err != nil {
		return Result{Err: fmt.Errorf("read: %w", err)}
	}
	out := string(raw)
	if t.F.Host.RefreshDigest() {
		out += "\nWARNING: the ward changed outside this session since the last operation (digest mismatch)."
	}
	return Result{Output: out}
}

// WriteTool is the exposed write (ward-scoped).
type WriteTool struct{ F FileTools }

// Def describes write.
func (t WriteTool) Def() Def { return t.F.writeDef() }

// Exec writes one bounded file with lint-voiced warnings.
func (t WriteTool) Exec(ctx context.Context, argsJSON string) Result {
	var args struct {
		Path    string
		Content string
	}
	if err := decodeArgs(argsJSON, &args); err != nil {
		return Result{Err: err}
	}
	if args.Path == "" {
		return Result{Err: errors.New("write: path is required")}
	}
	full, err := t.F.resolve(args.Path)
	if err != nil {
		return Result{Err: err}
	}
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return Result{Err: fmt.Errorf("write: %w", err)}
	}
	if err := writeNoFollow(full, []byte(args.Content)); err != nil {
		return Result{Err: fmt.Errorf("write: %w", err)}
	}
	out := "wrote " + args.Path
	if warn := t.F.Host.RefreshDigest(); warn {
		out += "\nWARNING: the ward changed outside this session since the last operation (digest mismatch)."
	}
	if warn := t.F.lintWarning(ctx, args.Path); warn != "" {
		out += "\nWARNING (layout lint): " + warn
	}
	return Result{Output: out}
}

// ListTool is the exposed ward_list.
type ListTool struct{ F FileTools }

// Def describes ward_list.
func (t ListTool) Def() Def { return t.F.listDef() }

// Exec lists a bounded subtree.
func (t ListTool) Exec(ctx context.Context, argsJSON string) Result {
	var args struct{ Path string }
	if err := decodeArgs(argsJSON, &args); err != nil {
		return Result{Err: err}
	}
	full, err := t.F.resolve(args.Path)
	if err != nil {
		return Result{Err: err}
	}
	var files []string
	err = filepath.WalkDir(full, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(t.F.Host.Active().Dir, p)
		if rerr != nil {
			return nil
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return Result{Err: fmt.Errorf("ward_list: %s does not exist", args.Path)}
		}
		return Result{Err: fmt.Errorf("ward_list: %w", err)}
	}
	sort.Strings(files)
	out := "(empty)"
	if len(files) > 0 {
		out = strings.Join(files, "\n")
	}
	// Digest discipline: EVERY ward tool call reports a ward that
	// changed outside the session (the spec's "next call warns").
	if t.F.Host.RefreshDigest() {
		out += "\nWARNING: the ward changed outside this session since the last operation (digest mismatch)."
	}
	return Result{Output: out}
}

// resolve maps a ward-relative path to the absolute path INSIDE the
// active ward, refusing traversal, absolute inputs, and symlinked
// directories (v1: lexical containment + Lstat on each parent).
func (f FileTools) resolve(rel string) (string, error) {
	view := f.Host.Active()
	if view == nil {
		return "", errors.New("no active ward — call ward_use (or ward_create) first")
	}
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return view.Dir, nil
	}
	if filepath.IsAbs(rel) {
		return "", fmt.Errorf("path %q must be ward-RELATIVE", rel)
	}
	clean := filepath.Clean(rel)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes the ward", rel)
	}
	full := filepath.Join(view.Dir, clean)
	// Lexical containment + no symlinked directories on the path.
	if !strings.HasPrefix(full, view.Dir+string(filepath.Separator)) && full != view.Dir {
		return "", fmt.Errorf("path %q escapes the ward", rel)
	}
	for dir := filepath.Dir(full); strings.HasPrefix(dir, view.Dir); dir = filepath.Dir(dir) {
		if fi, err := os.Lstat(dir); err == nil && fi.Mode()&os.ModeSymlink != 0 {
			return "", fmt.Errorf("path %q crosses a symlinked directory (not allowed in wards)", rel)
		}
	}
	return full, nil
}

// Exists reports whether a ward-relative path exists INSIDE the active
// ward — the same containment resolve enforces (no traversal, no
// symlinked directories) plus a leaf check that follows no symlink out
// of the ward. Used by host-side receipts, never by the tools.
func (f FileTools) Exists(rel string) (bool, error) {
	full, err := f.resolve(rel)
	if err != nil {
		return false, err
	}
	fi, err := os.Lstat(full)
	if err != nil {
		return false, nil // absent (or unreadable): not an error to the caller
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		// A symlinked leaf may point outside the ward — resolve it and
		// require the target to stay inside.
		target, err := filepath.EvalSymlinks(full)
		if err != nil {
			return false, nil
		}
		view := f.Host.Active()
		if view == nil || (!strings.HasPrefix(target, view.Dir+string(filepath.Separator)) && target != view.Dir) {
			return false, nil
		}
	}
	return true, nil
}

// lintWarning runs the ward's linter over the touched path and renders
// its findings for the tool result ("" when clean).
func (f FileTools) lintWarning(ctx context.Context, rel string) string {
	view := f.Host.Active()
	if view == nil || view.Conf == nil {
		return ""
	}
	findings, err := wards.Lint(ctx, view.Dir, view.Name, view.Conf)
	if err != nil {
		return ""
	}
	var msgs []string
	for _, fi := range findings {
		if fi.Path == filepath.ToSlash(rel) || strings.HasPrefix(fi.Path, filepath.ToSlash(rel)+"/") {
			msgs = append(msgs, fmt.Sprintf("%s [%s]: %s", fi.Path, fi.Kind, fi.Message))
		}
	}
	return strings.Join(msgs, "; ")
}
