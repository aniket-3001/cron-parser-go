**Title:** `stringify()` throws "Unexpected range step" for a field of repeated zeros

**Version:** 5.6.2 (`aeb2a1513fd33365a6414f4137516c9482f831ed`)

---

### Summary

An expression whose day-of-week field resolves to several zeros parses successfully, and then
`stringify()` throws an internal error that the source marks as unreachable:

```ts
// src/CronFieldCollection.ts:348-351
/* istanbul ignore if */
if (!step) {
  throw new Error('Unexpected range step');
}
```

The `istanbul ignore` comment records the belief that this cannot happen. It can, and only
ordinary input is needed to reach it.

### Reproduction

```js
const parser = require('cron-parser');

const e = parser.parse('0 0 * * 7,0,7');
console.log(e.fields.dayOfWeek.values);   // [ 0, 0, 0 ]

e.stringify();
// Error: Unexpected range step
```

`0 0 * * 0,0,0` behaves identically.

### Why it happens

Three behaviours combine:

1. **Day-of-week normalises with `% 7`**, so `7` and `0` both become `0` and the field ends up
   holding `[0, 0, 0]`.

2. **The duplicate check misses zeros.** `CronField.validate()` does
   `const duplicate = values.find(...); if (duplicate) { throw }`, and `find` returns the *value*,
   which is `0` and therefore falsy. Duplicates of any other value are rejected. (Filed separately
   as the duplicate-zero validation gap.)

3. **`compactField` derives a stride of zero.** With every element equal, `item - prevItem` is `0`,
   so the run is recorded as `{ start: 0, count: 3, end: 0, step: 0 }`. `#handleMultipleRanges`
   then rejects that stride as impossible.

```js
const { CronFieldCollection } = require('cron-parser');
CronFieldCollection.compactField([0, 0, 0]);
// [ { start: 0, count: 3, end: 0, step: 0 } ]
```

### Expected vs actual

**Expected:** either the expression is rejected at parse time as containing duplicates — which is
what happens for `0 0 * * 1,1,1` — or `stringify()` renders it, presumably as `0,0,0` or `0`.

**Actual:** it parses, and then `stringify()` fails with an error describing an internal
invariant, which gives a caller no way to understand what was wrong with their input.

The practical impact is that a schedule stored as `0 0 * * 7,0,7` cannot be round-tripped or
displayed, and code that renders expressions for logging or persistence will throw on input that
`parse()` accepted.

### Suggested fix

Fixing the duplicate check removes the cause: testing `duplicate !== undefined` rather than
`duplicate` would reject `[0, 0, 0]` at construction time, exactly as `[1, 1, 1]` is rejected
today.

Failing that, `compactField` could avoid emitting a zero stride, or `#handleMultipleRanges` could
treat one as a run of identical values.

### Notes

Found while porting the library to Go. The port checks a property over randomly generated
expressions — that rendering an expression and re-parsing it yields the same schedule — and this
input class made rendering fail outright. The Go port reproduces the failure, with a test pinning
the message.
