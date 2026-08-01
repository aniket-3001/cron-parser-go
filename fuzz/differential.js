/*
 * Differential fuzzing: the original TypeScript library and the Go port, given
 * identical inputs, must produce identical output.
 *
 * There is no template for this; the design is described in fuzz/README.md.
 * In short: both implementations are loaded into one process, a seeded
 * generator produces cron expressions, timezones and starting instants, and
 * every observable answer is compared — the schedules in both directions, the
 * rendered text, the parsed field values, membership answers, and the exact
 * text of every error. A disagreement is minimised to the smallest input that
 * still shows it and written out as a runnable reproduction.
 *
 * Usage:
 *   node fuzz/differential.js [--seconds=60] [--seed=1] [--quiet]
 */
'use strict';

const fs = require('fs');
const path = require('path');

const { PortExpression, PortDate } = require('./port');

const REFERENCE = path.join(__dirname, '..', '..', 'cron-parser', 'dist', 'index.js');
let CronExpressionParser;
let CronDate;
try {
  ({ CronExpressionParser, CronDate } = require(REFERENCE));
} catch {
  console.error(`Could not load the reference implementation from ${REFERENCE}`);
  console.error('The upstream clone must be present at ../cron-parser, built with npm run build.');
  process.exit(2);
}

// --- options ---------------------------------------------------------------

const args = process.argv.slice(2);
const optionOf = (name, fallback) => {
  const found = args.find((a) => a.startsWith(`--${name}=`));
  return found ? found.slice(name.length + 3) : fallback;
};
const DURATION_MS = Number(optionOf('seconds', '60')) * 1000;
const SEED = Number(optionOf('seed', '1'));
const QUIET = args.includes('--quiet');

// --- generator -------------------------------------------------------------

/**
 * mulberry32, so a run is reproducible from its seed alone. Any divergence can
 * be replayed exactly by rerunning with the same --seed.
 */
function rng(seed) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const rand = rng(SEED);
const pick = (xs) => xs[Math.floor(rand() * xs.length)];
const between = (lo, hi) => lo + Math.floor(rand() * (hi - lo + 1));

/**
 * Timezones chosen for the shapes of their transitions rather than for
 * coverage: a two-hour gap, half- and quarter-hour offsets, transitions that
 * happen at midnight, southern-hemisphere ordering, and a zone that abolished
 * daylight saving partway through the range.
 */
const ZONES = [
  'UTC',
  'America/New_York',
  'Europe/London',
  'Europe/Sofia',
  'Asia/Kolkata',
  'Australia/Lord_Howe',
  'Antarctica/Troll',
  'Pacific/Chatham',
  'America/Santiago',
  'Africa/Cairo',
  'Australia/Sydney',
  'America/Sao_Paulo',
  'Asia/Beirut',
  'Pacific/Auckland',
];

/** Generates one field, covering every form the grammar allows. */
function field(lo, hi, extras) {
  switch (between(0, 11)) {
    case 0:
    case 1:
      return '*';
    case 2:
      return String(between(lo, hi));
    case 3: {
      const n = between(1, 4);
      return Array.from({ length: n }, () => between(lo, hi)).join(',');
    }
    case 4:
      return `${between(lo, hi)}-${between(lo, hi)}`;
    case 5:
      return `*/${between(1, hi - lo + 1)}`;
    case 6:
      return `${between(lo, hi)}-${between(lo, hi)}/${between(1, 6)}`;
    case 7:
      return `${between(lo, hi)}/${between(1, 6)}`;
    case 8:
      return '?';
    case 9:
      return extras.length ? pick(extras) : '*';
    case 10:
      // Deliberately out of range, to compare how the two reject it.
      return String(between(hi + 1, hi + 20));
    default:
      return '*';
  }
}

