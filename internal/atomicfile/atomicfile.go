// Package atomicfile provides safe file replacement via write-to-temp,
// fsync, and atomic rename. This ensures that readers never observe a
// partially-written file.
package atomicfile

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Options controls the ownership and permissions of the written file.
type Options struct {
	// Mode is the file permission bits. Defaults to 0o644 if zero.
	Mode os.FileMode

	// UID and GID, if non-nil, are passed to os.Chown before the
	// rename. This is useful when running as root and writing files
	// owned by another user.
	UID *int
	GID *int
}

// WriteFile atomically replaces the file at path with data.
//
// It writes to a temporary file in the same directory as path, calls fsync,
// applies ownership/permissions from opts, and renames the temp file into
// place. If any step fails the temp file is removed and an error is returned.
func WriteFile(path string, data []byte, opts Options) error {
	return Write(path, func(w io.Writer) error {
		_, err := w.Write(data)
		return err
	}, opts)
}

// Write atomically replaces the file at path. The fn callback receives a
// writer for the new file contents. After fn returns successfully the file
// is fsynced, ownership/permissions are applied, and the temp file is
// renamed into place.
func Write(path string, fn func(w io.Writer) error, opts Options) (retErr error) {
	dir := filepath.Dir(path)

	f, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("atomicfile: create temp: %w", err)
	}
	tmpPath := f.Name()
	defer func() {
		if retErr != nil {
			f.Close()
			os.Remove(tmpPath)
		}
	}()

	if err := fn(f); err != nil {
		return fmt.Errorf("atomicfile: write: %w", err)
	}
	if err := f.Sync(); err != nil {
		return fmt.Errorf("atomicfile: fsync: %w", err)
	}

	mode := opts.Mode
	if mode == 0 {
		mode = 0o644
	}
	if err := f.Chmod(mode); err != nil {
		return fmt.Errorf("atomicfile: chmod: %w", err)
	}

	if opts.UID != nil || opts.GID != nil {
		uid, gid := -1, -1
		if opts.UID != nil {
			uid = *opts.UID
		}
		if opts.GID != nil {
			gid = *opts.GID
		}
		if err := f.Chown(uid, gid); err != nil {
			return fmt.Errorf("atomicfile: chown: %w", err)
		}
	}

	if err := f.Close(); err != nil {
		return fmt.Errorf("atomicfile: close: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("atomicfile: rename: %w", err)
	}
	return nil
}
