package sshkeys

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/andrew-d/anchor"
	"github.com/andrew-d/anchor/internal/anchortest"
)

func TestIntegration_SSHKeysWrittenToDisk(t *testing.T) {
	homeBase := t.TempDir()
	uid := uint32(os.Getuid())
	gid := uint32(os.Getgid())

	// Create the user's home directory.
	aliceHome := filepath.Join(homeBase, "alice")
	if err := os.MkdirAll(aliceHome, 0o755); err != nil {
		t.Fatal(err)
	}

	mods := make([]*Module, 3)
	cluster := anchortest.New(t, 3, func(i int) []anchor.Module {
		mods[i] = &Module{
			lookupUserFn: func(username string) (userInfo, error) {
				return userInfo{
					homeDir: filepath.Join(homeBase, username),
					uid:     uid,
					gid:     gid,
				}, nil
			},
		}
		return []anchor.Module{mods[i]}
	})

	store := mods[cluster.LeaderIndex()].store

	if err := store.Set("alice", Config{
		Keys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample1 alice@laptop",
			"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgExample2 alice@desktop",
		},
	}); err != nil {
		t.Fatalf("failed to set keys: %v", err)
	}

	// Poll for the authorized_keys file to be written by the watch loop.
	akPath := filepath.Join(aliceHome, ".ssh", "authorized_keys")
	var content string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(akPath)
		if err == nil && len(data) > 0 {
			content = string(data)
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if content == "" {
		t.Fatal("authorized_keys file was not written within timeout")
	}

	// Verify header comment.
	if !strings.HasPrefix(content, "# Managed by anchor") {
		t.Fatalf("missing header comment:\n%s", content)
	}
	if !strings.Contains(content, "# Deployment:") {
		t.Fatalf("missing deployment comment:\n%s", content)
	}
	if !strings.Contains(content, "# Last updated:") {
		t.Fatalf("missing timestamp comment:\n%s", content)
	}

	// Verify both keys are present (sorted).
	if !strings.Contains(content, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIExample1 alice@laptop") {
		t.Fatalf("missing ed25519 key:\n%s", content)
	}
	if !strings.Contains(content, "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgExample2 alice@desktop") {
		t.Fatalf("missing rsa key:\n%s", content)
	}

	// Verify ed25519 sorts before rsa.
	ed25519Idx := strings.Index(content, "ssh-ed25519")
	rsaIdx := strings.Index(content, "ssh-rsa")
	if ed25519Idx > rsaIdx {
		t.Fatalf("keys not sorted (ed25519 should come before rsa):\n%s", content)
	}

	// Verify file permissions.
	info, err := os.Stat(akPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("expected 0600, got %o", info.Mode().Perm())
	}

	// Verify .ssh directory permissions.
	sshInfo, err := os.Stat(filepath.Join(aliceHome, ".ssh"))
	if err != nil {
		t.Fatal(err)
	}
	if sshInfo.Mode().Perm() != 0o700 {
		t.Fatalf("expected .ssh 0700, got %o", sshInfo.Mode().Perm())
	}
}
