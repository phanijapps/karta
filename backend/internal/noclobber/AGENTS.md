# internal/noclobber

A move that never replaces an existing destination — the atomic
primitive for one-time relocations and seed installs. Linux:
renameat2(RENAME_NOREPLACE), one syscall for check+move. Other
platforms: Go's guarded rename (occupied targets fail with IsExist;
the residual microsecond window on empty-dir targets is documented).

## Responsibility

- `Move(old, new)` — rename refusing an occupied destination
  (`os.ErrExist`); files and directories alike.
- `IsExist(err)` — the occupied-destination predicate.

## Boundaries

- Lowest tier beside domain: stdlib + x/sys only; imported by config
  (legacy-tree migrations) and registry (managed installs) — the
  shared home of the no-replace primitive both previously carried
  separately.

## Key entrypoints

`Move`, `IsExist`.
