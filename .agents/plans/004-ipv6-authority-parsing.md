# Task Breakdown

> **How to read this plan.** It is written to be **executable once approved**, but
> it deliberately does **not** pre-decide the genuine design choices — those are
> collected in **Open Questions** at the bottom and must be settled in the
> discussion session *before* implementation starts. The task bodies are written
> against a **recommended baseline (unbracketed host storage, see OQ-1)** so the
> plan is concrete and runnable; wherever a decision would flip the code, the task
> cross-references the relevant `OQ-N` and states the variant. Approve the Open
> Questions first, adjust the two or three flagged lines if the session overrides a
> lean, then execute Tasks 1-4 in order.

## Overview

`Parse` mangles every IPv6 authority. The authority splitter at
`parser.go:236-243` uses `strings.SplitN(instance, ":", 2)`, which splits on the
**first** colon — and an IPv6 literal is nothing but colons. Behavior re-verified
against the current package (HEAD `749f521`, throwaway probe, since removed):

| Input | `Host()` | `Port()` (`KeyPort`) | `Database()` | `Addr()` | `Marshal` |
|---|---|---|---|---|---|
| `redis://[::1]:6379` | `[` | `:1]:6379` | `` | `[::1]:6379` | `redis://[::1]:6379` |
| `redis://[::1]:6379/0` | `[` | `:1]:6379` | `0` | `[::1]:6379` | `redis://[::1]:6379/0` |
| `redis://[::1]` | `[` | `:1]` | `` | `[::1]` | `redis://[::1]` |
| `postgres://u:p@[2001:db8::1]:5432/db?sslmode=disable` | `[2001` | `db8::1]:5432` | `db` | `[2001:db8::1]:5432` | (round-trips as string) |
| `http://[fe80::1%25eth0]:8080` | `[fe80` | `:1%25eth0]:8080` | `` | `[fe80::1%25eth0]:8080` | round-trips |
| `[::1]:6379` (scheme-less) | `` | `` | `/[::1]:6379` | `` | `////[::1]:6379` |
| `[::1]` (scheme-less) | `` | `` | `/[::1]` | `` | `////[::1]` |
| `[2001:db8::1]:5432/db` (scheme-less) | `` | `` | `/[2001:db8::1]:5432/db` | `` | `////[2001:db8::1]:5432/db` |
| `redis://127.0.0.1:6379` (IPv4 baseline) | `127.0.0.1` | `6379` | `` | `127.0.0.1:6379` | `redis://127.0.0.1:6379` |
| `New(WithHost("::1"), WithPort(6379))` | `::1` | `6379` | — | `::1:6379` | `//::1:6379` |
| `New(WithHost("[::1]"), WithPort(6379))` | `[::1]` | `6379` | — | `[::1]:6379` | `//[::1]:6379` |

**Two distinct failure classes:**

1. **Authority form** (`redis://[::1]:6379`): `Host()`/`Port()` are garbage, but
   `Addr()` and `Marshal` are **correct by accident**. The wrong split
   (`host="["`, `port=":1]:6379"`) is exactly *reversed* by `Addr()`'s rejoin at
   `source.go:99-108` (`h + ":" + p` → `"[" + ":" + ":1]:6379"` → `"[::1]:6379"`).
   This is the **same mechanism as the Telegram token trick**: everything after the
   first colon lands in `KeyPort`, and `Addr()` glues `host + ":" + port` back into
   the original instance string. The broken **observable** surface is
   `Host()`/`Port()`; `Addr()`/`Marshal` happen to survive.

2. **Scheme-less form** (`[::1]:6379`): no accidental save. The classifier at
   `parser.go:115-124` sees the first colon (inside the brackets), fails the
   colon-then-digit test, routes to the driver-opaque / bare-path branch, and the
   whole literal is force-prefixed into `Database()` as `/[::1]:6379`. Totally
   broken.

**Goal.** Make `Host()`, `Port()`, `Addr()`, and `Marshal`/`Redacted()` correct
for IPv6 in both forms, with a **focused, zero-dependency, regex-preserving**
change. The accidental `Addr()` correctness for authority IPv6 becomes
**intentional** correctness; the broken `Host()`/`Port()` become right.

**Explicitly decoupled from the `net/url` engine question.** Adopting
`net/url` (`plans/003` Topic 1) would fix IPv6 for free but is a much larger,
behavior-shifting decision that also touches the last-`@` credentials split,
percent-encoding, and the loose scheme-less inputs this library exists to accept.
This plan is the **surgical** alternative: three small edits and a test table.
"Delegate to `net/url`" is presented only as an alternative in Trade-offs and
**OQ-5**, not as the baseline. One plan per concern.

**The change is remarkably contained** (under the unbracketed baseline):

