#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

COPYLEFT_PATTERNS="GPL|AGPL|LGPL|MPL|CDDL|EPL"
HAS_THRESHOLD_ERRORS=0

cd_project_root

print_status "Validating Go dependency licenses..."
print_status "Project root: $PROJECT_ROOT"
print_status ""

require_command "go-licenses" "go install github.com/google/go-licenses@latest"

LICENSE_OUTPUT=$(go-licenses report ./... 2>&1) || true
if [[ -n "$LICENSE_OUTPUT" ]]; then
    COPYLEFT_FOUND=$(echo "$LICENSE_OUTPUT" | grep -iE "$COPYLEFT_PATTERNS" || true)
    if [[ -n "$COPYLEFT_FOUND" ]]; then
        print_error "Copyleft or restricted licenses found:"
        while read -r line; do
            echo "  $line"
        done <<<"$COPYLEFT_FOUND"
        HAS_THRESHOLD_ERRORS=1
    else
        print_status "No copyleft licenses found"
    fi
    DEP_COUNT=$(echo "$LICENSE_OUTPUT" | wc -l | tr -d ' ')
    print_status "Total dependencies with license info: $DEP_COUNT"
else
    print_warning "No dependency license data returned"
fi

echo ""
if [[ $HAS_THRESHOLD_ERRORS -ne 0 ]]; then
    print_error "Restricted licenses found"
    exit 2
else
    print_status "License validation passed"
fi
