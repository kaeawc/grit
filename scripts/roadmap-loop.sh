#!/usr/bin/env bash
# roadmap-loop.sh — iterate over roadmap/clusters/**/*.md, ask claude to land
# one reasonable chunk of work per item, verify build + tests, and (best
# effort) commit.
#
# Adapted from kaeawc/kaze/scripts/roadmap-loop.sh for the grit Android/JVM
# build runner.
#
# Usage:
#   scripts/roadmap-loop.sh [--max N] [--until MINUTES] [--cluster NAME]
#                           [--dry-run]
#
# Lifecycle:
#   Each concept file has a **Status:** line with one of three values:
#     planned      → never been worked on
#     in_progress  → has been worked on, not yet complete
#     shipped      → substantially implemented per its Shape section
#
#   The loop picks in_progress items first (finish what's started), then
#   planned items (start new work). After each successful iteration,
#   claude's output is checked for a CONCEPT_STATUS line to determine
#   whether the concept is done or needs more work.
#
# Iteration priority:
#   1. in_progress items from any cluster (finish partially-done work)
#   2. Cadence-driven large task for planned items
#   3. Normal next planned concept, round-robin across clusters
#
# The loop never pushes. If signing isn't available (1Password locked)
# it stops before doing work that can't be committed.
#
# Exit codes:
#   0   loop completed (either all items consumed or --max / --until hit)
#   2   preflight failure (ssh agent, dirty tree, missing tool, etc.)
#   130 interrupted by user (SIGINT between iterations)
#
# The loop never uses `git reset --hard`. Failed iterations leave their
# changes in the working tree — the next iteration proceeds on top of
# whatever state exists.

set -o pipefail

# ---------- argument parsing ----------

MAX_ITER=0            # 0 = unlimited
UNTIL_MINUTES=0       # 0 = unlimited
CLUSTER_FILTER=""
DRY_RUN=0

# Cadence for preferring a "large" (architectural / infrastructure)
# concept over a regular per-feature concept when picking from the
# planned pool. Does not affect in_progress priority.
LARGE_CADENCE_MIN=5
LARGE_CADENCE_MAX=12
LARGE_CLUSTERS=(cache-and-storage scheduler project-model transforms)

# Diminishing-returns trigger: if the last DIMINISHING_WINDOW iterations
# produced fewer than DIMINISHING_THRESHOLD commits, force a large
# attempt early.
DIMINISHING_WINDOW=5
DIMINISHING_THRESHOLD=2

usage() {
    cat <<'USAGE'
Usage: scripts/roadmap-loop.sh [--max N] [--until MINUTES] [--cluster NAME] [--dry-run]

The loop never pushes. If signing isn't available (1Password locked)
it stops before doing work that can't be committed.

Options:
  --max N         Stop after N iterations (0 = unlimited)
  --until M       Stop after M minutes (0 = unlimited)
  --cluster NAME  Only pick items from this cluster subdirectory
  --dry-run       Show item picks without calling claude or committing
USAGE
    exit 2
}

while [ $# -gt 0 ]; do
    case "$1" in
        --max)      MAX_ITER="$2"; shift 2 ;;
        --until)    UNTIL_MINUTES="$2"; shift 2 ;;
        --cluster)  CLUSTER_FILTER="$2"; shift 2 ;;
        --dry-run)  DRY_RUN=1; shift ;;
        -h|--help)  usage ;;
        *)          echo "unknown argument: $1" >&2; usage ;;
    esac
done

# ---------- locate repo + directories ----------

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT" || exit 2

LOOP_DIR="$REPO_ROOT/.loop"
mkdir -p "$LOOP_DIR"
DONE_FILE="$LOOP_DIR/done.txt"
PROGRESS_FILE="$LOOP_DIR/progress.txt"
STATE_LOG="$LOOP_DIR/state.jsonl"
RUN_LOG="$LOOP_DIR/run.log"
: > "$RUN_LOG"   # overwrite each session
touch "$DONE_FILE" "$PROGRESS_FILE" "$STATE_LOG"

# Detect sed -i syntax once: macOS = sed -i '', GNU = sed -i
if [[ "$OSTYPE" == darwin* ]]; then
    sedi() { sed -i '' "$@"; }
else
    sedi() { sed -i "$@"; }
fi

