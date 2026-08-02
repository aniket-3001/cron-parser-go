/* cron-parser-go — differential playground
 *
 * Both implementations run here, in the page. The original is the published
 * TypeScript bundle; the port is the same Go source the test suite runs
 * against, compiled to WebAssembly. Neither answer comes from a server, and
 * neither is precomputed: every number on screen is produced on demand by the
 * implementation named above it.
 */
'use strict';

const $ = (id) => document.getElementById(id);

/* ---------------------------------------------------------------- the port */

/**
 * Calls into the Go bridge, which answers {v} or {e}. Failures are turned into
 * exceptions so both implementations can be driven through identical code and
 * a rejection compares as a rejection rather than as a missing value.
 */
function go(op, ...args) {
  const r = globalThis.__cronBridge(op, ...args);
  if (r && r.e) throw new Error(r.e);
  return r ? r.v : undefined;
}

const Port = {
  parse(expr, opts) {
    const cfg = { tz: opts.tz };
    if (opts.currentDate != null) cfg.currentDate = opts.currentDate;
    if (opts.hashSeed) cfg.hashSeed = opts.hashSeed;
    return go('parse', expr, cfg);
  },
  next: (h) => go('expr.next', h),
  prev: (h) => go('expr.prev', h),
  format: (h) => go('expr.format', h, false),
  fields(h) {
    const fh = go('expr.fields', h);
    try { return go('fields.serialize', fh); } finally { go('release', fh); }
  },
  release: (h) => { try { go('release', h); } catch { /* already gone */ } },
};

/* ------------------------------------------------------------ the original */

const Original = {
  parse(expr, opts) {
    const o = { tz: opts.tz };
    if (opts.currentDate != null) o.currentDate = new Date(opts.currentDate);
    if (opts.hashSeed) o.hashSeed = opts.hashSeed;
    return CronOriginal.CronExpressionParser.parse(expr, o);
  },
  next: (e) => e.next().getTime(),
  prev: (e) => e.prev().getTime(),
  format: (e) => e.stringify(false),
  fields(e) {
    const f = e.fields;
    const one = (x) => ({ wildcard: x.isWildcard, values: x.values.map(String) });
    return {
      second: one(f.second), minute: one(f.minute), hour: one(f.hour),
      dayOfMonth: one(f.dayOfMonth), month: one(f.month), dayOfWeek: one(f.dayOfWeek),
    };
  },
  release: () => {},
};

/* ------------------------------------------------------------------ shared */

const ZONES = [
  ['UTC', 'UTC'],
  ['America/New_York', 'New York — 1h spring gap'],
  ['Europe/London', 'London'],
  ['Europe/Athens', 'Athens'],
  ['Antarctica/Troll', 'Troll — 2h gap, skips a day'],
  ['Australia/Lord_Howe', 'Lord Howe — 30 min shift'],
  ['Pacific/Chatham', 'Chatham — 45 min offset'],
  ['America/Santiago', 'Santiago — midnight transition'],
  ['Asia/Kolkata', 'Kolkata — half-hour offset'],
  ['Asia/Kathmandu', 'Kathmandu — 45 min offset'],
  ['America/Havana', 'Havana'],
  ['Pacific/Auckland', 'Auckland'],
  ['Asia/Tehran', 'Tehran'],
  ['America/Sao_Paulo', 'São Paulo — DST abolished'],
];

