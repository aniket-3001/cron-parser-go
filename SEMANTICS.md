# SEMANTICS, the exact behaviour the Go port must reproduce

A line-level behavioural specification of `cron-parser` v5.6.2, extracted from source and
**verified by execution**. This is the contract the Go implementation is written against; where
`DESIGN.md` says *how*, this says *what*.

Anything surprising is marked. Those are the places a reasonable port silently diverges.

---

# 1. The parse pipeline

`CronExpressionParser.parse(expression, options)`, exact order of operations:

```
1.  rand = seededRandom(options.hashSeed)      // seeded ONCE per parse, shared by all fields
2.  expression = PredefinedExpressions[expression] ?? expression
3.  rawFields  = #getRawFields(expression, strict)
4.  strict check: dayOfMonth and dayOfWeek cannot both be non-'*'
5.  #parseField for second, minute, hour, month, dayOfMonth   (in that order)
6.  #parseNthDay(rawFields.dayOfWeek) -> { dayOfWeek, nthDayOfWeek }
7.  #parseField for dayOfWeek
8.  construct the six CronField objects, each with { rawValue, nthDayOfWeek }
9.  new CronFieldCollection(...) -> new CronExpression(fields, options)
```

**Step 1 matters.** One PRNG instance is threaded through every field, so `H` values in
different fields are correlated by position. Re-seeding per field would produce different output
for the same seed.

**Step 5's ordering matters** for the same reason: the PRNG is consumed in field order:
second, minute, hour, month, dayOfMonth, then dayOfWeek. Note this is **not** the display order:
month is parsed *before* dayOfMonth.

## 1.1 Field padding

```ts
expression = expression || '0 * * * * *';       // empty string -> every minute (non-strict)
const atoms = expression.trim().split(/\s+/);
if (strict && atoms.length < 6) throw 'Invalid cron expression, expected 6 fields';
if (atoms.length > 6)           throw 'Invalid cron expression, too many fields';
const defaults = ['*', '*', '*', '*', '*', '0'];
if (atoms.length < defaults.length) atoms.unshift(...defaults.slice(atoms.length));
const [second, minute, hour, dayOfMonth, month, dayOfWeek] = atoms;
```

**`defaults.slice(atoms.length)` takes from the END, and `unshift` prepends.** For 5 atoms this
prepends `'0'` (seconds), which is the intended behaviour. But for fewer atoms it produces
nonsense that fails later with a confusing message. Verified:

| Input | Result |
|---|---|
| `"* * * * *"` (5) | → `0 * * * * *` |
| `"* * * * * *"` (6) | → `* * * * * *` |
| `"5"` (1) | → `Constraint error, got value 0 expected range 1-12`, the lone atom lands in **dayOfWeek** and `'0'` lands in **month** |
| `"5 *"` (2) | → `Constraint error, got value 0 expected range 1-31` |

The Go port reproduces this, including the misleading error.

## 1.2 Predefined expressions

All stored in **6-field** form. `@yearly`/`@annually` are distinct keys with identical values.

| Alias | Expansion | | Alias | Expansion |
|---|---|---|---|---|
| `@yearly` / `@annually` | `0 0 0 1 1 *` | | `@minutely` | `0 * * * * *` |
| `@monthly` | `0 0 0 1 * *` | | `@secondly` | `* * * * * *` |
| `@weekly` | `0 0 0 * * 0` | | `@weekdays` | `0 0 0 * * 1-5` |
| `@daily` | `0 0 0 * * *` | | `@weekends` | `0 0 0 * * 0,6` |
| `@hourly` | `0 0 * * * *` | | | |

The last four are **non-standard extensions**: no Unix cron has `@minutely`.

---

# 2. Field parsing

`#parseField(field, value, constraints, rand)`:

```
1. if field is Month or DayOfWeek: replace /[a-z]{3}/gi with the numeric alias
      unknown 3-letter run  ->  throw `Validation error, cannot resolve alias "${match}"`
2. if !constraints.validChars.test(value)  ->  throw `Invalid characters, got value: ${value}`
3. value = #parseWildcard(value)   //  [*?] -> `${min}-${max}`
4. value = #parseHashed(value, constraints, rand)
5. return #parseSequence(field, value, constraints)
```

