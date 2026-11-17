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
  ldflags="-s -w -X main.version=${VERSION}"

  echo "==> ${name}"
  if [ "$goos" = "windows" ]; then
    # Desktop build: no console window. Closing the browser tab exits the app
    # (use -d for a headless service).
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags "${ldflags} -H=windowsgui" \
      -o "${OUT}/${name}${ext}" .
    # Console build for service/troubleshooting use.
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags "${ldflags}" \
      -o "${OUT}/${name}-console${ext}" .
    (cd "$OUT" && zip -q "${name}.zip" "${name}${ext}" "${name}-console${ext}")
  else
    CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
      go build -trimpath -ldflags "${ldflags}" \
      -o "${OUT}/${name}${ext}" .
    if [ "$goos" = "linux" ]; then
      cp contrib/desktop/open-rmslist.desktop "${OUT}/open-rmslist.desktop"
      ln -sf "${name}${ext}" "${OUT}/open-rmslist"
      tar -C "$OUT" -czf "${OUT}/${name}.tar.gz" "${name}${ext}" "open-rmslist" "open-rmslist.desktop"
      rm -f "${OUT}/open-rmslist" "${OUT}/open-rmslist.desktop"
    else
      tar -C "$OUT" -czf "${OUT}/${name}.tar.gz" "${name}${ext}"
    fi
  fi

  if [ "$goos" = "darwin" ]; then
    # Double-clickable macOS app bundle (unsigned; see README for Gatekeeper).
    appdir="${OUT}/open-rmslist.app"
    rm -rf "$appdir"
    mkdir -p "${appdir}/Contents/MacOS"
    cp "${OUT}/${name}${ext}" "${appdir}/Contents/MacOS/open-rmslist"
    cat > "${appdir}/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>open-rmslist</string>
  <key>CFBundleDisplayName</key><string>Winlink Gateway Map</string>
  <key>CFBundleIdentifier</key><string>radio.ku0hn.open-rmslist</string>
  <key>CFBundleVersion</key><string>${VERSION}</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleExecutable</key><string>open-rmslist</string>
  <key>LSMinimumSystemVersion</key><string>10.13</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
    (cd "$OUT" && zip -qry "${name}-app.zip" "open-rmslist.app")
    rm -rf "$appdir"
  fi
done

(cd "$OUT" && sha256sum open-rmslist-*.* > checksums.txt)
echo "==> wrote artifacts to ${OUT}/"
