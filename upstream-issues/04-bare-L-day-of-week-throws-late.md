**Title:** Bare `L` in day-of-week parses successfully but throws on `next()`

**Version:** 5.6.2 (`aeb2a1513fd33365a6414f4137516c9482f831ed`)

---

### Summary

An expression whose day-of-week field is a bare `L` passes parsing and validation, then throws the
first time the resulting expression is iterated. The error surfaces far from its cause and is not
catchable at the point a user would naturally validate input.

`#isLastWeekdayOfMonthMatch` takes the first character of the value as the weekday number:

```ts
// src/CronExpression.ts:209
const weekday = parseInt(expression.toString().charAt(0), 10) % 7;
if (Number.isNaN(weekday)) {
  throw new Error(`Invalid last weekday of the month expression: ${expression}`);
}
```

For `'5L'` this yields `5`. For a bare `'L'`, `parseInt('L', 10)` is `NaN`, so it throws — but only
once iteration reaches a candidate date.

### Reproduction

```js
const parser = require('cron-parser');

const expr = parser.parse('0 0 * * L');   // succeeds -- no error
console.log('parsed OK');

expr.next();
// Error: Invalid last weekday of the month expression: L
```

### Expected vs actual

**Expected:** either `parse()` rejects `L` in day-of-week with a clear message, or a bare `L` is
given a defined meaning (e.g. treated as Saturday, as some cron dialects do).
**Actual:** `parse()` succeeds and the error is deferred to `next()`.

This matters because `parse()` is the natural validation boundary. Code that validates
user-supplied crontab entries at parse time will accept `0 0 * * L` and fail later at
scheduling time.

### Suggested fix

Validate during parsing. `CronDayOfWeek` already knows its allowed chars, so the check could
happen in `validate()` — rejecting an `L` that is not preceded by a digit — or `#parseNthDay` /
`#parseField` could reject the bare form explicitly.

### Notes

Found while porting the library to Go, where the equivalent code path had to decide whether to
fail at parse time or reproduce the deferred failure.
