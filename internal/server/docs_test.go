package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDesktopOpen(t *testing.T) {
	s := newTestServer(t, nil)
	s.desktopMode = true
	var opened string
	s.openURL = func(url string) error {
		opened = url
		return nil
	}

	do := func(method, body, contentType, origin string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/api/desktop/open", strings.NewReader(body))
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

	s.desktopMode = false
	s.desktopToken = ""
	if rec := do(http.MethodPost, `{"page":"clients"}`, "application/json", "http://127.0.0.1:1234"); rec.Code != http.StatusNotFound {
		t.Fatalf("server mode open = %d, want 404", rec.Code)
	}
}
