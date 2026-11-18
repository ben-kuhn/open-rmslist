package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/cmsapi"
)

func TestStartRejectsInvalidRefresh(t *testing.T) {
	if _, err := Start(context.Background(), Config{Refresh: 0, Listen: "127.0.0.1:0"}); err == nil {
		t.Fatal("expected an error for a zero refresh interval")
	}
	if _, err := Start(context.Background(), Config{Refresh: -1, Listen: "127.0.0.1:0"}); err == nil {
		t.Fatal("expected an error for a negative refresh interval")
	}
}

func TestStartRejectsInvalidServiceCodes(t *testing.T) {
	_, err := Start(context.Background(), Config{
		Refresh:      time.Hour,
		Listen:       "127.0.0.1:0",
		ServiceCodes: []string{"../../etc/passwd"},
		WebFS:        fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err == nil {
		t.Fatal("expected an error for an invalid service code")
	}
}

func TestServiceCodesFromDiskValidates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service_codes.json")
	fallback := []string{"PUBLIC"}
	if got := serviceCodesFromDisk(path, fallback); len(got) != 1 || got[0] != "PUBLIC" {
		t.Fatalf("missing file = %v", got)
	}
	if err := os.WriteFile(path, []byte(`["../pwned"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := serviceCodesFromDisk(path, fallback); len(got) != 1 || got[0] != "PUBLIC" {
		t.Fatalf("invalid file = %v, want fallback", got)
	}
	if err := os.WriteFile(path, []byte(`[" mercury ","PUBLIC"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := serviceCodesFromDisk(path, fallback)
	if len(got) != 2 || got[0] != "MERCURY" || got[1] != "PUBLIC" {
		t.Fatalf("valid file = %v", got)
	}
}

func TestStartAndClose(t *testing.T) {
	dir := t.TempDir()
	a, err := Start(context.Background(), Config{
		Version:      "test",
		Refresh:      time.Hour,
		Listen:       "127.0.0.1:0",
		CacheDir:     dir,
		ServiceCodes: []string{"PUBLIC"},
		WebFS:        fstest.MapFS{"index.html": &fstest.MapFile{Data: []byte("ok")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(a.URL(), "http://127.0.0.1:") {
		t.Fatalf("URL = %q", a.URL())
	}
	a.Close()
}

func TestServiceCodesFromDiskMigratesLegacyDefault(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "service_codes.json")
	fallback := append([]string(nil), cmsapi.DefaultServiceCodes...)
	if err := os.WriteFile(path, []byte(`["PUBLIC","EMCOMM"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	got := serviceCodesFromDisk(path, fallback)
	if len(got) != len(fallback) {
		t.Fatalf("legacy default was not migrated: %v", got)
	}
	if _, err := os.Stat(path + ".migrated"); err != nil {
		t.Fatalf("migration marker missing: %v", err)
	}
	// After migration, an explicit choice of just PUBLIC/EMCOMM is respected.
	if err := os.WriteFile(path, []byte(`["PUBLIC","EMCOMM"]`), 0o644); err != nil {
		t.Fatal(err)
	}
	got = serviceCodesFromDisk(path, fallback)
	if len(got) != 2 || got[0] != "EMCOMM" || got[1] != "PUBLIC" {
		t.Fatalf("explicit choice was overwritten: %v", got)
	}
}
