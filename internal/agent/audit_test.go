package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAuditLogAppendsEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	al := newAuditLog(path)

	al.log("00_base", "#!/bin/sh\nexit 0\n", "ok")
	al.log("10_users", "#!/bin/sh\nexit 1\n", "error")

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read audit log: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	var entry1, entry2 AuditEntry
	if err := json.Unmarshal([]byte(lines[0]), &entry1); err != nil {
		t.Fatalf("failed to parse line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &entry2); err != nil {
		t.Fatalf("failed to parse line 2: %v", err)
	}

	if entry1.Module != "00_base" {
		t.Errorf("entry1.Module: got %q, want %q", entry1.Module, "00_base")
	}
	if entry1.Status != "ok" {
		t.Errorf("entry1.Status: got %q, want %q", entry1.Status, "ok")
	}
	if entry1.Timestamp == 0 {
		t.Error("entry1.Timestamp should be non-zero")
	}

	// Verify script hash
	h := sha256.Sum256([]byte("#!/bin/sh\nexit 0\n"))
	expectedHash := hex.EncodeToString(h[:])
	if entry1.ScriptHash != expectedHash {
		t.Errorf("entry1.ScriptHash: got %q, want %q", entry1.ScriptHash, expectedHash)
	}

	if entry2.Module != "10_users" {
		t.Errorf("entry2.Module: got %q, want %q", entry2.Module, "10_users")
	}
	if entry2.Status != "error" {
		t.Errorf("entry2.Status: got %q, want %q", entry2.Status, "error")
	}
}

func TestAuditLogCreatesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "subdir", "audit.log")

	// Parent directory doesn't exist — log should silently fail
	al := newAuditLog(path)
	al.log("mod", "script", "ok")

	if _, err := os.Stat(path); err == nil {
		t.Error("expected file to not be created when parent dir missing")
	}
}

func TestAuditLogFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	al := newAuditLog(path)

	al.log("mod", "script", "ok")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("failed to stat audit log: %v", err)
	}
	if info.Mode().Perm() != 0600 {
		t.Errorf("permissions: got %04o, want 0600", info.Mode().Perm())
	}
}
