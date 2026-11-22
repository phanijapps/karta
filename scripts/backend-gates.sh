#!/usr/bin/env bash
# backend-gates.sh — mechanical enforcement for docs/development/go-rules.md.
# Wired into `task verify` (rule [gate] entries). Exit 1 on any violation.
#
# Honest-scope note: these gates are the MECHANICAL floor. Where a rule's
# full claim exceeds what grep can prove (I/O signatures take ctx, test
# files exercise entrypoints, AGENTS.md content quality, channel
# ownership outside engine streams), the residual check is the
# zbot-reviewer's — the rules doc says so per rule.
set -euo pipefail
cd "$(dirname "$0")/../backend"

# Optional mode: --budget runs the suite and enforces rule 4.3's
# measured warm-run budget (machine-qualified; constants below).
if [[ "${1:-}" == "--budget" ]]; then
  TOTAL_BUDGET=25   # seconds, warm run, this dev box (go-rules.md 4.3). Raised from 20 with the add-chat-streaming change: the streaming surface added ~2s of tests; observed run variance 19.8-21.2s made the 20s line flaky. Still gates regressions hard.
  PER_PKG=5         # seconds; over this a package is flagged
  start=$(date +%s)
  out=$(CGO_ENABLED=1 go test -count=1 ./... 2>&1)
  end=$(date +%s)
  if echo "$out" | grep -qE '^(FAIL|---)'; then
    echo "budget gate: tests failed" >&2; exit 1
  fi
  total=$(echo "$out" | awk '/^ok/ {for(i=1;i<=NF;i++) if ($i ~ /s$/) {gsub(/s/,"",$i); t+=$i}} END {printf "%.1f", t}')
  echo "suite total (per-pkg sum): ${total}s | wall $((end-start))s | $(go version | cut -d' ' -f3) @ $(hostname)"
  echo "$out" | awk -v per=$PER_PKG '/^ok/ {for(i=1;i<=NF;i++) if ($i ~ /s$/) {gsub(/s/,"",$i); if ($i+0 > per) print "GATE NOTE: slow package (>" per "s): " $2, $i "s" > "/dev/stderr"}}'
  if awk -v t="$total" -v b=$TOTAL_BUDGET 'BEGIN {exit !(t+0 > b)}'; then
    echo "GATE FAIL: rule 4.3: suite ${total}s exceeds ${TOTAL_BUDGET}s warm budget (this machine)" >&2; exit 1
  fi
  echo "budget gates: OK"; exit 0
fi

fail=0
note() { echo "GATE FAIL: $*" >&2; fail=1; }

# --- Rule 1.1: 500-LOC budget ------------------------------------------------
# Grandfather list = recorded cohesion reviews (go-rules.md 1.1). Scope
# excludes generated code, vendored code, tests, declared test doubles,
# and the tools/ one-shot mains (documented in the rule).
LOC_BUDGET=500
GRANDFATHER=(
  "chitta/internal/ladybug/db.go"           # 673 — F-17 ctx-threading will reshape it
  "chitta/internal/ladybug/memory_store.go" # 666 — same
  "internal/domain/memory.go"               # 546 — purity-guarded contracts+helpers
)
is_grandfathered() {
  local want="${1#./}" g
  for g in "${GRANDFATHER[@]}"; do [[ "$g" == "$want" ]] && return 0; done
  return 1
}
while IFS= read -r f; do
  loc=$(wc -l < "$f")
  if (( loc > LOC_BUDGET )) && ! is_grandfathered "$f"; then
    note "rule 1.1: $f is $loc LOC (budget $LOC_BUDGET) — split it or grandfather it with a cohesion verdict"
  fi
done < <(find . -name '*.go' ! -name '*_test.go' \
  ! -path './internal/server/apigen/*' \
  ! -path './third_party/*' \
  ! -path './chitta/fakedriver.go' \
  ! -path './tools/*' -type f)

# --- Rule 1.2: banned generic file names (name QUALITY is reviewer-owned) ----
while IFS= read -r f; do
  note "rule 1.2: generic name '$f' — files name their content"
done < <(find . -name '*.go' ! -path './third_party/*' | grep -E '/(util|utils|common|helpers|misc)[./]' || true)

# --- Rule 2.2: context discipline --------------------------------------------
# Mechanical part: literal context.Background()/context.TODO() outside
# tests is a finding UNLESS the line carries a `ctx-ok: <ledger-id>`
# marker naming its owning follow-up, or sits in the ladybug directory
# (F-17 owns the wholesale re-threading). Signature-shape enforcement
# ("I/O funcs take ctx first") is reviewer-owned (see rule text).
CTX_ALLOWLIST='(_test\.go|chitta/internal/ladybug/|internal/engine/contracttest/|testdata/)'
while IFS=$'\t' read -r f l; do
  note "rule 2.2: context.Background/TODO at $f:$l — thread the caller ctx or mark the line 'ctx-ok: <ledger-id>'"
