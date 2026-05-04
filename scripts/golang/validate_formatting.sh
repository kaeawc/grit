#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

HAS_ERRORS=0

cd_project_root

print_status "Validating Go formatting..."
print_status "Project root: $PROJECT_ROOT"
print_status ""

print_status "Running gofmt..."
UNFORMATTED=$(gofmt -l . 2>&1) || true
if [[ -n "$UNFORMATTED" ]]; then
    print_error "The following files are not properly formatted:"
    while read -r file; do
        echo "  - $file"
    done <<<"$UNFORMATTED"
    HAS_ERRORS=1
else
    print_status "All files are properly formatted (gofmt)"
fi

if command -v goimports >/dev/null 2>&1; then
    print_status "Running goimports..."
    UNIMPORTED=$(goimports -l . 2>&1) || true
    if [[ -n "$UNIMPORTED" ]]; then
        print_warning "The following files have import issues:"
        while read -r file; do
            echo "  - $file"
        done <<<"$UNIMPORTED"
        print_warning "Run: goimports -w ."
    else
        print_status "All imports are properly organized (goimports)"
    fi
else
    print_warning "goimports not installed; skipping import checks"
fi

echo ""
if [[ $HAS_ERRORS -ne 0 ]]; then
    print_error "Formatting validation failed"
    exit 1
else
    print_status "Formatting validation passed"
fi
