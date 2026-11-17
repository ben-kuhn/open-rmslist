package voacapget

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"os"
	"path/filepath"
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
