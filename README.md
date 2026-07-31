# cron-parser-go

A Go port of [`harrisiirak/cron-parser`](https://github.com/harrisiirak/cron-parser) v5.6.2,
built for **Port Mortem** (Track C — TypeScript → Go).

The original library is a cron **expression calculator** — not a scheduler. It answers
"when does this cron expression next fire?", forwards or backwards, with full IANA timezone and
DST handling. This port reproduces that behaviour in Go and proves it by running the original
280-test Jest suite **byte-unmodified** against the Go implementation.

> **Status: in progress.** Design is complete and the repository is scaffolded.
> See [`DESIGN.md`](DESIGN.md) for the low-level design and
> [`DECISIONS.md`](DECISIONS.md) for the architectural divergence log.

---

## Build

```bash
make build      # builds the Go library and the js/wasm bridge
```

## Test

```bash
make test           # native Go tests
make test-original  # the 280 original Jest tests, unmodified, against the Go port
make verify-hashes  # proves tests/original/ is byte-identical to upstream
```

---

## What makes this port non-trivial

The original delegates all date arithmetic to [luxon](https://moment.github.io/luxon/). Go's
`time` package disagrees with luxon in five measured ways, three of them severe:

| Case | luxon | Go naive |
|---|---|---|
| `2024-02-29 + 1 year` | `2025-02-28` **clamps** | `2025-03-01` overflows |
| `set(month=2)` on `2024-01-31` | `2024-02-29` **clamps** | `2024-03-02` overflows |
| `set(day=31)` in February | `2024-03-02` **overflows** | `2024-03-02` ✅ agrees |
| `startOf(day)` Santiago `2026-09-06` | `09-06T01:00-03` | `09-05T23:00-04` — **previous day** |
| non-existent `2026-03-08T02:30` NY | `03:30` **forward** | `01:30` **backward** |

luxon clamps for year and month but overflows for day, and resolves DST gaps in the opposite
direction from Go. Any uniform strategy is wrong. See [`DESIGN.md`](DESIGN.md) §4.

---

## Repository layout

```
cron/          the port — pure Go, zero unsafe, no JavaScript dependency
bridge/        js/wasm handle registry (test bridging only)
adapter/       TypeScript shim mirroring the original module layout
tests/original/  the 280 original tests, byte-identical, with kickoff SHA-256
fuzz/          differential fuzzing harness
```

The library and the test bridge are deliberately separate artifacts. `./cron` is what a Go
developer would import and what gets benchmarked; `./bridge` + `./adapter` exist only so the
original Jest suite can execute unmodified.

---

## Bugs found in the original

Four latent bugs were reproduced against v5.6.2 during this port:

1. **DST compensation assumes every transition is exactly one hour** (`CronDate.ts:559`) — the
   `diff === 2` check never fires for `Antarctica/Troll`'s two-hour gap.
2. **In-place sort mutates the caller's array** (`CronField.ts:99`).
3. **Unintended regex range** (`CronExpressionParser.ts:444`) — `/([,-/])/` is the character
   range `0x2C–0x2F`, so `.` matches by accident.
4. **Bare `L` in day-of-week parses but throws at iteration time** (`CronExpression.ts:209`).

Plus a design inconsistency: **`W` is a phantom feature** — the stringify path handles it, the
parser rejects it.

---

## License

MIT. The original work is © 2014-2023 Harri Siirak; `tests/original/` remains under that
copyright. See [`LICENSE`](LICENSE).