- `parser.go`: a new unexported `splitHostPort` (bracket-aware), called from the
  `instance` case of `parseAuthority`; one new classifier case for a leading `[`.
- `source.go`: `Addr()` brackets the host on emit when it contains a colon.
- `assembleDSN`/`Marshal`/`Redacted` need **no change** — they already route
  through `Addr()`, so fixing `Addr()` fixes serialization for free.

**Out of scope (do not attempt here):**

- The `net/url` parsing engine and last-`@` credentials split (`plans/003`
  Topics 1, and Finding 2) — see OQ-5.
- Percent **decoding/encoding** of host/zone-id (`%25` ↔ `%`) — zone ids are
  preserved as literal bytes; see OQ-2.
- Multi-host authorities (`redis://h1,h2`) — `plans/003` Topic 4, unrelated.
- IPv6 address **validation** (rejecting `[not-an-address]`) — the parser stays
  syntactic/best-effort; see OQ-2.

## Assumptions

1. **No consumer currently feeds an IPv6 DSN.** Verified across the five known
   consumers (beacon, com_lingocrm_api, hive, wordhunter, vpntunnel) in prior
   recon; they use IPv4 hosts, hostnames, the Telegram authority trick, and sqlite
   paths. Therefore `Host()`/`Port()` moving from garbage to correct has **no known
   downstream impact** — but it is still a behavior change and is flagged in Risks.
2. **A leading `[` unambiguously marks a bracketed IPv6 host literal** (authority
   form). No legitimate scheme-less DSN this library accepts begins with `[`
   otherwise; treating it as an authority is safe.
3. **Bracket-aware splitting triggers ONLY on a leading `[`.** Any instance
   without a leading `[` keeps the exact current first-colon `SplitN` behavior, so
   the Telegram token trick (`host:port` carrying `<botid>:<secret>`, no brackets)
   and all 38 existing cases are byte-for-byte unchanged.
4. **Zone ids and IPv4-mapped literals are preserved verbatim, not interpreted.**
   `[fe80::1%25eth0]` stores host `fe80::1%25eth0`; `[::ffff:1.2.3.4]` stores
   `::ffff:1.2.3.4`. We do not decode `%25`→`%`, validate, or canonicalize. This is
   what makes them round-trip cleanly; see OQ-2.
5. **Recommended baseline: host is stored UNBRACKETED** (`Host()` → `::1`, matching
   `net/url.URL.Hostname()`), and `Addr()` adds brackets on emit. The alternative
   (store bracketed, `Addr()` unchanged) is OQ-1. Task bodies assume the baseline.
6. **No new imports, no CGO, stdlib-only.** `parser.go` already imports `strings`;
   `source.go` already imports `strings` and `fmt`. Every edit uses those.
7. **Gates on this host:** `gofmt -l .` (empty), `CGO_ENABLED=0 go vet ./...`,
   `CGO_ENABLED=0 go test .`. Unlike `plans/001`/`002`, a C toolchain is present
   here, so the race gate runs as `CGO_ENABLED=1 go test -race .` (verified green on
   the current tree). **Never** run `CGO_ENABLED=0 go test -race` — that fails with
   `-race requires cgo`.

## Tasks

### Task 1: Bracket-aware authority split and IPv6-correct `Addr()`