const PRESETS = [
  { label: 'Weekday business hours', expr: '*/15 9-17 * * 1-5', tz: 'UTC', from: '2026-01-01T00:00:00Z' },
  { label: 'Into a 1-hour DST gap', expr: '30 2 * * *', tz: 'America/New_York', from: '2026-03-06T12:00:00Z' },
  { label: 'Into a 2-hour DST gap', expr: '30 1 * * *', tz: 'Antarctica/Troll', from: '2026-03-27T12:00:00Z' },
  { label: 'Half-hour DST shift', expr: '30 2 * * *', tz: 'Australia/Lord_Howe', from: '2026-10-02T12:00:00Z' },
  { label: 'Last day of the month', expr: '0 0 L * *', tz: 'UTC', from: '2026-01-01T00:00:00Z' },
  { label: 'Last Friday', expr: '0 0 * * 5L', tz: 'UTC', from: '2026-01-01T00:00:00Z' },
  { label: 'Second Monday', expr: '0 0 * * MON#2', tz: 'UTC', from: '2026-01-01T00:00:00Z' },
  { label: 'The OR rule: 13th or Friday', expr: '0 0 13 * 5', tz: 'UTC', from: '2026-02-01T00:00:00Z' },
  { label: 'Leap day only', expr: '0 0 29 2 *', tz: 'UTC', from: '2026-01-01T00:00:00Z' },
  { label: 'Hashed, seeded', expr: 'H H * * *', tz: 'UTC', from: '2026-01-01T00:00:00Z', seed: 'port-mortem' },
  { label: 'Every second', expr: '* * * * * *', tz: 'UTC', from: '2026-01-01T00:00:00Z' },
  { label: 'Rejected: hour 24', expr: '* 24 * * *', tz: 'UTC', from: '2026-01-01T00:00:00Z' },
];


/** Wall-clock rendering in the selected zone. The instant is what is compared;
 *  this is only how it is shown. */
function render(ms, tz) {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: tz, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false,
    timeZoneName: 'shortOffset',
  }).formatToParts(new Date(ms));
  const g = (t) => (parts.find((p) => p.type === t) || {}).value || '';
  return {
    local: `${g('year')}-${g('month')}-${g('day')}  ${g('hour')}:${g('minute')}:${g('second')}`,
    off: g('timeZoneName').replace('GMT', 'UTC'),
  };
}

/**
 * Runs one question against one implementation and captures everything
 * observable: the instants, or the exact text of whatever went wrong.
 */
function observe(impl, expr, opts, count, dir) {
  try {
    const h = impl.parse(expr, opts);
    try {
      const out = { ok: true, times: [], error: null, canonical: null, fields: null };
      try { out.canonical = impl.format(h); } catch { /* rendering can fail on its own */ }
      try { out.fields = impl.fields(h); } catch { /* ditto */ }
      const step = dir === 'prev' ? impl.prev : impl.next;
      for (let i = 0; i < count; i++) {
        try { out.times.push(step.call(impl, h)); } catch (e) { out.error = e.message; break; }
      }
      return out;
    } finally { impl.release(h); }
  } catch (e) {
    return { ok: false, times: [], error: e.message, canonical: null, fields: null };
  }
}

/* --------------------------------------------------------------------- UI */

const els = {};
let booted = false;

function readInputs() {
  const fromRaw = els.from.value.trim();
  const parsed = Date.parse(fromRaw);
  const bad = Number.isNaN(parsed);
  els.from.setAttribute('aria-invalid', String(bad));
  return {
    expr: els.expr.value.trim(),
    tz: els.tz.value,
    from: bad ? Date.parse('2026-01-01T00:00:00Z') : parsed,
    fromValid: !bad,
    count: Number(els.count.value),
    dir: els.dir.value,
    seed: els.seed || '',
  };
}

function paintRows(ul, obs, other, tz) {
  ul.textContent = '';
  const frag = document.createDocumentFragment();

  obs.times.forEach((ms, i) => {
    const li = document.createElement('li');
    const differs = other.times[i] !== ms;
    if (differs) li.className = 'is-diff';
    li.classList.add('enter');
    li.style.animationDelay = `${Math.min(i, 12) * 22}ms`;
    const { local, off } = render(ms, tz);
    li.innerHTML =
      `<span class="idx">${String(i + 1).padStart(2, '0')}</span>` +
      `<span class="local"></span><span class="off"></span>`;
    li.querySelector('.local').textContent = local;
    li.querySelector('.off').textContent = off;
    frag.appendChild(li);
  });

  if (obs.error) {
    const li = document.createElement('li');
    li.className = 'is-err enter';
    li.textContent = obs.error;
    frag.appendChild(li);
  }
  if (!obs.times.length && !obs.error) {
    const li = document.createElement('li');
    li.className = 'is-note';
    li.textContent = 'no occurrences';
    frag.appendChild(li);
  }
  ul.appendChild(frag);
}

