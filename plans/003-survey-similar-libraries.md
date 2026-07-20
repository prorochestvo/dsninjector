# Survey of Similar Libraries — Discussion & Decision Primer

> **STATUS: DISCUSSION ONLY — DO NOT IMPLEMENT.** Decisions to be made in a later session; each accepted topic becomes its own future plan.

## Purpose

This document surveys how the wider Go ecosystem parses and emits connection
strings, compares those approaches against `dsninjector` as it stands today, and
lays out the design questions worth deciding. It is a **decision menu**, not a
task breakdown: there are no acceptance criteria and no execution order here on
purpose. Each topic below states a tension, its cost against our core values, and
a soft lean — but makes no commitment. When a topic is accepted in a later
session it will get its own `plans/NNN-*.md` with a real breakdown.

The correctness findings in this document are real, present-day bugs; they are
included because they are the strongest evidence for several of the topics, and
because deciding "adopt `net/url`?" (Topic 1) is largely a decision about whether
to fix four of them for free versus by hand.

## Design values (the rubric every topic is weighed against)

Every option below is judged against what makes this library worth having:

1. **Zero runtime dependencies** — stdlib only; `testify` stays test-only; no CGO.
   Any topic that drags in a third-party runtime import, or a *maintenance*
   coupling that grows without bound, pays a heavy tax.
2. **Tiny surface** — a small, deep API. New public methods must earn their keep.
3. **Driver-agnostic** — one generic parser serves postgres/mysql/mongo/redis/
   sqlite *and* non-database schemes: telegram bot tokens, LLM/AI base URLs, HTTP
   proxies. Anything that special-cases a driver erodes this.
4. **Loose, scheme-optional input** — the deliberate niche. This library accepts
   what RFC 3986 parsers reject: `localhost:5432`, `google.com`, opaque
   `sqlite:/path`. It is env-first (`Unmarshal`, `InitEnvFrom`) with a free-form
   options bag. This is precisely the property most alternatives do **not** have,
   and the one most at risk when we borrow from them.

## What dsninjector is today (baseline)

- `Parse(dsn) -> *DataSourceMapper` — one named-group regex (`parser.go:91`),
  then post-processing of `instance` → host/port and `credentials` →
  login/password. A `map[string]string` value object; six reserved keys
  (`source.go:90-97`), everything else a free-form option.
- `Marshal(DataSource) -> string` — rebuilds a string (`parser.go:16-27`).
  Currently **not** a faithful inverse of `Parse` (see Finding 4).
- `Unmarshal` / `UnmarshalOrEmpty` — read a DSN from an env var.
- `InitEnvFrom` — load `KEY=VALUE` lines from `.env`-style files
  (`readEnvVarName`, `parser.go:158`).

## Correctness findings (the evidence)

All five are **silent** — the parser returns no error, it returns wrong data.
That is worse than a crash, because the caller connects to the wrong place or
persists a corrupted config. Severities reflect blast radius and how quietly the
bug fires, not review-gate priority.

| # | Finding | Severity | Location | Symptom |
|---|---------|----------|----------|---------|
| 1 | IPv6 authority mis-split | High | `parser.go:118-124` | `redis://[::1]:6379` → host `[`, port `:1]:6379`. Any bracketed IPv6 literal is silently mangled. |
| 2 | `@` in password mis-split | High | `parser.go:91` (credentials group `[^/@]+`) | The group forbids `@`, so `user:p@ss@host` splits on the first `@`. RFC parsers split authority on the **last** `@`. |
| 3 | No percent decode/encode for creds/host/db | Medium | `parser.go:143-149` (only `params` go through `url.ParseQuery`) | Encoded userinfo/host/path pass through raw; `Marshal` never re-encodes. Asymmetric and lossy. |
| 4 | `Marshal` is not the inverse of `Parse` | High | `parser.go:16-27` (base64 injected at `:26`) | Injects `AuthBasicBase64()` into the userinfo slot, force-adds `@` and `/database`, CSV-flattens options. `TestMarshal` is `t.Skip`. A real consumer — beacon `proxyutil` — already loses proxy userinfo because of this. |
| 5 | Reserved-key / option-key collision | High | `parser.go:147-149` vs `source.go:90-97` | Fields and options share one flat map and query params are applied **last**, so `?driver=x&port=y` silently overwrites the parsed driver/port. No surveyed library has this class of bug. |

