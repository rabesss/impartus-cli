#!/usr/bin/env bash

set -euo pipefail

desloppify_bin="${DESLOPPIFY_BIN:-.venv-desloppify/bin/desloppify}"
state_path="${DESLOPPIFY_STATE_PATH:-.desloppify/state.json}"
minimum_score="${QUALITY_MIN_SCORE:-80}"
script_dir=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
adapter="${DESLOPPIFY_GOLANGCI_LINT_ADAPTER:-$script_dir/desloppify-golangci-lint-adapter.sh}"

if ! awk -v value="$minimum_score" 'BEGIN {
	exit !(value ~ /^[0-9]+([.][0-9]+)?$/ && value >= 0 && value <= 100)
}'; then
	printf 'Quality gate error: invalid minimum score: %s\n' "$minimum_score" >&2
	exit 1
fi

if [[ ! -x "$desloppify_bin" ]]; then
	printf 'Quality gate error: Desloppify executable not found: %s\n' "$desloppify_bin" >&2
	exit 1
fi

python_bin="${DESLOPPIFY_PYTHON:-$(dirname -- "$desloppify_bin")/python}"
if [[ ! -x "$python_bin" ]]; then
	printf 'Quality gate error: project-local Python executable not found: %s\n' "$python_bin" >&2
	exit 1
fi

if [[ "$adapter" != /* ]]; then
	if ! adapter_parent=$(cd -- "$(dirname -- "$adapter")" 2>/dev/null && pwd -P); then
		printf 'Quality gate error: cannot resolve Desloppify golangci-lint adapter path: %s\n' "$adapter" >&2
		exit 1
	fi
	adapter="$adapter_parent/$(basename -- "$adapter")"
fi
if [[ ! -x "$adapter" ]]; then
	printf 'Quality gate error: Desloppify golangci-lint adapter not found: %s\n' "$adapter" >&2
	exit 1
fi

real_linter="${DESLOPPIFY_GOLANGCI_LINT_REAL:-}"
if [[ -z "$real_linter" ]]; then
	real_linter=$(command -v golangci-lint || true)
elif [[ "$real_linter" != */* ]]; then
	real_linter=$(command -v "$real_linter" || true)
fi
if [[ -z "$real_linter" || ! -x "$real_linter" ]]; then
	printf 'Quality gate error: real golangci-lint v2 executable not found: %s\n' "${DESLOPPIFY_GOLANGCI_LINT_REAL:-<unset>}" >&2
	exit 1
fi

adapter_dir=$(mktemp -d "${TMPDIR:-/tmp}/impartus-desloppify-lint.XXXXXX")
trap 'rm -rf "$adapter_dir"' EXIT
ln -s "$adapter" "$adapter_dir/golangci-lint"

set +e
report=$(
	PATH="$adapter_dir:$PATH" \
		DESLOPPIFY_GOLANGCI_LINT_REAL="$real_linter" \
		"$desloppify_bin" scan --path . --state "$state_path" --no-badge 2>&1
)
scan_exit=$?
set -e

printf '%s\n' "$report"

if ! scan_completeness=$("$python_bin" - "$state_path" <<'PY'
import json
import pathlib
import sys

state_path = pathlib.Path(sys.argv[1])
try:
    with state_path.open(encoding="utf-8") as state_file:
        state = json.load(state_file)
except FileNotFoundError:
    print(f"Desloppify state file is missing: {state_path}", file=sys.stderr)
    raise SystemExit(1)
except (OSError, json.JSONDecodeError) as error:
    print(f"Desloppify state file is malformed or unreadable: {state_path}: {error}", file=sys.stderr)
    raise SystemExit(1)

if not isinstance(state, dict):
    print("Desloppify state must be a JSON object at the top level", file=sys.stderr)
    raise SystemExit(1)

completeness = state.get("scan_completeness")
if not isinstance(completeness, dict) or not isinstance(completeness.get("go"), str):
    print("Desloppify state is missing string scan_completeness.go", file=sys.stderr)
    raise SystemExit(1)

go_completeness = completeness["go"]
if go_completeness != "full":
    print(
        f"Desloppify scan_completeness.go is {go_completeness!r}; expected 'full'",
        file=sys.stderr,
    )
    raise SystemExit(1)

print(go_completeness)
PY
); then
	printf 'Quality gate failed: Desloppify Go scan completeness is not full.\n' >&2
	exit 1
fi

extract_score() {
	local label=$1

	printf '%s\n' "$report" | awk -v label="$label" '
		{
			lower = tolower($0)
			if (lower ~ /^[[:space:]]*scores[[:space:]]*:/) {
				summary = $0
			}
		}
		END {
			if (summary == "") {
				exit 1
			}

			lower = tolower(summary)
			pattern = "(^|[[:space:]])" tolower(label) "[[:space:]]+"
			if (!match(lower, pattern)) {
				exit 1
			}

			value = substr(summary, RSTART + RLENGTH)
			if (!match(value, /^[0-9]+([.][0-9]+)?[[:space:]]*\/[[:space:]]*100([[:space:]]|$)/)) {
				exit 1
			}

			value = substr(value, RSTART, RLENGTH)
			sub(/[[:space:]]*\/[[:space:]]*100[[:space:]]*$/, "", value)
			print value
		}
	'
}

objective_score=$(extract_score objective || true)
overall_score=$(extract_score overall || true)
strict_score=$(extract_score strict || true)

if [[ -z "$objective_score" ]] || ! awk -v value="$objective_score" 'BEGIN {
	exit !(value ~ /^[0-9]+([.][0-9]+)?$/ && value >= 0 && value <= 100)
}'; then
	printf 'Quality gate failed: no valid Desloppify objective score was found (scanner exit: %s).\n' "$scan_exit" >&2
	exit 1
fi

printf 'Desloppify exit code: %s\n' "$scan_exit"
printf 'Desloppify Go scan completeness: %s\n' "$scan_completeness"
printf 'Desloppify objective score: %s (minimum: %s)\n' "$objective_score" "$minimum_score"
printf 'Desloppify overall score: %s\n' "${overall_score:-unavailable}"
printf 'Desloppify strict score: %s\n' "${strict_score:-unavailable}"

if ! awk -v score="$objective_score" -v minimum="$minimum_score" 'BEGIN { exit !(score >= minimum) }'; then
	printf 'Quality gate failed: Desloppify objective score %s is below minimum %s.\n' "$objective_score" "$minimum_score" >&2
	exit 1
fi

if (( scan_exit != 0 )); then
	printf 'Desloppify scanner exited with %s; the parsed objective score satisfies the explicit threshold policy.\n' "$scan_exit" >&2
fi

printf 'Quality gate passed: Desloppify objective score %s meets minimum %s.\n' "$objective_score" "$minimum_score"
