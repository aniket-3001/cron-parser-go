# Upstream bug reports

Latent bugs found in `harrisiirak/cron-parser` v5.6.2 while porting it to Go, each reproduced
against the pinned commit `aeb2a1513fd33365a6414f4137516c9482f831ed`.

Seven were filed upstream on 2026-08-01. **Three have been fixed and merged**, three more are
triaged and assigned to the maintainer, and one of those duplicates a pull request that was
already open — see [Outcome](#outcome) below for the honest accounting.

| Filed | Bug | Severity | Status | Draft |
|---|---|---|---|---|
| [#424](https://github.com/harrisiirak/cron-parser/issues/424) | `stringify()` round trip changes the schedule | **High** — silently reschedules a job | labelled `bug`, assigned | [06](06-stringify-round-trip-changes-schedule.md) |
| [#419](https://github.com/harrisiirak/cron-parser/issues/419) | DST compensation assumes one-hour transitions | High — an occurrence is skipped in `Antarctica/Troll` | open | [01](01-dst-two-hour-transitions.md) |
| [#420](https://github.com/harrisiirak/cron-parser/issues/420) | In-place `sort()` mutates the caller's array | Medium — silent caller data corruption | **fixed, [PR #427](https://github.com/harrisiirak/cron-parser/pull/427)** | [02](02-in-place-sort-mutates-caller.md) |
| [#422](https://github.com/harrisiirak/cron-parser/issues/422) | Bare `L` in day-of-week throws at `next()`, not `parse()` | Medium — validation boundary is wrong | **fixed, [PR #428](https://github.com/harrisiirak/cron-parser/pull/428)** | [04](04-bare-L-day-of-week-throws-late.md) |
| [#423](https://github.com/harrisiirak/cron-parser/issues/423) | A duplicated `0` escapes validation, masks later duplicates, and breaks `stringify()` | Medium — accepted input cannot be rendered | labelled `bug`, assigned — **duplicate of [PR #418](https://github.com/harrisiirak/cron-parser/pull/418)** | [05](05-duplicate-zero-escapes-validation.md) |
| [#425](https://github.com/harrisiirak/cron-parser/issues/425) | `stringify()` is not idempotent | Low — rendered text is not canonical | open — overlaps #279 | [08](08-stringify-not-idempotent.md) |
| [#421](https://github.com/harrisiirak/cron-parser/issues/421) | `[,-/]` is an unintended character range | Low — wrong rejection reason | **fixed, [PR #426](https://github.com/harrisiirak/cron-parser/pull/426)** | [03](03-regex-character-range.md) |

Listed by severity rather than by number.

## Outcome

Recorded as of 2026-08-01T15:30Z. The maintainer acted on the same day they were filed.

**Merged.** Both fixes match what the reports proposed, and both landed with regression tests:

| | |
|---|---|
| [PR #426](https://github.com/harrisiirak/cron-parser/pull/426) → #421 | `val.match(/([,-/])/)` → `val.match(/([,\-/])/)` — the hyphen escaped so it is a literal rather than a range. Commit `a551625`. |
| [PR #427](https://github.com/harrisiirak/cron-parser/pull/427) → #420 | `values.sort(...)` → `[...values].sort(...)` — a defensive copy. Commit `5c01e1f`. |
| [PR #428](https://github.com/harrisiirak/cron-parser/pull/428) → #422 | Standalone `L` in day-of-week rejected at construction instead of throwing later from `next()`. |

The second is the fix this port had already made independently on day one, for the same reason
(`DECISIONS.md` D4): Go slices share a backing array, so the naive translation would have inherited
the bug.

**Triaged but not yet fixed.** #423, #424 and #425 are labelled and assigned to the maintainer.

**Offered.** #419 is the only one still untriaged. A working patch exists — offset-based gap
detection plus multi-hour matching, 292 tests green and a 5,865-case sweep showing changes confined
to `Antarctica/Troll` — and it has been offered on the issue rather than pushed as an unsolicited
pull request, since the maintainer has self-assigned everything else and may want this one too.

**One duplicate, which is a miss on our part.** #423 restates
[PR #418](https://github.com/harrisiirak/cron-parser/pull/418), opened by `gaoflow` on 2026-07-29 —
three days before kickoff — describing the identical falsy-zero mechanism. The duplicate check
below searched *issues* and not *open pull requests*, which is how it was missed. The defect is
real and was reproduced independently here, but it was already known, so it does not count as an
original finding.

**One partial overlap.** #425's first step — `16-26/5` collapsing to `16/5` — is the `start/step`
shorthand behaviour that [PR #411](https://github.com/harrisiirak/cron-parser/pull/411) addresses
for issue #279. The distinct part of #425 is what follows: day-of-month maximum is narrowed to the
named month's length when stringifying but not when parsing, so re-parsing the output gains a 31st
of June. Related mechanism, different defect, and worth describing that way rather than as wholly
independent.

**So the defensible claim is six original findings, of which three are merged** — not seven
discoveries.

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

### What that check got wrong

**It searched issues and not open pull requests.** Two of the conclusions above did not survive
contact with the repository:

- [PR #418](https://github.com/harrisiirak/cron-parser/pull/418) had been open since 2026-07-29 with
  the duplicate-zero fix. #423 restates it. The maintainer pointed this out within hours.
- #279 was recorded as stale, but it has an **open** pull request,
  [#411](https://github.com/harrisiirak/cron-parser/pull/411), from June. The issue not reproducing
  as written is not the same as the underlying defect being gone, and #425 turned out to overlap it
  in part.

A duplicate search that ignores pull requests only covers half the project's memory of itself: on a
repository where fixes arrive as PRs, the most recent thinking about a bug often lives there and
never in an issue at all. The correct query set is issues **and** PRs, open **and** closed, searched
by mechanism rather than by title.

This is recorded rather than quietly corrected because it changes a headline number: six original
findings, not seven.

## Not filed

**`W` is a phantom feature.** `CronChars` is `'L' | 'W'`, `CronFieldCollection.compactField()`
handles `'W'`, and four tests exercise it — but no field's `validChars` contains `W`, so the
parser rejects `15W` and `LW` and no constructor accepts it. The stringify path can emit a value
the parse path cannot produce.

This is a design inconsistency rather than a defect with a wrong output, and whether `W` was meant
to be supported is a maintainer's call — there are already open requests for it (#167, #376). It
is documented in `DECISIONS.md` D15 and `SEMANTICS.md` section 7.