function expression() {
  const fields = [
    field(0, 59, []),
    field(0, 59, []),
    field(0, 23, []),
    field(1, 31, ['L', 'LW', '15W']),
    field(1, 12, ['JAN', 'jun', 'DEC', 'JAN-MAR']),
    field(0, 7, ['L', '5L', '1L', 'MON#2', '5#3', 'MON-FRI', 'SUN', 'sun,SAT']),
  ];

  switch (between(0, 9)) {
    case 0:
      // Predefined aliases.
      return pick([
        '@yearly', '@annually', '@monthly', '@weekly', '@daily',
        '@hourly', '@minutely', '@secondly', '@weekdays', '@weekends',
      ]);
    case 1:
      // Hashed forms, made deterministic by the shared seed.
      return [
        pick(['H', 'H/15', 'H(0-30)', 'H(0-30)/10', '0']),
        ...fields.slice(1, 5),
        fields[5],
      ].join(' ');
    case 2:
      // Malformed input, to compare rejection.
      return pick([
        '', '   ', '5', '5 *', '* * * * * * *', 'x', '*/0 * * * *',
        '5/5/5 * * * *', ', * * * *', '1,,2 * * * *', '30-20 * * * *',
        '* * * xyz *', '0 0 * * 1.2#2', '0 0 * * L', '0 0 30 2 *',
      ]);
    case 3:
      // Five-field form.
      return fields.slice(1).join(' ');
    default:
      return fields.join(' ');
  }
}

/** Transition instants per zone, so starts can cluster where the two might disagree. */
const transitionCache = new Map();
function transitions(zone) {
  if (transitionCache.has(zone)) {
    return transitionCache.get(zone);
  }
  const found = [];
  let previous = null;
  // Daily scan across the range, then bisect to the minute.
  for (let t = Date.UTC(2023, 0, 1); t < Date.UTC(2028, 0, 1); t += 86400000) {
    const offset = zoneOffset(t, zone);
    if (previous !== null && offset !== previous) {
      let lo = t - 86400000;
      let hi = t;
      while (hi - lo > 60000) {
        const mid = lo + Math.floor((hi - lo) / 2);
        if (zoneOffset(mid, zone) === previous) lo = mid;
        else hi = mid;
      }
      found.push(hi);
    }
    previous = offset;
  }
  transitionCache.set(zone, found);
  return found;
}

// Formatters are cached: constructing one is expensive enough that building a
// fresh one per lookup dominated the whole run, cutting throughput by two
// orders of magnitude.
const formatters = new Map();
function formatterFor(zone) {
  let dtf = formatters.get(zone);
  if (!dtf) {
    dtf = new Intl.DateTimeFormat('en-US', {
      timeZone: zone,
      hour12: false,
      year: 'numeric', month: '2-digit', day: '2-digit',
      hour: '2-digit', minute: '2-digit', second: '2-digit',
    });
    formatters.set(zone, dtf);
  }
  return dtf;
}

function zoneOffset(millis, zone) {
  // Intl reports the wall-clock reading in the zone; the difference from the
  // same reading in UTC is the offset.
  const dtf = formatterFor(zone);
  const parts = Object.fromEntries(dtf.formatToParts(new Date(millis)).map((p) => [p.type, p.value]));
  const asUTC = Date.UTC(
    Number(parts.year), Number(parts.month) - 1, Number(parts.day),
    Number(parts.hour) % 24, Number(parts.minute), Number(parts.second),
  );
  return Math.round((asUTC - millis) / 60000);
}

function startInstant(zone) {
  const trs = transitions(zone);
  if (trs.length > 0 && rand() < 0.5) {
    // Within a few hours of a real transition.
    return trs[Math.floor(rand() * trs.length)] + between(-240, 240) * 60000;
  }
  if (rand() < 0.25) {
    // Month and year boundaries, where clamping bites.
    return Date.UTC(between(2023, 2028), pick([0, 1, 2, 11]), pick([1, 28, 29, 30, 31]), between(0, 23), between(0, 59));
  }
  return Date.UTC(between(2020, 2030), between(0, 11), between(1, 28), between(0, 23), between(0, 59), between(0, 59));
}

// --- comparison ------------------------------------------------------------

const ITERATIONS = 8;

