#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

HAS_THRESHOLD_ERRORS=0
TOTAL_ISSUES=0

cd_project_root

print_status "Validating Go security..."
print_status "Project root: $PROJECT_ROOT"
print_status ""

if command -v gosec >/dev/null 2>&1; then
    print_status "Running gosec..."
    GOSEC_OUTPUT=$(gosec -fmt=json -quiet ./... 2>&1) || true
    if echo "$GOSEC_OUTPUT" | python3 -c "import sys,json; json.load(sys.stdin)" >/dev/null 2>&1; then
        ISSUE_COUNT=$(echo "$GOSEC_OUTPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(len(d.get('Issues', [])))")
        HIGH_COUNT=$(echo "$GOSEC_OUTPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(sum(1 for i in d.get('Issues', []) if i.get('severity') == 'HIGH'))")
        MEDIUM_COUNT=$(echo "$GOSEC_OUTPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(sum(1 for i in d.get('Issues', []) if i.get('severity') == 'MEDIUM'))")
        LOW_COUNT=$(echo "$GOSEC_OUTPUT" | python3 -c "import sys,json; d=json.load(sys.stdin); print(sum(1 for i in d.get('Issues', []) if i.get('severity') == 'LOW'))")
        if [[ "$ISSUE_COUNT" -gt 0 ]]; then
            print_error "gosec found $ISSUE_COUNT issue(s): $HIGH_COUNT high, $MEDIUM_COUNT medium, $LOW_COUNT low"
            TOTAL_ISSUES=$((TOTAL_ISSUES + ISSUE_COUNT))
            if [[ "$HIGH_COUNT" -gt 0 ]]; then
                HAS_THRESHOLD_ERRORS=1
            fi
        else
            print_status "gosec: no issues found"
        fi
        echo "$GOSEC_OUTPUT" > gosec-report.json
        print_status "gosec report: $PROJECT_ROOT/gosec-report.json"
    else
        print_status "gosec: no issues found"
    fi
else
    print_warning "gosec not installed; skipping static security analysis"
fi

if command -v govulncheck >/dev/null 2>&1; then
    print_status "Running govulncheck..."
    if ! govulncheck ./...; then
        print_error "govulncheck found vulnerabilities"
        HAS_THRESHOLD_ERRORS=1
    else
        print_status "govulncheck: no vulnerabilities found"
    fi
else
    print_warning "govulncheck not installed; skipping vulnerability check"
fi

print_status "Scanning for hardcoded secrets..."
SECRET_MATCHES=$(rg -n --glob '*.go' -e 'AKIA[0-9A-Z]{16}' -e 'sk_live_[0-9a-zA-Z]{24,}' -e 'ghp_[0-9a-zA-Z]{36}' -e 'gho_[0-9a-zA-Z]{36}' -e 'password\\s*[:=]\\s*\"[^\"]{8,}\"' -e 'secret\\s*[:=]\\s*\"[^\"]{8,}\"' || true)
if [[ -n "$SECRET_MATCHES" ]]; then
    SECRET_COUNT=$(echo "$SECRET_MATCHES" | wc -l | tr -d ' ')
    print_error "Found $SECRET_COUNT potential hardcoded secret match(es)"
    while read -r line; do
        echo "  $line"
    done <<<"$SECRET_MATCHES"
    HAS_THRESHOLD_ERRORS=1
else
    print_status "No hardcoded secrets detected"
fi

echo ""
print_status "Total issues: $TOTAL_ISSUES"
if [[ $HAS_THRESHOLD_ERRORS -ne 0 ]]; then
    print_error "Security issues found"
    exit 2
else
    print_status "Security validation passed"
fi
