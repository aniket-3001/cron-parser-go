/*
 * Measures the original TypeScript library on the same corpus and the same unit
 * of work as bench/main.go, emitting the same JSON shape so the two can be put
 * side by side.
 *
 * Usage:
 *   node bench/original.js [--iterations=2000] [--warmup=200]
 *   node bench/original.js --mode=coldstart
 */
'use strict';

const path = require('path');

const REFERENCE = path.join(__dirname, '..', '..', 'cron-parser', 'dist', 'index.js');
let CronExpressionParser;
try {
  ({ CronExpressionParser } = require(REFERENCE));
} catch {
  console.error(`Could not load the reference implementation from ${REFERENCE}`);
  console.error('The upstream clone must be present at ../cron-parser, built with npm run build.');
  process.exit(2);
}

const args = process.argv.slice(2);
const optionOf = (name, fallback) => {
  const found = args.find((a) => a.startsWith(`--${name}=`));
  return found ? found.slice(name.length + 3) : fallback;
};

// Copied from the original's own benchmarks/benchmark-inputs.ts.
const PATTERNS = [
  '* * * * * *',
  '0 15 */5 5 *',
  '10-30/2 2 12 8 0',
  '10 2 12 8 7',
  '0 12 */5 6 *',
  '0 * * 1,4-10,L * *',
  '0 0 0 * * 4,6L',
  '0 0 0 * * 1L,5L',
  '0 0 6-20/2,L 2 *',
  '0 H * * *',
  '0 H/3 * * *',
  'H H H(9-20)/3 1-11 *',
  '0 0 0 * * 5#3',
  '0 0 0 8 * 5#3',
  '0 0 0 15 * 5#3',
];

const START = new Date('2026-01-01T00:00:00Z');

/** One unit of work: parse an expression and take ten occurrences from it. */
function parseAndIterate(pattern) {
  const e = CronExpressionParser.parse(pattern, {
    tz: 'UTC',
    hashSeed: 'bench',
    currentDate: START,
  });
  e.take(10);
}

// One batch should take this long. Both harnesses batch identically: the Go
// side has to, because its clock reports in steps far larger than a single
// operation, and measuring the two differently would make the comparison
// meaningless even though this side could time individual calls.
const TARGET_BATCH_NS = 10_000_000n;

/**
 * Finds a batch size large enough to time reliably.
 *
 * Escalates well past the target before deciding, then derives the batch size
 * from the measured cost of one operation. Stopping at the first batch to
 * exceed the target is not reliable: one scheduling stall during calibration
 * makes a far too small batch look large enough.
 */
function calibrate(pattern) {
  const calibrationTarget = TARGET_BATCH_NS * 5n;
  let n = 1;
  let elapsed = 0n;

  while (n < 1 << 22) {
    const t0 = process.hrtime.bigint();
    for (let i = 0; i < n; i++) parseAndIterate(pattern);
    elapsed = process.hrtime.bigint() - t0;
    if (elapsed >= calibrationTarget) break;
    n *= 2;
  }

  const perOp = Number(elapsed) / n;
  return Math.max(1, Math.round(Number(TARGET_BATCH_NS) / perOp));
}

function percentile(sorted, p) {
  if (sorted.length === 0) return 0;
  return sorted[Math.floor(p * (sorted.length - 1))];
}

function summarise(pattern, samples, batchSize) {
  samples.sort((a, b) => a - b);
  const total = samples.reduce((acc, v) => acc + v, 0);
  return {
    pattern,
    samples: samples.length,
    batchSize,
    meanNs: total / samples.length,
    p50Ns: percentile(samples, 0.5),
    p90Ns: percentile(samples, 0.9),
    p99Ns: percentile(samples, 0.99),
    maxNs: samples[samples.length - 1],
  };
}

if (optionOf('mode', 'throughput') === 'coldstart') {
  // No warmup and no repetition: the point is the work a process does before it
  // can answer once. The caller times the process, not this block.
  const e = CronExpressionParser.parse('*/15 9-17 * * 1-5', { tz: 'UTC', currentDate: START });
  console.log(e.next().toISOString());
  process.exit(0);
}

const batches = Number(optionOf('batches', '200'));
const warmup = Number(optionOf('warmup', '200'));

const measurements = [];
for (const pattern of PATTERNS) {
  // Warm up first, so the samples exclude one-time costs and let the JIT settle.
  for (let i = 0; i < warmup; i++) parseAndIterate(pattern);

  const batchSize = calibrate(pattern);
  const samples = new Array(batches);
  for (let i = 0; i < batches; i++) {
    const t0 = process.hrtime.bigint();
    for (let j = 0; j < batchSize; j++) parseAndIterate(pattern);
    // One sample is the mean cost of an operation across the batch.
    samples[i] = Number(process.hrtime.bigint() - t0) / batchSize;
  }
  measurements.push(summarise(pattern, samples, batchSize));
}

console.log(
  JSON.stringify(
    {
      implementation: 'typescript-original',
      runtime: `node ${process.version} ${process.platform}/${process.arch}`,
      workload: 'parse an expression, then take 10 occurrences',
      sampleUnit: 'mean nanoseconds per operation across one batch',
      batches,
      warmup,
      measurements,
      memoryBytes: process.memoryUsage().rss,
      memoryMetric: 'node resident set size',
    },
    null,
    2,
  ),
);
