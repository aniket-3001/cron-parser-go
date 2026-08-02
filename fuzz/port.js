/*
 * Minimal access to the Go port for the differential harness.
 *
 * The harness talks to the port directly rather than through adapter/src. The
 * adapter exists to make the original's TypeScript tests runnable and carries
 * the conversions that job needs; putting it in the middle here would mean
 * comparing the shim as much as the port, and would cost a layer of allocation
 * on every call. What is under comparison is the Go library's behaviour.
 */
'use strict';

const path = require('path');

require(path.join(__dirname, '..', 'adapter', 'wasm_exec.js'));
const encoded = require(path.join(__dirname, '..', 'adapter', 'wasm-bytes.js'));

const go = new globalThis.Go();
const instance = new WebAssembly.Instance(
  new WebAssembly.Module(Buffer.from(encoded, 'base64')),
  go.importObject,
);
void go.run(instance);

const bridge = globalThis.__cronBridge;
if (typeof bridge !== 'function') {
  throw new Error('the port loaded but did not install its bridge');
}

/** Calls into the port, throwing with the message the Go library produced. */
function call(op, ...args) {
  const r = bridge(op, ...args);
  if (r.e !== undefined) {
    throw new Error(r.e);
  }
  return r.v;
}

/**
 * A parsed expression in the port.
 *
 * Handles are released explicitly. A run of tens of thousands of cases would
 * otherwise hold every expression it ever parsed.
 */
class PortExpression {
  constructor(expression, options) {
    this.handle = call('parse', expression, options);
  }

  next() {
    return call('expr.next', this.handle);
  }

  prev() {
    return call('expr.prev', this.handle);
  }

  format(includeSeconds) {
    return call('expr.format', this.handle, includeSeconds);
  }

  includes(millis) {
    return call('expr.includes', this.handle, millis);
  }

  hasNext() {
    return call('expr.hasNext', this.handle);
  }

  hasPrev() {
    return call('expr.hasPrev', this.handle);
  }

  take(n) {
    return call('expr.take', this.handle, n);
  }

  /**
   * Passing null rather than omitting the argument is deliberate: the bridge
   * distinguishes a numeric instant from anything else to decide between
   * resetting to a given time and resetting to the initial one, and reading a
   * missing argument would trip the dispatcher instead.
   */
  reset(millis) {
    call('expr.reset', this.handle, millis === undefined ? null : millis);
  }

  string() {
    return call('expr.string', this.handle);
  }

  fieldValues() {
    const fields = call('expr.fields', this.handle);
    const read = (name) => {
      const f = call('fields.field', fields, name);
      const values = call('field.values', f);
      call('release', f);
      return values;
    };
    const out = {
      second: read('second'),
      minute: read('minute'),
      hour: read('hour'),
      dayOfMonth: read('dayOfMonth'),
      month: read('month'),
      dayOfWeek: read('dayOfWeek'),
    };
    call('release', fields);
    return out;
  }

  release() {
    call('release', this.handle);
  }
}

module.exports = { PortExpression };

/**
 * A date in the port.
 *
 * The original's CronDate is public API and its arithmetic is reachable
 * independently of any expression, year arithmetic, for one, is never used by
 * the search loop. Comparing expressions alone would leave that surface
 * unchecked.
 */
class PortDate {
  constructor(millis, tz) {
    this.handle = call('date.fromMillis', millis, tz);
  }

  apply(operation) {
    call('date.arith', this.handle, operation);
  }

  set(property, value) {
    call('date.set', this.handle, property, value);
  }

  boundary(which) {
    call(which === 'startOfDay' ? 'date.startOfDay' : 'date.endOfDay', this.handle);
  }

  get(property) {
    return call('date.get', this.handle, property);
  }

  release() {
    call('release', this.handle);
  }
}

module.exports.PortDate = PortDate;
