#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

GOLANGCI_LINT_TIMEOUT="${GOLANGCI_LINT_TIMEOUT:-5m}"

cd_project_root

print_status "Validating Go lint..."
print_status "Project root: $PROJECT_ROOT"
print_status ""

require_command "golangci-lint" "go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"

print_status "Running golangci-lint with timeout ${GOLANGCI_LINT_TIMEOUT}..."
if golangci-lint run ./... --timeout "$GOLANGCI_LINT_TIMEOUT"; then
    echo ""
    print_status "Lint validation passed"
else
    echo ""
    print_error "Lint issues found"
    exit 2
fi
