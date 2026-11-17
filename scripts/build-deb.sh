#!/usr/bin/env bash
# Build a .deb from a prebuilt binary.
#
# Usage: build-deb.sh <binary> <package> <version> <arch> <web|desktop>
#   arch uses Debian names: amd64, arm64, armhf
set -euo pipefail

BIN="$1"
PKG="$2"
VER="$3"
ARCH="$4"
KIND="$5"
OUT="${OUT:-dist}"

case "$KIND" in
  web)
    INSTALL_NAME="rms-radar"
    SUMMARY="Winlink RMS gateway map (web/server)"
    LONG="A self-hosted web map of Winlink RMS gateways with service-code filtering and propagation ranking. Runs a local HTTP server and refreshes the Winlink gateway list in the background."
    DEPENDS="ca-certificates"
    ;;
  desktop)
    INSTALL_NAME="rms-radar-desktop"
    SUMMARY="Winlink RMS gateway map (desktop)"
    LONG="A native desktop window for mapping and ranking Winlink RMS gateways with service-code filtering and propagation ranking."
    DEPENDS="libwebkit2gtk-4.1-0, libgtk-3-0 | libgtk-3-0t64, ca-certificates"
    ;;
  *)
    echo "unknown package kind: $KIND" >&2
    exit 2
    ;;
esac

ROOT="$(mktemp -d)"
trap 'rm -rf "$ROOT"' EXIT

mkdir -p "$ROOT/DEBIAN" "$ROOT/usr/bin" "$ROOT/usr/share/applications" \
  "$ROOT/usr/share/doc/$PKG" "$ROOT/lib/systemd/system"

install -Dm755 "$BIN" "$ROOT/usr/bin/$INSTALL_NAME"
install -Dm644 LICENSE "$ROOT/usr/share/doc/$PKG/copyright"

if [ "$KIND" = "web" ]; then
  install -Dm644 packaging/common/rms-radar.service "$ROOT/lib/systemd/system/rms-radar.service"
  install -Dm644 contrib/desktop/rms-radar.desktop "$ROOT/usr/share/applications/rms-radar.desktop"
else
  install -Dm644 contrib/desktop/rms-radar-desktop.desktop "$ROOT/usr/share/applications/rms-radar-desktop.desktop"
fi

cat > "$ROOT/DEBIAN/control" <<EOF
Package: $PKG
Version: $VER
Section: hamradio
Priority: optional
Architecture: $ARCH
Maintainer: RMS Radar contributors <ben-kuhn@users.noreply.github.com>
Homepage: https://github.com/ben-kuhn/rms-radar
Depends: $DEPENDS
Description: $SUMMARY
 $LONG
EOF

mkdir -p "$OUT"
dpkg-deb --build --root-owner-group "$ROOT" "$OUT/${PKG}_${VER}_${ARCH}.deb" >/dev/null
echo "==> $OUT/${PKG}_${VER}_${ARCH}.deb"
