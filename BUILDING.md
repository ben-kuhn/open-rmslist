# Building RMS Radar

## Requirements

- **Go 1.25+** for the web/server binary. It has no CGo and no third-party
  runtime dependencies.
- **CGo + a platform webview runtime** for the native desktop binary:
  - Linux: `libwebkit2gtk-4.1-dev`, `libgtk-3-dev`, `pkg-config`, a C/C++
    compiler
  - macOS: Xcode command-line tools (WKWebView is built in)
  - Windows: MinGW-w64 and the WebView2 runtime

## Web/server

```bash
CGO_ENABLED=0 go build -o rms-radar .
./rms-radar              # browser mode
./rms-radar -d           # service mode
```

The build is static and can be cross-compiled for Linux, macOS, Windows, and
the BSDs (`CGO_ENABLED=0 GOOS=... GOARCH=... go build .`).

## Native desktop app

```bash
./scripts/build-desktop.sh     # artifact in dist-desktop/
```

or directly:

```bash
CGO_ENABLED=1 go build -tags desktop -o rms-radar-desktop .
```

The desktop binary is built from the same module with the `desktop` build tag.
It starts the shared server on `127.0.0.1:0` (an ephemeral port) and hosts the
UI in a native window; closing the window shuts everything down.

Linux note: `webview_go` asks pkg-config for `webkit2gtk-4.0`, while current
distributions ship the compatible `webkit2gtk-4.1` ABI. `build-desktop.sh`
creates a temporary `webkit2gtk-4.0.pc` shim automatically. For a manual build,
add such a shim to `PKG_CONFIG_PATH` if your distro has no `webkit2gtk-4.0.pc`.

## Tests

```bash
gofmt -l .
go vet ./...
go test ./...
```

The browser end-to-end suite lives in `tests/e2e` (Playwright). It runs against
a deployed instance by default; set `BASE_URL` to test another one:

```bash
cd tests/e2e
npm install
BASE_URL=http://localhost:8079 npx playwright test
```

## Release artifacts

- `scripts/build-release.sh` — CGO-free web/server binaries for every target,
  plus sha256 checksums (`dist/`).
- `scripts/build-desktop.sh` — native desktop artifact for the current OS:
  macOS `.app` zip, Windows `.exe` zip, Linux tarball + `.desktop` (`dist-desktop/`).
- `scripts/build-voacap.sh` — self-contained VOACAP engine bundle for the
  current Linux/macOS host (`dist-voacap/`), with a pinned upstream source
  hash. The Windows bundle was built once from the pinned upstream ITS HF
  distribution and is mirrored at the v0.2.4 release; CI/release pin and verify
  that mirror's digest, then sign every bundle with minisign. Desktop builds
  download and verify these bundles on first launch.
- `scripts/build-voacap-windows.sh` — maintainer tool to rebuild the Windows
  bundle from the pinned upstream archive (SHA-256
  `29d3902c...`); update the mirror digest in the workflows if it changes.
- `scripts/build-deb.sh <binary> <package> <version> <arch> <web|desktop>` —
  builds a `.deb` from a prebuilt binary.
- `Dockerfile` / `compose.yaml` — multi-stage, non-root container with the
  web server and VOACAP; `ghcr.io/ben-kuhn/rms-radar` is built by the
  `Container` workflow.
- `site/` — static download and self-hosting site, published to GitHub Pages
  by `.github/workflows/pages.yml`.
- `packaging/arch/PKGBUILD` — Arch package (web/server).
- `packaging/arch/PKGBUILD.desktop` — Arch package (native desktop).
- `packaging/rpm/rms-radar.spec` — RPM package (web/server).
- `packaging/rpm/rms-radar-desktop.spec` — RPM package (native desktop).
- `packaging/common/rms-radar.service` — systemd unit installed by the distro
  packages (`DynamicUser`, `StateDirectory=rms-radar`, `/usr/bin/rms-radar`).
- `contrib/systemd/rms-radar.service` — manual-install unit for
  `/usr/local/bin` and a static `rms-radar` user.
- `contrib/desktop/*.desktop` — desktop launchers.

The `v*` tag release workflow builds the web artifacts on Ubuntu, the native
desktop artifacts on Linux/macOS/Windows runners, the self-contained VOACAP
engine bundles for Linux/macOS/Windows, signs the bundles, then publishes
everything with a combined `SHA256SUMS`. Signing requires the
`VOACAP_MINISIGN_SECRET_KEY` Actions secret; the public key is embedded in
`internal/voacapget`. Keep an offline backup of the secret. The retired v0.2.4
key is no longer trusted; v0.2.6 and later bundles use the current key.

## CI/CD

- `.github/workflows/ci.yml` — vet, race tests, govulncheck, cross-compile the
  web targets, build the desktop binary, build/smoke-test the VOACAP bundles,
  and build/run the container.
- `.github/workflows/docker.yml` — publish multi-arch images to GHCR on main
  (`edge`) and release tags (`latest`, semver).
- `.github/workflows/pages.yml` — publish `site/` to GitHub Pages.
- `.github/workflows/packaging.yml` — build the Arch, RPM, and Nix packages on
  every push/PR.
- `.github/workflows/release.yml` — on a `v*` tag: web + desktop + `.deb`/`.rpm`/
  Arch packages for both the server and the native desktop app, plus the Arch
  `PKGBUILD` recipes, then publish the GitHub Release.
- `.github/workflows/deployed.yml` — Playwright smoke tests against the live
  site (or a supplied `base_url`).
- `.github/workflows/codeql.yml` — CodeQL analysis.

## Layout

- `main.go` — web/server entry point (`!desktop` build tag).
- `desktop.go` — native-window entry point (`desktop` build tag).
- `shared.go` — embedded web assets, version, env/flag helpers.
- `internal/app` — shared startup: loader, refresh loop, HTTP listener.
- `internal/cmsapi` — Winlink gateway fetch + disk cache/embedded fallback.
- `internal/catalog` — normalization, bands, service-code labels.
- `internal/propagation` — heuristic and VOACAP ranking, LQI, groundwave floor.
- `internal/server` — HTTP handlers, security headers, tile proxy, desktop lifecycle.
- `internal/voacapget` — checksum- and minisign-verified VOACAP distribution
  downloader used by the desktop first-run setup.
- `web/` — vanilla JS + vendored Leaflet UI, client/gateway guides.
- `packaging/`, `contrib/`, `scripts/` — distro packaging and release tooling.

## Why a native webview?

The desktop build uses the small C/C++ webview library (the same one behind
[`webview/webview`](https://github.com/webview/webview)): WKWebView on macOS,
WebView2 on Windows, WebKitGTK 4.1 on Linux. Closing the window is a definitive
quit; there is no browser, tab, or heartbeat heuristic. The cost is CGo plus
per-OS SDK/runtime dependencies for that build only. The web/server binary stays
CGO-free and is the right artifact for servers.

Alternatives considered:

1. **Browser + heartbeat** — zero dependencies, but tab-close detection is
   best-effort (beacon + grace + Quit button).
2. **System tray wrapper** — Open/Quit menu; the UI stays in the browser.
   Closing the browser is not the same as quitting.
3. **App-mode browser window** — `--app=URL` gives a chromeless window but still
   depends on the browser and the beacon heuristic.

## License

[MIT](LICENSE).
