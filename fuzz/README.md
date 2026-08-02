# Differential fuzzing

The original TypeScript library and the Go port are given identical inputs, and their
answers are compared across the whole public surface of `CronExpression` and `CronDate`.
A disagreement is a bug in one of them.

The one public entry point not fuzzed is `CronFileParser`, which reads crontab files: file
IO stays in JavaScript by design (`DECISIONS.md` D12), so there is no second
implementation of it to compare against. It is covered by the original suite instead.

There is no template for this; the design below is mine.

```bash
npm run fuzz                        # 60 seconds, seed 1
node fuzz/differential.js --seconds=90 --seed=7
```

Requires the upstream clone at `../cron-parser`, built with `npm run build`, and the
port built with `npm run build` here.

---

## Design

**Both implementations run in one process.** The reference is loaded from
`../cron-parser/dist`; the port is the WebAssembly build, reached through the same
bridge the test adapter uses. Running them together means an input is literally the
same object in both, with no serialisation step in between that could mask or invent a
difference.

**The port is reached directly, not through `adapter/src`.** The adapter exists to make
the original's TypeScript tests runnable and carries the conversions that job needs.
Putting it in the middle here would mean comparing the shim as much as the port. What
is under test is the Go library's behaviour.

**Every run is reproducible.** The generator is a seeded mulberry32, so a divergence
can be replayed exactly by rerunning with the same `--seed`.

## What is compared

For an expression case, all of:

| | |
|---|---|
| Parse outcome | accepted or rejected, and the exact error text when rejected |
| Forward schedule | 8 successive instants, to the millisecond |
| Backward schedule | 8 successive instants |
| Iteration errors | the exact text, and the point at which iteration stops |
| Rendered text | `stringify()` and `stringify(true)` |
| Field values | all six fields, including the number-or-token distinction |
| Membership | `includesDate` on each instant and one second either side |
| Rendered expression | `toString()`, which falls back to `stringify(true)` on a falsy expression |
| Cursor predicates | `hasNext()` and `hasPrev()`, **and the instant produced afterwards** |
| Bulk iteration | `take(4)` and `take(-4)`, the forward and backward branches |
| Rewinding | `reset()`, then the instant produced afterwards |

The last three rows are about state as much as answers. `hasNext` and `hasPrev` run a
search and then put the cursor back; a port that returned the right boolean while
consuming the occurrence would be indistinguishable unless the instant *after* the call
is compared too, so it is. `take` has a separate backward branch for a negative limit and
swallows the error at the end of a schedule, so a truncated result is part of the
contract. `reset` has to rewind far enough that the next occurrence is the first one
again.

**These four are sampled, not run on every case.** Each probed case re-parses the
expression four times and runs fourteen further searches, which cost about four fifths of
the run's throughput when applied universally. They are thin deterministic wrappers over
`next` and `prev`, so a fault in them is systematic rather than input-dependent and
surfaces on whichever cases are checked; a quarter of cases keeps a few hundred probes in
a 60-second run while leaving the budget on generating new inputs. The decision is drawn
once per case from the seeded generator and handed to both implementations, so a seed
still replays exactly and the two can never disagree about whether to probe.

For a date case, every operation the date type exposes, twelve arithmetic operations,
seven setters across several values each, and both day boundaries, is applied to one
starting instant, and after each the instant, its ISO rendering, its UTC offset and
both last-day predicates are compared.

## What is generated

**Expressions** cover every form the grammar allows: wildcards, single values, lists,
ranges, steps, stepped ranges, `?`, month and weekday names, `L`, `W`, `#N`, the ten
`@` aliases, hashed `H` forms, the five- and six-field shapes, and deliberately
malformed input. Roughly two thirds of generated expressions are rejected by both
implementations, which is itself worth comparing: the error text has to match too.

**Timezones** are chosen for the shape of their transitions rather than for coverage:
a two-hour gap (`Antarctica/Troll`), half- and quarter-hour offsets (`Australia/Lord_Howe`,
`Pacific/Chatham`), transitions at midnight (`America/Santiago`, `Asia/Beirut`),
southern-hemisphere ordering, and a zone that abolished daylight saving partway through
the range.

