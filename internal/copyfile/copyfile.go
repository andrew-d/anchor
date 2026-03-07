package copyfile

import (
	"fmt"
	"io"
	"os"
	"time"
)

// tryReflink, if non-nil, attempts a reflink clone from src to dst.
// Returns true if the reflink succeeded and no further copying is needed.
// Set by platform-specific init() on Linux.
var tryReflink func(dst, src *os.File) bool

// copyOwnership, if non-nil, copies uid/gid from srcInfo to the open dst file.
// Set by platform-specific init() on Linux.
var copyOwnership func(dst *os.File, srcInfo os.FileInfo) error

// Clone copies file data from src to dst using open file descriptors.
// On Linux it tries a FICLONE reflink first for instant copy-on-write,
// falling back to io.Copy (which uses copy_file_range internally).
// On other platforms it uses io.Copy directly.
func Clone(dst, src *os.File) error {
	if tryReflink != nil && tryReflink(dst, src) {
		return nil
	}
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}
	return nil
}

// CopyFile copies the file at src to dst, preserving mode and modification
// time. On Linux it also attempts FICLONE reflinks and preserves file
// ownership (uid/gid) on a best-effort basis.
func CopyFile(src, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat source: %w", err)
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open source: %w", err)
	}
	defer srcFile.Close()

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, srcInfo.Mode())
	if err != nil {
		return fmt.Errorf("create destination: %w", err)
	}
	defer dstFile.Close()

	if err := Clone(dstFile, srcFile); err != nil {
		return err
	}
	if err := srcFile.Close(); err != nil {
		return err
	}

	// Set metadata via the open file descriptor.
	if err := dstFile.Chmod(srcInfo.Mode()); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	if copyOwnership != nil {
		if err := copyOwnership(dstFile, srcInfo); err != nil {
			// best effort
		}
	}
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}

	// Chtimes must use the path — there is no File.Chtimes.
	if err := os.Chtimes(dst, time.Now(), srcInfo.ModTime()); err != nil {
		return fmt.Errorf("chtimes: %w", err)
	}

	return nil
}
