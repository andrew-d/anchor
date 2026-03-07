package module

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

// TestLoadErrors_TracksErrors verifies that broken modules appear in LoadErrors()
// while valid modules still load normally.
func TestLoadErrors_TracksErrors(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create a valid module
	writeTestModule(t, dir, "00_base", "Base", "Base setup")

	// Create a broken module (invalid JSON)
	brokenScript := "#!/bin/sh\ncase \"$1\" in\n    metadata) echo 'not json' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "99_broken"), []byte(brokenScript), 0755); err != nil {
		t.Fatal(err)
	}

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(modules) != 1 {
		t.Fatalf("expected 1 valid module, got %d", len(modules))
	}
	if modules[0].Filename != "00_base" {
		t.Errorf("expected valid module '00_base', got '%s'", modules[0].Filename)
	}

	errors := loader.LoadErrors()
	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}
	if errors[0].Filename != "99_broken" {
		t.Errorf("expected error for '99_broken', got '%s'", errors[0].Filename)
	}
	if errors[0].Error == "" {
		t.Error("expected non-empty error message")
	}
}

// TestLoadErrors_ClearedOnFix verifies that fixing a broken module moves it
// from errors to the valid module set.
func TestLoadErrors_ClearedOnFix(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create a broken module
	brokenScript := "#!/bin/sh\ncase \"$1\" in\n    metadata) echo 'bad' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "10_broken"), []byte(brokenScript), 0755); err != nil {
		t.Fatal(err)
	}

	_, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(loader.LoadErrors()) != 1 {
		t.Fatalf("expected 1 error before fix, got %d", len(loader.LoadErrors()))
	}

	// Fix the module
	writeTestModule(t, dir, "10_broken", "Fixed", "Now works")

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(modules) != 1 {
		t.Fatalf("expected 1 valid module after fix, got %d", len(modules))
	}
	if modules[0].Filename != "10_broken" {
		t.Errorf("expected '10_broken', got '%s'", modules[0].Filename)
	}
	if len(loader.LoadErrors()) != 0 {
		t.Errorf("expected 0 errors after fix, got %d", len(loader.LoadErrors()))
	}
}

// TestLoadErrors_ClearedOnDelete verifies that deleting a broken module file
// removes its error.
func TestLoadErrors_ClearedOnDelete(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create a broken module
	brokenScript := "#!/bin/sh\ncase \"$1\" in\n    metadata) echo 'bad' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "10_broken"), []byte(brokenScript), 0755); err != nil {
		t.Fatal(err)
	}

	_, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(loader.LoadErrors()) != 1 {
		t.Fatalf("expected 1 error, got %d", len(loader.LoadErrors()))
	}

	// Delete the broken file
	if err := os.Remove(filepath.Join(dir, "10_broken")); err != nil {
		t.Fatal(err)
	}

	_, err = loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(loader.LoadErrors()) != 0 {
		t.Errorf("expected 0 errors after delete, got %d", len(loader.LoadErrors()))
	}
}

// TestLoadAllWithArtifacts verifies that .d directories are walked and artifacts
// are collected with correct hashes.
func TestLoadAllWithArtifacts(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create a module
	writeTestModule(t, dir, "00_base", "Base System", "Basic system setup")

	// Create its .d directory with files
	dDir := filepath.Join(dir, "00_base.d")
	if err := os.MkdirAll(filepath.Join(dDir, "subdir"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dDir, "config.txt"), []byte("hello\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dDir, "subdir", "app.conf"), []byte("key=val\n"), 0644); err != nil {
		t.Fatal(err)
	}

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}

	arts := modules[0].Artifacts
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(arts))
	}

	// Sorted by RelPath
	if arts[0].RelPath != "config.txt" {
		t.Errorf("expected first artifact 'config.txt', got '%s'", arts[0].RelPath)
	}
	if arts[1].RelPath != "subdir/app.conf" {
		t.Errorf("expected second artifact 'subdir/app.conf', got '%s'", arts[1].RelPath)
	}

	// Verify hashes are non-empty and have correct length
	for _, art := range arts {
		if len(art.Hash) != 64 {
			t.Errorf("expected 64-char hash for %s, got %d chars", art.RelPath, len(art.Hash))
		}
		if art.Size <= 0 {
			t.Errorf("expected positive size for %s, got %d", art.RelPath, art.Size)
		}
		if art.DiskPath == "" {
			t.Errorf("expected non-empty DiskPath for %s", art.RelPath)
		}
	}
}