**Starting instants** are weighted toward where the two could actually disagree. Half
of them fall within four hours of a real transition, found by scanning each zone and
bisecting to the minute. Date cases additionally draw from a boundary corpus: leap
days, the last day of 31- and 30-day months, and year boundaries.

**Hashed expressions always carry a seed.** Without one the original draws from
`Math.random`, so `H` would differ between the two for reasons that are not bugs.

## Minimisation

A failing case is shrunk before it is recorded: each field is replaced with a wildcard
where that keeps the divergence, then the timezone is reduced to UTC and the start to a
round instant. A report therefore names the responsible field rather than the random
six-field expression that happened to expose it.

## Results

All runs below are on the full surface described above, including the cursor probes.

| Seed | Duration | Expression cases | Date cases | Divergences |
|---|---|---|---|---|
| 1 | 90s | 3,110 | 1,468 | **0** |
| 2 | 65s | 2,216 | 1,090 | **0** |
| 7 | 65s | 1,455 | 765 | **0** |
| 31337 | 65s | 2,143 | 985 | **0** |

`fuzz/log.txt` is the seed 1 run above, published in full.

Each expression case is roughly fifty individual comparisons and each date case around
forty more, so a 90-second run is on the order of 200,000 compared answers.

Throughput is about 48 cases per second. The limit is the reference: an expression that
can never match costs it 39 ms, because it performs ten thousand iterations of date
arithmetic before giving up, against 4 ms for an ordinary case. The generator is not
biased away from those, since how the two agree on giving up is worth comparing.

`fuzz/last-run.json` holds the summary of the most recent run;
`fuzz/divergences/` holds any minimised reproductions.

## Is the harness worth anything?

A fuzzer that reports no divergence is only meaningful if it would report one. This one
was validated by deliberately breaking the port and checking that it noticed.

**Breaking the search engine.** Replacing the hour fast path with a loop that steps one
hour at a time produced four minimised divergences within twenty seconds.

**Breaking month arithmetic.** Substituting Go's overflowing `AddDate` for the clamping
implementation produced divergences within seventeen seconds, correctly minimised to
the responsible field.

**Breaking year arithmetic, and the two blind spots that exposed.** This one went
unnoticed through three separate attempts, and each failure improved the harness:

1. The first run compared expressions only. Year arithmetic is unreachable from the
   search loop (it never uses that unit) so nothing exercised it. **Date operations
   became a separate case type.**
2. The second run picked one random operation per date case. The bug only shows on a
   leap day, and the chance of drawing both that instant and that operation was under
   one in four hundred. **Each date case now sweeps the whole surface from one
   instant.**
3. The third run still sampled instants uniformly, and a leap day is about one instant
   in a thousand. **Date cases now draw half their starts from a boundary corpus.**

With those in place the same sabotage is caught in seconds, with leap-day reproductions.

**Breaking the cursor restoration in `HasNext`.** Deleting the line that puts the cursor
back (so the predicate answers correctly but silently consumes an occurrence) produced
74 divergences in 25 seconds, all reported against `afterAsking`, the instant observed
after asking. Comparing only the boolean would have missed every one of them.

Worth recording honestly: the original suite catches that sabotage too, with 7 failures.
The cursor probes were added because the harness claimed a surface it did not cover, not
because they found something the tests had missed. What they add is the same surface
across thousands of generated expressions and fourteen timezones rather than a handful of
fixed cases.

## What it found

A real defect in the port, during a qualifying run:

```
DIVERGENCE  "* * * * * 0,7,4,4" tz=UTC
  parse: reference=accepted port=rejected: CronDayOfWeek Validation error, duplicate values found: 4
```

The original's duplicate check uses `Array.prototype.find`, which stops at the **first**
duplicate and then tests it for truthiness. Day-of-week normalises `7` to `0`, so this
field holds `[0, 0, 4, 4]`: two duplicate pairs. The `0` pair is found first and is
falsy, so nothing is reported, and because the search already stopped the `4` pair is
never reached either.

The port checked every value and skipped only the zeros, so it caught the `4`. That is
arguably the better behaviour, but it is not the original's. Fixed, and pinned by
`TestDuplicateZeroMasksLaterDuplicates`.

This is the upstream duplicate-zero bug being worse than first reported;
Issue #423 filed upstream describes the masking.
