# RMS Radar

**A free, open-source map of Winlink RMS gateways.**

RMS Radar shows the Winlink gateways near you on a map, lets you filter by the
service codes you care about, and ranks them by how likely you are to reach
them. It runs as a normal desktop app on macOS, Windows, and Linux — no server
or command line required — and can also be self-hosted for a club or network.

> **Unofficial.** RMS Radar is not affiliated with the Winlink Development Team
> or ARSFI. Gateway data comes from Winlink's public API. The official list and
> map are at [winlink.org/RMSChannels](https://winlink.org/RMSChannels).

## Download

Get the latest version from the
**[RMS Radar releases page](https://github.com/ben-kuhn/rms-radar/releases/latest)**.

### macOS

1. Download `rms-radar-desktop-<version>-macos-arm64-app.zip`.
2. Unzip it and drag **RMS Radar.app** into **Applications**.
3. The app is not signed yet, so the first launch needs **right-click → Open**
   (or run `xattr -dr com.apple.quarantine "RMS Radar.app"` in Terminal).

The desktop app is Apple Silicon (arm64). Intel Mac users can use the
web/server build or the browser-based launcher.

### Windows

1. Download `rms-radar-desktop-<version>-windows-amd64.zip`.
2. Unzip it and run `rms-radar-desktop.exe`.
3. If Windows SmartScreen appears, choose **More info → Run anyway**.

RMS Radar uses Microsoft's WebView2 runtime, which is included with Windows 10
and 11. If it is missing, install the
[WebView2 Evergreen Runtime](https://developer.microsoft.com/microsoft-edge/webview2/).

### Linux

- **Debian / Ubuntu:** download `rms-radar-desktop_<version>_amd64.deb`, then:

  ```bash
  sudo apt install ./rms-radar-desktop_<version>_amd64.deb
  ```

  Launch **RMS Radar** from your applications menu.

- **Any other distribution:** download
  `rms-radar-desktop-<version>-linux-amd64.tar.gz`, extract it, and run
  `./rms-radar-desktop`. It needs WebKitGTK 4.1
  (`libwebkit2gtk-4.1-0` on Debian/Ubuntu, `webkit2gtk-4.1` on Arch/Fedora).

- **NixOS:** use the
  [`nix-ham-packages`](https://github.com/ben-kuhn/nix-ham-packages) overlay.

Self-hosting the web version? See [SELF-HOSTING.md](SELF-HOSTING.md) for `.deb`,
`.rpm`, Arch, and NixOS server packages.

## Using RMS Radar

1. **Enter your grid square** (for example `EM38xx`). Not sure? Use a
   [Maidenhead locator tool](https://www.levinecentral.com/ham/grid_square.php).
2. **Pick a service code** — `PUBLIC` is the usual open amateur code.
   `EMCOMM`, `MERCURY`, `VARDEFM`, `MODEM73`, and `DATA2G` are common community
   codes; other projects and private groups use their own. You can also
   filter by band and mode.
3. **Press Rank** to score the gateways by predicted link quality. Each row
   shows the band and mode that produced its best score.
4. **Click a gateway** to center it on the map and open its details: frequencies,
   modes, service codes, and the link estimate.
5. On a phone, use the **Map** and **List** tabs to switch views.

The map is dark by default; the ◐ button switches to light mode. The gateway
list and map tiles are cached, so previously viewed areas work offline.

## What it does

- **Maps the live Winlink gateway list** on OpenStreetMap.
- **Filters by service code** — `PUBLIC`, `EMCOMM`, `MERCURY`, `VARDEFM`, `MODEM73`,
  `DATA2G`, and any other community or private codes you configure.
- **Shows the real mode** for every channel. Mercury and VardeFM gateways are
  labelled from their service code, even though Winlink advertises them as VARA.
- **Ranks by propagation** — a built-in heuristic works everywhere, and an
  optional VOACAP engine gives point-to-point HF forecasts.
- **Hides implausible paths** — 0% VHF/UHF results are dropped while ranking,
  and very short HF paths get a realistic groundwave/NVIS score.
- **Runs natively** on macOS, Windows, and Linux, or in any browser when
  self-hosted.

### Better HF predictions

RMS Radar always gives a built-in estimate. The desktop app goes further: on
first launch it downloads the matching VOACAP engine automatically (a few MB,
once) and uses it for full point-to-point forecasts — no setup, no package
manager.

If you prefer to manage the engine yourself, or you are offline, install one of
these and RMS Radar will use it instead:

- **macOS:** `brew install la5nta/tap/voacapl`
- **Debian/Ubuntu:** `sudo apt install voacapl voacapl-data`
- **Windows:** install the VOACAP/ITSHFBC package into `C:\itshfbc`
  ([download](https://www.greg-hand.com/versions/itshfbc_180417a.exe),
  [notes](https://www.greg-hand.com/hfwin32.html))
- **Arch:** `voacapl` from the AUR
- **NixOS:** use the `voacapl` package from
  [`nix-ham-packages`](https://github.com/ben-kuhn/nix-ham-packages)

The desktop app checks for these installs before downloading anything.
Self-hosters can also point at a remote API or a custom binary. See
[SELF-HOSTING.md](SELF-HOSTING.md).

## Guides

- **[Client setup guide](https://rmsradar.ku0hn.radio/clients.html)** — how to
  add service codes in Winlink Express, PAT, WoAD, RadioMail, and HTCommander.
- **[Gateway setup guide](https://rmsradar.ku0hn.radio/gateway.html)** — how to
  publish service codes from RMS Trimode and BPQ32/LinBPQ, including the
  one-frequency/mode reporting rule.

Both guides are also served by any self-hosted instance at `/clients.html` and
`/gateway.html`.

## Self-hosting

Want to run RMS Radar for a club, a net, or a whole LAN? See
**[SELF-HOSTING.md](SELF-HOSTING.md)** for server packages, `-d` service mode,
LAN exposure, reverse-proxy notes, the full flag list, the API, VOACAP setup,
and security details.

## Building from source

See **[BUILDING.md](BUILDING.md)** for the Go build, the native desktop build,
the test suite, the release scripts, and the distro packaging.

## License

[MIT](LICENSE). Gateway data is © the Winlink Development Team / Amateur Radio
Safety Foundation; solar data © the respective providers (see in-app
attribution).
