# Upstream bug reports

Latent bugs found in `harrisiirak/cron-parser` v5.6.2 while porting it to Go, each reproduced
against the pinned commit `aeb2a1513fd33365a6414f4137516c9482f831ed`.

Seven were filed upstream on 2026-08-01. **Four have been fixed and merged**, two more are
triaged and assigned to the maintainer, and one of those duplicates a pull request that was
already open — see [Outcome](#outcome) below for the honest accounting.

| Filed | Bug | Severity | Status |
|---|---|---|---|
| [#424](https://github.com/harrisiirak/cron-parser/issues/424) | `stringify()` round trip changes the schedule | **High** — silently reschedules a job | labelled `bug`, assigned |
| [#419](https://github.com/harrisiirak/cron-parser/issues/419) | DST compensation assumes one-hour transitions | High — an occurrence is skipped in `Antarctica/Troll` | open |
| [#420](https://github.com/harrisiirak/cron-parser/issues/420) | In-place `sort()` mutates the caller's array | Medium — silent caller data corruption | **fixed, [PR #427](https://github.com/harrisiirak/cron-parser/pull/427)** |
| [#422](https://github.com/harrisiirak/cron-parser/issues/422) | Bare `L` in day-of-week throws at `next()`, not `parse()` | Medium — validation boundary is wrong | **fixed, [PR #428](https://github.com/harrisiirak/cron-parser/pull/428)** |
| [#423](https://github.com/harrisiirak/cron-parser/issues/423) | A duplicated `0` escapes validation, masks later duplicates, and breaks `stringify()` | Medium — accepted input cannot be rendered | labelled `bug`, assigned — **duplicate of [PR #418](https://github.com/harrisiirak/cron-parser/pull/418)** |
| [#425](https://github.com/harrisiirak/cron-parser/issues/425) | `stringify()` is not idempotent | Low — rendered text is not canonical | **fixed, [PR #433](https://github.com/harrisiirak/cron-parser/pull/433)** |
| [#421](https://github.com/harrisiirak/cron-parser/issues/421) | `[,-/]` is an unintended character range | Low — wrong rejection reason | **fixed, [PR #426](https://github.com/harrisiirak/cron-parser/pull/426)** |

Listed by severity rather than by number. **Each row links to the filed issue, which carries the
full reproduction and root-cause analysis** — those were drafted here first and now live upstream,
so they are not duplicated in this repository.

Each is also pinned by a test in the port, since the port reproduces these bugs rather than
correcting them (`DECISIONS.md` D13). Searching the Go tests for an issue number finds the test
that holds the behaviour in place.

## Outcome

Recorded as of 2026-08-02T13:00Z. The maintainer began fixing them the same day they were filed.

**Merged.** Each fix matches what the report proposed, and each landed with regression tests:

| | |
|---|---|
| [PR #426](https://github.com/harrisiirak/cron-parser/pull/426) → #421 | `val.match(/([,-/])/)` → `val.match(/([,\-/])/)` — the hyphen escaped so it is a literal rather than a range. Commit `a551625`. |
| [PR #427](https://github.com/harrisiirak/cron-parser/pull/427) → #420 | `values.sort(...)` → `[...values].sort(...)` — a defensive copy. Commit `5c01e1f`. |
| [PR #428](https://github.com/harrisiirak/cron-parser/pull/428) → #422 | Standalone `L` in day-of-week rejected at construction instead of throwing later from `next()`. |
| [PR #433](https://github.com/harrisiirak/cron-parser/pull/433) → #425 | Day-of-month values the named month does not have are dropped, so `stringify()` settles on the first render. |

The second is the fix this port had already made independently on day one, for the same reason
(`DECISIONS.md` D4): Go slices share a backing array, so the naive translation would have inherited
the bug.

**Triaged but not yet fixed.** #423 and #424 are labelled and assigned to the maintainer.

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
of June. That second half is what PR #433 fixed, which supports treating it as a related mechanism
rather than the same defect.

**So the defensible claim is six original findings, of which four are merged** — not seven
discoveries.

## How they were found

#419, #420, #421 and #422 came from reading the source and confirming by execution. #423, #424 and
#425 came from checking a property over randomly generated expressions — that an expression and its
rendering describe the same schedule — and none of the three would have been found by reading.

The masking half of #423 was found by the differential fuzzer during a qualifying run: the port
checked every duplicate rather than only the first, and so rejected `0,7,4,4` where the original
accepts it. Reproducing the original exactly meant making the port accept it too.

#423 began as two findings. They share one root cause and one fix — a field that survives the
duplicate check is the same field that cannot be rendered — so they were merged before filing
rather than sent as two issues a maintainer would have to reconcile.

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
