**Title:** Duplicate-value validation silently passes when the duplicated value is `0`

**Version:** 5.6.2 (`aeb2a1513fd33365a6414f4137516c9482f831ed`)

---

### Summary

`CronField.validate()` finds a duplicated value and then tests it for truthiness:

```ts
// src/fields/CronField.ts:245-248
const duplicate = this.#values.find((value, index) => this.#values.indexOf(value) !== index);
if (duplicate) {
  throw new Error(`${this.constructor.name} Validation error, duplicate values found: ${duplicate}`);
}
```

`Array.prototype.find` returns the *value*, so when the duplicate is `0` the guard evaluates
`if (0)` and the error is never thrown. `0` is a legal value for second, minute, hour and
day-of-week, so this is reachable in normal use.

### Reproduction

```js
const { CronSecond } = require('cron-parser');

new CronSecond([1, 1]);   // throws: CronSecond Validation error, duplicate values found: 1
new CronSecond([0, 0]);   // accepted -- no error
new CronSecond([0, 0, 1]); // accepted -- no error
```

### Expected vs actual

**Expected:** `[0, 0]` is rejected exactly as `[1, 1]` is.
**Actual:** duplicates of `0` pass validation, so the field is constructed with a duplicated value
while every other duplicate is rejected. The validation is inconsistent depending on which value
happens to repeat.

### Suggested fix

Test for presence rather than truthiness:

```ts
const duplicate = this.#values.find((value, index) => this.#values.indexOf(value) !== index);
if (duplicate !== undefined) {
  throw new Error(`${this.constructor.name} Validation error, duplicate values found: ${duplicate}`);
}
```

Alternatively use `findIndex(...) !== -1` and read the value from the index.

### Notes

Found while porting the library to Go, where the equivalent check had to be written explicitly and
the truthiness dependence became visible.
