package voacapget

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestFind(t *testing.T) {
	dir := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dir, "bin"), 0o755))
	must(t, os.MkdirAll(filepath.Join(dir, "itshfbc", "coeffs"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "bin", "voacapl"), []byte("#!/bin/sh\n"), 0o755))
	must(t, os.WriteFile(filepath.Join(dir, "itshfbc", "coeffs", "ccir.dat"), []byte("x"), 0o644))

	exe, data, err := Find(dir)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(exe) != "voacapl" {
		t.Fatalf("exe = %q", exe)
	}
	if filepath.Base(data) != "itshfbc" {
		t.Fatalf("data = %q", data)
	}
}

func TestSafeJoin(t *testing.T) {
	// ".." components are neutralized (clamped inside base), never escaping.
	got, err := safeJoin("/base", "../evil")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/base", "evil") {
		t.Fatalf("got %q", got)
	}
	got, err = safeJoin("/base", "sub/file.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join("/base", "sub", "file.txt") {
		t.Fatalf("got %q", got)
	}
}

func TestExtractTarGz(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "dist.tar.gz")

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name, body string) {
		must(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, _ = tw.Write([]byte(body))
	}
	add("dist/voacapl", "engine")
	must(t, tw.WriteHeader(&tar.Header{Name: "dist/itshfbc/coeffs", Mode: 0o755, Typeflag: tar.TypeDir}))
	add("dist/itshfbc/coeffs/ccir.dat", "data")
	must(t, tw.Close())
	must(t, gz.Close())
	must(t, os.WriteFile(archive, buf.Bytes(), 0o644))

	dest := filepath.Join(dir, "out")
	must(t, os.MkdirAll(dest, 0o755))
	if err := extract(archive, "http://x/dist.tar.gz", dest); err != nil {
		t.Fatal(err)
	}
	exe, data, err := Find(dest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(exe) != "voacapl" || filepath.Base(data) != "itshfbc" {
		t.Fatalf("exe=%q data=%q", exe, data)
	}
}

func TestExtractZip(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "dist.zip")
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("dist/voacapw.exe")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("engine"))
	must(t, zw.Close())
	must(t, os.WriteFile(archive, buf.Bytes(), 0o644))

	dest := filepath.Join(dir, "out")
	must(t, os.MkdirAll(dest, 0o755))
	if err := extract(archive, "http://x/dist.zip", dest); err != nil {
		t.Fatal(err)
	}
	exe, _, err := Find(dest)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(exe) != "voacapw.exe" {
		t.Fatalf("exe = %q", exe)
	}
}

func TestWriteFileBudget(t *testing.T) {
	dir := t.TempDir()
	budget := int64(64)

	err := writeFile(filepath.Join(dir, "a.bin"), strings.NewReader(strings.Repeat("a", 40)), 0o644, &budget)
	if err != nil {
		t.Fatalf("first write failed: %v", err)
	}
	if budget != 24 {
		t.Fatalf("budget after first write = %d, want 24", budget)
	}

	// A second 40-byte entry exceeds the remaining 24-byte budget and must fail.
	err = writeFile(filepath.Join(dir, "b.bin"), strings.NewReader(strings.Repeat("b", 40)), 0o644, &budget)
	if err == nil {
		t.Fatal("second write should have exceeded the budget")
	}
	if budget != 24 {
		t.Errorf("budget was modified on failure: %d", budget)
	}
}

func TestExtractURLQueryAndFragment(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	add := func(name, body string) {
		must(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, _ = tw.Write([]byte(body))
	}
	must(t, tw.WriteHeader(&tar.Header{Name: "dist/itshfbc", Mode: 0o755, Typeflag: tar.TypeDir}))
	add("dist/itshfbc/ccir.dat", "data")
	must(t, tw.Close())
	must(t, gz.Close())
	archive := filepath.Join(dir, "dist.tar.gz")
	must(t, os.WriteFile(archive, buf.Bytes(), 0o644))

	for _, u := range []string{"http://x/dist.tar.gz?token=abc", "http://x/dist.tar.gz#page2"} {
		dest := t.TempDir()
		if err := extract(archive, u, dest); err != nil {
			t.Errorf("extract(%q) failed: %v", u, err)
		}
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("hello"))
	if err := verifySHA256(path, hex.EncodeToString(sum[:])); err != nil {
		t.Errorf("matching hash rejected: %v", err)
	}
	if err := verifySHA256(path, strings.Repeat("0", 64)); err == nil {
		t.Error("mismatched hash accepted")
	}
}

func buildTarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		must(t, tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
		_, _ = tw.Write([]byte(body))
	}
	must(t, tw.Close())
	must(t, gz.Close())
	return buf.Bytes()
}

