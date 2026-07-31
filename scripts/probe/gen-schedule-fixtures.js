/*
 * Generates cron/testdata/schedule-fixtures.json: for each (expression, zone,
 * starting instant), the sequence of instants the reference implementation
 * produces going forwards and backwards, plus includesDate answers.
 *
 * This is the engine's primary correctness check. Starting instants are weighted
 * toward daylight-saving transitions and month boundaries, where the search loop
 * and the DST compensation actually differ from the naive implementation.
 *
 * Usage:  node scripts/probe/gen-schedule-fixtures.js
 */
'use strict';

const fs = require('fs');
const path = require('path');

const REF = path.join(__dirname, '..', '..', '..', 'cron-parser', 'dist', 'index.js');
let CronExpressionParser;
try {
  ({ CronExpressionParser } = require(REF));
} catch {
  console.error(`Could not load the reference build from ${REF}`);
  console.error('Run `npm run build` in ../cron-parser first.');
  process.exit(2);
}

const ITERATIONS = 8;

const expressions = [
  '* * * * *',
  '0 * * * *',
  '0 0 * * *',
  '30 2 * * *',          // the classic DST casualty
  '0 3 * * *',
  '*/15 * * * *',
  '*/15 9-17 * * 1-5',
  '0 0 1 * *',
  '0 0 L * *',           // last day of month
  '0 0 * * 5L',          // last Friday
  '0 0 * * MON#2',       // second Monday
  '0 0 13 * 5',          // the 13th OR any Friday - rule 1 is OR
  '0 0 29 2 *',          // leap day only
  '0 0 31 * *',          // months with 31 days
  '0 12 * * 0',
  '15,45 * * * *',
  '0 0 1 1 *',
  '* * 29 2 *',
  '0 0,12 * * *',
  '5 0 * 8 *',
  '0 22 * * 1-5',
  '23 0-20/2 * * *',
  '0 0-23/6 * * *',
  '*/5 * * * * *',       // six-field, every 5 seconds
  '0 0 0 * * *',
  '30 30 3 * * *',
];

// Zones chosen for the shape of their transitions.
const zones = [
  'UTC',
  'America/New_York',    // 1h
  'Europe/London',       // 1h, different dates
  'Asia/Kolkata',        // +05:30, no DST
  'Australia/Lord_Howe', // 30min
  'Antarctica/Troll',    // 2h
  'Pacific/Chatham',     // +12:45
  'America/Santiago',    // midnight transition
];

// Instants just before real transitions, plus ordinary and boundary dates.
const starts = [
  '2026-03-08T05:00:00Z', // New York spring forward
  '2026-11-01T04:00:00Z', // New York fall back
  '2026-03-29T00:00:00Z', // Troll and London spring forward
  '2026-10-04T14:00:00Z', // Lord Howe spring forward
  '2026-09-05T22:00:00Z', // Santiago midnight transition
  '2024-02-28T12:00:00Z', // leap year boundary
  '2026-02-27T12:00:00Z', // non-leap February end
  '2026-01-31T23:30:00Z', // month end
  '2026-12-31T23:00:00Z', // year end
  '2026-07-15T09:37:42Z', // an ordinary instant
];

function sequence(expr, tz, currentDate, reverse) {
  const out = [];
  let error = null;
  try {
    const it = CronExpressionParser.parse(expr, { currentDate: new Date(currentDate), tz });
    for (let i = 0; i < ITERATIONS; i++) {
      out.push((reverse ? it.prev() : it.next()).toISOString());
    }
  } catch (err) {
    error = err.message;
  }
  return { values: out, error };
}

const cases = [];
for (const expr of expressions) {
  for (const tz of zones) {
    for (const currentDate of starts) {
      const fwd = sequence(expr, tz, currentDate, false);
      const rev = sequence(expr, tz, currentDate, true);

      // includesDate is checked against the instants the schedule itself
      // produced, plus one second either side of the first of them.
      const includes = [];
      if (fwd.values.length > 0) {
        try {
          const it = CronExpressionParser.parse(expr, { currentDate: new Date(currentDate), tz });
          const first = new Date(fwd.values[0]);
          for (const probe of [
            first.toISOString(),
            new Date(first.getTime() + 1000).toISOString(),
            new Date(first.getTime() - 1000).toISOString(),
          ]) {
            includes.push({ date: probe, result: it.includesDate(new Date(probe)) });
          }
        } catch {
          // includesDate can throw for the malformed L form; skip those.
        }
      }

      cases.push({
        expression: expr,
        tz,
        currentDate,
        next: fwd.values,
        nextError: fwd.error,
        prev: rev.values,
        prevError: rev.error,
        includes,
      });
    }
  }
}

// Bounded iteration: start and end windows.
const bounded = [];
for (const [expr, tz, currentDate, startDate, endDate] of [
  ['0 0 * * *', 'UTC', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-05T00:00:00Z'],
  ['0 0 * * *', 'UTC', '2026-01-10T00:00:00Z', '2026-01-01T00:00:00Z', '2026-01-05T00:00:00Z'],
  ['0 * * * *', 'America/New_York', '2026-03-08T04:00:00Z', '2026-03-08T00:00:00Z', '2026-03-09T00:00:00Z'],
  ['0 0 * * *', 'UTC', '2026-06-01T00:00:00Z', null, '2026-06-03T00:00:00Z'],
  ['0 0 * * *', 'UTC', '2026-06-01T00:00:00Z', '2026-05-30T00:00:00Z', null],
]) {
  const opts = { currentDate: new Date(currentDate), tz };
  if (startDate) opts.startDate = new Date(startDate);
  if (endDate) opts.endDate = new Date(endDate);

  const values = [];
  let error = null;
  try {
    const it = CronExpressionParser.parse(expr, opts);
    for (let i = 0; i < ITERATIONS; i++) values.push(it.next().toISOString());
  } catch (err) {
    error = err.message;
  }
  bounded.push({ expression: expr, tz, currentDate, startDate, endDate, next: values, nextError: error });
}

const fixtures = {
  note: 'Generated from harrisiirak/cron-parser v5.6.2 by scripts/probe/gen-schedule-fixtures.js. Do not edit by hand.',
  iterations: ITERATIONS,
  cases,
  bounded,
};

const out = path.join(__dirname, '..', '..', 'cron', 'testdata', 'schedule-fixtures.json');
fs.mkdirSync(path.dirname(out), { recursive: true });
fs.writeFileSync(out, JSON.stringify(fixtures) + '\n');

const errs = cases.filter((c) => c.nextError).length;
console.log(`wrote ${cases.length} schedule cases (${errs} with a forward error) + ${bounded.length} bounded`);
console.log(`   size: ${(fs.statSync(out).size / 1024 / 1024).toFixed(2)} MB`);
console.log(`-> ${out}`);
