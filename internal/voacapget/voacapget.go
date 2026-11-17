// Package voacapget downloads a VOACAP distribution (the voacapl/voacapw
// engine plus its itshfbc data) for platforms without package management,
// such as macOS and Windows.
//
// Desktop builds call Fetch with the published release bundle by default; web
// and server builds stay opt-in. Every bundled download is verified against a
// SHA-256 sidecar fetched from the same release, and nothing is executed until
// the archive has been extracted into a private cache directory.
package voacapget

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// DefaultURL returns the release bundle URL for the current platform. The
// release workflow publishes voacap-<os>-<arch>.tar.gz (or .zip on Windows)
// plus a .sha256 sidecar alongside the desktop artifacts.
func DefaultURL() string {
	osName := runtime.GOOS
	if osName == "darwin" {
		osName = "macos"
	}
	ext := ".tar.gz"
	if osName == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("https://github.com/ben-kuhn/rms-radar/releases/latest/download/voacap-%s-%s%s", osName, runtime.GOARCH, ext)
}

// RemoteSHA256 fetches a .sha256 sidecar and returns its digest.
func RemoteSHA256(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("checksum %s: HTTP %s", url, resp.Status)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(b))
	if len(fields) == 0 {
		return "", fmt.Errorf("checksum %s: empty file", url)
	}
	sum := strings.ToLower(fields[0])
	if len(sum) != 64 {
		return "", fmt.Errorf("checksum %s: invalid digest %q", url, sum)
	}
	if _, err := hex.DecodeString(sum); err != nil {
		return "", fmt.Errorf("checksum %s: invalid digest %q", url, sum)
	}
	return sum, nil
}

const (
	maxDownloadBytes  = 512 << 20 // compressed archive cap
	maxExtractedBytes = 256 << 20 // total uncompressed cap (zip-bomb guard)
	maxEntries        = 20000
)

// downloadClient has a bounded timeout so a hung server cannot stall startup.
var downloadClient = &http.Client{Timeout: 120 * time.Second}

// Fetch downloads the archive at url, optionally verifies its SHA-256, extracts
// it into a staging directory, and atomically replaces destDir. An existing
// installation is reused only when no checksum was requested or its recorded
// checksum matches; a failed download/extract leaves the previous install
// intact.
func Fetch(ctx context.Context, url, destDir, wantSHA256 string) (string, string, error) {
	if destDir == "" || destDir == "." || destDir == string(filepath.Separator) {
		return "", "", fmt.Errorf("refusing to install into %q", destDir)
	}
	wantSHA256 = strings.ToLower(strings.TrimSpace(wantSHA256))

	if exe, data, err := Find(destDir); err == nil && exe != "" && data != "" {
		if wantSHA256 == "" || markerMatches(destDir, wantSHA256) {
			log.Printf("voacapl: already installed at %s", destDir)
			return exe, data, nil
		}
		log.Printf("voacapl: existing install does not match the requested checksum; reinstalling")
	}

	parent := filepath.Dir(destDir)
	if err := os.MkdirAll(parent, 0o755); err != nil {
		return "", "", err
	}

	tmp, err := download(ctx, url, parent)
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmp)

	if wantSHA256 != "" {
		if err := verifySHA256(tmp, wantSHA256); err != nil {
			return "", "", err
		}
	}

	staging, err := os.MkdirTemp(parent, ".voacap-staging-")
	if err != nil {
		return "", "", err
	}
	defer os.RemoveAll(staging)
	if err := extract(tmp, url, staging); err != nil {
		return "", "", err
	}
	if exe, data, err := Find(staging); err != nil || exe == "" || data == "" {
		if err == nil {
			err = fmt.Errorf("missing voacapl/voacapw or itshfbc")
		}
		return "", "", fmt.Errorf("archive does not contain a complete installation: %w", err)
	}

	// Publish atomically, keeping the old install as a rollback on failure.
	backup := destDir + ".old"
	_ = os.RemoveAll(backup)
	if _, err := os.Stat(destDir); err == nil {
		if err := os.Rename(destDir, backup); err != nil {
			return "", "", err
		}
	}
	if err := os.Rename(staging, destDir); err != nil {
		_ = os.Rename(backup, destDir)
		return "", "", err
	}
	_ = os.RemoveAll(backup)

	if wantSHA256 != "" {
		_ = os.WriteFile(filepath.Join(destDir, ".voacap-sha256"), []byte(wantSHA256), 0o644)
	}
	return Find(destDir)
}

func markerMatches(destDir, want string) bool {
	b, err := os.ReadFile(filepath.Join(destDir, ".voacap-sha256"))
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(string(b)), strings.TrimSpace(want))
}

