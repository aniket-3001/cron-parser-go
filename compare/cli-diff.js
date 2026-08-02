/*
 * Runs the same command line against the original and the port, and diffs the
 * output.
 *
 * The differential fuzzer compares the two through their APIs. This compares
 * them the way a user would meet them: as programs, on a shared input set,
 * comparing stdout, stderr and exit status. It is the coarser check of the two
 * and it is the one that catches a difference the API comparison cannot see:
 * a rejected expression that produces a different message, or a command that
 * exits non-zero on one side only.
 *
 * Writes compare/CLI-DIFF.md and compare/cli-diff.json.
 *
 * Usage:  npm run compare
 */
'use strict';

const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');
const binary = path.join(root, process.platform === 'win32' ? 'cron-parser.exe' : 'cron-parser');

if (!fs.existsSync(binary)) {
  console.log('building the port binary');
  execFileSync('go', ['build', '-o', path.basename(binary), './cmd/cron-parser'], {
    cwd: root,
    stdio: 'inherit',
    shell: process.platform === 'win32',
  });
}

/** Runs a command and captures everything a shell would observe. */
function capture(cmd, args, useShell) {
  try {
    const stdout = execFileSync(cmd, args, {
      cwd: root,
      encoding: 'utf8',
      stdio: ['ignore', 'pipe', 'pipe'],
      shell: useShell,
    });
    return { status: 0, stdout, stderr: '' };
  } catch (err) {
    return {
      status: err.status ?? -1,
      stdout: err.stdout ?? '',
      stderr: err.stderr ?? String(err.message),
    };
  }
}

const runPort = (args) => capture(binary, args, false);
// Neither side runs through a shell. A Windows shell would split an expression
// such as "*/15 9-17 * * 1-5" on its spaces before the program ever saw it,
// which would compare two different inputs.
const runOriginal = (args) => capture('node', ['compare/original-cli.js', ...args], false);

// A shared input set covering the grammar, the special characters, the
// timezone handling and the rejection paths. The starting instant is fixed so
// the comparison is deterministic.
const FROM = '2026-01-01T00:00:00Z';
const DST = '2026-03-06T12:00:00Z';

const cases = [];
const add = (name, args) => cases.push({ name, args });

for (const expr of [
  '* * * * *', '0 0 * * *', '*/15 9-17 * * 1-5', '0 12 * * MON-FRI',
  '30 2 * * *', '0 0 1 * *', '0 0 L * *', '0 0 * * 5L', '0 0 * * MON#2',
  '0 0 13 * 5', '0 0 29 2 *', '@daily', '@weekly', '@yearly', '@weekends',
  '0 0 ? * *', '*/5 * * * * *', '0 0 1,15 * *', '15,45 * * * *',
  '* * * * 5-7', '* * * * 7', '0 0 1-30 4 *',
]) {
  add(`next ${expr}`, ['next', expr, '-n', '6', '-from', FROM]);
  add(`prev ${expr}`, ['prev', expr, '-n', '4', '-from', FROM]);
  add(`parse ${expr}`, ['parse', expr]);
}

// Timezones, including the transitions that differ in width.
for (const tz of [
  'UTC', 'America/New_York', 'Europe/London', 'Europe/Sofia', 'Asia/Kolkata',
  'Australia/Lord_Howe', 'Antarctica/Troll', 'Pacific/Chatham',
  'America/Santiago', 'Africa/Cairo',
]) {
  add(`next 30 2 * * * in ${tz}`, ['next', '30 2 * * *', '-n', '6', '-tz', tz, '-from', DST]);
  add(`next 0 * * * * in ${tz}`, ['next', '0 * * * *', '-n', '6', '-tz', tz, '-from', DST]);
  add(`prev 30 2 * * * in ${tz}`, ['prev', '30 2 * * *', '-n', '4', '-tz', tz, '-from', DST]);
}

// Hashed fields, made deterministic by a shared seed.
for (const expr of ['H * * * *', 'H/15 * * * *', 'H(0-30) * * * *', 'H H * * *']) {
  add(`next ${expr} seeded`, ['next', expr, '-n', '4', '-from', FROM, '-seed', 'port-mortem']);
  add(`parse ${expr} seeded`, ['parse', expr, '-seed', 'port-mortem']);
}

