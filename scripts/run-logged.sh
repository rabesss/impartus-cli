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

# If stdout goes away (a closed reader), SIGPIPE would kill tee and then the
# command, losing both the log and the command's status. tee runs with SIGPIPE
# ignored (an ignored signal survives exec), so it gets EPIPE instead and keeps
# draining the command into the log (GNU and BSD tee alike). Only tee's
# subshell ignores it: the command keeps its own SIGPIPE behaviour and status.
#
# errexit stays off (only nounset is set), so a failing command cannot end the
# script before PIPESTATUS is read.
"$@" 2>&1 | (trap '' PIPE && exec tee -- "$log_file")
statuses=("${PIPESTATUS[@]}")

command_status=${statuses[0]}
tee_status=${statuses[1]}

if [ "$tee_status" -ne 0 ]; then
  echo "::error::tee failed writing the log $log_file or stdout (tee status $tee_status); the log may be incomplete" >&2
fi
if [ "$command_status" -ne 0 ]; then
  echo "::error::$1 exited with status $command_status" >&2
  exit "$command_status"
fi
if [ "$tee_status" -ne 0 ]; then
  exit 1
fi
# One line on stderr, so the streamed output and the log stay exactly the
# command's own output. A failed write there must not turn a successful run
# into a failure, so the exit status stays 0.
printf 'run-logged: %s succeeded; log saved to %s\n' "$1" "$log_file" >&2 || true
exit 0
