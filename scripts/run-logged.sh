#!/usr/bin/env bash
# Run a command, stream its combined output to stdout and to a log file, and
# exit with the command's own status.
#
# usage: run-logged.sh LOG_FILE COMMAND [ARG...]
#
# A plain `command | tee log; status=$?` records tee's status unless pipefail
# is set, and GitHub's default `bash -e` step shell does not set it. This helper
# reads both statuses from PIPESTATUS instead:
#   - a nonzero command status is the exit status, so the step fails;
#   - a log that could not be fully written fails the step even when the
#     command succeeded, so a missing or truncated artifact never passes.
set -u

if [ "$#" -lt 2 ]; then
  echo "usage: $0 LOG_FILE COMMAND [ARG...]" >&2
  exit 2
fi

log_file=$1
shift

# GNU tee's --output-error=warn keeps writing the log when stdout goes away
# (instead of dying on SIGPIPE and taking the command with it) and still exits
# nonzero, which is reported below.
tee_options=()
if tee --output-error=warn /dev/null </dev/null >/dev/null 2>&1; then
  tee_options=(--output-error=warn)
fi

# errexit stays off (only nounset is set), so a failing command cannot end the
# script before PIPESTATUS is read. The ${a[@]+...} form keeps an empty array
# safe under nounset on bash 3.2.
"$@" 2>&1 | tee ${tee_options[@]+"${tee_options[@]}"} -- "$log_file"
statuses=("${PIPESTATUS[@]}")

command_status=${statuses[0]}
tee_status=${statuses[1]}

if [ "$tee_status" -ne 0 ]; then
  echo "::error::failed to write log $log_file (tee status $tee_status); the log may be incomplete" >&2
fi
if [ "$command_status" -ne 0 ]; then
  echo "::error::$1 exited with status $command_status" >&2
  exit "$command_status"
fi
if [ "$tee_status" -ne 0 ]; then
  exit 1
fi
