#!/usr/bin/env bash
# selfcheck.sh — maintainer tool for the course itself (learners don't need it).
#
# It checks, in scratch copies of the repository:
#   1. solutions: the reference solutions in solutions/ pass every stage
#      (with -race unless RACE= is set), and the quiz files parse.
#   2. starter:   the untouched starter code fails every stage (so no stage
#                 can be "passed" by accident), quickly, and without panicking
#                 (a panic would hide the tests of later stages).
#
# Usage:
#   scripts/selfcheck.sh            # both checks
#   scripts/selfcheck.sh solutions  # only the solutions check
#   scripts/selfcheck.sh starter    # only the starter check
#   RUN='TestStage0[1-5]_' scripts/selfcheck.sh solutions   # subset of tests
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
mode="${1:-all}"
run="${RUN:-^TestStage}"
race="${RACE--race}"
budget="${BUDGET:-20}" # seconds a stage may take to fail on the starter
packages=(rawhttp gobin gocurl kvclient loadgen proxy)

work=""
cleanup() { if [ -n "$work" ]; then rm -rf "$work"; fi; }
trap cleanup EXIT

# Stage numbers are taken from the test files, e.g. tests/stage07_*_test.go.
stage_numbers() {
  ls "$root"/tests/stage[0-9][0-9]_*_test.go | sed -E 's|.*/stage([0-9]{2})_.*|\1|' | sort -u
}

copy_repo() {
  local dest="$1"
  mkdir -p "$dest"
  (cd "$root" && tar --exclude=.git --exclude=.course -cf - .) | (cd "$dest" && tar -xf -)
}

check_solutions() {
  work="$(mktemp -d)"
  copy_repo "$work"
  for p in "${packages[@]}"; do
    rm -rf "${work:?}/${p:?}"
    cp -r "$work/solutions/$p" "$work/$p"
  done
  echo "==> solutions: go vet"
  (cd "$work" && go vet ./...)
  echo "==> quizzes and runner: go test ./cmd/course"
  (cd "$work" && go test -count=1 ./cmd/course/)
  echo "==> solutions: go test $race -run '$run'"
  if (cd "$work" && go test $race -count=1 -run "$run" ./tests/); then
    echo "PASS: reference solutions pass"
  else
    echo "FAIL: reference solutions do not pass" >&2
    return 1
  fi
}

check_starter() {
  echo "==> starter: go vet"
  (cd "$root" && go vet ./...)
  echo "==> starter: every stage fails, within ${budget}s, without panicking"
  local failed=0 out start secs
  for n in $(stage_numbers); do
    start=$(date +%s)
    if out=$(cd "$root" && go test -count=1 -run "^TestStage${n}_" ./tests/ 2>&1); then
      echo "  stage $n: PASSES on starter code (bad!)" >&2
      failed=1
      continue
    fi
    secs=$(( $(date +%s) - start ))
    if grep -q "^panic:\|fatal error:" <<<"$out"; then
      echo "  stage $n: fails, but the starter PANICS (bad: it hides later tests)" >&2
      failed=1
    elif [ "$secs" -gt "$budget" ]; then
      echo "  stage $n: fails, but slowly (${secs}s; learners wait for timeouts)" >&2
      failed=1
    else
      echo "  stage $n: fails on starter code in ${secs}s (good)"
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