## Decision topics

Ordered by leverage. For each: the tension, the zero-dependency cost, a one-line
lean. No decisions, no steps.

### Topic 1 — Parsing engine: adopt `net/url` internally, or keep the regex

- **Tension / decision:** `net/url` correctly handles IPv6 brackets, splits
  authority on the last `@`, and does percent decode/encode — fixing Findings
  1, 2, and 3 outright — and hands us `Redacted()` and a faithful `String()` for
  free (relevant to Topics 2 and 10). **But** `net/url` rejects or misreads
  exactly the loose, scheme-less inputs that are this library's whole reason to
  exist (`localhost:5432` parses with `localhost` as the scheme; `google.com`
  has no authority), and it **inverts our credentials rule**: our contract is
  "credentials with no colon ⇒ password-only" (`parser.go:132-135`), whereas
  `net/url` treats a colon-less userinfo as **username-only**. Adoption is
  therefore not a swap; it is `net/url` behind a pre-normalizer plus a full
  compatibility sweep against the existing `TestParse` truth table
  (`parser_test.go:47-127`) — *including the scheme-less cases that plan 001 is
  about to add*.
- **Zero-dependency cost:** None — `net/url` is stdlib. The cost is
  compatibility risk and a normalizer layer, not an import.
- **Lean:** Lean toward a **hybrid** (normalize the loose input, then delegate to
  `net/url`, keeping our contract on top) over both raw adoption and the
  status-quo regex — but this is the single most important item to discuss, and
  it must be sequenced *after* plan 001 so the compat sweep tests the final
  scheme-less behavior, not today's.
- **Related findings:** 1, 2, 3.

### Topic 2 — Make `Marshal` a faithful inverse of `Parse`

- **Tension / decision:** `Marshal` today is effectively broken: it writes
  `base64(login:password)` into the userinfo slot, always emits `@` and
  `/database` even when absent, and joins multi-value options with commas. The
  invariant a serializer must hold — `Parse(Marshal(x))` round-trips `x` — does
  not hold, which is why `TestMarshal` is skipped and why beacon `proxyutil`
  loses proxy userinfo. `AuthBasicBase64()` (`source.go:36-41`) is an HTTP
  `Authorization: Basic` header helper and should stay one — it does not belong
  in DSN serialization.
- **Zero-dependency cost:** None — in-house, stdlib only. (If Topic 1 lands,
  `net/url.String()` does most of this for free.)
- **Lean:** Lean **fix**. A `Marshal` that cannot reproduce what `Parse` read is
  not a feature; decouple base64 into the explicit header helper and make
  `Marshal` emit only what was present.
- **Related finding:** 4.

### Topic 3 — Reserved-key vs option-key collision

- **Tension / decision:** Because the six reserved fields and every free-form
  option live in one flat `map[string]string`, and query params are applied last
  (`parser.go:147-149`), a DSN like `postgres://h/db?driver=x&port=9999` silently
  overwrites the parsed driver and port. Options: reject/ignore reserved keys
  arriving from the query string (cheap, backward-compatible), or restructure so
  reserved fields and the options bag are separate namespaces (cleaner, but a
  data-model change touching `OptionsNames`/`Option`/`Marshal`).
- **Zero-dependency cost:** None — in-house.
- **Lean:** Lean **fix**, and lean toward **guarding reserved keys** over a full
  restructure: it closes the silent-corruption hole with the smallest, most
  backward-compatible change.
- **Related finding:** 5.

### Topic 4 — IPv6 literals, and whether to support multi-host authorities

