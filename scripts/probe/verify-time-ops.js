/*
 * Differential check of the Go time layer (cron/time.go) against luxon.
 *
 * Every operation CronDate exposes is applied to tens of thousands of starting
 * instants -- weighted toward DST transitions, month ends and leap days -- in
 * both implementations, and the resulting instants are compared exactly.
 *
 * The luxon call chains below are copied from src/CronDate.ts. They must stay in
 * step with it: if the original changes, this is where the divergence shows up.
 *
 * Usage:
 *     CRON_GEN_CORPUS=1 go test -run TestGenerateTimeOpCorpus ./cron/
 *     node scripts/probe/verify-time-ops.js
 */
'use strict';

const fs = require('fs');
const path = require('path');

const LUXON = path.join(__dirname, '..', '..', '..', 'cron-parser', 'node_modules', 'luxon');
let DateTime;
try {
  ({ DateTime } = require(LUXON));
} catch {
  console.error(`Could not load luxon from ${LUXON}`);
  console.error('The reference clone must be present at ../cron-parser with node_modules installed.');
  process.exit(2);
}

const corpusPath = path.join(__dirname, 'time-op-corpus.json');
if (!fs.existsSync(corpusPath)) {
  console.error(`Corpus not found at ${corpusPath}`);
  console.error('Generate it:  CRON_GEN_CORPUS=1 go test -run TestGenerateTimeOpCorpus ./cron/');
  process.exit(2);
}

// Exactly the chains in src/CronDate.ts. `arg` is reduced the same way the Go
// generator reduces it, so both sides use identical operands.
const OPS = {
  startOfMonth:  (d) => d.startOf('month'),
  startOfDay:    (d) => d.startOf('day'),
  startOfHour:   (d) => d.startOf('hour'),
  startOfMinute: (d) => d.startOf('minute'),
  startOfSecond: (d) => d.startOf('second'),
  endOfMonth:    (d) => d.endOf('month'),
  endOfDay:      (d) => d.endOf('day'),
  endOfHour:     (d) => d.endOf('hour'),
  endOfMinute:   (d) => d.endOf('minute'),

  addYear:   (d) => d.plus({ years: 1 }),
  addMonth:  (d) => d.plus({ months: 1 }).startOf('month'),
  addDay:    (d) => d.plus({ days: 1 }).startOf('day'),
  addHour:   (d) => d.plus({ hours: 1 }).startOf('hour'),
  addMinute: (d) => d.plus({ minutes: 1 }).startOf('minute'),
  addSecond: (d) => d.plus({ seconds: 1 }),

  subtractYear:   (d) => d.minus({ years: 1 }),
  subtractMonth:  (d) => d.minus({ months: 1 }).endOf('month').startOf('second'),
  subtractDay:    (d) => d.minus({ days: 1 }).endOf('day').startOf('second'),
  subtractHour:   (d) => d.minus({ hours: 1 }).endOf('hour').startOf('second'),
  subtractMinute: (d) => d.minus({ minutes: 1 }).endOf('minute').startOf('second'),
  subtractSecond: (d) => d.minus({ seconds: 1 }),

  setHour:       (d, a) => d.set({ hour: a % 24 }),
  setMinute:     (d, a) => d.set({ minute: a % 60 }),
  setSecond:     (d, a) => d.set({ second: a % 60 }),
  setDayOfMonth: (d, a) => d.set({ day: (a % 31) + 1 }),
  setMonth:      (d, a) => d.set({ month: (a % 12) + 1 }),
  setYear:       (d, a) => d.set({ year: 2020 + (a % 10) }),
  setWeekday:    (d, a) => d.set({ weekday: (a % 7) + 1 }),
};

const cases = JSON.parse(fs.readFileSync(corpusPath, 'utf8'));

let checked = 0;
const failures = [];
const byOp = new Map();
const failuresByOp = new Map();

for (const c of cases) {
  const fn = OPS[c.op];
  if (!fn) {
    console.error(`No luxon mapping for op ${c.op}`);
    process.exit(2);
  }

  // Absolute instants are unambiguous, so both sides start from the same place
  // and only the operation is under test.
  const start = DateTime.fromMillis(c.anchorMs, { zone: c.zone });
  const result = fn(start, c.arg);

  checked++;
  byOp.set(c.op, (byOp.get(c.op) || 0) + 1);

  if (!result.isValid) {
    failures.push({ ...c, note: `luxon invalid: ${result.invalidReason}` });
    failuresByOp.set(c.op, (failuresByOp.get(c.op) || 0) + 1);
    continue;
  }

  if (result.toMillis() !== c.resultMs) {
    failuresByOp.set(c.op, (failuresByOp.get(c.op) || 0) + 1);
    failures.push({
      ...c,
      startISO: start.toISO(),
      luxonISO: result.toISO(),
      goISO: DateTime.fromMillis(c.resultMs, { zone: c.zone }).toISO(),
      deltaMinutes: (c.resultMs - result.toMillis()) / 60000,
    });
  }
}

console.log(`checked ${checked.toLocaleString()} operations across ${byOp.size} ops`);

if (failures.length === 0) {
  console.log('\nPASS - Go and luxon agree on every operation.');
  process.exit(0);
}

console.log(`\nFAIL - ${failures.length} divergence(s)\n`);
console.log('by operation:');
for (const [op, n] of [...failuresByOp].sort((a, b) => b[1] - a[1])) {
  console.log(`  ${op.padEnd(16)} ${n} / ${byOp.get(op)}`);
}

console.log('\nsamples:');
for (const f of failures.slice(0, 20)) {
  console.log(`  ${f.zone}  ${f.op}(${f.arg})`);
  console.log(`      from : ${f.startISO}`);
  console.log(`      luxon: ${f.luxonISO ?? f.note}`);
  console.log(`      go   : ${f.goISO ?? '-'}   delta=${f.deltaMinutes ?? '-'} min`);
}
if (failures.length > 20) console.log(`  ... and ${failures.length - 20} more`);
process.exit(1);
