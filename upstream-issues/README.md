# Upstream bug reports

Latent bugs found in `harrisiirak/cron-parser` v5.6.2 while porting it to Go, each reproduced
against the pinned commit `aeb2a1513fd33365a6414f4137516c9482f831ed`.

All seven are **filed upstream** and open.

| Filed | Bug | Severity | Draft |
|---|---|---|---|
| [#424](https://github.com/harrisiirak/cron-parser/issues/424) | `stringify()` round trip changes the schedule | **High** — silently reschedules a job | [06](06-stringify-round-trip-changes-schedule.md) |
| [#419](https://github.com/harrisiirak/cron-parser/issues/419) | DST compensation assumes one-hour transitions | High — wrong results in `Antarctica/Troll` | [01](01-dst-two-hour-transitions.md) |
| [#420](https://github.com/harrisiirak/cron-parser/issues/420) | In-place `sort()` mutates the caller's array | Medium — silent caller data corruption | [02](02-in-place-sort-mutates-caller.md) |
| [#422](https://github.com/harrisiirak/cron-parser/issues/422) | Bare `L` in day-of-week throws at `next()`, not `parse()` | Medium — validation boundary is wrong | [04](04-bare-L-day-of-week-throws-late.md) |
| [#423](https://github.com/harrisiirak/cron-parser/issues/423) | A duplicated `0` escapes validation, masks later duplicates, and breaks `stringify()` | Medium — accepted input cannot be rendered | [05](05-duplicate-zero-escapes-validation.md) |
| [#425](https://github.com/harrisiirak/cron-parser/issues/425) | `stringify()` is not idempotent | Low — rendered text is not canonical | [08](08-stringify-not-idempotent.md) |
| [#421](https://github.com/harrisiirak/cron-parser/issues/421) | `[,-/]` is an unintended character range | Low — wrong rejection reason | [03](03-regex-character-range.md) |

Filed 2026-08-01. Listed by severity rather than by number.

## How they were found

Reports 01 to 04 came from reading the source and confirming by execution. Reports 05, 06 and 08
came from checking a property over randomly generated expressions — that an expression and its
rendering describe the same schedule — and none of the three would have been found by reading.

The masking half of 05 was found by the differential fuzzer during a qualifying run: the port
checked every duplicate rather than only the first, and so rejected `0,7,4,4` where the original
accepts it. Reproducing the original exactly meant making the port accept it too.

Reports 05 and 07 were originally separate. They share one root cause and one fix — a field that
survives the duplicate check is the same field that cannot be rendered — so they were merged
before filing rather than sent as two issues a maintainer would have to reconcile.

## Before filing

Existing issues were checked for overlap, to avoid duplicates:

- **#279** (`stringifyField` renders `6-18/3` as `6/6`) no longer reproduces on v5.6.2 and links to
  the v4 source layout.
- **#273** is a different daylight-saving bug, about `endDate` and the timespan check rather than
  the width of a transition.
- **#257** asks for `L` combined with lists to be rejected in day-of-month, which is adjacent to
  #422 but not the same defect.
- **#268** is closed and concerns a different duplicate-value path.

Searches for the two-hour transition, the sort mutation, the round-trip divergence and the
internal render error returned nothing.

## Not filed

**`W` is a phantom feature.** `CronChars` is `'L' | 'W'`, `CronFieldCollection.compactField()`
handles `'W'`, and four tests exercise it — but no field's `validChars` contains `W`, so the
parser rejects `15W` and `LW` and no constructor accepts it. The stringify path can emit a value
the parse path cannot produce.

This is a design inconsistency rather than a defect with a wrong output, and whether `W` was meant
to be supported is a maintainer's call — there are already open requests for it (#167, #376). It
is documented in `DECISIONS.md` D15 and `SEMANTICS.md` section 7.
