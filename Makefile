.PHONY: build test run-cli run-api lint clean install pre-commit-install pre-commit quality-gate quality-gate-scan quality-gate-next docs docs-toc agents-md-validate security security-scan security-gitleaks security-gosec security-trivy security-govulncheck

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
	go build -ldflags "-X github.com/rabesss/impartus-cli/internal/buildinfo.Version=$(shell git describe --tags --always --dirty) -X github.com/rabesss/impartus-cli/internal/buildinfo.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) -X github.com/rabesss/impartus-cli/internal/buildinfo.Commit=$(shell git rev-parse HEAD)" -o impartus .
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

# Generate documentation table of contents using Go-based generator
docs-toc:
	@echo "Generating documentation table of contents..."
	@for file in README.md AGENTS.md docs/*.md; do \
		if [ -f "$$file" ]; then \
			echo "Processing: $$file"; \
			go run scripts/generate-toc.go "$$file"; \
		fi \
	done
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

# Security scanning targets
security-gitleaks:
	@echo "Running secret scanning..."
	@if command -v gitleaks >/dev/null 2>&1; then \
		gitleaks detect --config-path=.gitleaks.toml --verbose --redact; \
	elif docker info > /dev/null 2>&1; then \
		docker run --rm -v "$$(pwd):/data" zricethezav/gitleaks:latest detect --config /data/.gitleaks.toml --verbose --redact; \
	else \
		echo "gitleaks not found. Install from: https://github.com/gitleaks/gitleaks/releases"; \
		exit 1; \
	fi
	@echo "Secret scanning complete!"

security-gosec:
	@echo "Running Go security analysis..."
	@if command -v gosec >/dev/null 2>&1; then \
		gosec ./...; \
	else \
		echo "Installing gosec..."; \
		go install github.com/securego/gosec/v2/cmd/gosec@latest; \
		gosec ./...; \
	fi
	@echo "Go security analysis complete!"

security-trivy:
	@echo "Running Trivy vulnerability scanner..."
	@if command -v trivy >/dev/null 2>&1; then \
		trivy fs --scanners vuln,secret,misconfig --severity CRITICAL,HIGH,MEDIUM .; \
	elif docker info > /dev/null 2>&1; then \
		docker run --rm -v "$$(pwd):/workspace" aquasec/trivy:latest fs --scanners vuln,secret,misconfig --severity CRITICAL,HIGH,MEDIUM /workspace; \
	else \
		echo "trivy not found. Install from: https://aquasecurity.github.io/trivy/latest/getting-started/installation/"; \
		exit 1; \
	fi
	@echo "Trivy scanning complete!"

security-govulncheck:
	@echo "Running Go vulnerability check..."
	@go install golang.org/x/vuln/cmd/govulncheck@latest
	@govulncheck ./...
	@echo "Go vulnerability check complete!"

# Run all security scans
security: security-gitleaks security-gosec security-trivy security-govulncheck
	@echo "All security scans complete!"

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
	@echo "  docs-toc           - Generate documentation table of contents"
	@echo "  agents-md-validate - Validate AGENTS.md commands reference valid targets"
	@echo "  docs               - Run docs-toc and agents-md-validate"
	@echo "  security-gitleaks  - Run secret scanning with gitleaks"
	@echo "  security-gosec     - Run Go security analysis with gosec"
	@echo "  security-trivy     - Run vulnerability scanning with trivy"
	@echo "  security-govulncheck - Run Go vulnerability check"
	@echo "  security           - Run all security scans"
	@echo "  help               - Show this help message"
