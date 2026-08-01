/*
 * A command-line front end for the original library, matching the port's CLI
 * output byte for byte.
 *
 * The port ships a real CLI; the original ships only a library. To diff command
 * line output on a shared input set, the original needs an equivalent entry
 * point, and this is the thinnest one that produces the same shape. It
 * deliberately contains no logic beyond formatting: every answer comes from the
 * original library, so a difference in the diff is a difference in behaviour
 * rather than in presentation.
 *
 *   node compare/original-cli.js next  "* * * * *" -n 5 -tz UTC -from <rfc3339>
 *   node compare/original-cli.js parse "0 0 L * *"
 *   node compare/original-cli.js check "0 0 * * *" -at <rfc3339>
 */
'use strict';

const path = require('path');
const { DateTime } = require(path.join(__dirname, '..', '..', 'cron-parser', 'node_modules', 'luxon'));

const REFERENCE = path.join(__dirname, '..', '..', 'cron-parser', 'dist', 'index.js');
let CronExpressionParser;
try {
  ({ CronExpressionParser } = require(REFERENCE));
} catch {
  console.error(`Could not load the reference implementation from ${REFERENCE}`);
  process.exit(2);
}

const [command, expression, ...rest] = process.argv.slice(2);

function flag(name, fallback) {
  const i = rest.indexOf(`-${name}`);
  return i >= 0 && rest[i + 1] !== undefined ? rest[i + 1] : fallback;
}
const has = (name) => rest.includes(`-${name}`);

const count = Number(flag('n', '1'));
const tz = flag('tz', 'UTC');
const from = flag('from', '');
const at = flag('at', '');
const seed = flag('seed', '');
const asJSON = has('json');

/**
 * Renders an instant the way Go's time.Format(time.RFC3339) does.
 *
 * The two runtimes format differently out of the box — Go writes a trailing Z
 * for UTC and omits milliseconds, luxon includes them — so the formatting is
 * pinned here. Otherwise every line would differ for reasons that have nothing
 * to do with the schedule.
 */
function rfc3339(millis) {
  const iso = DateTime.fromMillis(millis, { zone: tz }).toISO({
    suppressMilliseconds: true,
    suppressSeconds: false,
  });
  // Go's time.RFC3339 writes a zero offset as "Z"; luxon writes "+00:00"
  // whenever the zone is named rather than literally UTC. The instants are the
  // same, so the convention is normalised here rather than being reported as a
  // difference.
  return iso.replace(/\+00:00$/, 'Z');
}

const options = { tz };
if (seed) options.hashSeed = seed;
if (from) options.currentDate = new Date(from);

let expr;
try {
  expr = CronExpressionParser.parse(expression, options);
} catch (err) {
  console.error(`cron-parser: ${err.message}`);
  process.exit(1);
}

function occurrences(direction) {
  const out = [];
  for (let i = 0; i < count; i++) {
    try {
      out.push(rfc3339((direction === 'prev' ? expr.prev() : expr.next()).getTime()));
    } catch {
      break;
    }
  }
  return out;
}

switch (command) {
  case 'next':
  case 'prev': {
    const out = occurrences(command);
    if (asJSON) console.log(JSON.stringify({ occurrences: out }, null, 2));
    else for (const line of out) console.log(line);
    break;
  }

  case 'parse': {
    const f = expr.fields;
    const named = [
      ['second', f.second],
      ['minute', f.minute],
      ['hour', f.hour],
      ['dayOfMonth', f.dayOfMonth],
      ['month', f.month],
      ['dayOfWeek', f.dayOfWeek],
    ];

    let canonical;
    try {
      canonical = expr.stringify(true);
    } catch (err) {
      console.error(`cron-parser: ${err.message}`);
      process.exit(1);
    }

    if (asJSON) {
      const fields = {};
      for (const [name, field] of named) {
        fields[name] = { values: field.values.map(String), wildcard: field.isWildcard };
      }
      console.log(JSON.stringify({ expression, canonical, fields }, null, 2));
    } else {
      console.log(`expression  ${expression}`);
      console.log(`canonical   ${canonical}`);
      for (const [name, field] of named) {
        const mark = field.isWildcard ? '  (wildcard)' : '';
        console.log(`${name.padEnd(11)} ${field.values.map(String).join(',')}${mark}`);
      }
    }
    break;
  }

  case 'check': {
    let matched;
    try {
      matched = expr.includesDate(new Date(at));
    } catch (err) {
      console.error(`cron-parser: ${err.message}`);
      process.exit(1);
    }
    console.log(matched);
    process.exit(matched ? 0 : 1);
  }

  default:
    console.error('unknown command');
    process.exit(2);
}
