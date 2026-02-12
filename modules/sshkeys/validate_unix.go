//go:build unix

package sshkeys

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"syscall"
)

// validateHomeDir checks that homeDir exists, is a directory, and that the
// user identified by uid/gid has write permission according to the directory's
// ownership and mode bits. This catches common patterns like /var/empty or
// /nonexistent that prevent login.
func validateHomeDir(homeDir, username string, uid, gid uint32) error {
	info, err := os.Stat(homeDir)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("home directory %q for user %q does not exist", homeDir, username)
	}
	if err != nil {
		return fmt.Errorf("cannot stat home directory %q for user %q: %w", homeDir, username, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("home directory %q for user %q is not a directory", homeDir, username)
	}

	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// Can't extract ownership; skip the ownership check.
		return nil
	}

	dirUID := stat.Uid
	dirGID := stat.Gid
	mode := info.Mode().Perm()

	if !userCanWrite(uid, gid, dirUID, dirGID, mode) {
		return fmt.Errorf(
			"home directory %q for user %q is not writable by the user "+
				"(dir owned by uid=%d gid=%d mode=%o, user has uid=%d gid=%d)",
			homeDir, username, dirUID, dirGID, mode, uid, gid,
		)
	}

	return nil
}

// userCanWrite returns true if a user with the given uid/gid can write to a
// file/directory owned by dirUID/dirGID with the given permission bits.
func userCanWrite(uid, gid, dirUID, dirGID uint32, mode fs.FileMode) bool {
	switch {
	case uid == 0:
		// Root can always write.
		return true
	case uid == dirUID:
		return mode&0o200 != 0
	case gid == dirGID:
		return mode&0o020 != 0
	default:
		return mode&0o002 != 0
	}
}
