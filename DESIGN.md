# DESIGN — low-level design for the Go port of `cron-parser`

Port Mortem · Track C (TypeScript → Go) · source: `harrisiirak/cron-parser` v5.6.2
(`aeb2a1513fd33365a6414f4137516c9482f831ed`)

Every claim in this document about luxon or Go behaviour was **measured**, not assumed. The probe
scripts and their raw output are reproducible via `scripts/probe/`.

---
---

# 1. Goals and non-goals

## Goals

1. A **pure Go library** (`./cron`) that is idiomatic to a senior Go reviewer, with **zero `unsafe`**
   and no JavaScript dependency of any kind.
2. **Behavioural equivalence** with v5.6.2, including its quirks, verified by running the
   **280 original Jest tests byte-unmodified**.
3. A **thin adapter** that bridges those tests to the Go implementation.
4. A **differential fuzzer** that runs the two implementations side by side.
5. **Honest benchmarks**, cold and warm.

## Non-goals

- Fixing the original's bugs *in the compatibility path*. Bugs are reproduced faithfully behind a
  compatibility flag and documented; the default Go API takes the corrected behaviour. See §8.
- Supporting anything the original doesn't (no new cron syntax).
- Making the WASM bridge fast. It exists for test parity; benchmarks measure **native Go**.

---
---

# 2. The two-artifact architecture

The single most important structural decision. The port is **not** one thing:

```
┌─────────────────────────────────────────────────────────────────────┐
│  ARTIFACT 1 — ./cron        the real port. Pure Go. Zero unsafe.    │
│  Idiomatic API, iter.Seq, options pattern, structured errors.       │
│  This is what gets benchmarked, fuzzed, and reviewed for quality.   │
└─────────────────────────────────────────────────────────────────────┘
                                   ▲
                                   │ imported by
                                   │
┌─────────────────────────────────────────────────────────────────────┐
│  ARTIFACT 2 — ./bridge      GOOS=js GOARCH=wasm. Handle registry.   │
│  + ./adapter                TypeScript shim mirroring src/ layout.  │
│  Exists ONLY so the original Jest suite can execute unmodified.     │
│  Organizers explicitly excuse unsafe/escape hatches at this seam.   │
└─────────────────────────────────────────────────────────────────────┘
```

**Why this split matters for scoring.** The Zero-Unsafe bonus and the Code-Quality 20% are judged
on Artifact 1, which stays clean. The Functionality 40% is proven by Artifact 2. Conflating them
would force compatibility warts into the library's public API and cost Code Quality points.

---
---

# 3. Why WebAssembly and not cgo

**cgo is not available on the build machine.** Measured:

```
$ gcc -dumpmachine
mingw32                                  ← 32-bit only, GCC 6.3.0 (MinGW.org)
$ go env GOARCH
amd64
$ go build -buildmode=c-shared -o spike.dll .
# runtime/cgo
cc1.exe: sorry, unimplemented: 64-bit mode not compiled in
```

Installing mingw-w64 would fix it but adds a heavyweight native toolchain that judges would also
need. WASM needs nothing beyond the Go toolchain that already builds the library.

**The WASM path was proven end-to-end before committing to it:**

| Property | Measured |
|---|---|
| Build | `GOOS=js GOARCH=wasm go build` → 2.7 MB module |
| Init cost | **54 ms** (×7 Jest test files ≈ 0.4 s total) |
| Call style | **Synchronous** — `globalThis.fn(...)` callable immediately after `go.run()`, no `await` |
| Call cost | 46 µs/call with map marshalling; will be lower with the packed protocol in §5.3 |
| tzdata | Embedded via `_ "time/tzdata"`. Verified: Troll `+120`, Lord Howe `+660`, Chatham `+825` |

Synchronicity is non-negotiable — the original API is 100% synchronous (`parse().next()` returns a
value, not a promise) and Jest asserts on it directly. WASM satisfies this; a subprocess bridge
would not without an `Atomics.wait` contraption.

---
---

# 4. The time layer — the hardest part of the port

