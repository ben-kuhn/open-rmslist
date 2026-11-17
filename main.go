//go:build !desktop

package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ben-kuhn/open-rmslist/internal/app"
	"github.com/ben-kuhn/open-rmslist/internal/browser"
	"github.com/ben-kuhn/open-rmslist/internal/cmsapi"
	"github.com/ben-kuhn/open-rmslist/internal/voacapget"
)

// Web/service binary. The native-window build lives in desktop.go behind the
// "desktop" build tag, so the default build stays CGO-free.
func main() {
	var (
		addr         = flag.String("listen", envOr("OPENRMSLIST_LISTEN", "127.0.0.1:8079"), "HTTP listen address (use 0.0.0.0:8079 to serve the LAN)")
		serviceCodes = flag.String("service-codes", envOr("OPENRMSLIST_SERVICE_CODES", "PUBLIC,EMCOMM,MERCURY,VARDEFM"), "comma-separated Winlink service codes to fetch (the API accepts explicit codes; there is no wildcard)")
		refresh      = flag.Duration("refresh", envDuration("OPENRMSLIST_REFRESH", 1*time.Hour), "gateway list refresh interval")
		cacheDir     = flag.String("cache-dir", envOr("OPENRMSLIST_CACHE_DIR", "./data"), "cache directory for gateway list and propagation data")
		accessKey    = flag.String("key", envOr("OPENRMSLIST_KEY", cmsapi.DefaultAccessKey), "Winlink CMS access key")
		historyHours = flag.Int("history-hours", envInt("OPENRMSLIST_HISTORY_HOURS", 48), "hours of channel history to fetch (1-48)")
		voacapURL    = flag.String("voacap-url", envOr("OPENRMSLIST_VOACAP_URL", ""), "optional remote VOACAP API base URL (enables forecasts when set)")
		voacapExec   = flag.String("voacap-executable", envOr("OPENRMSLIST_VOACAP_EXECUTABLE", ""), "path to a local voacapl/voacapw binary (enables forecasts)")
		voacapData   = flag.String("voacap-data-dir", envOr("OPENRMSLIST_VOACAP_DATA_DIR", ""), "ITSHFBC data directory for the local VOACAP binary")
		voacapDlURL  = flag.String("voacap-download-url", envOr("OPENRMSLIST_VOACAP_DOWNLOAD_URL", ""), "download a VOACAP distribution (binary + itshfbc data) from this URL (opt-in; no package manager required)")
		voacapDlDir  = flag.String("voacap-download-dir", envOr("OPENRMSLIST_VOACAP_DOWNLOAD_DIR", defaultVOACAPDownloadDir()), "directory to extract a downloaded VOACAP distribution into")
		voacapDlSum  = flag.String("voacap-download-sha256", envOr("OPENRMSLIST_VOACAP_DOWNLOAD_SHA256", ""), "optional SHA-256 of the VOACAP distribution archive")
		webDir       = flag.String("web-dir", envOr("OPENRMSLIST_WEB_DIR", ""), "serve web assets from this directory instead of the embedded copy")
		tileURL      = flag.String("tile-url", envOr("OPENRMSLIST_TILE_URL", "https://tile.openstreetmap.org/{z}/{x}/{y}.png"), "upstream tile URL template proxied+cached by the server (OSM by default)")
		vhfRange     = flag.Float64("vhf-max-range", envFloat("OPENRMSLIST_VHF_MAX_RANGE", 400), "max plausible VHF/UHF (>=30 MHz) path length in km for ranking (distance-based; set large to effectively disable)")
		daemonShort  = flag.Bool("d", envBool("OPENRMSLIST_DAEMON", false), "run as a background service (no browser, no auto-exit)")
		daemonLong   = flag.Bool("daemon", envBool("OPENRMSLIST_DAEMON", false), "run as a background service (alias for -d)")
		openBrowser  = flag.Bool("open-browser", envBool("OPENRMSLIST_OPEN_BROWSER", defaultOpenBrowser()), "open the web UI in the default browser on start")
	)
	flag.Parse()
	isDaemon := *daemonShort || *daemonLong
	if isDaemon {
		*openBrowser = false
	}

	// Optional: bootstrap a VOACAP engine for platforms without package
	// management (macOS, Windows). Only runs when a URL is explicitly set.
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

	codes := splitCSV(*serviceCodes)
	mode := "desktop"
	if isDaemon {
		mode = "daemon"
	}
	log.Printf("open-rmslist-web %s | %s mode | service codes: %v", version, mode, codes)

	webFSys, err := webFilesystem(*webDir)
	if err != nil {
		log.Fatalf("web: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	desktopToken := ""
	if !isDaemon {
		desktopToken = randomHex(16)
	}

	a, err := app.Start(ctx, app.Config{
		Version:          version,
		Listen:           *addr,
		ServiceCodes:     codes,
		HistoryHours:     *historyHours,
		CacheDir:         *cacheDir,
		AccessKey:        *accessKey,
		Refresh:          *refresh,
		VOACAPURL:        *voacapURL,
		VOACAPExecutable: *voacapExec,
		VOACAPDataDir:    *voacapData,
		TileURL:          *tileURL,
		VHFMaxRangeKm:    *vhfRange,
		WebFS:            webFSys,
		DesktopToken:     desktopToken,
	})
	if err != nil {
		log.Fatal(err)
	}

	if *openBrowser {
		pageURL := a.URL()
		if desktopToken != "" {
			pageURL += "/?desktop=" + desktopToken
		}
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := browser.Open(pageURL); err != nil {
				log.Printf("open browser: %v", err)
			}
		}()
	}

	log.Printf("listening on %s", a.URL())
	a.Wait()
}

func defaultOpenBrowser() bool {
	// Desktop (double-click/browser) mode is the default; -d serves headless.
	return true
}
