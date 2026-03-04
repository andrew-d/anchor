package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// TestReadOrCreateUUID_GeneratesNewUUID verifies that a new UUID is generated and persisted
// when the agent-id file doesn't exist (AC6.1).
func TestReadOrCreateUUID_GeneratesNewUUID(t *testing.T) {
	tmpDir := t.TempDir()
	uuid, err := readOrCreateUUID(tmpDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify UUID is non-empty
	if uuid == "" {
		t.Fatal("UUID is empty")
	}

	// Verify UUID format is v4 (8-4-4-4-12 hex with version/variant bits)
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRegex.MatchString(uuid) {
		t.Fatalf("UUID does not match v4 format: %s", uuid)
	}

	// Verify the file was created
	filePath := filepath.Join(tmpDir, "agent-id")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("agent-id file was not created at %s", filePath)
	}

	// Verify the file contains the same UUID
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != uuid {
		t.Fatalf("file content does not match returned UUID: got %s, expected %s", string(content), uuid)
	}
}

// TestReadOrCreateUUID_ReadsExistingUUID verifies that an existing UUID is read from file
// and not overwritten (AC6.2).
func TestReadOrCreateUUID_ReadsExistingUUID(t *testing.T) {
	tmpDir := t.TempDir()
	knownUUID := "12345678-1234-4234-8234-123456789012"

	// Write a known UUID to the file
	filePath := filepath.Join(tmpDir, "agent-id")
	if err := os.WriteFile(filePath, []byte(knownUUID), 0644); err != nil {
		t.Fatalf("failed to write test UUID: %v", err)
	}

	// Call readOrCreateUUID
	uuid, err := readOrCreateUUID(tmpDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify it returned the existing UUID
	if uuid != knownUUID {
		t.Fatalf("readOrCreateUUID did not return existing UUID: got %s, expected %s", uuid, knownUUID)
	}

	// Verify the file was not modified
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != knownUUID {
		t.Fatalf("file was modified: got %s, expected %s", string(content), knownUUID)
	}
}

// TestReadOrCreateUUID_CreatesParentDir verifies that parent directories are created if they don't exist.
func TestReadOrCreateUUID_CreatesParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "path", "to", "datadir")

	uuid, err := readOrCreateUUID(nestedDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify UUID is non-empty
	if uuid == "" {
		t.Fatal("UUID is empty")
	}

	// Verify the directory was created
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Fatalf("parent directories were not created at %s", nestedDir)
	}

	// Verify the file exists and contains the UUID
	filePath := filepath.Join(nestedDir, "agent-id")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != uuid {
		t.Fatalf("file content does not match returned UUID: got %s, expected %s", string(content), uuid)
	}
}
