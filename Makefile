# gitlab-enhanced Makefile
# Common developer tasks. Run `make help` for a summary.

BINARY      := gitlab-enhanced
GO          := go
MODULE      := gitlab.com/openos-project/git-management_deving/gitlab-enhanced
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DATE        ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w \
  -X $(MODULE)/version.Version=$(VERSION) \
  -X $(MODULE)/version.Commit=$(COMMIT) \
  -X $(MODULE)/version.Date=$(DATE)
BUILD_DIR   := bin
PACKAGE_DIR := dist

.PHONY: help build test lint fmt vet clean package release dev-setup install

## help: print this help message
help:
	@echo "Usage: make <target>"
	@echo ""
	@sed -n 's/^## //p' $(MAKEFILE_LIST) | column -t -s ':' | sed -e 's/^/  /'

## build: compile the binary to bin/gitlab-enhanced
build:
	@mkdir -p $(BUILD_DIR)
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BINARY) ./cmd/gitlab-enhanced/
	@echo "Built $(BUILD_DIR)/$(BINARY) ($(VERSION))"

## test: run all Go tests (cache disabled to prevent stale results)
test:
	$(GO) test -count=1 -timeout 60s ./...

## test-race: run tests with the race detector
test-race:
	$(GO) test -race -count=1 -timeout 120s ./...

## test-integration: run integration tests only
test-integration:
	$(GO) test -count=1 -timeout 60s -run 'TestIntegration|TestBWIntegration' ./...

## lint: run all linters (go vet, shellcheck, yamllint)
lint: vet
	@echo "--- shellcheck ---"
	@find . -name "*.sh" -not -path "./.git/*" | xargs shellcheck --severity=warning || true
	@echo "--- yamllint ---"
	@find . -name "*.yaml" -o -name "*.yml" | grep -v ".git\|node_modules" | xargs yamllint -c .yamllint.yaml || true

## vet: run go vet
vet:
	$(GO) vet ./...

## fmt: format Go source files
fmt:
	$(GO) fmt ./...
	@which goimports > /dev/null 2>&1 && goimports -w . || true

## clean: remove build artifacts
clean:
	rm -rf $(BUILD_DIR) $(PACKAGE_DIR)

## package: build .deb and .rpm packages (requires nfpm)
package: build
	@which nfpm > /dev/null 2>&1 || (echo "nfpm not found: https://nfpm.goreleaser.com/install/" && exit 1)
	@mkdir -p $(PACKAGE_DIR)
	VERSION=$(VERSION) nfpm package \
		--config packaging/config/nfpm.yaml \
		--packager deb \
		--target $(PACKAGE_DIR)/
	VERSION=$(VERSION) nfpm package \
		--config packaging/config/nfpm.yaml \
		--packager rpm \
		--target $(PACKAGE_DIR)/
	@echo "Packages written to $(PACKAGE_DIR)/"

## release: tag and push a release (maintainers only — requires VERSION=x.y.z)
release:
	@[ -n "$(VERSION)" ] || (echo "Usage: make release VERSION=x.y.z" && exit 1)
	@bash scripts/release.sh $(VERSION)

## dev-setup: install pre-commit hooks and verify tooling
dev-setup:
	@which pre-commit > /dev/null 2>&1 || (echo "pre-commit not found: pip install pre-commit" && exit 1)
	pre-commit install
	@echo "Checking Go version..."
	@$(GO) version
	@echo "Checking shellcheck..."
	@shellcheck --version | head -1 || echo "WARNING: shellcheck not found"
	@echo "Checking yamllint..."
	@yamllint --version || echo "WARNING: yamllint not found"
	@echo "Dev setup complete."

## install: install the binary to /usr/local/bin
install: build
	install -m 0755 $(BUILD_DIR)/$(BINARY) /usr/local/bin/$(BINARY)
	@echo "Installed /usr/local/bin/$(BINARY)"
