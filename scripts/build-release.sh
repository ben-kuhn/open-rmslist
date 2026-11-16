#!/usr/bin/env bash
# Build release artifacts for all supported platforms.
#
# Usage:
#   ./scripts/build-release.sh            # uses "git describe" for the version
#   VERSION=0.1.0 ./scripts/build-release.sh
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}" # strip a leading "v" (e.g. from a vX.Y.Z tag)
OUT="${OUT:-dist}"

rm -rf "$OUT"
mkdir -p "$OUT"

targets=(
  "linux amd64"
  "linux arm64"
  "linux arm"
  "darwin amd64"
  "darwin arm64"
  "windows amd64"
  "windows arm64"
  "freebsd amd64"
  "openbsd amd64"
  "netbsd amd64"
)

for t in "${targets[@]}"; do
  read -r goos goarch <<<"$t"
  ext=""
  [ "$goos" = "windows" ] && ext=".exe"
  name="open-rmslist-${VERSION}-${goos}-${goarch}"

  echo "==> ${name}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${OUT}/${name}${ext}" .

  if [ "$goos" = "windows" ]; then
    (cd "$OUT" && zip -q "${name}.zip" "${name}${ext}")
  else
    tar -C "$OUT" -czf "${OUT}/${name}.tar.gz" "${name}${ext}"
  fi
done

(cd "$OUT" && sha256sum open-rmslist-*.* > checksums.txt)
echo "==> wrote artifacts to ${OUT}/"
