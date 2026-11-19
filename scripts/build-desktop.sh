#!/usr/bin/env bash
# Build the native-window desktop artifact on its native OS.
#
# CGo is required, so this cannot cross-compile: run it on Linux (WebKitGTK),
# macOS (Xcode command-line tools), and Windows (MinGW + WebView2).
#
# Usage:
#   ./scripts/build-desktop.sh
#   VERSION=0.1.0 ./scripts/build-desktop.sh
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
VERSION="${VERSION#v}"
OUT="${OUT:-dist-desktop}"
GOOS="$(go env GOOS)"
GOARCH="$(go env GOARCH)"
ARTOS="$GOOS"
[ "$GOOS" = "darwin" ] && ARTOS="macos"
NAME="rms-radar-desktop-${VERSION}-${ARTOS}-${GOARCH}"
EXT=""
[ "$GOOS" = "windows" ] && EXT=".exe"

rm -rf "$OUT"
mkdir -p "$OUT"

# webview_go asks pkg-config for webkit2gtk-4.0. Modern distributions ship the
# compatible 4.1 ABI under a different package name, so provide a tiny shim.
case "$GOOS" in
  linux|freebsd|openbsd|netbsd)
    if ! pkg-config --exists webkit2gtk-4.0 2>/dev/null && pkg-config --exists webkit2gtk-4.1 2>/dev/null; then
      shim="$(mktemp -d)"
      cat > "$shim/webkit2gtk-4.0.pc" <<EOF
Name: webkit2gtk-4.0
Description: WebKitGTK 4.1 compatibility shim for webview
Version: $(pkg-config --modversion webkit2gtk-4.1)
Requires: webkit2gtk-4.1
EOF
      export PKG_CONFIG_PATH="$shim${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}"
      trap 'rm -rf "$shim"' EXIT
    fi
    ;;
esac

LDFLAGS="-s -w -X main.version=${VERSION}"
[ "$GOOS" = "windows" ] && LDFLAGS="${LDFLAGS} -H=windowsgui"

echo "==> ${NAME}"
CGO_ENABLED=1 go build -tags desktop -trimpath -ldflags "${LDFLAGS}" \
  -o "${OUT}/${NAME}${EXT}" .

case "$GOOS" in
  darwin)
    app="${OUT}/RMS Radar.app"
    mkdir -p "${app}/Contents/MacOS" "${app}/Contents/Resources"
    cp "${OUT}/${NAME}" "${app}/Contents/MacOS/rms-radar-desktop"
    cp contrib/desktop/rms-radar.icns "${app}/Contents/Resources/rms-radar.icns"
    cat > "${app}/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>RMS Radar</string>
  <key>CFBundleDisplayName</key><string>RMS Radar</string>
  <key>CFBundleIdentifier</key><string>radio.ku0hn.rms-radar-desktop</string>
  <key>CFBundleVersion</key><string>${VERSION}</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleExecutable</key><string>rms-radar-desktop</string>
  <key>CFBundleIconFile</key><string>rms-radar.icns</string>
  <key>LSMinimumSystemVersion</key><string>10.13</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST
    (cd "$OUT" && zip -qry "${NAME}-app.zip" "RMS Radar.app")
    rm -rf "$app"
    ;;
  windows)
    if command -v zip >/dev/null 2>&1; then
      (cd "$OUT" && zip -q "${NAME}.zip" "${NAME}${EXT}")
    else
      (cd "$OUT" && powershell.exe -NoProfile -Command "Compress-Archive -Path '${NAME}${EXT}' -DestinationPath '${NAME}.zip' -Force")
    fi
    ;;
  *)
    cp contrib/desktop/rms-radar-desktop.desktop "${OUT}/rms-radar-desktop.desktop"
    cp contrib/desktop/rms-radar.svg "${OUT}/rms-radar.svg"
    ln -sf "${NAME}${EXT}" "${OUT}/rms-radar-desktop"
    tar -C "$OUT" -czf "${OUT}/${NAME}.tar.gz" "${NAME}${EXT}" "rms-radar-desktop" "rms-radar-desktop.desktop" "rms-radar.svg"
    rm -f "${OUT}/rms-radar-desktop" "${OUT}/rms-radar-desktop.desktop" "${OUT}/rms-radar.svg"
    ;;
esac

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$OUT" && sha256sum rms-radar-desktop-* > checksums.txt)
fi
echo "==> wrote desktop artifacts to ${OUT}/"