/** Runs one implementation and captures everything observable. */
function observeReference(expr, options) {
  try {
    const parsed = CronExpressionParser.parse(expr, options);
    const out = { parsed: true, next: [], prev: [], nextError: null, prevError: null };

    try {
      for (let i = 0; i < ITERATIONS; i++) out.next.push(parsed.next().getTime());
    } catch (e) {
      out.nextError = e.message;
    }

    const backwards = CronExpressionParser.parse(expr, options);
    try {
      for (let i = 0; i < ITERATIONS; i++) out.prev.push(backwards.prev().getTime());
    } catch (e) {
      out.prevError = e.message;
    }

    const fresh = CronExpressionParser.parse(expr, options);
    try {
      out.stringify = fresh.stringify(false);
      out.stringifySeconds = fresh.stringify(true);
    } catch (e) {
      out.stringifyError = e.message;
    }

    out.fields = {
      second: fresh.fields.second.values.slice(),
      minute: fresh.fields.minute.values.slice(),
      hour: fresh.fields.hour.values.slice(),
      dayOfMonth: fresh.fields.dayOfMonth.values.slice(),
      month: fresh.fields.month.values.slice(),
      dayOfWeek: fresh.fields.dayOfWeek.values.slice(),
    };

    out.includes = out.next.slice(0, 3).flatMap((ms) => {
      const probe = (t) => {
        try {
          return fresh.includesDate(new Date(t));
        } catch (e) {
          return `error:${e.message}`;
        }
      };
      return [probe(ms), probe(ms + 1000), probe(ms - 1000)];
    });

    return out;
  } catch (e) {
    return { parsed: false, error: e.message };
  }
}

/** The same observations, from the port. */
function observePort(expr, options) {
  let parsed;
  try {
    parsed = new PortExpression(expr, options);
  } catch (e) {
    return { parsed: false, error: e.message };
  }

  const out = { parsed: true, next: [], prev: [], nextError: null, prevError: null };
  const held = [parsed];

  try {
    try {
      for (let i = 0; i < ITERATIONS; i++) out.next.push(parsed.next());
    } catch (e) {
      out.nextError = e.message;
    }

    const backwards = new PortExpression(expr, options);
    held.push(backwards);
    try {
      for (let i = 0; i < ITERATIONS; i++) out.prev.push(backwards.prev());
    } catch (e) {
      out.prevError = e.message;
    }

    const fresh = new PortExpression(expr, options);
    held.push(fresh);
    try {
      out.stringify = fresh.format(false);
      out.stringifySeconds = fresh.format(true);
    } catch (e) {
      out.stringifyError = e.message;
    }

    out.fields = fresh.fieldValues();

    out.includes = out.next.slice(0, 3).flatMap((ms) => {
      const probe = (t) => {
        try {
          return fresh.includes(t);
        } catch (e) {
          return `error:${e.message}`;
        }
      };
      return [probe(ms), probe(ms + 1000), probe(ms - 1000)];
    });

    return out;
  } finally {
    for (const h of held) h.release();
  }
}

/** Reports the first way two observations differ, or null when they agree. */
function compare(a, b) {
  if (a.parsed !== b.parsed) {
    return {
      what: 'parse',
      reference: a.parsed ? 'accepted' : `rejected: ${a.error}`,
      port: b.parsed ? 'accepted' : `rejected: ${b.error}`,
    };
  }
  if (!a.parsed) {
    return a.error === b.error
      ? null
      : { what: 'parse error text', reference: a.error, port: b.error };
  }

  for (const key of ['nextError', 'prevError', 'stringifyError']) {
    if ((a[key] ?? null) !== (b[key] ?? null)) {
      return { what: key, reference: a[key] ?? '(none)', port: b[key] ?? '(none)' };
    }
  }

  for (const key of ['next', 'prev']) {
    if (a[key].length !== b[key].length) {
      return { what: `${key} length`, reference: a[key].length, port: b[key].length };
    }
    for (let i = 0; i < a[key].length; i++) {
      if (a[key][i] !== b[key][i]) {
        return {
          what: `${key}[${i}]`,
          reference: new Date(a[key][i]).toISOString(),
          port: new Date(b[key][i]).toISOString(),
        };
      }
    }
  }

  for (const key of ['stringify', 'stringifySeconds']) {
    if ((a[key] ?? null) !== (b[key] ?? null)) {
      return { what: key, reference: a[key], port: b[key] };
    }
  }

  for (const name of Object.keys(a.fields)) {
    const x = JSON.stringify(a.fields[name]);
    const y = JSON.stringify(b.fields[name]);
    if (x !== y) {
      return { what: `fields.${name}`, reference: x, port: y };
    }
  }

  for (let i = 0; i < a.includes.length; i++) {
    if (a.includes[i] !== b.includes[i]) {
      return { what: `includesDate[${i}]`, reference: a.includes[i], port: b.includes[i] };
    }
  }

  return null;
}