log()  { printf '[%(%H:%M:%S)T] %s\n' -1 "$*" | tee -a "$RUN_LOG"; }
warn() { log "WARN: $*"; }
die()  { log "FATAL: $*"; exit 2; }

# Prune done.txt and progress.txt entries whose files no longer exist.
for _prune_file in "$DONE_FILE" "$PROGRESS_FILE"; do
    if [ -s "$_prune_file" ]; then
        _before=$(wc -l < "$_prune_file" | tr -d ' ')
        _tmp=$(mktemp)
        while IFS= read -r _path; do
            [ -f "$REPO_ROOT/$_path" ] && printf '%s\n' "$_path"
        done < "$_prune_file" > "$_tmp"
        mv "$_tmp" "$_prune_file"
        _after=$(wc -l < "$_prune_file" | tr -d ' ')
        _pruned=$((_before - _after))
        [ "$_pruned" -gt 0 ] && log "preflight: pruned $_pruned stale entries from $(basename "$_prune_file")"
    fi
done
unset _before _after _pruned _tmp _path _prune_file

# ---------- preflight ----------

require_cmd() {
    command -v "$1" >/dev/null 2>&1 || die "missing tool: $1"
}

log "preflight: checking tools"
require_cmd claude
require_cmd git
require_cmd go
require_cmd jq

log "preflight: locating 1Password SSH agent"
OP_AGENT="$HOME/Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"
if [ -S "$OP_AGENT" ]; then
    export SSH_AUTH_SOCK="$OP_AGENT"
    log "SSH_AUTH_SOCK → 1Password agent"
else
    warn "1Password agent socket not found at $OP_AGENT"
    warn "falling back to current SSH_AUTH_SOCK=$SSH_AUTH_SOCK"
fi

log "preflight: ssh-add -l (agent socket reachable?)"
if ! ssh-add -l >/dev/null 2>&1; then
    ec=$?
    case $ec in
        1) die "ssh agent has no identities — unlock 1Password" ;;
        2) die "cannot connect to ssh agent at SSH_AUTH_SOCK=$SSH_AUTH_SOCK" ;;
        *) die "ssh-add failed with exit $ec" ;;
    esac
fi
log "  ok: agent lists identities"

log "preflight: ssh -T git@github.com (real signing probe)"
ssh_probe_out=$(ssh -T -o BatchMode=yes -o ConnectTimeout=5 \
                    -o StrictHostKeyChecking=accept-new \
                    git@github.com 2>&1 || true)
ssh_probe_rc=$?
if printf '%s' "$ssh_probe_out" | grep -qi "successfully authenticated"; then
    log "  ok: authenticated against github.com (exit $ssh_probe_rc)"
elif [ "$ssh_probe_rc" -eq 255 ]; then
    warn "ssh signing probe failed — 1Password is likely locked"
    die "unlock 1Password and rerun"
else
    warn "unexpected ssh probe exit $ssh_probe_rc: $ssh_probe_out"
    die "ssh preflight failed"
fi

log "preflight: probe signing via commit-tree"
TREE=$(git write-tree 2>/dev/null) || TREE=""
if [ -z "$TREE" ]; then
    die "git write-tree failed"
fi
if git commit-tree -S -m "grit-loop signing probe" -p HEAD "$TREE" \
        >/dev/null 2>"$LOOP_DIR/signing-probe.err"; then
    log "  ok: signing probe succeeded (dangling object will be GC'd)"
else
    err1=$(head -1 "$LOOP_DIR/signing-probe.err" 2>/dev/null || echo "")
    warn "signing probe failed: $err1"
    die "signing unavailable — cannot commit, refusing to start"
fi

if [ -n "$(git status --porcelain)" ]; then
    warn "working tree has uncommitted changes — loop will proceed on top of them"
fi

START_BRANCH=$(git symbolic-ref --short HEAD 2>/dev/null || echo "")
START_SHA=$(git rev-parse HEAD)
log "preflight: starting on $START_BRANCH @ ${START_SHA:0:12}"

# ---------- concept status helpers ----------

# Read the **Status:** line from a concept file. Returns one of:
# planned, in_progress, shipped, or "unknown".
get_item_status() {
    local file="$1"
    local status
    status=$(sed -n 's/.*\*\*Status:\*\* \([a-z_]*\).*/\1/p' "$file" 2>/dev/null | head -1)
    printf '%s' "${status:-unknown}"
}

