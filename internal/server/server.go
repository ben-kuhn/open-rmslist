package server

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/catalog"
	"github.com/ben-kuhn/rms-radar/internal/cmsapi"
	"github.com/ben-kuhn/rms-radar/internal/propagation"
)

// Config configures the HTTP server.
type Config struct {
	Version          string
	ServiceCodes     []string
	HistoryHours     int
	VOACAPURL        string
	VOACAPExecutable string
	VOACAPDataDir    string
	TileURL          string
	TileCacheDir     string
	VHFMaxRangeKm    float64

	// DesktopToken enables the desktop lifecycle endpoints. It is non-empty
	// only when the app was launched in desktop (double-click) mode.
	DesktopToken string
	// OnQuit asks the process to exit (the desktop window/tab is gone, or the
	// user pressed Quit).
	OnQuit func()
}

// Server serves the web UI and JSON API.
type Server struct {
	cfg       Config
	web       fs.FS
	loader    *cmsapi.Loader
	heuristic *propagation.Heuristic
	ranker    *propagation.Ranker

	stationsMu sync.RWMutex
	stations   []catalog.Station

	voacapEnabled bool

	// assetVersion is a short content hash of the mutable web assets, used to
	// cache-bust app.js/style.css behind a CDN.
	assetVersion string

	tileClient *http.Client

	// Desktop (double-click) lifecycle: the page checks in while it is open,
	// and the process exits once every page has gone away.
	desktopToken   string
	onQuit         func()
	desktopMu      sync.Mutex
	desktopClients map[string]time.Time
	desktopSeen    bool
	desktopEmptyAt time.Time
}

func New(cfg Config, web fs.FS, loader *cmsapi.Loader, heur *propagation.Heuristic) *Server {
	// More accurate predictors first; the always-available heuristic is last.
	var predictors []propagation.Predictor
	voacapEnabled := false
	if cfg.VOACAPURL != "" {
		predictors = append(predictors, propagation.NewVOACAPAPI(cfg.VOACAPURL))
		voacapEnabled = true
	}
	if cfg.VOACAPExecutable != "" {
		if p, err := propagation.NewVOACAP(cfg.VOACAPExecutable, cfg.VOACAPDataDir); err == nil {
			predictors = append(predictors, p)
			voacapEnabled = true
		}
	}
	predictors = append(predictors, heur)

	s := &Server{
		cfg:           cfg,
		web:           web,
		loader:        loader,
		heuristic:     heur,
		ranker:        &propagation.Ranker{Predictors: predictors},
		voacapEnabled: voacapEnabled,
		assetVersion:  assetVersion(web),
		tileClient:    &http.Client{Timeout: 15 * time.Second},

		desktopToken:   cfg.DesktopToken,
		onQuit:         cfg.OnQuit,
		desktopClients: make(map[string]time.Time),
	}
	if s.desktopToken != "" {
		go s.monitorDesktop()
	}
	return s
}

// assetVersion returns a short hash of the mutable web assets. When app.js,
// theme.js, or style.css changes, the hash changes and the HTML points at a new
// URL, which bypasses CDN edge caches (which ignore a browser hard-refresh).
func assetVersion(web fs.FS) string {
	h := sha256.New()
	for _, name := range []string{"app.js", "theme.js", "style.css", "vendor/leaflet/leaflet.js", "vendor/leaflet/leaflet.css"} {
		if b, err := fs.ReadFile(web, name); err == nil {
			_, _ = h.Write(b)
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}

// Refresh re-parses the loader's current snapshot into the normalized catalog.
func (s *Server) Refresh(ctx context.Context) error {
	data, _, _ := s.loader.Snapshot()
	if len(data) == 0 {
		return nil
	}
	var status cmsapi.GatewayStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return err
	}
	stations := catalog.Normalize(&status)
	s.stationsMu.Lock()
	s.stations = stations
	s.stationsMu.Unlock()
	return nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/gateways", s.handleGateways)
	mux.HandleFunc("/api/servicecodes", s.handleServiceCodes)
	mux.HandleFunc("/api/conditions", s.handleConditions)
	mux.HandleFunc("/api/rank", s.handleRank)
	mux.HandleFunc("/api/config", s.handleConfig)
	mux.HandleFunc("/api/desktop/hello", s.handleDesktopHello)
	mux.HandleFunc("/api/desktop/beat", s.handleDesktopBeat)
	mux.HandleFunc("/api/desktop/bye", s.handleDesktopBye)
	mux.HandleFunc("/api/desktop/quit", s.handleDesktopQuit)
	mux.HandleFunc("/tiles/", s.handleTiles)

	fileServer := http.FileServer(http.FS(s.web))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html":
			s.serveHTML(w, r, "index.html")
		case "/clients.html":
			s.serveHTML(w, r, "clients.html")
		case "/gateway.html":
			s.serveHTML(w, r, "gateway.html")
		default:
			// Content-hashed (?v=) assets can be cached for a few days: a new
			// build changes the hash, so the HTML always points at fresh URLs.
			// Unversioned files get a short TTL so a stale copy can't linger.
			if r.URL.Query().Get("v") != "" {
				w.Header().Set("Cache-Control", "public, max-age=259200") // 3 days
			} else {
				w.Header().Set("Cache-Control", "public, max-age=300")
			}
			fileServer.ServeHTTP(w, r)
		}
	})
	return securityHeaders(mux)
}