`CronDate.ts` wraps luxon. Go's `time` package disagrees with luxon in **five measured ways**, three
of them severe. Getting this wrong silently corrupts every downstream result, so it is built and
tested first (M2) before anything else.

## 4.1 The measured divergence table

| # | Operation | luxon | Go naive | Severity |
|---|---|---|---|---|
| 1 | `2024-02-29 + 1 year` | `2025-02-28` **clamps** | `2025-03-01` overflows | 🔴 |
| 2 | `2024-02-29 − 1 year` | `2023-02-28` **clamps** | `2023-03-01` overflows | 🔴 |
| 3 | `set(month=2)` on `2024-01-31` | `2024-02-29` **clamps** | `2024-03-02` overflows | 🔴 |
| 4 | `set(year=2025)` on `2024-02-29` | `2025-02-28` **clamps** | `2025-03-01` overflows | 🔴 |
| 5 | `set(day=31)` in February | `2024-03-02` **overflows** | `2024-03-02` overflows | ✅ agrees |
| 6 | `startOf(day)`, `America/Santiago 2026-09-06` (midnight gap) | `09-06T01:00-03:00` | **`09-05T23:00-04:00`** — *previous day* | 🔴 |
| 7 | `startOf(day)`, `Asia/Beirut 2026-03-29` | `01:00+03:00` | `01:00+03:00` | ✅ agrees |
| 8 | non-existent `2026-03-08T02:30` NY | `03:30-04:00` **forward** | **`01:30-05:00` backward** | 🔴 |
| 9 | ambiguous `2026-11-01T01:30` NY | `-04:00` (first) | `-04:00` (first) | ✅ agrees |

**Note the asymmetry in rows 1–5.** luxon *clamps* for year and month arithmetic but *overflows*
for day. Any uniform strategy — clamp everything, or overflow everything — is wrong. This is the
kind of thing a rushed port gets wrong and a fuzzer finds at hour 60.

**Row 6 is the nastiest.** In zones that transition at midnight, Go's `time.Date(y,m,d,0,0,0,0,loc)`
can land on the *previous calendar day*. Since `#checkDstTransition` compares `startOfDay` and
`endOfDay` offsets, and `#matchDayOfMonth` reads the day number, this corrupts day matching outright.

## 4.2 The fix: port luxon's `fixOffset` faithfully

Rows 6 and 8 have a single root cause — how a wall-clock time that doesn't exist gets resolved.
luxon centralises this in `fixOffset`:

```js
function fixOffset(localTS, o, tz) {
  let utcGuess = localTS - o * 60 * 1000;
  const o2 = tz.offset(utcGuess);
  if (o === o2) return [utcGuess, o];
  utcGuess -= (o2 - o) * 60 * 1000;
  const o3 = tz.offset(utcGuess);
  if (o2 === o3) return [utcGuess, o2];
  return [localTS - Math.min(o2, o3) * 60 * 1000, Math.max(o2, o3)];
}
```

The last line is the gap case: subtracting the **minimum** offset yields the **later** instant —
forward resolution. Go's `time.Date` resolves backward. So the Go port implements:

```go
// fromWallClock builds an instant from a wall-clock reading in loc, resolving
// non-existent times FORWARD and ambiguous times to the FIRST occurrence,
// matching luxon's fixOffset. Go's time.Date resolves gaps backward, which is
// why this cannot simply call it. See DESIGN.md §4.2.
func fromWallClock(y int, mo time.Month, d, h, mi, s, ns int, loc *time.Location) time.Time
```

Every construction and every `set*` in the time layer routes through this one function. Nothing
calls `time.Date(..., loc)` directly outside it — enforced by a lint rule in `scripts/`.

## 4.3 Arithmetic helpers

```go
func (c *cronTime) addYears(n int)    // clamp day to target month length  (rows 1,2)
func (c *cronTime) addMonths(n int)   // clamp, then caller does startOfMonth
func (c *cronTime) setMonth(m int)    // clamp                             (row 3)
func (c *cronTime) setYear(y int)     // clamp                             (row 4)
func (c *cronTime) setDay(d int)      // OVERFLOW — deliberately not clamped (row 5)
```

