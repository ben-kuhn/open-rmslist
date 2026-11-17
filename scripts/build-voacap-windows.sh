#!/usr/bin/env bash
# Build the canonical Windows VOACAP bundle from the pinned upstream ITS HF
# archive. This is a maintainer tool, not part of every release: CI and the
# release workflow consume the signed, digest-pinned mirror at the v0.2.4
# release. Re-run this script only when intentionally refreshing the mirror,
# then update the pinned digest in .github/workflows/{ci,release}.yml and the
# v0.2.5 (or later) release asset.
set -euo pipefail

cd "$(dirname "$0")/.."

OUT="${OUT:-dist-voacap}"
UPSTREAM_SHA256="${VOACAP_WINDOWS_UPSTREAM_SHA256:-29d3902c695523841dae395db937cda4f154a9e2cbe9aed38f38bd1f98d42e9a}"
UPSTREAM_URL="${VOACAP_WINDOWS_UPSTREAM_URL:-https://greg-hand.com/versions/itshfbc%20.zap}"

for tool in curl unzip zip sha256sum; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "missing required tool: $tool" >&2
    exit 1
  }
done

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/win" "$OUT"

echo "==> downloading pinned upstream Windows VOACAP archive"
curl -fsSL --retry 4 --retry-delay 5 "$UPSTREAM_URL" -o "$WORK/itshfbc.zip"
got="$(sha256sum "$WORK/itshfbc.zip" | awk '{print $1}')"
if [ "$got" != "$UPSTREAM_SHA256" ]; then
  echo "upstream Windows archive SHA-256 mismatch: got $got want $UPSTREAM_SHA256" >&2
  exit 1
fi

unzip -q "$WORK/itshfbc.zip" -d "$WORK/win"
test -x "$WORK/win/itshfbc/bin_win/voacapw.exe" 2>/dev/null || test -f "$WORK/win/itshfbc/bin_win/voacapw.exe"
test -f "$WORK/win/itshfbc/bin_win/SALFLIBC.DLL"

NAME="voacap-windows-amd64.zip"
rm -f "$OUT/$NAME" "$OUT/$NAME.sha256"
(cd "$WORK/win" && zip -qr "$OUT/$NAME" itshfbc)
(cd "$OUT" && sha256sum "$NAME" > "$NAME.sha256")
echo "==> wrote $OUT/$NAME"
cat "$OUT/$NAME.sha256"