- **Tension / decision:** IPv6 support (Finding 1) falls out of Topic 1 almost
  for free. The genuinely open question is **multi-host** authorities —
  `redis://h1,h2,h3`, mongo replica-set / SRV seed lists, postgres HA
  `host1,host2` — which the driver-native parsers all support and which our
  single-`hostname` model (`source.go:46-47`, `Addr` at `:25-34`) cannot
  represent without breaking that model.
- **Zero-dependency cost:** IPv6 — none. Multi-host — no import, but a
  value-object model change (host becomes a list) that ripples through `Addr`,
  `Marshal`, and every consumer reading `Host()`.
- **Lean:** Lean **fix IPv6**; lean **defer/decline multi-host** unless a
  concrete consumer needs it — breaking the single-host value object for a case
  that may never appear in our usage is speculative. Poll the actual beacon /
  lingocrm usage before deciding.
- **Related finding:** 1.

### Topic 5 — Typed option accessors

- **Already scoped in plan 002.** Mentioned here only so the leverage ordering is
  complete; do not duplicate the work. See "Relationship to plans 001 & 002".

### Topic 6 — Per-driver default-port table

- **Tension / decision:** Libraries like go-redis default to `6379`, mysql to
  `3306`, etc. A built-in table would let `Port()`/`Addr()` return the right
  default per driver. But that table *is* driver knowledge, embedded in a
  library whose value is being driver-agnostic — and it would need perpetual
  maintenance as schemes appear.
- **Zero-dependency cost:** No import, but a permanent conceptual coupling: the
  library would now have to *know* every driver's default port.
- **Lean:** Lean **con**. Keep defaults at the call site — `Addr(defaultPort ...int)`
  (`source.go:25`) already lets the caller supply the one port it knows about.
- **Related:** compare with go-redis `ParseURL` (Sources).

### Topic 7 — Driver alias normalization (`pg` → `postgres`)

- **Tension / decision:** `xo/dburl` keeps an alias table and exposes
  `UnaliasedDriver`. Normalizing would make `pg://` and `postgres://` interchange
  cleanly. But our niche is preserving the scheme **verbatim** — `pg://` today
  round-trips as `pg` (`parser_test.go:69-71`), and callers using non-DB schemes
  (`tbot`, `https`, LLM base URLs) rely on the scheme being untouched.
- **Zero-dependency cost:** No import, but the same driver-knowledge coupling as
  Topic 6, and a direct conflict with the "keep scheme verbatim" contract.
- **Lean:** Lean **opt-in only** — never normalize by default; if demanded, offer
  an explicit `UnaliasedDriver()`-style accessor that leaves the stored scheme
  untouched.

### Topic 8 — Generate driver-native DSNs (à la `xo/dburl` `GenXxx`)

- **Tension / decision:** `xo/dburl` converts a URL into each driver's *native*
  DSN (`GenPostgres`, `GenMySQL`, …) so the result can be handed straight to
  `sql.Open`. High practical value — but this is literally `xo/dburl`'s entire
  reason to exist, and it requires encoding and maintaining every driver's DSN
  grammar.
- **Zero-dependency cost:** No hard import (generating a string does not import
  the driver), but a **maintenance dependency** that scales with the number of
  drivers — the opposite of a tiny, driver-agnostic surface.
- **Lean:** Lean **out-of-scope**. Point users at `xo/dburl` for native-DSN
  generation rather than absorbing its scope; keep this library the generic
  front-end.

### Topic 9 — Struct-tag binding (envconfig-style)

- **Tension / decision:** `kelseyhightower/envconfig` and `caarlos0/env` bind
  environment variables directly onto tagged structs (`env:"..."`), with
  generics (`ParseAs[T]`). Attractive ergonomics, but it is a **different product
  category** — reflection-driven config binding, not DSN parsing.
- **Zero-dependency cost:** Achievable with stdlib `reflect` (zero import) but a
  large new surface and reflection cost, or a third-party dep — both violate the
  "tiny surface" value.