# Returns 0 if the item is eligible for picking:
# - not in done.txt (shipped)
# - not in progress.txt (being worked on right now)
# - Status is not "shipped"
is_item_pickable() {
    local item="$1"
    grep -qxF "$item" "$DONE_FILE" 2>/dev/null && return 1
    grep -qxF "$item" "$PROGRESS_FILE" 2>/dev/null && return 1
    local status
    status=$(get_item_status "$item")
    [ "$status" = "shipped" ] && return 1
    return 0
}

# ---------- item selection ----------

collect_items_for_cluster() {
    local base="roadmap/clusters/${1:?}"
    find "$base" -maxdepth 1 -type f -name '*.md' ! -name 'README.md' 2>/dev/null | sort
}

# Build an interleaved item order: round-robin across all cluster
# directories so the loop doesn't front-load any single cluster.
INTERLEAVED_FILE="$LOOP_DIR/interleaved-items.txt"
build_interleaved_order() {
    local -a dirs
    mapfile -t dirs < <(find roadmap/clusters -mindepth 1 -maxdepth 1 -type d | sort)

    local -a cluster_arrays=()
    local max=0
    for dir in "${dirs[@]}"; do
        local -a items=()
        mapfile -t items < <(find "$dir" -maxdepth 1 -type f -name '*.md' ! -name 'README.md' 2>/dev/null | sort)
        cluster_arrays+=("${#items[@]}")
        local idx=${#cluster_arrays[@]}
        eval "_cl_${idx}=(\"\${items[@]}\")"
        [ "${#items[@]}" -gt "$max" ] && max=${#items[@]}
    done

    : > "$INTERLEAVED_FILE"
    local nclusters=${#dirs[@]}
    for ((i=0; i<max; i++)); do
        for ((c=1; c<=nclusters; c++)); do
            eval "local item=\${_cl_${c}[$i]:-}"
            [ -n "$item" ] && printf '%s\n' "$item" >> "$INTERLEAVED_FILE"
        done
    done
    log "preflight: built interleaved item order ($(wc -l < "$INTERLEAVED_FILE" | tr -d ' ') items across $nclusters clusters)"
}
if [ -z "$CLUSTER_FILTER" ]; then
    build_interleaved_order
fi

collect_items() {
    local sub="${1:-}"
    if [ -n "$sub" ]; then
        collect_items_for_cluster "$sub"
    else
        cat "$INTERLEAVED_FILE"
    fi
}

# Pick the first item with the given Status value.
pick_first_with_status() {
    local sub="${1:-}" target_status="${2:?}"
    local item
    while IFS= read -r item; do
        is_item_pickable "$item" || continue
        local status
        status=$(get_item_status "$item")
        [ "$status" = "$target_status" ] && { printf '%s\n' "$item"; return 0; }
    done < <(collect_items "$sub")
    return 1
}

is_large_item() {
    local item="$1" cluster
    for cluster in "${LARGE_CLUSTERS[@]}"; do
        case "$item" in
            roadmap/clusters/"$cluster"/*) return 0 ;;
        esac
    done
    return 1
}

pick_first_large_with_status() {
    local target_status="${1:?}"
    local cluster
    for cluster in "${LARGE_CLUSTERS[@]}"; do
        if item=$(pick_first_with_status "$cluster" "$target_status"); then
            printf '%s\n' "$item"
            return 0
        fi
    done
    return 1
}

rand_between() {
    local lo=$1 hi=$2
    echo $(( (RANDOM % (hi - lo + 1)) + lo ))
}

recent_commit_count() {
    local window=$1
    if [ ! -s "$STATE_LOG" ]; then
        echo 0
        return
    fi
    tail -n "$window" "$STATE_LOG" \
        | jq -r '.status' \
        | grep -c '^committed' || true
}

ITER_SINCE_LARGE=0
NEXT_LARGE_AT=$(rand_between "$LARGE_CADENCE_MIN" "$LARGE_CADENCE_MAX")

PICK_RESULT=""
PICK_IS_LARGE=0

pick_item() {
    PICK_RESULT=""
    PICK_IS_LARGE=0

    # --- Priority 1: in_progress items (finish what's started) ---
    # These always come first regardless of cadence or cluster filter.

    if [ -n "$CLUSTER_FILTER" ]; then
        if PICK_RESULT=$(pick_first_with_status "$CLUSTER_FILTER" "in_progress"); then
            return 0
        fi
        if PICK_RESULT=$(pick_first_with_status "$CLUSTER_FILTER" "planned"); then
            return 0
        fi
        return 1
    fi

    # Try in_progress from any cluster first.
    if PICK_RESULT=$(pick_first_with_status "" "in_progress"); then
        is_large_item "$PICK_RESULT" && PICK_IS_LARGE=1
        return 0
    fi

    # --- Priority 2: planned items (start new work) ---
    # Large-cadence logic applies here.

    local force_large=0
    if [ "$ITER_SINCE_LARGE" -ge "$NEXT_LARGE_AT" ]; then
        force_large=1
        log "pick: cadence reached ($ITER_SINCE_LARGE >= $NEXT_LARGE_AT); trying large pool"
    else
        local recent_commits
        recent_commits=$(recent_commit_count "$DIMINISHING_WINDOW")
        if [ "$iter" -gt "$DIMINISHING_WINDOW" ] \
            && [ "$recent_commits" -lt "$DIMINISHING_THRESHOLD" ]; then
            force_large=1
            log "pick: diminishing returns ($recent_commits commits in last $DIMINISHING_WINDOW); trying large pool"
        fi
    fi

    if [ "$force_large" -eq 1 ]; then
        if PICK_RESULT=$(pick_first_large_with_status "planned"); then
            PICK_IS_LARGE=1
            ITER_SINCE_LARGE=0
            NEXT_LARGE_AT=$(rand_between "$LARGE_CADENCE_MIN" "$LARGE_CADENCE_MAX")
            return 0
        fi
        ITER_SINCE_LARGE=0
        NEXT_LARGE_AT=$(rand_between "$LARGE_CADENCE_MIN" "$LARGE_CADENCE_MAX")
        log "pick: no large planned item ready; falling back (next large at +$NEXT_LARGE_AT)"
    fi

    if PICK_RESULT=$(pick_first_with_status "" "planned"); then
        return 0
    fi
    return 1
}

# ---------- prompt assembly ----------

build_prompt_planned() {
    local item="$1"
    cat <<PROMPT
You are iterating on grit, an Android/JVM build runner written in Go.
The roadmap concept file below describes a component or feature that
needs implementation. This is the FIRST iteration on this concept.
Read the "Immediate next step" section and do exactly that step.

Prefer steps that are:
  1. Testable — add or extend tests alongside implementation.
  2. Incremental — one coherent slice, not a sprawling multi-concern diff.
  3. Compilable — the tree must build and test cleanly when you finish.

VERIFY AND FIX:
  After making your changes, run these commands IN ORDER and fix any
  issues they surface before finishing:
    go build ./...
    go vet ./...
    go test ./internal/... -count=1
  If a build error or test failure appears, diagnose and fix it — do not
  leave broken code. Iterate until all three commands pass cleanly.

HARD CONSTRAINTS:
  - All three verify commands MUST pass when you finish.
  - Do not modify code unrelated to this concept.
  - Do not create new roadmap/ files.
  - Do not touch .loop/ or scripts/roadmap-loop.sh.
  - Follow the project CLAUDE.md conventions if one exists.
  - Keep the diff small and reviewable.

CONCEPT FILE: $item

--- BEGIN CONCEPT ---
$(cat "$item")
--- END CONCEPT ---

COMPLETENESS CHECK:
  After listing your changes, end your response with exactly one of
  these lines (no other text on that line):
    CONCEPT_STATUS: in_progress
    CONCEPT_STATUS: shipped
  Use "in_progress" if the concept's Shape section has more work beyond
  what you just did. Use "shipped" ONLY if the Shape section is now
  substantially implemented and tested — not just the Immediate next
  step, but the full scope described in Shape.

When done, briefly list the files you created or modified. Do not commit;
the surrounding script handles git.
PROMPT
}

build_prompt_in_progress() {
    local item="$1"
    cat <<PROMPT
You are iterating on grit, an Android/JVM build runner written in Go.
The roadmap concept file below describes a component or feature that
has ALREADY had previous iterations. Some of its Shape is implemented;
some remains.

Your job: read the Shape section, look at what code already exists for
this concept (check the packages and types mentioned in Shape), and
implement the NEXT piece that isn't done yet. If the "Immediate next
step" has already been completed, pick the next cheapest high-value
step from the Shape section.

Do NOT redo work that already exists. Read the existing files first.

Prefer steps that are:
  1. Testable — add or extend tests alongside implementation.
  2. Incremental — one coherent slice, not a sprawling multi-concern diff.
  3. Compilable — the tree must build and test cleanly when you finish.

VERIFY AND FIX:
  After making your changes, run these commands IN ORDER and fix any
  issues they surface before finishing:
    go build ./...
    go vet ./...
    go test ./internal/... -count=1
  If a build error or test failure appears, diagnose and fix it — do not
  leave broken code. Iterate until all three commands pass cleanly.

HARD CONSTRAINTS:
  - All three verify commands MUST pass when you finish.
  - Do not modify code unrelated to this concept.
  - Do not create new roadmap/ files.
  - Do not touch .loop/ or scripts/roadmap-loop.sh.
  - Follow the project CLAUDE.md conventions if one exists.
  - Keep the diff small and reviewable.

CONCEPT FILE: $item

--- BEGIN CONCEPT ---
$(cat "$item")
--- END CONCEPT ---

COMPLETENESS CHECK:
  After listing your changes, end your response with exactly one of
  these lines (no other text on that line):
    CONCEPT_STATUS: in_progress
    CONCEPT_STATUS: shipped
  Use "in_progress" if the concept's Shape section has more work beyond
  what exists now (including what you just added). Use "shipped" ONLY
  if the Shape section is now substantially implemented and tested.

When done, briefly list the files you created or modified. Do not commit;
the surrounding script handles git.
PROMPT
}

build_prompt_large() {
    local item="$1"
    local status
    status=$(get_item_status "$item")
    cat <<PROMPT
You are iterating on grit, an Android/JVM build runner written in Go.
The roadmap concept file below describes an ARCHITECTURAL task,
infrastructure component, or cross-cutting feature. The roadmap loop
elevated this item deliberately because we want one bigger step.

$([ "$status" = "in_progress" ] && echo "This concept has had previous iterations. Read what exists before adding new code." || echo "This is the first iteration on this concept.")

Make one MEANINGFUL, multi-file step toward the concept. You are
allowed (and expected) to touch more than one file:

  1. If the concept depends on new infrastructure that doesn't yet
     exist, scaffold the minimum viable version in the appropriate
     internal/ package with unit tests.
  2. If the concept is a wiring/integration task, add the integration
     code and an end-to-end test.
  3. If the concept is a refactor, apply the refactor across the minimum
     set of files needed and update affected tests.

Read the "Immediate next step" section and use it as your starting
point. If it's already done, read the "Shape" section and pick the
next coherent piece.

VERIFY AND FIX:
  After making your changes, run these commands IN ORDER and fix any
  issues they surface before finishing:
    go build ./...
    go vet ./...
    go test ./internal/... -count=1
  If a build error or test failure appears, diagnose and fix it — do not
  leave broken code. Iterate until all three commands pass cleanly.

HARD CONSTRAINTS:
  - All three verify commands MUST pass when you finish.
  - Do not leave half-wired code that breaks the build. A small but
    complete slice is always better than a large half-finished one.
  - Do not create new roadmap/ files.
  - Do not touch .loop/ or scripts/roadmap-loop.sh.
  - Commit-sized diff is fine; sprawling "touch everything" diffs are not.

CONCEPT FILE: $item

--- BEGIN CONCEPT ---
$(cat "$item")
--- END CONCEPT ---

COMPLETENESS CHECK:
  After listing your changes, end your response with exactly one of
  these lines (no other text on that line):
    CONCEPT_STATUS: in_progress
    CONCEPT_STATUS: shipped
  Use "in_progress" if the concept's Shape section has more work beyond
  what exists now. Use "shipped" ONLY if the Shape section is now
  substantially implemented and tested.

When done, briefly list the files you created or modified and what
followup remains. Do not commit; the surrounding script handles git.
PROMPT
}

build_prompt() {
    local item="$1"
    if is_large_item "$item"; then
        build_prompt_large "$item"
        return
    fi
    local status
    status=$(get_item_status "$item")
    if [ "$status" = "in_progress" ]; then
        build_prompt_in_progress "$item"
    else
        build_prompt_planned "$item"
    fi
}

# ---------- completeness detection ----------

# Parse claude's output for CONCEPT_STATUS: line. Returns "in_progress"
# or "shipped". Defaults to "in_progress" if not found (conservative).
detect_concept_status() {
    local outfile="$1"
    if [ ! -f "$outfile" ]; then
        printf 'in_progress'
        return
    fi
    local status
    status=$(grep -o 'CONCEPT_STATUS: [a-z_]*' "$outfile" 2>/dev/null | tail -1 | awk '{print $2}')
    case "$status" in
        shipped)     printf 'shipped' ;;
        in_progress) printf 'in_progress' ;;
        *)           printf 'in_progress' ;;
    esac
}

# ---------- iteration body ----------

LAST_OUTPUT_FILE=""

run_iteration() {
    local item="$1"
    LAST_OUTPUT_FILE=$(mktemp -t grit-loop-XXXXXX.txt)

    log "claude exec on $item"
    if [ "$DRY_RUN" -eq 1 ]; then
        log "  dry-run: skipping claude"
        return 0
    fi

    if ! build_prompt "$item" | claude \
            --dangerously-skip-permissions \
            -p \
            > "$LAST_OUTPUT_FILE" 2>>"$RUN_LOG"; then
        warn "claude returned non-zero for $item"
        return 1
    fi

    log "claude finished; output head:"
    head -10 "$LAST_OUTPUT_FILE" 2>/dev/null | tee -a "$RUN_LOG"
    return 0
}

cleanup_output() {
    [ -n "$LAST_OUTPUT_FILE" ] && rm -f "$LAST_OUTPUT_FILE"
    LAST_OUTPUT_FILE=""
}

verify_build_and_tests() {
    log "verify: go build && go vet && go test (final gate)"
    if ! go build ./... >>"$RUN_LOG" 2>&1; then
        warn "go build failed"
        return 1
    fi
    if ! go vet ./... >>"$RUN_LOG" 2>&1; then
        warn "go vet failed"
        return 1
    fi
    if ! go test ./internal/... -count=1 >>"$RUN_LOG" 2>&1; then
        warn "go test failed"
        return 1
    fi
    return 0
}

try_commit() {
    local item="$1"
    if [ -z "$(git status --porcelain)" ]; then
        log "commit: no changes to commit"
        return 0
    fi
    log "commit: staging and committing"
    git add -A
    local msg
    msg="roadmap loop: $(basename "$(dirname "$item")")/$(basename "$item" .md)"
    if git commit -m "$msg" >>"$RUN_LOG" 2>&1; then
        log "commit: ok ($(git rev-parse --short HEAD))"
        return 0
    fi
    warn "commit failed mid-loop; 1Password likely relocked"
    warn "changes are left staged — you can commit manually after unlocking"
    return 1
}

# Transition: planned → in_progress (first successful iteration)
# or keep in_progress (subsequent iterations, not yet complete)
mark_item_in_progress() {
    local item="$1"
    if [ -f "$REPO_ROOT/$item" ]; then
        sedi 's/\*\*Status:\*\* planned/**Status:** in_progress/' "$REPO_ROOT/$item"
    fi
    # Remove from progress.txt (concurrent-run guard, not lifecycle)
    if [ -f "$PROGRESS_FILE" ]; then
        local tmp
        tmp=$(mktemp)
        grep -vxF "$item" "$PROGRESS_FILE" > "$tmp" 2>/dev/null || true
        mv "$tmp" "$PROGRESS_FILE"
    fi
}

# Transition: in_progress → shipped (concept substantially complete)
mark_item_shipped() {
    local item="$1"
    if [ -f "$REPO_ROOT/$item" ]; then
        sedi 's/\*\*Status:\*\* in_progress/**Status:** shipped/; s/\*\*Status:\*\* planned/**Status:** shipped/' \
            "$REPO_ROOT/$item"
    fi
    # Add to done.txt so the loop never picks it again.
    if ! grep -qxF "$item" "$DONE_FILE" 2>/dev/null; then
        printf '%s\n' "$item" >> "$DONE_FILE"
    fi
    # Remove from progress.txt.
    if [ -f "$PROGRESS_FILE" ]; then
        local tmp
        tmp=$(mktemp)
        grep -vxF "$item" "$PROGRESS_FILE" > "$tmp" 2>/dev/null || true
        mv "$tmp" "$PROGRESS_FILE"
    fi
}

record_state() {
    local item="$1" status="$2"
    local large=false
    is_large_item "$item" && large=true
    local concept_status
    concept_status=$(get_item_status "$item")
    jq -cn \
        --arg ts "$(date -u +%FT%TZ)" \
        --arg item "$item" \
        --arg status "$status" \
        --arg concept_status "$concept_status" \
        --arg sha "$(git rev-parse HEAD)" \
        --argjson large "$large" \
        --argjson iter "$iter" \
        '{ts:$ts,iter:$iter,item:$item,status:$status,concept_status:$concept_status,large:$large,sha:$sha}' \
        >>"$STATE_LOG"
}

# ---------- signal handling ----------

INTERRUPTED=0
trap 'INTERRUPTED=1; log "SIGINT received — will stop after this iteration"' INT

# ---------- main loop ----------

log "loop: starting"
START_TS=$(date +%s)
iter=0

# Count pickable items for the log.
_planned=$(grep -rl '\*\*Status:\*\* planned' roadmap/clusters/ 2>/dev/null | wc -l | tr -d ' ')
_in_progress=$(grep -rl '\*\*Status:\*\* in_progress' roadmap/clusters/ 2>/dev/null | wc -l | tr -d ' ')
_shipped=$(grep -rl '\*\*Status:\*\* shipped' roadmap/clusters/ 2>/dev/null | wc -l | tr -d ' ')
log "pool: ${_in_progress} in_progress, ${_planned} planned, ${_shipped} shipped"
unset _planned _in_progress _shipped

while :; do
    iter=$((iter + 1))

    if [ "$MAX_ITER" -gt 0 ] && [ "$iter" -gt "$MAX_ITER" ]; then
        log "loop: reached --max=$MAX_ITER, stopping"
        iter=$((iter - 1))
        break
    fi
    if [ "$UNTIL_MINUTES" -gt 0 ]; then
        now=$(date +%s)
        if [ $((now - START_TS)) -ge $((UNTIL_MINUTES * 60)) ]; then
            log "loop: reached --until=${UNTIL_MINUTES}m, stopping"
            break
        fi
    fi
    [ "$INTERRUPTED" -eq 1 ] && { log "loop: interrupted, stopping"; break; }

    if ! pick_item; then
        log "loop: no more items to process"
        break
    fi
    item="$PICK_RESULT"
    item_status=$(get_item_status "$item")

    if [ "$PICK_IS_LARGE" -eq 1 ]; then
        log "==== iteration $iter (LARGE, $item_status): $item ===="
    else
        log "==== iteration $iter ($item_status): $item ===="
        ITER_SINCE_LARGE=$((ITER_SINCE_LARGE + 1))
    fi

    if [ "$DRY_RUN" -eq 1 ]; then
        log "  dry-run: would work on $item ($item_status)"
        # In dry-run, transition planned → in_progress so subsequent
        # picks show realistic behavior. Don't mark shipped.
        if [ "$item_status" = "planned" ]; then
            mark_item_in_progress "$item"
        elif [ "$item_status" = "in_progress" ]; then
            mark_item_shipped "$item"
        fi
        continue
    fi

    # Mark item as in-progress (concurrent-run guard).
    if ! grep -qxF "$item" "$PROGRESS_FILE" 2>/dev/null; then
        printf '%s\n' "$item" >> "$PROGRESS_FILE"
    fi

    if run_iteration "$item"; then
        if verify_build_and_tests; then
            log "verify: ok"
            if try_commit "$item"; then
                # Check claude's completeness assessment.
                local concept_result
                concept_result=$(detect_concept_status "$LAST_OUTPUT_FILE")
                if [ "$concept_result" = "shipped" ]; then
                    log "lifecycle: $item → shipped (claude assessed complete)"
                    mark_item_shipped "$item"
                    record_state "$item" "committed-shipped"
                else
                    log "lifecycle: $item → in_progress (more work remaining)"
                    mark_item_in_progress "$item"
                    record_state "$item" "committed-in-progress"
                fi
            else
                record_state "$item" "commit-failed"
                warn "stopping loop: signing is unavailable"
                warn "uncommitted changes remain in the working tree"
                cleanup_output
                break
            fi
        else
            warn "verify failed; changes left in working tree"
            mark_item_in_progress "$item"
            record_state "$item" "verify-failed"
        fi
    else
        warn "claude step failed; changes (if any) left in working tree"
        mark_item_in_progress "$item"
        record_state "$item" "codex-failed"
    fi
    cleanup_output
done

log "loop: done after $iter iteration(s)"
[ "$INTERRUPTED" -eq 1 ] && exit 130 || exit 0
