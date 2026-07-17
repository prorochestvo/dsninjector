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

## Conventions

Generic Go conventions (style, file declaration order, test structure, test-only
code placement, godoc, error discipline, code organization) come from the
`stack-go` plugin skills — they are not restated here. Project-specific constraints:

- **Zero runtime dependencies**: this is a standard-library-only library. `go.mod`'s
  only non-indirect `require` is `testify`, and it must stay **test-only**. Do not
  introduce a third-party runtime import without an explicit decision; nothing
  CGO-dependent may enter the build.
- **Compile-time interface checks** apply to production types too — notably
  `var _ DataSource = &DataSourceMapper{}`, not just test mocks.
- **No Makefile**: gates run as raw Go commands from the module root
  (`gofmt -l .`, `CGO_ENABLED=0 go vet ./...`, `CGO_ENABLED=0 go test -race .`).
  Keep `cover.out` and other scratch under `./tmp/`, never the repo root.

## Working agreement

All non-trivial work follows the plan-first pipeline:

1. **Plan** — the `architect` agent writes `plans/NNN-slug.md` (create via the
   `pipeline:new-plan` skill). No source edits before a plan exists.
2. **Implement** — the `engineer` agent executes the plan's tasks with tests.
3. **Review** — three `reviewer` agents launched in parallel in ONE message, each
   prompt naming its lens (A: correctness & tests, B: security & operations,
   C: performance & architecture) and the changed files. Full three-lens fan-out is
   mandatory on the first review; the post-fix re-review is ONE solo reviewer scoped
   to the changed lines.
4. **Gate** — `gofmt -l .` clean, `go vet` and `go test -race .` green (all with
   `CGO_ENABLED=0`) before review; a red tree goes to the `testdoctor` agent first.
5. **Complete** — the orchestrator merges the three reports, deduplicates, resolves
   conflicting verdicts (naming what was rejected and why; the user has final say).
   P0/P1 findings loop back to the engineer. Only when every P0/P1 is fixed or
   explicitly accepted: move the plan via the `pipeline:complete-plan` skill.

Plans live in `plans/` (active), `plans/completed/` (shipped, `YYMMDD.NNNN.slug.md`),
`plans/history/` (abandoned/superseded). One plan per concern.
