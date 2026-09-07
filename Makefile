BIN_DIR ?= $(HOME)/.local/bin
SHARE_DIR ?= $(HOME)/.local/share/azform
PKG     := ./...

.PHONY: help build install widget-install test test-race cover lint lint-fix fmt vet tidy clean

help: ## Show available targets
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-14s\033[0m %s\n", $$1, $$2}'

build: ## Build azform binary into ./bin
	@mkdir -p bin
	go build -o bin/azform ./cmd/azform

# Depends on install-bin, NOT build: `build` only writes ./bin/azform,
# a working-tree artifact nobody runs from. An earlier version of this
# target was `install: build widget-install`, which silently left the
# binary in ~/.local/bin frozen at an old commit while every test
# passed. Do not "simplify" it back.
install: install-bin widget-install ## Install binary and refresh widget

install-bin: build ## go install into $(BIN_DIR) (depends on build for the local ./bin/azform copy)
	GOBIN=$(BIN_DIR) go install ./cmd/azform

# Emits the widgets from the binary rather than copying widget/*, the
# same path install.sh takes. Copying would work here (the repo is
# right there) but would leave this target as the one place where a
# widget can be newer than the binary that reads it.
widget-install: build ## Emit widgets from the built binary into $(SHARE_DIR)
	@mkdir -p $(SHARE_DIR)
	./bin/azform shell-init zsh > $(SHARE_DIR)/widget.zsh
	./bin/azform shell-init bash > $(SHARE_DIR)/widget.bash
	@echo "widgets installed in $(SHARE_DIR) — restart shell or: exec $$SHELL"

test: ## Run tests
	go test $(PKG)

test-race: ## Run tests with race detector
	go test -race $(PKG)

cover: ## Run tests with coverage report
	go test -cover $(PKG)

lint: ## Run golangci-lint
	golangci-lint run $(PKG)

lint-fix: ## Run golangci-lint with --fix
	golangci-lint run --fix $(PKG)

fmt: ## Format code with goimports
	goimports -w -local github.com/someson/azform $(shell find . -name '*.go' -not -path './.*/*')

vet: ## Run go vet
	go vet $(PKG)

tidy: ## Tidy go.mod
	go mod tidy

clean: ## Remove build artifacts
	rm -rf bin