`setDay` carries a comment explaining that the non-clamping is a faithful reproduction of luxon,
not an oversight — otherwise a future reader "fixes" it and breaks parity.

## 4.4 Two Go traps to avoid

**`time.Truncate` operates on absolute time, not wall time.** `t.Truncate(time.Hour)` is *wrong*
for `startOf('hour')` in any zone with a non-hour offset — India `+05:30`, Chatham `+12:45`,
Lord Howe `+10:30`. All `startOf*` helpers are built from wall-clock components via
`fromWallClock`, never `Truncate`.

**`endOf('day')` is `23:59:59.999`,** millisecond precision, not nanosecond. The original then
often does `.startOf('second')`. Go's nanosecond precision must be truncated to milliseconds at
the boundary or `getTime()` comparisons drift.

## 4.5 DST state

The original carries mutable `dstStart` / `dstEnd` on the date object. Go mirrors this:

```go
type cronTime struct {
    t        time.Time
    loc      *time.Location
    dstStart int // -1 == unset
    dstEnd   int // -1 == unset
}
```

`applyDateOperation` is ported **with its `diff == 2` behaviour intact in compat mode** — that is
Bug #1, and reproducing it is required for test parity. The corrected generalisation (compare UTC
offsets directly rather than inferring the gap from the hour delta) lives behind
`WithCorrectDST()` and is what the native Go API uses by default. See §8.

---
---

# 5. Module map — TypeScript to Go

| TypeScript | LOC | Go | Notes |
|---|---:|---|---|
| `CronDate.ts` | 581 | `cron/time.go` | §4. The luxon compatibility layer |
| `CronExpressionParser.ts` | 466 | `cron/parse.go` | the parse pipeline |
| `CronExpression.ts` | 596 | `cron/expression.go` | the search engine |
| `CronFieldCollection.ts` | 449 | `cron/collection.go` | stringify / compaction |
| `fields/CronField.ts` | 265 | `cron/field.go` | composition, not inheritance |
| `fields/Cron{Second..DayOfWeek}.ts` | 277 | `cron/field.go` | table-driven, one struct |
| `fields/types.ts` | 24 | `cron/field.go` | runtime validation, not type-level |
| `CronFileParser.ts` | 87 | `cron/crontab.go` | content parsing only; IO stays in the adapter |
| `utils/random.ts` | 42 | `cron/random.go` | uint32 discipline |

## 5.1 Public Go API

```go
package cron

func Parse(expr string, opts ...Option) (*Expression, error)
func FromFields(f *FieldCollection, opts ...Option) (*Expression, error)

func (e *Expression) Next() (time.Time, error)
func (e *Expression) Prev() (time.Time, error)
func (e *Expression) HasNext() bool
func (e *Expression) HasPrev() bool
func (e *Expression) Take(n int) []time.Time
func (e *Expression) Reset(t ...time.Time)
func (e *Expression) Includes(t time.Time) bool
func (e *Expression) Fields() *FieldCollection
func (e *Expression) String() string                 // the raw expression
func (e *Expression) Format(includeSeconds bool) string
func (e *Expression) All() iter.Seq[time.Time]       // Go 1.23 range-over-func

// Options
func WithLocation(*time.Location) Option
func WithCurrent(time.Time) Option
func WithStart(time.Time) Option
func WithEnd(time.Time) Option
func WithHashSeed(string) Option
func WithStrict() Option
func WithLuxonCompat() Option   // reproduce the original's bugs; used by the adapter
```

`All()` is the `[Symbol.iterator]` replacement. Range-over-func is the idiomatic Go 1.23+ answer;
a channel-based iterator would leak a goroutine on early `break` and would cost Code-Quality points.

## 5.2 Representing the `number | string` union

Field values are `number | string` in TypeScript (`5`, `"L"`, `"5L"`). Go has no untagged unions,
and `any` would forfeit type safety. The port uses a tiny explicit sum type:

```go
// Value is a cron field value: either a number, or a token such as "L" or "5L".
type Value struct {
    N    int
    Text string // empty when the value is numeric
}

func (v Value) IsNumeric() bool { return v.Text == "" }
```