**The alias regex is `/[a-z]{3}/gi`, any three consecutive letters.** It is applied to Month
and DayOfWeek only. `SUN` → `0`, `JAN` → `1`. An unrecognised triple throws.

**`*` and `?` are both expanded to the full range** before sequence parsing, so `?` is a synonym
for `*` at the value level. The distinction survives only via `hasQuestionMarkChar`, which
`stringifyField` uses to re-emit `?` instead of `*`.

## 2.1 Sequence → repeat → range

```
#parseSequence:  split on ','   (empty atom -> throw 'Invalid list value format')
#parseRepeat:    split on '/'
                   > 2 atoms          -> throw `Invalid repeat: ${val}`
                   2 atoms, no '-'    -> atoms[0] = `${atoms[0]}-${constraints.max}`
                   -> #parseRange(field, atoms[0], parseInt(atoms[1]), constraints)
                   1 atom             -> #parseRange(field, val, 1, constraints)
#parseRange:     split on '-'
                   <= 1 atom -> return isNaN(+val) ? val : +val      // string passthrough
                   validate range, validate repeat interval, #createRange
```

**`5/10` becomes `5-59/10`**, not "every 10th starting at 5 forever". The max comes from the
field's own constraint.

**`#parseRange` returns a raw string when the value isn't numeric**. This is how `L`, `5L` and
`W` survive parsing as strings. They then get validated by `#isValidConstraintChar`.

## 2.2 `#createRange` and the day-of-week anomaly 

```ts
static #createRange(field, min, max, repeatInterval) {
  const stack = [];
  if (field === CronUnit.DayOfWeek && max % 7 === 0) stack.push(0);
  for (let index = min; index <= max; index += repeatInterval) {
    if (stack.indexOf(index) === -1) stack.push(index);
  }
  return stack;
}
```

And in `#parseSequence`'s `handleResult`:

```ts
if (Array.isArray(result)) { stack.push(...result); }        // NO modulo
else { ... stack.push(field === CronUnit.DayOfWeek ? v % 7 : result); }   // modulo
```

**The `% 7` normalisation is applied to single values but NOT to ranges.** Combined with the
`max % 7 === 0` rule, this produces a Sunday that is represented **twice**. Verified against
v5.6.2:

| Expression | `dayOfWeek.values` | `stringify()` |
|---|---|---|
| `* * * * 7` | `[0]` | `* * * * 0` |
| `* * * * 0` | `[0]` | `* * * * 0` |
| `* * * * 6,7` | `[0,6]` | `* * * * 0,6` |
| `* * * * 5-7` | **`[0,5,6,7]`** | `* * * * 0,5,6` |
| `* * * * 1-7` | **`[0,1,2,3,4,5,6,7]`** | `* * * * *` |
| `* * * * 0-7` | **`[0,1,2,3,4,5,6,7]`** | `* * * * *` |

So `5-7` means **Sunday, Friday, Saturday**, the `0` is injected by the `max % 7` rule while `7`
remains in the array. Both denote Sunday.

This is why `stringifyField` special-cases day-of-week:

```ts
if (field instanceof CronDayOfWeek) {
  max = 6;
  values = dayOfWeek[dayOfWeek.length - 1] === 7 ? dayOfWeek.slice(0, -1) : dayOfWeek;
}
```

**Port requirement:** reproduce the duplicate representation exactly. A port that "cleans up" by
normalising 7→0 everywhere produces `[0,5,6]`, which stringifies identically but has a different
`values` array, and 19 tests read `.values` directly.

## 2.3 Hashed (`H`) values

`#parseHashed` calls `rand()` **once per field**, then replaces every
`H(?:\((\d+)-(\d+)\))?(?:\/(\d+))?` occurrence using that single random value.

| Form | Result |
|---|---|
| `H` | `floor(r * (max - min + 1) + min)` |
| `H(a-b)` | `floor(r * (b - a + 1) + a)` |
| `H/step` | values from `floor(min/step)*step + floor(r*step)`, stepping by `step`, `>= min`, `<= max` |
| `H(a-b)/step` | same but bounded by `max(a, min)` … `b` |

