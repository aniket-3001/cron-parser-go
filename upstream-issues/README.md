# Upstream bug reports

Latent bugs found in `harrisiirak/cron-parser` v5.6.2 while porting it to Go, each reproduced
against the pinned commit `aeb2a1513fd33365a6414f4137516c9482f831ed`.

The Port Mortem **Bug Catcher** bonus requires these to be *filed upstream during the 72-hour
window* — reproducing them is not sufficient. This directory holds the drafts; the table below
tracks what has actually been filed.

| # | Bug | Severity | Filed |
|---|---|---|---|
| 1 | [DST compensation assumes 1-hour transitions](01-dst-two-hour-transitions.md) | High — wrong results in `Antarctica/Troll` | ☐ |
| 2 | [In-place `sort()` mutates the caller's array](02-in-place-sort-mutates-caller.md) | Medium — silent caller data corruption | ☐ |
| 3 | [`[,-/]` is an unintended character range](03-regex-character-range.md) | Low — wrong rejection reason | ☐ |
| 4 | [Bare `L` in day-of-week throws at `next()`, not `parse()`](04-bare-L-day-of-week-throws-late.md) | Medium — validation boundary is wrong | ☐ |
| 5 | [Duplicate `0` escapes the duplicate check](05-duplicate-zero-escapes-validation.md) | Low — inconsistent validation | ☐ |

## Filing notes

- Each report is self-contained: summary, runnable repro, expected vs actual, suggested fix.
- Every repro was executed against v5.6.2, not inferred from reading the source.
- Reports mention that the bug was found while porting, which explains the unusual thoroughness
  without editorialising.
- Bug 3 is deliberately framed as low severity — the input is invalid either way, and overstating
  it would undermine the credibility of the other four.

## Not filed as a bug

**`W` is a phantom feature.** `CronChars` is `'L' | 'W'`, `CronFieldCollection.compactField()`
handles `'W'`, and four tests exercise it — but no `validChars` regex contains `W`, so the parser
rejects `15W` and `LW`. The stringify path can emit a value the parse path cannot produce.

This is a design inconsistency rather than a defect with a wrong output, and whether `W` was meant
to be supported is a maintainer's call, not ours. It is documented in `DECISIONS.md` D15 and
`SEMANTICS.md` §7. If raised upstream at all, it belongs as a question, not a bug report.
