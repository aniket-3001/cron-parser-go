/*
 * Drives the playground in a real browser and fails if it does not work.
 *
 * The page is the one artifact whose correctness cannot be checked by any of
 * the other suites: it loads two implementations into a browser and compares
 * them there. A page that deploys but answers wrongly, or silently fails to
 * start the WebAssembly module, is worse than one that fails to deploy — so
 * this runs before publishing and exits non-zero on any of:
 *
 *   - either implementation failing to load
 *   - any console error
 *   - any preset producing a divergence
 *   - a fuzz run finding a divergence, or not finishing
 *
 * Usage:  node scripts/verify-web.js
 */
'use strict';

const http = require('http');
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');
const web = path.join(root, 'web');
const PORT = 8137;
const CASES = Number(process.env.WEB_FUZZ_CASES || 300);

const TYPES = {
  '.html': 'text/html; charset=utf-8',
  '.js': 'text/javascript; charset=utf-8',
  '.css': 'text/css; charset=utf-8',
  '.wasm': 'application/wasm',
  '.svg': 'image/svg+xml',
};

function chromePath() {
  if (process.env.CHROME_PATH) return process.env.CHROME_PATH;
  const candidates = process.platform === 'win32'
    ? ['C:/Program Files/Google/Chrome/Application/chrome.exe',
       'C:/Program Files (x86)/Google/Chrome/Application/chrome.exe',
       'C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe']
    : ['/usr/bin/google-chrome', '/usr/bin/chromium-browser', '/usr/bin/chromium'];
  return candidates.find((p) => fs.existsSync(p));
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

(async () => {
  let puppeteer;
  try {
    puppeteer = require('puppeteer-core');
  } catch {
    console.error('puppeteer-core is not installed; run `npm ci`');
    process.exit(1);
  }

  const exe = chromePath();
  if (!exe) {
    console.error('No Chrome or Edge found. Set CHROME_PATH to a browser binary.');
    process.exit(1);
  }

  for (const f of ['index.html', 'app.js', 'style.css', 'cron.wasm', 'original.js', 'wasm_exec.js']) {
    if (!fs.existsSync(path.join(web, f))) {
      console.error(`web/${f} is missing. Run \`npm run build:web\` first.`);
      process.exit(1);
    }
  }

  const server = http.createServer((req, res) => {
    const rel = decodeURIComponent(req.url.split('?')[0]).replace(/^\//, '') || 'index.html';
    const file = path.join(web, rel);
    if (!file.startsWith(web) || !fs.existsSync(file) || fs.statSync(file).isDirectory()) {
      res.writeHead(404); return res.end('not found');
    }
    res.writeHead(200, { 'Content-Type': TYPES[path.extname(file)] || 'application/octet-stream' });
    fs.createReadStream(file).pipe(res);
  });
  await new Promise((r) => server.listen(PORT, r));

  const browser = await puppeteer.launch({
    executablePath: exe,
    headless: 'new',
    args: ['--no-sandbox', '--disable-dev-shm-usage'],
  });

  const failures = [];
  try {
    const page = await browser.newPage();
    await page.setViewport({ width: 1440, height: 1000 });
    page.on('console', (m) => { if (m.type() === 'error') failures.push(`console: ${m.text()}`); });
    page.on('pageerror', (e) => failures.push(`pageerror: ${e.message}`));

    await page.goto(`http://localhost:${PORT}/index.html`, { waitUntil: 'networkidle0', timeout: 120000 });
    await page.waitForFunction(() => !document.getElementById('boot'), { timeout: 120000 })
      .catch(() => failures.push('the port never finished loading'));

    const state = () => page.evaluate(() => ({
      s: document.getElementById('verdict').dataset.state,
      t: document.getElementById('verdictText').textContent,
      ts: document.getElementById('rowsTs').children.length,
      go: document.getElementById('rowsGo').children.length,
    }));

    const acceptable = new Set(['match', 'rejected']);

    const presets = await page.evaluate(() => document.querySelectorAll('#presets .chip').length);
    for (let i = 0; i < presets; i++) {
      await page.evaluate((i) => document.querySelectorAll('#presets .chip')[i].click(), i);
      await sleep(200);
      const v = await state();
      if (!acceptable.has(v.s)) failures.push(`preset ${i}: ${v.s} — ${v.t}`);
      if (v.ts === 0 || v.go === 0) failures.push(`preset ${i}: a column rendered nothing`);
    }
    console.log(`  ${presets} presets checked`);


    await page.evaluate((n) => {
      const b = document.getElementById('fRun');
      window.__fuzzTarget = n;
      b.click();
    }, CASES);
    await page.waitForFunction(
      () => document.getElementById('fRun').textContent.includes('more'),
      { timeout: 300000 },
    ).catch(() => failures.push('the fuzz run did not finish'));

    const fuzz = await page.evaluate(() => ({
      cases: document.getElementById('fCases').textContent,
      checks: document.getElementById('fChecks').textContent,
      div: document.getElementById('fDiv').textContent,
    }));
    if (fuzz.div !== '0') failures.push(`the fuzzer found ${fuzz.div} divergences`);
    console.log(`  fuzz: ${fuzz.cases} cases, ${fuzz.checks} comparisons, ${fuzz.div} divergences`);
  } finally {
    await browser.close();
    server.close();
  }

  if (failures.length) {
    console.error('\nthe playground is not working:');
    for (const f of failures) console.error(`  ${f}`);
    process.exit(1);
  }
  console.log('\nplayground verified: both implementations load and agree in a real browser');
})();