// Membership.
for (const [expr, at] of [
  ['0 0 * * *', '2026-01-02T00:00:00Z'],
  ['0 0 * * *', '2026-01-02T00:00:01Z'],
  ['0 12 * * MON', '2026-01-05T12:00:00Z'],
  ['0 0 L * *', '2026-01-31T00:00:00Z'],
]) {
  add(`check ${expr} at ${at}`, ['check', expr, '-at', at]);
}

// Rejection paths: the message and the exit status both have to match.
for (const expr of [
  '61 * * * *', '* 24 * * *', '* * 32 * *', '* * * 13 *', '* * * * 8',
  'x', '* * * xyz *', '*/0 * * * *', '5/5/5 * * * *', '30-20 * * * *',
  '1,,2 * * * *', '0 0 * * 1.2#2', '0 0 15W * *', '* * * * * * *',
  '0 0 30 2 *', '0 0 * * L',
]) {
  add(`reject ${expr}`, ['next', expr, '-n', '1', '-from', FROM]);
}

console.log(`comparing ${cases.length} command lines`);

const results = [];
let matched = 0;
for (const c of cases) {
  const port = runPort(c.args);
  const original = runOriginal(c.args);

  const same =
    port.stdout === original.stdout &&
    port.status === original.status &&
    port.stderr.trim() === original.stderr.trim();

  if (same) matched++;
  results.push({ ...c, same, port, original });
}

const differing = results.filter((r) => !r.same);

// --- report ----------------------------------------------------------------

const block = (label, r) =>
  [
    `${label} (exit ${r.status})`,
    '```',
    (r.stdout + (r.stderr ? `[stderr] ${r.stderr}` : '')).trimEnd() || '(no output)',
    '```',
  ].join('\n');

const doc = `# CLI output diff

Generated by \`npm run compare\` on ${new Date().toISOString()}.

The differential fuzzer compares the two implementations through their APIs.
This compares them as **programs**: the same command line is run against each,
and stdout, stderr and exit status are compared.

The original ships only a library, so \`compare/original-cli.js\` gives it a
command-line front end with the same output shape. It contains no logic beyond
formatting (every answer comes from the original library) and it pins the
timestamp format, since Go and luxon render RFC 3339 differently by default and
every line would otherwise differ for reasons unrelated to the schedule.

## Result

| | |
|---|---:|
| Command lines compared | ${cases.length} |
| Identical output | **${matched}** |
| Differing | ${differing.length} |

Covering ${22 * 3} expression cases across the grammar, ${10 * 3} timezone cases
including two-hour, half-hour and quarter-hour transitions, 8 hashed cases under
a shared seed, 4 membership queries, and 16 rejection paths where the error text
and the exit status both have to match.

${
  differing.length === 0
    ? '**No differences.** Every command produced identical stdout, identical stderr and the same exit status.'
    : `## Differences\n\n${differing
        .map((r) => `### ${r.name}\n\n${block('original', r.original)}\n\n${block('port', r.port)}`)
        .join('\n\n')}`
}

## Reproducing

\`\`\`bash
npm run compare
\`\`\`

Requires the upstream clone at \`../cron-parser\`, built with \`npm run build\`.
Raw output for every case is in \`compare/cli-diff.json\`.
`;

fs.writeFileSync(path.join(__dirname, 'CLI-DIFF.md'), doc);
fs.writeFileSync(
  path.join(__dirname, 'cli-diff.json'),
  JSON.stringify({ generatedAt: new Date().toISOString(), total: cases.length, matched, results }, null, 2),
);

console.log(`  identical: ${matched} / ${cases.length}`);
console.log('  wrote compare/CLI-DIFF.md');

if (differing.length > 0) {
  for (const r of differing.slice(0, 10)) {
    console.log(`\nDIFFERS  ${r.name}`);
    console.log(`  original(${r.original.status}): ${JSON.stringify(r.original.stdout.slice(0, 90))}`);
    console.log(`  port    (${r.port.status}): ${JSON.stringify(r.port.stdout.slice(0, 90))}`);
    if (r.original.stderr || r.port.stderr) {
      console.log(`  stderr original: ${JSON.stringify(r.original.stderr.trim().slice(0, 90))}`);
      console.log(`  stderr port    : ${JSON.stringify(r.port.stderr.trim().slice(0, 90))}`);
    }
  }
  process.exit(1);
}
