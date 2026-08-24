#!/usr/bin/env bash

set -euo pipefail

release_tag="${1:-}"
build_date="${2:-}"

mode="snapshot"
version="main"
major=""
major_minor=""
stable="false"
source_ref=""

if [[ -n "${release_tag}" ]]; then
  if [[ ! "${release_tag}" =~ ^impartus-cli-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*))?$ ]]; then
    echo "release tag must match impartus-cli-vMAJOR.MINOR.PATCH with an optional semver prerelease suffix" >&2
    exit 2
  fi

  major="${BASH_REMATCH[1]}"
  minor="${BASH_REMATCH[2]}"
  patch="${BASH_REMATCH[3]}"
  prerelease_suffix="${BASH_REMATCH[4]}"
  prerelease="${BASH_REMATCH[5]}"

  if [[ -n "${prerelease}" ]]; then
    IFS='.' read -r -a prerelease_identifiers <<< "${prerelease}"
    for identifier in "${prerelease_identifiers[@]}"; do
      if [[ "${identifier}" =~ ^[0-9]+$ && "${#identifier}" -gt 1 && "${identifier}" == 0* ]]; then
        echo "numeric prerelease identifiers must not contain leading zeroes" >&2
        exit 2
      fi
    done
  else
    stable="true"
  fi

  mode="release"
  version="${major}.${minor}.${patch}${prerelease_suffix}"
  major_minor="${major}.${minor}"
  source_ref="refs/tags/${release_tag}"
fi

if [[ -z "${build_date}" ]]; then
  build_date="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
fi
if [[ "${build_date}" == *$'\n'* || "${build_date}" == *$'\r'* ]]; then
  echo "build date must be a single line" >&2
  exit 2
fi

printf 'mode=%s\n' "${mode}"
printf 'release_tag=%s\n' "${release_tag}"
printf 'source_ref=%s\n' "${source_ref}"
printf 'version=%s\n' "${version}"
printf 'major_minor=%s\n' "${major_minor}"
printf 'major=%s\n' "${major}"
printf 'stable=%s\n' "${stable}"
printf 'build_date=%s\n' "${build_date}"
