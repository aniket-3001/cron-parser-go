**Title:** `CronField` constructor mutates the caller's array via in-place `sort()`

**Version:** 5.6.2 (`aeb2a1513fd33365a6414f4137516c9482f831ed`)

---

### Summary

`CronField`'s constructor sorts the values array in place:

```ts
// src/fields/CronField.ts:99
this.#values = values.sort(CronField.sorter);
```

`Array.prototype.sort()` sorts the receiver and returns the same reference, so constructing any
field **rewrites the array the caller passed in**. The caller has no reason to expect that a
constructor mutates its argument.

### Reproduction

```js
const { CronMinute } = require('cron-parser');

const mine = [30, 10, 20];
new CronMinute(mine);
console.log(mine);   // [10, 20, 30]  <-- caller's array was reordered
```

### Expected vs actual

**Expected:** `mine` is still `[30, 10, 20]`; the field keeps its own sorted copy.
**Actual:** `mine` is reordered in place.

This is most likely to bite code that reuses a values array across several fields, or that holds a
configuration array it expects to remain stable:

```js
const workdayHours = [9, 17, 12];
const a = new CronHour(workdayHours);
// workdayHours is now [9, 12, 17] -- any later use sees the mutated order
```

### Suggested fix

Copy before sorting:

```ts
this.#values = [...values].sort(CronField.sorter);
```

One-line change; no behavioural difference for the field itself, since only the internal copy is
observed afterwards.

### Notes

Found while porting the library to Go. Go slices share backing arrays in the same way, so the naive
translation reproduces the bug — which is what drew attention to it.