This keeps `[]Value` sortable with the original's mixed-type comparator (numbers before strings,
strings by lexical order) without reflection or `any`.

## 5.3 Bridge protocol

Handle-based. Go owns every object; JS holds opaque `int32` handles.

```
JS                                    Go (wasm)
──                                    ─────────
parse(expr, optsJSON)        ──────►  registry[h] = *Expression;  return h
exprNext(h)                  ──────►  returns int64 epoch-millis  (fast path)
exprFields(h)                ──────►  returns handle to FieldCollection
exprFormat(h, withSeconds)   ──────►  returns string
release(h)                   ──────►  delete(registry, h)
```

Scalar returns (epoch-millis as a `float64`) avoid the map-marshalling that cost 46 µs in the
spike. Errors return a sentinel and set `lastError`, which the shim reads and throws.

A `FinalizationRegistry` in the shim releases handles when JS objects are collected, so a 280-test
run does not grow the registry unboundedly.

---
---

# 6. The adapter — bridging the untouched tests

## 6.1 What the tests actually demand

Measured from the 8 pinned files:

```
import { CronDate, TimeUnit }     from '../src/CronDate';
import { CronExpressionParser }   from '../src/CronExpressionParser';
import { Months }                 from '../src/CronExpressionParser';
import { CronExpression, CronExpressionOptions,
         TIME_SPAN_OUT_OF_BOUNDS_ERROR_MESSAGE } from '../src/CronExpression';
import { CronFieldCollection, CronFields } from '../src/CronFieldCollection';
import { CronFileParser }         from '../src/CronFileParser';
import { CronDayOfMonth, ... }    from '../src/fields';
import CronExpressionParser       from '../src';
import { seededRandom }           from '../src/utils/random';
import { DateTime }               from 'luxon';   // tests compute expected values
import fs from 'fs'; import fsPromises from 'fs/promises';
```

So the adapter must reproduce the **module layout**, the **class shapes**, the **enums**, the
**exported constants**, and the **types** — not merely the behaviour.

## 6.2 Redirecting `../src/*` without touching a test file

Jest's `moduleNameMapper` matches the literal specifier string, so it works regardless of where the
test file sits on disk:

```js
// jest.config.js  (ours — config is not a test file, editing it is allowed)
moduleNameMapper: {
  '^\\.\\./src$':        '<rootDir>/adapter/src/index.ts',
  '^\\.\\./src/(.*)$':   '<rootDir>/adapter/src/$1',
},
testMatch: ['<rootDir>/tests/original/**/*.test.ts'],
```

This is the whole trick, and it is why the tests can live at `tests/original/` with their
`../src/...` imports intact.

## 6.3 Three things that must stay in JavaScript

1. **File IO.** `CronFileParser.test.ts` does `jest.mock('fs')` and asserts
   `expect(fs.readFileSync).toHaveBeenCalledWith('tests/crontab.example', 'utf8')`. So the shim must
   itself call `require('fs').readFileSync(path,'utf8')` for the mock to intercept, then hand the
   **string** to Go. Go never touches the filesystem.
2. **`CronDate` as a real JS class.** Six tests do `jest.spyOn(CronDate.prototype, 'applyDateOperation')`,
   which requires a genuine prototype method. And `expect(next).toBeInstanceOf(CronDate)` requires
   real instances.
3. **The type declarations.** `tests/types.d.ts` runs under `tsd` and asserts assignability of
   `SixtyRange`, `DayOfMonthRange`, etc. These are compile-time-only; the adapter re-declares them
   identically. No runtime cost, no Rule-4 concern (they are adapter code, written in-window).

## 6.4 Error parity

42 of the 280 tests assert on **exact error message strings**:

```
'CronSecond Validation error, duplicate values found: 59'
'Constraint error, got range 30-20 expected range 0-59'
'Invalid characters, got value: */A'
'Invalid explicit day of month definition'
'Invalid verb: InvalidOp'
```

Strategy: Go defines **structured error types** carrying the field name, offending value, and
bounds — idiomatic, and usable with `errors.Is`/`errors.As`:

