package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func buildBundle(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name, body string, mode int64) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: mode, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatal(err)
		}
	}
	add("voacap/voacapl", "engine", 0o755)
	add("voacap/itshfbc/coeffs/ccir.dat", "data", 0o644)
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDownloadVOACAP(t *testing.T) {
	archive := buildBundle(t)
	sum := sha256.Sum256(archive)
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		a, h := archive, sum
		mu.Unlock()
		if filepath.Ext(r.URL.Path) == ".sha256" {
			_, _ = w.Write([]byte(hex.EncodeToString(h[:]) + "  bundle.tar.gz\n"))
			return
		}
		_, _ = w.Write(a)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "voacap")
	exe, data := downloadVOACAPOpts(srv.URL+"/bundle.tar.gz", dest, false)
	if filepath.Base(exe) != "voacapl" || filepath.Base(data) != "itshfbc" {
		t.Fatalf("exe=%q data=%q", exe, data)
	}
	marker := filepath.Join(dest, ".voacap-sha256")
	first, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}

	// A new release is picked up instead of reusing the old marker forever.
	next := buildBundleWithData(t, "data-v2")
	nextSum := sha256.Sum256(next)
	mu.Lock()
	archive, sum = next, nextSum
	mu.Unlock()
	if exe2, data2 := downloadVOACAPOpts(srv.URL+"/bundle.tar.gz", dest, false); exe2 == "" || data2 == "" {
		t.Fatal("update did not produce an install")
	}
	second, err := os.ReadFile(marker)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, second) || !strings.Contains(string(second), hex.EncodeToString(nextSum[:])) {
		t.Fatalf("marker was not updated: %q -> %q", first, second)
	}

	// A verified install is reused offline (the server is gone).
	srv.Close()
	if exe2, data2 := downloadVOACAPOpts(srv.URL+"/bundle.tar.gz", dest, false); exe2 != exe || data2 != data {
		t.Fatalf("offline reuse failed: exe=%q data=%q", exe2, data2)
	}
}

func buildBundleWithData(t *testing.T, payload string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for _, f := range []struct {
		name, body string
		mode       int64
	}{
		{"voacap/voacapl", "engine", 0o755},
		{"voacap/itshfbc/coeffs/ccir.dat", payload, 0o644},
	} {
		if err := tw.WriteHeader(&tar.Header{Name: f.name, Mode: f.mode, Size: int64(len(f.body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(f.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDownloadVOACAPRequiresChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if exe, data := downloadVOACAPOpts(srv.URL+"/bundle.tar.gz", filepath.Join(t.TempDir(), "voacap"), false); exe != "" || data != "" {
		t.Fatalf("download without a checksum should be refused: %q %q", exe, data)
	}
	if exe, data := downloadVOACAPOpts("", filepath.Join(t.TempDir(), "voacap"), false); exe != "" || data != "" {
		t.Fatalf("empty URL should be a no-op: %q %q", exe, data)
	}
}