- **Description:** Fix the authority-form failure (class 1) end to end. Two
  co-located edits that **must land in one commit** (see Pitfalls — under the
  unbracketed baseline, the parse edit alone regresses `Addr()`):

  **1a — `parser.go`, replace the `instance` split.** At `parser.go:236-243` the
  `case "instance":` block does a first-colon `SplitN`. Replace its body with a
  call to a new unexported helper `splitHostPort`:

  ```go
  case "instance":
      instance = matches[i]
      m[KeyHost], m[KeyPort] = splitHostPort(instance)
  ```

  Add `splitHostPort` at the bottom of `parser.go` (unexported funcs, alphabetical
  — after `readEnvVarName`):

  ```go
  // splitHostPort splits an authority instance into host and port. A leading '['
  // marks an IPv6 literal: the host runs to the matching ']', with an optional
  // ":port" after it. Without a leading '[' it splits on the FIRST colon —
  // preserving the legacy behavior the Telegram token trick relies on
  // (host:port carrying <botid>:<secret>). A '[' with no matching ']' is malformed
  // and falls through to the first-colon split (best-effort, never panics).
  func splitHostPort(instance string) (host, port string) {
      if strings.HasPrefix(instance, "[") {
          if end := strings.IndexByte(instance, ']'); end >= 0 {
              host = instance[1:end] // UNBRACKETED (OQ-1 baseline); bracketed variant: instance[:end+1]
              if rest := instance[end+1:]; strings.HasPrefix(rest, ":") {
                  port = rest[1:]
              }
              return host, port
          }
      }
      if i := strings.IndexByte(instance, ':'); i >= 0 {
          return instance[:i], instance[i+1:]
      }
      return instance, ""
  }
  ```

  The non-bracket branch is provably identical to the current
  `SplitN(instance, ":", 2)` (first colon → `[:i]`, `[i+1:]`; no colon →
  `instance, ""`).

  **1b — `source.go`, bracket the host in `Addr()`.** At `source.go:99-108`,
  bracket the host when it contains a colon and is not already bracketed, before
  the port is appended:

  ```go
  func (dsm *DataSourceMapper) Addr(defaultPort ...int) string {
      h := dsm.Option(KeyHost)
      if strings.Contains(h, ":") && !strings.HasPrefix(h, "[") {
          h = "[" + h + "]" // IPv6 literal -> bracketed authority form
      }
      p := dsm.Option(KeyPort)
      if len(p) > 0 {
          p = fmt.Sprintf(":%s", p)
      } else if len(defaultPort) > 0 {
          p = fmt.Sprintf(":%d", defaultPort[0])
      }
      return h + p
  }
  ```

  The `!strings.HasPrefix(h, "[")` guard is defensive: it prevents double-bracketing
  whether the host arrived unbracketed (`::1`, the baseline) or already bracketed
  (`[::1]`, e.g. via `New(WithHost("[::1]"))` or a map literal). Because the
  Telegram host component carries no colon (`987654321`), that path is untouched.

  **1c — godoc.** Update the `Addr()` godoc (`source.go:90-98`) to note that a host
  containing `:` is emitted bracketed. Update the `Marshal` godoc
  (`parser.go:16-25`) to carve IPv6 host literals **out** of the "values
  containing `:` do not round-trip" caveat — they now do; the `@`-in-credentials
  and percent-escaping caveats stand.
- **Acceptance Criteria:**
  - `Parse("redis://[::1]:6379")` → `Host()=="::1"`, `Port()==6379`,
    `Addr()=="[::1]:6379"` (baseline; `Host()=="[::1]"` under the bracketed variant
    of OQ-1).
  - `Parse("postgres://u:p@[2001:db8::1]:5432/db")` → `Host()=="2001:db8::1"`,
    `Port()==5432`, `Login()=="u"`, `Password()=="p"`, `Database()=="db"`,
    `Addr()=="[2001:db8::1]:5432"`.
  - `Parse("redis://[::1]")` → `Host()=="::1"`, `Port()==0`, `Addr()=="[::1]"`
    (host-only; see OQ-3).
  - `Marshal(Parse("redis://[::1]:6379/0"))` returns `"redis://[::1]:6379/0"`, and
    the accessor-level round-trip `Parse(Marshal(x))` holds for every authority
    IPv6 case (Task 4 owns the assertions).
  - **Regression:** all 38 `successfulTestCases` pass unchanged; the Telegram
    subtests (`parser_test.go:106-124`, `:409-434`) pass unchanged (`Host()`
    `987654321`, `Port()` the secret, `Addr()` `987654321:<secret>`); IPv4
    `redis://127.0.0.1:6379` → `Addr()=="127.0.0.1:6379"` (no brackets).
  - `gofmt -l .` empty; `CGO_ENABLED=0 go vet ./...` clean;
    `CGO_ENABLED=0 go test .` green; `CGO_ENABLED=1 go test -race .` green.
  - File Declaration Order respected: `splitHostPort` sits with the unexported
    helpers at the bottom of `parser.go`, alphabetically after `readEnvVarName`.
- **Pitfalls & edge cases:**
  - **1a and 1b are NOT independently shippable under the baseline.** After 1a
    alone, authority IPv6 storage becomes `host="::1", port="6379"`, and the *old*
    `Addr()` would emit `"::1:6379"` — regressing the currently-accidentally-correct
    `Addr()`/`Marshal`. 1b restores correctness intentionally. Commit them together;
    do not merge a tree with 1a but not 1b. (Under the **bracketed** OQ-1 variant,
    `splitHostPort` keeps `[::1]`, the old `Addr()` rejoin already yields
    `[::1]:6379`, and 1b is unnecessary — a genuinely smaller change; see OQ-1.)
  - The bracket branch must fire **only** on a leading `[`. Verify the Telegram
    instance `987654321:AAHb1-...` (no `[`) still takes the first-colon path.
  - `Addr()` brackets based on the **host** containing `:`, never the port. The
    Telegram port value carries no colon, so it is never bracketed.
  - Malformed `[::1` (no `]`) must not panic — it falls through to the first-colon
    split (garbage but safe). Add a `require.NotPanics` row.
  - Do **not** touch `assembleDSN` (`parser.go:173-205`) — it calls `ds.Addr()`, so
    1b propagates automatically. Duplicating the bracket logic there would create
    two homes for one decision.