// serveHTML renders an HTML page with content-hashed asset URLs and no-cache,
// so a fresh page always points at fresh JS/CSS even behind an aggressive CDN.
func (s *Server) serveHTML(w http.ResponseWriter, r *http.Request, page string) {
	b, err := fs.ReadFile(s.web, page)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	html := string(b)
	if s.assetVersion != "" {
		v := s.assetVersion
		html = strings.ReplaceAll(html, `href="style.css"`, `href="style.css?v=`+v+`"`)
		html = strings.ReplaceAll(html, `src="app.js"`, `src="app.js?v=`+v+`"`)
		html = strings.ReplaceAll(html, `src="theme.js"`, `src="theme.js?v=`+v+`"`)
		html = strings.ReplaceAll(html, `href="vendor/leaflet/leaflet.css"`, `href="vendor/leaflet/leaflet.css?v=`+v+`"`)
		html = strings.ReplaceAll(html, `src="vendor/leaflet/leaflet.js"`, `src="vendor/leaflet/leaflet.js?v=`+v+`"`)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write([]byte(html))
}

// securityHeaders adds defensive headers to every response. The CSP allows the
// pinned OSM/CARTO tile hosts and Leaflet's inline styles.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy",
			"default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' https:; "+
				"connect-src 'self'; font-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) snapshotStations() []catalog.Station {
	s.stationsMu.RLock()
	defer s.stationsMu.RUnlock()
	return s.stations
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	_, source, at := s.loader.Snapshot()
	stations := s.snapshotStations()
	writeJSON(w, map[string]any{
		"ok":            true,
		"version":       s.cfg.Version,
		"source":        source.String(),
		"fetched_at":    at.UTC().Format(time.RFC3339),
		"stations":      len(stations),
		"service_codes": s.cfg.ServiceCodes,
	})
}

func (s *Server) handleGateways(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.snapshotStations())
}

func (s *Server) handleServiceCodes(w http.ResponseWriter, r *http.Request) {
	counts := map[string]int{}
	for _, st := range s.snapshotStations() {
		for _, ch := range st.Channels {
			sc := ch.ServiceCode
			if sc == "" {
				sc = "PUBLIC"
			}
			counts[sc]++
		}
	}
	type scEntry struct {
		Code  string `json:"code"`
		Count int    `json:"count"`
	}
	list := make([]scEntry, 0, len(counts))
	for k, v := range counts {
		list = append(list, scEntry{Code: k, Count: v})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Count > list[j].Count })
	writeJSON(w, list)
}

func (s *Server) handleConditions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.heuristic.Conditions())
}

func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"version":       s.cfg.Version,
		"service_codes": s.cfg.ServiceCodes,
		"voacap":        s.voacapEnabled,
		"tile_url":      proxyTilePath,
	})
}

// ---- desktop (double-click) lifecycle ----
//
// In desktop mode main generates a random token and opens the browser at
// /?desktop=<token>. The page registers, heartbeats, and sends a beacon when it
// closes. When every registered page has gone away, the process exits. -d
// (daemon) disables the token entirely, so servers never auto-exit.

const (
	desktopGrace     = 15 * time.Second // wait this long after the last page closes
	desktopClientTTL = 2 * time.Minute  // drop pages that stop heartbeating
)

func (s *Server) desktopAuthorized(w http.ResponseWriter, r *http.Request) bool {
	if s.desktopToken == "" {
		http.NotFound(w, r)
		return false
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	got := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.desktopToken)) != 1 {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) desktopTouch(id string) {
	if id == "" {
		return
	}
	s.desktopMu.Lock()
	defer s.desktopMu.Unlock()
	s.desktopClients[id] = time.Now()
	s.desktopSeen = true
	s.desktopEmptyAt = time.Time{}
}

