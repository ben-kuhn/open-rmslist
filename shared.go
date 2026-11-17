package main

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/voacapget"
)

// version is overridable at link time via -ldflags "-X main.version=...".
var version = "0.2.3"

//go:embed all:web
var webFS embed.FS

func webFilesystem(dir string) (fs.FS, error) {
	if dir != "" {
		return os.DirFS(dir), nil
	}
	return fs.Sub(webFS, "web")
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
		return filepath.Join(dir, "rms-radar", "voacap")
	}
	return filepath.Join(".", "data", "voacap")
}

// maybeAutoDownloadVOACAP fetches the signed release bundle for this platform
// when no engine was configured. Failures never block startup: they are logged
// and the heuristic predictor stays in use.
func maybeAutoDownloadVOACAP(enabled bool, destDir string) (string, string) {
	if !enabled {
		return "", ""
	}
	return downloadVOACAP(voacapget.DefaultURL(), destDir)
}

func downloadVOACAP(url, destDir string) (string, string) {
	if url == "" {
		return "", ""
	}
	// Reuse a previously verified install without touching the network. This
	// keeps first-run setup fast and makes the desktop app usable offline.
	if exe, data, err := voacapget.Find(destDir); err == nil && exe != "" && data != "" {
		if _, err := os.Stat(filepath.Join(destDir, ".voacap-sha256")); err == nil {
			log.Printf("voacapl: using existing install at %s", destDir)
			return exe, data
		}
	}

	log.Printf("voacapl: downloading %s", url)
	// Probe the (tiny) checksum sidecar with a short timeout so an offline
	// machine does not sit at a blank window for minutes.
	sumCtx, cancelSum := context.WithTimeout(context.Background(), 10*time.Second)
	sum, err := voacapget.RemoteSHA256(sumCtx, url+".sha256")
	cancelSum()
	if err != nil {
		log.Printf("voacapl: cannot verify download: %v", err)
		return "", ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	exe, data, err := voacapget.Fetch(ctx, url, destDir, sum)
	if err != nil {
		log.Printf("voacapl: download failed: %v", err)
		return "", ""
	}
	log.Printf("voacapl ready: %s (data %s)", exe, data)
	return exe, data
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
