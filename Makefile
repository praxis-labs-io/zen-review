.PHONY: help test build lint lint-fix fmt fmt-fix mod-tidy all clean coverage install golden

.DEFAULT_GOAL := help

BINARY_NAME := zen-review
MAIN_PACKAGE := ./cmd/zen-review
COVERAGE_FILE := coverage.out
INSTALL_DIR := $(HOME)/.local/bin

help: ## Show this help message
	@echo 'Usage: make [target]'
	@echo ''
	@echo 'Available targets:'
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run tests with race detection and coverage
	@echo "Running tests..."
	go test -race -coverprofile=$(COVERAGE_FILE) ./...

golden: ## Regenerate the golden files
	@echo "Regenerating golden files..."
	go test ./internal/diff/ ./internal/cli/ ./internal/tui/app/ ./internal/tui/paint/ -update

coverage: test ## Show test coverage report
	@echo "Generating coverage report..."
	go tool cover -func=$(COVERAGE_FILE)

build: ## Build the binary
	@echo "Building $(BINARY_NAME)..."
	go build -v $(MAIN_PACKAGE)

install: ## Build into ~/.local/bin so the installed binary matches this tree
	@echo "Installing $(BINARY_NAME) to $(INSTALL_DIR)..."
	go build -o $(INSTALL_DIR)/$(BINARY_NAME) $(MAIN_PACKAGE)

fmt: ## Check code formatting with gofmt
	@echo "Checking code formatting..."
	@if [ -n "$$(gofmt -l .)" ]; then \
		echo "The following files are not formatted correctly:"; \
		gofmt -l .; \
		exit 1; \
	fi
	@echo "All files are properly formatted."

fmt-fix: ## Fix code formatting with gofmt
	@echo "Fixing code formatting..."
	gofmt -w .
	@echo "Formatting complete."

mod-tidy: ## Check if go.mod and go.sum are tidy
	@echo "Checking go.mod and go.sum..."
	@go mod tidy -diff
	@echo "go.mod and go.sum are tidy."

lint: fmt mod-tidy ## Run all linting checks (gofmt, mod-tidy, golangci-lint)
	@echo "Running golangci-lint..."
	@golangci-lint run
	@echo "All linting checks passed."

lint-fix: fmt-fix ## Fix linting issues automatically
	@echo "Fixing linting issues..."
	@golangci-lint run --fix
	@echo "Linting fixes complete."

all: lint test build ## Run all checks (lint, test, build)
	@echo "All checks completed successfully!"

clean: ## Clean build artifacts
	@echo "Cleaning build artifacts..."
	@rm -f $(BINARY_NAME) $(COVERAGE_FILE)
	@echo "Clean complete."