- **Lean:** Lean **con**. Those libraries already own this space and are
  zero-dep; composing with them beats reimplementing them.

### Topic 10 — Secret-handling policy

- **Tension / decision:** `net/url` offers `Redacted()` (password replaced with
  `xxxxx`) so URLs can be logged safely. The concrete `Redacted()` accessor is
  scoped in plan 002; the **policy** question is broader: document explicitly
  that `AuthBasicBase64()` is *encoding, not secrecy* (base64 is trivially
  reversible), and that `Marshal`/`String()` output must never be logged raw.
- **Zero-dependency cost:** None — stdlib plus documentation.
- **Lean:** Lean **adopt a policy**: let plan 002 land `Redacted()`, and here
  commit only to the documentation stance (base64 ≠ secret; redact before
  logging).
- **Related:** overlaps plan 002 (redaction).

### Topic 11 — `.env` reader robustness

- **Tension / decision:** `readEnvVarName` (`parser.go:158`) is a bare
  `SplitN("=", 2)` plus upper-casing — no comment lines (`#`), no quoted values,
  no `export ` prefix, no multiline values. `joho/godotenv` handles all of these
  robustly. The choice is harden in-house versus adopt `godotenv`.
- **Zero-dependency cost:** In-house hardening — none (comments/quotes/`export`
  are a few lines). `godotenv` — **one runtime dependency**, breaking the core
  invariant for convenience.
- **Lean:** Lean **harden in-house**. The gap is small and the zero-dep value is
  the whole point; borrow `godotenv`'s behavior, not its import.

## Capabilities comparison

Legend: ✓ yes · ✗ no · ~ partial/awkward · n/a not applicable.
"driver-native" summarizes the dedicated per-driver parsers (pgx, go-sql-driver/
mysql, mongo `connstring`, go-redis); "envconfig-style" summarizes
`kelseyhightower/envconfig`, `caarlos0/env`, and `joho/godotenv`.

| Capability | `net/url` | `xo/dburl` | driver-native | envconfig-style | dsninjector (today) |
|---|:---:|:---:|:---:|:---:|:---:|
| Loose / scheme-optional input (`localhost:5432`, `google.com`) | ✗ | ✗ | ~ | n/a | **✓** |
| Opaque path DSN (`sqlite:/path`, `mysql:/sock`) | ~ | ✓ | ✓ | n/a | ✓ |
| IPv6 bracket authority | ✓ | ✓ | ✓ | n/a | ✗ (Finding 1) |
| Percent decode/encode round-trip | ✓ | ✓ | ✓ | n/a | ✗ (Finding 3) |
| Faithful round-trip serialization | ✓ (`String`) | ~ (`GenXxx`, not identity) | ✓ (`FormatDSN`) | n/a | ✗ (Finding 4) |
| Secret redaction | ✓ (`Redacted`) | ~ | ~ | n/a | ✗ (plan 002) |
| Multi-value query params | ✓ (`url.Values`) | ✓ | ~ | n/a | ✗ (CSV-flattened) |
| Multi-host authority (`h1,h2`) | ✗ | ~ | ✓ | n/a | ✗ |
| Driver alias table (`pg`→`postgres`) | ✗ | ✓ | ✗ | n/a | ✗ |
| Per-driver default ports | ✗ | ~ | ✓ | n/a | ✗ (call-site `Addr`) |
| Generate driver-native DSN | ✗ | ✓ (`GenXxx`) | ✓ (own format) | n/a | ✗ |
| Env-var + `.env` loading | ✗ | ✗ | ✗ | ✓ | ✓ |
| Struct-tag binding | ✗ | ✗ | ✗ | ✓ | ✗ |
| Domain-agnostic (DB **and** non-DB: telegram/LLM/proxy) | ~ (generic URLs, rejects loose) | ✗ (DB schemes only) | ✗ | ~ (any config, not DSN-shaped) | **✓** |
| Zero runtime deps (stdlib-only) | ✓ | ~ (parsing light; `Open`/`Gen` assume driver imports at call site) | ✗ (is the driver) | ✓ | **✓** |

