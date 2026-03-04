# Anchor MVP Implementation Plan — Phase 3: Module Loading

**Goal:** Server can read module scripts from a config directory, hash them, cache metadata, and detect changes between calls.

**Architecture:** `internal/module/` package with a `Loader` struct. On each call to `LoadAll()`, the loader scans a directory, SHA-256 hashes each file, compares against an in-memory cache, and only re-parses metadata for changed or new files. Removed files are dropped from cache.

**Tech Stack:** Go 1.26, stdlib only (`crypto/sha256`, `os`, `os/exec`, `encoding/json`)

**Scope:** 7 phases from original design (phase 3 of 7)

**Codebase verified:** 2026-03-03 — confirmed greenfield, Phase 1 creates scaffolding, Phase 2 creates DB layer

---

## Acceptance Criteria Coverage

This phase implements and tests:

### anchor-mvp.AC4: Module Loading
- **anchor-mvp.AC4.1 Success:** Server reads all scripts from config directory and extracts metadata
- **anchor-mvp.AC4.2 Success:** Adding a new script file is detected on next check-in without restart
- **anchor-mvp.AC4.3 Success:** Modifying a script file triggers re-parsing of metadata
- **anchor-mvp.AC4.4 Success:** Removing a script file drops it from the available module set
- **anchor-mvp.AC4.5 Success:** Unchanged files reuse cached metadata (hash comparison)

---

<!-- START_SUBCOMPONENT_A (tasks 1-3) -->
<!-- START_TASK_1 -->
### Task 1: Create Module type and Loader struct

**Files:**
- Create: `internal/module/module.go`

**Implementation:**

Define the `Module` type and `Loader` struct:

```go
package module

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
)

// Module represents a loaded module script with its metadata.
// IMPORTANT: The module naming convention uses the FILENAME (e.g., "00_base")
// as the canonical identifier across the system — in module_assignments, API
// responses, and cache keys. The metadata Name/Description are display-only fields.
type Module struct {
	Filename    string // Canonical identifier (the script filename, e.g., "00_base")
	Name        string // Display name from metadata JSON
	Description string // Display description from metadata JSON
	Script      string // Full script content
	hash        string // SHA-256 hex digest of file contents
}

// Metadata is the JSON structure returned by a module's "metadata" command.
type Metadata struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Loader reads module scripts from a directory and caches their metadata.
type Loader struct {
	dir   string
	mu    sync.Mutex
	cache map[string]*Module // keyed by filename
}

// NewLoader creates a Loader that reads from the given directory.
func NewLoader(dir string) *Loader {
	return &Loader{
		dir:   dir,
		cache: make(map[string]*Module),
	}
}
```

Implement `LoadAll() ([]Module, error)` method:

1. Read all regular files from `l.dir` using `os.ReadDir`
2. For each file:
   - Read contents, compute SHA-256 hex digest
   - If hash matches cached entry for this filename, reuse cached module (AC4.5)
   - If hash differs or file is new: write to a temp file, `chmod +x`, execute with `metadata` argument, parse JSON stdout into `Metadata` struct (AC4.1, AC4.2, AC4.3)
   - Store in new cache map
3. Replace old cache with new cache (files not in directory are dropped — AC4.4)
4. Return sorted slice of all modules (sorted by filename for deterministic order)

For extracting metadata, shell out to the script:
```go
cmd := exec.Command(tmpFile, "metadata")
output, err := cmd.Output()
```

Parse the JSON output into `Metadata`. If metadata extraction fails, log a warning and skip the file.

The `Script` field of each `Module` should contain the full file contents (read from disk), so the server can send it to agents.

Also implement `GetModule(filename string) (Module, bool)` to look up a single module by its filename (the canonical identifier used in module assignments).

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: add module Loader with hash-based caching`
<!-- END_TASK_1 -->

<!-- START_TASK_2 -->
### Task 2: Create test helper script generator

**Files:**
- Create: `internal/module/module_test.go`

**Implementation:**

Create a test helper function that writes sample module scripts to a temp directory. These scripts implement the module interface:

```go
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
```

This helper is used by tests in Task 3.

**Verification:**

Run: `go build ./...`
Expected: Compiles (test file only builds during `go test`)

**Commit:** `test: add module test helper for writing sample scripts`
<!-- END_TASK_2 -->

<!-- START_TASK_3 -->
### Task 3: Test Loader with all AC4 scenarios

**Verifies:** anchor-mvp.AC4.1, anchor-mvp.AC4.2, anchor-mvp.AC4.3, anchor-mvp.AC4.4, anchor-mvp.AC4.5

**Files:**
- Modify: `internal/module/module_test.go`
- Test file: `internal/module/module_test.go` (integration — real filesystem with temp dirs)

**Testing:**

All tests use `t.TempDir()` for the module scripts directory. Tests should use the `writeTestModule` helper from Task 2.

Tests must verify each AC listed above:

- **anchor-mvp.AC4.1:** Create a directory with two module scripts (e.g., `00_base` and `10_users`). Call `LoadAll()`. Verify both modules are returned with correct name, description, and script content.
- **anchor-mvp.AC4.2:** Call `LoadAll()` with one module. Add a second script file to the directory. Call `LoadAll()` again. Verify the new module appears in the result.
- **anchor-mvp.AC4.3:** Call `LoadAll()` with one module. Overwrite the script file with different metadata (changed name/description). Call `LoadAll()` again. Verify the returned module has the updated metadata.
- **anchor-mvp.AC4.4:** Call `LoadAll()` with two modules. Remove one script file. Call `LoadAll()` again. Verify only the remaining module is returned.
- **anchor-mvp.AC4.5:** Call `LoadAll()` twice without changing any files. Verify both calls return the same modules. To confirm caching is working, check that the second call does not re-execute the `metadata` command. One approach: track execution by having the test script write to a marker file on metadata calls, and verify the marker file is written only once for unchanged files.

Also test:
- Modules are returned in sorted filename order
- Script with invalid metadata JSON is skipped (logged, not fatal)
- Empty directory returns empty slice, no error

**Verification:**

Run: `go test ./internal/module/ -count=1 -v`
Expected: All tests pass

Run: `go test ./... -count=1`
Expected: All tests pass (including Phase 2 db tests)

**Commit:** `test: add comprehensive module Loader tests`
<!-- END_TASK_3 -->
<!-- END_SUBCOMPONENT_A -->
