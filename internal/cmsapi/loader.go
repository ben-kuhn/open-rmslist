package cmsapi

import (
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed gateway_status.json.gz
var embeddedGatewayStatus []byte

// Source describes where the current gateway data came from.
type Source int

const (
	SourceNetwork Source = iota
	SourceCache
	SourceEmbedded
)

func (s Source) String() string {
	switch s {
	case SourceNetwork:
		return "network"
	case SourceCache:
		return "cache"
	default:
		return "embedded"
	}
}

// Loader refreshes, caches, and serves the gateway status feed.
type Loader struct {
	client       *Client
	cacheDir     string
	historyHours int

	codesMu sync.RWMutex
	codes   []string

	refreshMu sync.Mutex

	mu        sync.RWMutex
	data      []byte
	source    Source
	fetchedAt time.Time
}

func NewLoader(cacheDir string, codes []string, historyHours int, key string) *Loader {
	if len(codes) == 0 {
		codes = []string{"PUBLIC"}
	}
	return &Loader{
		client:       NewClient(key),
		cacheDir:     cacheDir,
		codes:        codes,
		historyHours: historyHours,
	}
}

func (l *Loader) cachePath() string {
	codes := l.ServiceCodes()
	name := "gateway_status"
	if !(len(codes) == 1 && codes[0] == "PUBLIC") {
		name += "-" + strings.Join(codes, "-")
	}
	return filepath.Join(l.cacheDir, name+".json")
}

// ServiceCodes returns a copy of the service codes currently requested.
func (l *Loader) ServiceCodes() []string {
	l.codesMu.RLock()
	defer l.codesMu.RUnlock()
	return append([]string(nil), l.codes...)
}

// SetServiceCodes changes the requested service codes. The next Refresh (or
// Seed) uses the new cache key and network query.
func (l *Loader) SetServiceCodes(codes []string) {
	if len(codes) == 0 {
		codes = []string{"PUBLIC"}
	}
	l.codesMu.Lock()
	defer l.codesMu.Unlock()
	l.codes = append([]string(nil), codes...)
}

// Seed loads data from the on-disk cache, or the embedded snapshot if there is
// no cache. It never touches the network, so it is safe to call at startup
// before serving. The caller can then kick off a forced network refresh in the
// background.
func (l *Loader) Seed() {
	if b, err := os.ReadFile(l.cachePath()); err == nil && len(b) > 0 {
		l.set(b, SourceCache, time.Now())
		return
	}
	if b, err := l.embedded(); err == nil {
		l.set(b, SourceEmbedded, time.Now())
	}
}

// Refresh loads data preferring cache, then the network, then an embedded
// snapshot. When force is true the cache is skipped and the network is tried
// first (falling back to the embedded snapshot only on failure).
func (l *Loader) Refresh(ctx context.Context, force bool) error {
	// Serialize network refreshes so a UI-triggered refresh cannot race the
	// background ticker.
	l.refreshMu.Lock()
	defer l.refreshMu.Unlock()

	if !force {
		if b, err := os.ReadFile(l.cachePath()); err == nil && len(b) > 0 {
			l.set(b, SourceCache, time.Now())
			return nil
		}
	}

	b, fetchErr := l.client.Fetch(ctx, l.ServiceCodes(), l.historyHours)
	if fetchErr == nil {
		if l.cacheDir != "" {
			if err := os.MkdirAll(l.cacheDir, 0o755); err == nil {
				_ = os.WriteFile(l.cachePath(), b, 0o644)
			}
		}
		l.set(b, SourceNetwork, time.Now())
		return nil
	}

	if b, err := l.embedded(); err == nil {
		l.set(b, SourceEmbedded, time.Now())
		return nil
	}
	return fmt.Errorf("gateway fetch failed: %w", fetchErr)
}

func (l *Loader) embedded() ([]byte, error) {
	if len(embeddedGatewayStatus) == 0 {
		return nil, errors.New("no embedded gateway status")
	}
	zr, err := gzip.NewReader(bytes.NewReader(embeddedGatewayStatus))
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	return io.ReadAll(zr)
}

func (l *Loader) set(b []byte, src Source, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.data = b
	l.source = src
	l.fetchedAt = at
}

// Snapshot returns the current data, its source, and when it was loaded.
func (l *Loader) Snapshot() ([]byte, Source, time.Time) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.data, l.source, l.fetchedAt
}
