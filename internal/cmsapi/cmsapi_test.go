package cmsapi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCachePath(t *testing.T) {
	if got := NewLoader("/tmp/x", []string{"PUBLIC"}, 48, "").cachePath(); got != filepath.Join("/tmp/x", "gateway_status.json") {
		t.Errorf("public cachePath = %q", got)
	}
	if got := NewLoader("/tmp/x", []string{"PUBLIC", "MERCURY"}, 48, "").cachePath(); got != filepath.Join("/tmp/x", "gateway_status-PUBLIC-MERCURY.json") {
		t.Errorf("multi cachePath = %q", got)
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

func TestSeedCache(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(dir, []string{"PUBLIC"}, 48, "")
	if err := os.WriteFile(l.cachePath(), []byte(`{"Gateways":[]}`), 0o644); err != nil {
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