// TestGetArtifactByHash verifies that artifacts can be looked up by hash.
func TestGetArtifactByHash(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	writeTestModule(t, dir, "00_base", "Base", "Base setup")

	dDir := filepath.Join(dir, "00_base.d")
	if err := os.MkdirAll(dDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dDir, "file.txt"), []byte("content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(modules[0].Artifacts) != 1 {
		t.Fatalf("expected 1 artifact, got %d", len(modules[0].Artifacts))
	}

	hash := modules[0].Artifacts[0].Hash
	art, ok := loader.GetArtifactByHash(hash)
	if !ok {
		t.Fatal("GetArtifactByHash returned false for known hash")
	}
	if art.RelPath != "file.txt" {
		t.Errorf("expected RelPath 'file.txt', got '%s'", art.RelPath)
	}

	// Unknown hash
	_, ok = loader.GetArtifactByHash("0000000000000000000000000000000000000000000000000000000000000000")
	if ok {
		t.Error("GetArtifactByHash should return false for unknown hash")
	}
}

// TestLoadAllArtifactPermissions verifies that artifact file permissions are captured.
func TestLoadAllArtifactPermissions(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	writeTestModule(t, dir, "00_base", "Base", "Base setup")

	dDir := filepath.Join(dir, "00_base.d")
	if err := os.MkdirAll(dDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a regular file (0644) and an executable file (0755)
	if err := os.WriteFile(filepath.Join(dDir, "config.txt"), []byte("cfg\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dDir, "helper.sh"), []byte("#!/bin/sh\necho hi\n"), 0755); err != nil {
		t.Fatal(err)
	}

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}

	arts := modules[0].Artifacts
	if len(arts) != 2 {
		t.Fatalf("expected 2 artifacts, got %d", len(arts))
	}

	// Sorted: config.txt, helper.sh
	if arts[0].Mode != 0644 {
		t.Errorf("config.txt mode: got %04o, want 0644", arts[0].Mode)
	}
	if arts[1].Mode != 0755 {
		t.Errorf("helper.sh mode: got %04o, want 0755", arts[1].Mode)
	}
}

// TestLoadAllModuleWithoutArtifacts verifies that modules without .d directories
// still work and have empty artifact slices.
func TestLoadAllModuleWithoutArtifacts(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	writeTestModule(t, dir, "00_base", "Base", "Base setup")

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if len(modules[0].Artifacts) != 0 {
		t.Errorf("expected 0 artifacts for module without .d dir, got %d", len(modules[0].Artifacts))
	}
}

// TestLoadErrors_BrokenArtifactDir verifies that a module whose .d directory
// cannot be walked is treated as an error (not silently loaded without artifacts).
func TestLoadErrors_BrokenArtifactDir(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	// Create a valid module
	writeTestModule(t, dir, "00_good", "Good", "Works fine")

	// Create another module with a broken .d directory
	writeTestModule(t, dir, "10_broken_art", "Broken Art", "Has bad .d dir")
	dDir := filepath.Join(dir, "10_broken_art.d")
	if err := os.MkdirAll(dDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Create an unreadable file inside the .d dir to cause walkArtifacts to fail
	unreadable := filepath.Join(dDir, "secret.conf")
	if err := os.WriteFile(unreadable, []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	// Make the .d directory unreadable so WalkDir fails
	if err := os.Chmod(dDir, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dDir, 0755) }) // restore for cleanup

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	// Only the good module should be returned
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Filename != "00_good" {
		t.Errorf("expected '00_good', got '%s'", modules[0].Filename)
	}

	// The broken one should appear in errors
	errors := loader.LoadErrors()
	if len(errors) != 1 {
		t.Fatalf("expected 1 error, got %d", len(errors))
	}
	if errors[0].Filename != "10_broken_art" {
		t.Errorf("expected error for '10_broken_art', got '%s'", errors[0].Filename)
	}
}

// TestLoadAllConcurrentDedup verifies that concurrent LoadAll calls are
// deduplicated via singleflight — only one metadata execution occurs.
func TestLoadAllConcurrentDedup(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	markerDir := t.TempDir()
	markerFile := filepath.Join(markerDir, "metadata_marker")

	// Create a module that appends to a marker file on metadata execution
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
    metadata)
        echo '{"name": "Base", "description": "Base setup"}'
        echo "marker" >> "%s"
        ;;
    apply)
        exit 0
        ;;