function paintFields(a, b) {
  els.fieldsGrid.textContent = '';
  const names = ['second', 'minute', 'hour', 'dayOfMonth', 'month', 'dayOfWeek'];
  const src = a.fields || b.fields;
  if (!src) { els.fieldsBox.hidden = true; return; }
  els.fieldsBox.hidden = false;

  for (const n of names) {
    const fa = a.fields && a.fields[n];
    const fb = b.fields && b.fields[n];
    const same = JSON.stringify(fa) === JSON.stringify(fb);
    const cell = document.createElement('div');
    cell.className = 'fcell' + (fa && fa.wildcard ? ' fcell--wild' : '');
    const v = fa ? (fa.wildcard ? '* (wildcard)' : fa.values.join(',')) : '—';
    cell.innerHTML = '<div class="fcell__k"></div><div class="fcell__v"></div>';
    cell.querySelector('.fcell__k').textContent = n.replace(/([A-Z])/g, ' $1');
    cell.querySelector('.fcell__v').textContent = same ? v : `${v}  ≠  ${fb ? fb.values.join(',') : '—'}`;
    if (!same) cell.style.borderLeft = '2px solid var(--bad)';
    els.fieldsGrid.appendChild(cell);
  }

  els.canonical.textContent =
    a.canonical && b.canonical
      ? (a.canonical === b.canonical
        ? `stringify() → ${a.canonical}   (identical)`
        : `stringify() differs → original ${a.canonical} · port ${b.canonical}`)
      : '';
}

function setVerdict(state, text, meta) {
  els.verdict.dataset.state = state;
  els.verdictText.textContent = text;
  els.verdictMeta.textContent = meta || '';
}

/** Compares two observations and returns the first difference, or null. */
function firstDifference(a, b) {
  if (a.ok !== b.ok) return `one accepted the expression and the other did not`;
  if (!a.ok) return a.error === b.error ? null : `rejection text differs`;
  if (a.times.length !== b.times.length) return `different number of occurrences`;
  for (let i = 0; i < a.times.length; i++) {
    if (a.times[i] !== b.times[i]) return `occurrence ${i + 1} differs`;
  }
  if ((a.error || null) !== (b.error || null)) return `iteration stopped differently`;
  if ((a.canonical || null) !== (b.canonical || null)) return `stringify() differs`;
  return null;
}

let runToken = 0;
function run() {
  if (!booted) return;
  const token = ++runToken;
  const inp = readInputs();
  if (!inp.expr) { setVerdict('idle', 'Type an expression'); return; }

  const opts = { tz: inp.tz, currentDate: inp.from, hashSeed: inp.seed };
  const t0 = performance.now();
  const a = observe(Original, inp.expr, opts, inp.count, inp.dir);
  const b = observe(Port, inp.expr, opts, inp.count, inp.dir);
  const ms = performance.now() - t0;
  if (token !== runToken) return;

  paintRows(els.rowsTs, a, b, inp.tz);
  paintRows(els.rowsGo, b, a, inp.tz);
  paintFields(a, b);

  const diff = firstDifference(a, b);
  const checks = a.times.length + b.times.length + 2;
  if (diff) {
    setVerdict('diverge', 'Divergence', diff);
  } else if (!a.ok) {
    setVerdict('rejected', 'Both rejected it, with the same message', `${ms.toFixed(1)} ms`);
  } else if (a.error && !a.times.length) {
    // Parsed, then failed on the first occurrence — and failed the same way.
    setVerdict('rejected', 'Both parsed it, then failed identically', `${ms.toFixed(1)} ms`);
  } else if (a.error) {
    setVerdict('match', 'Identical, and both stopped at the same point',
      `${checks} values compared · ${ms.toFixed(1)} ms`);
  } else {
    setVerdict('match', 'Identical', `${checks} values compared · ${ms.toFixed(1)} ms`);
  }
}

