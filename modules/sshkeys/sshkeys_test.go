package sshkeys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAuthorizedKeys(t *testing.T) {
	homeDir := t.TempDir()
	m := &Module{HomeDir: homeDir}

	keys := []string{"ssh-rsa AAAA1", "ssh-ed25519 AAAA2"}
	if err := m.writeAuthorizedKeys("alice", keys); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(homeDir, "alice", ".ssh", "authorized_keys")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	expected := "# Managed by anchor - do not edit manually\nssh-rsa AAAA1\nssh-ed25519 AAAA2\n"
	if string(data) != expected {
		t.Fatalf("unexpected content:\n%s\nexpected:\n%s", data, expected)
	}

	// Verify permissions.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", info.Mode().Perm())
	}

	sshDir := filepath.Join(homeDir, "alice", ".ssh")
	dirInfo, err := os.Stat(sshDir)
	if err != nil {
		t.Fatal(err)
	}
	if dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected 0700 for .ssh dir, got %o", dirInfo.Mode().Perm())
	}
}

func TestWriteAuthorizedKeys_Clear(t *testing.T) {
	homeDir := t.TempDir()
	m := &Module{HomeDir: homeDir}

	// Write some keys first.
	if err := m.writeAuthorizedKeys("bob", []string{"ssh-rsa KEY"}); err != nil {
		t.Fatal(err)
	}

	// Clear them (simulating a delete event).
	if err := m.writeAuthorizedKeys("bob", nil); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(homeDir, "bob", ".ssh", "authorized_keys")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 0 {
		t.Fatalf("expected empty file after clear, got %q", data)
	}
}

func TestWriteAuthorizedKeys_Overwrite(t *testing.T) {
	homeDir := t.TempDir()
	m := &Module{HomeDir: homeDir}

	if err := m.writeAuthorizedKeys("carol", []string{"ssh-rsa OLD"}); err != nil {
		t.Fatal(err)
	}
	if err := m.writeAuthorizedKeys("carol", []string{"ssh-rsa NEW"}); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(homeDir, "carol", ".ssh", "authorized_keys")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	expected := "# Managed by anchor - do not edit manually\nssh-rsa NEW\n"
	if string(data) != expected {
		t.Fatalf("unexpected content after overwrite:\n%s", data)
	}
}
