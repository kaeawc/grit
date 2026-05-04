#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

TEST_TIMEOUT="${TEST_TIMEOUT:-5m}"
COVERAGE_WARN_PCT="${COVERAGE_WARN_PCT:-60}"
COVERAGE_FAIL_PCT="${COVERAGE_FAIL_PCT:-40}"

HAS_ERRORS=0
HAS_THRESHOLD_ERRORS=0

cd_project_root

print_status "Validating Go tests..."
print_status "Project root: $PROJECT_ROOT"
print_status ""

TEST_FILE_COUNT=$(( $( (rg --files -g '*_test.go' || true) | wc -l | tr -d ' ' ) ))

if command -v gotestsum >/dev/null 2>&1; then
    print_status "Running tests with gotestsum..."
    if ! gotestsum --junitfile junit-report.xml --format standard-verbose -- -race -coverprofile=coverage.out -timeout "$TEST_TIMEOUT" ./...; then
        print_error "Tests failed"
        HAS_ERRORS=1
    else
        print_status "All tests passed"
    fi
else
    print_warning "gotestsum not installed; falling back to go test"
    if ! go test -race -coverprofile=coverage.out -timeout "$TEST_TIMEOUT" -v ./...; then
        print_error "Tests failed"
        HAS_ERRORS=1
    else
        print_status "All tests passed"
    fi
fi

if [[ -f "coverage.out" ]]; then
    print_status "Analyzing coverage..."
    COVERAGE_LINE=$(go tool cover -func=coverage.out | tail -1)
    COVERAGE_PCT=$(echo "$COVERAGE_LINE" | grep -oE '[0-9]+\.[0-9]+' | tail -1)
    COVERAGE_INT=${COVERAGE_PCT%.*}
    print_status "Total coverage: ${COVERAGE_PCT}%"
    if [[ "$TEST_FILE_COUNT" -eq 0 ]]; then
        print_warning "No *_test.go files found; skipping coverage thresholds"
    elif [[ $COVERAGE_INT -lt $COVERAGE_FAIL_PCT ]]; then
        print_error "Coverage below fail threshold (${COVERAGE_PCT}% < ${COVERAGE_FAIL_PCT}%)"
        HAS_THRESHOLD_ERRORS=1
    elif [[ $COVERAGE_INT -lt $COVERAGE_WARN_PCT ]]; then
        print_warning "Coverage below warn threshold (${COVERAGE_PCT}% < ${COVERAGE_WARN_PCT}%)"
    fi
    go tool cover -html=coverage.out -o coverage.html 2>/dev/null || true
else
    print_warning "No coverage.out file generated"
fi

echo ""
if [[ $HAS_ERRORS -ne 0 ]]; then
    print_error "Test validation failed"
    exit 1
elif [[ $HAS_THRESHOLD_ERRORS -ne 0 ]]; then
    print_error "Coverage thresholds not met"
    exit 2
else
    print_status "Test validation passed"
fi