The two bottom-most rows are the point: **loose input**, **domain-agnosticism**,
and **zero-dep** are where this library is alone or nearly alone. Every topic that
borrows a capability from the left columns must not cost a checkmark in those
three rows.

## Relationship to plans 001 & 002

This document deliberately excludes what the sibling plans in this review batch
already cover, so it addresses only the genuinely deferred topics.

- **Plan 001 — scheme-less parsing (and the relative-path bug).** Fixes the
  validity gap from the review: today `tbot://TOKEN` and a bare domain like
  `https://google.com` are awkward, and `path.Join("/", …)` at `parser.go:138`
  force-prepends a slash (`file:myfile.sqlite3` → `/myfile.sqlite3`,
  `parser_test.go:104-107`). **Consequence for this document:** Topic 1 (adopt
  `net/url`) and Topic 4 (IPv6) must be sequenced *after* plan 001, because the
  `net/url` compatibility sweep has to validate against the *final* scheme-less
  truth table, not today's.
- **Plan 002 — typed options, redaction, non-leaking errors, test helpers.**
  Adds typed option accessors (Topic 5, do not duplicate), lands `Redacted()`
  (overlaps Topic 10 — this document keeps only the broader *policy* question),
  tightens error messages so inputs do not leak, and un-skips / builds out the
  test helpers (`TestMarshal`, `TestUnmarshal` at `parser_test.go:11-21`). Topic 2
  (Marshal round-trip) will finally make `TestMarshal` meaningful, so it should be
  aware of plan 002's helper work.

Everything else in this document — the parsing engine, Marshal round-trip, the
reserved-key collision, multi-host, default ports, alias normalization,
native-DSN generation, struct-tag binding, and `.env` hardening — is **not**
touched by plans 001 or 002 and is the actual subject of the future session.

## Recommended discussion order / next-session agenda

Decide in this order; the reasoning is dependency and cheapness, not leverage:

1. **Topic 1 (engine) — decide the *direction* first.** It is the fork in the
   road: Topics 2, 3, 4, and 10 all change shape depending on whether `net/url`
   sits underneath. Decide the principle now, but **gate implementation on plan
   001** landing so the compat sweep tests the final behavior.
2. **Topics 3 and 2 (collision, Marshal) — the cheap, unambiguous bug fixes.**
   Both are silent-data-integrity bugs, zero-dep, and decidable independently of
   Topic 1's outcome. Good early wins; each is small enough to plan quickly.
3. **Topic 4 (IPv6 + multi-host).** IPv6 rides on Topic 1. Multi-host needs a
   concrete consumer — check the real beacon / lingocrm usage before committing
   to break the single-host model.
4. **Topics 6, 7, 8, 9 together — "what dsninjector is *not*."** These are scope
   / identity calls (default ports, alias normalization, native-DSN generation,
   struct-tag binding). The lean on all four is out-of-scope or opt-in; decide
   them as a batch to draw the boundary once and close them fast.
5. **Topic 11 (`.env` hardening).** Independent and cheap; decide any time.
6. **Topic 10 (secret policy).** Fold into the plan 002 discussion, since
   `Redacted()` lands there; only the documentation stance is left to ratify.

**Each accepted topic becomes its own `plans/NNN-*.md`** with a real task
breakdown, acceptance criteria, and execution order. This document creates none
of that; it exists to make the decisions above informed.

## Sources

- `net/url` (RFC 3986 parsing, IPv6, percent-encoding, `url.Values`,
  `Redacted()`, faithful `String()`, `Userinfo`): https://pkg.go.dev/net/url
- `xo/dburl` (50+ schemes, alias table, `UnaliasedDriver`, per-driver `GenXxx`,
  `sql.Open` wrapper): https://github.com/xo/dburl
- `jackc/pgx` (`ParseConfig` → typed `*pgx.ConnConfig`; connection-string
  reconstruction via the underlying `pgconn` config):
  https://github.com/jackc/pgx · https://pkg.go.dev/github.com/jackc/pgx/v5
