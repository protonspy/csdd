# csdd — build, test, and npm release tasks.
#
# Day-to-day:
#   make build                       # local binary -> ./csdd
#   make check                       # gofmt + vet + race tests (the CI gate)
#
# Release (recommended — CI builds + publishes to npm on the pushed tag):
#   make release VERSION=v0.2.0
#
# Manual npm publish (bootstrap / fallback, when CI can't do it):
#   make dist      VERSION=v0.2.0    # cross-compile all 5 targets into dist/
#   make npm-build VERSION=v0.2.0    # assemble npm/dist/ from those artifacts
#   make npm-dry-run                 # validate all 6 packages without publishing
#   make npm-publish [OTP=123456]    # publish the 6 packages (skips already-published)
#
# Auth for a manual publish: either an Automation token
#   npm config set //registry.npmjs.org/:_authToken <token>
# or pass a fresh 2FA code:  make npm-publish OTP=123456

SHELL     := bash
MODULE    := github.com/protonspy/csdd
VERSION   ?= dev
LDFLAGS   := -s -w -X $(MODULE)/cmd.version=$(VERSION)
DIST      ?= dist
ARTIFACTS ?= $(DIST)

# GOOS/GOARCH targets shipped to npm — must match npm/scripts/build-packages.mjs.
PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

.DEFAULT_GOAL := help

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z0-9_-]+:.*## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN{FS=":.*## "}{printf "  \033[36m%-14s\033[0m %s\n",$$1,$$2}'

.PHONY: build
build: ## Build the binary locally (-> ./csdd); set VERSION to stamp it
	go build -trimpath -ldflags '$(LDFLAGS)' -o csdd .

.PHONY: test
test: ## Run tests (race + coverage)
	go test -race -coverprofile=coverage.out ./...

.PHONY: fmt
fmt: ## Fail if any file is not gofmt-clean
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then echo "not gofmt-clean:"; echo "$$unformatted"; exit 1; fi; \
	echo "gofmt clean"

.PHONY: vet
vet: ## go vet ./...
	go vet ./...

.PHONY: check
check: fmt vet test ## Run the full CI gate (gofmt + vet + race tests)

.PHONY: dist
dist: ## Cross-compile every npm target into $(DIST)/ (set VERSION=vX.Y.Z)
	@rm -rf '$(DIST)' && mkdir -p '$(DIST)'
	@set -euo pipefail; for p in $(PLATFORMS); do \
	  goos=$${p%/*}; goarch=$${p#*/}; bin=csdd; [ "$$goos" = windows ] && bin=csdd.exe; \
	  echo ">> $$goos/$$goarch"; \
	  GOOS=$$goos GOARCH=$$goarch CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o '$(DIST)'/$$bin .; \
	  name=csdd_$(VERSION)_$${goos}_$${goarch}; \
	  if [ "$$goos" = windows ]; then (cd '$(DIST)' && zip -q $$name.zip $$bin); \
	  else (cd '$(DIST)' && tar -czf $$name.tar.gz $$bin); fi; \
	  rm -f '$(DIST)'/$$bin; \
	done; \
	echo "artifacts in $(DIST)/"

.PHONY: npm-build
npm-build: ## Assemble npm/dist/ from artifacts (set VERSION=vX.Y.Z; ARTIFACTS=dir)
	node npm/scripts/build-packages.mjs '$(VERSION)' '$(ARTIFACTS)'

.PHONY: npm-dry-run
npm-dry-run: ## Dry-run publish every assembled package
	@set -euo pipefail; for d in npm/dist/csdd-*/ npm/dist/csdd/; do \
	  echo "== $$d"; npm publish "$$d" --access public --dry-run; done

.PHONY: npm-publish
npm-publish: ## Publish the 6 packages, platforms first (skips already-published; OTP=123456 if 2FA)
	@set -euo pipefail; \
	otp=; if [ -n "$(OTP)" ]; then otp="--otp=$(OTP)"; fi; \
	for d in npm/dist/csdd-*/ npm/dist/csdd/; do \
	  name=$$(cd "$$d" && node -p "require('./package.json').name"); \
	  ver=$$(cd "$$d" && node -p "require('./package.json').version"); \
	  if npm view "$$name@$$ver" version >/dev/null 2>&1; then \
	    echo "skip $$name@$$ver (already published)"; continue; fi; \
	  echo ">> publishing $$name@$$ver"; npm publish "$$d" --access public $$otp; \
	done

.PHONY: release
release: ## Tag VERSION and push -> CI builds + publishes (set VERSION=vX.Y.Z)
	@case '$(VERSION)' in v*.*.*) : ;; *) echo "VERSION must look like v1.2.3 (got '$(VERSION)')"; exit 1 ;; esac
	git tag -a '$(VERSION)' -m 'csdd $(VERSION)'
	git push origin '$(VERSION)'

.PHONY: clean
clean: ## Remove build artifacts (dist/, npm/dist/, ./csdd, coverage)
	rm -rf '$(DIST)' npm/dist csdd csdd.exe coverage.out
