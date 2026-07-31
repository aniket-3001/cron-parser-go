**Title:** `#parseNthDay` character class `[,-/]` is an unintended range and matches `.`

**Version:** 5.6.2 (`aeb2a1513fd33365a6414f4137516c9482f831ed`)

---

### Summary

```ts
// src/CronExpressionParser.ts:444
const matches = val.match(/([,-/])/);
```

Inside a character class, `,-/` is parsed as a **range** from `,` (`0x2C`) to `/` (`0x2F`), which
covers four characters: `,` `-` `.` `/`. The `.` is almost certainly unintended — the guard exists
to reject list/range/step syntax combined with `#`, and `.` is not cron syntax at all.

### Reproduction

The library's own error message reveals the range:

```js
const parser = require('cron-parser');

parser.parse('0 0 * * 1.2#2');
// Error: Constraint error, invalid dayOfWeek `#` and `.` special characters are incompatible
//                                                    ^^^ matched by the unintended range
```

Compare with the intended characters:

```js
parser.parse('0 0 * * 1,2#2');  // ... and `,` ...   (intended)
parser.parse('0 0 * * 1-2#2');  // ... and `-` ...   (intended)
```

### Expected vs actual

`1.2#2` is invalid input either way, so the practical impact is only that the rejection happens for
the wrong reason and the message names a character the author never meant to list. The concern is
that the character class does not say what it appears to say, which is a hazard for future edits.

### Suggested fix

Escape the hyphen so the three characters are matched literally:

```ts
const matches = val.match(/([,\-/])/);
```

or reorder so the hyphen is unambiguous:

```ts
const matches = val.match(/([-,/])/);
```

### Notes

Found while porting the library to Go — Go's `regexp` package requires the same escaping, and
transcribing the class literally raised the question of whether the range was deliberate.
