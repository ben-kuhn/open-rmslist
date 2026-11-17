package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ben-kuhn/open-rmslist/internal/browser"
	"github.com/ben-kuhn/open-rmslist/internal/cmsapi"
	"github.com/ben-kuhn/open-rmslist/internal/propagation"
	"github.com/ben-kuhn/open-rmslist/internal/server"
	"github.com/ben-kuhn/open-rmslist/internal/voacapget"
)

// version is overridable at link time via -ldflags "-X main.version=...".
var version = "0.1.2"

//go:embed all:web
var webFS embed.FS

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
	log.Printf("open-rmslist %s | %s mode | service codes: %v", version, mode, codes)

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

	loader := cmsapi.NewLoader(*cacheDir, codes, *historyHours, *accessKey)
	heur := propagation.NewHeuristic(*cacheDir)
	srv := server.New(server.Config{
		Version:          version,
		ServiceCodes:     codes,
		HistoryHours:     *historyHours,
		VOACAPURL:        *voacapURL,
		VOACAPExecutable: *voacapExec,
		VOACAPDataDir:    *voacapData,
		TileURL:          *tileURL,
		TileCacheDir:     filepath.Join(*cacheDir, "tiles"),
		VHFMaxRangeKm:    *vhfRange,
		DesktopToken:     desktopToken,
		OnQuit:           stop,
	}, webFSys, loader, heur)

	// Seed immediately from cache or the embedded snapshot (never blocks on the
	// network), then refresh from the network in the background.
	loader.Seed()
	if err := srv.Refresh(ctx); err != nil {
		log.Printf("warning: parse gateways: %v", err)
	}

	// Background: fetch fresh propagation conditions, then refresh the gateway
	// list (immediately once, then on the configured interval).
	go func() {
		heur.RefreshConditions(ctx)
		if err := loader.Refresh(ctx, true); err == nil {
			_ = srv.Refresh(ctx)
		}
		ticker := time.NewTicker(*refresh)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				heur.RefreshConditions(ctx)
				if err := loader.Refresh(ctx, true); err != nil {
					log.Printf("refresh: %v", err)
				} else if err := srv.Refresh(ctx); err != nil {
					log.Printf("parse: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Strictly greater than any rank deadline, so a long rank's response
		// is never aborted mid-write.
		WriteTimeout: server.RankTimeoutVOACAP + 2*time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	if *openBrowser {
		pageURL := browserURL(*addr)
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

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutdownCtx)
	}()

	log.Printf("listening on %s", *addr)
	if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func webFilesystem(dir string) (fs.FS, error) {
	if dir != "" {
		return os.DirFS(dir), nil
	}
	return fs.Sub(webFS, "web")
}

func browserURL(addr string) string {
	host := addr
	if host == "" {
		host = ":8079"
	}
	switch {
	case strings.HasPrefix(host, ":"):
		return "http://localhost" + host
	case strings.HasPrefix(host, "0.0.0.0"):
		return "http://localhost" + strings.TrimPrefix(host, "0.0.0.0")
	case strings.HasPrefix(host, "::"):
		return "http://localhost" + strings.TrimPrefix(host, "::")
	default:
		return "http://" + host
	}
}

func defaultOpenBrowser() bool {
	// Desktop (double-click) mode is the default everywhere; -d serves headless.
	return true
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
}

func defaultVOACAPDownloadDir() string {
	if dir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(dir, "open-rmslist", "voacap")
	}
	return filepath.Join(".", "data", "voacap")
}

func splitCSV(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func envOr(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v, ok := os.LookupEnv(key); ok {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envFloat(key string, def float64) float64 {
	if v, ok := os.LookupEnv(key); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v, ok := os.LookupEnv(key); ok {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func envDuration(key string, def time.Duration) time.Duration {
	if v, ok := os.LookupEnv(key); ok {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return def
}