func (s *Server) desktopRemove(id string) {
	s.desktopMu.Lock()
	defer s.desktopMu.Unlock()
	delete(s.desktopClients, id)
	if s.desktopSeen && len(s.desktopClients) == 0 && s.desktopEmptyAt.IsZero() {
		s.desktopEmptyAt = time.Now()
	}
}

func (s *Server) monitorDesktop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		s.desktopMu.Lock()
		for id, seen := range s.desktopClients {
			if now.Sub(seen) > desktopClientTTL {
				delete(s.desktopClients, id)
			}
		}
		if s.desktopSeen && len(s.desktopClients) == 0 && s.desktopEmptyAt.IsZero() {
			s.desktopEmptyAt = now
		}
		quit := s.desktopSeen && len(s.desktopClients) == 0 &&
			!s.desktopEmptyAt.IsZero() && now.Sub(s.desktopEmptyAt) > desktopGrace
		s.desktopMu.Unlock()
		if quit {
			if s.onQuit != nil {
				s.onQuit()
			}
			return
		}
	}
}

func (s *Server) handleDesktopHello(w http.ResponseWriter, r *http.Request) {
	if !s.desktopAuthorized(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	s.desktopTouch(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDesktopBeat(w http.ResponseWriter, r *http.Request) {
	if !s.desktopAuthorized(w, r) {
		return
	}
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	s.desktopTouch(id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDesktopBye(w http.ResponseWriter, r *http.Request) {
	if !s.desktopAuthorized(w, r) {
		return
	}
	s.desktopRemove(r.URL.Query().Get("id"))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleDesktopQuit(w http.ResponseWriter, r *http.Request) {
	if !s.desktopAuthorized(w, r) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
	if s.onQuit != nil {
		go s.onQuit()
	}
}

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

	cachePath := filepath.Join(s.cfg.TileCacheDir, strconv.Itoa(z), strconv.Itoa(x), strconv.Itoa(y)+".png")
	if b, err := os.ReadFile(cachePath); err == nil && len(b) > 0 {
		serveTile(w, b)
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
		tmp := cachePath + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, cachePath)
		}
	}
	serveTile(w, b)
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

// rankedEntry is one (station, channel) candidate in a ranked result.
type rankedEntry struct {
	Callsign    string  `json:"callsign"`
	Latitude    float64 `json:"latitude"`
	Longitude   float64 `json:"longitude"`
	Gridsquare  string  `json:"gridsquare"`
	ServiceCode string  `json:"service_code"`
	Mode        string  `json:"mode"`
	ModeFamily  string  `json:"mode_family"`
	Band        string  `json:"band"`
	FrequencyHz float64 `json:"frequency_hz"`
	Frequency   string  `json:"frequency"`
	DistanceKm  float64 `json:"distance_km"`
	BearingDeg  float64 `json:"bearing_deg"`
	LinkQuality int     `json:"link_quality"`
	BandOpen    bool    `json:"band_open"`
	MUF         float64 `json:"muf_mhz,omitempty"`
	Method      string  `json:"method"`
	Note        string  `json:"note,omitempty"`
}

// Per-request rank deadlines. main.go sets WriteTimeout to
// RankTimeoutVOACAP + a margin, so the HTTP write always outlives the rank.
const (
	RankTimeoutHeuristic = 120 * time.Second
	RankTimeoutVOACAP    = 5 * time.Minute
)

// maxRankCandidates bounds a single rank request; a whole-feed rank (all
// bands/modes) is a self-DoS, especially when each candidate runs VOACAP.
const maxRankCandidates = 500

func (s *Server) handleRank(w http.ResponseWriter, r *http.Request) {
	grid := r.URL.Query().Get("grid")
	band := r.URL.Query().Get("band")
	mode := r.URL.Query().Get("mode")
	sc := r.URL.Query().Get("service_code")

	if r.Method == http.MethodPost {
		var rr struct {
			Grid        string `json:"grid"`
			Band        string `json:"band"`
			Mode        string `json:"mode"`
			ServiceCode string `json:"service_code"`
		}
		body := http.MaxBytesReader(w, r.Body, 1<<20)
		if err := json.NewDecoder(body).Decode(&rr); err == nil {
			grid, band, mode, sc = rr.Grid, rr.Band, rr.Mode, rr.ServiceCode
		}
	}

	from, err := propagation.ParseLocator(grid)
	if err != nil {
		http.Error(w, "invalid grid locator: "+err.Error(), http.StatusBadRequest)
		return
	}

	ssn := s.heuristic.Conditions().Sunspots

	type cand struct {
		st   catalog.Station
		ch   catalog.Channel
		dist float64
	}
	var cands []cand
	for _, st := range s.snapshotStations() {
		to := propagation.Point{Lat: st.Latitude, Lon: st.Longitude}
		d := propagation.DistanceKm(from, to)
		for _, ch := range st.Channels {
			if sc != "" && !strings.EqualFold(ch.ServiceCode, sc) {
				continue
			}
			if band != "" && ch.Band != band {
				continue
			}
			if mode != "" && !strings.EqualFold(ch.ModeFamily, mode) {
				continue
			}
			// VHF/UHF is line-of-sight. This is a distance-based sanity bound
			// (not continent-based), so nearby cross-border stations still
			// appear while implausibly distant paths are dropped.
			if ch.FrequencyHz >= 30e6 && d > s.vhfMaxRangeKm(parseRangeKm(ch.RadioRange)) {
				continue
			}
			cands = append(cands, cand{st: st, ch: ch, dist: d})
		}
	}

	// Keep the nearest candidates when capping. Truncating in callsign order
	// used to drop the closest stations and keep arbitrary distant ones.
	sort.Slice(cands, func(i, j int) bool { return cands[i].dist < cands[j].dist })
	truncated := len(cands) > maxRankCandidates
	if truncated {
		cands = cands[:maxRankCandidates]
		w.Header().Set("X-Rank-Truncated", "true")
		log.Printf("rank: truncated to %d candidates; narrow band/mode for complete results", maxRankCandidates)
	}

	timeout := RankTimeoutHeuristic
	if s.voacapEnabled {
		timeout = RankTimeoutVOACAP
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()

	out := make([]rankedEntry, len(cands))
	ok := make([]bool, len(cands))
	workers := runtime.NumCPU()
	if workers < 1 {
		workers = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				c := cands[idx]
				pred, err := s.ranker.Predict(ctx, propagation.Params{
					FromLat:      from.Lat,
					FromLon:      from.Lon,
					ToLat:        c.st.Latitude,
					ToLon:        c.st.Longitude,
					FrequencyHz:  c.ch.FrequencyHz,
					Mode:         c.ch.ModeFamily,
					RadioRangeKm: parseRangeKm(c.ch.RadioRange),
					SSN:          ssn,
				})
				if err != nil {
					continue
				}
				ok[idx] = true
				out[idx] = rankedEntry{
					Callsign:    c.st.Callsign,
					Latitude:    c.st.Latitude,
					Longitude:   c.st.Longitude,
					Gridsquare:  c.ch.Gridsquare,
					ServiceCode: c.ch.ServiceCode,
					Mode:        c.ch.Mode,
					ModeFamily:  c.ch.ModeFamily,
					Band:        c.ch.Band,
					FrequencyHz: c.ch.FrequencyHz,
					Frequency:   c.ch.Frequency,
					DistanceKm:  pred.DistanceKm,
					BearingDeg:  pred.BearingDeg,
					LinkQuality: pred.LinkQuality,
					BandOpen:    pred.BandOpen,
					MUF:         pred.MUF,
					Method:      pred.Method,
					Note:        pred.Note,
				}
			}
		}()
	}
	for i := range cands {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	results := make([]rankedEntry, 0, len(out))
	failed := 0
	for i := range out {
		if ok[i] {
			results = append(results, out[i])
		} else {
			failed++
		}
	}
	if failed > 0 {
		w.Header().Set("X-Rank-Errors", strconv.Itoa(failed))
		log.Printf("rank: %d of %d candidates failed", failed, len(out))
	}
	if ctx.Err() == context.DeadlineExceeded {
		log.Printf("rank: deadline (%s) exceeded", timeout)
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].LinkQuality != results[j].LinkQuality {
			return results[i].LinkQuality > results[j].LinkQuality
		}
		return results[i].DistanceKm < results[j].DistanceKm
	})
	writeJSON(w, results)
}

// parseRangeKm parses the gateway's advertised radio range. Winlink reports
// this in miles; we convert to km (the exact unit matters little for ranking).
func parseRangeKm(s string) float64 {
	f, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || f <= 0 {
		return 0
	}
	return f * 1.60934
}

// vhfMaxRangeKm returns the maximum plausible distance for a VHF/UHF
// (line-of-sight) path. This is purely distance-based (not continent-based), so
// e.g. a station at Gibraltar still sees nearby Europe and Africa. The floor is
// configurable (-vhf-max-range); set it large to effectively disable the filter.
func (s *Server) vhfMaxRangeKm(advertisedKm float64) float64 {
	floor := s.cfg.VHFMaxRangeKm
	if floor <= 0 {
		floor = 400 // default
	}
	return math.Max(floor, advertisedKm*1.5)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	// Gateway lists and propagation data are refreshed from Winlink/upstream and
	// must never be cached (by the browser or a CDN).
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(v)
}
