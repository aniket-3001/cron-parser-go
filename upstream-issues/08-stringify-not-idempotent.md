**Title:** `stringify()` is not idempotent for day-of-month when a single month is named

**Version:** 5.6.2 (`aeb2a1513fd33365a6414f4137516c9482f831ed`)

---

### Summary

Rendering an expression and re-rendering the result can produce different text. The output
changes once and then settles, so `stringify()` is not idempotent:

```
* * 9-20/3 16-26/5 jun *   ->   * * 9-18/3 16/5 6 *
* * 9-18/3 16/5 6 *        ->   * * 9-18/3 16-31/5 6 *
* * 9-18/3 16-31/5 6 *     ->   * * 9-18/3 16-31/5 6 *   (settles)
```

### Reproduction

```js
const parser = require('cron-parser');

let e = '* * 9-20/3 16-26/5 jun *';
for (let i = 0; i < 3; i++) {
  const x = parser.parse(e);
  const s = x.stringify(true);
  console.log(e, '->', s, JSON.stringify(x.fields.dayOfMonth.values));
  if (s === e) break;
  e = s;
}
```

```
* * 9-20/3 16-26/5 jun *  ->  * * 9-18/3 16/5 6 *          [16,21,26]
* * 9-18/3 16/5 6 *       ->  * * 9-18/3 16-31/5 6 *       [16,21,26,31]
* * 9-18/3 16-31/5 6 *    ->  * * 9-18/3 16-31/5 6 *       [16,21,26,31]
```

### Why it happens

`stringifyField` narrows the day-of-month maximum to the named month's length:

```ts
if (field instanceof CronDayOfMonth) {
  max = this.#month.values.length === 1 ? CronMonth.daysInMonth[this.#month.values[0] - 1] : field.max;
}
```

June has 30 days, so `[16, 21, 26]` reaches `max - step + 1` and is rendered in the open form
`16/5`.

Parsing does not apply the same narrowing. `#parseRepeat` expands a bare start against the
**field's** maximum of 31:

```ts
if (!atoms[0].includes('-')) {
  atoms[0] = `${atoms[0]}-${constraints.max}`;   // 16-31, not 16-30
}
```

So `16/5` comes back as `[16, 21, 26, 31]`, which no longer reaches the narrowed maximum and is
rendered in the closed form `16-31/5`.

### Impact

The **schedule is unaffected**: 31 June does not exist, so the extra value never fires. The
problem is that the rendered text and the field values are not stable, which matters for anything
that treats `stringify()` output as canonical — deduplicating schedules by their rendered form,
diffing configuration, or using the output as a cache key will see two spellings of one schedule.

It is also surprising that `parse(x.stringify())` has more day-of-month values than `x`.

### Suggested fix

Make the two sides agree on the maximum. Either have `#parseRepeat` narrow to the month's length
when exactly one month is named, or have `stringifyField` use the field's own maximum of 31 so
that the open form is only emitted when it round-trips.

The second is the smaller change and keeps parsing context-free.

### Notes

Found while porting the library to Go, by checking a property over randomly generated
expressions: that rendering is a fixed point after one application. The Go port reproduces the
behaviour, with a test pinning the two-step convergence.
