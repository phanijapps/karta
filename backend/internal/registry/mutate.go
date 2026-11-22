package registry

import ()

// This file implements the registry's write paths (design D1–D3 of the
// manage-mcps-and-skills change). Invariants:
//
//   - Mutations run under reloadMu, serialized with watcher flushes and
//     LoadAll, so no reload can publish over a completed mutation.
//   - Writes are atomic (temp file + rename/link); creates and rename
//     targets install with NO-CLOBBER semantics (hard link for files,
//     renameat2(RENAME_NOREPLACE) for directories on Linux), so an
//     occupied path fails duplicate_name even when the occupant is
//     invisible to the snapshot (invalid file, debounce window,
//     assets-only directory) or racing an external creator.
//   - After ANY failed filesystem step the kind is reloaded from disk and
//     THAT is published (disk truth wins; the snapshot never diverges).
//   - The single publication path (reloadKindLocked) computes and emits
//     the entry-level diff for the WHOLE kind: mutation results, external
//     edits absorbed by a racing reload, and failure residuals all notify
//     exactly once; no-diff reloads (a mutation's own watcher settle)
//     emit nothing.

// errFileExists marks a no-clobber install that lost a race.
func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// Ready reports (read-only) whether the registry is configured for
// kind — mutations and imports preflight on it so a wiring bug fails
// before any entry instead of surfacing as per-entry io errors.
func (r *Registry) Ready(kind Kind) bool {
	dirs := r.dirsPtr.Load()
	return dirs != nil && dirs.byKind()[kind] != ""
}

// kindDirLocked returns the configured directory for kind; mutations need
// LoadAll (or Watch) to have configured the registry first.
func (r *Registry) kindDirLocked(kind Kind) (string, error) {
	dirs := r.dirsPtr.Load()
	if dirs == nil {
		return "", mutationErr(ErrUnconfigured, "registry has no configured directories (LoadAll first)")
	}
	dir := dirs.byKind()[kind]
	if dir == "" {
		return "", mutationErr(ErrUnconfigured, "registry has no %s directory configured", kind)
	}
	return dir, nil
}

// reloadKindLocked rebuilds one kind from disk into a fresh snapshot,
// stores it, refreshes that kind's load errors, emits the entry-level
// diff for the WHOLE kind (mutation results, external edits absorbed by
// this reload, failure residuals — each exactly once; no diff, no
// notifications), and returns the new snapshot. Caller holds reloadMu.
// This is the single publish path shared by mutations and watcher
// flushes — disk truth wins.
func (r *Registry) reloadKindLocked(kind Kind) Snapshot {
	dirs := r.dirsPtr.Load()
	var dir string
	if dirs != nil {
		dir = dirs.byKind()[kind]
	}
	fresh := emptySnapshot()
	var errs []LoadError
	if dir != "" {
		loadKind(fresh, dir, kind, &errs)
	}
	current := r.snap.Load()
	next := emptySnapshot()
	*next = *current
	switch kind {
	case KindPrompt:
		next.Prompts = fresh.Prompts
	case KindAgent:
		next.Agents = fresh.Agents
	case KindSkill:
		next.Skills = fresh.Skills
	case KindHook:
		next.Hooks = fresh.Hooks
	case KindMCP:
		next.MCPs = fresh.MCPs
	}
	r.snap.Store(next)
	r.storeErrorsLocked(kind, errs)
	r.notifyKindDiff(kind, current, next)
	return *next
}

// notifyKindDiff emits added/removed/changed for one kind's old→new
// maps; changed means a real content difference (reflect.DeepEqual),
// so a reload that re-reads identical state emits nothing.
func (r *Registry) notifyKindDiff(kind Kind, old, new *Snapshot) {
	emit := func(c Change) { r.notify(c) }
	switch kind {
	case KindPrompt:
		diffMaps(kind, old.Prompts, new.Prompts, emit)
	case KindAgent:
		diffMaps(kind, old.Agents, new.Agents, emit)
	case KindSkill:
		diffMaps(kind, old.Skills, new.Skills, emit)
	case KindHook:
		diffMaps(kind, old.Hooks, new.Hooks, emit)
	case KindMCP:
		diffMaps(kind, old.MCPs, new.MCPs, emit)
	}
}

// ioFail is the failure tail of every mutation filesystem step: publish
// disk truth, then return the typed io_error. Caller holds reloadMu.
func (r *Registry) ioFail(kind Kind, err error) error {
	r.reloadKindLocked(kind)
	return mutationErr(ErrIO, "%v", err)
}
