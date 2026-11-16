// Package voacapget downloads a VOACAP distribution (the voacapl/voacapw
// engine plus its itshfbc data) for platforms without package management,
// such as macOS and Windows.
//
// This is strictly opt-in: the caller supplies a URL (and optionally a
// SHA-256). Downloading and executing a third-party binary is a trust
// decision, so nothing is fetched unless explicitly configured.
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
)

// Fetch downloads the archive at url, optionally verifies its SHA-256, extracts
// it into destDir, and returns the voacapl/voacapw executable path and the
// itshfbc data directory.
func Fetch(ctx context.Context, url, destDir, wantSHA256 string) (string, string, error) {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return "", "", err
	}

	tmp, err := download(ctx, url, destDir)
	if err != nil {
		return "", "", err
	}
	defer os.Remove(tmp)

	if wantSHA256 != "" {
		if err := verifySHA256(tmp, wantSHA256); err != nil {
			return "", "", err
		}
	}
	if err := extract(tmp, url, destDir); err != nil {
		return "", "", err
	}
	return Find(destDir)
}

func download(ctx context.Context, url, destDir string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download %s: HTTP %s", url, resp.Status)
	}

	tmp, err := os.CreateTemp(destDir, "voacap-*.dl")
	if err != nil {
		return "", err
	}
	n, err := io.Copy(tmp, resp.Body)
	tmp.Close()
	if err != nil {
		return "", err
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
	lower := strings.ToLower(url)
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
		err = writeFile(target, rc, f.Mode())
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
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
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
			if err := writeFile(target, tr, os.FileMode(hdr.Mode)); err != nil {
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

func writeFile(path string, r io.Reader, mode os.FileMode) error {
	perm := mode.Perm()
	if perm == 0 {
		perm = 0o644
	}
	out, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
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
