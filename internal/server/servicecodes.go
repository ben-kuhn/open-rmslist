package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"log"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/atomicfile"
	"github.com/ben-kuhn/rms-radar/internal/cmsapi"
)

// handleServiceCodesConfig returns or updates the persisted list of service
// codes to fetch. The Winlink API has no wildcard, so adding a code is how a
// group publishes a new community modem without a new release.
func (s *Server) handleServiceCodesConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, map[string]any{
			"codes":          s.loader.ServiceCodes(),
			"editable":       s.serviceCodeEditingEnabled(),
			"token_required": s.serviceCodeTokenRequired(),
		})
	case http.MethodPost:
		if !s.serviceCodeEditingEnabled() {
			http.Error(w, "service-code editing is disabled", http.StatusForbidden)
			return
		}
		if !s.validServiceCodeRequest(w, r) {
			return
		}
		if !s.allowServiceCodeChange() {
			w.Header().Set("Retry-After", "2")
			http.Error(w, "service codes changed recently; try again shortly", http.StatusTooManyRequests)
			return
		}

		var req struct {
			Codes  []string `json:"codes"`
			Add    string   `json:"add"`
			Remove string   `json:"remove"`
		}
		body := http.MaxBytesReader(w, r.Body, 1<<16)
		dec := json.NewDecoder(body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			http.Error(w, "unexpected trailing data", http.StatusBadRequest)
			return
		}

		next := s.loader.ServiceCodes()
		if req.Codes != nil {
			next = req.Codes
		} else {
			if req.Add == "" && req.Remove == "" {
				http.Error(w, "nothing to change", http.StatusBadRequest)
				return
			}
			next = append(next, req.Add)
			if req.Remove != "" {
				filtered := next[:0]
				for _, c := range next {
					if !strings.EqualFold(c, req.Remove) {
						filtered = append(filtered, c)
					}
				}
				next = filtered
			}
		}
		normalized, err := cmsapi.NormalizeServiceCodes(next)
		if err != nil {
			http.Error(w, "invalid service codes", http.StatusBadRequest)
			return
		}
		if len(normalized) == 0 {
			http.Error(w, "at least one service code is required", http.StatusBadRequest)
			return
		}
		if err := s.saveServiceCodes(normalized); err != nil {
			log.Printf("service-code save: %v", err)
			http.Error(w, "could not save service codes", http.StatusInternalServerError)
			return
		}
		if err := s.loader.SetServiceCodes(normalized); err != nil {
			http.Error(w, "invalid service codes", http.StatusBadRequest)
			return
		}

		resp := map[string]any{"codes": normalized}
		if s.refreshCodes != nil {
			select {
			case s.codeRefreshSem <- struct{}{}:
				resp["refresh"] = "started"
				go func() {
					defer func() { <-s.codeRefreshSem }()
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
					defer cancel()
					if err := s.refreshCodes(ctx); err != nil {
						log.Printf("service-code refresh: %v", err)
					}
				}()
			default:
				// A refresh is already in flight; it will pick up the new codes
				// because the loader reads them at fetch time.
				resp["warning"] = "a refresh is already running"
			}
		}
		writeJSON(w, resp)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

const adminTokenHeader = "X-RMS-Radar-Admin"

func (s *Server) serviceCodeEditingEnabled() bool {
	return s.adminToken != "" || s.desktopMode || s.desktopToken != ""
}

func (s *Server) serviceCodeTokenRequired() bool {
	return s.adminToken != ""
}

// validServiceCodeRequest enforces an authenticated, same-origin JSON request.
// Requiring application/json forces a CORS preflight for cross-site browser
// requests; the Origin/Sec-Fetch-Site checks reject that preflight.
func (s *Server) validServiceCodeRequest(w http.ResponseWriter, r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	mediaType, _, err := mime.ParseMediaType(ct)
	if err != nil || mediaType != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	if s.adminToken != "" {
		got := r.Header.Get(adminTokenHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.adminToken)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return false
		}
	}
	if !s.sameOrigin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func (s *Server) sameOrigin(r *http.Request) bool {
	if sfs := r.Header.Get("Sec-Fetch-Site"); sfs == "cross-site" || sfs == "same-site" {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		// Non-browser clients (and some webviews) omit Origin. Without an admin
		// token, only loopback Host headers are allowed, which blocks DNS
		// rebinding from a public hostname to 127.0.0.1.
		return s.adminToken != "" || isLocalHost(r.Host)
	}
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	if !strings.EqualFold(u.Host, r.Host) {
		return false
	}
	return s.adminToken != "" || isLocalHost(r.Host)
}

func isLocalHost(host string) bool {
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}
	if strings.EqualFold(hostname, "localhost") {
		return true
	}
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

func (s *Server) allowServiceCodeChange() bool {
	if s.codeChangeMinInterval <= 0 {
		return true
	}
	s.codeChangeMu.Lock()
	defer s.codeChangeMu.Unlock()
	now := time.Now()
	if !s.lastCodeChange.IsZero() && now.Sub(s.lastCodeChange) < s.codeChangeMinInterval {
		return false
	}
	s.lastCodeChange = now
	return true
}

func (s *Server) saveServiceCodes(codes []string) error {
	if s.cfg.ServiceCodesPath == "" {
		return nil
	}
	b, err := json.Marshal(codes)
	if err != nil {
		return err
	}
	return atomicfile.Write(s.cfg.ServiceCodesPath, b, 0o644)
}
