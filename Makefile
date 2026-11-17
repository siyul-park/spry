# The one place for pinned tool versions: init and CI install exactly these.
# LIGHTHOUSE_VERSION is read from .lighthouse-version; the npm tools
# (prettier, markdownlint-cli2) are pinned in package.json and package-lock.json.
# Every release archive is verified against the SHA-256 below before it is
# extracted. A version bump also updates that tool's four digests: read them from
# the release's SHA256SUMS or `gh api repos/<owner>/<repo>/releases/tags/<tag> --jq '.assets[].digest'`.
LIGHTHOUSE_VERSION := $(shell cat .lighthouse-version)
RUFF_VERSION := 0.17.3
LYCHEE_VERSION := 0.24.2
TARGETS := x86_64-unknown-linux-gnu aarch64-unknown-linux-gnu aarch64-apple-darwin x86_64-apple-darwin
LIGHTHOUSE_SHA256_x86_64-unknown-linux-gnu := cafc85ecdb2bdeacf4dee5cc2b153faccba21e6ea5273fad4ed136f33fb35966
LIGHTHOUSE_SHA256_aarch64-unknown-linux-gnu := d652b262e3e17c6c58ee620d54c994a60c8c3da457ec5167a23a450418297a47
LIGHTHOUSE_SHA256_aarch64-apple-darwin := ac3c95d4ecfcbf2045a3253218b115ae29f69aba86ba83ba952a2e399ce362b9
LIGHTHOUSE_SHA256_x86_64-apple-darwin := 68a1759e7ff0ce270feb627ee71e6602e4887e5e4f4266196c406c02b329c9de
RUFF_SHA256_x86_64-unknown-linux-gnu := 2b0890f5a55900af741f133918c2656919bbf18c2ab7741b4ca8403a90e647a9
RUFF_SHA256_aarch64-unknown-linux-gnu := 3f4639152c0e42bb5938e6c23d53d527a38f3133e000102934518f90529fa299
RUFF_SHA256_aarch64-apple-darwin := 118f7121aa838785672bf612487f755a89108bb5f44e0f3df96c5967a72614f9
RUFF_SHA256_x86_64-apple-darwin := 17f95b1bee24d70de62ffe5beec7f71bb541c7e3d147a1c3d84f7cd6525f6fdf
LYCHEE_SHA256_x86_64-unknown-linux-gnu := 1f4e0ef7f6554a6ed33dd7ac144fb2e1bbed98598e7af973042fc5cd43951c9a
LYCHEE_SHA256_aarch64-unknown-linux-gnu := 91a7bd65685da41b90ccb9bc867a3d649a7818042dae04ff405e55a25bddee4c
LYCHEE_SHA256_aarch64-apple-darwin := c9d3740ea2d891854d37116c9fba840f37b6e7c89d330e7db84ac333631c4977
LYCHEE_SHA256_x86_64-apple-darwin := 887503a9cff667d322b8d0892b40bf49976eb9507af8483220a3706cdad55978
# Where install-lighthouse and install-lint-tools put executables (<prefix>/bin).
PREFIX ?= $(HOME)/.local
export PATH := $(CURDIR)/node_modules/.bin:$(PREFIX)/bin:$(PATH)

fuzz-time ?= 10s
cover-profile ?= coverage.out

# fetch-release(TOOL,url): sets $$target, downloads the archive into a fresh
# $$work, and checks it against TOOL's pinned digest for $$target.
# The caller extracts and places the result, and $$work is removed on exit.
define fetch-release
case "$$(uname -s)-$$(uname -m)" in \
	Linux-x86_64) target=x86_64-unknown-linux-gnu ;; \
	Linux-aarch64|Linux-arm64) target=aarch64-unknown-linux-gnu ;; \
	Darwin-arm64) target=aarch64-apple-darwin ;; \
	Darwin-x86_64) target=x86_64-apple-darwin ;; \
	*) echo "$(1) has no release for $$(uname -s)-$$(uname -m)" >&2; exit 1 ;; \
esac; \
$(foreach t,$(TARGETS),test "$$target" != $(t) || sha=$($(1)_SHA256_$(t)); )\
work="$$(mktemp -d)"; \
trap 'rm -rf "$$work"' EXIT; \
curl -fsSL -o "$$work/archive.tar.gz" $(2) && \
echo "$$sha  $$work/archive.tar.gz" | shasum -a 256 -c - && \
mkdir -p "$$work/out"
endef

.PHONY: all init install-hooks install-lighthouse install-lint-tools fmt vet lighthouse lint test cover fuzz conformance bench check tidy build corpus corpus-extract

all: check

init:
	@$(MAKE) install-hooks
	@$(MAKE) install-lighthouse
	@$(MAKE) install-lint-tools

install-hooks:
	@git config core.hooksPath .githooks

# Lighthouse keeps its plugins next to the real executable, so the archive goes
# to <prefix>/lib/lighthouse/<version>/ and bin/lighthouse links to it.
install-lighthouse:
	@$(call fetch-release,LIGHTHOUSE,https://github.com/siyul-park/lighthouse/releases/download/v$(LIGHTHOUSE_VERSION)/lighthouse-$(LIGHTHOUSE_VERSION)-$$target.tar.gz) && \
	home="$(PREFIX)/lib/lighthouse/$(LIGHTHOUSE_VERSION)" && \
	rm -rf "$$home" && mkdir -p "$$home" "$(PREFIX)/bin" && \
	tar -xzf "$$work/archive.tar.gz" -C "$$home" --strip-components=1 && \
	ln -sf "$$home/lighthouse" "$(PREFIX)/bin/lighthouse"

# ruff and lychee are single executables from release archives for the host OS
# and arch (ubuntu-24.04 marks the system Python externally managed, so no pip).
# npm ci --include=dev installs the lockfile's tools even under NODE_ENV=production.
install-lint-tools:
	@npm ci --include=dev
	@$(call fetch-release,RUFF,https://github.com/astral-sh/ruff/releases/download/$(RUFF_VERSION)/ruff-$$target.tar.gz) && \
	tar -xzf "$$work/archive.tar.gz" -C "$$work/out" && \
	mkdir -p $(PREFIX)/bin && \
	install "$$(find "$$work/out" -name ruff -type f | head -n 1)" $(PREFIX)/bin/ruff
	@$(call fetch-release,LYCHEE,https://github.com/lycheeverse/lychee/releases/download/lychee-v$(LYCHEE_VERSION)/lychee-$$target.tar.gz) && \
	tar -xzf "$$work/archive.tar.gz" -C "$$work/out" && \
	mkdir -p $(PREFIX)/bin && \
	install "$$(find "$$work/out" -name lychee -type f | head -n 1)" $(PREFIX)/bin/lychee

fmt:
	@gofmt -w .

vet:
	@go vet ./...
	@(cd benchmarks && go vet ./...)

# Installed by make init at the version in .lighthouse-version.
lighthouse:
	@lighthouse check

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

check: corpus lint test conformance
	@go build ./...
	@test -z "$$(gofmt -l .)" || { echo "gofmt needed:"; gofmt -l .; exit 1; }

# Fetches the pinned CPython source into .cache/ (checksum verified, idempotent).
corpus:
	@go run ./internal/cpython/cmd/pycorpus fetch

# Regenerates the checked-in CPython manifests; needs python3.14 (3.14.9 for a clean diff).
corpus-extract: corpus
	@go run ./internal/cpython/cmd/pycorpus extract
