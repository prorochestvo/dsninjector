# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

Pure-Go single package `dsninjector` at the module root - no binaries, no CGO. `CGO_ENABLED=0` is mandatory on every build/test/vet invocation. The package is at the root, so the test path is `.` (e.g. `CGO_ENABLED=0 go test -race -run 'TestParse/sub' .`). Otherwise standard Go tooling: `go build ./...`, `go test -race`, `go vet ./...`, `gofmt -l .`, `go test -bench=. -benchmem -run=^$ .`, coverage via `-coverprofile`.

## Architecture Overview

`dsninjector` is a small, dependency-free library for parsing/emitting DSNs (`driver://login:password@host:port/database?opt=val`). `Parse` turns a DSN into a `DataSourceMapper` (a `map[string]string` with typed accessors + arbitrary query options); `Marshal` rebuilds the string. `Unmarshal`/`UnmarshalOrEmpty` read a DSN from the environment; `InitEnvFrom` loads `.env`-style files. No service, no long-running process, no I/O beyond optional env-file reading.

### File Responsibilities

- `parser.go` - free functions: `Parse`, `Marshal`, `Unmarshal`/`UnmarshalOrEmpty`, `InitEnvFrom` (+ unexported `readEnvVarName` `.env` scanner).
- `source.go` - the data model: `DataSource` interface + its `DataSourceMapper` (`map[string]string`) impl, typed getters/setters, `Addr`, `AuthBasicBase64`, the `Option`/`OptionsNames` bag, and the reserved `key*Name` field-key constants.

### Key Patterns

- **Map-backed value object** - `DataSourceMapper` is a `map[string]string`. Six well-known fields live under reserved `key*Name` constants (`driver`, `hostname`, `port`, `login`, `password`, `database`); every other key is a free-form option. `OptionsNames` = exactly the non-reserved keys, which is how `Marshal` rebuilds the `?a=b` query tail.
- **Regex-driven parsing** - `Parse` splits the DSN with one named-group regex, then post-processes `instance`->host/port and `credentials`->login/password. Non-obvious: a credentials string with no `:` is treated as password-only.
- **Coerce-on-read getters** - no caching; e.g. `Port` does `strconv.ParseInt` and swallows the error, returning `0` on a bad value; `Addr`/`AuthBasicBase64` compose stored fields on demand.
- **Env/file layer** - `InitEnvFrom` reads `KEY=VALUE` lines (default `.env`), upper-cases keys, `os.Setenv`; missing files skipped silently.

### Key Dependencies

Standard library only at runtime; `testify` is the sole direct require and stays test-only (see Constraints). Go `1.23.3` per `go.mod`.

## File Declaration Order

Public surface at the top, private internals at the bottom - a reader sees everything important first. Per file: (1) exported `const`/`var` + `New<Object>` constructor(s), (2) the struct, (3) its methods (alphabetical preferred), (4) unexported `const`/`var`, (5) unexported auxiliary structs, (6) unexported funcs/methods. Files with no object (free funcs + a data type) follow the same spirit. Multiple structs: primary struct first, same layout - but two large objects in one file usually means split it. Treat a violating file as something to fix.

## Error Handling

No custom error type - exported funcs return plain `error`. Contract to preserve: wrap propagated failures with `%w` (so callers can `errors.Is`/`errors.As`; `readEnvVarName` also `errors.Join`s the deferred `Close` error), return plain sentinel-free errors for bad input (no regex match, empty/whitespace config), and reserve error-swallowing for the explicitly best-effort `*OrEmpty` helpers (`UnmarshalOrEmpty` returns an empty `DataSourceMapper`, never errors).

## Constraints

- **Zero runtime dependencies**: this is a standard-library-only library. Keep it that
  way — `go.mod`'s only non-indirect `require` is `testify`, and it must stay
  **test-only**. Do not introduce a third-party runtime import without an explicit
  decision; nothing CGO-dependent may enter the build.
- **Testing**: Use `github.com/stretchr/testify`; run tests with `-race`; parallel
  subtests preferred where there's no shared mutable state.