/**
 * The two implementations take the starting instant in different shapes: the
 * reference accepts a Date, while the bridge carries epoch milliseconds because
 * a Date cannot cross into WebAssembly. Both describe the same instant.
 */
function optionsFor(testCase) {
  const common = { tz: testCase.tz, hashSeed: testCase.hashSeed };
  return {
    reference: { ...common, currentDate: new Date(testCase.startMs) },
    port: { ...common, currentDate: testCase.startMs },
  };
}

/**
 * Runs one case, returning both the disagreement (or null) and the reference's
 * observation. The observation is returned rather than recomputed because
 * reference runs are the expensive half: an unsatisfiable expression costs it
 * ten thousand iterations of date arithmetic before it gives up.
 */
function runCase(testCase) {
  const options = optionsFor(testCase);
  const reference = observeReference(testCase.expr, options.reference);
  const port = observePort(testCase.expr, options.port);
  return { difference: compare(reference, port), reference };
}

// --- date operations -------------------------------------------------------

/**
 * The date operations both implementations expose.
 *
 * Kept separate from the expression cases because this surface is reachable
 * without any expression: the search loop never performs year arithmetic, so
 * comparing schedules alone would leave addYear and subtractYear unchecked.
 * That gap was found by deliberately breaking year arithmetic and watching an
 * expression-only run stay green.
 */
const DATE_OPERATIONS = [
  'addYear', 'addMonth', 'addDay', 'addHour', 'addMinute', 'addSecond',
  'subtractYear', 'subtractMonth', 'subtractDay', 'subtractHour',
  'subtractMinute', 'subtractSecond',
];

// Values chosen for the clamping they provoke: day 31 in a short month, month
// February from a 31-day month, a non-leap year from a leap day.
const DATE_SETTERS = [
  ['setDate', 'day', [1, 28, 29, 30, 31]],
  ['setMonth', 'month', [0, 1, 3, 11]],
  ['setFullYear', 'year', [2023, 2024, 2025]],
  ['setHours', 'hour', [0, 2, 3, 23]],
  ['setMinutes', 'minute', [0, 30, 59]],
  ['setSeconds', 'second', [0, 59]],
  ['setDay', 'weekday', [1, 4, 7]],
];

/**
 * Applies every date operation to one starting instant and compares each.
 *
 * Exercising the whole surface per instant rather than one operation per case
 * is what makes this useful. Picking a single random operation spread the
 * budget so thinly that breaking year arithmetic on purpose went unnoticed
 * through several runs: the bug only shows on a leap day, and the chance of
 * drawing both that instant and that operation was under one in four hundred.
 */