func download(ctx context.Context, url, destDir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %s", url, resp.Status)
	}
	if resp.ContentLength > maxDownloadBytes {
		return "", fmt.Errorf("archive is larger than %d MiB", maxDownloadBytes>>20)
	}

	tmp, err := os.CreateTemp(destDir, "voacap-*.dl")
	if err != nil {
		return "", err
	}
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxDownloadBytes+1))
	tmp.Close()
	if err != nil {
		return "", err
	}
	if n > maxDownloadBytes {
		os.Remove(tmp.Name())
		return "", fmt.Errorf("archive exceeds %d MiB", maxDownloadBytes>>20)
	}
	log.Printf("voacapl: downloaded %.1f MB", float64(n)/(1024*1024))
	return tmp.Name(), nil
}

func verifySHA256(path, want string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return err
	}
	got := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(got, strings.TrimSpace(want)) {
		return fmt.Errorf("archive SHA-256 mismatch: got %s want %s", got, want)
	}
	return nil
}

func extract(path, url, destDir string) error {
	strip := url
	if i := strings.IndexAny(strip, "?#"); i >= 0 {
		strip = strip[:i]
	}
	lower := strings.ToLower(strip)
	switch {
	case strings.HasSuffix(lower, ".zip"):
		return extractZip(path, destDir)
	case strings.HasSuffix(lower, ".tar.gz"), strings.HasSuffix(lower, ".tgz"):
		return extractTarGz(path, destDir)
	case strings.HasSuffix(lower, ".tar"):
		return extractTarFile(path, destDir)
	default:
		return fmt.Errorf("unsupported archive type (need .zip, .tar.gz, .tgz, or .tar)")
	}
}

func extractZip(path, destDir string) error {
	zr, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer zr.Close()
	if len(zr.File) > maxEntries {
		return fmt.Errorf("archive has too many entries (%d)", len(zr.File))
	}
	budget := int64(maxExtractedBytes)
	for _, f := range zr.File {
		target, err := safeJoin(destDir, f.Name)
		if err != nil {
			return err
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		err = writeFile(target, rc, f.Mode(), &budget)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractTarGz(path, destDir string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()
	return extractTar(gz, destDir)
}

func extractTarFile(path, destDir string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTar(f, destDir)
}

func extractTar(r io.Reader, destDir string) error {
	tr := tar.NewReader(r)
	budget := int64(maxExtractedBytes)
	entries := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		entries++
		if entries > maxEntries {
			return fmt.Errorf("archive has too many entries (max %d)", maxEntries)
		}
		target, err := safeJoin(destDir, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			if err := writeFile(target, tr, os.FileMode(hdr.Mode), &budget); err != nil {
				return err
			}
		}
	}
}

// safeJoin joins base and name, ensuring the result stays within base.
func safeJoin(base, name string) (string, error) {
	clean := filepath.Clean("/" + strings.ReplaceAll(name, "\\", "/"))
	rel := strings.TrimPrefix(clean, "/")
	target := filepath.Join(base, rel)
	if target != base && !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("archive entry escapes destination: %q", name)
	}
	return target, nil
}

// writeFile writes r to path, subtracting the bytes written from budget. It
// fails if the entry would push the total extraction past the budget.
func writeFile(path string, r io.Reader, mode os.FileMode, budget *int64) error {
	if *budget <= 0 {
		return fmt.Errorf("archive exceeds extraction size limit (%d MiB)", maxExtractedBytes>>20)
	}
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o644
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	lr := &io.LimitedReader{R: r, N: *budget + 1}
	n, err := io.Copy(out, lr)
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if lr.N == 0 { // the entry tried to write past the remaining budget
		return fmt.Errorf("archive exceeds extraction size limit (%d MiB)", maxExtractedBytes>>20)
	}
	*budget -= n
	return nil
}

// Find locates the voacapl/voacapw executable and the itshfbc data directory
// within an extracted VOACAP distribution.
func Find(root string) (string, string, error) {
	var exe, data string
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() {
			switch strings.ToLower(info.Name()) {
			case "itshfbc":
				data = path
			case "coeffs":
				data = filepath.Dir(path)
			}
			return nil
		}
		if exe == "" && isEngineBinary(info.Name()) {
			exe = path
		}
		return nil
	})
	if exe == "" {
		return "", "", fmt.Errorf("no voacapl/voacapw executable found under %s", root)
	}
	if runtime.GOOS != "windows" {
		_ = os.Chmod(exe, 0o755)
	}
	return exe, data, nil
}

func isEngineBinary(name string) bool {
	switch strings.ToLower(filepath.Base(name)) {
	case "voacapl", "voacapw", "voacapl.exe", "voacapw.exe":
		return true
	}
	return false
}
