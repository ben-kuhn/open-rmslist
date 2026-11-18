package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopOpen(t *testing.T) {
	s := newTestServer(t, nil)
	s.cfg.DesktopMode = true
	var opened string
	s.cfg.OpenURL = func(url string) error {
		opened = url
		return nil
	}

	do := func(method, body, contentType, origin string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/desktop/open", strings.NewReader(body))
		req.Host = "127.0.0.1:1234"
		req.RemoteAddr = "127.0.0.1:2345"
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		s.Handler().ServeHTTP(rec, req)
		return rec
	}

	if rec := do(http.MethodPost, `{"page":"clients"}`, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusOK {
		t.Fatalf("clients open = %d %s", rec.Code, rec.Body.String())
	}
	if opened != "https://rmsradar.ku0hn.radio/clients.html" {
		t.Fatalf("opened = %q", opened)
	}
	if rec := do(http.MethodPost, `{"page":"gateway"}`, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusOK {
		t.Fatalf("gateway open = %d", rec.Code)
	}
	if opened != "https://rmsradar.ku0hn.radio/gateway.html" {
		t.Fatalf("opened = %q", opened)
	}
	if rec := do(http.MethodPost, `{"page":"other"}`, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown page = %d", rec.Code)
	}
	if rec := do(http.MethodGet, "", "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET = %d", rec.Code)
	}
	if rec := do(http.MethodPost, `{"page":"clients"}`, "text/plain", "http://127.0.0.1:1234"); rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain = %d", rec.Code)
	}
	if rec := do(http.MethodPost, `{"page":"clients"}`, "application/json", "https://evil.example"); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-origin = %d", rec.Code)
	}
	lan := httptest.NewRecorder()
	lanReq := httptest.NewRequest(http.MethodPost, "/api/desktop/open", strings.NewReader(`{"page":"clients"}`))
	lanReq.Host = "127.0.0.1:1234"
	lanReq.RemoteAddr = "192.0.2.50:2345"
	lanReq.Header.Set("Content-Type", "application/json")
	lanReq.Header.Set("Origin", "http://127.0.0.1:1234")
	s.Handler().ServeHTTP(lan, lanReq)
	if lan.Code != http.StatusForbidden {
		t.Fatalf("LAN Host spoof = %d, want 403", lan.Code)
	}

	s.cfg.DesktopMode = false
	s.cfg.DesktopToken = ""
	if rec := do(http.MethodPost, `{"page":"clients"}`, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusNotFound {
		t.Fatalf("server mode open = %d, want 404", rec.Code)
	}
}
