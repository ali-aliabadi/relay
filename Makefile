# Relay's single entry point for build, lint and test. CI runs the same targets.

SHELL := /usr/bin/env bash
.SHELLFLAGS := -eu -o pipefail -c
.DEFAULT_GOAL := help

BIN       := bin/relay
TOOLS     := $(CURDIR)/bin/tools
IMAGE     ?= relay:dev
VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS   := -s -w -X main.version=$(VERSION)

# Pinned tool versions. Bump deliberately; `make tools` installs them into bin/tools.
GOLANGCI_LINT_VERSION := v2.14.0
GOFUMPT_VERSION       := v0.12.0
GOIMPORTS_VERSION     := v0.51.0
GOVULNCHECK_VERSION   := v1.8.0
ACTIONLINT_VERSION    := v1.7.12
GITLEAKS_VERSION      := v8.30.1
HADOLINT_VERSION      := v2.15.1
HADOLINT_SHA256       := c7187db94eeeeca956519a6af171adc31453941a1e777961f6e680f697c8c507

# Build tools with the repo's toolchain so golangci-lint understands go.mod's Go version.
GO_TOOLCHAIN := $(shell go env GOVERSION)

export PATH := $(TOOLS):$(PATH)

.PHONY: help
help: ## list targets
	@grep -hE '^[a-z-]+:.*## ' $(MAKEFILE_LIST) | awk -F':.*## ' '{printf "  %-10s %s\n", $$1, $$2}'

.PHONY: tools
tools: ## install pinned dev tools into bin/tools
	GOBIN=$(TOOLS) GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	GOBIN=$(TOOLS) GOTOOLCHAIN=$(GO_TOOLCHAIN) go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	GOBIN=$(TOOLS) GOTOOLCHAIN=$(GO_TOOLCHAIN) go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	GOBIN=$(TOOLS) GOTOOLCHAIN=$(GO_TOOLCHAIN) go install golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION)
	GOBIN=$(TOOLS) GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/rhysd/actionlint/cmd/actionlint@$(ACTIONLINT_VERSION)
	GOBIN=$(TOOLS) GOTOOLCHAIN=$(GO_TOOLCHAIN) go install github.com/zricethezav/gitleaks/v8@$(GITLEAKS_VERSION)
	curl -fsSL -o $(TOOLS)/hadolint \
		https://github.com/hadolint/hadolint/releases/download/$(HADOLINT_VERSION)/hadolint-linux-x86_64
	echo "$(HADOLINT_SHA256)  $(TOOLS)/hadolint" | sha256sum -c -
	chmod +x $(TOOLS)/hadolint

.PHONY: run
run: ## run the server locally with ./data/relay.db (reads .env if present)
	mkdir -p data
	set -a; [ -f .env ] && . ./.env; set +a; \
		RELAY_DB_PATH=$${RELAY_DB_PATH:-./data/relay.db} go run ./cmd/relay serve

.PHONY: fmt
fmt: ## format code with gofumpt and goimports
	golangci-lint fmt ./...

.PHONY: fmt-check
fmt-check: ## fail if any file needs formatting
	golangci-lint fmt --diff ./...

.PHONY: lint
lint: ## golangci-lint, file length, go mod tidy, actionlint, hadolint
	golangci-lint run ./...
	scripts/check-file-length.sh
	go mod tidy -diff
	actionlint
	hadolint Dockerfile

.PHONY: test
test: ## unit and integration tests with -race and coverage
	CGO_ENABLED=1 go test -race -covermode=atomic -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

.PHONY: test-e2e
test-e2e: docker ## end-to-end tests against the built image (needs Docker)
	RELAY_E2E_IMAGE=$(IMAGE) go test -tags e2e -count=1 -timeout 5m ./test/e2e/...

.PHONY: sec
sec: ## govulncheck and gitleaks (trivy on the image when installed)
	govulncheck ./...
	gitleaks git --redact --no-banner .
	if command -v trivy >/dev/null; then \
		trivy image --exit-code 1 --severity HIGH,CRITICAL --ignore-unfixed $(IMAGE); \
	else echo "trivy not installed; image scan runs in CI"; fi

.PHONY: check
check: fmt-check lint test sec ## everything to run before a commit

.PHONY: generate
generate: ## sqlc generate (lands with the storage phase)
	@if [ -f sqlc.yaml ]; then sqlc generate; else echo "no sqlc.yaml yet"; fi

.PHONY: build
build: ## static binary in ./bin/relay
	CGO_ENABLED=0 go build -trimpath -ldflags '$(LDFLAGS)' -o $(BIN) ./cmd/relay

.PHONY: docker
docker: ## build the image locally as $(IMAGE)
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .
