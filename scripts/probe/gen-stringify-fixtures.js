/*
 * Generates cron/testdata/stringify-fixtures.json: for each expression, the text
 * the reference renders it back to, plus the round-trip result and the
 * serialized field form.
 *
 * Rendering is the inverse of parsing, so the round trip is a property worth
 * checking directly: parsing the rendered text must yield the same fields again.
 *
 * Usage:  node scripts/probe/gen-stringify-fixtures.js
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

const HASH_SEED = 'port-mortem';

const expressions = [
  // --- forms that should collapse back to a wildcard ---------------------
  '* * * * *', '* * * * * *', '0 * * * *', '0 0 * * *',
  '0-59 * * * *', '0-23 * * *'.length ? '* 0-23 * * *' : '',

  // --- steps -------------------------------------------------------------
  '*/2 * * * *', '*/3 * * * *', '*/5 * * * *', '*/10 * * * *', '*/15 * * * *',
  '*/20 * * * *', '*/30 * * * *', '0 */2 * * *', '0 */3 * * *', '0 */6 * * *',
  '0 */12 * * *', '*/7 * * * *', '*/11 * * * *', '*/13 * * * *', '*/17 * * * *',
  '*/59 * * * *', '* */23 * * *',

  // --- ranges and stepped ranges ----------------------------------------
  '1-5 * * * *', '10-30 * * * *', '0-30/5 * * * *', '10-30/5 * * * *',
  '5-55/10 * * * *', '2-58/4 * * * *', '1-59/2 * * * *', '0 9-17 * * *',
  '0 9-17/2 * * *', '0 0 1-15 * *', '0 0 1-15/2 * *', '0 0 * 1-6 *',
  '0 0 * 1-6/2 *', '0 0 * * 1-5',

  // --- lists -------------------------------------------------------------
  '0,30 * * * *', '1,3,5 * * * *', '0,15,30,45 * * * *', '1,2,3 * * * *',
  '0 0,6,12,18 * * *', '0 0 1,15 * *', '0 0 * 1,6,12 *', '0 0 * * 0,6',
  '1,5,9,13 * * * *', '0,2,4,6,8 * * * *', '5,10,20,40 * * * *',

  // --- runs of exactly two, which render as two singletons ---------------
  '1,2 * * * *', '0,1 * * * *', '58,59 * * * *', '0 0,1 * * *',

  // --- bare start with a step -------------------------------------------
  '5/10 * * * *', '5/15 * * * *', '0/5 * * * *', '1/2 * * * *',

  // --- special characters ------------------------------------------------
  '0 0 L * *', '0 0 * * 5L', '0 0 * * 0L', '0 0 * * MON#2', '0 0 * * 5#3',
  '0 0 * * 1#1', '0 0 1,L * *', '0 0 15,L * *',

  // --- question mark round-trip -----------------------------------------
  '0 0 ? * *', '0 0 * * ?', '0 0 ? * ?',

  // --- day-of-week seven asymmetry ---------------------------------------
  '* * * * 0', '* * * * 7', '* * * * 0-7', '* * * * 1-7', '* * * * 5-7',
  '* * * * 6,7', '* * * * 0,6', '* * * * 2-6',

  // --- day-of-month rendered against a single month's length -------------
  '0 0 1-28 2 *', '0 0 1-29 2 *', '0 0 1-30 4 *', '0 0 1-31 1 *',
  '0 0 */2 2 *', '0 0 1-28/7 2 *',

  // --- named forms, which normalise to numbers --------------------------
  '0 0 1 JAN *', '0 0 1 jan-mar *', '0 0 * * MON', '0 0 * * MON-FRI',
  '0 0 * * SUN,SAT',

  // --- predefined aliases ------------------------------------------------
  '@yearly', '@annually', '@monthly', '@weekly', '@daily', '@hourly',
  '@minutely', '@secondly', '@weekdays', '@weekends',

  // --- six-field forms ---------------------------------------------------
  '0 0 0 1 1 *', '*/5 * * * * *', '30 30 * * * *', '0-30/10 * * * * *',
  '1,2,3 4,5,6 7,8,9 10,11 12 3',

  // --- hashed, deterministic under HASH_SEED -----------------------------
  'H * * * *', 'H/15 * * * *', 'H(0-30) * * * *', 'H(0-30)/10 * * * *',

  // --- single values -----------------------------------------------------
  '0 0 1 1 0', '30 12 25 12 *', '59 23 31 12 6',
].filter(Boolean);

