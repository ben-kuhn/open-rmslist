#!/usr/bin/env bash
# Enforce per-package coverage floors. The overall percentage can hide a large
# untested package, so key packages are checked individually.
set -euo pipefail

cd "$(dirname "$0")/.."

out="$(go test -cover ./... 2>&1)"

check() {
  pkg="github.com/ben-kuhn/rms-radar$1"
  min="$2"
  got="$(printf '%s\n' "$out" | awk -v pkg="$pkg" '$1 == "ok" && $2 == pkg { print $5 }' | tr -d '%')"
  if [ -z "$got" ]; then
    echo "no coverage line for $pkg" >&2
    exit 1
  fi
  if ! awk -v got="$got" -v min="$min" 'BEGIN { if (got + 0 < min + 0) exit 1 }'; then
    echo "$pkg coverage ${got}% is below ${min}%" >&2
    exit 1
  fi
  echo "$pkg: ${got}% (floor ${min}%)"
}

check "" 10
check "/internal/app" 60
check "/internal/atomicfile" 40
check "/internal/catalog" 75
check "/internal/cmsapi" 75
check "/internal/propagation" 45
check "/internal/propagation/voacap" 60
check "/internal/server" 70
check "/internal/voacapget" 65