/* --------------------------------------------------------------- the fuzzer */

const FUZZ = (() => {
  const FIELDS = [
    ['0', '*', '*/5', '30', '0,30', '15-45', '*/13', '7'],
    ['*', '0', '*/15', '30', '1,31,59', '0-30/7', 'H', 'H/20'],
    ['*', '0', '9-17', '*/3', '2', '0,12', 'H', '1,4-10'],
    ['*', '1', 'L', '15', '1,15', '29', '*/9', '1-28/4'],
    ['*', '1', '2', 'JAN', '*/3', '6', '1-6', 'FEB-JUN'],
    ['*', '0', '5L', 'MON#2', '1-5', 'SUN', '0,7', '*/2'],
  ];
  const rand = (n) => Math.floor(Math.random() * n);
  const pick = (a) => a[rand(a.length)];

  function expression() {
    const n = Math.random() < 0.25 ? 6 : 5;
    const parts = [];
    for (let i = 0; i < n; i++) parts.push(pick(FIELDS[n === 6 ? i : i + 1]));
    // A quarter of cases are deliberately malformed: agreeing on a rejection,
    // and on its wording, is as much a claim as agreeing on a date.
    if (Math.random() < 0.25) {
      const broken = ['61 * * * *', '* 24 * * *', '* * 32 * *', '* * * 13 *', '* * * * 8',
        'x', '*/0 * * * *', '5/5/5 * * * *', '30-20 * * * *', '1,,2 * * * *',
        '0 0 15W * *', '0 0 * * L', '* * * * * * *'];
      return pick(broken);
    }
    return parts.join(' ');
  }

  function instant() {
    // Weighted toward transitions, where the two could actually disagree.
    const anchors = [
      Date.parse('2026-03-08T00:00:00Z'), Date.parse('2026-03-29T00:00:00Z'),
      Date.parse('2026-10-25T00:00:00Z'), Date.parse('2026-11-01T00:00:00Z'),
      Date.parse('2026-09-06T00:00:00Z'), Date.parse('2024-02-29T00:00:00Z'),
    ];
    if (Math.random() < 0.55) return pick(anchors) + (rand(48) - 24) * 3600e3;
    return Date.parse('2026-01-01T00:00:00Z') + rand(365 * 24) * 3600e3;
  }

  return { expression, instant, zone: () => pick(ZONES)[0] };
})();

let fuzzRunning = false;
let fuzzState = { cases: 0, checks: 0, div: 0, target: 500 };

function fuzzTick() {
  if (!fuzzRunning) return;
  const budgetEnd = performance.now() + 24; // keep frames responsive

  while (performance.now() < budgetEnd && fuzzState.cases < fuzzState.target) {
    const expr = FUZZ.expression();
    const tz = FUZZ.zone();
    const from = FUZZ.instant();
    const opts = { tz, currentDate: from, hashSeed: 'playground' };

    const a = observe(Original, expr, opts, 5, 'next');
    const b = observe(Port, expr, opts, 5, 'next');
    const diff = firstDifference(a, b);

    fuzzState.cases++;
    fuzzState.checks += a.times.length + b.times.length + 2;
    if (diff) fuzzState.div++;

    if (fuzzState.cases % 17 === 0 || diff) {
      addTick(expr, tz, diff);
    }
  }

  els.fCases.textContent = fuzzState.cases.toLocaleString();
  els.fChecks.textContent = fuzzState.checks.toLocaleString();
  els.fDiv.textContent = fuzzState.div.toLocaleString();
  els.fDivBox.dataset.bad = String(fuzzState.div > 0);
  els.fBar.style.width = `${(fuzzState.cases / fuzzState.target) * 100}%`;

  if (fuzzState.cases >= fuzzState.target) {
    stopFuzz(true);
    return;
  }
  requestAnimationFrame(fuzzTick);
}

