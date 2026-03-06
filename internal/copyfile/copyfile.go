//go:build unix

package copyfile

import (
	"fmt"
	"io"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// Clone copies file data from src to dst using open file descriptors. It tries
// a FICLONE reflink first for instant copy-on-write, falling back to io.Copy
// (which uses copy_file_range internally). Both files must already be open.
func Clone(dst, src *os.File) error {
	err := unix.IoctlSetInt(int(dst.Fd()), unix.FICLONE, int(src.Fd()))
	if err == nil {
		return nil
	}
	// FICLONE failed — fall back to io.Copy.
	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("copy data: %w", err)
	}
	return nil
}

// CopyFile copies the file at src to dst. It tries a FICLONE reflink first
// for instant copy-on-write, falling back to io.Copy (which uses
// copy_file_range internally). After copying data, it copies the file mode
// and modification time. Ownership (uid/gid) is copied on a best-effort
// basis — failure is silently ignored.
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
	srcFile.Close()

	// Set metadata via the open file descriptor.
	if err := dstFile.Chmod(srcInfo.Mode()); err != nil {
		return fmt.Errorf("chmod: %w", err)
	}
	stat := srcInfo.Sys().(*syscall.Stat_t)
	_ = dstFile.Chown(int(stat.Uid), int(stat.Gid)) // best-effort
	if err := dstFile.Close(); err != nil {
		return fmt.Errorf("close destination: %w", err)
	}

	// Chtimes must use the path — there is no File.Chtimes.
	if err := os.Chtimes(dst, time.Now(), srcInfo.ModTime()); err != nil {
		return fmt.Errorf("chtimes: %w", err)
	}

	return nil
}
