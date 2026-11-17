package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ben-kuhn/open-rmslist/internal/catalog"
	"github.com/ben-kuhn/open-rmslist/internal/cmsapi"
	"github.com/ben-kuhn/open-rmslist/internal/propagation"
)

func newTestServer(t *testing.T, stations []catalog.Station) *Server {
	t.Helper()
	dir := t.TempDir()
	loader := cmsapi.NewLoader(dir, []string{"PUBLIC"}, 48, "test")
	heur := propagation.NewHeuristic(dir)
	s := New(Config{
		Version:      "test",
		ServiceCodes: []string{"PUBLIC"},
		ModeSwaps:    catalog.DefaultModeSwaps,
	}, fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(`<link rel="stylesheet" href="style.css"><script src="app.js"></script>`)},
		"app.js":     &fstest.MapFile{Data: []byte("//app")},
		"style.css":  &fstest.MapFile{Data: []byte("/*css*/")},
	}, loader, heur)
	s.stations = stations
	return s
}

func fixture() []catalog.Station {
	return []catalog.Station{
		{
			Callsign: "N0CALL-10", BaseCallsign: "N0CALL",
			Latitude: 40, Longitude: -100, HoursSinceStatus: 1,
			Channels: []catalog.Channel{
				{ServiceCode: "PUBLIC", Mode: "VARA", ModeFamily: "VARA", FrequencyHz: 7103000, Frequency: "7.103 MHz", Band: "40m"},
				{ServiceCode: "PUBLIC", Mode: "VARA FM", ModeFamily: "VARA FM", FrequencyHz: 145030000, Frequency: "145.03 MHz", Band: "2m"},
			},
		},
		{
			Callsign: "K0TEST-10", BaseCallsign: "K0TEST",
			Latitude: 41, Longitude: -101, HoursSinceStatus: 5,
			Channels: []catalog.Channel{
				{ServiceCode: "MERCURY", Mode: "Mercury", ModeFamily: "Mercury", FrequencyHz: 7105000, Frequency: "7.105 MHz", Band: "40m"},
			},
		},
	}
}

func TestSecurityHeaders(t *testing.T) {
	h := securityHeaders(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Header().Get("Content-Security-Policy") == "" {
		t.Error("missing Content-Security-Policy")
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
		t.Errorf("X-Frame-Options = %q", got)
	}
	if got := rec.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Errorf("Referrer-Policy = %q", got)
	}
}

