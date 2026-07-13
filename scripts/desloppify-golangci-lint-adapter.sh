#!/usr/bin/env bash

set -euo pipefail

real_linter="${DESLOPPIFY_GOLANGCI_LINT_REAL:-}"
if [[ -z "$real_linter" ]]; then
	printf 'desloppify golangci-lint adapter: DESLOPPIFY_GOLANGCI_LINT_REAL is required\n' >&2
	exit 1
fi
if [[ ! -x "$real_linter" ]]; then
	printf 'desloppify golangci-lint adapter: real executable not found: %s\n' "$real_linter" >&2
	exit 1
fi

args=()
for arg in "$@"; do
	if [[ "$arg" == "--out-format=json" ]]; then
		args+=(
			"--output.json.path" "stdout"
			"--output.text.path" "/dev/null"
			"--show-stats=false"
		)
	else
		args+=("$arg")
	fi
done

exec "$real_linter" "${args[@]}"
