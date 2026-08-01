# DECISIONS

Architectural divergences between `harrisiirak/cron-parser` v5.6.2 (TypeScript) and this Go port,
with the reasoning behind each.

Every entry here is implemented and verified. Performance and behaviour claims are measured, not
estimated; the probes that produced them live in `scripts/probe/`, `fuzz/` and `bench/`.

Where a decision reversed an earlier one, the reversal is recorded rather than the conclusion
alone, because the reasoning that changed is the useful part.

---

## D1 — Type-level integer ranges become runtime validation 🟢

**Original.** `fields/types.ts` builds literal unions at compile time through recursive types:

```ts
type RangeFrom<LENGTH extends number, ACC extends unknown[] = []> =
  ACC['length'] extends LENGTH ? ACC : RangeFrom<LENGTH, [...ACC, 1]>;
type SixtyRange = IntRange<RangeFrom<0>, 59>;   // 0 | 1 | ... | 59
```

`new CronMinute([99])` is rejected by `tsc` before the program runs.

**Port.** Go's type system cannot express "an integer between 0 and 59". Validation moves to
construction time, returning a `*ValidationError`.

**Why.** The alternatives are worse. Generating 60 named constants buys no safety, because Go would
still accept any `int` where one is expected. A `type Minute uint8` distinct type prevents mixing
units but not out-of-range values. Runtime validation with structured errors is the honest,
idiomatic answer.

**Cost.** A class of errors moves from compile time to run time. Mitigated by validating at every
constructor boundary and by the fuzzer.

---

## D2 — Inheritance becomes composition 🟢

**Original.** `abstract class CronField` with six subclasses, each overriding `static min`,
`static max`, `static chars`, `static validChars`. Behaviour is selected by `this.constructor`
lookups at runtime.

**Port.** One `Field` struct holding a pointer to an immutable `fieldSpec` descriptor:

```go
type fieldSpec struct {
    name       string   // "CronSecond" — used in error messages
    min, max   int
    chars      []rune
    validChars *regexp.Regexp
}
```

**Why.** Go has embedding but no method overriding, so the TypeScript pattern has no direct
translation. Six near-identical structs with a shared interface would be 6× the boilerplate for
zero benefit — the subclasses differ only in *data*, never in behaviour. A table-driven descriptor
expresses that honestly and collapses ~277 lines of subclass boilerplate to a 6-row table.

**Consequence.** `field.Spec().Name` replaces `this.constructor.name` in error messages, preserving
strings like `CronSecond Validation error, ...` exactly.

---

## D3 — `number | string` becomes an explicit sum type 🟢

**Original.** Field values are `(number | string)[]` — `5`, `"L"`, `"5L"` coexist in one array.

**Port.**

```go
type Value struct {
    N    int
    Text string // empty when numeric
}
func (v Value) IsNumeric() bool { return v.Text == "" }
```

**Why.** `any`/`interface{}` would forfeit type safety and force type switches at every use — and
would disqualify the port from the Zero-`any` spirit of the bonus. Two parallel slices
(`[]int` + `[]string`) would lose ordering, which matters because the original's comparator sorts
numbers before strings. A 2-field struct is 16 bytes, needs no allocation, and keeps one ordered
slice.

---

## D4 — Defensive copy instead of in-place sort 🟢

**Original.** `CronField.ts:99`

```ts
this.#values = values.sort(CronField.sorter);
```

`Array.prototype.sort` sorts in place, so constructing a field **mutates the caller's array**:

```js
const mine = [30, 10, 20];
new CronMinute(mine);
mine; // [10, 20, 30]  ← rewritten
```

Reproduced against v5.6.2.

**Port.** Values are copied before sorting. Go slices share backing arrays, so a naive
`sort.Slice(values, ...)` would reproduce the bug exactly — this is a case where the *easy* Go
translation is also the buggy one.

**Why not bug-compatible.** No test depends on the mutation; it is invisible to the suite. Silent
caller-data corruption is not behaviour worth preserving. Filed upstream.

---

## D5 — `[Symbol.iterator]` becomes `iter.Seq[time.Time]` 🟢

**Original.** Implements the ES6 iterator protocol, enabling `for (const d of interval)`.

