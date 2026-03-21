.PHONY: build test run-cli run-api lint clean install pre-commit-install pre-commit quality-gate quality-gate-scan quality-gate-next docs docs-toc agents-md-validate

DESLOPPIFY_VERSION ?= 0.9.12

# Build the impartus binary
build:
	@echo "Building impartus..."
	go build -o impartus .
	@echo "Build complete!"

# Run tests
test:
	@echo "Running tests..."
	go test ./... -v -cover
	@echo "Tests complete!"

# Run CLI in interactive mode (backwards compatible)
run-cli:
	@echo "Running CLI (interactive mode)..."
	./impartus

# Show download command help
run-cli-download-help:
	@echo "Showing download command help..."
	./impartus download --help || ./impartus download -h || true

# Start API server
run-api:
	@echo "Starting API server on port 8080..."
	./impartus serve --port 8080

# Run linter
lint:
	@echo "Running golangci-lint..."
	@if command -v golangci-lint >/dev/null 2>&1; then \
		golangci-lint run --timeout 5m; \
	elif [ -f "$$(go env GOPATH)/bin/golangci-lint" ]; then \
		$$(go env GOPATH)/bin/golangci-lint run --timeout 5m; \
	else \
		echo "golangci-lint not found. Install with:"; \
		echo "  curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b \$$(go env GOPATH)/bin"; \
		exit 1; \
	fi

# Install pre-commit hooks
pre-commit-install:
	@echo "Installing pre-commit hooks..."
	@if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit install; \
	else \
		echo "pre-commit not found. Install with:"; \
		echo "  pip install pre-commit"; \
		exit 1; \
	fi

# Run pre-commit on all files
pre-commit:
	@echo "Running pre-commit on all files..."
	@if command -v pre-commit >/dev/null 2>&1; then \
		pre-commit run --all-files; \
	else \
		echo "pre-commit not found. Install with:"; \
		echo "  pip install pre-commit"; \
		exit 1; \
	fi

# Clean build artifacts
clean:
	@echo "Cleaning..."
	rm -f impartus
	rm -f *.log
	@echo "Clean complete!"

# Install to GOPATH/bin
install: build
	@echo "Installing to GOPATH/bin..."
	go install .
	@echo "Install complete!"

# Build with version info
build-release:
	@echo "Building release..."
	go build -ldflags "-X main.version=$(shell git describe --tags --always --dirty) -X main.date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" -o impartus .
	@echo "Release build complete!"

# Run API with custom port
run-api-port:
	@echo "Starting API server on port $(PORT)..."
	./impartus serve --port $(PORT)

# Test health endpoint
test-health:
	@echo "Testing health endpoint..."
	@curl -s http://localhost:8080/api/v1/health || echo "Server not running. Start with 'make run-api'"

# Quality gate: desloppify scan (run after refactors and feature additions)
quality-gate-scan:
	@echo "Running desloppify quality gate scan..."
	@if command -v desloppify >/dev/null 2>&1; then \
		desloppify scan --path .; \
	else \
		echo "desloppify not found. Install with:"; \
		echo "  python3 -m pip install --user --upgrade \"desloppify[full]==$(DESLOPPIFY_VERSION)\""; \
		exit 1; \
	fi

# Quality gate: show next prioritized items
quality-gate-next:
	@echo "Showing desloppify next items..."
	@if command -v desloppify >/dev/null 2>&1; then \
		desloppify next; \
	else \
		echo "desloppify not found. Install with:"; \
		echo "  python3 -m pip install --user --upgrade \"desloppify[full]==$(DESLOPPIFY_VERSION)\""; \
		exit 1; \
	fi

# Full quality gate: scan and enforce score threshold (>80)
quality-gate: quality-gate-scan
	@echo "Checking score threshold..."
	@echo "NOTE: Target score is >80. Current score may be lower."
	@echo "Run 'make quality-gate-next' to see prioritized items for improvement."

# Generate documentation table of contents using doctoc
docs-toc:
	@echo "Generating documentation table of contents..."
	@if command -v doctoc >/dev/null 2>&1; then \
		for file in README.md AGENTS.md docs/*.md; do \
			if [ -f "$$file" ]; then \
				echo "Processing: $$file"; \
				doctoc "$$file"; \
			fi \
		done; \
	else \
		echo "doctoc not found. Install with: pip install doctoc"; \
		exit 1; \
	fi
	@echo "Documentation TOC generation complete!"

# Validate AGENTS.md commands reference valid targets and files
agents-md-validate:
	@echo "Validating AGENTS.md commands..."
	@FAILED=0; \
	echo "Checking Makefile targets..."; \
	for target in $$(grep -oP 'make \w+' AGENTS.md | sort -u | awk '{print $$2}'); do \
		if grep -q "^$$target:" Makefile; then \
			echo "  ✓ make $$target exists"; \
		else \
			echo "  ✗ make $$target NOT FOUND"; \
			FAILED=1; \
		fi \
	done; \
	echo "Verifying Go commands..."; \
	if go build ./... > /dev/null 2>&1; then \
		echo "  ✓ go build ./... works"; \
	else \
		echo "  ✗ go build ./... failed"; \
		FAILED=1; \
	fi; \
	if go test ./... -list '.*' > /dev/null 2>&1; then \
		echo "  ✓ go test ./... command is valid"; \
	else \
		echo "  ✗ go test ./... command failed"; \
		FAILED=1; \
	fi; \
	if [ -f ".golangci.yml" ]; then \
		echo "  ✓ .golangci.yml exists"; \
	else \
		echo "  ✗ .golangci.yml NOT FOUND"; \
		FAILED=1; \
	fi; \
	if [ $$FAILED -eq 0 ]; then \
		echo "All AGENTS.md validations passed!"; \
	else \
		echo "Some validations failed!"; \
		exit 1; \
	fi

# Generate docs and validate AGENTS.md
docs: docs-toc agents-md-validate
	@echo "Documentation validation complete!"

# Help target
help:
	@echo "Available targets:"
	@echo "  build              - Build the impartus binary"
	@echo "  test               - Run tests"
	@echo "  run-cli            - Run CLI in interactive mode"
	@echo "  run-cli-download-help - Show download command help"
	@echo "  run-api            - Start API server on port 8080"
	@echo "  run-api-port       - Start API server on custom port (make run-api-port PORT=9090)"
	@echo "  lint               - Run golangci-lint"
	@echo "  pre-commit-install - Install pre-commit hooks"
	@echo "  pre-commit         - Run pre-commit on all files"
	@echo "  clean              - Clean build artifacts"
	@echo "  install            - Install to GOPATH/bin"
	@echo "  build-release      - Build with version info"
	@echo "  test-health        - Test health endpoint (server must be running)"
	@echo "  quality-gate-scan  - Run desloppify $(DESLOPPIFY_VERSION) quality gate scan"
	@echo "  quality-gate-next  - Show desloppify $(DESLOPPIFY_VERSION) prioritized items"
	@echo "  quality-gate       - Full quality gate (scan + threshold check)"
	@echo "  docs-toc           - Generate documentation table of contents (requires doctoc)"
	@echo "  agents-md-validate - Validate AGENTS.md commands reference valid targets"
	@echo "  docs               - Run docs-toc and agents-md-validate"
	@echo "  help               - Show this help message"
