#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

COMPLEXITY_WARN="${COMPLEXITY_WARN:-10}"
COMPLEXITY_FAIL="${COMPLEXITY_FAIL:-120}"
MAX_FILE_LINES_WARN="${MAX_FILE_LINES_WARN:-400}"
MAX_FILE_LINES_FAIL="${MAX_FILE_LINES_FAIL:-4000}"

HAS_THRESHOLD_ERRORS=0

cd_project_root

print_status "Validating Go complexity..."
print_status "Project root: $PROJECT_ROOT"
print_status ""

require_command "gocyclo" "go install github.com/fzipp/gocyclo/cmd/gocyclo@latest"

print_status "Running gocyclo (warn > $COMPLEXITY_WARN, fail > $COMPLEXITY_FAIL, test files excluded)..."
WARN_FUNCS=$(gocyclo -ignore "_test\.go" -over "$COMPLEXITY_WARN" . 2>&1) || true
FAIL_FUNCS=$(gocyclo -ignore "_test\.go" -over "$COMPLEXITY_FAIL" . 2>&1) || true

WARN_COUNT=0
FAIL_COUNT=0

if [[ -n "$FAIL_FUNCS" ]]; then
    FAIL_COUNT=$(echo "$FAIL_FUNCS" | wc -l | tr -d ' ')
    print_error "$FAIL_COUNT function(s) exceed fail threshold:"
    while read -r line; do
        echo "  $line"
    done <<<"$FAIL_FUNCS"
    HAS_THRESHOLD_ERRORS=1
fi

if [[ -n "$WARN_FUNCS" ]]; then
    WARN_COUNT=$(echo "$WARN_FUNCS" | wc -l | tr -d ' ')
    WARN_ONLY=$((WARN_COUNT - FAIL_COUNT))
    if [[ $WARN_ONLY -gt 0 ]]; then
        print_warning "$WARN_ONLY function(s) exceed warn threshold:"
        while read -r line; do
            echo "  $line"
        done <<<"$WARN_FUNCS"
    fi
else
    print_status "No high-complexity functions found"
fi

print_status "Checking file line counts (fail threshold applies to non-test files only)..."
LONG_FILES_WARN=0
LONG_FILES_FAIL=0
while IFS= read -r file; do
    [[ -z "$file" ]] && continue
    LINE_COUNT=$(wc -l < "$file" | tr -d ' ')
    IS_TEST=0
    [[ "$file" == *_test.go ]] && IS_TEST=1
    if [[ $IS_TEST -eq 0 && $LINE_COUNT -gt $MAX_FILE_LINES_FAIL ]]; then
        print_error "File too long: $file ($LINE_COUNT lines > $MAX_FILE_LINES_FAIL)"
        LONG_FILES_FAIL=$((LONG_FILES_FAIL + 1))
    elif [[ $LINE_COUNT -gt $MAX_FILE_LINES_WARN ]]; then
        print_warning "File getting long: $file ($LINE_COUNT lines > $MAX_FILE_LINES_WARN)"
        LONG_FILES_WARN=$((LONG_FILES_WARN + 1))
    fi
done < <(find . -name '*.go' -not -path './.git/*')

if [[ $LONG_FILES_FAIL -gt 0 ]]; then
    HAS_THRESHOLD_ERRORS=1
elif [[ $LONG_FILES_WARN -eq 0 ]]; then
    print_status "All files within line count limits"
fi

print_status "Counting TODO/FIXME/HACK markers..."
TODO_MATCHES=$(grep -rn --include='*.go' -E '\b(TODO|FIXME|HACK)\b' . || true)
if [[ -n "$TODO_MATCHES" ]]; then
    TODO_COUNT=$(echo "$TODO_MATCHES" | wc -l | tr -d ' ')
    print_warning "Found $TODO_COUNT TODO/FIXME/HACK marker(s)"
    while read -r line; do
        echo "  $line"
    done <<<"$TODO_MATCHES"
else
    print_status "No TODO/FIXME/HACK markers found"
fi

echo ""
if [[ $HAS_THRESHOLD_ERRORS -ne 0 ]]; then
    print_error "Complexity thresholds exceeded"
    exit 2
else
    print_status "Complexity validation passed"
fi
