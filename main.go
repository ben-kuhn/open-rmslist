package main

import (
	"context"
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ben-kuhn/open-rmslist/internal/browser"
	"github.com/ben-kuhn/open-rmslist/internal/catalog"
	"github.com/ben-kuhn/open-rmslist/internal/cmsapi"
	"github.com/ben-kuhn/open-rmslist/internal/propagation"
	"github.com/ben-kuhn/open-rmslist/internal/server"
	"github.com/ben-kuhn/open-rmslist/internal/voacapget"
)

// version is overridable at link time via -ldflags "-X main.version=...".
var version = "0.1.1"

//go:embed all:web
var webFS embed.FS

func main() {
	var (
		addr         = flag.String("listen", envOr("OPENRMSLIST_LISTEN", "127.0.0.1:8080"), "HTTP listen address (use 0.0.0.0:8080 to serve the LAN)")
		serviceCodes = flag.String("service-codes", envOr("OPENRMSLIST_SERVICE_CODES", "PUBLIC,MERCURY,VardeFM"), "comma-separated Winlink service codes to fetch")
		refresh      = flag.Duration("refresh", envDuration("OPENRMSLIST_REFRESH", 15*time.Minute), "gateway list refresh interval")
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
		openBrowser  = flag.Bool("open-browser", envBool("OPENRMSLIST_OPEN_BROWSER", defaultOpenBrowser()), "open the web UI in the default browser on start")
	)
	flag.Parse()

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
	log.Printf("open-rmslist %s | service codes: %v", version, codes)

	webFSys, err := webFilesystem(*webDir)
	if err != nil {
		log.Fatalf("web: %v", err)
	}

	loader := cmsapi.NewLoader(*cacheDir, codes, *historyHours, *accessKey)
	heur := propagation.NewHeuristic(*cacheDir)
	srv := server.New(server.Config{
		Version:          version,
		ServiceCodes:     codes,
		HistoryHours:     *historyHours,
		ModeSwaps:        catalog.DefaultModeSwaps,
		VOACAPURL:        *voacapURL,
		VOACAPExecutable: *voacapExec,
		VOACAPDataDir:    *voacapData,
	}, webFSys, loader, heur)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

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
		WriteTimeout:      5 * time.Minute, // accommodates a full VOACAP rank
		IdleTimeout:       120 * time.Second,
	}

	if *openBrowser {
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := browser.Open(browserURL(*addr)); err != nil {
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
		host = ":8080"
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
	// Auto-open on desktop platforms by default; headless Unix stays quiet.
	return runtime.GOOS == "windows" || runtime.GOOS == "darwin"
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
