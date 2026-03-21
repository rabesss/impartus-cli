#!/usr/bin/env sh
set -eu

go mod download

if ! command -v desloppify >/dev/null 2>&1; then
  python3 -m pip install --user "desloppify[full]"
fi
