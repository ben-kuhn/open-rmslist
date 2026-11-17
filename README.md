# open-rmslist

A minimal, secure, self-hostable web map of Winlink RMS gateways — built for
the community of operators running non-VARA modems (Mercury, VardeFM, and
friends) under Winlink **service codes**.

It periodically refreshes the live Winlink gateway list, maps stations on
OpenStreetMap, lets you filter by service code / band / mode, and ranks
stations by predicted link success using current propagation conditions.

- **Dark mode (default) and light mode**
- **Mobile-friendly** — on narrow screens, Map/List tabs keep the map visible and the gateway list scrolls in its own pane
- **Service-code dropdown** populated from the fetched gateway list
- **Service-code aware** — gateways are shown and filtered by the Winlink
  service code the API returns (`PUBLIC`, `EMCOMM`, `MERCURY`, `VARDEFM`, …);
  community-modem channels are labelled with the service name (Mercury,
  VardeFM), while other codes show the reported mode
- **Client setup guide** — a static page explains how to add service codes in
  Winlink Express, PAT, WoAD, RadioMail, and HTCommander
- **Propagation ranking** — pick your grid, band, and mode; stations are
  scored and highlighted by predicted link quality. While ranking, 0% VHF/UHF
  paths are hidden as implausible. HF stations are kept regardless of score.
- **Offline-tolerant** — caches the gateway list and propagation data, with an
  embedded snapshot fallback

---

## Why

The Winlink gateway list advertises community modems by their *VARA-compatible*
API: a Mercury HF modem is listed as `VARA`, a VardeFM modem as `VARA FM`, and
the real identity is carried in the service code (`MERCURY`, `VARDEFM`, …).
Rather than matching mode strings, `open-rmslist` uses the service code: a
channel whose service code names a community modem is shown under that name
(Mercury, VardeFM), while everything else keeps the advertised mode. The raw
API mode stays available as a hover hint and in the `raw_mode` field.

## Data source

