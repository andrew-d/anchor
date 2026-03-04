package module

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestModule writes a sample module script to a temp directory.
// The script implements the module interface with "metadata" and "apply" commands.
func writeTestModule(t *testing.T, dir, filename, name, description string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
    metadata)
        echo '{"name": "%s", "description": "%s"}'
        ;;
    apply)
        echo "applying %s"
        exit 0
        ;;
    *)
        echo "unknown command: $1" >&2
        exit 1
        ;;
esac
`, name, description, name)
	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte(script), 0755)
	if err != nil {
		t.Fatal(err)
	}
}

// TestLoadAllBasic verifies AC4.1: Server reads all scripts from config directory
// and extracts metadata.
func TestLoadAllBasic(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create two test modules
	writeTestModule(t, dir, "00_base", "Base System", "Basic system setup")
	writeTestModule(t, dir, "10_users", "User Management", "Manage system users")

	// Load all modules
	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Should have exactly 2 modules
	if len(modules) != 2 {
		t.Fatalf("expected 2 modules, got %d", len(modules))
	}

	// Verify they are in sorted order
	if modules[0].Filename != "00_base" || modules[1].Filename != "10_users" {
		t.Fatalf("modules not in sorted order: %v", []string{modules[0].Filename, modules[1].Filename})
	}

	// Verify first module
	if modules[0].Name != "Base System" {
		t.Errorf("expected name 'Base System', got '%s'", modules[0].Name)
	}
	if modules[0].Description != "Basic system setup" {
		t.Errorf("expected description 'Basic system setup', got '%s'", modules[0].Description)
	}
	if modules[0].Script == "" {
		t.Error("expected non-empty script")
	}

	// Verify second module
	if modules[1].Name != "User Management" {
		t.Errorf("expected name 'User Management', got '%s'", modules[1].Name)
	}
	if modules[1].Description != "Manage system users" {
		t.Errorf("expected description 'Manage system users', got '%s'", modules[1].Description)
	}
}

// TestLoadAllNewFile verifies AC4.2: Adding a new script file is detected on next
// LoadAll call without restart.
func TestLoadAllNewFile(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Load initially with one module
	writeTestModule(t, dir, "00_base", "Base", "Base setup")
	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}

	// Add a new module
	writeTestModule(t, dir, "20_networking", "Networking", "Network configuration")

	// Load again
	modules, err = loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Should now have 2 modules
	if len(modules) != 2 {
		t.Fatalf("expected 2 modules after adding new file, got %d", len(modules))
	}

	// Verify the new module is present
	found := false
	for _, m := range modules {
		if m.Filename == "20_networking" {
			found = true
			if m.Name != "Networking" {
				t.Errorf("expected name 'Networking', got '%s'", m.Name)
			}
			break
		}
	}
	if !found {
		t.Error("new module not found in LoadAll result")
	}
}

// TestLoadAllModifiedFile verifies AC4.3: Modifying a script file triggers re-parsing
// of metadata.
func TestLoadAllModifiedFile(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create initial module
	writeTestModule(t, dir, "00_base", "Old Name", "Old description")
	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if modules[0].Name != "Old Name" {
		t.Fatalf("expected initial name 'Old Name', got '%s'", modules[0].Name)
	}

	// Modify the module by overwriting with different metadata
	writeTestModule(t, dir, "00_base", "New Name", "New description")

	// Load again
	modules, err = loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Should have 1 module with updated metadata
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Name != "New Name" {
		t.Errorf("expected updated name 'New Name', got '%s'", modules[0].Name)
	}
	if modules[0].Description != "New description" {
		t.Errorf("expected updated description 'New description', got '%s'", modules[0].Description)
	}
}

// TestLoadAllRemovedFile verifies AC4.4: Removing a script file drops it from the
// available module set.
func TestLoadAllRemovedFile(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create two modules
	writeTestModule(t, dir, "00_base", "Base", "Base setup")
	writeTestModule(t, dir, "10_users", "Users", "User management")
	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(modules) != 2 {
		t.Fatalf("expected 2 modules initially, got %d", len(modules))
	}

	// Remove one file
	err = os.Remove(filepath.Join(dir, "10_users"))
	if err != nil {
		t.Fatalf("failed to remove file: %v", err)
	}

	// Load again
	modules, err = loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Should have only 1 module
	if len(modules) != 1 {
		t.Fatalf("expected 1 module after removal, got %d", len(modules))
	}
	if modules[0].Filename != "00_base" {
		t.Errorf("expected remaining module '00_base', got '%s'", modules[0].Filename)
	}
}

// TestLoadAllCaching verifies AC4.5: Unchanged files reuse cached metadata
// (hash comparison). We verify this by checking that metadata execution is only
// called once for unchanged files.
func TestLoadAllCaching(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create a separate directory for the marker file to avoid polluting the module directory
	markerDir := t.TempDir()
	markerFile := filepath.Join(markerDir, "metadata_marker")

	// Create a custom script that writes a marker when metadata is called
	filename := filepath.Join(dir, "00_base")
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
    metadata)
        echo '{"name": "Base", "description": "Base setup"}'
        echo "marker" >> "%s"
        ;;
    apply)
        echo "applying"
        exit 0
        ;;
    *)
        echo "unknown command: $1" >&2
        exit 1
        ;;
esac
`, markerFile)
	err := os.WriteFile(filename, []byte(script), 0755)
	if err != nil {
		t.Fatal(err)
	}

	// First LoadAll
	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("first LoadAll failed: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}

	// Check marker file - should have 1 line
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}
	lines := strings.Count(string(data), "\n")
	if lines == 0 {
		t.Fatal("marker file is empty after first LoadAll")
	}

	// Second LoadAll - should NOT re-execute metadata
	modules, err = loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("second LoadAll failed: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}

	// Check marker file - should still have only 1 line (not incremented)
	data, err = os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("failed to read marker file after second LoadAll: %v", err)
	}
	lines2 := strings.Count(string(data), "\n")
	if lines2 != lines {
		t.Errorf("metadata was re-executed for unchanged file: marker file grew from %d lines to %d lines", lines, lines2)
	}
}

