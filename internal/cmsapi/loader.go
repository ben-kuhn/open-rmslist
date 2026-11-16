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
	codes        []string
	historyHours int

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
	name := "gateway_status"
	if !(len(l.codes) == 1 && l.codes[0] == "PUBLIC") {
		name += "-" + strings.Join(l.codes, "-")
	}
	return filepath.Join(l.cacheDir, name+".json")
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
	if !force {
		if b, err := os.ReadFile(l.cachePath()); err == nil && len(b) > 0 {
			l.set(b, SourceCache, time.Now())
			return nil
		}
	}

	b, fetchErr := l.client.Fetch(ctx, l.codes, l.historyHours)
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
