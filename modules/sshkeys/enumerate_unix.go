//go:build unix

package sshkeys

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// enumeratedUser holds the username and home directory of a system user.
type enumeratedUser struct {
	username string
	homeDir  string
}

// getentTimeout is the maximum time to wait for "getent passwd" to complete.
const getentTimeout = 30 * time.Second

// enumerateSystemUsers returns all system users by trying getent first,
// then falling back to parsing /etc/passwd directly. If getent fails,
// getentErr is non-nil so the caller can log it.
func enumerateSystemUsers(ctx context.Context) (users []enumeratedUser, getentErr error, err error) {
	users, getentErr = enumerateViaGetent(ctx)
	if getentErr == nil {
		return users, nil, nil
	}
	users, err = enumerateViaPasswdFile("/etc/passwd")
	return users, getentErr, err
}

// enumerateViaGetent runs "getent passwd" and parses the output.
func enumerateViaGetent(ctx context.Context) ([]enumeratedUser, error) {
	ctx, cancel := context.WithTimeout(ctx, getentTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "getent", "passwd").Output()
	if err != nil {
		return nil, fmt.Errorf("getent passwd: %w", err)
	}
	return parsePasswdData(string(out))
}

// enumerateViaPasswdFile reads a passwd-format file and parses it.
func enumerateViaPasswdFile(path string) ([]enumeratedUser, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parsePasswdData(string(data))
}

// parsePasswdData parses colon-delimited passwd data into enumeratedUser
// entries. It skips blank lines, comments, and lines with fewer than 6
// colon-delimited fields.
func parsePasswdData(data string) ([]enumeratedUser, error) {
	var users []enumeratedUser
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) < 6 {
			continue
		}
		users = append(users, enumeratedUser{
			username: fields[0],
			homeDir:  fields[5],
		})
	}
	return users, nil
}
