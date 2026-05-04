# grit

`grit` is an Android/JVM build runner written in Go.

The long-term goal is full replacement of incumbent Android/JVM build tooling. The current MVP is intentionally narrower: it models a Kotlin DSL Android repo, validates the local toolchain, resolves dependencies from the local artifact cache, and invokes `kotlinc` directly for the supported task surface.

## Current MVP

- Inspect a Kotlin DSL Android repo
- Check for required local tooling
- Run native debug compilation for `:app`
- Run native JVM unit-test compilation and execution for `:app`

## Why start this way

The future system still needs a project model, environment validation, dependency resolution, variant planning, task execution, caching, and Android packaging. Building those layers directly into `grit` gives a usable CLI immediately and keeps the architecture pointed at replacing external build orchestration instead of baking another tool's semantics directly into the entrypoint.

## Commands

```bash
go run ./cmd/grit inspect --repo ~/path/to/android-repo
go run ./cmd/grit doctor --repo ~/path/to/android-repo
go run ./cmd/grit compile-debug --repo ~/path/to/android-repo
go run ./cmd/grit assemble-debug --repo ~/path/to/android-repo
go run ./cmd/grit test-debug-unit --repo ~/path/to/android-repo
```

## Validation

```bash
./scripts/golang/validate_build.sh
./scripts/golang/validate_formatting.sh
./scripts/golang/validate_vet.sh
./scripts/golang/validate_test.sh
./scripts/golang/validate_lint.sh
```

## Near-term roadmap

1. Expand the service layer into a fuller graph/planning boundary.
2. Grow semantic summaries into richer inspection and IDE-facing queries.
3. Add generated source and compiler plugin configuration parity.
4. Recreate Android debug APK assembly with `aapt2`, D8, and signing.
5. Expand native test execution coverage and filtering.

## Non-goals for the current MVP

- Generic Kotlin DSL interpretation
- Full APK packaging or release-build parity
- R8, lint, instrumentation tests, publishing, or broad plugin compatibility
