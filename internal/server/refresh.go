package server

import (
	"context"
	"log"
	"mime"
	"net/http"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/cmsapi"
)

// handleRefresh fetches the Winlink gateway list on demand. It is public but
// rate-limited and coalesced: the background refresh runs once a day, and a
// manual refresh is allowed at most once per refreshCooldown (five minutes).
func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	if !originHostMatches(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if s.refreshCodes == nil {
		http.Error(w, "refresh is unavailable", http.StatusServiceUnavailable)
		return
	}
	if s.loader.Refreshing() {
		w.Header().Set("Retry-After", "10")
		writeJSON(w, map[string]any{"status": "refreshing"})
		return
	}

	// Coalesce with a service-code refresh too.
	select {
	case s.codeRefreshSem <- struct{}{}:
		defer func() { <-s.codeRefreshSem }()
	default:
		w.Header().Set("Retry-After", "10")
		writeJSON(w, map[string]any{"status": "refreshing"})
		return
	}

	_, source, at := s.loader.Snapshot()
	s.refreshMu.Lock()
	recentFetch := source == cmsapi.SourceNetwork && !at.IsZero() && time.Since(at) < s.refreshCooldown
	recentManual := s.refreshCooldown > 0 && !s.lastManualRefresh.IsZero() && time.Since(s.lastManualRefresh) < s.refreshCooldown
	if !recentFetch && !recentManual {
		s.lastManualRefresh = time.Now()
	}
	s.refreshMu.Unlock()
	if recentFetch || recentManual {
		w.Header().Set("Retry-After", "300")
		http.Error(w, "gateway list was refreshed recently; try again later", http.StatusTooManyRequests)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := s.refreshCodes(ctx); err != nil {
		log.Printf("manual gateway refresh: %v", err)
		http.Error(w, "gateway refresh failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"status": "refreshed"})
}