- **Complexity:** Medium (small diff, but guardrail-sensitive and OQ-coupled).

### Task 2: Route scheme-less bracketed IPv6 through the `Parse` classifier

- **Description:** Fix the scheme-less failure (class 2). In `Parse`
  (`parser.go:96-135`), the `?opts` tail is already peeled first for every form
  (`parser.go:101-104`), then a top-level switch splits authority-marked forms
  (`://` or leading `//`) from scheme-less forms. Add a single case to the inner
  scheme-less switch (`parser.go:115-124`), **as its first case**, recognizing a
  leading `[` and reshaping to authority form:

  ```go
  switch colon := strings.IndexByte(dns, ':'); {
  case strings.HasPrefix(dns, "["):
      ds, err = parseAuthority("//" + dns) // scheme-less bracketed IPv6 literal
  case colon >= 0 && colon+1 < len(dns) && dns[colon+1] >= '0' && dns[colon+1] <= '9':
      ds, err = parseAuthority("//" + dns) // host:port
  case colon >= 0:
      ds, err = parseAuthority(dns) // driver:opaque
  case strings.Contains(dns, "/"):
      ds, err = parseBarePath(dns) // bare filesystem path
  default:
      ds, err = parseAuthority("//" + dns) // bare host
  }
  ```

  Reshaping `"//" + dns` reuses the existing regex + Task 1's `splitHostPort`: the
  instance group `[^/@]+` captures the whole `[...]:port` (brackets, inner colons,
  and `%zone` are all outside `/@`), and any `/database` tail falls out for free.
  `parseAuthority` is called directly (never recursing into `Parse`), matching the
  existing host:port and bare-host cases.
- **Acceptance Criteria:**
  - `Parse("[::1]:6379")` → `Host()=="::1"`, `Port()==6379`, `Addr()=="[::1]:6379"`,
    `Database()==""`.
  - `Parse("[::1]")` → `Host()=="::1"`, `Port()==0`, `Addr()=="[::1]"`.
  - `Parse("[2001:db8::1]:5432/db")` → `Host()=="2001:db8::1"`, `Port()==5432`,
    `Database()=="db"`.
  - `Parse("[::1]:6379?db=0")` → `Host()=="::1"`, `Port()==6379`, option `db=="0"`
    (proves `?opts` peeling composes with the new case).
  - The pre-existing `t.Run("ipv6 does not panic")` (`parser_test.go:436-441`) is
    upgraded in Task 4 from a bare `NotPanics` into real assertions (it is now in
    scope).
  - All 38 `successfulTestCases` unchanged; gates green (as Task 1).
- **Pitfalls & edge cases:**
  - **Case order is load-bearing.** The `[` case must precede the colon cases:
    `[::1]:6379`'s first colon is at index 1 (inside the brackets) and is followed
    by `:`, so without the `[` case first it falls to `case colon >= 0`
    (driver-opaque) and breaks exactly as today.
  - **Do not recurse.** Pass `"//" + dns` to `parseAuthority`, never back to
    `Parse` — a `//`-prefixed string would re-enter the top-level authority branch,
    but calling `parseAuthority` directly is the established, loop-free pattern.
  - `?opts` is already peeled before this switch, so `[::1]:6379?db=0` arrives as
    `[::1]:6379`; the new case needs no `?` handling of its own.
  - Authority IPv6 (`redis://[::1]...`) is unaffected here — it matches the
    top-level `Contains("://")` branch and never reaches this switch.
- **Complexity:** Easy.

### Task 3: Normalize host brackets on `SetHost` — construction consistency (OQ-4)

