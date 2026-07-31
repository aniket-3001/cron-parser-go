**Title:** `stringify()` is not round-trip safe: re-parsing its output can produce a different schedule

**Version:** 5.6.2 (`aeb2a1513fd33365a6414f4137516c9482f831ed`)

---

### Summary

`CronExpressionParser.parse(expr.stringify())` can yield an expression that fires on
**completely different days** from `expr`.

The cause is a disagreement between two notions of "is this field a wildcard?":

- **`isWildcard`** is derived from the *raw text* (`CronField.#isWildcardValue`): true only when
  the field was literally written `*` or `?`.
- **`stringifyField`** works from the *expanded values*, so any field covering its whole range is
  rendered as `*`.

So a day field written as `0-6`, `0-7` or `*/1` is **not** a wildcard, yet renders as `*`.
That matters because `#matchDayOfMonth` switches between OR and AND on exactly this flag:

- both day fields restricted → the day matches if **either** matches (rule 1)
- one of them a wildcard → the other **must** match (rules 2 and 3)

Rendering therefore silently converts an OR into an AND.

### Reproduction

```js
const parser = require('cron-parser');
const opts = { currentDate: new Date('2026-01-01T00:00:00Z'), tz: 'UTC' };

const a = parser.parse('0 0 16 * 0-6', opts);
const s = a.stringify();               // "0 0 16 * *"
const b = parser.parse(s, opts);

console.log(s);
console.log(a.take(2).map((d) => d.toISOString()));
console.log(b.take(2).map((d) => d.toISOString()));
```

```
0 0 16 * *
[ '2026-01-02T00:00:00.000Z', '2026-01-03T00:00:00.000Z' ]   <- every day
[ '2026-01-16T00:00:00.000Z', '2026-02-16T00:00:00.000Z' ]   <- only the 16th
```

The original fires **daily**; its own `stringify()` output fires **monthly**.

Equivalent reproductions:

| Expression | `stringify()` | Before | After |
|---|---|---|---|
| `0 0 16 * 0-6` | `0 0 16 * *` | every day | the 16th |
| `0 0 16 * 0-7` | `0 0 16 * *` | every day | the 16th |
| `0 0 16 * */1` | `0 0 16 * *` | every day | the 16th |
| `0 0 1-31 * 5` | `0 0 * * 5` | every day | Fridays only |

The last case runs the other way: an exhaustive **day-of-month** turns a daily schedule into a
weekly one.

For contrast, these are unaffected because the wildcard flag never changes:

| Expression | `stringify()` | Round-trips |
|---|---|---|
| `0 0 16 */1 *` | `0 0 16 * *` | ✅ (month is not part of the rule) |
| `0 0 16 * ?` | `0 0 16 * ?` | ✅ (`?` is preserved) |

### Expected vs actual

**Expected:** `parse(x.stringify())` fires at the same instants as `x`. `stringify()` is
documented as producing "the string representation of the cron expression", which implies it
denotes the same schedule.

**Actual:** for any expression where a day field covers its full range without being written `*`
or `?`, the schedule changes.

This is most likely to bite code that normalises user input by round-tripping it through
`stringify()` before storing it, or that displays a canonical form back to the user — the stored
or displayed schedule is then not the one that was requested.

### Suggested fix

Make the two notions agree. Either:

1. Have `stringifyField` preserve non-wildcard-ness — emit `0-6` rather than `*` when
   `isWildcard` is false — so the flag survives the round trip; or
2. Derive `isWildcard` from the values rather than from the raw text, so a field covering its
   whole range is a wildcard however it was written. This also makes `0 0 16 * 0-6` and
   `0 0 16 * *` behave alike, which is arguably the less surprising reading.

Option 2 changes existing behaviour for expressions that spell out a full range; option 1 is
conservative but keeps the two spellings distinct.

### Notes

Found while porting the library to Go. The port checks a property over randomly generated
expressions — that an expression and its rendering fire at identical instants — and this class of
input violates it. The Go port reproduces the behaviour faithfully for compatibility, with a test
that pins it and points at this report.
