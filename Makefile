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

## build-wasm: compile the bridge and stage everything the adapter loads
##   The module is embedded as base64 because the adapter cannot read it from
##   disk: the original's file-parser tests replace the filesystem module.
build-wasm:
	GOOS=js GOARCH=wasm $(GO) build -o $(WASM) ./bridge
	cp "$(GOROOT_)/lib/wasm/wasm_exec.js" adapter/wasm_exec.js
	node scripts/embed-wasm.js

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

## verify-schedule: differential-check the engine against the reference
##   Sweeps randomly generated expressions across zones and DST transitions,
##   comparing both iteration directions and every error message.
##   Override the seed with CRON_CORPUS_SEED to cover fresh ground.
verify-schedule:
	CRON_GEN_CORPUS=1 $(GO) test -run TestGenerateScheduleCorpus ./cron/
	node scripts/probe/verify-schedule.js

## verify-all: every differential check
verify-all: verify-time verify-schedule

## fuzz: differential fuzzing against the TypeScript original
fuzz: build-wasm
	node fuzz/differential.js

## bench: native Go benchmarks
bench:
	$(GO) test ./cron/... -bench=. -benchmem -run=^$$

clean:
	rm -f $(WASM) adapter/wasm_exec.js adapter/wasm-bytes.js
	$(GO) clean