Errors: `Invalid range: ${min}-${max}, min > max` and `Invalid step: ${step}, must be positive`.

Using `H` without `options.hashSeed` still works: `seededRandom(undefined)` falls back to
`Math.floor(Math.random() * 10_000_000_000)`, making the result **non-deterministic**.

## 2.4 `#parseNthDay`

```ts
const atoms = val.split('#');
if (atoms.length <= 1) return { dayOfWeek: atoms[0] };
const nthValue = +atoms[atoms.length - 1];
const matches = val.match(/([,-/])/);            // BUG: 0x2C-0x2F range = , - . /
if (matches !== null) throw `Constraint error, invalid dayOfWeek \`#\` and \`${matches[0]}\` special characters are incompatible`;
if (!(atoms.length <= 2 && !isNaN(nthValue) && nthValue >= 1 && nthValue <= 5))
  throw 'Constraint error, invalid dayOfWeek occurrence number (#)';
return { dayOfWeek: atoms[0], nthDayOfWeek: nthValue };
```

`[,-/]` inside a character class is the **range** `0x2C`–`0x2F` = `,` `-` `.` `/`. The `.` is
unintended. Verified: `0 0 * * 1.2#2` throws with `` `.` `` in the message.

---

# 3. Field objects

## 3.1 Construction

```ts
this.#values         = values.sort(CronField.sorter);          // in-place, mutates caller
this.#wildcard       = options.wildcard ?? this.#isWildcardValue();
this.#hasLastChar    = options.rawValue.includes('L') || values.includes('L');
this.#hasQuestionMarkChar = options.rawValue.includes('?') || values.includes('?');
```

`hasLastChar` tests the **raw string**, so any `L` anywhere sets it, including the `L` in a
resolved alias. (Month aliases resolve to numbers before this point, so in practice only
day-of-month and day-of-week are affected.)

## 3.2 `sorter`, mixed-type ordering

```
both numbers  -> a - b
both strings  -> a.localeCompare(b)
mixed         -> numbers first
```

## 3.3 `#isWildcardValue`

```
if rawValue is non-empty  -> rawValue is '*' or '?'
else                      -> values covers the entire min..max range
```

The second branch matters for `fieldsToExpression`, where fields are built without a raw value.
`new CronHour([0..23])` is therefore a wildcard even though no `*` was written.

## 3.4 `validate()`

```
numeric value -> min <= v <= max
string value  -> matches /^\d{0,2}${char}$/ for some char in this.chars
```

So `L`, `5L`, `15L` all validate against `chars = ['L']`; `123L` does not.

Then a duplicate check: `values.find((v,i) => values.indexOf(v) !== i)`.

**The duplicate check uses a falsy test** (`if (duplicate)`), so a duplicated **`0`** is not
reported. `[0,0]` passes validation; `[1,1]` throws.

## 3.5 Field constraints

| Field | min | max | chars | `validChars` extra |
|---|---:|---:|---|---|
| `CronSecond` | 0 | 59 | | base |
| `CronMinute` | 0 | 59 | | base |
| `CronHour` | 0 | 23 | | base |
| `CronDayOfMonth` | 1 | 31 | `L` | adds `L` |
| `CronMonth` | 1 | 12 | | base |
| `CronDayOfWeek` | 0 | 7 | `L` | adds `L` and `#` |

Base `validChars`: `/^[?,*\dH/-]+$|^.*H\(\d+-\d+\)\/\d+.*$|^.*H\(\d+-\d+\).*$|^.*H\/\d+.*$/`

**No field's `validChars` contains `W`**, see §7.

---

# 4. Collection-level validation

```ts
if (month.values.length === 1 && !dayOfMonth.hasLastChar && dayOfWeek.isWildcard) {
  if (!(parseInt(dayOfMonth.values[0]) <= CronMonth.daysInMonth[month.values[0] - 1]))
    throw 'Invalid explicit day of month definition';
}
```

Only checks `values[0]` (the **first** day-of-month value) and only when exactly one month is
specified and day-of-week is a wildcard. So `0 0 30,31 2 *` throws (first value 30 > 29) but
`0 0 1,31 2 *` is **accepted** and then never matches, eventually hitting the loop limit.

