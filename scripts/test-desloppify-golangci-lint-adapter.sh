#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
adapter="$script_dir/desloppify-golangci-lint-adapter.sh"
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

real_linter="$temp_dir/real-golangci-lint"
args_file="$temp_dir/args"
cat >"$real_linter" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$@" >"$FAKE_ARGS_FILE"
exit "${FAKE_EXIT:-0}"
FAKE
chmod +x "$real_linter"

assert_args() {
	local name=$1
	shift
	local expected
	expected=$(printf '%s\n' "$@")
	if [[ $(<"$args_file") != "$expected" ]]; then
		printf 'FAIL: %s\nexpected:\n%s\nactual:\n%s\n' \
			"$name" "$expected" "$(<"$args_file")" >&2
		exit 1
	fi
	printf 'PASS: %s\n' "$name"
}

FAKE_ARGS_FILE="$args_file" \
	DESLOPPIFY_GOLANGCI_LINT_REAL="$real_linter" \
	"$adapter" run --timeout 5m ./...
assert_args "forwards unchanged arguments" run --timeout 5m ./...

FAKE_ARGS_FILE="$args_file" \
	DESLOPPIFY_GOLANGCI_LINT_REAL="$real_linter" \
	"$adapter" run --out-format=json --timeout=5m ./...
assert_args "translates Desloppify v1 JSON output flag" \
	run \
	--output.json.path stdout \
	--output.text.path /dev/null \
	--show-stats=false \
	--timeout=5m ./...

set +e
missing_output=$(DESLOPPIFY_GOLANGCI_LINT_REAL='' "$adapter" run 2>&1)
missing_status=$?
set -e
if (( missing_status == 0 )) || [[ "$missing_output" != *"DESLOPPIFY_GOLANGCI_LINT_REAL is required"* ]]; then
	printf 'FAIL: missing real linter fails closed\n%s\n' "$missing_output" >&2
	exit 1
fi
printf 'PASS: missing real linter fails closed\n'

set +e
FAKE_ARGS_FILE="$args_file" \
	FAKE_EXIT=23 \
	DESLOPPIFY_GOLANGCI_LINT_REAL="$real_linter" \
	"$adapter" run --out-format=json >/dev/null 2>&1
real_status=$?
set -e
if (( real_status != 23 )); then
	printf 'FAIL: propagates real linter failure (got %s)\n' "$real_status" >&2
	exit 1
fi
printf 'PASS: propagates real linter failure\n'

printf 'All 4 golangci-lint adapter cases passed.\n'
