#!/usr/bin/env bash
# Build a self-contained VOACAP engine bundle for this platform.
#
# The bundle contains the voacapl engine, the generated itshfbc coefficient
# data, and (on Linux/macOS) the gfortran runtime it needs, so the desktop app
# can download one archive and run it without a package manager.
#
# Output: ${OUT:-dist-voacap}/voacap-<os>-<arch>.tar.gz and a .sha256 sidecar.
# Windows bundles are assembled in the release workflow from the Linux data plus
# the upstream voacapw.exe, so this script only runs on Linux and macOS.
set -euo pipefail

cd "$(dirname "$0")/.."

VERSION="${VOACAP_VERSION:-0.7.7}"
OUT="${OUT:-dist-voacap}"
GOOS="${GOOS:-$(go env GOOS)}"
GOARCH="${GOARCH:-$(go env GOARCH)}"
ARTOS="$GOOS"
[ "$GOOS" = "darwin" ] && ARTOS="macos"

case "$GOOS" in
  linux|darwin) ;;
  *)
    echo "build-voacap.sh builds Linux and macOS bundles only (got $GOOS)" >&2
    exit 1
    ;;
esac

for tool in gfortran make curl tar; do
  command -v "$tool" >/dev/null 2>&1 || {
    echo "missing required tool: $tool" >&2
    exit 1
  }
done
if [ "$GOOS" = "darwin" ]; then
  command -v install_name_tool >/dev/null 2>&1 || {
    echo "missing required tool: install_name_tool" >&2
    exit 1
  }
fi

WORK="$(mktemp -d)"
trap 'rm -rf "$WORK"' EXIT
mkdir -p "$WORK/src" "$OUT"

echo "==> building voacapl ${VERSION} for ${GOOS}/${GOARCH}"
curl -fsSL "https://github.com/jawatson/voacapl/archive/refs/tags/v${VERSION}.tar.gz" \
  -o "$WORK/src.tar.gz"
tar -xzf "$WORK/src.tar.gz" -C "$WORK/src" --strip-components=1

(
  cd "$WORK/src"
  ./configure --prefix="$WORK/install" >/dev/null
  # The upstream Makefile does not declare all Fortran module dependencies, so
  # a parallel build can compile users before their .mod file exists.
  make >/dev/null
  make install >/dev/null
)

BUNDLE="$WORK/bundle"
mkdir -p "$BUNDLE/voacap/lib"
cp "$WORK/install/bin/voacapl" "$BUNDLE/voacap/voacapl"
cp -R "$WORK/install/share/voacapl/itshfbc" "$BUNDLE/voacap/itshfbc"

# Ship the gfortran runtime next to the engine. voacapLocal adds this directory
# to LD_LIBRARY_PATH / DYLD_LIBRARY_PATH when it launches the bundle.
if [ "$GOOS" = "linux" ]; then
  libs="$(ldd "$BUNDLE/voacap/voacapl" | awk '/=> \// {print $3}' | grep -E 'libgfortran|libquadmath|libgcc_s' || true)"
else
  libs="$(otool -L "$BUNDLE/voacap/voacapl" | awk 'NR>1 {print $1}' | grep -E 'libgfortran|libquadmath|libgcc_s' || true)"
fi
for lib in $libs; do
  cp -L "$lib" "$BUNDLE/voacap/lib/"
done

if [ "$GOOS" = "darwin" ]; then
  # Rewrite Homebrew install names so the bundle does not depend on the
  # builder's /opt/homebrew (or /usr/local) tree.
  for old in $(otool -L "$BUNDLE/voacap/voacapl" | awk 'NR>1 {print $1}' | grep -E '^/opt/homebrew|^/usr/local' || true); do
    install_name_tool -change "$old" "@executable_path/lib/$(basename "$old")" "$BUNDLE/voacap/voacapl"
  done
  for lib in "$BUNDLE"/voacap/lib/*.dylib; do
    [ -e "$lib" ] || continue
    install_name_tool -id "@loader_path/$(basename "$lib")" "$lib"
    for old in $(otool -L "$lib" | awk 'NR>1 {print $1}' | grep -E '^/opt/homebrew|^/usr/local' || true); do
      install_name_tool -change "$old" "@loader_path/$(basename "$old")" "$lib"
    done
  done
fi

NAME="voacap-${ARTOS}-${GOARCH}"
rm -f "$OUT/${NAME}.tar.gz" "$OUT/${NAME}.tar.gz.sha256"
tar -C "$BUNDLE" -czf "$OUT/${NAME}.tar.gz" voacap

if command -v sha256sum >/dev/null 2>&1; then
  (cd "$OUT" && sha256sum "${NAME}.tar.gz" > "${NAME}.tar.gz.sha256")
else
  (cd "$OUT" && shasum -a 256 "${NAME}.tar.gz" > "${NAME}.tar.gz.sha256")
fi

echo "==> wrote $OUT/${NAME}.tar.gz"