`daysInMonth` is `[31, 29, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]`, February is **29**, i.e.
leap-permissive.

---

# 5. Iteration

## 5.1 `#findSchedule` order

```
loop (max LOOP_LIMIT = 10000):
  validateTimeSpan                       -> throw 'Out of the time span range'
  !matchDayOfMonth  -> applyDateOperation(verb, Day);   continue
  !matchMonth       -> applyDateOperation(verb, Month); continue
  !matchHour        -> (matchHour mutates internally);  continue
  !matchMinute      -> moveToNextMinute;                continue
  !matchSecond      -> moveToNextSecond;                continue
  if startTimestamp === current:
      if verb === 'Add' || milliseconds === 0: applyDateOperation(verb, Second); continue
  break
if stepCount >= LOOP_LIMIT -> throw 'Invalid expression, loop limit exceeded'
milliseconds -> 0
```

**Day is matched before Month.** A port that checks month first will produce different
`applyDateOperation` call sequences (which two tests observe) even when results agree.

**Results are exclusive of the start instant**, landing exactly on `currentDate` steps one
second and keeps searching. The exception is a *backwards* search that began at a sub-second
offset, which accepts the millisecond-stripped value instead.

## 5.2 Day-of-month / day-of-week, the three rules

```
Rule 1: DOM restricted AND DOW restricted  -> match if (DOM matched OR DOW matched)
Rule 2: DOM matched AND DOW not restricted -> match
Rule 3: DOM wildcard AND DOW not wildcard AND DOW matched -> match
otherwise -> no match
```

Rule 1 is **OR**, inherited from Unix cron. `0 0 13 * 5` is "the 13th **or** any Friday", not
"Friday the 13th".

`matchedDOM` also succeeds when `dayOfMonth.hasLastChar && currentDate.isLastDayOfMonth()`.
`matchedDOW` additionally honours the `#` nth-occurrence constraint and the `L` last-weekday form.

## 5.3 `isLastWeekdayOfMonth`

```ts
return day > lastDay - 7;
```

i.e. "within the final 7 days of the month", combined with a weekday match, that identifies the
last occurrence of that weekday.

`#isLastWeekdayOfMonthMatch` does `parseInt(expression.toString().charAt(0), 10) % 7`. For a
bare `'L'` that is `NaN`, and the function throws at **iteration** time, not parse time.

## 5.4 DST compensation

`applyDateOperation(op, unit, hoursLength)`:

```
Month or Day        -> plain operation, no DST bookkeeping
otherwise:
   diff = hourAfter - hourBefore
   diff === 2                                   -> if hoursLength !== 24: dstStart = hourBefore + 1
   diff === 0 && minutes === 0 && seconds === 0  -> if hoursLength !== 24: dstEnd = hourAfter
```

`diff === 2` assumes every transition is exactly one hour. Measured failures:

| Zone | Gap | `diff` | Fires? |
|---|---|---|---|
| `America/New_York` | 1 h | 2 | |
| `Australia/Lord_Howe` | 30 min | 2 | (incidentally) |
| `Antarctica/Troll` | **2 h** | **3** | **bug** |

---

# 6. Stringify

`compactField(input)` folds a sorted value list into ranges by detecting arithmetic runs, emitting
`{start, count, end?, step?}`. Then:

- exactly one range → try `#handleSingleRange`: `*` (or `?` when `hasQuestionMarkChar`) when
  `step === 1 && start === min && end >= max`; `*/step` when `start === min && end >= max-step+1`
- otherwise → per range: `count === 1` emits the bare start, else `#handleMultipleRanges`

`#handleMultipleRanges` emits `a-b` for step 1; for other steps it emits either an explicit
comma list (when `step * multiplier > end`) or `a/step` / `a-b/step`.

Field-specific overrides in `stringifyField`:
- **DayOfWeek**: `max` forced to `6`; a trailing `7` is dropped; `#nthDay` appended when `> 0`
- **DayOfMonth**: `max` becomes that month's length when exactly one month is specified

`stringify(includeSeconds = false)`, **seconds are omitted by default**, so the default output is
the 5-field form. `toString()` returns `options.expression` (the raw input) when available.

---

# 7. `W`, a phantom feature