function capture(expr) {
  try {
    const e = CronExpressionParser.parse(expr, { hashSeed: HASH_SEED });
    const out = {
      expression: expr,
      ok: true,
      stringify: e.stringify(),
      stringifyWithSeconds: e.stringify(true),
      toString: e.toString(),
      serialize: e.fields.serialize(),
    };

    // Round trip: parsing the rendered text must reproduce the same fields.
    try {
      const again = CronExpressionParser.parse(out.stringifyWithSeconds, { hashSeed: HASH_SEED });
      out.roundTrip = again.stringify(true);
      out.roundTripStable = again.stringify(true) === out.stringifyWithSeconds;
    } catch (err) {
      out.roundTripError = err.message;
    }
    return out;
  } catch (err) {
    return { expression: expr, ok: false, error: err.message };
  }
}

const cases = expressions.map(capture);

// Crontab file content, parsed as a whole.
const crontabs = [
  {
    name: 'variables, comments and schedules',
    content: [
      '# a comment line',
      'ENV1="test1"',
      "ENV2='test2'",
      '',
      '*/10 * * * * /path/to/exe',
      '*/10 * * * * /path/to/exe',
      '0 09-18 * * 1-5 /path/to/exe',
    ].join('\n'),
  },
  {
    name: 'an invalid line is collected rather than fatal',
    content: [
      'FOO=bar',
      '*/5 * * * * valid-command',
      'invalid expression here',
      '* * * * * another-valid',
    ].join('\n'),
  },
  {
    name: 'indented content',
    content: '\n  FOO=bar\n  */5 * * * * cmd\n',
  },
  {
    name: 'no command after the schedule',
    content: '0 0 * * *\n',
  },
  {
    name: 'an equals sign in the value',
    content: 'PATH=/usr/bin:/bin\nKEY=a=b\n',
  },
];

function captureCrontab(c) {
  // The reference reads through the filesystem module; the content is written
  // to a temporary file so the same code path is exercised.
  const tmp = path.join(require('os').tmpdir(), `crontab-fixture-${Math.random().toString(36).slice(2)}`);
  fs.writeFileSync(tmp, c.content);
  try {
    const { CronFileParser } = require(REF);
    const r = CronFileParser.parseFileSync(tmp);
    return {
      name: c.name,
      content: c.content,
      variables: r.variables,
      expressions: r.expressions.map((e) => e.stringify(true)),
      errors: Object.keys(r.errors),
    };
  } finally {
    fs.unlinkSync(tmp);
  }
}

const fixtures = {
  note: 'Generated from harrisiirak/cron-parser v5.6.2 by scripts/probe/gen-stringify-fixtures.js. Do not edit by hand.',
  hashSeed: HASH_SEED,
  cases,
  crontabs: crontabs.map(captureCrontab),
};

const out = path.join(__dirname, '..', '..', 'cron', 'testdata', 'stringify-fixtures.json');
fs.mkdirSync(path.dirname(out), { recursive: true });
fs.writeFileSync(out, JSON.stringify(fixtures, null, 2) + '\n');

const okCount = cases.filter((c) => c.ok).length;
const unstable = cases.filter((c) => c.ok && c.roundTripStable === false);
console.log(`wrote ${cases.length} cases (${okCount} render, ${cases.length - okCount} error)`);
console.log(`   + ${fixtures.crontabs.length} crontab cases`);
if (unstable.length) {
  console.log(`   note: ${unstable.length} expression(s) do not round-trip stably in the reference:`);
  for (const u of unstable) console.log(`     ${JSON.stringify(u.expression)} -> ${u.stringifyWithSeconds} -> ${u.roundTrip}`);
}
console.log(`-> ${out}`);
