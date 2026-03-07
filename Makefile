.PHONY: help build test integration-test cross-compile lint fmt format fmt-check gofix gofix-check deps deps-check ci

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*## "}; {printf "  %-18s %s\n", $$1, $$2}'

build: ## Build all packages
	go build ./...

test: ## Run unit tests (use TEST_FLAGS to customize, e.g. TEST_FLAGS="-run TestFoo")
	go test ./... -count=1 -short $(TEST_FLAGS)

integration-test: ## Run integration tests (requires podman; may need sudo)
	go test ./... -count=1 -run Integration -timeout 300s $(TEST_FLAGS)

CROSS_PLATFORMS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64 windows/arm64

cross-compile: ## Verify tests compile on all supported OS/arch pairs
	@for platform in $(CROSS_PLATFORMS); do \
		GOOS=$${platform%/*} GOARCH=$${platform#*/} go test -short -exec=/usr/bin/true ./... || exit 1; \
		echo "  ok  $$platform"; \
	done

lint: ## Run go vet
	go vet ./...

fmt: ## Format code
	gofmt -w .

format: fmt ## Alias for fmt

fmt-check: ## Check formatting (fails if not formatted)
	@test -z "$$(gofmt -l .)" || { gofmt -l .; echo "Files above are not formatted. Run 'make fmt' to fix."; exit 1; }

gofix: ## Apply go fix modernizations
	go fix ./...

gofix-check: ## Check for available go fix modernizations (fails if any)
	@test -z "$$(go fix -diff ./... 2>&1)" || { go fix -diff ./... 2>&1 | sed -n 's|^--- \(.*\) (old)$$|\1|p'; echo "Files above need fixes. Run 'make gofix' to fix."; exit 1; }

deps: ## Fetch dependencies and tidy go.mod
	go mod download
	go mod tidy

deps-check: ## Check that go.mod is tidy (fails if not)
	@go mod tidy -diff || { echo "go.mod/go.sum are not tidy. Run 'make deps' to fix."; exit 1; }

ci: ## Run all checks (CI mode)
	@$(MAKE) --no-print-directory deps-check
	@$(MAKE) --no-print-directory fmt-check
	@$(MAKE) --no-print-directory gofix-check
	@$(MAKE) --no-print-directory lint
	@$(MAKE) --no-print-directory build
	@$(MAKE) --no-print-directory test