function addTick(expr, tz, diff) {
  const li = document.createElement('li');
  li.innerHTML = `<span class="${diff ? 'no' : 'ok'}">${diff ? '✕' : '✓'}</span>` +
    `<span class="ticker__expr"><b></b></span><span></span>`;
  li.querySelector('b').textContent = expr;
  li.lastElementChild.textContent = diff ? diff : tz;
  els.fTicker.prepend(li);
  while (els.fTicker.children.length > 40) els.fTicker.lastElementChild.remove();
}

function startFuzz() {
  fuzzState = { cases: 0, checks: 0, div: 0, target: 500 };
  els.fTicker.textContent = '';
  fuzzRunning = true;
  els.fRun.disabled = true;
  els.fStop.disabled = false;
  els.fRun.textContent = 'Running…';
  requestAnimationFrame(fuzzTick);
}

function stopFuzz(finished) {
  fuzzRunning = false;
  els.fRun.disabled = false;
  els.fStop.disabled = true;
  els.fRun.textContent = finished ? 'Run 500 more' : 'Run 500 cases';
  if (finished && fuzzState.div === 0) {
    addTick(`${fuzzState.cases} cases · ${fuzzState.checks.toLocaleString()} comparisons · no divergence`, '', null);
  }
}

/* ------------------------------------------------------------------- boot */

function buildChrome() {
  els.tz.innerHTML = ZONES
    .map(([v, l]) => `<option value="${v}">${l}</option>`).join('');

  els.presets.innerHTML = PRESETS
    .map((p, i) => `<button class="chip" type="button" data-i="${i}">${p.label}</button>`).join('');
  els.presets.addEventListener('click', (e) => {
    const b = e.target.closest('.chip');
    if (!b) return;
    const p = PRESETS[Number(b.dataset.i)];
    els.expr.value = p.expr;
    els.tz.value = p.tz;
    els.from.value = p.from;
    els.seed = p.seed || '';
    for (const c of els.presets.querySelectorAll('.chip')) c.setAttribute('aria-pressed', 'false');
    b.setAttribute('aria-pressed', 'true');
    run();
  });


  let t;
  const debounced = () => { clearTimeout(t); t = setTimeout(run, 140); };
  els.expr.addEventListener('input', debounced);
  els.from.addEventListener('input', debounced);
  for (const el of [els.tz, els.count, els.dir]) el.addEventListener('change', run);

  els.fRun.addEventListener('click', startFuzz);
  els.fStop.addEventListener('click', () => stopFuzz(false));
}

async function boot() {
  for (const id of ['boot', 'bootHint', 'expr', 'tz', 'from', 'count', 'dir', 'presets',
    'verdict', 'verdictText', 'verdictMeta', 'rowsTs', 'rowsGo', 'fieldsBox', 'fieldsGrid',
    'canonical', 'fCases', 'fChecks', 'fDiv', 'fDivBox', 'fBar', 'fRun', 'fStop', 'fTicker']) {
    els[id] = $(id);
  }
  els.seed = '';

  try {
    const goRt = new Go();
    const resp = await fetch('cron.wasm');
    if (!resp.ok) throw new Error(`could not fetch cron.wasm (${resp.status})`);
    const total = Number(resp.headers.get('content-length')) || 0;
    const bytes = await resp.arrayBuffer();
    if (total) els.bootHint.textContent = `${(bytes.byteLength / 1048576).toFixed(1)} MB of WebAssembly, compiled from ./cron`;
    const { instance } = await WebAssembly.instantiate(bytes, goRt.importObject);
    goRt.run(instance);
    if (!globalThis.__cronBridgeReady) throw new Error('the Go bridge did not come up');
  } catch (err) {
    els.bootHint.textContent = String(err.message || err);
    els.boot.querySelector('.boot__label').textContent = 'The port failed to load';
    return;
  }

  booted = true;
  buildChrome();
  run();
  els.boot.setAttribute('hidden-soft', '');
  setTimeout(() => els.boot.remove(), 600);
}

boot();