// TestLoadAllSortedOrder verifies that modules are returned in sorted filename order
// for deterministic results.
func TestLoadAllSortedOrder(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create modules in non-sorted order
	writeTestModule(t, dir, "20_users", "Users", "User management")
	writeTestModule(t, dir, "00_base", "Base", "Base setup")
	writeTestModule(t, dir, "10_services", "Services", "Service management")

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	expected := []string{"00_base", "10_services", "20_users"}
	if len(modules) != len(expected) {
		t.Fatalf("expected %d modules, got %d", len(expected), len(modules))
	}

	for i, m := range modules {
		if m.Filename != expected[i] {
			t.Errorf("position %d: expected '%s', got '%s'", i, expected[i], m.Filename)
		}
	}
}

// TestLoadAllInvalidJSON verifies that a script with invalid metadata JSON is skipped
// (logged, not fatal) and LoadAll continues.
func TestLoadAllInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create a valid module
	writeTestModule(t, dir, "00_base", "Base", "Base setup")

	// Create an invalid module (bad JSON output)
	invalidPath := filepath.Join(dir, "99_invalid")
	invalidScript := `#!/bin/sh
case "$1" in
    metadata)
        echo 'not valid json at all'
        ;;
    apply)
        exit 0
        ;;
esac
`
	err := os.WriteFile(invalidPath, []byte(invalidScript), 0755)
	if err != nil {
		t.Fatalf("failed to write invalid script: %v", err)
	}

	// LoadAll should succeed but skip the invalid module
	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Should only have the valid module
	if len(modules) != 1 {
		t.Fatalf("expected 1 valid module (invalid should be skipped), got %d", len(modules))
	}
	if modules[0].Filename != "00_base" {
		t.Errorf("expected valid module '00_base', got '%s'", modules[0].Filename)
	}
}

// TestLoadAllEmptyDirectory verifies that an empty directory returns an empty slice
// with no error.
func TestLoadAllEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(modules) != 0 {
		t.Fatalf("expected 0 modules from empty directory, got %d", len(modules))
	}
}

// TestGetModule verifies that GetModule correctly retrieves a module from the cache
// after LoadAll has been called.
func TestGetModule(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	writeTestModule(t, dir, "00_base", "Base", "Base setup")
	writeTestModule(t, dir, "10_users", "Users", "User management")

	// Load modules first
	_, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Test GetModule for existing module
	module, found := loader.GetModule("00_base")
	if !found {
		t.Fatal("GetModule failed to find existing module")
	}
	if module.Name != "Base" {
		t.Errorf("expected name 'Base', got '%s'", module.Name)
	}

	// Test GetModule for non-existing module
	_, found = loader.GetModule("99_nonexistent")
	if found {
		t.Error("GetModule should not find non-existent module")
	}
}

// TestGetModuleBeforeLoad verifies that GetModule returns false if called before
// LoadAll.
func TestGetModuleBeforeLoad(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Don't call LoadAll, just try GetModule
	_, found := loader.GetModule("00_base")
	if found {
		t.Error("GetModule should return false on empty cache")
	}
}
