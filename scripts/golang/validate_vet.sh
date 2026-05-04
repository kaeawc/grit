#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

HAS_ERRORS=0

cd_project_root

print_status "Validating Go vet and modules..."
print_status "Project root: $PROJECT_ROOT"
print_status ""

print_status "Running go vet..."
if ! go vet ./...; then
    print_error "go vet found issues"
    HAS_ERRORS=1
else
    print_status "go vet: no issues found"
fi

print_status "Verifying module integrity..."
if ! go mod verify; then
    print_error "go mod verify failed"
    HAS_ERRORS=1
else
    print_status "go mod verify: all modules verified"
fi

print_status "Checking if go.mod/go.sum are tidy..."
TIDY_DIFF=$(go mod tidy -diff 2>/dev/null) || true
if [[ -n "$TIDY_DIFF" ]]; then
    print_error "go.mod/go.sum are not tidy. Run: go mod tidy"
    echo "$TIDY_DIFF"
    HAS_ERRORS=1
else
    print_status "go.mod/go.sum are tidy"
fi

echo ""
if [[ $HAS_ERRORS -ne 0 ]]; then
    print_error "Vet and module validation failed"
    exit 1
else
    print_status "Vet and module validation passed"
fi
