#!/usr/bin/env bash

set -euo pipefail

script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
repo_root=$(cd -- "$script_dir/.." && pwd)
gate="$script_dir/run-quality-gate.sh"
temp_dir=$(mktemp -d)
trap 'rm -rf "$temp_dir"' EXIT

if git -C "$repo_root" rev-parse --is-inside-work-tree >/dev/null 2>&1; then
	for script in \
		scripts/desloppify-golangci-lint-adapter.sh \
		scripts/test-desloppify-golangci-lint-adapter.sh \
		scripts/run-quality-gate.sh \
		scripts/test-quality-gate.sh; do
		if git -C "$repo_root" check-ignore --no-index -q "$script"; then
			printf 'FAIL: quality tooling script is ignored: %s\n' "$script" >&2
			exit 1
		fi
	done
	printf 'PASS: all quality tooling scripts are allowlisted\n'
fi

fake_desloppify="$temp_dir/desloppify"
fake_real_linter="$temp_dir/real-golangci-lint"
args_file="$temp_dir/args"
state_file="$temp_dir/state.json"
python_bin=$(command -v python3)

cat >"$fake_desloppify" <<'FAKE'
#!/usr/bin/env bash
printf '%s\n' "$*" >"$FAKE_ARGS_FILE"
if [[ "${FAKE_INVOKE_LINTER:-0}" == 1 ]]; then
	golangci-lint run --out-format=json --timeout=5m ./...
fi
printf '%s\n' "$FAKE_OUTPUT"
exit "${FAKE_EXIT:-0}"
FAKE
chmod +x "$fake_desloppify"

cat >"$fake_real_linter" <<'FAKE_LINTER'
#!/usr/bin/env bash
if [[ -n "${FAKE_LINTER_ARGS_FILE:-}" ]]; then
	printf '%s\n' "$@" >"$FAKE_LINTER_ARGS_FILE"
fi
exit 0
FAKE_LINTER
chmod +x "$fake_real_linter"

case_count=0

run_case() {
	local name=$1
	local expected_status=$2
	local scanner_exit=$3
	local scanner_output=$4
	local expected_text=${5:-}
	local state_mode=${6:-full}
	local output status

	case_count=$((case_count + 1))
	: >"$args_file"
	case "$state_mode" in
		full)
			printf '{"scan_completeness":{"go":"full"}}\n' >"$state_file"
			;;
		reduced)
			printf '{"scan_completeness":{"go":"reduced"}}\n' >"$state_file"
			;;
		missing)
			rm -f "$state_file"
			;;
		malformed)
			printf '{not-json\n' >"$state_file"
			;;
		list)
			printf '[]\n' >"$state_file"
			;;
		scalar)
			printf '42\n' >"$state_file"
			;;
		missing_key)
			printf '{"scan_completeness":{}}\n' >"$state_file"
			;;
		*)
			printf 'FAIL: unknown state mode: %s\n' "$state_mode" >&2
			exit 1
			;;
	esac

	set +e
	output=$(
		FAKE_ARGS_FILE="$args_file" \
			FAKE_EXIT="$scanner_exit" \
			FAKE_OUTPUT="$scanner_output" \
			DESLOPPIFY_BIN="$fake_desloppify" \
			DESLOPPIFY_PYTHON="$python_bin" \
			DESLOPPIFY_STATE_PATH="$state_file" \
			DESLOPPIFY_GOLANGCI_LINT_REAL="$fake_real_linter" \
			QUALITY_MIN_SCORE=80 \
			bash "$gate" 2>&1
	)
	status=$?
	set -e

	if [[ "$expected_status" == pass && $status -ne 0 ]]; then
		printf 'FAIL: %s (expected pass, got exit %s)\n%s\n' "$name" "$status" "$output" >&2
		exit 1
	fi
	if [[ "$expected_status" == fail && $status -eq 0 ]]; then
		printf 'FAIL: %s (expected failure)\n%s\n' "$name" "$output" >&2
		exit 1
	fi
	if [[ -n "$expected_text" && "$output" != *"$expected_text"* ]]; then
		printf 'FAIL: %s (missing output: %s)\n%s\n' "$name" "$expected_text" "$output" >&2
		exit 1
	fi
	if [[ $(<"$args_file") != "scan --path . --state $state_file --no-badge" ]]; then
		printf 'FAIL: %s (unexpected scanner arguments: %s)\n' "$name" "$(<"$args_file")" >&2
		exit 1
	fi

	printf 'PASS: %s\n' "$name"
}

