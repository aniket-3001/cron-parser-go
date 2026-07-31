/*
 * Differential check of the Go engine against the reference implementation.
 *
 * Consumes the randomly generated corpus written by TestGenerateScheduleCorpus
 * and replays every probe through the original, comparing the schedules produced
 * in both directions as well as the exact text of any error.
 *
 * Usage:
 *     CRON_GEN_CORPUS=1 go test -run TestGenerateScheduleCorpus ./cron/
 *     node scripts/probe/verify-schedule.js
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
  process.exit(2);
}

const corpusPath = path.join(__dirname, 'schedule-corpus.json');
if (!fs.existsSync(corpusPath)) {
  console.error(`Corpus not found at ${corpusPath}`);
  console.error('Generate it:  CRON_GEN_CORPUS=1 go test -run TestGenerateScheduleCorpus ./cron/');
  process.exit(2);
}

const probes = JSON.parse(fs.readFileSync(corpusPath, 'utf8'));

function walk(expr, tz, startMs, n, reverse) {
  const out = [];
  try {
    const it = CronExpressionParser.parse(expr, { currentDate: new Date(startMs), tz });
    for (let i = 0; i < n; i++) out.push((reverse ? it.prev() : it.next()).toISOString());
  } catch (err) {
    return { values: out, error: err.message };
  }
  return { values: out, error: '' };
}

const failures = [];
let compared = 0;
let parseErrorsAgreed = 0;

for (const p of probes) {
  // Parse errors must agree in text as well as in occurrence.
  let refParseError = '';
  try {
    CronExpressionParser.parse(p.expression, { currentDate: new Date(p.startMs), tz: p.tz });
  } catch (err) {
    refParseError = err.message;
  }

  if (p.parseError || refParseError) {
    if (p.parseError !== refParseError) {
      failures.push({
        kind: 'parse', expression: p.expression, tz: p.tz, startMs: p.startMs,
        go: p.parseError || '(accepted)', ref: refParseError || '(accepted)',
      });
    } else {
      parseErrorsAgreed++;
    }
    continue;
  }

  for (const [label, goValues, goError, reverse] of [
    ['next', p.next || [], p.nextError || '', false],
    ['prev', p.prev || [], p.prevError || '', true],
  ]) {
    const ref = walk(p.expression, p.tz, p.startMs, 6, reverse);
    compared++;

    if (goError !== ref.error) {
      failures.push({
        kind: label + '-error', expression: p.expression, tz: p.tz, startMs: p.startMs,
        go: goError || '(none)', ref: ref.error || '(none)',
      });
      continue;
    }

    const n = Math.min(goValues.length, ref.values.length);
    if (goValues.length !== ref.values.length) {
      failures.push({
        kind: label + '-length', expression: p.expression, tz: p.tz, startMs: p.startMs,
        go: `${goValues.length} values`, ref: `${ref.values.length} values`,
      });
    }
    for (let i = 0; i < n; i++) {
      if (goValues[i] !== ref.values[i]) {
        failures.push({
          kind: label + `[${i}]`, expression: p.expression, tz: p.tz, startMs: p.startMs,
          go: goValues[i], ref: ref.values[i],
        });
        break;
      }
    }
  }
}

console.log(`probes:            ${probes.length.toLocaleString()}`);
console.log(`sequences compared: ${compared.toLocaleString()}`);
console.log(`parse errors agreed: ${parseErrorsAgreed.toLocaleString()}`);

if (failures.length === 0) {
  console.log('\nPASS - Go and the reference agree on every schedule.');
  process.exit(0);
}

const byKind = new Map();
for (const f of failures) byKind.set(f.kind, (byKind.get(f.kind) || 0) + 1);

console.log(`\nFAIL - ${failures.length} divergence(s)\n`);
console.log('by kind:');
for (const [k, n] of [...byKind].sort((a, b) => b[1] - a[1])) console.log(`  ${k.padEnd(16)} ${n}`);

console.log('\nsamples:');
for (const f of failures.slice(0, 20)) {
  console.log(`  [${f.kind}] ${JSON.stringify(f.expression)}  tz=${f.tz}  start=${new Date(f.startMs).toISOString()}`);
  console.log(`      go : ${f.go}`);
  console.log(`      ref: ${f.ref}`);
}
if (failures.length > 20) console.log(`  ... and ${failures.length - 20} more`);
process.exit(1);