**Port.** `func (e *Expression) All() iter.Seq[time.Time]`, Go 1.23 range-over-func:

```go
for t := range expr.All() { ... }
```

**Why.** The obvious alternatives are worse. A channel-based iterator leaks a goroutine whenever the
consumer `break`s early — a real bug, not a style question. A stateful `Next() (time.Time, bool)`
iterator works but reads as Java. `iter.Seq` is the idiomatic answer since 1.23, composes with the
`slices`/`maps` iterator helpers, and handles early termination correctly by construction.

**Verified.** `iter.Seq[int]` compiles and iterates on the pinned toolchain (Go 1.26.5).

---

## D6 — Structured errors that render upstream-compatible strings 🟢

**Original.** Throws `Error` with formatted messages. 42 of the 280 tests assert on the exact text.

**Port.** Typed errors carrying structured fields, whose `Error()` renders the upstream string:

```go
type ValidationError struct {
    Field    string
    Kind     string
    Value    Value
    Min, Max int
}
```

**Why.** Two goals conflict: Go callers want `errors.As` and structured data; the test suite wants
byte-identical strings. Rendering the compatible string from `Error()` satisfies both with **one**
source of truth. The rejected alternative — idiomatic Go messages plus a translation table in the
adapter — creates two places to edit and will drift.

**Acknowledged deviation.** Go convention says error strings should be lowercase and unpunctuated.
`CronSecond Validation error, got value 61 expected range 0-59` violates that deliberately, in
service of a stated project goal. Flagged here so a reviewer sees it as a choice, not an oversight.

---

## D7 — luxon's asymmetric date arithmetic, reproduced exactly 🟢

**The finding.** luxon *clamps* for year and month arithmetic but *overflows* for day. Measured:

| Operation | luxon | Go naive |
|---|---|---|
| `2024-02-29 + 1 year` | `2025-02-28` clamp | `2025-03-01` overflow |
| `set(month=2)` on `2024-01-31` | `2024-02-29` clamp | `2024-03-02` overflow |
| `set(day=31)` in February | `2024-03-02` **overflow** | `2024-03-02` overflow |

**Port.** Hand-written arithmetic: clamping helpers for year/month, deliberately non-clamping
`setDay`.

**Why this entry exists.** Any *uniform* strategy is wrong. A port that clamps everything breaks
`setDay`; one that uses `AddDate` throughout breaks year and month arithmetic. `setDay` carries an
explicit comment saying the non-clamping is intentional, so a future reader does not "fix" it.

---

## D8 — DST gaps resolve forward, matching luxon, not Go 🟢

**The finding.** For a wall-clock time that does not exist, luxon and Go move in **opposite
directions**:

| Case | luxon | Go `time.Date` |
|---|---|---|
| `2026-03-08T02:30` America/New_York | `03:30-04:00` forward | `01:30-05:00` **backward** |
| `startOf(day)` America/Santiago `2026-09-06` | `09-06T01:00-03:00` | `09-05T23:00-04:00` **previous day** |

The Santiago case is the dangerous one: `time.Date(y,m,d,0,0,0,0,loc)` can land on the *previous
calendar day* in zones that transition at midnight, corrupting day-of-month matching.

**Port.** luxon's `fixOffset` is ported faithfully into `fromWallClock`, the single chokepoint for
every wall-clock construction. Nothing else in the package calls `time.Date` with a non-UTC
location; a lint check enforces it.

**Why.** Concentrating the divergence in one 20-line function makes it testable and reviewable.
Scattering ad-hoc corrections across `setHours`, `setDay`, `startOfDay` would be unauditable.

---

## D9 — `time.Truncate` is never used for `startOf` 🟢

**Original.** `startOf('hour')`, `startOf('day')` operate on **wall-clock** components.

**Port.** All `startOf*` helpers rebuild the instant from components via `fromWallClock`.

**Why.** Go's `Time.Truncate` rounds against absolute time since the zero instant, not local time.
In any zone with a non-hour offset it produces the wrong result — India `+05:30`,
Australia/Lord_Howe `+10:30`, Pacific/Chatham `+12:45`. This is a trap precisely because
`t.Truncate(time.Hour)` looks like the obvious translation and is correct in UTC, where most
testing happens.

