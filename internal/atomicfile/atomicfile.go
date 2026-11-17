// Package atomicfile writes files via a temporary file in the destination
// directory followed by a rename, so a crash or full disk leaves either the
// old file or the new one, never a truncated file.
package atomicfile

import (
	"fmt"
	"os"
	"path/filepath"
)

// Write replaces path with data. The parent directory is created when needed.
// The temporary file is created in the same directory so the final rename stays
// on one filesystem.
func Write(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after a successful rename

	if err := f.Chmod(perm); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename %s: %w", filepath.Base(path), err)
	}
	return nil
}