- `go-sql-driver/mysql` (`Config`, `ParseDSN`, `FormatDSN` — typed, round-trips,
  IPv6): https://github.com/go-sql-driver/mysql
- MongoDB `connstring` (multi-host, SRV, typed options; experimental/internal;
  v1 deprecated, moved to `.../v2/...`):
  https://github.com/mongodb/mongo-go-driver ·
  https://pkg.go.dev/go.mongodb.org/mongo-driver/v2/x/mongo/driver/connstring
- `redis/go-redis` (`ParseURL`, default port `6379`, typed query options):
  https://github.com/redis/go-redis ·
  https://pkg.go.dev/github.com/redis/go-redis/v9
- `kelseyhightower/envconfig` (struct-tag env binding):
  https://github.com/kelseyhightower/envconfig
- `caarlos0/env` (generics `ParseAs[T]`, zero third-party deps):
  https://github.com/caarlos0/env  (module `github.com/caarlos0/env/v11`)
- `joho/godotenv` (robust `.env`: comments, quotes, `export`, multiline):
  https://github.com/joho/godotenv

## Review Revisions (7-reviewer gate, 2026-07-19)

Gate: **REVISE** — six of seven lenses passed; one blocking consistency defect
plus minor accuracy corrections. This document is discussion-only, so the fixes
below are applied to the text itself.

### Blocking — Topic 2 (Marshal round-trip) is already owned by plan 002 Task 2

Plan 002 Task 2 **fully implements** the Marshal round-trip fix (drops
`AuthBasicBase64`/base64 from `Marshal`, emits credentials/database only when
present, sorts query keys deterministically, un-skips `TestMarshal`). Topic 2 must
not re-own it. Corrections:

- Give Topic 2 the same "**already scoped in plan 002 Task 2 — do not
  duplicate**" treatment used for Topics 5 and 10. Narrow any residual concern to
  **deep** round-trip correctness only (percent-escaping, socket/file forms) and
  fold that under Topic 1 (the `net/url` engine).
- In "Relationship to plans 001 & 002": move **Marshal round-trip** out of the
  "not touched by 001/002" list, and correct the plan-002 bullet to state that 002
  delivers the Marshal round-trip fix itself (not merely `TestMarshal`
  test-helper work).

### Minor corrections (apply to the text)

- **Finding 4 / Topic 2 mechanism:** the comma-flattening of multi-value query
  params happens in **`Parse`** (`parser.go:148`, `strings.Join(v, ",")`), not in
  `Marshal`. The round-trip-loss conclusion stands; the mechanism attribution is
  corrected.
- **Finding 3 nuance:** `url.ParseQuery` **does** percent-decode query values on
  `Parse`; the precise defect is **asymmetry** (decoded on `Parse`, not re-encoded
  on `Marshal`), not a total absence of decoding.
- **Stale post-001 line refs:** Topic 3's `parser.go:147-149` should point at the
  new `applyOptions` helper once plan 001 lands.
- **Topic 1 sharpening:** `net/url`'s `String()` emits the RFC-normalized form, so
  it does **not** round-trip loose inputs (`localhost:5432`, `sqlite:/path`) for
  free — the pre-normalizer, not `net/url`, remains the owner of the loose-input
  niche. The compatibility sweep must also cover `net/url`'s **userinfo**
  percent-decoding, not only the colon-less credentials inversion.
- **Topic 11 cost:** multiline `.env` values require abandoning the current
  line-by-line `bufio.ScanLines` loop (`parser.go:167-183`) — more than "a few
  lines"; the lean (harden in-house) is unchanged.
- **Agenda:** note that Topic 5 (typed options) is delivered in plan 002 and is
  off the next-session agenda.
- **Discussion-only phrasing:** soften "gate implementation on plan 001 landing"
  to "if adopted, sequence after plan 001" to keep the framing hypothetical.
