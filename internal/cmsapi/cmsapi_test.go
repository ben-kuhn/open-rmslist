package cmsapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCachePath(t *testing.T) {
	if got := NewLoader("/tmp/x", []string{"PUBLIC"}, 48, "").cachePath(); got != filepath.Join("/tmp/x", "gateway_status.json") {
		t.Errorf("public cachePath = %q", got)
	}
	l := NewLoader("/tmp/x", []string{"PUBLIC", "MERCURY"}, 48, "")
	got := l.cachePath()
	if filepath.Dir(got) != "/tmp/x" || !strings.HasPrefix(filepath.Base(got), "gateway_status-") {
		t.Errorf("multi cachePath = %q", got)
	}
	if strings.Contains(got, "MERCURY") {
		t.Errorf("cache path contains raw code: %q", got)
	}
}

func TestCachePathTraversalSafe(t *testing.T) {
	l := NewLoader("/tmp/x", []string{"PUBLIC"}, 48, "")
	// Even if a caller bypasses SetServiceCodes, the filename is hashed.
	l.codes = []string{"/../../../tmp/pwned", "PUBLIC"}
	got := l.cachePath()
	if filepath.Dir(got) != "/tmp/x" {
		t.Fatalf("cache path escaped: %q", got)
	}
}

func TestNormalizeServiceCodes(t *testing.T) {
	got, err := NormalizeServiceCodes([]string{" public ", "MERCURY", "mercury", "", "DATA2G"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"DATA2G", "MERCURY", "PUBLIC"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for _, bad := range [][]string{
		{"/../../../tmp/pwned"},
		{"../../etc/passwd"},
		{"bad code"},
		{"ABCDEFGHIJKLMNOPQ"}, // 17 chars
	} {
		if _, err := NormalizeServiceCodes(bad); err == nil {
			t.Errorf("accepted invalid code %q", bad)
		}
	}
	tooMany := make([]string, MaxServiceCodes+1)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("CODE%d", i)
	}
	if _, err := NormalizeServiceCodes(tooMany); err == nil {
		t.Error("accepted too many codes")
	}
}

func TestSetServiceCodesRejectsInvalid(t *testing.T) {
	l := NewLoader(t.TempDir(), []string{"PUBLIC"}, 48, "")
	if err := l.SetServiceCodes([]string{"../pwned"}); err == nil {
		t.Fatal("invalid code accepted")
	}
	if got := l.ServiceCodes(); len(got) != 1 || got[0] != "PUBLIC" {
		t.Fatalf("codes changed after rejection: %v", got)
	}
}

func TestSeedEmbedded(t *testing.T) {
	l := NewLoader(t.TempDir(), []string{"PUBLIC"}, 48, "")
	l.Seed()
	data, src, _ := l.Snapshot()
	if len(data) == 0 {
		t.Fatal("Seed produced no data (embedded snapshot missing?)")
	}
	if src != SourceEmbedded {
		t.Errorf("source = %v, want embedded", src)
	}
}

var validFeed = []byte(`{"Gateways":[{"Callsign":"N0CALL","GatewayChannels":[]}]}`)

func TestSeedCache(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir, []string{"PUBLIC"}, 48, "")
	if err := os.WriteFile(l.cachePath(), validFeed, 0o644); err != nil {
		t.Fatal(err)
	}
	l.Seed()
	data, src, _ := l.Snapshot()
	if src != SourceCache {
		t.Errorf("source = %v, want cache", src)
	}
	if len(data) == 0 {
		t.Error("cache data not loaded")
	}
}