---

## D10 — The library is split from its test bridge 🟢

**Port.** Two artifacts: `./cron` (pure Go, zero `unsafe`, no JS) and `./bridge` + `./adapter`
(js/wasm module plus a TypeScript shim), the latter existing only to run the original Jest suite.

**Why.** Compatibility requirements and API quality pull in opposite directions. Handle registries,
opaque `int32`s and upstream-shaped error strings belong at a seam, not in the library a Go
developer imports. Organizers explicitly excuse escape hatches at the FFI boundary, so isolating
them there protects both the Zero-Unsafe bonus and the Code-Quality score.

---

## D11 — WebAssembly, not cgo, for the bridge 🟢

**Decision.** `GOOS=js GOARCH=wasm`, loaded into Node via the `wasm_exec.js` that ships with Go.

**Why.** cgo is unavailable on the build machine — `gcc -dumpmachine` reports `mingw32` (32-bit
GCC 6.3.0) against a windows/amd64 Go, and `-buildmode=c-shared` fails with
`cc1.exe: sorry, unimplemented: 64-bit mode not compiled in`. Installing mingw-w64 would add a
native toolchain that judges would also need; WASM needs only the Go toolchain already required.

**Measured before committing.** 2.7 MB module, 54 ms init, **synchronous** calls (mandatory — the
original API is entirely synchronous and Jest asserts on return values directly), 46 µs/call with
map marshalling, and correct embedded tzdata (Troll `+120`, Lord Howe `+660`, Chatham `+825`).

**Rejected alternative.** A subprocess speaking JSON over stdio needs `execFileSync` per call
(far too slow) or an `Atomics.wait` sync-over-async contraption (complex and fragile).

---

## D12 — File IO stays in JavaScript 🟢

**Original.** `CronFileParser` calls `require('fs').readFileSync` internally.

**Port.** Go parses crontab **content**; the adapter performs the read.

**Why.** `CronFileParser.test.ts` does `jest.mock('fs')` and asserts
`expect(fs.readFileSync).toHaveBeenCalledWith('tests/crontab.example', 'utf8')`. Only a real call
through the mocked module satisfies that. It is also better design: a parser that takes content
rather than a path is more testable, and the Go API exposes both
`ParseCrontab(content string)` and a convenience `ParseCrontabFile(path string)`.

---

## D13 — The original's bugs are reproduced, not corrected 🟢

**Decision.** Where the original is wrong, the port is wrong in the same way. There is no mode
that behaves better.

| Bug | Reproduced |
|---|---|
| DST compensation keyed on a wall-clock delta of exactly 2, so a two-hour gap goes unrecorded | yes |
| `stringify()` output can describe a different schedule from its input | yes |
| A duplicated `0` suppresses the duplicate check and masks every later duplicate | yes |
| Bare `L` in day-of-week parses and then fails during iteration | yes |
| `[,-/]` matches `.` as well as the three intended characters | yes |
| Repeated zeros make rendering fail with an internal error | yes |

**Why, when the plan said otherwise.** The design called for a `WithLuxonCompat()` option:
corrected behaviour by default, the original's behaviour on request. It was not built, and the
reasoning is worth recording rather than quietly dropping.

A second behavioural mode doubles the surface every equivalence check has to cover. The
differential fuzzer compares one implementation against one other; with two modes it would have
to compare two, and whichever mode the original suite does not exercise would rot. Behavioural
equivalence is worth 30% of the score and the 40% criterion is the original suite passing. Both
reward one faithful implementation over two half-checked ones.

There is a plainer argument too. Someone reaching for this library wants a drop-in replacement.
Scheduling differently from the original, even more correctly, is the surprising outcome — and
silent divergence is worse than a documented bug. Each reproduced bug is pinned by a test naming
its report in `upstream-issues/`, so the behaviour is deliberate and visible rather than inherited
by accident.

**What would change this.** If the upstream reports are accepted and fixed, the port should follow
the fix rather than keep the bug. The pinning tests are where that work would start.

---

## D14 — The engine records its date operations so the original's spies can see them 🟢

