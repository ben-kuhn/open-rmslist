// Package app wires the loader, propagation conditions, HTTP server, and
// background refresh loop together. Both the web/service binary and the
// native-window desktop binary start from here.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/cmsapi"
	"github.com/ben-kuhn/rms-radar/internal/propagation"
	"github.com/ben-kuhn/rms-radar/internal/server"
)

// Config is the shared startup configuration.
type Config struct {
	Version           string
	Listen            string
	ServiceCodes      []string
	HistoryHours      int
	CacheDir          string
	AccessKey         string
	Refresh           time.Duration
	VOACAPURL         string
	VOACAPExecutable  string
	VOACAPDataDir     string
	TileURL           string
	TileCacheMaxBytes int64
	VHFMaxRangeKm     float64
	WebFS             fs.FS

	// DesktopToken enables the browser desktop lifecycle (web binary only). The
	// native-window binary leaves it empty and quits when its window closes.
	DesktopToken string
}

// App is a running instance.
type App struct {
	loader *cmsapi.Loader
	heur   *propagation.Heuristic
	srv    *server.Server
	http   *http.Server
	ln     net.Listener
	cancel context.CancelFunc
	done   chan struct{}
}

// Start builds the catalog, seeds it from cache/embedded data, starts the
// refresh loop and the HTTP listener, and returns once the listener is up.
func Start(parent context.Context, cfg Config) (*App, error) {
	if cfg.Refresh <= 0 {
		return nil, fmt.Errorf("refresh interval must be positive (got %s)", cfg.Refresh)
	}
	ctx, cancel := context.WithCancel(parent)
	a := &App{cancel: cancel, done: make(chan struct{})}

	tileCache := ""
	serviceCodesPath := ""
	if cfg.CacheDir != "" {
		tileCache = filepath.Join(cfg.CacheDir, "tiles")
		serviceCodesPath = filepath.Join(cfg.CacheDir, "service_codes.json")
	}
	codes := serviceCodesFromDisk(serviceCodesPath, cfg.ServiceCodes)
	a.loader = cmsapi.NewLoader(cfg.CacheDir, codes, cfg.HistoryHours, cfg.AccessKey)
	a.heur = propagation.NewHeuristic(cfg.CacheDir)
	a.srv = server.New(server.Config{
		Version:           cfg.Version,
		ServiceCodes:      codes,
		HistoryHours:      cfg.HistoryHours,
		VOACAPURL:         cfg.VOACAPURL,
		VOACAPExecutable:  cfg.VOACAPExecutable,
		VOACAPDataDir:     cfg.VOACAPDataDir,
		TileURL:           cfg.TileURL,
		TileCacheDir:      tileCache,
		TileCacheMaxBytes: cfg.TileCacheMaxBytes,
		VHFMaxRangeKm:     cfg.VHFMaxRangeKm,
		ServiceCodesPath:  serviceCodesPath,
		DesktopToken:      cfg.DesktopToken,
		OnQuit:            cancel,
	}, cfg.WebFS, a.loader, a.heur)

	// Seed immediately from cache or the embedded snapshot (never blocks on the
	// network), then refresh from the network in the background.
	a.loader.Seed()
	if err := a.srv.Refresh(ctx); err != nil {
		log.Printf("warning: parse gateways: %v", err)
	}

	go func() {
		a.heur.RefreshConditions(ctx)
		if err := a.loader.Refresh(ctx, true); err == nil {
			_ = a.srv.Refresh(ctx)
		}
		ticker := time.NewTicker(cfg.Refresh)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				a.heur.RefreshConditions(ctx)
				if err := a.loader.Refresh(ctx, true); err != nil {
					log.Printf("refresh: %v", err)
				} else if err := a.srv.Refresh(ctx); err != nil {
					log.Printf("parse: %v", err)
				}
			case <-ctx.Done():
				return
			}
		}
	}()

	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		cancel()
		return nil, err
	}
	a.ln = ln
	a.http = &http.Server{
		Handler:           a.srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Strictly greater than any rank deadline, so a long rank's response
		// is never aborted mid-write.
		WriteTimeout: server.RankTimeoutVOACAP + 2*time.Minute,
		IdleTimeout:  120 * time.Second,
	}
	go func() {
		if err := a.http.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("http server: %v", err)
			cancel()
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = a.http.Shutdown(shutdownCtx)
		close(a.done)
	}()
	return a, nil
}

// serviceCodesFromDisk uses the persisted runtime list when present, otherwise
// the configured seed list.
func serviceCodesFromDisk(path string, fallback []string) []string {
	if path == "" {
		return fallback
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return fallback
	}
	var codes []string
	if json.Unmarshal(b, &codes) != nil || len(codes) == 0 {
		return fallback
	}
	return codes
}

// URL returns a browser-friendly base URL for the listener. A :0 listen
// address reports its actual port; wildcard binds are shown as 127.0.0.1.
func (a *App) URL() string {
	host, port, err := net.SplitHostPort(a.ln.Addr().String())
	if err != nil {
		return "http://" + a.ln.Addr().String()
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port)
}

// Wait blocks until the HTTP server has shut down.
func (a *App) Wait() { <-a.done }

// Close stops the app and waits for the HTTP server to shut down.
func (a *App) Close() {
	a.cancel()
	<-a.done
}
