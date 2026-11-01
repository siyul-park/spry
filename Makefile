fuzz-time ?= 10s
cover-profile ?= coverage.out

.PHONY: all fmt vet lighthouse lint test cover fuzz conformance bench check tidy build

all: check

fmt:
	@gofmt -w .

vet:
	@go vet ./...
	@(cd benchmarks && go vet ./...)

# Skips with a message when lighthouse is not installed, so CI without it stays green.
lighthouse:
	@if command -v lighthouse >/dev/null 2>&1; then \
		lighthouse check; \
	else \
		echo "lighthouse not installed; skipping design checks"; \
	fi

lint: vet lighthouse

test:
	@go test -race ./...
	@(cd benchmarks && go test -race ./...)

cover:
	@go test -race -covermode=atomic -coverprofile=$(cover-profile) ./...
	@go tool cover -func=$(cover-profile) | tail -n 1

# Fuzz targets appear with the scanner and parser (phase P1).
fuzz:
	@targets="$$(grep -rl --include='*_test.go' '^func Fuzz' . || true)"; \
	if [ -z "$$targets" ]; then echo "no fuzz targets yet"; exit 0; fi; \
	for file in $$targets; do \
		dir="$$(dirname $$file)"; \
		for name in $$(grep -h '^func Fuzz' $$file | sed 's/func \(Fuzz[A-Za-z0-9_]*\).*/\1/'); do \
			go test -run='^$$' -fuzz="^$$name$$" -fuzztime=$(fuzz-time) $$dir || exit 1; \
		done; \
	done

conformance:
	@go test -count=1 ./conformance/...

bench:
	@(cd benchmarks && go run ./cmd/bench -o ../docs/benchmarks.md)

build:
	@mkdir -p bin
	@go build -o bin/spry ./cmd/spry

tidy:
	@go mod tidy
	@(cd benchmarks && go mod tidy)

check: lint test conformance
	@go build ./...
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }
