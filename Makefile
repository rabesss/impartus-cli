.PHONY: build config-init test run-cli run-api lint clean install pre-commit-install pre-commit quality-gate-install quality-gate quality-gate-scan quality-gate-next docs docs-toc security security-scan security-gitleaks security-gosec security-trivy security-govulncheck

DESLOPPIFY_VERSION ?= 1.0.0
TREE_SITTER_LANGUAGE_PACK_VERSION ?= 1.6.2
DESLOPPIFY_VENV ?= .venv-desloppify
DESLOPPIFY_BIN ?= $(DESLOPPIFY_VENV)/bin/desloppify
QUALITY_MIN_SCORE ?= 80
GO_TOOLCHAIN ?= $(shell awk '/^toolchain / { print $$2 }' go.mod)
CONFIG_FILE ?= config.json
SAMPLE_CONFIG ?= sample.config.json

# Build the impartus binary
build:
	@echo "Building impartus..."
	go build -o impartus .
	@echo "Build complete!"

# Create a private config file, or secure an existing one without overwriting it.
config-init:
	@if [ -e "$(CONFIG_FILE)" ]; then \
		chmod 600 "$(CONFIG_FILE)" && \
		echo "Secured existing $(CONFIG_FILE) (mode 0600)."; \
	else \
		install -m 600 "$(SAMPLE_CONFIG)" "$(CONFIG_FILE)" && \
		echo "Created $(CONFIG_FILE) from $(SAMPLE_CONFIG) (mode 0600)."; \
	fi

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
		GOTOOLCHAIN=$(GO_TOOLCHAIN) golangci-lint run --timeout 5m; \
	elif [ -f "$$(go env GOPATH)/bin/golangci-lint" ]; then \
		GOTOOLCHAIN=$(GO_TOOLCHAIN) $$(go env GOPATH)/bin/golangci-lint run --timeout 5m; \
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
	go build -ldflags "-X github.com/rabesss/impartus-cli/internal/buildinfo.Version=$(shell git describe --tags --always --dirty) -X github.com/rabesss/impartus-cli/internal/buildinfo.Date=$(shell date -u +%Y-%m-%dT%H:%M:%SZ)" -o impartus .
	@echo "Release build complete!"

# Run API with custom port
run-api-port:
	@echo "Starting API server on port $(PORT)..."
	./impartus serve --port $(PORT)

# Test health endpoint
test-health:
	@echo "Testing health endpoint..."
	@curl -s http://localhost:8080/api/v1/health || echo "Server not running. Start with 'make run-api'"

# Install the pinned scanner into the repository-local tool environment.
quality-gate-install:
	python3 -m venv "$(DESLOPPIFY_VENV)"
	"$(DESLOPPIFY_VENV)/bin/python" -m pip install --disable-pip-version-check \
		"desloppify[full]==$(DESLOPPIFY_VERSION)" \
		"tree-sitter-language-pack==$(TREE_SITTER_LANGUAGE_PACK_VERSION)"

# Quality gate: desloppify scan (run after refactors and feature additions)
quality-gate-scan:
	@echo "Running desloppify quality gate scan..."
	@if [ -x "$(DESLOPPIFY_BIN)" ]; then \
		"$(DESLOPPIFY_BIN)" scan --path . --no-badge; \
	else \
		echo "project-local desloppify not found. Install with:"; \
		echo "  make quality-gate-install"; \
		exit 1; \
	fi

# Quality gate: show next prioritized items
quality-gate-next:
	@echo "Showing desloppify next items..."
	@if [ -x "$(DESLOPPIFY_BIN)" ]; then \
		"$(DESLOPPIFY_BIN)" next; \
	else \
		echo "project-local desloppify not found. Install with:"; \
		echo "  make quality-gate-install"; \
		exit 1; \
	fi

# Full quality gate: scan once and enforce the objective/mechanical threshold.
quality-gate:
	@DESLOPPIFY_BIN="$(DESLOPPIFY_BIN)" QUALITY_MIN_SCORE="$(QUALITY_MIN_SCORE)" bash scripts/run-quality-gate.sh

# Generate documentation table of contents using Go-based generator
docs-toc:
	@echo "Generating documentation table of contents..."
	@for file in README.md CONTRIBUTING.md SECURITY.md docs/*.md; do \
		if [ -f "$$file" ]; then \
			echo "Processing: $$file"; \
			go run scripts/generate-toc.go "$$file"; \
		fi \
	done
	@echo "Documentation TOC generation complete!"

# Generate docs table of contents
docs: docs-toc
	@echo "Documentation generation complete!"

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
	@echo "  config-init        - Create or secure config.json with owner-only permissions"
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
	@echo "  quality-gate-install - Install desloppify $(DESLOPPIFY_VERSION) into .venv-desloppify"
	@echo "  quality-gate-scan  - Run desloppify $(DESLOPPIFY_VERSION) quality gate scan"
	@echo "  quality-gate-next  - Show desloppify $(DESLOPPIFY_VERSION) prioritized items"
	@echo "  quality-gate       - Enforce a minimum Desloppify objective score of $(QUALITY_MIN_SCORE)"
	@echo "  docs-toc           - Generate documentation table of contents"
	@echo "  docs               - Run docs-toc"
	@echo "  security-gitleaks  - Run secret scanning with gitleaks"
	@echo "  security-gosec     - Run Go security analysis with gosec"
	@echo "  security-trivy     - Run vulnerability scanning with trivy"
	@echo "  security-govulncheck - Run Go vulnerability check"
	@echo "  security           - Run all security scans"
	@echo "  help               - Show this help message"
