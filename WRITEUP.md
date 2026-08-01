# 41,088 divergences, and four tests that were lying to me

*Porting `cron-parser` from TypeScript to Go, and trying to prove it actually holds up.*

---

I spent this weekend porting [`harrisiirak/cron-parser`](https://github.com/harrisiirak/cron-parser)
(v5.6.2, ~1.5k stars, the library behind a lot of Node scheduling) from TypeScript to Go for
[Hackathon Raptors](https://github.com/hackathon-raptors)' Port Mortem.

The rule that makes the event interesting: **you must run the original test suite, unmodified, against
your port.** Not a rewrite. Not a "spiritually equivalent" suite. The original `.ts` files, byte for
byte, with their SHA-256 pinned at kickoff.

280 of 280 pass. But the number I actually care about is a different one, and getting to it meant
admitting that four of those tests had been passing without testing anything at all.

---

## The part nobody warns you about: two languages disagree about what a date *is*

`cron-parser` uses luxon. Go has `time`. Both are competent. They also disagree, silently, in ways
that only show up in specific timezones on specific days.

**Disagreement one — arithmetic is asymmetric.** luxon *clamps* for year and month, but *overflows*
for day:

| Operation | luxon | Naive Go |
|---|---|---|
| `2024-02-29 + 1 year` | `2025-02-28` (clamp) | `2025-03-01` (overflow) |
| `set(month = Feb)` on `2024-01-31` | `2024-02-29` (clamp) | `2024-03-02` (overflow) |
| `set(day = 31)` in February | `2024-03-02` (**overflow**) | `2024-03-02` (overflow) |

Read that third row again. luxon clamps for year and month, then *doesn't* clamp for day. Any
uniform strategy is wrong. Clamp everything and you break `setDay`; use `AddDate` everywhere and you
break year and month arithmetic.

**Disagreement two — nonexistent times resolve in opposite directions.** When a wall-clock time
falls inside a DST gap, luxon jumps *forward* and Go's `time.Date` falls *backward*:

| Case | luxon | Go `time.Date` |
|---|---|---|
| `2026-03-08T02:30` New York | `03:30 -04:00` (forward) | `01:30 -05:00` (**backward**) |
| `startOf(day)` Santiago `2026-09-06` | `09-06T01:00 -03:00` | `09-05T23:00 -04:00` (**previous day**) |

The Santiago row is the genuinely dangerous one. In zones that transition at midnight,
`time.Date(y, m, d, 0, 0, 0, 0, loc)` can land on the **previous calendar day** — which quietly
corrupts day-of-month matching, in a way that looks like a cron bug rather than a timezone bug.

So I ported luxon's `fixOffset` into a single function, `fromWallClock`, and made it the only place
in the package that constructs a wall-clock instant. Nothing else calls `time.Date` with a non-UTC
location. One 20-line function holds the entire divergence, which makes it reviewable instead of
scattered across a dozen setters.

---

## 41,088 → 143 → 0

I built a differential harness: same inputs to both implementations, compare every answer. First
full run against a corpus of date operations:

```
41,088 divergences
```

The cause was embarrassing and instructive. luxon's `fixOffset` needs a *provisional* offset to seed
its search. I was computing that seed from the instant being read, rather than from the instant
being operated on. Subtle, one-line, and wrong in exactly the cases that matter — near transitions.
Fixed by passing the provisional offset in as a parameter instead of deriving it locally.

```
143 divergences
```

Better. The remaining 143 all lived in **Australia/Lord Howe** — a zone with a **30-minute** DST
shift (+10:30 ↔ +11:00), which is unusual enough that it breaks assumptions you didn't know you had.

The bug: I had implemented `endOf(unit)` as

```
startOf(unit) → plus(1 unit) → minus(1ms)
```

luxon does

```
plus(1 unit) → startOf(unit) → minus(1ms)
```

In a one-hour-offset world those are the same. In a half-hour-offset world they are not.

```
0 divergences
```

That third number is the one this whole project is about — and I don't fully trust it, which is why
I kept going.

---

## The four tests that were lying to me

Here is the thing I actually want to tell you about, because I think it generalises well beyond
cron.

Six tests in the original suite do this:

```js
const spy = jest.spyOn(CronDate.prototype, 'applyDateOperation');
// ...
expect(spy).not.toHaveBeenCalled();
```

My search loop lives in Go. That JavaScript method is *never called*, by construction. So:

- **Two** tests assert positive call counts. They failed. Honest failures — I could see them.
- **Four** tests assert `not.toHaveBeenCalled()`. They **passed**. Vacuously. A spy that is never
  invoked trivially satisfies "was not invoked."

That's the trap. My suite said 278/280 and the two failures were visible, but four *additional*
tests had quietly degraded into no-ops. They were green. They were also worthless. A port can
convert a real assertion into a tautology without anything turning red.

The fix wasn't to stub the spy. It was to make the engine **record** the date operations it actually
performs — every one goes through a single method, `applyOp`, which appends to a log before
delegating — and have the adapter replay that recording through `CronDate.prototype`. What the spy
observes is therefore what the engine did.

The test that this is a recording and not theatre: **does it still fail when the implementation
regresses?** I replaced the hour fast path with a loop that steps one hour at a time:

```
● when past the last scheduled hour for the day, jumps a full day first
● jumps to next allowed hour without stepping via applyDateOperation()
Tests: 2 failed, 67 passed, 69 total
```

It fails. Good. And the four vacuous tests got their diagnostic power *back* — they now genuinely
assert that certain operations did not happen.

280/280, and four of them mean something again.

**If you take one thing from this post:** when you port, audit your negative assertions. `not.toHaveBeenCalled`,
`assert_not_called`, `verify(never())` — every one of them is a candidate for silently becoming a
tautology the moment the thing being spied on stops existing in that language.

---

## Three layers of proof, because one isn't enough

**1. The original suite, unmodified.** 280/280. Hashes pinned at kickoff
(`615075d3…2863de`) and re-verified by a script. `tests/original/` is touched by exactly one commit
in the entire history — the one that added it.

**2. Differential fuzzing.** Random expressions, 14 timezones, boundary-weighted start instants,
both implementations, every answer compared. Latest published run: **90 seconds, 2,903 expression
cases, 1,381 date cases, zero divergences.**

**3. CLI output diff.** The fuzzer compares APIs. This compares them as *programs* — same command
line, diffing stdout, stderr and exit status, including 16 rejection paths where the error text and
exit code both have to match. **124/124 identical.**

Two things had to be pinned before layer 3 meant anything. A Windows shell splits `*/15 9-17 * * 1-5`
on its spaces before the program ever sees it, so neither side runs through a shell. And Go writes a
zero UTC offset as `Z` where luxon writes `+00:00` — a formatting convention, not a behavioural
difference, so it's normalised in the wrapper and *documented as normalised* rather than quietly
smoothed over.

---

## How I know the fuzzer works: I broke things on purpose

A fuzzer that reports zero divergences is indistinguishable from a fuzzer that isn't looking. So I
sabotaged the port and checked it noticed.

**One sabotage defeated it three times.** I broke year arithmetic — a bug that only manifests on a
leap day. Three consecutive runs came back clean, and each failure taught me something about the
harness:

1. It only fuzzed expressions, never raw date operations. → Added date-op cases.
2. It picked *one* random operation per case. Chance of drawing both a leap-day instant and the
   broken operation: under 1 in 400. → Switched to sweeping the full operation surface per instant.
3. It sampled instants uniformly. Leap days are ~1 in 1,000. → Added a boundary-weighted corpus.

Only after all three did the sabotage die in seconds, with leap-day reproductions. Each of those
was a real blind spot that a green run had been hiding.

**And I found one more while auditing my own work for this write-up.** My fuzzer README opened by
claiming "every answer either can produce is compared." That was false. Five public methods —
`hasNext`, `hasPrev`, `take`, `reset`, `toString` — were never touched. They look like thin
wrappers. They aren't:

- `hasNext`/`hasPrev` run a search and then **restore the cursor** in a `finally`. A port could
  return the right boolean while silently consuming an occurrence.
- `take` has a separate backward branch for negative limits.
- `toString` falls through a JavaScript `||` on a falsy expression — the same falsy trap that had
  already bitten me once, when `seededRandom("")` took the random branch instead of the hashed one
  because `""` is falsy.

I added them, comparing **the state left behind**, not just the return value. Then sabotaged the
cursor restoration: **74 divergences in 25 seconds.** Comparing only the boolean would have caught
none of them.

Honest footnote, because the brief asks for honest numbers: **the original test suite catches that
same sabotage too**, with 7 failures. I added those probes because my harness was claiming a surface
it didn't cover — not because they caught something the tests had missed. Saying otherwise would
overstate them.

---

## The benchmark that printed "Infinityx"

My first throughput run reported that every operation took **zero nanoseconds**. 1,000 samples out
of 1,000.

Go's monotonic clock on this machine advances in steps of about **527 microseconds**. One parse-plus-
ten-occurrences costs tens of microseconds. So every individual measurement rounded to zero, and the
speedup calculation divided by it and printed `Infinityx`.

I was about ten minutes from having a very impressive slide.

The fix: batch both sides identically, calibrating batch size per pattern until a batch takes at
least 10ms. Node's timer is fine-grained enough that it didn't *need* batching — which is exactly
why both sides batch. Measuring the two differently would have made the comparison meaningless.

Real numbers, on the original library's own 15 benchmark patterns (not ones I picked):

| | Original (node v22) | Port (go1.26) | |
|---|---:|---:|---|
| Throughput, sum of medians | — | — | **23.5x** |
| Cold start p50 | 136.0 ms | 12.0 ms | **11.3x** |
| Memory after workload | 84.9 MB | 20.2 MB | *not comparable* |

Three caveats I'd rather state than have someone find:

- **The memory row is not a real ratio.** Node's RSS counts the whole process including V8; Go's
  `Sys` is what the runtime obtained from the OS and excludes the binary. Different measurements.
  I report both as each runtime reports them rather than inventing a single number.
- **23.5x is a sum of medians**, which flatters. Per-pattern it ranges from **2.87x** to 36x.
- **The 2.87x is on `* * * * * *`** (104.7µs → 36.5µs) — the most permissive expression in the
  corpus, where every candidate instant matches immediately. With almost no searching to do, the
  cost is dominated by constructing timezone-aware instants, which both sides pay in full. The port
  wins big precisely where there's a *search* to win; the headline number is real, but it is not
  evenly distributed.

Cold start is the number I'd actually defend. For a CLI invoked from a scheduler, 136ms → 12ms
matters more than throughput nobody is bottlenecked on.

---

## Six bugs in the original, two already merged, and one I deliberately didn't file

Differential testing finds bugs in the *reference*, not just the port. Seven reports went upstream
during the hackathon ([#419–#425](https://github.com/harrisiirak/cron-parser/issues)). Two were
fixed and merged the same day:

| | |
|---|---|
| [PR #426](https://github.com/harrisiirak/cron-parser/pull/426) | `val.match(/([,-/])/)` → `/([,\-/])/` — escape the hyphen so it's a literal, not a range |
| [PR #427](https://github.com/harrisiirak/cron-parser/pull/427) | `values.sort(...)` → `[...values].sort(...)` — a defensive copy |

Filed 07:12Z, merged by 14:16Z. That second one is the fix I'd already made in Go on day one, for
the same reason — Go slices share a backing array, so the naive translation inherits the bug. Nice
to have the maintainer arrive at the same line independently.

Two more (`#423`, `#424`) are now labelled `bug` and assigned to the maintainer.

**And one of the seven wasn't mine.** More on that below.

**`stringify()` isn't round-trip safe** — the worst one. `0 0 16 * 0-6` renders as `0 0 16 * *`.
The original fires **daily**; its own rendered output fires **monthly**. Cause: `isWildcard` is
derived from raw text (`*` or `?` literally), but `stringifyField` works from expanded values, so
`0-6` renders as `*`. And day-of-month/day-of-week switch between OR and AND on exactly that flag.
Rendering silently converts an OR into an AND.

**A DST gap wider than one hour skips an occurrence entirely.** The compensation checks
`currentHour - previousHour === 2`. Antarctica/Troll jumps **two hours** (00:00 → 03:00), so the
diff is 3 and the branch never fires:

```
America/New_York, 1h gap, "30 2 * * *"     Antarctica/Troll, 2h gap, "30 1 * * *"
  2026-03-07 02:30                           2026-03-28 01:30
  2026-03-08 03:30  ← shifted, still runs    2026-03-30 01:30  ← 29 March never appears
  2026-03-09 02:30                           2026-03-31 01:30
```

A late job versus a **missing** job. I filed the shifted-time version first, then found the skip
while re-verifying and added it as a follow-up comment — the harder failure to notice, and I'd
initially understated it.

**Bare `L` in day-of-week parses fine and throws on `next()`.** `parse('0 0 * * L')` succeeds; the
error arrives later, from a different call, after you've already stored the expression as valid.
Validation that happens at use rather than at parse is validation you can't build a UI around.

Plus: `stringify()` not being idempotent, and the two already merged above.

### The one that wasn't mine

I filed a seventh — a duplicated `0` escaping validation, because the check is `if (duplicate)`
against `Array.prototype.find`'s return value, which is falsy when the value found is `0`. Real bug.
`[1,1]` is rejected, `[0,0]` sails through.

It also had **an open pull request since three days before the hackathon started**, which the
maintainer pointed out within hours.

My duplicate check searched *issues*. It didn't search *open pull requests*. On a repo where fixes
arrive as PRs, that's half the project's memory of itself — the most recent thinking about a bug
often lives in a PR and never in an issue at all. A second report (`#425`) turned out to partly
overlap an issue I'd dismissed as stale, on the grounds that it "no longer reproduces as written" —
which, I now know, is not the same as the defect being gone. It had an open PR too.

So: **six original findings, not seven.** I'm leaving the wrong number visible in my repo's issue
log with a note, rather than editing it into a cleaner story, because the failure mode is more
useful than the count. If you're going to claim novelty against a live codebase, search issues *and*
PRs, open *and* closed, and search by **mechanism** rather than by title — my query was "duplicate
values", and the PR was titled "reject duplicate 0 in field validation", which a title-shaped search
finds and a concept-shaped one finds faster.

**The one I didn't file:** `W` (nearest weekday) is a phantom. It's in the `CronChars` type, it has
an explicit branch in `compactField`, four tests exercise it — but it's absent from
`CronDayOfMonth.validChars`, so `parse('0 0 15W * *')` throws. The stringify path can emit a value
the parse path cannot produce. It's reachable only by hand-constructing field objects.

I didn't file it because I couldn't tell whether it's a bug or a half-landed feature, and filing
ambiguous findings to inflate a count is exactly the behaviour that makes maintainers stop reading
issues. My port reproduces it precisely: `W` accepted in stringify, rejected by the parser.

---

## The decision I'd take back

I deleted several branches as unreachable — a "values is not an array" error that the Go signature
makes unrepresentable, a nil-location fallback both entry points already prevent. Reasonable.

Then I deleted the `wildcard` constructor override on the same grounds. The parser never sets it, so
it looked dead.

**It wasn't.** The field constructors are public API, and the original's own tests set it directly.
I'd reasoned about reachability from *one* entry point and called it reachability in general.

Restored, with a test. It's in my decision log as a counter-example, because the lesson isn't "don't
delete dead code" — it's that **a reachability argument is only as good as the set of entry points
you considered**, and I considered one.

---

## Honest numbers

The brief asked for honest numbers over confident claims, so:

| | |
|---|---|
| Original tests passing | **280 / 280**, zero modifications |
| `unsafe` in Go sources | **0** — the string doesn't appear, even in a comment |
| `reflect` in the library | **0** |
| Statement coverage | **99.5%** default, 100% with corpus generators enabled |
| Differential fuzz | 90s, 4,284 cases, **0 divergences** |
| CLI output diff | **124 / 124** identical |

Two of those I had to correct while writing this.

**Coverage.** My README claimed a flat 100%. Measured, it's **99.5%**; it only reaches 100% when two
corpus-generator tests run, and those are gated behind an env var because they write multi-megabyte
fixtures. Both readings are now published with the gap explained, because quoting only the higher one
is the flattering presentation rather than the true one.

**Pass rate.** My first generator scored those two skipped generators as `0.00%` pass rate — which is
as misleading in the other direction. It's now passed-over-*executed*, with the skipped column
visible next to it.

There are 23 `any`s in my WebAssembly bridge. They're all forced by `syscall/js` — `js.FuncOf`
mandates an `any` return — and one of them is a generic constraint (`lookup[T any]`), which isn't an
escape hatch at all. I counted it anyway. If you're going to publish a number, err against yourself.

---

## What I'd tell someone starting one of these

**Your test suite is not a proof.** It's a sample. The original suite passing 280/280 told me far
less than I wanted it to, because four of those tests had degraded into tautologies and I only found
out by asking what each one would do if the implementation were wrong.

**Build the differential harness first, then attack it.** Not "does it pass" — "what can I break
that it won't notice." Every blind spot I found came from sabotage, never from a clean run.

**When two runtimes disagree, find the single chokepoint.** One 20-line `fromWallClock` holding
every luxon-versus-Go divergence was the difference between a reviewable port and an unauditable one.

**Write down the decisions you'd take back.** My decision log has 21 entries and the most useful one
records a call I got wrong. Judges — and future readers — can tell the difference between a document
written to persuade and one written to remember.

---

*Code, decision log, fuzz harness and full numbers:
[github.com/aniket-3001/cron-parser-go](https://github.com/aniket-3001/cron-parser-go)*

*Built for Port Mortem by [@hackathon_raptors](https://github.com/hackathon-raptors). Track C,
TypeScript → Go.*