esac
`, markerFile)
	if err := os.WriteFile(filepath.Join(dir, "00_base"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	results := make([][]Module, goroutines)
	errs := make([]error, goroutines)

	for i := range goroutines {
		go func(idx int) {
			defer wg.Done()
			results[idx], errs[idx] = loader.LoadAll(t.Context())
		}(i)
	}
	wg.Wait()

	// All goroutines should succeed with the same correct result
	for i := range goroutines {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: LoadAll failed: %v", i, errs[i])
		}
		if len(results[i]) != 1 {
			t.Fatalf("goroutine %d: expected 1 module, got %d", i, len(results[i]))
		}
		if results[i][0].Filename != "00_base" {
			t.Fatalf("goroutine %d: expected filename '00_base', got '%s'", i, results[i][0].Filename)
		}
	}

	// The metadata script should have executed only once
	data, err := os.ReadFile(markerFile)
	if err != nil {
		t.Fatalf("failed to read marker file: %v", err)
	}
	lines := strings.Count(string(data), "\n")
	if lines != 1 {
		t.Errorf("expected metadata to execute once, but marker file has %d lines", lines)
	}
}

// TestLoadAllCriticalTrue verifies that a module with "critical": true in its
// metadata JSON has Critical set to true on the loaded Module.
func TestLoadAllCriticalTrue(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	script := `#!/bin/sh
case "$1" in
    metadata) echo '{"name": "Critical Module", "description": "Must not fail", "critical": true}' ;;
    apply) exit 0 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "00_critical"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if !modules[0].Critical {
		t.Error("expected Critical to be true")
	}
}

// TestLoadAllCriticalDefaultFalse verifies that omitting "critical" from
// metadata JSON defaults to false.
func TestLoadAllCriticalDefaultFalse(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	writeTestModule(t, dir, "00_base", "Base", "Base setup")

	modules, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}
	if len(modules) != 1 {
		t.Fatalf("expected 1 module, got %d", len(modules))
	}
	if modules[0].Critical {
		t.Error("expected Critical to default to false")
	}
}

// TestLoadErrors_SortedByFilename verifies that errors are returned sorted
// by filename.
func TestLoadErrors_SortedByFilename(t *testing.T) {
	dir := t.TempDir()
	loader := NewLoader(dir)

	brokenScript := "#!/bin/sh\ncase \"$1\" in\n    metadata) echo 'bad' ;;\nesac\n"
	for _, name := range []string{"20_z_broken", "10_a_broken"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(brokenScript), 0755); err != nil {
			t.Fatal(err)
		}
	}

	_, err := loader.LoadAll(t.Context())
	if err != nil {
		t.Fatalf("LoadAll failed: %v", err)
	}

	errors := loader.LoadErrors()
	if len(errors) != 2 {
		t.Fatalf("expected 2 errors, got %d", len(errors))
	}
	if errors[0].Filename != "10_a_broken" {
		t.Errorf("expected first error '10_a_broken', got '%s'", errors[0].Filename)
	}
	if errors[1].Filename != "20_z_broken" {
		t.Errorf("expected second error '20_z_broken', got '%s'", errors[1].Filename)
	}
}