**The situation.** Six tests do `jest.spyOn(CronDate.prototype, 'applyDateOperation')`. With the
search loop in Go, that JS method is never called. Four assert `not.toHaveBeenCalled()` and pass
vacuously; **two** assert positive call counts and fail:

- `expect(spy).toHaveBeenCalledTimes(1)` with `calls[0][1] === TimeUnit.Hour`
- `expect(spy.mock.calls.filter(c => c[1] === TimeUnit.Day)).toHaveLength(1)`

Both tests' *observable* assertions — the returned `toISOString()` — pass.

**Decision.** The engine records the operations it performs, and the adapter replays that recording
through `CronDate.prototype.applyDateOperation`, so the spies observe the real sequence. The suite
passes **280/280**.

**Why this is a recording and not theatre.** Every date operation the search makes goes through one
method, `Expression.applyOp`, which appends to the log before delegating. What the spy sees is
therefore what the engine did, not what the test hoped for. The property that matters is whether
the test still fails when the implementation regresses, and it does: replacing the hour fast path
with a loop that steps one hour at a time makes exactly these assertions fail —

```
● CronExpression › iteration jump flows › when past the last scheduled hour for the day, jumps a full day first
● CronExpression › iteration jump flows › jumps to next allowed hour without stepping via applyDateOperation()
Tests: 2 failed, 67 passed, 69 total
```

Note that the second of those passed *vacuously* before the recording existed, because a spy that
is never called trivially satisfies `not.toHaveBeenCalled()`. Replaying the log gave four such
tests their diagnostic power back rather than taking any away.

**Cost.** One boolean and one slice on `Expression`, off by default, and a nil-guarded append in a
single method. No global state, and nothing else in the library reads them. `TestOperationTrace`
pins the recorded sequences directly in Go, so the behaviour is checked without the bridge.

**What was decided earlier, and why it changed.** The plan had been to ship 278/280 and document
these two as structurally unbridgeable, on the grounds that instrumenting the library for two tests
was a poor trade. That reasoning assumed the recorder would have to be global state reachable from
`cronTime`. It does not: every call site is already a method on `Expression`, so the recorder lives
on the object that owns the search. At roughly ten lines with no global state, the trade is
comfortably worth it for the strongest evidence the rubric asks for.

**The stop-rule, unchanged.** This is legitimate only while the log reflects what the engine did.
If it ever became necessary to adjust the log to satisfy an assertion, the right move would be to
revert to 278/280 and publish this write-up instead. A suspicious 280 is worth less than an honest
278.

---

## D15 — `W` is a phantom feature; the port does not resurrect it 🟢

**The finding.** `CronChars` is `'L' | 'W'`; `CronFieldCollection.compactField()` has an explicit
`if (item === 'L' || item === 'W')` branch; four tests exercise it. But `CronDayOfMonth.validChars`
is

```ts
/^[?,*\dLH/-]+$|^.*H\(\d+-\d+\)\/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H\/\d+.*$/
```

— **no `W`**. So `parse('0 0 15W * *')` and `parse('0 0 LW * *')` both throw
`Invalid characters, got value: 15W`. The stringify path can emit a value the parse path cannot
produce; `W` is reachable only by hand-constructing field objects.

**Port.** Reproduce exactly — `W` accepted in the compaction/stringify path, rejected by the parser.

**Why.** Four pinned tests depend on `compactField` handling `W`, so it cannot be dropped. Adding
`W` to the parser would be a *feature addition*, diverging from the reference and breaking
differential equivalence. The dead branch is preserved with a comment pointing here.

---

## D16 — Two test tracks, not one 🟢

**Decision.** Ship both the original suite through the adapter (Track A) *and* native Go tests
covering the same behaviour (Track B).

**Why.** The organizer Q&A confirmed that native 1:1 tests are an accepted substitute, penalised
only if the originals are edited — and Rule 2 has always left *additional* tests unrestricted.
Track A is worth more, but it depends entirely on the WASM bridge; a single integration failure at
M6/M7 would leave no equivalence evidence at all. Track B removes that single point of failure and
is the only evidence a reviewer can reproduce with nothing but `go test`.

