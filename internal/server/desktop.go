package server

import (
	"crypto/subtle"
	"encoding/json"
	"log"
	"mime"
	"net/http"
	"time"
)

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
	if s.cfg.DesktopToken == "" {
		http.NotFound(w, r)
		return false
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return false
	}
	got := r.URL.Query().Get("token")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.DesktopToken)) != 1 {
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
			if s.cfg.OnQuit != nil {
				s.cfg.OnQuit()
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
	if s.cfg.OnQuit != nil {
		go s.cfg.OnQuit()
	}
}

// handleDesktopOpen opens one of the published documentation pages in the
// system browser. The native webview does not handle target=_blank links
// reliably, so the desktop UI calls this endpoint instead. Only two fixed pages
// are allowed, and the endpoint exists only in desktop mode.
func (s *Server) handleDesktopOpen(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.DesktopMode && s.cfg.DesktopToken == "" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !s.sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return
	}
	var req struct {
		Page string `json:"page"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<12))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	var url string
	switch req.Page {
	case "clients":
		url = "https://rmsradar.ku0hn.radio/clients.html"
	case "gateway":
		url = "https://rmsradar.ku0hn.radio/gateway.html"
	default:
		http.Error(w, "unknown documentation page", http.StatusBadRequest)
		return
	}
	if err := s.cfg.OpenURL(url); err != nil {
		log.Printf("open documentation: %v", err)
		http.Error(w, "could not open documentation", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]string{"status": "opened"})
}