- `CronChars = 'L' | 'W'` (`fields/types.ts`)
- `compactField` has an explicit `if (item === 'L' || item === 'W')` branch
- 4 tests in `CronFieldCollection.test.ts` exercise it
- **but no `validChars` regex contains `W`**

So `parse('0 0 15W * *')` and `parse('0 0 LW * *')` both throw `Invalid characters, got value: 15W`.
`W` is reachable only by hand-constructing field objects. The stringify path can emit a value the
parse path cannot produce.

---

# 8. Complete error catalogue

Every message the port must reproduce byte-for-byte. 42 tests assert on these.

| Source | Message |
|---|---|
| `CronDate:56` | `CronDate: unhandled timestamp: ${timestamp}` |
| `CronDate:253` | `Invalid verb: ${verb}` |
| `CronExpression:211` | `Invalid last weekday of the month expression: ${expression}` |
| `CronExpression:18` | `Out of the time span range` |
| `CronExpression:23` | `Invalid expression, loop limit exceeded` |
| `Parser:100` | `Cannot use both dayOfMonth and dayOfWeek together in strict mode!` |
| `Parser:161` | `Invalid cron expression` |
| `Parser:166` | `Invalid cron expression, expected 6 fields` |
| `Parser:169` | `Invalid cron expression, too many fields` |
| `Parser:194` | `Validation error, cannot resolve alias "${match}"` |
| `Parser:202` | `Invalid characters, got value: ${value}` |
| `Parser:237,260` | `Invalid range: ${min}-${max}, min > max` |
| `Parser:240,270` | `Invalid step: ${step}, must be positive` |
| `Parser:309` | `Constraint error, got value ${result} expected range ${min}-${max}` |
| `Parser:321` | `Invalid list value format` |
| `Parser:339` | `Invalid repeat: ${val}` |
| `Parser:363` | `Constraint error, got range ${min}-${max} expected range ${cmin}-${cmax}` |
| `Parser:366` | `Invalid range: ${min}-${max}, min(${min}) > max(${max})` |
| `Parser:379` | `Constraint error, cannot repeat at every ${n} time.` |
| `Parser:446` | ``Constraint error, invalid dayOfWeek `#` and `${c}` special characters are incompatible`` |
| `Parser:451` | `Constraint error, invalid dayOfWeek occurrence number (#)` |
| `Collection:143-158` | `Validation error, Field ${name} is missing` |
| `Collection:166` | `Invalid explicit day of month definition` |
| `Collection:350/354/360` | `Unexpected range step` / `end` / `start` |
| `CronField:88` | `${ClassName} Validation error, values is not an array` |
| `CronField:91` | `${ClassName} Validation error, values contains no values` |
| `CronField:240` | `${ClassName} Validation error, got value ${v} expected range ${min}-${max}${charsSuffix}` |
| `CronField:247` | `${ClassName} Validation error, duplicate values found: ${v}` |

`charsSuffix` is `` ` or chars ${chars.join('')}` `` when the field has special chars, else empty.

`${ClassName}` is `this.constructor.name`: `CronSecond`, `CronMinute`, … The Go port carries
these names in the field descriptor (see `DECISIONS.md` D2).

---

# 9. Port checklist

Behaviours that a "sensible" Go implementation would get wrong without being told:

- [ ] Day-of-week `7` normalised for single values but **not** for ranges (§2.2)
- [ ] `max % 7 === 0` injects a `0` into day-of-week ranges (§2.2)
- [ ] Day matched **before** month in the search loop (§5.1)
- [ ] Rule 1 is OR, not AND (§5.2)
- [ ] Single PRNG threaded through all fields, consumed in parse order (§1)
- [ ] `5/10` expands to `5-59/10` (§2.1)
- [ ] `?` behaves as `*` but round-trips back to `?` (§2)
- [ ] Duplicate `0` escapes the duplicate check (§3.4)
- [ ] Collection validation only inspects `values[0]` (§4)
- [ ] `stringify()` omits seconds by default (§6)
- [ ] `W` parses nowhere but stringifies fine (§7)
- [ ] Results exclusive of the start instant (§5.1)
- [ ] All 39 error messages byte-identical (§8)