done < <(grep -rn -E 'context\.(Background|TODO)\(' --include='*.go' internal/ chitta/ distiller/ 2>/dev/null \
  | grep -Ev "$CTX_ALLOWLIST" \
  | grep -v 'ctx-ok:' \
  | awk -F: '{print $1"\t"$2}' || true)

# --- Rule 4.1: test floor (file existence; entrypoint exercise is reviewer+suite) --
# Rule scope: internal/ + chitta/ + distiller/. Out of scope: cmd mains,
# tools/ one-shots, generated apigen, and the contracttest test-infra
# package (its suite runs in consumers' tests).
while IFS= read -r d; do
  note "rule 4.1: package $d has no _test.go"
done < <(for d in $(go list ./... | grep -v '/cmd/'); do
  case "$d" in
    *tools/*|*third_party/*|*server/apigen|*engine/contracttest) continue;;
  esac
  ls "${d#zbot/backend/}"/*_test.go >/dev/null 2>&1 || echo "$d"
done)

# --- Rule 4.2: no live network in unit tests ---------------------------------
# Mechanical part: literal remote URLs in the convenience helpers.
# Variable-URL calls, http.Client.Do, and other clients are reviewer-
# owned (see rule text).
while IFS=$'\t' read -r f l; do
  note "rule 4.2: literal-URL HTTP call at $f:$l — use fakes/httptest"
done < <(grep -rn -E 'http\.(Get|Post|Head)\(\s*"?https?://' --include='*_test.go' . 2>/dev/null \
  | grep -v 'httptest\|127\.0\.0\.1\|localhost' | awk -F: '{print $1"\t"$2}' || true)

# --- Rule 5.1: constitution boundaries ---------------------------------------
while IFS= read -r f; do
  note "rule 5.1: eino imported outside engine/eino: $f"
done < <(grep -rln 'cloudwego/eino' --include='*.go' . | grep -v 'internal/engine/eino/' || true)
# Client packages: the backend module (zbot/backend) must not be imported
# into internal/ from cmd, and no frontend/desktop module path may appear
# at all (they are separate modules; any such import is a boundary break).
while IFS= read -r f; do
  note "rule 5.1: internal/ imports a client package: $f"
done < <(grep -rln -E '"([^"]*/)?(frontend|desktop)(/[^"]*)?"|zbot/backend/cmd/' --include='*.go' internal/ || true)

# --- Rule 5.2: every package carries an AGENTS.md (content is reviewer-owned) -
while IFS= read -r d; do
  note "rule 5.2: package dir ${d#zbot/backend/} lacks AGENTS.md"
done < <(for d in $(go list ./...); do
  dir="${d#zbot/backend/}"
  [[ "$dir" == "tools/"* || "$dir" == "third_party/"* ]] && continue
  [[ -f "$dir/AGENTS.md" ]] || echo "$d"
done)

# --- Rule 3.1: -race stays on THE backend test task (not just anywhere) ------
awk '/^  test:/{f=1;next} f && /^  [a-z][a-z:-]*:/{f=0} f' ../Taskfile.yml | grep -q -- '-race' \
  || note "rule 3.1: -race missing from the backend test task in Taskfile.yml"

# --- Rule 5.3: contract drift (HARD gate) ------------------------------------
# (a) contracts/openapi.yaml must be clean in `git diff HEAD` unless the
#     run declares CONTRACT_CHANGE=1 (a change that declares contract
#     modification). NOTE: this workspace's baseline tree carries prior,
#     CONSISTENT contract+codegen work uncommitted — verify runs during
#     change backend-code-quality-refactor set CONTRACT_CHANGE=1 with the
#     pre-existing diff recorded in that change's findings.md.
if [[ "${CONTRACT_CHANGE:-0}" != "1" ]]; then
  if git diff HEAD --stat -- ../contracts/openapi.yaml 2>/dev/null | grep -q '^ contracts'; then
    note "rule 5.3: contracts/openapi.yaml changed without CONTRACT_CHANGE=1 — contract-first flow: declare, codegen, then implement"
  fi
fi
# (b) generated output must MATCH the contract: regenerate and compare —
#     any delta means the working generated code is stale.
gen_hash() { md5sum internal/server/apigen/gen.go ../frontend/src/shared/api/client.ts ../frontend/src/shared/api/schema.d.ts 2>/dev/null | md5sum | cut -d' ' -f1; }
before=$(gen_hash)
if ! (cd .. && task codegen >/dev/null 2>&1); then
  note "rule 5.3: task codegen itself failed — contract or toolchain broken"
fi
after=$(gen_hash)
if [[ "$before" != "$after" ]]; then
  note "rule 5.3: codegen drift — regenerating changed generated output; run task codegen and commit the result"
fi

if (( fail )); then
  echo "backend gates: FAILED" >&2
  exit 1
fi
echo "backend gates: OK"
