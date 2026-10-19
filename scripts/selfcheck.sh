#!/usr/bin/env bash
# selfcheck.sh — maintainer tool for the course itself (learners don't need it).
#
# It checks two things in a scratch copy of the repository:
#   1. solutions: the reference solutions in solutions/ pass every stage.
#   2. starter:   the untouched starter code fails every stage (so no stage
#                 can be "passed" by accident).
#
# Usage:
#   scripts/selfcheck.sh            # both checks
#   scripts/selfcheck.sh solutions  # only the solutions check
#   scripts/selfcheck.sh starter    # only the starter check
#   RUN='TestStage0[1-5]_' scripts/selfcheck.sh solutions   # subset
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
mode="${1:-all}"
run="${RUN:-^TestStage}"
race="${RACE:--race}"
packages=(rawhttp gobin gocurl kvclient loadgen proxy)

copy_repo() {
  local dest="$1"
  mkdir -p "$dest"
  (cd "$root" && tar --exclude=.git --exclude=.course -cf - .) | (cd "$dest" && tar -xf -)
}

check_solutions() {
  local work
  work="$(mktemp -d)"
  copy_repo "$work"
  for p in "${packages[@]}"; do
    rm -rf "${work:?}/$p"
    cp -r "$work/solutions/$p" "$work/$p"
  done
  echo "==> solutions: go vet"
  (cd "$work" && go vet ./...)
  echo "==> solutions: go test $race -run '$run'"
  if (cd "$work" && go test $race -count=1 -run "$run" ./tests/); then
    echo "PASS: reference solutions pass"
  else
    echo "FAIL: reference solutions do not pass" >&2
    rm -rf "$work"
    return 1
  fi
  rm -rf "$work"
}

check_starter() {
  echo "==> starter: go vet"
  (cd "$root" && go vet ./...)
  echo "==> starter: every stage must have at least one failing test"
  local failed=0
  for n in $(seq -w 1 26); do
    if (cd "$root" && go test -count=1 -run "^TestStage${n}_" ./tests/ >/dev/null 2>&1); then
      echo "  stage $n: PASSES on starter code (bad!)" >&2
      failed=1
    else
      echo "  stage $n: fails on starter code (good)"
    fi
  done
  return $failed
}

case "$mode" in
  solutions) check_solutions ;;
  starter) check_starter ;;
  all) check_solutions && check_starter ;;
  *) echo "usage: $0 [solutions|starter|all]" >&2; exit 2 ;;
esac
