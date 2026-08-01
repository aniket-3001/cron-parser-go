**Title:** DST compensation assumes every transition is exactly one hour, breaking `Antarctica/Troll`

**Version:** 5.6.2 (`aeb2a1513fd33365a6414f4137516c9482f831ed`)

---

### Summary

`CronDate.applyDateOperation()` detects a spring-forward transition by checking whether the hour
advanced by exactly 2:

```ts
// src/CronDate.ts:559
const diff = currentHour - previousHour;
if (diff === 2) {
  if (hoursLength !== 24) {
    this.dstStart = previousHour + 1;
  }
}
```

This encodes the assumption that every DST gap is one hour wide. Not all are.
`Antarctica/Troll` jumps **two hours** (UTC+0 → UTC+2), so stepping across the transition produces
`diff === 3` and the branch never fires. `dstStart` is left `null`, and the compensation in
`CronExpression.#matchHour()` that depends on it never runs.

### Reproduction

```js
const { DateTime } = require('luxon');

let t = DateTime.fromISO('2026-03-28T23:30', { zone: 'Antarctica/Troll' });
for (let i = 0; i < 3; i++) {
  console.log(t.toISO(), 'offset=', t.offset);
  t = t.plus({ hours: 1 }).startOf('hour');
}
```

```
2026-03-28T23:30:00.000+00:00 offset= 0
2026-03-29T00:00:00.000+00:00 offset= 0
2026-03-29T03:00:00.000+02:00 offset= 120     <-- hour goes 0 -> 3, diff === 3
```

For comparison, `America/New_York` on `2026-03-08` goes `01:00 → 03:00`, i.e. `diff === 2`, and the
branch fires as intended.

### Expected vs actual

`dstStart` should record the skipped hour(s) for any transition. For `Antarctica/Troll` it stays
`null`, so an expression scheduled inside the skipped window is not compensated the way it is in
one-hour zones.

The consequence is an occurrence that is **silently skipped** rather than shifted:

```js
// America/New_York, 1-hour gap 02:00-03:00 on 2026-03-08. Schedule 02:30 daily.
//   2026-03-07 02:30
//   2026-03-08 03:30   <- shifted past the gap, still runs
//   2026-03-09 02:30

// Antarctica/Troll, 2-hour gap 00:00-03:00 on 2026-03-29. Schedule 01:30 daily.
//   2026-03-28 01:30
//   2026-03-30 01:30   <- 29 March never appears
//   2026-03-31 01:30
```

For a daily job that is one missed execution rather than a late one, which is the harder failure
to notice. This was found after filing and added to the issue as a follow-up comment.

### Suggested fix

Infer the transition from the UTC offset rather than from the wall-clock hour delta:

```ts
const previousOffset = /* offset before the operation */;
this.invokeDateOperation(op, unit);
const currentOffset = this.getUTCOffset();
const shiftMinutes = currentOffset - previousOffset;
if (shiftMinutes > 0) { /* spring forward, of any width */ }
```

This also covers zones whose transitions are not whole hours — `Australia/Lord_Howe` shifts by
30 minutes, and `Pacific/Chatham` sits at `+12:45`.

### Notes

Found while porting the library to Go for a code-port exercise; the divergence surfaced when the
Go implementation handled the two-hour gap and the reference did not.
