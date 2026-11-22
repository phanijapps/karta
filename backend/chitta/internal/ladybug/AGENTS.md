# chitta/internal/ladybug

The ONLY code that imports go-ladybug: the production storage driver
for chitta, persisting both domains (memory records + knowledge graph)
in one LadybugDB directory. See the package doc in db.go for the
deliberate v1 notes (keyword/vector lane implementation, engine
extensions, transaction discipline).

- Boundaries: imported only by the chitta service layer behind the
  driver seam; imports domain + textutil + the vendored go-ladybuf —
  never daemon/server code.
- Known debt (ledger): context is not threaded through store methods
  (F-17 — follow-up re-shapes this package, incl. its two grandfathered
  oversized files db.go / memory_store.go).
- `task test:chitta:ladybug` runs the suite against this real driver.
