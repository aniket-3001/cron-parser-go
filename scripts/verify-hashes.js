/*
 * Proves tests/original/ is byte-identical to the upstream suite pinned at
 * kickoff.
 *
 * A Node implementation alongside the shell one, so the check runs on any
 * machine that can run the tests at all.
 *
 * Usage:  npm run verify-hashes
 */
'use strict';

const crypto = require('crypto');
const fs = require('fs');
const path = require('path');

const dir = path.join(__dirname, '..', 'tests', 'original');
const manifest = path.join(dir, 'HASHES.txt');

const expected = fs
  .readFileSync(manifest, 'utf8')
  .split('\n')
  .filter((line) => line.trim() && !line.startsWith('#'))
  .map((line) => {
    const [hash, name] = line.trim().split(/\s+/);
    return { hash, name: name.replace(/^\*/, '') };
  });

let failed = 0;
for (const { hash, name } of expected) {
  const file = path.join(dir, name);
  if (!fs.existsSync(file)) {
    console.error(`MISSING  ${name}`);
    failed++;
    continue;
  }
  const actual = crypto.createHash('sha256').update(fs.readFileSync(file)).digest('hex');
  if (actual !== hash) {
    console.error(`CHANGED  ${name}\n  expected ${hash}\n  actual   ${actual}`);
    failed++;
  }
}

if (failed > 0) {
  console.error(
    `\nFAIL: ${failed} of ${expected.length} original test files no longer match their kickoff hash.\n` +
      'These files must never be edited. Restore them from upstream:\n' +
      '  harrisiirak/cron-parser @ aeb2a1513fd33365a6414f4137516c9482f831ed',
  );
  process.exit(1);
}

console.log(`OK: all ${expected.length} original test files match their kickoff SHA-256.`);