run_case "objective boundary passes" pass 0 "Scores: overall 22.5/100  objective 80/100  strict 22.5/100  verified 80/100" "objective score: 80 (minimum: 80)"
run_case "full completeness passes" pass 0 "Scores: overall 22.5/100  objective 80/100  strict 22.5/100  verified 80/100" "Go scan completeness: full"
run_case "decimal objective above threshold passes" pass 0 "Scores: overall 21.0/100  objective 80.1/100  strict 20.0/100  verified 79.0/100"
run_case "objective below threshold fails" fail 0 "Scores: overall 90/100  objective 79/100  strict 90/100  verified 79/100" "objective score 79 is below minimum"
run_case "Desloppify 1.0 score deltas parse" pass 0 "Scores: overall 22.5/100 (+22.5)  objective 90.0/100 (+90.0)  strict 22.5/100 (+22.5)  verified 90.0/100 (+90.0)" "strict score: 22.5"
run_case "overall does not control the gate" pass 0 "Scores: overall 0/100  objective 90/100  strict 0/100  verified 90/100" "overall score: 0"
run_case "missing objective fails closed" fail 0 "Scores: overall 90/100  strict 90/100  verified 90/100" "no valid Desloppify objective score"
run_case "malformed objective fails closed" fail 0 "Scores: overall 90/100  objective pending/100  strict 90/100  verified 90/100" "no valid Desloppify objective score"
run_case "out-of-range objective fails closed" fail 0 "Scores: overall 90/100  objective 101/100  strict 90/100  verified 90/100" "no valid Desloppify objective score"
run_case "missing summary with successful scanner fails" fail 0 "Scan complete without scores" "no valid Desloppify objective score"
run_case "missing summary with failed scanner fails" fail 7 "Scanner failed before scoring" "scanner exit: 7"
run_case "optional presentation scores report unavailable" pass 0 "Scores: objective 81.5/100  verified 81.5/100" "strict score: unavailable"
run_case "passing objective controls nonzero scanner exit" pass 9 "Scores: overall 20/100  objective 81.5/100  strict 20/100  verified 81.5/100" "scanner exited with 9"
run_case "reduced completeness fails" fail 9 "Scores: overall 20/100  objective 90/100  strict 20/100  verified 90/100" "expected 'full'" reduced
run_case "missing state fails" fail 0 "Scores: overall 20/100  objective 90/100  strict 20/100  verified 90/100" "state file is missing" missing
run_case "malformed state fails" fail 0 "Scores: overall 20/100  objective 90/100  strict 20/100  verified 90/100" "state file is malformed" malformed
run_case "list state fails cleanly" fail 0 "Scores: overall 20/100  objective 90/100  strict 20/100  verified 90/100" "must be a JSON object at the top level" list
run_case "scalar state fails cleanly" fail 0 "Scores: overall 20/100  objective 90/100  strict 20/100  verified 90/100" "must be a JSON object at the top level" scalar
run_case "missing completeness key fails" fail 0 "Scores: overall 20/100  objective 90/100  strict 20/100  verified 90/100" "missing string scan_completeness.go" missing_key

relative_adapter_dir="$temp_dir/relative adapter"
relative_adapter="$relative_adapter_dir/adapter link.sh"
linter_args_file="$temp_dir/relative-adapter-linter-args"
mkdir -p "$relative_adapter_dir"
ln -s "$script_dir/desloppify-golangci-lint-adapter.sh" "$relative_adapter"
printf '{"scan_completeness":{"go":"full"}}\n' >"$state_file"
: >"$args_file"
: >"$linter_args_file"
set +e
relative_output=$(
	cd -- "$temp_dir"
	FAKE_ARGS_FILE="$args_file" \
		FAKE_EXIT=0 \
		FAKE_INVOKE_LINTER=1 \
		FAKE_LINTER_ARGS_FILE="$linter_args_file" \
		FAKE_OUTPUT="Scores: overall 22.5/100  objective 90/100  strict 22.5/100  verified 90/100" \
		DESLOPPIFY_BIN="$fake_desloppify" \
		DESLOPPIFY_PYTHON="$python_bin" \
		DESLOPPIFY_STATE_PATH="$state_file" \
		DESLOPPIFY_GOLANGCI_LINT_ADAPTER="relative adapter/adapter link.sh" \
		DESLOPPIFY_GOLANGCI_LINT_REAL="$fake_real_linter" \
		QUALITY_MIN_SCORE=80 \
		bash "$gate" 2>&1
)
relative_status=$?
set -e
expected_linter_args=$(printf '%s\n' \
	run \
	--output.json.path stdout \
	--output.text.path /dev/null \
	--show-stats=false \
	--timeout=5m ./...)
if (( relative_status != 0 )) || \
	[[ "$relative_output" != *"Quality gate passed"* ]] || \
	[[ $(<"$linter_args_file") != "$expected_linter_args" ]]; then
	printf 'FAIL: relative adapter override runs through canonical symlink\n%s\n' "$relative_output" >&2
	exit 1
fi
case_count=$((case_count + 1))
printf 'PASS: relative adapter override runs through canonical symlink\n'

set +e
invalid_minimum_output=$(
	FAKE_ARGS_FILE="$args_file" \
		FAKE_EXIT=0 \
		FAKE_OUTPUT="Scores: overall 22.5/100  objective 90/100  strict 22.5/100  verified 90/100" \
		DESLOPPIFY_BIN="$fake_desloppify" \
		QUALITY_MIN_SCORE=101 \
		bash "$gate" 2>&1
)
invalid_minimum_status=$?
set -e
if (( invalid_minimum_status == 0 )) || [[ "$invalid_minimum_output" != *"invalid minimum score"* ]]; then
	printf 'FAIL: invalid minimum fails closed\n%s\n' "$invalid_minimum_output" >&2
	exit 1
fi
case_count=$((case_count + 1))
printf 'PASS: invalid minimum fails closed\n'

printf 'All %s quality-gate cases passed.\n' "$case_count"
