# CLAUDE.md

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

## Error Handling

No custom error type - exported funcs return plain `error`. Contract to preserve: wrap propagated failures with `%w` (so callers can `errors.Is`/`errors.As`; `readEnvVarName` also `errors.Join`s the deferred `Close` error), return plain sentinel-free errors for bad input (no regex match, empty/whitespace config), and reserve error-swallowing for the explicitly best-effort `*OrEmpty` helpers (`UnmarshalOrEmpty` returns an empty `DataSourceMapper`, never errors).

## Conventions

Generic Go conventions (style, declaration order, test structure, godoc, error discipline,
build hygiene, organisation) come from the `stack-go` plugin skills and are not restated
anywhere in this repo. One deliberate override: the File Declaration Order section above
puts the constructor before its struct, where `stack-go:conventions` puts it after, and this
project's order wins here. Project-specific constraints:

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

Plan-first pipeline; the canonical procedure is the `pipeline:working-agreement` skill — load it
before starting non-trivial work. Project delta:

- **Gate:** `gofmt -l .` clean, then `CGO_ENABLED=0 go vet ./...` and
  `CGO_ENABLED=0 go test -race .` green — the package is at the module root, so the test
  path is `.`, not `./...`.
- **Lenses:** standard set — see `pipeline:working-agreement`, which includes lens O.
- **Branching:** standard (`type/<issue>-<slug>`, PR into `main`).
