package sshkeys

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var fixedTime = time.Date(2026, 2, 12, 21, 30, 0, 0, time.UTC)

func testModule(t *testing.T) (*Module, string) {
	t.Helper()
	homeBase := t.TempDir()

	// Use the test process's real uid/gid so the ownership checks pass
	// against directories created by this process.
	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())

	m := &Module{
		lookupUserFn: func(username string) (userInfo, error) {
			return userInfo{
				homeDir: filepath.Join(homeBase, username),
				uid:     uid,
				gid:     gid,
			}, nil
		},
		nowFn: func() time.Time { return fixedTime },
	}
	return m, homeBase
}

// createUserHome creates a fake home directory for a user.
func createUserHome(t *testing.T, homeBase, username string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(homeBase, username), 0o755); err != nil {
		t.Fatal(err)
	}
}

func readAuthorizedKeys(t *testing.T, homeBase, username string) string {
	t.Helper()
	path := filepath.Join(homeBase, username, ".ssh", "authorized_keys")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestWriteAuthorizedKeys(t *testing.T) {
	m, homeBase := testModule(t)
	createUserHome(t, homeBase, "alice")

	keys := []string{"ssh-rsa AAAA1", "ssh-ed25519 AAAA2"}
	if err := m.writeAuthorizedKeys("alice", keys); err != nil {
		t.Fatal(err)
	}

	content := readAuthorizedKeys(t, homeBase, "alice")

	// Keys should be sorted.
	expected := "# Managed by anchor - do not edit manually\n" +
		"# Last updated: 2026-02-12T21:30:00Z\n" +
		"ssh-ed25519 AAAA2\n" +
		"ssh-rsa AAAA1\n"
	if content != expected {
		t.Fatalf("unexpected content:\ngot:\n%s\nwant:\n%s", content, expected)
	}

	// Verify permissions.
	path := filepath.Join(homeBase, "alice", ".ssh", "authorized_keys")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	sshDir := filepath.Join(homeBase, "alice", ".ssh")
	dirInfo, err := os.Stat(sshDir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected 0700 for .ssh dir, got %o", dirInfo.Mode().Perm())
	}
}

func TestWriteAuthorizedKeys_Overwrite(t *testing.T) {
	m, homeBase := testModule(t)
	createUserHome(t, homeBase, "carol")

	if err := m.writeAuthorizedKeys("carol", []string{"ssh-rsa OLD"}); err != nil {
		t.Fatal(err)
	}
	if err := m.writeAuthorizedKeys("carol", []string{"ssh-rsa NEW"}); err != nil {
		t.Fatal(err)
	}

	content := readAuthorizedKeys(t, homeBase, "carol")
	if !strings.Contains(content, "ssh-rsa NEW") {
		t.Fatalf("expected new key, got:\n%s", content)
	}
	if strings.Contains(content, "ssh-rsa OLD") {
		t.Fatalf("old key should not be present:\n%s", content)
	}
}

func TestWriteAuthorizedKeys_RefusesEmptyKeys(t *testing.T) {
	m, homeBase := testModule(t)
	createUserHome(t, homeBase, "bob")

	// Write some keys first.
	if err := m.writeAuthorizedKeys("bob", []string{"ssh-rsa KEY"}); err != nil {
		t.Fatal(err)
	}

	// Attempt to clear them.
	err := m.writeAuthorizedKeys("bob", nil)
	if err == nil {
		t.Fatal("expected error when writing empty keys, got nil")
	}
	if !strings.Contains(err.Error(), "lock out") {
		t.Fatalf("expected lockout error, got: %v", err)
	}

	// Also check with empty slice.
	err = m.writeAuthorizedKeys("bob", []string{})
	if err == nil {
		t.Fatal("expected error when writing empty slice, got nil")
	}

	// Original keys should still be intact.
	content := readAuthorizedKeys(t, homeBase, "bob")
	if !strings.Contains(content, "ssh-rsa KEY") {
		t.Fatalf("original keys should still be present, got:\n%s", content)
	}
}

func TestWriteAuthorizedKeys_UserNotFound(t *testing.T) {
	m := &Module{
		lookupUserFn: func(username string) (userInfo, error) {
			return userInfo{}, fmt.Errorf("user %q not found", username)
		},
	}

	err := m.writeAuthorizedKeys("nobody", []string{"ssh-rsa KEY"})
	if err == nil {
		t.Fatal("expected error for unknown user, got nil")
	}
	if !strings.Contains(err.Error(), "user lookup failed") {
		t.Fatalf("expected user lookup error, got: %v", err)
	}
}

func TestWriteAuthorizedKeys_HomeDirNotExist(t *testing.T) {
	m := &Module{
		lookupUserFn: func(username string) (userInfo, error) {
			return userInfo{
				homeDir: "/nonexistent/path/that/does/not/exist",
				uid:     uint32(os.Getuid()),
				gid:     uint32(os.Getgid()),
			}, nil
		},
	}

	err := m.writeAuthorizedKeys("ghost", []string{"ssh-rsa KEY"})
	if err == nil {
		t.Fatal("expected error for nonexistent home dir, got nil")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("expected 'does not exist' error, got: %v", err)
	}
}

func TestWriteAuthorizedKeys_HomeDirNotWritable(t *testing.T) {
	homeBase := t.TempDir()

	// Create a directory owned by the current user but with no write bits.
	restrictedDir := filepath.Join(homeBase, "nologin")
	if err := os.MkdirAll(restrictedDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(restrictedDir, 0o755) })

	m := &Module{
		lookupUserFn: func(username string) (userInfo, error) {
			return userInfo{
				homeDir: restrictedDir,
				// Use a non-root uid that matches the dir owner (current user).
				// The dir has 0555, so owner has no write bit.
				uid: uint32(os.Getuid()),
				gid: uint32(os.Getgid()),
			}, nil
		},
	}

	err := m.writeAuthorizedKeys("locked", []string{"ssh-rsa KEY"})
	if err == nil {
		// If running as root, the check allows it (root can always write).
		if os.Getuid() == 0 {
			t.Skip("test not meaningful when running as root")
		}
		t.Fatal("expected error for non-writable home dir, got nil")
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("expected 'not writable' error, got: %v", err)
	}
}