func TestFetchFormEncoding(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("ParseForm: %v", err)
		}
		got = r.PostForm
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(validFeed)
	}))
	defer srv.Close()

	c := NewClient("testkey")
	c.baseURL = srv.URL

	// historyHours above the max is clamped to 48; two service codes are sent
	// as repeated form fields.
	if _, err := c.Fetch(context.Background(), []string{"PUBLIC", "MERCURY"}, 99); err != nil {
		t.Fatal(err)
	}
	if got.Get("key") != "testkey" {
		t.Errorf("key = %q", got.Get("key"))
	}
	if got.Get("Mode") != "AnyAll" {
		t.Errorf("Mode = %q", got.Get("Mode"))
	}
	if got.Get("HistoryHours") != "48" {
		t.Errorf("HistoryHours = %q, want 48 (clamped)", got.Get("HistoryHours"))
	}
	if codes := got["ServiceCodes"]; len(codes) != 2 || codes[0] != "PUBLIC" || codes[1] != "MERCURY" {
		t.Errorf("ServiceCodes = %v", codes)
	}

	// Empty service codes default to PUBLIC; zero history defaults to 48.
	if _, err := c.Fetch(context.Background(), nil, 0); err != nil {
		t.Fatal(err)
	}
	if codes := got["ServiceCodes"]; len(codes) != 1 || codes[0] != "PUBLIC" {
		t.Errorf("default ServiceCodes = %v", codes)
	}
	if got.Get("HistoryHours") != "48" {
		t.Errorf("default HistoryHours = %q", got.Get("HistoryHours"))
	}
}

func TestSourceString(t *testing.T) {
	if SourceNetwork.String() != "network" || SourceCache.String() != "cache" || SourceEmbedded.String() != "embedded" {
		t.Error("unexpected Source strings")
	}
}

func TestRefreshCache(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir, []string{"PUBLIC"}, 48, "k")
	if err := os.WriteFile(l.cachePath(), validFeed, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := l.Refresh(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	data, src, _ := l.Snapshot()
	if src != SourceCache || len(data) == 0 {
		t.Errorf("src = %v, len = %d", src, len(data))
	}
}

func TestRefreshNetwork(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"Gateways":[{"Callsign":"N0CALL"}]}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	l := NewLoader(dir, []string{"PUBLIC"}, 48, "k")
	l.client.baseURL = srv.URL
	if err := l.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	data, src, _ := l.Snapshot()
	if src != SourceNetwork {
		t.Errorf("src = %v, want network", src)
	}
	if string(data) != string(payload) {
		t.Errorf("data = %q", data)
	}
	if _, err := os.Stat(l.cachePath()); err != nil {
		t.Errorf("cache not written: %v", err)
	}
}

func TestRefreshEmbedded(t *testing.T) {
	dir := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	l := NewLoader(dir, []string{"PUBLIC"}, 48, "k")
	l.client.baseURL = srv.URL
	if err := l.Refresh(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	data, src, _ := l.Snapshot()
	if src != SourceEmbedded {
		t.Errorf("src = %v, want embedded", src)
	}
	if len(data) == 0 {
		t.Error("expected embedded data")
	}
}

func TestValidateGatewayStatus(t *testing.T) {
	if err := ValidateGatewayStatus(validFeed); err != nil {
		t.Fatalf("valid feed rejected: %v", err)
	}
	for name, body := range map[string]string{
		"empty":   `{"Gateways":[]}`,
		"error":   `{"ResponseStatus":{"ErrorCode":"InvalidAccessKey","Message":"nope"}}`,
		"code":    `{"ErrorCode":42,"Gateways":[{"Callsign":"N0CALL"}]}`,
		"no call": `{"Gateways":[{"GatewayChannels":[]}]}`,
		"bad":     `not json`,
	} {
		if err := ValidateGatewayStatus([]byte(body)); err == nil {
			t.Errorf("%s: expected rejection", name)
		}
	}
}

func TestRefreshKeepsLastGood(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir, []string{"PUBLIC"}, 48, "k")
	if err := os.WriteFile(l.cachePath(), validFeed, 0o644); err != nil {
		t.Fatal(err)
	}
	l.Seed()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	l.client.baseURL = srv.URL

	if err := l.Refresh(context.Background(), true); err == nil {
		t.Fatal("expected refresh error")
	}
	data, src, _ := l.Snapshot()
	if src != SourceCache || string(data) != string(validFeed) {
		t.Errorf("last-known-good not kept: src=%v len=%d", src, len(data))
	}
	if l.LastError() == "" {
		t.Error("LastError is empty")
	}
}

func TestSeedRejectsInvalidCache(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir, []string{"PUBLIC"}, 48, "")
	if err := os.WriteFile(l.cachePath(), []byte(`{"Gateways":[]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	l.Seed()
	_, src, _ := l.Snapshot()
	if src != SourceEmbedded {
		t.Errorf("source = %v, want embedded after invalid cache", src)
	}
}