The gateway list comes from the same public endpoint the open-source
[Pat](https://github.com/la5nta/pat) Winlink client uses:

This endpoint returns per-gateway latitude/longitude and per-channel
`ServiceCode`, `SupportedModes`, frequency, and grid square.

## Quick start

Requires Go 1.23+ (no third-party Go dependencies; the web UI is embedded).

```bash
go build -o open-rmslist .
./open-rmslist                 # serves on http://localhost:8079
```

Your default browser opens automatically on macOS and Windows. On Linux/BSD,
open <http://localhost:8079> yourself (or pass `-open-browser`).

### Configuration

| Flag | Env | Default | Description |
|------|-----|---------|-------------|
| `-listen` | `OPENRMSLIST_LISTEN` | `127.0.0.1:8079` | HTTP listen address (use `0.0.0.0:8079` to serve the LAN) |
| `-service-codes` | `OPENRMSLIST_SERVICE_CODES` | `PUBLIC,EMCOMM,MERCURY,VARDEFM` | Comma-separated service codes to fetch (no wildcard) |
| `-refresh` | `OPENRMSLIST_REFRESH` | `1h` | Gateway list refresh interval |
| `-cache-dir` | `OPENRMSLIST_CACHE_DIR` | `./data` | Cache directory |
| `-key` | `OPENRMSLIST_KEY` | Pat's community key | Winlink CMS access key |
| `-history-hours` | `OPENRMSLIST_HISTORY_HOURS` | `48` | Channel history (1–48) |
| `-voacap-url` | `OPENRMSLIST_VOACAP_URL` | *(empty)* | Optional remote VOACAP API base URL (enables forecasts) |
| `-voacap-executable` | `OPENRMSLIST_VOACAP_EXECUTABLE` | *(empty)* | Path to a local `voacapl`/`voacapw` binary (enables forecasts) |
| `-voacap-data-dir` | `OPENRMSLIST_VOACAP_DATA_DIR` | *(auto)* | ITSHFBC data directory for the local VOACAP binary |
| `-voacap-download-url` | `OPENRMSLIST_VOACAP_DOWNLOAD_URL` | *(empty)* | Download a VOACAP distribution (binary + data) from this URL |
| `-voacap-download-dir` | `OPENRMSLIST_VOACAP_DOWNLOAD_DIR` | *user cache* | Extract directory for the downloaded VOACAP distribution |
| `-voacap-download-sha256` | `OPENRMSLIST_VOACAP_DOWNLOAD_SHA256` | *(empty)* | Optional SHA-256 of the distribution archive |
| `-web-dir` | `OPENRMSLIST_WEB_DIR` | *(embedded)* | Serve web assets from a directory instead |
| `-tile-url` | `OPENRMSLIST_TILE_URL` | `https://tile.openstreetmap.org/{z}/{x}/{y}.png` | Upstream tile template; proxied and disk-cached by the server |
| `-vhf-max-range` | `OPENRMSLIST_VHF_MAX_RANGE` | `400` | Max plausible VHF/UHF path (km) for ranking — distance-based, not continent-based; set large to disable |
| `-open-browser` | `OPENRMSLIST_OPEN_BROWSER` | macOS/Windows: `true`, else `false` | Open the browser on start |

### Service codes

The service-code dropdown is built from the codes present in the fetched data,
and the mode dropdown shows the modes those channels advertise. Community-modem
codes are labelled with the service name: `MERCURY` channels show as **Mercury**
and `VARDEFM` channels as **VardeFM**, even though the API advertises them as
`VARA`. Other codes (`PUBLIC`, `EMCOMM`, private groups) keep the reported mode.
The API's raw mode is preserved in `raw_mode` and shown as a hover hint.

The Winlink API has no wildcard for service codes, so `-service-codes` lists the
codes to request. The default is the set the API currently serves:

| Code | Notes |
|------|-------|
| `PUBLIC` | maritime and open amateur gateways |
| `EMCOMM` | emergency gateways that do not allow public access |
| `MERCURY` | Mercury HF gateways |
| `VARDEFM` | VardeFM gateways |

Add any private group code you are entitled to with
`-service-codes PUBLIC,MYGROUP` (or `OPENRMSLIST_SERVICE_CODES`); codes with no
gateways simply contribute nothing.

A per-client setup guide is served at `/clients.html` (source:
`web/clients.html`), covering Winlink Express, PAT, WoAD, RadioMail, and
HTCommander.

## Propagation

`open-rmslist` estimates link success two ways, preferring the more accurate
one and falling back gracefully:

1. **Heuristic (always available)** — real-time ionosonde `foF2`/MUF from
   [prop.kc2g.com](https://prop.kc2g.com) plus solar indices (SFI/A/K) from
   [hamqsl.com](https://www.hamqsl.com), combined with great-circle distance
   and the gateway's advertised radio range. The last-known conditions are
   cached to disk, so ranking still works offline (marked as stale).
2. **VOACAP (optional, point-to-point forecast)** — a port of Pat's
   MIT-licensed VOACAP encoder/parser and Link Quality Index (LQI) scoring. Two
   engines are supported: a **remote VOACAP API** (`-voacap-url`) or a **local
   `voacapl`/`voacapw` binary with ITSHFBC data** (`-voacap-executable` /
   `-voacap-data-dir`). The LQI combines path reliability, SNR, takeoff angle,
   MUFday, propagation mode (hops/layer), and signal power into a 0–100 score.
   The smoothed sunspot number (SSN) input comes from the current hamqsl daily
   sunspot count (Pat uses a SILSO smoothed-SSN forecast instead). The ranker
   prefers VOACAP when configured and automatically falls back to the
   heuristic when it is unavailable or errors.

Close-in HF paths (a few to a few dozen km) are groundwave/NVIS, not long-haul
skywave, so skywave statistics such as MUFday under-report them. Both
predictors apply a band-aware groundwave/NVIS floor for these distances: low
bands (80/60/40 m) score in the high 90s, while high bands (20 m and up) stay
low because their groundwave is weak and they are in the skip zone.

The rank endpoint (`POST /api/rank`) accepts `{grid, band, mode, service_code}`
and returns stations sorted by predicted link quality, with distance, bearing,
band-open status, and MUF. When ranking across multiple bands or modes, each
station row shows the band and mode that produced its best score, and that
channel is highlighted.

## Installing VOACAP (optional)

Point-to-point forecasts use an external VOACAP engine. `open-rmslist` is MIT;
the engine it shells out to is a separate program with its own permissive
terms: the core VOACAP prediction code is U.S. Government public domain
(NTIA/ITS), the `voacapl` Linux port (J.A. Watson) is CC0, and its `itshfbc`
coefficient data is freely redistributable (Debian ships it as `voacapl-data`).
Because it is exec'd rather than linked, there is no combined-work license
question.

Install it with your package manager, then either let `open-rmslist` find it on
`PATH` or point at it explicitly:

- **Debian/Ubuntu:** `sudo apt install voacapl voacapl-data`
- **Arch:** install `voacapl` from the AUR
- **Fedora/NixOS:** build from [jawatson/voacapl](https://github.com/jawatson/voacapl)
  or use the `nix-ham-packages` `voacapl` package

```bash
./open-rmslist -voacap-executable "$(command -v voacapl)"
```

`-voacap-data-dir` is only needed if the `itshfbc` data lives somewhere
non-standard; on Linux it auto-detects a read-only data directory (voacapl runs
with `--run-dir`).

### Auto-downloading VOACAP (macOS / Windows)

On platforms without a package manager, `open-rmslist` can fetch and extract a
VOACAP distribution for you:

```bash
./open-rmslist -voacap-download-url https://example.com/voacap-macos.tar.gz
```

Point `-voacap-download-url` at a `.zip`, `.tar.gz`, `.tgz`, or `.tar`
containing the `voacapl`/`voacapw` binary and its `itshfbc` data. The archive is
extracted to `-voacap-download-dir`, the engine and data directory are located
automatically, and the resulting paths are used. Supply `-voacap-download-sha256`
to verify the archive before extracting.

This is strictly opt-in (nothing downloads unless you set a URL), and
downloading + executing a third-party binary is a trust decision you make by
configuring it.

## API

| Endpoint | Description |
|----------|-------------|
| `GET /api/health` | Service status, data source, station count |
| `GET /api/gateways` | Normalized stations + channels (cached) |
| `GET /api/servicecodes` | Distinct service codes present in the data |
| `GET /api/conditions` | Current solar + MUF summary |
| `POST /api/rank` | Ranked stations for a grid/band/mode |
| `GET /api/config` | Service codes, version, VOACAP status, tile URL |

## Platform support & packaging

First-class targets (CI builds all of them, static binaries via
`CGO_ENABLED=0`):

- **Linux** — `amd64`, `arm64`, `arm`
- **macOS** — `amd64`, `arm64`
- **Windows** — `amd64`, `arm64`
- **FreeBSD / OpenBSD / NetBSD** — `amd64` (build support)

Packaging included:

- **systemd unit** — `contrib/systemd/open-rmslist.service`
- **Arch** — `packaging/arch/PKGBUILD`
- **Nix / NixOS** — see the sibling
  [`nix-ham-packages`](https://github.com/ben-kuhn/nix-ham-packages) overlay
  (`pkgs.open-rmslist` + `services.open-rmslist` module)

### CI/CD

- `.github/workflows/ci.yml` — vet, test, and cross-compile every target on
  push/PR.
- `.github/workflows/release.yml` — on a `v*` tag, build and package all
  targets (`tar.gz`/`zip` + SHA-256 checksums) and publish a GitHub Release via
  `scripts/build-release.sh`.

## Offline behavior

1. The gateway list is cached to `-cache-dir`. If the network is unavailable on
   a later start, the cache is served.
2. If there is no cache, an embedded snapshot (bundled in the binary) is used.
3. Propagation conditions are cached too; ranking degrades to a
   distance/band-based estimate when no ionospheric data is available.

Map tiles are proxied and cached to disk by the server (`<cache-dir>/tiles`),
so previously viewed areas render offline; new areas require connectivity.

## Security & privacy

`open-rmslist` is designed as a single-user, self-hosted tool and has **no
authentication**. Treat it as a LAN/loopback service:

- **Binds to `127.0.0.1:8079` by default.** Pass `-listen 0.0.0.0:8079` (and
  put it behind a reverse proxy or VPN) only if you intend to expose it.
- **No auth, no cookies, no write endpoints.** The API is read-only; `/api/rank`
  only computes predictions. It is still a compute endpoint, so it is capped
  (500 candidates per request) and time-limited.
- **Security headers** (CSP, `X-Content-Type-Options: nosniff`,
  `X-Frame-Options`, `Referrer-Policy`) are set on every response, and all
  third-party gateway data is HTML-escaped before rendering.
- **Outbound only:** the service fetches the gateway list from
  `api.winlink.org` and propagation data from `prop.hamqsl.com` /
  `prop.kc2g.com`, over verified TLS. It makes no inbound connections.
- **VOACAP auto-download (`-voacap-download-url`) executes a third-party
  binary.** It is opt-in and off by default. Prefer your OS package manager; if
  you use the downloader, supply `-voacap-download-sha256` and trust the source
  you point it at. Archives are size-capped, and extraction rejects
  path-traversal entries.
- **Privacy:** gateway data (including the sysop `Comments` field, which may
  contain contact details) is the same data Winlink publishes publicly. The
  embedded fallback snapshot is a frozen copy of that public feed.

## License

[MIT](LICENSE). Gateway data is © the Winlink Development Team / Amateur Radio
Safety Foundation; solar data © the respective providers (see in-app
attribution).