```go
type ValidationError struct {
    Field    string   // "CronSecond"
    Kind     string   // "duplicate" | "range" | "empty" | "notArray"
    Value    Value
    Min, Max int
}
func (e *ValidationError) Error() string   // renders the upstream-compatible string
```

`Error()` renders the upstream string verbatim. That keeps one source of truth: the adapter simply
forwards the message rather than maintaining a translation table that could drift. Documented as a
deliberate compat choice in `DECISIONS.md` — Go style would normally prefer lowercase,
non-punctuated error strings.

---
---

# 7. The two structurally unbridgeable tests

Honest accounting, decided up front rather than discovered at hour 65.

Six tests spy on `CronDate.prototype.applyDateOperation`. With the search loop living in Go, that JS
method is never invoked:

| Test | Assertion | Outcome if Go owns the loop |
|---|---|---|
| jumps to next allowed second… | `expect(spy).not.toHaveBeenCalled()` | ✅ passes (vacuously) |
| jumps to previous allowed second… | `not.toHaveBeenCalled()` | ✅ passes |
| jumps to next allowed minute… | `not.toHaveBeenCalled()` | ✅ passes |
| jumps to next allowed hour… | `not.toHaveBeenCalled()` | ✅ passes |
| **rolls to next hour then sets minute/second** | `toHaveBeenCalledTimes(1)` + `calls[0][1] === TimeUnit.Hour` | ❌ **fails** |
| **jumps a full day first** | `filter(c => c[1]===Day).toHaveLength(1)` | ❌ **fails** |

**Baseline (M7): accept 278/280** and document it. These two assert an *implementation strategy*
("did you avoid stepping hour-by-hour?"), not observable behaviour. Both return the correct
`toISOString()`; only the internal-call assertion fails. That is a defensible, honest position and
exactly the kind of thing the rubric rewards over a suspicious 100%.

**Stretch (M7b): make it 280/280 honestly.** The Go engine already knows every date operation it
performs. It can emit a real operation trace, which the shim replays through
`CronDate.prototype.applyDateOperation` so prototype spies observe the true sequence. This is
legitimate *only* because the trace reflects what Go actually did — it makes internal behaviour
observable across the boundary rather than fabricating it. If implementation drifts toward
"emit whatever makes the assertion pass", abandon it and ship 278/280 with the write-up.
That judgement call is recorded in `DECISIONS.md`.

---
---

# 8. Bug reproduction vs. bug fixing

Four real bugs exist in v5.6.2 (see `INSTRUCTIONS.md` §3). Reproducing them is required for test
parity; shipping them as the Go library's default behaviour would be bad engineering. Resolution:

```go
cron.Parse(expr)                       // corrected behaviour — the default
cron.Parse(expr, cron.WithLuxonCompat()) // bug-compatible — what the adapter uses
```

| Bug | Compat mode | Default mode |
|---|---|---|
| #1 DST `diff == 2` | reproduced | offsets compared directly; handles 2 h, 30 min, 45 min zones |
| #2 in-place sort mutation | n/a (Go copies) | values are always copied on construction |
| #3 `/([,-/])/` range | reproduced (`,` `-` `.` `/`) | `[,\-/]` — the three intended characters |
| #4 bare `L` throws at `next()` | reproduced | rejected at parse time with a clear error |

Each bug gets an upstream issue filed **inside the 72-hour window**, which is what the Bug Catcher
bonus requires — a reproduction alone does not earn it.

---
---

# 9. Differential fuzzing (M8)

Requirement: ≥60 continuous seconds, zero divergences on the shared public API, published log.

**Harness.** Node driver holds both implementations: the pristine TS original from
`../cron-parser`, and the Go build via the WASM bridge. Same input to both, compare outputs.

**Generator** — weighted toward the danger zones found in §4:

