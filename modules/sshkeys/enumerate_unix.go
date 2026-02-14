//go:build unix

package sshkeys

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// enumeratedUser holds the username and home directory of a system user.
type enumeratedUser struct {
	username string
	homeDir  string
}

// enumerateSystemUsers returns all system users by trying getent first,
// then falling back to parsing /etc/passwd directly.
func enumerateSystemUsers() ([]enumeratedUser, error) {
	users, err := enumerateViaGetent()
	if err == nil {
		return users, nil
	}
	return enumerateViaPasswdFile("/etc/passwd")
}

// enumerateViaGetent runs "getent passwd" and parses the output.
func enumerateViaGetent() ([]enumeratedUser, error) {
	out, err := exec.Command("getent", "passwd").Output()
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