- **Description:** *Conditional on OQ-4.* After Tasks 1-2, `New(WithHost("::1"), …)`
  and `New(WithHost("[::1]"), …)` both produce a correct `Addr()`/`Marshal`
  (Task 1b's `Addr()` handles both bracketed and unbracketed stored hosts). The
  **only** residual wart is that `Host()` is path-dependent: `::1` from `Parse`
  (brackets stripped by `splitHostPort`) versus `[::1]` from `WithHost("[::1]")`.
  To make `Host()` consistent regardless of construction path, normalize in
  `SetHost` (`source.go:151`) so storage is always unbracketed:

  ```go
  func (dsm *DataSourceMapper) SetHost(v string) {
      if strings.HasPrefix(v, "[") && strings.HasSuffix(v, "]") {
          v = v[1 : len(v)-1]
      }
      dsm.SetOption(KeyHost, v)
  }
  ```

  This is a **nicety, not a correctness requirement** — `Addr()`/`Marshal` are
  already correct without it. If OQ-4 chooses "normalize on read only," skip this
  task entirely and accept the documented `Host()` path-dependence. If OQ-1 chooses
  **bracketed** storage, this inverts (`SetHost` *adds* brackets when the value
  contains a colon and lacks them) and becomes **mandatory**, because bracketed
  storage relies on the old `Addr()` rejoin and `WithHost("::1")` would otherwise
  emit `::1:6379`.
- **Acceptance Criteria (baseline, if OQ-4 = normalize-on-set):**
  - `New(WithHost("[::1]")).Host() == "::1"` and `New(WithHost("::1")).Host() == "::1"`.
  - `New(WithHost("[::1]"), WithPort(6379)).Addr() == "[::1]:6379"` (no double
    bracket) and equal to the `WithHost("::1")` construction.
  - `Parse("redis://[::1]:6379").Host()` equals `New(WithHost("::1")).Host()` —
    path-independent `Host()`.
  - Existing `Set*` tests unchanged; gates green.
- **Pitfalls & edge cases:**
  - Strip only a **matched** `[`…`]` pair; leave `[::1` (unmatched) alone.
  - Do not add stripping to the raw `SetOption(KeyHost, …)` path or to
    `parseAuthority`'s direct map write — `splitHostPort` already stores unbracketed
    from `Parse`, and over-normalizing risks surprising a caller who deliberately
    stored brackets. Keep the normalization at the one ergonomic setter.
  - This task may be **dropped** without affecting any Task 1/2 acceptance
    criterion. Sequence it last so it can be cut cleanly if OQ-4 says so.
- **Complexity:** Easy.

### Task 4: IPv6 truth-table tests, round-trip assertions, and subtest upgrade

- **Description:** Lock every IPv6 shape as table-driven assertions and upgrade the
  placeholder test. Concretely:
  - **Append IPv6 rows to `successfulTestCases`** (`parser_test.go:237-395`), each a
    named `t.Run(testcase.src, …)` asserting the full `*DataSourceMapper` (host
    values per the OQ-1 baseline). See the truth table below.
  - **Add IPv6 authority + scheme-less inputs to `TestMarshal`'s network-form
    round-trip `cases`** (`parser_test.go:42-60`), reusing the existing
    `assertRoundTrip` helper (`parser_test.go:14-32`) which compares
    Driver/Host/Port/Login/Password/Database/OptionsNames after `Parse→Marshal→Parse`
    (accessor-level, per plan 002 policy — not raw map identity).
  - **Add a construction subtest** asserting the `New(WithHost(…))` cases, including
    the no-double-bracket and (if OQ-3 = bracket) the host-only `Addr()`.
  - **Upgrade `t.Run("ipv6 does not panic")`** (`parser_test.go:436-441`): replace
    the "out of scope / must not panic" comment and bare `NotPanics` with real
    parse assertions, plus one genuinely-malformed `require.NotPanics` row
    (`[::1` — no closing bracket).

  **Truth table** (`—` = key present but empty; host column is the OQ-1 **baseline**
  unbracketed value):

  | # | Input | driver | host | port | login | password | database | options | `Addr()` |
  |---|---|---|---|---|---|---|---|---|---|
  | 1 | `redis://[::1]:6379` | redis | ::1 | 6379 | — | — | — | — | `[::1]:6379` |
  | 2 | `redis://[::1]:6379/0` | redis | ::1 | 6379 | — | — | 0 | — | `[::1]:6379` |
  | 3 | `redis://[::1]` | redis | ::1 | — | — | — | — | — | `[::1]` |
  | 4 | `redis://[::1]/0` | redis | ::1 | — | — | — | 0 | — | `[::1]` |
  | 5 | `postgres://u:p@[2001:db8::1]:5432/db` | postgres | 2001:db8::1 | 5432 | u | p | db | — | `[2001:db8::1]:5432` |
  | 6 | `postgres://u:p@[2001:db8::1]:5432/db?sslmode=disable` | postgres | 2001:db8::1 | 5432 | u | p | db | sslmode=disable | `[2001:db8::1]:5432` |
  | 7 | `http://[2001:0db8:0000:0000:0000:0000:0000:0001]:8080/x` | http | 2001:0db8:0000:0000:0000:0000:0000:0001 | 8080 | — | — | x | — | `[2001:0db8:…:0001]:8080` |
  | 8 | `http://[fe80::1%25eth0]:8080` | http | fe80::1%25eth0 | 8080 | — | — | — | — | `[fe80::1%25eth0]:8080` |
  | 9 | `https://[::ffff:192.168.1.1]:443/db` | https | ::ffff:192.168.1.1 | 443 | — | — | db | — | `[::ffff:192.168.1.1]:443` |
  | 10 | `[::1]:6379` | — | ::1 | 6379 | — | — | — | — | `[::1]:6379` |
  | 11 | `[::1]` | — | ::1 | — | — | — | — | — | `[::1]` |
  | 12 | `[2001:db8::1]:5432/db` | — | 2001:db8::1 | 5432 | — | — | db | — | `[2001:db8::1]:5432` |
  | 13 | `[::1]:6379?db=0` | — | ::1 | 6379 | — | — | — | db=0 | `[::1]:6379` |

  **Round-trip set** (add rows 1, 2, 5, 6, 8, 10, 12 to `TestMarshal` cases):
  each asserts `Parse(Marshal(Parse(src)))` is accessor-equal to `Parse(src)`.
  Note the string form for scheme-less rows changes (`[::1]:6379` → Marshal →
  `//[::1]:6379`) but the accessors are stable — that is the correct, documented
  behavior.

  **Construction subtest** (`New`):
  - `New(WithHost("::1"), WithPort(6379)).Addr() == "[::1]:6379"`.
  - `New(WithHost("[::1]"), WithPort(6379)).Addr() == "[::1]:6379"` and equals the
    line above.
  - `New(WithHost("::1")).Addr() == "[::1]"` (per OQ-3 baseline).

  **Regression guardrails** (must remain green, unchanged):
  - All 38 `successfulTestCases`.
  - Telegram: `tbot://123456789:@987654321:AAHb1-Cd_EfGhIjKlMnOpQrStUvWxYz0123456789`
    with and without trailing slash → `Login()=="123456789"`,
    `Addr()=="987654321:AAHb1-Cd_…"`, identical across the slash variants.
  - IPv4: `redis://127.0.0.1:6379` → `Addr()=="127.0.0.1:6379"`; scheme-less
    `1.2.3.4:5432` → `Host()=="1.2.3.4"`, `Port()==5432`.
- **Acceptance Criteria:**
  - `CGO_ENABLED=0 go test -run 'TestParse|TestMarshal' .` green; every truth-table
    row is a named subtest asserting the full mapper (or the OQ-1 host value).
  - Round-trip rows green through `assertRoundTrip`.
  - The former `ipv6 does not panic` subtest now asserts real host/port/addr values
    plus a malformed `NotPanics` row; no test comment still calls IPv6 "out of
    scope."
  - `CGO_ENABLED=1 go test -race .` green; `gofmt -l .` empty; `go vet` clean.
  - `t.Parallel()` on the new top-level/subtests where there is no shared state,
    per `stack-go` conventions.
- **Pitfalls & edge cases:**
  - Fixtures list all six reserved keys; rows with empty login/password/database
    must set them to `""` present, mirroring exactly what `parseAuthority` emits, or
    `require.Equal` on the `*DataSourceMapper` fails on a missing key.
  - The **host column flips with OQ-1**: if the session picks bracketed storage,
    every host cell becomes `[::1]`, `[2001:db8::1]`, etc. Regenerate the column
    from the decision before writing fixtures — do not hardcode before OQ-1 is
    settled.
  - Zone-id row 8 asserts the byte-literal `fe80::1%25eth0`; do **not** assert a
    decoded `fe80::1%eth0` unless OQ-2 chooses decoding (it leans against).
  - Keep the malformed `[::1` assertion to `NotPanics` only — its parsed output is
    intentionally unspecified (best-effort), so asserting exact garbage would lock
    an accident.
- **Complexity:** Medium (breadth, not depth; co-developed with Tasks 1-2).

## Execution Order

1. **Task 1** (authority split + `Addr()`) — the core fix; 1a+1b co-committed.
2. **Task 2** (scheme-less classifier) — depends on `splitHostPort` from Task 1;
   purely additive (only reshapes inputs that were garbage before), so it cannot
   regress the 38 guardrails.
3. **Task 4** (tests) — co-developed with Tasks 1-2 in practice (write the row,
   make it pass); per `CLAUDE.md` commit discipline the tests ride in the commit of
   the code they cover. Listed after so the full table is one reviewable deliverable.
4. **Task 3** (`SetHost` normalization) — **last and optional**, gated on OQ-4;
   droppable without touching any Task 1/2/4 acceptance criterion.

Tasks 1 and 2 touch the same two functions region and are sequential. Nothing here
parallelizes meaningfully; the whole plan is a few dozen lines plus a test table.

## Requirements Traceability

| Requirement (from the task brief) | Satisfied by |
|---|---|
| Central design decision: bracketed vs unbracketed host storage | **OQ-1** (headline); baseline threaded through Tasks 1, 3, 4 |
| Parse authority path: bracket-aware host:port split, Telegram untouched | Task 1a (`splitHostPort`); Task 4 regression rows |
| Parse scheme-less path: bracket-aware classifier | Task 2; Task 4 rows 10-13 |
| `Addr()` brackets host containing `:`, no double-bracket, no-port behavior | Task 1b; **OQ-3**; Task 4 `Addr()` column + construction subtest |
| `assembleDSN`/`Marshal`/`Redacted` emit bracketed authority + round-trip | Task 1b (via `Addr()`, no assembler change); Task 4 round-trip set |
| Construction `New(WithHost("::1"))` and `New(WithHost("[::1]"))` both correct | Task 1b (Addr covers both); Task 3 + **OQ-4** (Host() consistency) |
| Zone ids (`%25`) and IPv4-mapped: support scope | Assumption 4; **OQ-2**; Task 4 rows 8, 9 |
| Full test truth table incl. regression set | Task 4 (13 rows + round-trip + Telegram + IPv4 + malformed) |
| 38 existing `TestParse` cases pass unchanged | Task 1 Pitfalls (non-bracket path byte-identical); Task 4 regression |
| Telegram `Addr()`-as-token trick intact | Assumption 3; Task 1 Pitfalls; Task 4 Telegram subtest |
| Accidental `Addr()` correctness becomes intentional; `Host()`/`Port()` change flagged | Overview; Risks; **OQ-1** |
| Zero-dependency, no CGO, regex-preserving, surgical vs `net/url` | Overview; Assumption 6; **OQ-5**; Trade-offs |
| Re-verify claims against live package | Done (probe at HEAD `749f521`, removed; table in Overview) |

## Risks

- **`Host()`/`Port()` behavior change for authority IPv6.** They go from garbage
  (`[`, `:1]:6379`) to correct (`::1`, `6379`). No known consumer reads IPv6
  (Assumption 1), and garbage-to-correct is the intended fix, but any consumer that
  somehow depended on the old values (implausible) would see a change. Classify the
  commit `fix:`; flag at review.
- **`Addr()` regression window inside Task 1.** Landing 1a without 1b (unbracketed
  baseline) emits `::1:6379` — worse than today's accidental correctness. Mitigation:
  co-commit; the round-trip tests catch it immediately.
- **OQ-1 not settled before implementation.** The host storage decision changes the
  `splitHostPort` slice bound, whether Task 1b/Task 3 are needed, and every host
  fixture. Mitigation: this plan gates execution on approving the Open Questions;
  the flip points are marked inline.
- **Leading-`[` false positives.** A non-IPv6 input beginning with `[` would be
  routed to `parseAuthority`. No such DSN exists in this library's accepted set
  (Assumption 2); documented, low risk.
- **Zone-id `%` and future percent-decoding.** Preserving `%25` literally is correct
  today and round-trips, but if `plans/003`'s `net/url` engine later adds
  percent-decoding, `%25`→`%` normalization must be reconciled then. Flagged for
  OQ-5 sequencing, not fixed here.
- **`-race` toolchain.** Runs here only as `CGO_ENABLED=1 go test -race .` (C
  toolchain present); the CGO-disabled gate must never carry `-race`. Same
  reconciliation the earlier plans noted, now runnable on this host.

## Trade-offs

- **Surgical bracket-aware split over adopting `net/url` (chose the ~15-line fix).**
  `net/url` fixes IPv6 for free but drags in the last-`@` credentials split,
  percent-encoding, and rejection of the loose scheme-less inputs that are this
  library's reason to exist — a behavior-shifting redesign (`plans/003` Topic 1).
  IPv6 is a self-contained bug; fixing it in place keeps the 38 guardrails and the
  Telegram trick provably intact and defers the engine question to a real decision
  (OQ-5). Chose the scalpel; the `net/url` swap stays a separate, larger plan.
- **Hand-rolled `splitHostPort` over stdlib `net.SplitHostPort` (chose in-house).**
  `net.SplitHostPort` rejects a missing port (`redis://[::1]` has none), rejects the
  bare host case, and would not honor the Telegram first-colon trick — so it cannot
  serve this splitter without wrapping every rejection. A six-line bracket-aware
  helper is simpler and keeps behavior under our control. (`net` is stdlib, so this
  is an ergonomics/behavior call, not a dependency one.)
- **Bracket-on-emit in `Addr()` over `net.JoinHostPort` (chose inline).**
  `net.JoinHostPort` brackets correctly but *mandates* a port — it emits a trailing
  `:` for the empty-port case (`[::1]:`), which breaks the host-only `Addr()` and
  `Marshal("redis://[::1]")`. The inline `strings.Contains(h, ":")` bracket avoids
  that and keeps the no-port form clean.
- **`Addr()` as the single serialization chokepoint (no `assembleDSN` change).**
  Because `assembleDSN`/`Marshal`/`Redacted` all compose through `Addr()`, fixing
  `Addr()` fixes every emitter at once — one home for the bracketing decision, no
  duplication.
- **Unbracketed storage baseline over bracketed (soft lean, deferred to OQ-1).**
  Unbracketed gives clean, `net/url.Hostname()`-shaped `Host()`/`Port()` accessors —
  the observable API — at the cost of an `Addr()` edit. Bracketed is a strictly
  smaller diff (`Addr()` untouched) but leaks `[` `]` into `Host()`. The observable
  API wins the lean; the session ratifies in OQ-1.

## Open Questions (for the discussion session)

Settle these **before** implementing; each flips one or more of the flagged lines
above. Leans are noted but not committed.

### OQ-1 — Host storage: UNBRACKETED (`::1`) or BRACKETED (`[::1]`)? *(headline)*

The one decision that ripples through the whole plan.

- **Unbracketed (`Host()` → `::1`)** — *recommended.* Matches
  `net/url.URL.Hostname()`; `Host()`/`Port()` are clean and directly usable.
  **Cost:** `splitHostPort` strips brackets, and `Addr()` **must** re-add them on
  emit (Task 1b) — otherwise `Marshal` produces the ambiguous `::1:6379`. Also makes
  `WithHost("[::1]")` vs `Parse` diverge on `Host()` unless normalized (OQ-4).
- **Bracketed (`Host()` → `[::1]`)** — *smaller diff.* `splitHostPort` keeps the
  brackets; the existing `Addr()` rejoin (`h + ":" + p`) already yields
  `[::1]:6379`, so **Task 1b is unnecessary** and this becomes the minimal change.
  **Cost:** `Host()` returns `[::1]` (leaky, non-`net/url`, a caller must strip
  brackets before `net.Dial`), and `New(WithHost("::1"))` now needs `SetHost` to
  *add* brackets (Task 3 becomes mandatory and inverts).
- **Lean:** unbracketed — the accessor is the public contract and should be the
  clean form; pay the one `Addr()` edit.

### OQ-2 — Zone ids and validation scope

`[fe80::1%25eth0]` (percent-encoded) / `[fe80::1%eth0]` (raw), and
`[::ffff:1.2.3.4]`. Do we (a) preserve the literal bytes, (b) percent-decode
`%25`→`%`, (c) validate that the brackets contain a real IPv6 address?

- **Lean:** preserve literal, no decode, no validation. It round-trips cleanly,
  stays zero-dependency, and matches the library's syntactic/best-effort character.
  Percent-decoding belongs with the `net/url` engine (OQ-5); validation would reject
  inputs the loose parser is meant to pass through. Revisit only if a consumer needs
  canonicalization.

### OQ-3 — `Addr()` with no port: `[::1]` or `::1`?

For a host-only IPv6 (`redis://[::1]`), should `Addr()` return `[::1]` or `::1`?

- **Lean / near-forced:** `[::1]`. `assembleDSN` builds `Marshal` from `Addr()`, and
  `redis://::1` is ambiguous/invalid, so the bracket is required for `Marshal` to be
  correct — i.e. bracket **whenever the host contains a colon, regardless of port**.
  The only way to return a bare `::1` from `Addr()` while keeping `Marshal` correct
  would be to duplicate the bracket logic into `assembleDSN` (two homes). Confirm
  the forced answer, or accept the duplication.

### OQ-4 — Normalize host on Set or on Read?

Under the unbracketed baseline, `Addr()` already normalizes **on read** (brackets
if needed, guards against double-bracket), so `Marshal` is correct from any
construction path. The open part is whether to *also* normalize **on set**
(`SetHost` strips a bracket pair, Task 3) so `Host()` is path-independent.

- **Lean:** yes, normalize on set too — one extra `if` for a consistent `Host()`;
  cheap, and it removes the "`Parse` gives `::1` but `WithHost("[::1]")` gives
  `[::1]`" wart. But it is optional under the baseline (correctness holds without
  it), so it can be dropped to keep the diff minimal. (Under bracketed storage,
  OQ-1, on-set normalization is mandatory, not optional.)

### OQ-5 — Keep this surgical, or fold IPv6 into the `net/url` engine decision?

`plans/003` Topic 1 weighs adopting `net/url` internally, which would fix IPv6,
the last-`@` split, and percent-encoding together.

- **Lean:** keep IPv6 surgical **now** (this plan), and let the `net/url` decision
  proceed on its own timeline. IPv6 is a live, self-contained silent-corruption bug
  with no consumer workaround; the `net/url` migration is a larger compatibility
  sweep that must be sequenced after the scheme-less work and cannot be rushed to
  carry IPv6. If the session instead wants to fold them, this plan becomes the IPv6
  slice of that larger effort and its `splitHostPort`/`Addr()` edits are superseded
  by the engine — decide the sequencing, not the destination.
