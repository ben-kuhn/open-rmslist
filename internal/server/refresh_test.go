package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRefreshEndpoint(t *testing.T) {
	s := newTestServer(t, nil)
	s.refreshCooldown = 0
	calls := 0
	s.refreshCodes = func(context.Context) error {
		calls++
		return nil
	}

	do := func(method, contentType, origin string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/refresh", strings.NewReader(`{}`))
		req.Host = "127.0.0.1:1234"
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodPost, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusOK {
		t.Fatalf("refresh = %d %s", rec.Code, rec.Body.String())
	}
	if calls != 1 {
		t.Fatalf("refresh calls = %d", calls)
	}
	if rec := do(http.MethodGet, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET refresh = %d", rec.Code)
	}
	if rec := do(http.MethodPost, "text/plain", "http://127.0.0.1:1234"); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain refresh = %d", rec.Code)
	}
	if rec := do(http.MethodPost, "application/json", "https://evil.example"); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin refresh = %d", rec.Code)
	}

	// A recent manual refresh is rate-limited.
	s.refreshCooldown = time.Hour
	s.refreshMu.Lock()
	s.lastManualRefresh = time.Now()
	s.refreshMu.Unlock()
	if rec := do(http.MethodPost, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("cooldown refresh = %d", rec.Code)
	}

	// A service-code refresh in flight is coalesced.
	s.refreshCooldown = 0
	s.refreshMu.Lock()
	s.lastManualRefresh = time.Time{}
	s.refreshMu.Unlock()
	s.codeRefreshSem <- struct{}{}
	defer func() { <-s.codeRefreshSem }()
	if rec := do(http.MethodPost, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "refreshing") {
		t.Fatalf("busy refresh = %d %s", rec.Code, rec.Body.String())
	}
}
