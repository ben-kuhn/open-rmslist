package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if filepath.Ext(r.URL.Path) == ".sha256" {
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  bundle.tar.gz\n"))
			return
		}
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "voacap")
	exe, data := downloadVOACAP(srv.URL+"/bundle.tar.gz", dest)
	if filepath.Base(exe) != "voacapl" || filepath.Base(data) != "itshfbc" {
		t.Fatalf("exe=%q data=%q", exe, data)
	}

	// A verified install is reused offline (the server is gone).
	srv.Close()
	if exe2, data2 := downloadVOACAP(srv.URL+"/bundle.tar.gz", dest); exe2 != exe || data2 != data {
		t.Fatalf("offline reuse failed: exe=%q data=%q", exe2, data2)
	}
}

func TestDownloadVOACAPRequiresChecksum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer srv.Close()

	if exe, data := downloadVOACAP(srv.URL+"/bundle.tar.gz", filepath.Join(t.TempDir(), "voacap")); exe != "" || data != "" {
		t.Fatalf("download without a checksum should be refused: %q %q", exe, data)
	}
	if exe, data := downloadVOACAP("", filepath.Join(t.TempDir(), "voacap")); exe != "" || data != "" {
		t.Fatalf("empty URL should be a no-op: %q %q", exe, data)
	}
}