**Why it's nearly free.** Track B is a by-product of M2–M5 — the table tests those milestones need
in order to be correct *are* Track B. The marginal cost is organising them, not writing them.

**Explicitly not a substitute.** If Track B is ever used to justify skipping Track A, this decision
has been misapplied. The original suite unchanged is the primary deliverable.

---

## D17 — `Format` returns an error 🟢

**Original.** `stringify()` throws for a field of repeated zeros, reaching an internal error its
own source marks as unreachable.

**Port.** `Format(includeSeconds bool) (string, error)` rather than returning a bare string.

**Why.** The failure is reachable from ordinary input, so swallowing it would diverge from the
original at exactly the point the original gives up. Returning an error is also the idiomatic Go
shape for an operation that can fail.

**Consequence.** `String()` keeps the `fmt.Stringer` signature and reports the empty string when
rendering fails, since a Stringer cannot return an error. That is documented on the method, and
callers who need to know are pointed at `Format`.

---

## D18 — Benchmarks batch on both sides 🟢

**The finding.** Go's clock on the measurement machine advances in steps of roughly 527
microseconds. One unit of benchmark work costs tens of microseconds, so timing operations
individually returned zero for every sample: a median of 0 ns beside a mean of 47 microseconds.

**Port.** Both harnesses time a batch of operations and report the mean cost across it, with the
batch size calibrated per pattern so a batch takes at least 10 ms.

**Why both, when only one needs it.** Node's timer is fine-grained enough to time individual
calls. Measuring one side per-operation and the other per-batch would compare two different
things while presenting them in one table. The honest option is to measure both the same way and
say what the number is: `bench/RESULTS.md` states that p99 is the 99th percentile of batch means,
not of individual calls.

**A second correction.** Calibration originally stopped at the first batch to exceed the target.
One scheduling stall was enough to lock in a batch sixteen operations long, whose every later
sample then read as zero. It now escalates well past the target and derives the batch size from a
measured per-operation cost.

---

## D19 — The fuzzer compares the port directly, not through the adapter 🟢

**Decision.** `fuzz/differential.js` reaches the port through the WebAssembly bridge rather than
through `adapter/src`.

**Why.** The adapter exists to make the original's TypeScript tests runnable and carries the
conversions that job needs. Routing the fuzzer through it would mean comparing the shim as much
as the port, and would add a layer of allocation to every call. What is under comparison is the Go
library's behaviour.

**What this leaves uncovered.** The adapter itself. That is covered instead by the 280 original
tests, which exercise nothing but the adapter's surface — so between the two, both layers are
checked, each by the harness suited to it.

---

## D20 — Deleting unreachable code rather than testing it 🟢

**Decision.** Where a branch translated from the original cannot be reached in Go, it is removed
and a comment explains why, instead of being kept and covered by a contrived test.

Removed on those grounds: the "values is not an array" error, unrepresentable when the signature
takes `[]Value`; a nil-location fallback both entry points already prevent; an `strconv.Atoi`
error on input already proven to be a sign followed by digits; and the `Unexpected range end`
guard, which cannot fire because a run ending at zero holds nothing but zeros and so trips the
step guard first.

**Why.** Coverage was a means of finding untested behaviour, not a target. Writing tests to reach
branches that cannot occur would have produced a number that looked the same while making the
code worse.

**The counter-example that kept this honest.** The wildcard override was removed on the same
grounds and it was a mistake: the parser never sets it, but the field constructors are public API
and the original's own tests set it directly. It is restored, with a test. Reachability arguments
are only as good as the entry points they consider.

---

## D21 — npm is the one-command build, not make 🟢

**Decision.** `npm run build` is the documented entry point. The `Makefile` remains for anyone who
prefers it.

**Why.** The rule is that one command produces a runnable artifact, and `make` is absent from a
stock Windows install — it was absent from the machine this was built on. Node is already required
to run the original suite, so driving the build through it adds no dependency.

**What writing it caught.** Passing an absolute path to a command run through a shell breaks on
any checkout whose directory contains a space, which this one does. Build paths stay relative, and
the benchmark binary is launched without a shell so its absolute path needs no quoting.
