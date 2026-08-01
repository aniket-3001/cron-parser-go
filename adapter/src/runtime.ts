/**
 * Loads the Go port, compiled to WebAssembly, and exposes a synchronous call
 * into it.
 *
 * Everything in adapter/src is a shim. It holds no scheduling logic: each
 * method forwards to Go and converts the result. The shim exists so the
 * original TypeScript test suite can run unmodified against the port.
 */

declare global {
  // eslint-disable-next-line no-var
  var Go: new () => { importObject: WebAssembly.Imports; run(i: WebAssembly.Instance): void };
  // eslint-disable-next-line no-var
  var __cronBridge: ((op: string, ...args: unknown[]) => { v?: unknown; e?: string }) | undefined;
}

// eslint-disable-next-line @typescript-eslint/no-require-imports
require('../wasm_exec.js');

/**
 * The port is embedded as base64 rather than read from disk.
 *
 * The original's CronFileParser tests replace the filesystem module, and Jest's
 * replacement covers the node: prefixed specifier too, so a read here would
 * return undefined the moment those tests ran. Embedding removes the filesystem
 * from the load path entirely. scripts/embed-wasm.js generates the module.
 */
// eslint-disable-next-line @typescript-eslint/no-require-imports
const encoded = require('../wasm-bytes.js') as string;

/**
 * Instantiates the port synchronously.
 *
 * This matters: the original API is entirely synchronous, and the tests assert
 * on returned values rather than on promises. Node places no size limit on the
 * synchronous WebAssembly constructors, unlike browsers, so the module can be
 * compiled during module initialisation.
 */
function boot(): (op: string, ...args: unknown[]) => { v?: unknown; e?: string } {
  const go = new globalThis.Go();
  const instance = new WebAssembly.Instance(
    new WebAssembly.Module(Buffer.from(encoded, 'base64')),
    go.importObject,
  );

  // go.run starts the Go runtime and returns a promise that never settles,
  // because the program parks in a select with no cases. The exported function
  // is installed before it parks, so it is callable the moment run returns.
  void go.run(instance);

  const bridge = globalThis.__cronBridge;
  if (typeof bridge !== 'function') {
    throw new Error('the port loaded but did not install its bridge');
  }
  return bridge;
}

const bridge = boot();

/**
 * Calls into the port.
 *
 * Results are wrapped so that a failure is distinguishable from a legitimate
 * null. Errors carry the message the Go library produced, which is the same
 * text the original throws.
 */
export function call<T = unknown>(op: string, ...args: unknown[]): T {
  const result = bridge(op, ...args);
  if (result.e !== undefined) {
    throw new Error(result.e);
  }
  return result.v as T;
}

/**
 * Releases a handle held by the port.
 *
 * Handles are also released once their wrapper becomes unreachable, so callers
 * rarely need this.
 */
export function release(handle: number): void {
  call('release', handle);
}

/**
 * Frees port-side objects when their JavaScript wrappers are collected, so that
 * a long test run does not grow the handle registry without bound.
 *
 * FinalizationRegistry gives no guarantee about when, or whether, it runs. That
 * is acceptable here: the registry is a cache whose worst case is holding
 * objects until the process exits.
 */
const finalizer =
  typeof FinalizationRegistry === 'function'
    ? new FinalizationRegistry((handle: number) => {
        try {
          release(handle);
        } catch {
          // The module may already be torn down; nothing useful to do.
        }
      })
    : undefined;

/** Registers a wrapper so its handle is released when the wrapper is collected. */
export function trackHandle(owner: object, handle: number): void {
  finalizer?.register(owner, handle);
}
