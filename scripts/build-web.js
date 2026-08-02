/*
 * Builds the differential playground in web/.
 *
 * The page runs both implementations client-side, so both have to be built for
 * the browser: the port compiled to WebAssembly from ./bridge, and the original
 * bundled from ../cron-parser/dist. Neither is checked in: they are build
 * output, and a stale copy of either would make the page compare something
 * other than what the repository contains.
 *
 * Usage:  npm run build:web
 */
'use strict';

const { execFileSync } = require('child_process');
const fs = require('fs');
const path = require('path');

const root = path.join(__dirname, '..');
const web = path.join(root, 'web');
const upstream = path.join(root, '..', 'cron-parser');

function run(cmd, args, opts = {}) {
  console.log(`  ${cmd} ${args.join(' ')}`);
  execFileSync(cmd, args, { cwd: root, stdio: 'inherit', ...opts });
}

const mb = (p) => (fs.statSync(p).size / 1048576).toFixed(2);

console.log('compiling the port to WebAssembly');
run('go', ['build', '-o', path.join('web', 'cron.wasm'), './bridge'], {
  env: { ...process.env, GOOS: 'js', GOARCH: 'wasm' },
});

console.log('staging the WebAssembly host support');
const goroot = execFileSync('go', ['env', 'GOROOT'], { encoding: 'utf8' }).trim();
fs.copyFileSync(path.join(goroot, 'lib', 'wasm', 'wasm_exec.js'), path.join(web, 'wasm_exec.js'));

const dist = path.join(upstream, 'dist', 'index.js');
if (!fs.existsSync(dist)) {
  console.error(`\nCould not find the reference build at ${dist}`);
  console.error('Clone harrisiirak/cron-parser to ../cron-parser and run `npm run build` there.');
  process.exit(1);
}

console.log('bundling the original for the browser');
// The original declares `"browser": { "fs": false }`, so its file-parser entry
// point drops out cleanly and the rest bundles without a Node shim.
//
// esbuild is invoked through its JavaScript API rather than its CLI. Going
// through npx on Windows means either a shell, which splits the reference path
// on the space in "Port Mortem" and reads it as two input files, or no shell,
// which cannot spawn the .cmd shim at all. The API sidesteps both.
let esbuild;
try {
  esbuild = require('esbuild');
} catch {
  console.log('  installing esbuild');
  execFileSync(process.platform === 'win32' ? 'npm.cmd' : 'npm',
    ['install', '--no-save', '--silent', 'esbuild'],
    { cwd: root, stdio: 'inherit', shell: process.platform === 'win32' });
  esbuild = require('esbuild');
}

esbuild.buildSync({
  entryPoints: [dist],
  bundle: true,
  format: 'iife',
  globalName: 'CronOriginal',
  platform: 'browser',
  minify: true,
  outfile: path.join(web, 'original.js'),
  logLevel: 'warning',
});

console.log('\nplayground built in web/');
for (const f of ['cron.wasm', 'original.js', 'wasm_exec.js']) {
  console.log(`  ${f.padEnd(14)} ${mb(path.join(web, f))} MB`);
}
console.log('\nserve it with:  npx --yes serve web');