func TestFetch(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"dist/voacapl":              "engine",
		"dist/itshfbc/coeffs/c.dat": "data",
	})
	sum := sha256.Sum256(archive)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dest := t.TempDir()
	exe, data, err := Fetch(context.Background(), srv.URL+"/dist.tar.gz", dest, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(exe) != "voacapl" || filepath.Base(data) != "itshfbc" {
		t.Fatalf("exe=%q data=%q", exe, data)
	}
	if _, err := os.Stat(exe); err != nil {
		t.Errorf("engine missing: %v", err)
	}

	// Idempotent: the second Fetch must succeed without the server.
	srv.Close()
	if e2, d2, err := Fetch(context.Background(), srv.URL+"/dist.tar.gz", dest, ""); err != nil {
		t.Errorf("cached Fetch failed: %v", err)
	} else if e2 != exe || d2 != data {
		t.Errorf("cached paths differ: %q/%q", e2, d2)
	}
}

func TestFetchBadHash(t *testing.T) {
	archive := buildTarGz(t, map[string]string{"dist/voacapl": "engine"})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	if _, _, err := Fetch(context.Background(), srv.URL+"/dist.tar.gz", t.TempDir(), strings.Repeat("0", 64)); err == nil {
		t.Error("expected hash-mismatch error")
	}
}

func TestExtractTarFile(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	must(t, tw.WriteHeader(&tar.Header{Name: "d/itshfbc", Mode: 0o755, Typeflag: tar.TypeDir}))
	must(t, tw.WriteHeader(&tar.Header{Name: "d/itshfbc/coeffs", Mode: 0o755, Typeflag: tar.TypeDir}))
	body := "x"
	must(t, tw.WriteHeader(&tar.Header{Name: "d/itshfbc/coeffs/a.dat", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}))
	_, _ = tw.Write([]byte(body))
	must(t, tw.Close())

	dir := t.TempDir()
	archive := filepath.Join(dir, "d.tar")
	must(t, os.WriteFile(archive, buf.Bytes(), 0o644))
	dest := filepath.Join(dir, "out")
	must(t, os.MkdirAll(dest, 0o755))
	if err := extract(archive, "http://x/d.tar", dest); err != nil {
		t.Fatal(err)
	}
}

func TestFindNoEngine(t *testing.T) {
	if _, _, err := Find(t.TempDir()); err == nil {
		t.Error("expected error when no engine is present")
	}
}

func TestFetchReusesVerifiedInstall(t *testing.T) {
	archive := buildTarGz(t, map[string]string{
		"dist/voacapl":              "engine",
		"dist/itshfbc/coeffs/c.dat": "data",
	})
	sum := sha256.Sum256(archive)
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _ = w.Write(archive)
	}))
	defer srv.Close()

	dest := t.TempDir()
	sha := hex.EncodeToString(sum[:])
	if _, _, err := Fetch(context.Background(), srv.URL+"/dist.tar.gz", dest, sha); err != nil {
		t.Fatal(err)
	}
	// Same checksum: reuse without downloading again.
	if _, _, err := Fetch(context.Background(), srv.URL+"/dist.tar.gz", dest, sha); err != nil {
		t.Fatal(err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	// A different checksum forces a reinstall attempt; a bad download must not
	// destroy the verified install.
	if _, _, err := Fetch(context.Background(), srv.URL+"/dist.tar.gz", dest, strings.Repeat("0", 64)); err == nil {
		t.Fatal("expected checksum failure")
	}
	if exe, data, err := Find(dest); err != nil || exe == "" || data == "" {
		t.Fatalf("previous install was lost: %v", err)
	}
}

func TestFetchIncompleteArchiveKeepsOld(t *testing.T) {
	good := buildTarGz(t, map[string]string{
		"dist/voacapl":              "engine",
		"dist/itshfbc/coeffs/c.dat": "data",
	})
	bad := buildTarGz(t, map[string]string{"dist/voacapl": "engine"})
	badSum := sha256.Sum256(bad)
	goodSum := sha256.Sum256(good)
	served := good
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(served)
	}))
	defer srv.Close()

	dest := t.TempDir()
	if _, _, err := Fetch(context.Background(), srv.URL+"/v.tar.gz", dest, hex.EncodeToString(goodSum[:])); err != nil {
		t.Fatal(err)
	}
	served = bad
	if _, _, err := Fetch(context.Background(), srv.URL+"/v.tar.gz", dest, hex.EncodeToString(badSum[:])); err == nil {
		t.Fatal("expected incomplete-archive failure")
	}
	if exe, data, err := Find(dest); err != nil || exe == "" || data == "" {
		t.Fatalf("previous install was lost: %v", err)
	}
}