function runDateCase(testCase) {
  const { startMs, tz } = testCase;

  const checkPair = (label, mutate) => {
    const reference = new CronDate(new Date(startMs), tz);
    const port = new PortDate(startMs, tz);
    try {
      mutate.reference(reference);
      mutate.port(port);
      const checks = [
        ['time', reference.getTime(), port.get('time')],
        ['iso', reference.toISOString(), port.get('iso')],
        ['offset', reference.getUTCOffset(), port.get('offsetMinutes')],
        ['isLastDayOfMonth', reference.isLastDayOfMonth(), port.get('isLastDayOfMonth')],
        ['isLastWeekdayOfMonth', reference.isLastWeekdayOfMonth(), port.get('isLastWeekdayOfMonth')],
      ];
      for (const [what, a, b] of checks) {
        if (a !== b) {
          return { what: `${label}.${what}`, reference: String(a), port: String(b) };
        }
      }
      return null;
    } finally {
      port.release();
    }
  };

  for (const operation of DATE_OPERATIONS) {
    const d = checkPair(operation, {
      reference: (r) => r[operation](),
      port: (p) => p.apply(operation),
    });
    if (d) return d;
  }

  for (const [method, property, values] of DATE_SETTERS) {
    for (const value of values) {
      const d = checkPair(`${method}(${value})`, {
        reference: (r) => r[method](value),
        port: (p) => p.set(property, value),
      });
      if (d) return d;
    }
  }

  for (const boundary of ['startOfDay', 'endOfDay']) {
    const d = checkPair(boundary, {
      reference: (r) => r[boundary === 'startOfDay' ? 'setStartOfDay' : 'setEndOfDay'](),
      port: (p) => p.boundary(boundary),
    });
    if (d) return d;
  }

  return null;
}

/**
 * Instants where date arithmetic is most likely to differ.
 *
 * Uniform sampling is close to useless here: the clamping rules only show
 * themselves at month ends and on leap days, and a leap day is roughly one
 * instant in a thousand. Breaking year arithmetic on purpose and watching a
 * uniformly-sampled run stay green is what showed this up, so these are drawn
 * from deliberately.
 */
function boundaryInstant() {
  const leapYear = pick([2024, 2028]);
  const commonYear = pick([2023, 2025, 2026, 2027]);
  const hour = between(0, 23);
  const minute = between(0, 59);

  return pick([
    // Leap day, where adding or subtracting a year has to clamp.
    Date.UTC(leapYear, 1, 29, hour, minute),
    Date.UTC(leapYear, 1, 28, hour, minute),
    Date.UTC(commonYear, 1, 28, hour, minute),
    // The last day of a 31-day month, where adding a month has to clamp.
    Date.UTC(pick([leapYear, commonYear]), pick([0, 2, 4, 6, 7, 9, 11]), 31, hour, minute),
    // The last day of a 30-day month.
    Date.UTC(pick([leapYear, commonYear]), pick([3, 5, 8, 10]), 30, hour, minute),
    // Year boundaries.
    Date.UTC(commonYear, 11, 31, 23, 59),
    Date.UTC(commonYear, 0, 1, 0, 0),
  ]);
}

function dateCase() {
  const tz = pick(ZONES);
  // Half the cases start on a boundary, half wherever the expression generator
  // would have started.
  return { tz, startMs: rand() < 0.5 ? boundaryInstant() : startInstant(tz) };
}

// --- minimisation ----------------------------------------------------------

/**
 * Shrinks a failing case to the smallest input that still diverges, so a report
 * names the responsible field rather than a random six-field expression.
 */
function minimise(testCase) {
  let best = testCase;

  const stillFails = (candidate) => {
    try {
      return runCase(candidate).difference !== null;
    } catch {
      return false;
    }
  };

  // Replace each field with a wildcard where that keeps the divergence.
  const parts = best.expr.trim().split(/\s+/);
  for (let i = 0; i < parts.length; i++) {
    if (parts[i] === '*') continue;
    const candidate = { ...best, expr: parts.map((p, j) => (j === i ? '*' : p)).join(' ') };
    if (stillFails(candidate)) {
      best = candidate;
      parts[i] = '*';
    }
  }

  // Prefer UTC, then a round starting instant.
  if (best.tz !== 'UTC' && stillFails({ ...best, tz: 'UTC' })) {
    best = { ...best, tz: 'UTC' };
  }
  const round = Date.UTC(2026, 0, 1);
  if (stillFails({ ...best, startMs: round })) {
    best = { ...best, startMs: round };
  }

  return best;
}

// --- driver ----------------------------------------------------------------

