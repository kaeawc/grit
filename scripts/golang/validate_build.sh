#!/usr/bin/env bash

set -euo pipefail

source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/common.sh"

BINARY_PATH="${BINARY_PATH:-bin/grit}"
BINARY_SIZE_WARN_MB="${BINARY_SIZE_WARN_MB:-50}"
BINARY_SIZE_FAIL_MB="${BINARY_SIZE_FAIL_MB:-100}"

HAS_ERRORS=0
HAS_THRESHOLD_ERRORS=0

cd_project_root

print_status "Validating Go build..."
print_status "Project root: $PROJECT_ROOT"
print_status ""

print_status "Checking package compilation..."
if ! go build ./...; then
    print_error "Compilation failed"
    exit 1
fi
print_status "Compilation succeeded"

print_status "Building grit binary..."
mkdir -p "$(dirname "$BINARY_PATH")"
if ! go build -o "$BINARY_PATH" ./cmd/grit; then
    print_error "Failed to build grit binary"
    HAS_ERRORS=1
else
    print_status "Binary built: $BINARY_PATH"
fi

if [[ ! -f "$BINARY_PATH" ]]; then
    print_error "Binary not found at $BINARY_PATH"
    HAS_ERRORS=1
elif [[ ! -s "$BINARY_PATH" ]]; then
    print_error "Binary is empty: $BINARY_PATH"
    HAS_ERRORS=1
else
    BINARY_SIZE_BYTES=$(wc -c < "$BINARY_PATH" | tr -d ' ')
    BINARY_SIZE_MB=$((BINARY_SIZE_BYTES / 1024 / 1024))
    print_status "Binary size: ${BINARY_SIZE_MB}MB (${BINARY_SIZE_BYTES} bytes)"
    if [[ $BINARY_SIZE_MB -gt $BINARY_SIZE_FAIL_MB ]]; then
        print_error "Binary exceeds fail threshold (${BINARY_SIZE_MB}MB > ${BINARY_SIZE_FAIL_MB}MB)"
        HAS_THRESHOLD_ERRORS=1
    elif [[ $BINARY_SIZE_MB -gt $BINARY_SIZE_WARN_MB ]]; then
        print_warning "Binary exceeds warn threshold (${BINARY_SIZE_MB}MB > ${BINARY_SIZE_WARN_MB}MB)"
    fi
fi

echo ""
if [[ $HAS_ERRORS -ne 0 ]]; then
    print_error "Build validation failed"
    exit 1
elif [[ $HAS_THRESHOLD_ERRORS -ne 0 ]]; then
    print_error "Binary size thresholds exceeded"
    exit 2
else
    print_status "Build validation passed"
fi