- **One `Test*` per method, scenarios as subtests**: each tested method/function gets
  exactly one top-level test function named after it (e.g. `TestEncode` for `Encode`),
  and every scenario for that method lives as a `t.Run("descriptive name", ...)`
  subtest inside it. Do **not** create separate top-level tests like
  `TestEncode_EmptyInput`, `TestEncode_Unicode`, `TestEncode_Error` — these belong
  as subtests of a single `TestEncode`. Methods on a type follow the same rule with
  the standard `TestType_Method` form (e.g. `TestUser_Validate`).
- **No CGO**: `CGO_ENABLED=0` must be set for all build and test commands (unless the
  project intentionally requires CGO).
- **Compile-time interface checks**: Any type that implements an interface (notably
  `DataSourceMapper` against `DataSource`), and every mock/stub struct in test files,
  must have a `var _ DataSource = &DataSourceMapper{}` style assertion so the
  implementation is enforced at compile time.
- **No section-divider comments**: Do not use `// --- section ---` or `// ----` style
  separator comments. Let the code structure speak for itself.
- **No skipped errors**: Never use `_` to discard error return values in production or
  test code. Always capture the error and assert/check it. The only exceptions are
  `fmt.Fprint*` writes to loggers, `Rollback()` calls in error-recovery paths, and
  resource `.Close()` in `t.Cleanup` / `defer`.
- **Comments**: all comments are in English and start with a lowercase first word
  (e.g. `// wrap the driver error so callers can match on it`).
- **Godoc on exported identifiers**: every exported Type/Func/Method/Var/Const gets a doc comment starting with its name, ending with a period; skip it if it would only restate the signature. Exactly one `// Package <name> ...` per package; `cmd/*` uses `// Command <name> ...`. Document the non-obvious: concurrency guarantees, the `%w`-wrap-vs-swallow error contract, and coercions like `Port` returning `0` on a bad value. Preserve existing WHY-comments verbatim; don't bulk-comment private helpers.
- **Scratch artifacts stay out of the repo root**: this library ships no binaries, so
  `go build` produces nothing to place. Any throwaway artifacts, fixtures, coverage
  profiles, or intermediate files belong in `./tmp/` (not the repo root, where
  `git add .` would pick them up). Keep `cover.out` and similar out of commits.

## Planning Workflow

Non-trivial work gets a Markdown plan file before code. Plans live in `plans/`, with `plans/completed/` and `plans/history/` subdirs.

Naming:
- Active (`plans/`): `NNN-kebab-slug.md`, zero-padded sequential; next number = highest existing prefix across `plans/`, `completed/`, `history/`.
- Completed (`plans/completed/`): `YYMMDD.NNNN.slug.md`; `NNNN` is a per-day index resetting to `0001` daily.
- Archived (`plans/history/`): keeps the original `NNN-` name.

Lifecycle: create in `plans/` -> implement (file stays put) -> on all criteria met + tests green, `mv` to `completed/` with the date name -> if abandoned/superseded, move to `history/`.

Rules: one plan per concern; plan (or confirm an existing one) before touching source; keep the plan honest (update it if implementation diverges, before completing); slug readable at a glance.

Plan format sections: Overview, Assumptions, Tasks (each: Description / Acceptance Criteria / Pitfalls & edge cases / Complexity Easy|Medium|Hard), Execution Order, Risks, Trade-offs.

## Agent Pipeline

Non-trivial tasks run three stages with the `gocode-*` agents (defined in `.claude/agents/`):

1. `gocode-architect` -> writes the plan file in `plans/` (see Planning Workflow).
2. `gocode-engineer` -> implements the plan's tasks + tests.
3. `gocode-reviewer` x3 in parallel (one message, three tool calls), each a distinct lens:
   - A correctness & tests (races, edge/error paths, context, cleanup, test structure/coverage)
   - B security & ops (input validation, auth, secrets, injection, observability/log volume)
   - C performance & architecture (allocs, blocking I/O, leaks, layering, API/breaking changes)
   Each lens prompt names its focus AND what to SKIP; deliverable is P0-P3 findings with `file:line` + patch sketch.

The orchestrator (main session) synthesises the three reports, resolves conflicts (naming the rejected call), and gates completion: the plan moves to `plans/completed/` only once every P0/P1 is fixed or explicitly accepted. The first review is always the full three-lens fan-out; after a fix, re-review is a SINGLE pass scoped to the changed lines. `make test` must be green before review; on failure hand logs to `gocode-testdoctor` (scoped patches only, no redesign) first.
