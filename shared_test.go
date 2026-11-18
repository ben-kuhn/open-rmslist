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
	"time"

	"github.com/ben-kuhn/rms-radar/internal/voacapget"
	"github.com/jedisct1/go-minisign"
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

const testMinisignPub = "RWTO6ve6avjuYCO3xYGzH5ufXuKydRlvVZyxjjqDAzzcnp0jtjf0iUAQ"
const testMinisignSecret = "RWQAAEIyAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAzur3umr47mDNXrPUfGg71sNhZfgZPA5fKFfEY5Hf+AhgjvsFl1VowyO3xYGzH5ufXuKydRlvVZyxjjqDAzzcnp0jtjf0iUAQAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA="

func signBundle(t *testing.T, archive []byte) []byte {
	t.Helper()
	sk, err := minisign.NewPrivateKey(testMinisignSecret)
	if err != nil {
		t.Fatal(err)
	}
	defer sk.Wipe()
	sig, err := sk.Sign(archive, minisign.SignOptions{TrustedComment: "test", Hashed: true})
	if err != nil {
		t.Fatal(err)
	}
	return sig.Encode()
}

func TestDownloadVOACAPSignature(t *testing.T) {
	oldKeys := append([]string(nil), voacapget.MinisignPublicKeys...)
	voacapget.MinisignPublicKeys = []string{testMinisignPub}
	defer func() { voacapget.MinisignPublicKeys = oldKeys }()

	archive := buildBundle(t)
	sum := sha256.Sum256(archive)
	sig := signBundle(t, archive)
	var mu sync.Mutex
	serveSig := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		a, s, ok := archive, sig, serveSig
		mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, ".sha256"):
			_, _ = w.Write([]byte(hex.EncodeToString(sum[:]) + "  bundle.tar.gz\n"))
		case strings.HasSuffix(r.URL.Path, ".minisig"):
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(s)
		default:
			_, _ = w.Write(a)
		}
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "voacap")
	exe, data := downloadVOACAPOpts(srv.URL+"/bundle.tar.gz", dest, true)
	if filepath.Base(exe) != "voacapl" || filepath.Base(data) != "itshfbc" {
		t.Fatalf("signed install failed: %q %q", exe, data)
	}
	installed, _, firstSum, ok := voacapget.Installed(dest)
	if !ok || installed == "" {
		t.Fatal("signed install marker missing")
	}

	// A bad signature must not replace the verified install.
	mu.Lock()
	archive = buildBundleWithData(t, "tampered")
	sum = sha256.Sum256(archive)
	sig = signBundle(t, []byte("different payload"))
	mu.Unlock()
	if gotExe, gotData := downloadVOACAPOpts(srv.URL+"/bundle.tar.gz", filepath.Join(t.TempDir(), "bad"), true); gotExe != "" || gotData != "" {
		t.Fatalf("bad signature installed: %q %q", gotExe, gotData)
	}

	// A missing signature keeps the existing install.
	mu.Lock()
	archive = buildBundleWithData(t, "v2")
	sum = sha256.Sum256(archive)
	serveSig = false
	mu.Unlock()
	exe2, data2 := downloadVOACAPOpts(srv.URL+"/bundle.tar.gz", dest, true)
	if exe2 != exe || data2 != data {
		t.Fatalf("missing signature did not keep existing install: %q %q", exe2, data2)
	}
	if _, _, after, ok := voacapget.Installed(dest); !ok || after != firstSum {
		t.Fatalf("existing install marker changed: %q -> %q", firstSum, after)
	}

	// Offline with an existing install: reuse it.
	srv.Close()
	if exe3, data3 := downloadVOACAPOpts(srv.URL+"/bundle.tar.gz", dest, true); exe3 != exe || data3 != data {
		t.Fatalf("offline signed reuse failed: %q %q", exe3, data3)
	}
}

func TestValidateVOACAPDownloadURL(t *testing.T) {
	sha := strings.Repeat("a", 64)
	cases := []struct {
		url, sha string
		ok       bool
	}{
		{"https://example.com/v.tar.gz", "", true},
		{"https://example.com/v.tar.gz", sha, true},
		{"http://example.com/v.tar.gz", sha, true},
		{"http://example.com/v.tar.gz", "", false},
		{"ftp://example.com/v.tar.gz", sha, false},
		{"file:///tmp/v.tar.gz", sha, false},
	}
	for _, c := range cases {
		err := validateVOACAPDownloadURL(c.url, c.sha)
		if (err == nil) != c.ok {
			t.Errorf("validate(%q, sha=%v) = %v, want ok=%v", c.url, c.sha != "", err, c.ok)
		}
	}
}

func TestEnvHelpers(t *testing.T) {
	t.Setenv("RMS_TEST_OR", "x")
	if got := envOr("RMS_TEST_OR", "def"); got != "x" {
		t.Errorf("envOr = %q", got)
	}
	t.Setenv("RMS_TEST_INT", "42")
	if got := envInt("RMS_TEST_INT", 1); got != 42 {
		t.Errorf("envInt = %d", got)
	}
	t.Setenv("RMS_TEST_FLOAT", "1.5")
	if got := envFloat("RMS_TEST_FLOAT", 0); got != 1.5 {
		t.Errorf("envFloat = %v", got)
	}
	t.Setenv("RMS_TEST_BOOL", "true")
	if !envBool("RMS_TEST_BOOL", false) {
		t.Error("envBool = false")
	}
	t.Setenv("RMS_TEST_DUR", "2h")
	if got := envDuration("RMS_TEST_DUR", 0); got != 2*time.Hour {
		t.Errorf("envDuration = %v", got)
	}
	if got := envInt("RMS_TEST_MISSING", 7); got != 7 {
		t.Errorf("envInt default = %d", got)
	}
	if got := splitCSV(" PUBLIC, IONHF ,, "); len(got) != 2 || got[0] != "PUBLIC" || got[1] != "IONHF" {
		t.Errorf("splitCSV = %v", got)
	}
}