func TestRemoteSHA256(t *testing.T) {
	sum := strings.Repeat("a", 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(sum + "  dist.tar.gz\n"))
	}))
	defer srv.Close()

	got, err := RemoteSHA256(context.Background(), srv.URL+"/dist.tar.gz.sha256")
	if err != nil {
		t.Fatal(err)
	}
	if got != sum {
		t.Fatalf("got %q want %q", got, sum)
	}
}

func TestRemoteSHA256RejectsBadSidecar(t *testing.T) {
	for _, body := range []string{"", "not-a-hash\n", strings.Repeat("z", 64)} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(body))
		}))
		if _, err := RemoteSHA256(context.Background(), srv.URL+"/x.sha256"); err == nil {
			t.Errorf("RemoteSHA256(%q) succeeded, want error", body)
		}
		srv.Close()
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()
	if _, err := RemoteSHA256(context.Background(), srv.URL+"/x.sha256"); err == nil {
		t.Error("404 sidecar accepted")
	}
}

func TestDefaultURL(t *testing.T) {
	url := DefaultURL()
	if !strings.HasPrefix(url, "https://github.com/ben-kuhn/rms-radar/releases/latest/download/voacap-") {
		t.Fatalf("unexpected URL: %q", url)
	}
	if runtime.GOOS == "windows" && !strings.HasSuffix(url, ".zip") {
		t.Errorf("windows URL should be a zip: %q", url)
	}
	if runtime.GOOS != "windows" && !strings.HasSuffix(url, ".tar.gz") {
		t.Errorf("unix URL should be a tarball: %q", url)
	}
	pinned := BundleURL("0.2.4")
	if !strings.Contains(pinned, "/releases/download/v0.2.4/") {
		t.Errorf("release URL is not pinned: %q", pinned)
	}
	if dev := BundleURL("0.2.4-1-gabc"); !strings.Contains(dev, "/releases/latest/download/") {
		t.Errorf("dev URL is not latest: %q", dev)
	}
}

func TestVerifyMinisign(t *testing.T) {
	payload := []byte("voacap test payload v2")
	sig := []byte("untrusted comment: test v2\n" +
		"RUS7XOFIwTDVkWLY0j46UYUMmlbe2eg2MRHPpMgYiVxx9mTynm+teI9bR/Ve/ljkWsgKGtY0rwMtunAI7wzyHt18TL0Jzz6Gngk=\n" +
		"trusted comment: timestamp:1794954702\tfile:voacap-test2.txt\thashed\n" +
		"7+xT4GUUN3yJKPd4bVuUAmpavPBrKo0PuWiK2cT548DK+PCvR3cIV16KpoSI4JBYwlW6Y3OoBnq5L++75pKQAA==\n")
	if err := VerifyMinisign(payload, sig); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := VerifyMinisign([]byte("tampered"), sig); err == nil {
		t.Fatal("tampered payload accepted")
	}
	if err := VerifyMinisign(payload, []byte("garbage")); err == nil {
		t.Fatal("garbage signature accepted")
	}
}
