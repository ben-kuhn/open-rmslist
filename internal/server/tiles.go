package server

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// proxyTilePath is the same-origin tile endpoint the browser uses; the server
// proxies and caches tiles from the configured upstream.
const proxyTilePath = "/tiles/{z}/{x}/{y}.png"

// defaultTileUpstream is a keyless OSM tile template.
const defaultTileUpstream = "https://tile.openstreetmap.org/{z}/{x}/{y}.png"

// handleTiles fetches a tile from the upstream once (with a proper
// User-Agent), caches it on disk, and serves it. The browser never talks to the
// tile server directly, which avoids browser-side 403s and lets previously
// viewed areas work offline.
func (s *Server) handleTiles(w http.ResponseWriter, r *http.Request) {
	z, x, y, ok := parseTilePath(r.URL.Path)
	if !ok || z < 0 || z > 19 {
		http.Error(w, "invalid tile path", http.StatusBadRequest)
		return
	}
	if n := 1 << z; x < 0 || x >= n || y < 0 || y >= n {
		http.Error(w, "invalid tile coordinates", http.StatusBadRequest)
		return
	}
	if !s.tileLimiter.allow(clientIP(r), time.Now()) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "tile rate limit exceeded", http.StatusTooManyRequests)
		return
	}

	cachePath := filepath.Join(s.cfg.TileCacheDir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
	if b, err := os.ReadFile(cachePath); err == nil && len(b) > 0 {
		serveTile(w, b)
		return
	}

	// Coalesce concurrent misses for the same tile: one request fetches, the
	// rest wait and read the cached result.
	s.tileMu.Lock()
	wait, busy := s.tileInFlight[cachePath]
	if !busy {
		wait = make(chan struct{})
		s.tileInFlight[cachePath] = wait
	}
	s.tileMu.Unlock()
	if busy {
		select {
		case <-wait:
		case <-r.Context().Done():
			http.Error(w, "request canceled", http.StatusRequestTimeout)
			return
		}
		if b, err := os.ReadFile(cachePath); err == nil && len(b) > 0 {
			serveTile(w, b)
			return
		}
		http.Error(w, "tile upstream error", http.StatusBadGateway)
		return
	}
	defer func() {
		s.tileMu.Lock()
		delete(s.tileInFlight, cachePath)
		close(wait)
		s.tileMu.Unlock()
	}()

	select {
	case s.tileSem <- struct{}{}:
		defer func() { <-s.tileSem }()
	case <-r.Context().Done():
		http.Error(w, "request canceled", http.StatusRequestTimeout)
		return
	}

	upstream := s.cfg.TileURL
	if upstream == "" {
		upstream = defaultTileUpstream
	}
	u := strings.NewReplacer(
		"{z}", strconv.Itoa(z),
		"{x}", strconv.Itoa(x),
		"{y}", strconv.Itoa(y),
		"{s}", "a",
		"{r}", "",
	).Replace(upstream)

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "tile upstream error", http.StatusBadGateway)
		return
	}
	req.Header.Set("User-Agent", "rms-radar/"+s.cfg.Version+" (self-hosted Winlink RMS map)")
	req.Header.Set("Accept", "image/png,image/*;q=0.8")
	resp, err := s.tileClient.Do(req)
	if err != nil {
		http.Error(w, "tile upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		http.Error(w, "tile upstream error", http.StatusBadGateway)
		return
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil || len(b) == 0 {
		http.Error(w, "tile upstream error", http.StatusBadGateway)
		return
	}
	if s.cfg.TileCacheDir != "" {
		_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
		if tmp, err := os.CreateTemp(filepath.Dir(cachePath), ".tile-*"); err == nil {
			name := tmp.Name()
			_, werr := tmp.Write(b)
			cerr := tmp.Close()
			if werr == nil && cerr == nil && os.Rename(name, cachePath) == nil {
				s.tileCacheAdd(len(b))
			} else {
				_ = os.Remove(name)
			}
		}
	}
	serveTile(w, b)
}

// dirSize returns the total size of regular files under dir.
func dirSize(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// tileCacheAdd accounts for a new tile and evicts oldest-first when the quota
// is exceeded.
func (s *Server) tileCacheAdd(n int) {
	if s.cfg.TileCacheDir == "" || s.tileMaxBytes <= 0 {
		return
	}
	s.tileMu.Lock()
	s.tileBytes += int64(n)
	over := s.tileBytes > s.tileMaxBytes
	s.tileMu.Unlock()
	if over {
		s.evictTiles()
	}
}

func (s *Server) evictTiles() {
	s.tileMu.Lock()
	if s.tileEvicting {
		s.tileMu.Unlock()
		return
	}
	s.tileEvicting = true
	s.tileMu.Unlock()
	defer func() {
		s.tileMu.Lock()
		s.tileEvicting = false
		s.tileMu.Unlock()
	}()

	type entry struct {
		path string
		size int64
		mod  time.Time
	}
	var entries []entry
	var total int64
	_ = filepath.Walk(s.cfg.TileCacheDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		entries = append(entries, entry{path, info.Size(), info.ModTime()})
		total += info.Size()
		return nil
	})
	target := s.tileMaxBytes * 8 / 10
	if total > target {
		sort.Slice(entries, func(i, j int) bool { return entries[i].mod.Before(entries[j].mod) })
		for _, e := range entries {
			if total <= target {
				break
			}
			if os.Remove(e.path) == nil {
				total -= e.size
			}
		}
	}
	s.tileMu.Lock()
	s.tileBytes = total
	s.tileMu.Unlock()
}

func serveTile(w http.ResponseWriter, b []byte) {
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(b)
}

// parseTilePath parses "/tiles/{z}/{x}/{y}.png" (integers only).
func parseTilePath(p string) (z, x, y int, ok bool) {
	p = strings.TrimPrefix(p, "/tiles/")
	p = strings.TrimSuffix(p, ".png")
	parts := strings.Split(p, "/")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var err error
	if z, err = strconv.Atoi(parts[0]); err != nil {
		return 0, 0, 0, false
	}
	if x, err = strconv.Atoi(parts[1]); err != nil {
		return 0, 0, 0, false
	}
	if y, err = strconv.Atoi(parts[2]); err != nil {
		return 0, 0, 0, false
	}
	return z, x, y, true
}
