//go:build desktop

package main

import (
	"context"
	"flag"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/app"
	"github.com/ben-kuhn/rms-radar/internal/cmsapi"
	"github.com/ben-kuhn/rms-radar/internal/propagation"
	"github.com/ben-kuhn/rms-radar/internal/voacapget"
	webview "github.com/webview/webview_go"
)

// Native-window build. Requires CGo and the platform webview runtime:
// WebKitGTK 4.1 (Linux), WebView2 (Windows), WKWebView (macOS). The window
// hosts the same local server and UI as the web binary; closing it exits.
func main() {
	var (
		addr         = flag.String("listen", envOr("RMSRADAR_LISTEN", "127.0.0.1:0"), "local listen address (default: an ephemeral localhost port)")
		serviceCodes = flag.String("service-codes", envOr("RMSRADAR_SERVICE_CODES", "PUBLIC,EMCOMM"), "comma-separated Winlink service codes to fetch (the API accepts explicit codes; there is no wildcard)")
		refresh      = flag.Duration("refresh", envDuration("RMSRADAR_REFRESH", 1*time.Hour), "gateway list refresh interval")
		cacheDir     = flag.String("cache-dir", envOr("RMSRADAR_CACHE_DIR", defaultDesktopCacheDir()), "cache directory for gateway list, tiles, and logs")
		accessKey    = flag.String("key", envOr("RMSRADAR_KEY", cmsapi.DefaultAccessKey), "Winlink CMS access key")
		historyHours = flag.Int("history-hours", envInt("RMSRADAR_HISTORY_HOURS", 48), "hours of channel history to fetch (1-48)")
		voacapURL    = flag.String("voacap-url", envOr("RMSRADAR_VOACAP_URL", ""), "optional remote VOACAP API base URL (enables forecasts when set)")
		voacapExec   = flag.String("voacap-executable", envOr("RMSRADAR_VOACAP_EXECUTABLE", ""), "path to a local voacapl/voacapw binary (enables forecasts)")
		voacapData   = flag.String("voacap-data-dir", envOr("RMSRADAR_VOACAP_DATA_DIR", ""), "ITSHFBC data directory for the local VOACAP binary")
		voacapDlURL  = flag.String("voacap-download-url", envOr("RMSRADAR_VOACAP_DOWNLOAD_URL", ""), "download a VOACAP distribution (binary + itshfbc data) from this URL (opt-in; no package manager required)")
		voacapDlDir  = flag.String("voacap-download-dir", envOr("RMSRADAR_VOACAP_DOWNLOAD_DIR", defaultVOACAPDownloadDir()), "directory to extract a downloaded VOACAP distribution into")
		voacapDlSum  = flag.String("voacap-download-sha256", envOr("RMSRADAR_VOACAP_DOWNLOAD_SHA256", ""), "optional SHA-256 of the VOACAP distribution archive")
		webDir       = flag.String("web-dir", envOr("RMSRADAR_WEB_DIR", ""), "serve web assets from this directory instead of the embedded copy")
		tileURL      = flag.String("tile-url", envOr("RMSRADAR_TILE_URL", "https://tile.openstreetmap.org/{z}/{x}/{y}.png"), "upstream tile URL template proxied+cached by the server (OSM by default)")
		tileCacheMax = flag.Int("tile-cache-max", envInt("RMSRADAR_TILE_CACHE_MAX_MB", 512), "tile cache quota in MiB (set a very large value to disable eviction)")
		vhfRange     = flag.Float64("vhf-max-range", envFloat("RMSRADAR_VHF_MAX_RANGE", 400), "max plausible VHF/UHF (>=30 MHz) path length in km for ranking (distance-based; set large to effectively disable)")
		debug        = flag.Bool("debug", envBool("RMSRADAR_DEBUG", false), "enable the webview developer tools")
	)
	flag.Parse()

	// Native builds have no console on Windows, so also write logs to the cache.
	if err := os.MkdirAll(*cacheDir, 0o755); err == nil {
		logPath := filepath.Join(*cacheDir, "rms-radar.log")
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
			log.SetOutput(teeWriter{os.Stderr, f})
			defer f.Close()
		}
	}

	// Optional: bootstrap a VOACAP engine (macOS/Windows have no package manager).
	if *voacapDlURL != "" && *voacapExec == "" {
		dlCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		exe, dataDir, err := voacapget.Fetch(dlCtx, *voacapDlURL, *voacapDlDir, *voacapDlSum)
		cancel()
		if err != nil {
			log.Printf("voacapl download failed: %v", err)
		} else {
			*voacapExec = exe
			*voacapData = dataDir
			log.Printf("voacapl ready: %s (data %s)", exe, dataDir)
		}
	}

	if *voacapExec == "" {
		if exe, data := propagation.Detect(); exe != "" {
			*voacapExec = exe
			if *voacapData == "" {
				*voacapData = data
			}
			log.Printf("voacapl: detected %s (data %s)", exe, data)
		}
	}

	codes := splitCSV(*serviceCodes)
	log.Printf("RMS Radar desktop %s | service codes: %v", version, codes)

	webFSys, err := webFilesystem(*webDir)
	if err != nil {
		log.Fatalf("web: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	a, err := app.Start(ctx, app.Config{
		Version:           version,
		Listen:            *addr,
		ServiceCodes:      codes,
		HistoryHours:      *historyHours,
		CacheDir:          *cacheDir,
		AccessKey:         *accessKey,
		Refresh:           *refresh,
		VOACAPURL:         *voacapURL,
		VOACAPExecutable:  *voacapExec,
		VOACAPDataDir:     *voacapData,
		TileURL:           *tileURL,
		TileCacheMaxBytes: int64(*tileCacheMax) << 20,
		VHFMaxRangeKm:     *vhfRange,
		WebFS:             webFSys,
	})
	if err != nil {
		log.Fatalf("start: %v", err)
	}

	w := webview.New(*debug)
	if w == nil {
		a.Close()
		log.Fatal("webview: failed to create a window (is WebView2/WebKitGTK installed?)")
	}
	defer w.Destroy()
	w.SetTitle("Winlink Gateway Map")
	w.SetSize(1200, 820, webview.HintNone)
	w.Navigate(a.URL())
	log.Printf("window: %s", a.URL())
	w.Run()
	a.Close()
}

func defaultDesktopCacheDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "rms-radar")
	}
	return "data"
}

// teeWriter keeps logging to the console when there is one, but never lets a
// failing stderr (Windows GUI builds have none) drop the file log.
type teeWriter struct{ a, b io.Writer }

func (t teeWriter) Write(p []byte) (int, error) {
	_, _ = t.a.Write(p)
	return t.b.Write(p)
}