func TestHandleRankInvalidGrid(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rank", bytes.NewBufferString(`{"grid":"ZZZZZZ"}`))
	s.handleRank(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleRankFilters(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rank", bytes.NewBufferString(`{"grid":"EM38xx","band":"40m"}`))
	s.handleRank(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var out []rankedEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 {
		t.Fatalf("got %d results, want 2 (both 40m channels)", len(out))
	}
	for _, e := range out {
		if e.Band != "40m" {
			t.Errorf("band = %q, want 40m", e.Band)
		}
	}
}

func TestHandleRankServiceCode(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rank", bytes.NewBufferString(`{"grid":"EM38xx","service_code":"MERCURY"}`))
	s.handleRank(rec, req)
	var out []rankedEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Callsign != "K0TEST-10" {
		t.Fatalf("got %+v", out)
	}
}

func TestHandleRankTruncated(t *testing.T) {
	var stations []catalog.Station
	for i := 0; i < maxRankCandidates+1; i++ {
		stations = append(stations, catalog.Station{
			Callsign: "N0CALL-" + strconv.Itoa(i),
			Latitude: 40, Longitude: -100,
			Channels: []catalog.Channel{{ServiceCode: "PUBLIC", Mode: "VARA", ModeFamily: "VARA", FrequencyHz: 7103000, Frequency: "7.103 MHz", Band: "40m"}},
		})
	}
	s := newTestServer(t, stations)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/rank", bytes.NewBufferString(`{"grid":"EM38xx","band":"40m"}`))
	s.handleRank(rec, req)
	if rec.Header().Get("X-Rank-Truncated") != "true" {
		t.Error("expected X-Rank-Truncated header")
	}
	var out []rankedEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out) > maxRankCandidates {
		t.Errorf("got %d results, want <= %d", len(out), maxRankCandidates)
	}
}

func TestHandleServiceCodes(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	s.handleServiceCodes(rec, httptest.NewRequest(http.MethodGet, "/api/servicecodes", nil))
	var codes []struct {
		Code  string `json:"code"`
		Count int    `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &codes); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, c := range codes {
		got[c.Code] = c.Count
	}
	if got["PUBLIC"] != 2 || got["MERCURY"] != 1 {
		t.Errorf("counts = %v, want PUBLIC:2 MERCURY:1", got)
	}
}

func TestHandleConfigVoacapOff(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	s.handleConfig(rec, httptest.NewRequest(http.MethodGet, "/api/config", nil))
	var cfg map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatal(err)
	}
	if v, ok := cfg["voacap"].(bool); !ok || v {
		t.Errorf("voacap = %v, want false", cfg["voacap"])
	}
}

func TestParseRangeKm(t *testing.T) {
	if got := parseRangeKm("8"); got < 12 || got > 14 {
		t.Errorf("parseRangeKm(8) = %v, want ~12.9", got)
	}
	if got := parseRangeKm(""); got != 0 {
		t.Errorf("parseRangeKm(empty) = %v, want 0", got)
	}
	if got := parseRangeKm("junk"); got != 0 {
		t.Errorf("parseRangeKm(junk) = %v, want 0", got)
	}
}

func TestHandlerRoutes(t *testing.T) {
	h := newTestServer(t, fixture()).Handler()
	for _, path := range []string{"/", "/api/health", "/api/gateways", "/api/servicecodes", "/api/conditions", "/api/config"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Errorf("%s = %d, want 200", path, rec.Code)
		}
		if rec.Header().Get("Content-Security-Policy") == "" {
			t.Errorf("%s missing CSP header", path)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/nope", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("unknown path = %d, want 404", rec.Code)
	}
}

func TestServerRefresh(t *testing.T) {
	dir := t.TempDir()
	loader := cmsapi.NewLoader(dir, []string{"PUBLIC"}, 48, "k")
	loader.Seed() // embedded snapshot
	heur := propagation.NewHeuristic(dir)
	s := New(Config{Version: "test", ServiceCodes: []string{"PUBLIC"}, ModeSwaps: catalog.DefaultModeSwaps},
		fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}}, loader, heur)

	if err := s.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := len(s.snapshotStations()); n == 0 {
		t.Error("Refresh produced no stations from the embedded snapshot")
	}
}

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	s.handleHealth(rec, httptest.NewRequest(http.MethodGet, "/api/health", nil))
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["ok"] != true || out["version"] != "test" {
		t.Errorf("health = %v", out)
	}
}

func TestHandleGateways(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	s.handleGateways(rec, httptest.NewRequest(http.MethodGet, "/api/gateways", nil))
	var st []catalog.Station
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil {
		t.Fatal(err)
	}
	if len(st) != 2 {
		t.Fatalf("got %d stations, want 2", len(st))
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestHandleConditions(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	s.handleConditions(rec, httptest.NewRequest(http.MethodGet, "/api/conditions", nil))
	var c propagation.Conditions
	if err := json.Unmarshal(rec.Body.Bytes(), &c); err != nil {
		t.Fatalf("conditions response not valid JSON: %v", err)
	}
}

func TestServeIndexVersioned(t *testing.T) {
	s := newTestServer(t, fixture())
	rec := httptest.NewRecorder()
	s.serveIndex(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	body := rec.Body.String()
	if !strings.Contains(body, "app.js?v=") || !strings.Contains(body, "style.css?v=") {
		t.Errorf("index not cache-busted: %s", body)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", got)
	}
}
