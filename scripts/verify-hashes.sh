#!/usr/bin/env bash
# Proves tests/original/ is byte-identical to the upstream suite pinned at kickoff.
#
# The judges verify this independently. If it fails, the port's test-parity evidence
# is void -- so it runs in CI and should be run before every commit that touches tests/.
set -euo pipefail

cd "$(dirname "$0")/../tests/original"

if ! sha256sum --check --quiet HASHES.txt; then
  echo
  echo "FAIL: tests/original/ no longer matches the kickoff hashes." >&2
  echo "The original test files must never be edited. Restore them from upstream:" >&2
  echo "  harrisiirak/cron-parser @ aeb2a1513fd33365a6414f4137516c9482f831ed" >&2
  exit 1
fi

echo "OK: all 8 original test files match their kickoff SHA-256."
