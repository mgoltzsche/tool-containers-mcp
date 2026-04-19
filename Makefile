GORELEASER_VERSION ?= v2.14.3
GOLANGCI_LINT_VERSION ?= v2.7.2

DIST_DIR := build/dist
BIN_DIR := build/bin
GOLANGCI_LINT = $(BIN_DIR)/golangci-lint
GORELEASER = $(BIN_DIR)/goreleaser


.PHONY: default
default: help

.PHONY: tool-containers-mcp
tool-containers-mcp: ## Build the binary.
	CGO_ENABLED=0 go build -o "$(DIST_DIR)/tool-containers-mcp" -ldflags '-s -w -extldflags "-static"' ./cmd/tool-containers-mcp

.PHONY: tool-containers-mcp
container: tool-containers-mcp ## Build the container image.
	docker build --force-rm --build-arg=TARGETPLATFORM=./build/dist -t ghcr.io/mgoltzsche/tool-containers-mcp:dev .

compose: container ## Run the compose stack.
	docker compose up --force-recreate --remove-orphans

compose-test-request: ## Run an inference test request.
	curl -fsS http://localhost:9000/v1/chat/completions -H "Content-Type: application/json" -d '{"model": "qwen3-4b", "messages": [{"role": "user", "content": "Which tools do you have access to?"}]}' | jq .

.PHONY: test
test: ## Run the tests.
	go test -timeout 3m -cover ./...

.PHONY: lint
lint: $(GOLANGCI_LINT) ## Run the linters.
	$(GOLANGCI_LINT) run ./...

.PHONY: fmt
fmt: ## Auto-format the Go code.
	go fmt ./...

snapshot: $(GORELEASER) ## Builds a snapshot release without publish it.
	$(GORELEASER) release --snapshot --clean --fail-fast

.PHONY: clean
clean: ## Remove local build artifacts.
	rm -rf build

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

$(GOLANGCI_LINT):
	$(call go-get-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION))

$(GORELEASER):
	$(call go-get-tool,$(GORELEASER),github.com/goreleaser/goreleaser/v2@$(GORELEASER_VERSION))

# go-get-tool will 'go get' any package $2 and install it to $1.
PROJECT_DIR := $(shell dirname $(abspath $(lastword $(MAKEFILE_LIST))))
define go-get-tool
@[ -f $(1) ] || { \
set -e ;\
TMP_DIR=$$(mktemp -d) ;\
cd $$TMP_DIR ;\
go mod init tmp ;\
echo "Downloading $(2)" ;\
GOBIN=$(PROJECT_DIR)/build/bin go install $(2) ;\
rm -rf $$TMP_DIR ;\
}
endef
