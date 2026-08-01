# cron-parser-go

A Go port of [`harrisiirak/cron-parser`](https://github.com/harrisiirak/cron-parser) v5.6.2,
built for **Port Mortem** (Track C — TypeScript → Go).

The original is a cron **expression calculator**, not a scheduler: it answers "when does this
expression next fire?", forwards or backwards, with full IANA timezone and daylight-saving
handling. This port reproduces that behaviour in Go.

```
280 of 280 original tests pass, byte-unmodified
```

---

## Quick start

```bash
npm install
npm run build          # compiles the library and the test bridge
npm test               # runs the original suite against the port
```

Requires Go 1.24+ and Node 18+. The upstream clone must sit at `../cron-parser`, built with
`npm run build`, for the tests and the fuzzer to compare against.

| Command | What it does |
|---|---|
| `npm run build` | Builds the Go library and the WebAssembly bridge |
| `npm test` | The 280 original tests, unmodified, against the port |
| `npm run test:go` | The port's own Go tests |
| `npm run verify-hashes` | Proves `tests/original/` matches the kickoff hashes |
| `npm run fuzz` | 60 seconds of differential fuzzing against the original |
| `npm run bench` | Benchmarks both implementations and writes `bench/RESULTS.md` |
| `npm run compare` | Runs both CLIs on a shared input set and diffs the output |
| `npm run honest-numbers` | Measures unsafe/`any`/pass rate/coverage into `HONEST-NUMBERS.md` |

A `Makefile` provides the same targets for anyone who prefers it. The npm scripts are the
primary path because `make` is not present on a stock Windows install.

### The runnable artifact

```bash
go build -o cron-parser ./cmd/cron-parser

./cron-parser next "*/15 9-17 * * 1-5" -n 3
./cron-parser next "30 2 * * *" -n 2 -tz America/New_York   # crosses a DST gap
./cron-parser parse "0 0 L * *"
./cron-parser check "0 0 * * *" -at 2026-01-02T00:00:00Z    # exit status is the answer
```

Or without a Go toolchain, from the 2.7 MB scratch image — the build runs `go vet` and the full
test suite, so an image that exists is an image whose tests passed:

```bash
docker build -t cron-parser-go .
docker run --rm cron-parser-go next "0 0 * * *" -n 3
```

The zone database is embedded, so timezones resolve inside `scratch` with no OS packages.

---

## Evidence

### The original test suite, unmodified

All 280 tests run against the Go port with **no edit to any test file**. Two things make that
checkable rather than merely claimed:

- `tests/original/HASHES.txt` records the SHA-256 of each file, computed from the upstream suite
  at kickoff before any port code existed. `npm run verify-hashes` re-checks them, and CI would
  fail if a byte moved.
- The files have not changed in a single commit since the repository was created, which
  `git log -- tests/original/` shows directly.

The hashes are self-computed and reproducible: clone `harrisiirak/cron-parser` at
`aeb2a1513fd33365a6414f4137516c9482f831ed`, take the SHA-256 of `tests/*.test.ts` and
`tests/types.d.ts`, and compare.

Nothing reaches into a test file. `jest.config.js` remaps the literal specifier `../src/...`
onto the adapter, which is enough because Jest matches the request string rather than a resolved
path.

### Differential fuzzing

Both implementations are loaded into one process, given identical inputs, and every answer
either can produce is compared. Four runs, no divergences:

| Seed | Duration | Expression cases | Date cases | Divergences |
|---|---|---|---|---|
| 1 | 90s | 3,446 | 1,666 | **0** |
| 2 | 65s | 2,267 | 1,098 | **0** |
| 7 | 65s | 1,855 | 898 | **0** |
| 31337 | 65s | 2,773 | 1,348 | **0** |

The harness was validated by deliberately breaking the port and checking it noticed — and one
sabotage defeated it three times, each failure exposing a real blind spot. See
[`fuzz/README.md`](fuzz/README.md), which also records the genuine defect it found in this port.
A published run is in [`fuzz/log.txt`](fuzz/log.txt).

### CLI output diff

The fuzzer compares the two through their APIs. This compares them as **programs**: the same
command line against each, diffing stdout, stderr and exit status.

| | |
|---|---:|
| Command lines compared | 124 |
| Identical output | **124** |

Covering 66 expression cases across the grammar, 30 timezone cases including two-hour, half-hour
and quarter-hour transitions, 8 hashed cases under a shared seed, 4 membership queries, and 16
rejection paths where the error text and the exit status both have to match.

The original ships only a library, so `compare/original-cli.js` gives it a front end with the same
output shape. It contains no logic beyond formatting — every answer comes from the original — and
it pins the timestamp format, since Go writes a zero offset as `Z` where luxon writes `+00:00` and
every line would otherwise differ for reasons unrelated to the schedule. See
[`compare/CLI-DIFF.md`](compare/CLI-DIFF.md).

### Performance

Measured on the original's own 15-pattern benchmark corpus, so neither side is measured on
input chosen to flatter it. Full methodology and per-pattern figures in
[`bench/RESULTS.md`](bench/RESULTS.md).

| | Original | Port | |
|---|---:|---:|---|
| Throughput (sum of medians) | — | — | **23.5x faster** |
| Cold start, p50 | 136.0 ms | 12.0 ms | **11.4x faster** |
| Cold start, p99 | 148.2 ms | 13.1 ms | 11.3x faster |
| Memory after the workload | 84.9 MB | 20.2 MB | different metrics, see below |

Faster on all 15 patterns, by between 2.9x and 36x. The memory figures are Node's resident set
size against Go's runtime `Sys`; they are not the same measurement and are reported separately
rather than reduced to a ratio that would imply more precision than exists.

### Code quality

| | |
|---|---|
| Statement coverage | **100%** with `CRON_GEN_CORPUS=1`, 99.5% in a default run |
| `unsafe` in the library | **0** |
| `interface{}` in the library | **0** |
| Reflection in the library | **0** |
| `any` in the adapter | **0** |
| `gofmt`, `go vet` | clean |

The compatibility surface needed by the test bridge lives in `cron/bridge_wasm.go` behind a
`js && wasm` build tag, so it does not exist in an ordinary build of the library.

Both coverage figures are given because the port's suite has two honest readings. Two tests are
corpus *generators* that write multi-megabyte fixtures, so they are gated behind an environment
variable rather than run every time, and the statements only they reach are uncovered without it.
Quoting the higher number alone would be the more flattering presentation and the less true one.

Every figure in this section is produced by `npm run honest-numbers`, which writes
[`HONEST-NUMBERS.md`](HONEST-NUMBERS.md): unsafe count, `any` count, per-file test pass rate and
the coverage diff against the original, measured rather than typed in, so the table cannot drift
away from the repository.

---

## What made this port non-trivial

The original delegates all date arithmetic to [luxon](https://moment.github.io/luxon/), which
disagrees with Go's `time` package in ways that are easy to miss and quietly wrong:

| Case | luxon | Naive Go |
|---|---|---|
| `2024-02-29 + 1 year` | `2025-02-28` **clamps** | `2025-03-01` overflows |
| `set(month=2)` on `2024-01-31` | `2024-02-29` **clamps** | `2024-03-02` overflows |
| `set(day=31)` in February | `2024-03-02` **overflows** | `2024-03-02` agrees |
| `startOf(day)` Santiago `2026-09-06` | `09-06T01:00-03` | `09-05T23:00-04` — **previous day** |
| non-existent `2026-03-08T02:30` NY | `03:30` **forward** | `01:30` **backward** |

luxon clamps for year and month but overflows for day, and resolves impossible local times in
the opposite direction from Go. Any uniform strategy is wrong. The Santiago case is the sharpest:
`time.Date` can land on the previous calendar day, which corrupts day-of-month matching outright.

The time layer was built first and validated against luxon over 244,944 operations before
anything depended on it. See [`DESIGN.md`](DESIGN.md) §4.

---

## Bugs found in the original

Seven, all **filed upstream and open**. Three were found by differential and property-based
testing rather than by reading. Existing issues were checked first to avoid duplicates; see
[`upstream-issues/`](upstream-issues/) for the reproductions and that check.

| Issue | Bug |
|---|---|
| [#424](https://github.com/harrisiirak/cron-parser/issues/424) | **`stringify()` can change the schedule** — `0 0 16 * 0-6` renders to `0 0 16 * *`; the first fires daily, the second monthly |
| [#419](https://github.com/harrisiirak/cron-parser/issues/419) | **DST compensation assumes one-hour transitions** — `diff === 2` never fires for `Antarctica/Troll`'s two-hour gap |
| [#420](https://github.com/harrisiirak/cron-parser/issues/420) | **In-place `sort()` mutates the caller's array** — a constructor rewrites its argument |
| [#422](https://github.com/harrisiirak/cron-parser/issues/422) | **Bare `L` in day-of-week fails at `next()`** — parses cleanly, throws during iteration |
| [#423](https://github.com/harrisiirak/cron-parser/issues/423) | **A duplicated `0` masks every other duplicate** and makes rendering throw — `0,7,4,4` is accepted, `4,4` is rejected |
| [#425](https://github.com/harrisiirak/cron-parser/issues/425) | **`stringify()` is not idempotent** — day-of-month renders and parses against different maxima |
| [#421](https://github.com/harrisiirak/cron-parser/issues/421) | **`[,-/]` is an unintended character range** — matches `.` as well as `,` `-` `/` |

Plus a design inconsistency: **`W` is a phantom feature** — the stringify path handles it, no
field accepts it, so it is unreachable through any constructor.

---

## Repository layout

```
cron/              the port. Pure Go, zero unsafe, no JavaScript dependency
cmd/cron-parser/   the CLI, and the runnable artifact
bridge/            js/wasm handle registry, for test bridging only
adapter/src/       TypeScript shim mirroring the original's module layout
tests/original/    the 280 original tests, byte-identical, with kickoff hashes
fuzz/              differential fuzzing harness, its write-up, and a published run
bench/             benchmark harnesses, methodology and results
compare/           CLI output diff against the original, on a shared input set
upstream-issues/   drafted bug reports
Dockerfile         multi-stage build; vets and tests, then a scratch image
.port-mortem.toml  submission manifest: track, source, kickoff hash, results
HONEST-NUMBERS.md  generated: unsafe, any, per-file pass rate, coverage diff
```

The library and the test bridge are deliberately separate. `./cron` is what a Go developer would
import and what gets benchmarked; `./bridge` and `./adapter` exist only so the original suite can
run unmodified.

Design and rationale: [`DESIGN.md`](DESIGN.md) (how), [`SEMANTICS.md`](SEMANTICS.md) (what the
original actually does), [`DECISIONS.md`](DECISIONS.md) (why the port differs where it does).

---

## Using it

```go
import "github.com/aniket-3001/cron-parser-go/cron"

expr, err := cron.Parse("*/15 9-17 * * 1-5", cron.WithLocation(time.UTC))
if err != nil {
    return err
}

next, err := expr.Next()

for t := range expr.All() {   // Go 1.23 range-over-func
    fmt.Println(t)
}
```

---

## License

MIT. The original work is © 2014-2023 Harri Siirak; `tests/original/` remains under that
copyright. See [`LICENSE`](LICENSE).