function main() {
  for (const zone of ZONES) transitions(zone);
  const started = Date.now();
  const divergences = [];
  let cases = 0;
  let dateCases = 0;
  let bothRejected = 0;
  let bothAccepted = 0;

  if (!QUIET) {
    console.log('differential fuzzing: original vs port');
    console.log(`  duration : ${DURATION_MS / 1000}s`);
    console.log(`  seed     : ${SEED}`);
    console.log(`  zones    : ${ZONES.length}`);
    console.log('');
  }

  // Transition tables are built before the clock starts, so the reported rate
  // measures comparison rather than setup.
  for (const zone of ZONES) transitions(zone);

  let lastReport = started;
  while (Date.now() - started < DURATION_MS) {
    // A third of the budget goes to the date surface, which no expression
    // reaches in full.
    if (rand() < 0.33) {
      const dc = dateCase();
      dateCases++;
      let d = null;
      try {
        d = runDateCase(dc);
      } catch (e) {
        d = { what: 'harness', reference: '(n/a)', port: e.message };
      }
      if (d) {
        divergences.push({ case: dc, difference: d });
        if (!QUIET) {
          console.log(`DIVERGENCE  date tz=${dc.tz} from=${new Date(dc.startMs).toISOString()}`);
          console.log(`  ${d.what}: reference=${d.reference} port=${d.port}`);
        }
      }
      continue;
    }

    const tz = pick(ZONES);
    const testCase = {
      expr: expression(),
      tz,
      startMs: startInstant(tz),
      hashSeed: pick(['port-mortem', 'seed-a', 'seed-b']),
    };

    cases++;
    let difference = null;
    let reference = null;
    try {
      ({ difference, reference } = runCase(testCase));
    } catch (e) {
      difference = { what: 'harness', reference: '(n/a)', port: e.message };
    }

    if (difference) {
      const minimal = minimise(testCase);
      divergences.push({
        case: minimal,
        original: testCase,
        difference: runCase(minimal).difference ?? difference,
      });
      if (!QUIET) {
        console.log(`DIVERGENCE  ${JSON.stringify(minimal.expr)} tz=${minimal.tz}`);
        console.log(`  ${difference.what}: reference=${difference.reference} port=${difference.port}`);
      }
    } else if (reference) {
      // Track how the agreement splits, so "no divergences" cannot be read as
      // "everything was rejected identically".
      if (reference.parsed) bothAccepted++;
      else bothRejected++;
    }

    if (!QUIET && Date.now() - lastReport > 10000) {
      const elapsed = ((Date.now() - started) / 1000).toFixed(0);
      console.log(
        `  ${elapsed}s  ${cases.toLocaleString()} expression + ` +
          `${dateCases.toLocaleString()} date cases, ${divergences.length} divergences`,
      );
      lastReport = Date.now();
    }
  }

  const elapsed = (Date.now() - started) / 1000;
  const summary = {
    seed: SEED,
    seconds: Number(elapsed.toFixed(1)),
    expressionCases: cases,
    dateCases,
    casesPerSecond: Math.round((cases + dateCases) / elapsed),
    bothAccepted,
    bothRejected,
    divergences: divergences.length,
    zones: ZONES,
    generatedAt: new Date().toISOString(),
  };

  const outDir = path.join(__dirname, 'divergences');
  fs.mkdirSync(outDir, { recursive: true });
  fs.writeFileSync(path.join(__dirname, 'last-run.json'), JSON.stringify({ summary, divergences }, null, 2));
  for (const [i, d] of divergences.entries()) {
    fs.writeFileSync(path.join(outDir, `divergence-${SEED}-${i}.json`), JSON.stringify(d, null, 2));
  }

  console.log('');
  console.log(`ran ${elapsed.toFixed(1)}s`);
  console.log(`  expression cases ${cases.toLocaleString()}`);
  console.log(`  date cases       ${dateCases.toLocaleString()}`);
  console.log(`  combined rate    ${summary.casesPerSecond}/s`);
  console.log(`  both accepted    ${bothAccepted.toLocaleString()}`);
  console.log(`  both rejected    ${bothRejected.toLocaleString()} (identical error text)`);
  console.log(`  divergences      ${divergences.length}`);
  console.log('');

  if (divergences.length > 0) {
    console.log('FAIL - the port and the reference disagree.');
    process.exit(1);
  }
  console.log('PASS - no divergence on any compared behaviour.');
}

main();
