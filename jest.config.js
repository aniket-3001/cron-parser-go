/**
 * Runs the original cron-parser test suite against the Go port.
 *
 * The files in tests/original are byte-identical to upstream and must stay that
 * way: their SHA-256 is pinned and checked by scripts/verify-hashes.sh. They
 * import the library as '../src/...', so those specifiers are remapped here to
 * the adapter.
 *
 * moduleNameMapper matches the literal request string rather than a resolved
 * path, which is what lets the tests sit somewhere else on disk while their
 * imports stay untouched. This mapping is the whole trick that makes running
 * the suite unmodified possible.
 */
module.exports = {
  testEnvironment: 'node',
  testMatch: ['<rootDir>/tests/original/**/*.test.ts'],
  transform: {
    '^.+\\.ts$': [
      'ts-jest',
      {
        tsconfig: {
          target: 'ES2022',
          module: 'commonjs',
          // The WebAssembly namespace is declared in the DOM library rather
          // than in the Node types, even though Node implements it.
          lib: ['ES2022', 'DOM'],
          // The original suite is compiled as-is; it was written against the
          // upstream types, not against the adapter's.
          strict: false,
          esModuleInterop: true,
          skipLibCheck: true,
        },
        diagnostics: false,
      },
    ],
  },
  moduleNameMapper: {
    '^\\.\\./src$': '<rootDir>/adapter/src/index.ts',
    '^\\.\\./src/(.*)$': '<rootDir>/adapter/src/$1',
  },
  testTimeout: 30000,

  // Each worker instantiates its own copy of the port: a WebAssembly module
  // with the Go runtime and an embedded zone database. Running several in
  // parallel multiplies that, and Jest workers were crashing with out-of-memory
  // errors that surfaced as unrelated suites failing to load. Serialising the
  // run keeps one instance alive at a time and makes the result deterministic,
  // at the cost of a few seconds.
  maxWorkers: 1,

  // Recycle a worker that grows past this, so a long run cannot accumulate.
  workerIdleMemoryLimit: '512MB',
};
