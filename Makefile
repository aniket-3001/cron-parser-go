# cron-parser-go
#
# Rule 3 of Port Mortem: one command builds a runnable artifact.
#   make build

GO      ?= go
WASM    := adapter/cron.wasm
GOROOT_ := $(shell $(GO) env GOROOT)

.PHONY: all build build-wasm test test-original verify-hashes fuzz bench clean

all: build

## build: compile the Go library and the js/wasm test bridge
build: build-wasm
	$(GO) build ./...

## build-wasm: compile the bridge and stage wasm_exec.js for the adapter
build-wasm:
	GOOS=js GOARCH=wasm $(GO) build -o $(WASM) ./bridge
	cp "$(GOROOT_)/lib/wasm/wasm_exec.js" adapter/wasm_exec.js

## test: native Go tests
test:
	$(GO) test ./cron/... -count=1

## verify-hashes: prove tests/original/ is byte-identical to upstream
verify-hashes:
	bash scripts/verify-hashes.sh

## test-original: run the 280 original Jest tests, unmodified, against the Go port
test-original: verify-hashes build-wasm
	npx cross-env TZ=UTC npx jest

## gen-fixtures: recapture parser fixtures from the reference implementation
##   The committed fixtures let `go test` verify the parser with no Node
##   present. Regenerate only when the reference version changes.
gen-fixtures:
	node scripts/probe/gen-parse-fixtures.js

## verify-time: differential-check the time layer against luxon
##   Generates a corpus of every CronDate operation applied to tens of thousands
##   of instants, weighted toward DST transitions, and compares against luxon.
##   Requires the reference clone at ../cron-parser with node_modules installed.
verify-time:
	CRON_GEN_CORPUS=1 $(GO) test -run TestGenerateTimeOpCorpus ./cron/
	node scripts/probe/verify-time-ops.js

## fuzz: differential fuzzing against the TypeScript original
fuzz: build-wasm
	node fuzz/differential.js

## bench: native Go benchmarks
bench:
	$(GO) test ./cron/... -bench=. -benchmem -run=^$$

clean:
	rm -f $(WASM) adapter/wasm_exec.js
	$(GO) clean