func TestWriteAuthorizedKeys_WrongOwner(t *testing.T) {
	homeBase := t.TempDir()
	dir := filepath.Join(homeBase, "someone")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Pretend the user has a different uid than the directory owner.
	// The directory is 0700, so only the owner can write. A non-owner,
	// non-group, non-root user should be denied.
	m := &Module{
		lookupUserFn: func(username string) (userInfo, error) {
			return userInfo{
				homeDir: dir,
				uid:     99999, // definitely not the dir owner
				gid:     99999, // definitely not the dir group
			}, nil
		},
	}

	err := m.writeAuthorizedKeys("someone", []string{"ssh-rsa KEY"})
	if err == nil {
		t.Fatal("expected error for wrong-owner home dir, got nil")
	}
	if !strings.Contains(err.Error(), "not writable") {
		t.Fatalf("expected 'not writable' error, got: %v", err)
	}
}

func TestDeduplicateAndSort(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "no duplicates",
			in:   []string{"ssh-rsa BBB", "ssh-rsa AAA"},
			want: []string{"ssh-rsa AAA", "ssh-rsa BBB"},
		},
		{
			name: "with duplicates",
			in:   []string{"ssh-rsa AAA", "ssh-rsa BBB", "ssh-rsa AAA"},
			want: []string{"ssh-rsa AAA", "ssh-rsa BBB"},
		},
		{
			name: "all same",
			in:   []string{"ssh-rsa AAA", "ssh-rsa AAA", "ssh-rsa AAA"},
			want: []string{"ssh-rsa AAA"},
		},
		{
			name: "already sorted and unique",
			in:   []string{"ssh-ed25519 AAA", "ssh-rsa BBB"},
			want: []string{"ssh-ed25519 AAA", "ssh-rsa BBB"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deduplicateAndSort(tt.in)
			if len(got) != len(tt.want) {
				t.Fatalf("got %v, want %v", got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("index %d: got %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestWriteAuthorizedKeys_Timestamp(t *testing.T) {
	m, homeBase := testModule(t)
	createUserHome(t, homeBase, "dave")

	if err := m.writeAuthorizedKeys("dave", []string{"ssh-rsa KEY"}); err != nil {
		t.Fatal(err)
	}

	content := readAuthorizedKeys(t, homeBase, "dave")
	if !strings.Contains(content, "# Last updated: 2026-02-12T21:30:00Z") {
		t.Fatalf("expected timestamp comment, got:\n%s", content)
	}
}

func TestWriteAuthorizedKeys_DuplicateKeysDeduped(t *testing.T) {
	m, homeBase := testModule(t)
	createUserHome(t, homeBase, "eve")

	keys := []string{"ssh-rsa AAA", "ssh-rsa BBB", "ssh-rsa AAA"}
	if err := m.writeAuthorizedKeys("eve", keys); err != nil {
		t.Fatal(err)
	}

	content := readAuthorizedKeys(t, homeBase, "eve")
	count := strings.Count(content, "ssh-rsa AAA")
	if count != 1 {
		t.Fatalf("expected ssh-rsa AAA to appear once, appeared %d times:\n%s", count, content)
	}
}
