/*
 * Generates cron/testdata/parse-fixtures.json: for each expression, the field
 * values the reference implementation produces, or the exact error it throws.
 *
 * The Go parser tests assert against this file, so parser equivalence is checked
 * against real captured behaviour rather than against my reading of the source.
 * Unlike the time-op corpus this is small and IS committed, so `go test` alone
 * verifies the parser with no Node required.
 *
 * Usage:  node scripts/probe/gen-parse-fixtures.js
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

// Every expression is parsed with a fixed hashSeed so H forms are deterministic.
const HASH_SEED = 'port-mortem';

const expressions = [
  // --- basic forms -------------------------------------------------------
  '* * * * *', '* * * * * *', '0 0 * * *', '30 * * * *', '0 12 * * *',
  '0 0 1 1 *', '59 23 31 12 *',

  // --- lists, ranges, steps ---------------------------------------------
  '1,3,5 * * * *', '1-5 * * * *', '*/10 * * * *', '10-30/5 * * * *',
  '0 */2 * * *', '5/10 * * * *', '0 0 */3 * *', '*/1 * * * *',
  '0-59 * * * *', '0,30 0,12 * * *', '*/7 * * * *', '2-58/4 * * * *',

  // --- day of week, including the 7/range asymmetry ----------------------
  '* * * * 0', '* * * * 7', '* * * * 0-7', '* * * * 1-7', '* * * * 5-7',
  '* * * * 6,7', '* * * * 1-5', '* * * * 0,6', '* * * * */2',
  '* * * * MON', '* * * * mon', '* * * * MON-FRI', '* * * * SUN,SAT',
  '* * * * TUE-THU',

  // --- months as names ---------------------------------------------------
  '0 0 1 JAN *', '0 0 1 jan-mar *', '0 0 1 JAN,JUL *', '0 0 1 DEC *',

  // --- special characters ------------------------------------------------
  '0 0 L * *', '0 0 * * 5L', '0 0 * * 0L', '0 0 * * MON#2', '0 0 * * 5#3',
  '0 0 * * 1#1', '0 0 * * 1#5',

  // --- question mark -----------------------------------------------------
  '0 0 ? * *', '0 0 * * ?', '0 0 ? * ?',

  // --- predefined aliases ------------------------------------------------
  '@yearly', '@annually', '@monthly', '@weekly', '@daily', '@hourly',
  '@minutely', '@secondly', '@weekdays', '@weekends',

  // --- six-field forms ---------------------------------------------------
  '0 0 0 1 1 *', '*/5 * * * * *', '30 30 * * * *', '0-30/10 * * * * *',

  // --- hashed (deterministic under HASH_SEED) ----------------------------
  'H * * * *', 'H H * * *', 'H/15 * * * *', 'H(0-30) * * * *',
  'H(0-30)/10 * * * *', '0 H * * *', 'H H H H H',

  // --- boundaries --------------------------------------------------------
  '0 0 29 2 *', '0 0 30 4 *', '0 0 31 1 *', '0 0 1,31 2 *',

  // --- errors: field counts ----------------------------------------------
  '', '5', '5 *', '* * * * * * *',

  // --- errors: out of range ----------------------------------------------
  '60 * * * *', '* 24 * * *', '* * 32 * *', '* * 0 * *', '* * * 13 *',
  '* * * 0 *', '* * * * 8', '-1 * * * *',

  // --- errors: malformed -------------------------------------------------
  'x', '* * * xyz *', '* * * * xyz', '0,1,z * * * *', '0-z * * * *',
  '*/A * * * *', '! * * * *', ') * * * *', '5/5/5 * * * *',
  '*/0 * * * *', '30-20 * * * *', '10-5 * * * *', '0 0 * * 1.2#2',
  '0 0 * * 1,2#2', '0 0 * * 1-2#2', '0 0 * * 1#0', '0 0 * * 1#6',
  '0 0 * * 1#a', '0 0 15W * *', '0 0 LW * *', '0 0 * * L',
  '0 0 30,31 2 *', ', * * * *', '1,,2 * * * *',
];

function capture(expr, opts) {
  try {
    const e = CronExpressionParser.parse(expr, opts);
    const f = e.fields;
    return {
      expression: expr,
      ok: true,
      second: f.second.values,
      minute: f.minute.values,
      hour: f.hour.values,
      dayOfMonth: f.dayOfMonth.values,
      month: f.month.values,
      dayOfWeek: f.dayOfWeek.values,
      wildcards: {
        second: f.second.isWildcard,
        minute: f.minute.isWildcard,
        hour: f.hour.isWildcard,
        dayOfMonth: f.dayOfMonth.isWildcard,
        month: f.month.isWildcard,
        dayOfWeek: f.dayOfWeek.isWildcard,
      },
      nthDayOfWeek: f.dayOfWeek.nthDay,
      hasLast: { dayOfMonth: f.dayOfMonth.hasLastChar, dayOfWeek: f.dayOfWeek.hasLastChar },
      hasQuestion: { dayOfMonth: f.dayOfMonth.hasQuestionMarkChar, dayOfWeek: f.dayOfWeek.hasQuestionMarkChar },
    };
  } catch (err) {
    return { expression: expr, ok: false, error: err.message };
  }
}

const fixtures = {
  note: 'Generated from harrisiirak/cron-parser v5.6.2 by scripts/probe/gen-parse-fixtures.js. Do not edit by hand.',
  hashSeed: HASH_SEED,
  cases: expressions.map((e) => capture(e, { hashSeed: HASH_SEED })),
  strictCases: ['* * * * *', '* * * * * *', '0 0 1 * 1', '0 0 * * 1', '0 0 1 * *', '']
    .map((e) => ({ ...capture(e, { hashSeed: HASH_SEED, strict: true }), strict: true })),
};

const out = path.join(__dirname, '..', '..', 'cron', 'testdata', 'parse-fixtures.json');
fs.mkdirSync(path.dirname(out), { recursive: true });
fs.writeFileSync(out, JSON.stringify(fixtures, null, 2) + '\n');

const okCount = fixtures.cases.filter((c) => c.ok).length;
console.log(`wrote ${fixtures.cases.length} cases (${okCount} parse, ${fixtures.cases.length - okCount} error)`);
console.log(`   + ${fixtures.strictCases.length} strict-mode cases`);
console.log(`-> ${out}`);