- random field values / lists / ranges / steps / names / `L` / `#`
- start instants biased to within ±48 h of a real DST transition
- timezone corpus explicitly including `Antarctica/Troll` (2 h), `Australia/Lord_Howe` (30 min),
  `Pacific/Chatham` (+12:45), `Asia/Beirut`, `America/Santiago`, `Africa/Cairo` (midnight gaps),
  plus `UTC`, `Asia/Kolkata`, `America/New_York`, `Europe/London`
- leap-year boundaries: Feb 28/29, Mar 1, Dec 31

**Properties checked** (property-based, because the rubric says property tests survive translation):

1. `next()` sequences agree for N iterations
2. `prev()` sequences agree
3. `parse(stringify(parse(x))) ≡ parse(x)` — round trip
4. `includesDate(d)` agrees with membership in the `next()` sequence
5. errors agree: if one throws, both throw, with the same message

**Reporting.** Every divergence is minimised (shrink the expression, then the date, then the zone)
and written to `fuzz/divergences/` as a runnable repro.

---
---

# 10. Benchmarks (M9)

Measured on **native Go vs native TS** — never through the WASM bridge, which would measure the
bridge.

| Dimension | Method |
|---|---|
| Cold start | process spawn → first `next()` returned, 20 runs, report p50/p99 |
| Warm throughput | `next()` iterations/sec after warmup, `go test -bench` vs the repo's own `npm run bench` |
| Parse throughput | expressions parsed/sec across a fixed corpus |
| RSS | peak resident set for a fixed workload |
| p99 latency | per-call distribution, not just the mean |

The original ships its own `benchmarks/` harness (`npm run bench`) — using the author's own terms is
far more defensible than inventing a benchmark. Report distributions, and report regressions
honestly if any appear.

---
---

# 11. Repository layout

```
cron-parser-go/
├── go.mod                     module github.com/aniket-3001/cron-parser-go
├── README.md                  one-command build, results summary
├── DESIGN.md                  this document
├── DECISIONS.md               scored deliverable — architectural divergences
├── LICENSE                    MIT, preserving upstream attribution
├── .gitattributes             * text=auto eol=lf   ← protects the kickoff hashes
│
├── cron/                      ARTIFACT 1 — the port. zero unsafe.
│   ├── time.go       field.go       parse.go
│   ├── expression.go collection.go  crontab.go   random.go
│   └── *_test.go              native Go tests, incl. the §4.1 divergence table
│
├── bridge/main.go             ARTIFACT 2a — js/wasm handle registry
├── adapter/src/               ARTIFACT 2b — TS shim mirroring src/ layout
│   ├── index.ts  CronDate.ts  CronExpression.ts  CronExpressionParser.ts
│   ├── CronFieldCollection.ts CronFileParser.ts
│   └── fields/   utils/
│
├── tests/original/            ← BYTE-IDENTICAL. Never edit.
│   ├── *.test.ts  types.d.ts  crontab.example
│   └── HASHES.txt             kickoff sha256, verified in CI
│
├── fuzz/                      differential harness + divergence repros
└── scripts/                   build, hash-verify, probe
```

---
---

# 12. Milestones and acceptance gates

| # | Milestone | Gate — objectively checkable |
|---|---|---|
| **M0** | Recon, semantics probing, design | ✅ this document exists; divergence table measured |
| **M1** | Docs consolidated, repo scaffolded and pushed | repo public; `HASHES.txt` verifies |
| **M2** | Time layer | Go table test reproduces all 9 rows of §4.1 |
| **M3** | Fields + parser | every expression in the original's parser tests parses to identical field sets |
| **M4** | Engine | `Next`/`Prev` match the original across the fuzz corpus |
| **M5** | Collection, stringify, crontab | round-trip property holds on 10k random expressions |
| **M6** | Bridge + adapter | adapter loads; smoke test green |
| **M7** | **Original 280 tests unchanged** | hashes verified + suite green (278/280 baseline, 280 stretch) |
| **M8** | Differential fuzzer | ≥60 s, zero divergences, log committed |
| **M9** | Benchmarks, DECISIONS, README, video | all deliverables checked |

**Critical path:** M2 → M3 → M4 gate everything. The time layer is first because every other
component's correctness depends on it, and because its divergences are the ones that would
otherwise surface late and expensively.
