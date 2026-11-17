package propagation

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func testParams() Params {
	return Params{
		FromLat: 40, FromLon: -100,
		ToLat: 60.29, ToLon: 5.33,
		FrequencyHz: 14.102e6,
		Mode:        "VARA",
		SSN:         121,
	}
}

func readFixture(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/pat_20m.out")
	if err != nil {
		t.Skip("fixture missing")
	}
	return b
}

func TestGenerateInputFile(t *testing.T) {
	in, err := generateInputFile(testParams())
	if err != nil {
		t.Fatal(err)
	}
	s := string(in)
	for _, want := range []string{"FREQUENCY 14.102", "SUNSPOT", "CIRCUIT", "EXECUTE", "QUIT"} {
		if !strings.Contains(s, want) {
			t.Errorf("input deck missing %q:\n%s", want, s)
		}
	}

	// VHF is out of VOACAP's range; the ranker relies on this error to fall back.
	p := testParams()
	p.FrequencyHz = 145e6
	if _, err := generateInputFile(p); err != ErrFrequencyOutOfBounds {
		t.Errorf("VHF: got %v, want ErrFrequencyOutOfBounds", err)
	}
}

func TestToPrediction(t *testing.T) {
	pred, err := toPrediction(bytes.NewReader(readFixture(t)), testParams())
	if err != nil {
		t.Fatal(err)
	}
	if pred.Method != "voacap" {
		t.Errorf("Method = %q, want voacap", pred.Method)
	}
	if pred.LinkQuality < 0 || pred.LinkQuality > 100 {
		t.Errorf("LinkQuality = %d, out of range", pred.LinkQuality)
	}
}

func TestFindExecutable(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "voacapl")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := findExecutable(exe); err != nil || got != exe {
		t.Errorf("findExecutable(abs) = %q, %v", got, err)
	}
	if _, err := findExecutable(filepath.Join(dir, "missing")); err == nil {
		t.Error("expected error for missing absolute path")
	}
	if _, err := findExecutable("definitely-not-a-real-binary-xyz"); err == nil {
		t.Error("expected error for missing PATH lookup")
	}
}

func TestDefaultDataDir(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir()) // hermetic on Windows too

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(root, "itshfbc")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := defaultDataDir(filepath.Join(root, "bin", "voacapl")); got != want {
		t.Errorf("defaultDataDir = %q, want %q", got, want)
	}
}

func TestIsVoacapl(t *testing.T) {
	cases := map[string]bool{
		"voacapl": true, "/usr/bin/voacapl": true,
		"voacapw": false, "voacapl.exe": false,
	}
	for in, want := range cases {
		if got := isVoacapl(in); got != want {
			t.Errorf("isVoacapl(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestNewVOACAP(t *testing.T) {
	root := t.TempDir()
	mk := func(p string) {
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk(filepath.Join(root, "bin"))
	exe := filepath.Join(root, "bin", "voacapl")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "itshfbc")
	mk(data)

	p, err := NewVOACAP(exe, data)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Available() {
		t.Error("expected Available")
	}
	if _, err := NewVOACAP(filepath.Join(root, "missing"), data); err == nil {
		t.Error("expected error for missing executable")
	}
}

func TestNewVOACAPAPI(t *testing.T) {
	if NewVOACAPAPI("").Available() {
		t.Error("empty URL should be unavailable")
	}
	if !NewVOACAPAPI("http://example.com").Available() {
		t.Error("non-empty URL should be available")
	}
}

func TestVOACAPAPIPredict(t *testing.T) {
	fixture := readFixture(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/predict" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_, _ = w.Write(fixture)
	}))
	defer srv.Close()

	pred, err := NewVOACAPAPI(srv.URL).Predict(context.Background(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	if pred.Method != "voacap" || pred.LinkQuality < 0 || pred.LinkQuality > 100 {
		t.Errorf("bad prediction: %+v", pred)
	}
}

func TestVOACAPAPIPredictError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if _, err := NewVOACAPAPI(srv.URL).Predict(context.Background(), testParams()); err == nil {
		t.Error("expected error on HTTP 500")
	}
}

func TestVOACAPLocalPredict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake voacapl uses a shell script")
	}
	abs, err := filepath.Abs("testdata/pat_20m.out")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(root, "bin", "voacapl")
	// Fake engine: writes the fixture output into the run directory.
	script := "#!/bin/sh\ncat \"" + abs + "\" > output.out\n"
	if err := os.WriteFile(exe, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	data := filepath.Join(root, "itshfbc")
	if err := os.MkdirAll(data, 0o755); err != nil {
		t.Fatal(err)
	}

	p, err := NewVOACAP(exe, data)
	if err != nil {
		t.Fatal(err)
	}
	pred, err := p.Predict(context.Background(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	if pred.Method != "voacap" {
		t.Errorf("Method = %q", pred.Method)
	}
}

// TestVOACAPLocalRealEngine runs a real engine when the release workflow asks
// for it. It is skipped in normal CI and locally.
func TestVOACAPLocalRealEngine(t *testing.T) {
	exe := os.Getenv("RMSRADAR_TEST_VOACAP_EXECUTABLE")
	data := os.Getenv("RMSRADAR_TEST_VOACAP_DATA_DIR")
	if exe == "" || data == "" {
		t.Skip("set RMSRADAR_TEST_VOACAP_EXECUTABLE and RMSRADAR_TEST_VOACAP_DATA_DIR to run")
	}
	p, err := NewVOACAP(exe, data)
	if err != nil {
		t.Fatal(err)
	}
	pred, err := p.Predict(context.Background(), testParams())
	if err != nil {
		t.Fatal(err)
	}
	if pred.Method != "voacap" {
		t.Errorf("Method = %q", pred.Method)
	}
	if pred.LinkQuality < 0 || pred.LinkQuality > 100 {
		t.Errorf("LinkQuality = %v", pred.LinkQuality)
	}
	t.Logf("VOACAP result: LQI=%d SNR=%.1f dB", pred.LinkQuality, pred.SNRdB)
}
