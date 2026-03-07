//go:build linux

package copyfile

import (
	"log/slog"
	"os"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

var logReflink sync.Once

func init() {
	tryReflink = tryReflinkLinux
	copyOwnership = copyOwnershipLinux
}

// tryReflink attempts a FICLONE ioctl for instant copy-on-write.
// Returns true if the reflink succeeded.
func tryReflinkLinux(dst, src *os.File) bool {
	err := unix.IoctlSetInt(int(dst.Fd()), unix.FICLONE, int(src.Fd()))
	if err == nil {
		logReflink.Do(func() {
			slog.Debug("FICLONE reflink supported")
		})
		return true
	}
	logReflink.Do(func() {
		slog.Debug("FICLONE reflink not supported, using fallback copy", "error", err)
	})
	return false
}

// copyOwnership copies uid/gid from the source stat to the destination file.
func copyOwnershipLinux(dst *os.File, srcInfo os.FileInfo) error {
	stat := srcInfo.Sys().(*syscall.Stat_t)
	return dst.Chown(int(stat.Uid), int(stat.Gid))
}
