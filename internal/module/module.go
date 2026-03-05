package module

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

// ModuleError represents a module that failed to load.
type ModuleError struct {
	Filename string
	Error    string
}

// Loader reads module scripts from a directory and caches their metadata.
type Loader struct {
	dir      string
	loadMu   sync.Mutex         // serializes LoadAll calls so concurrent loads don't race
	mu       sync.Mutex         // protects cache reads/writes
	cache    map[string]*Module // keyed by filename
	errCache map[string]string  // keyed by filename, value is error message
}

// NewLoader creates a Loader that reads from the given directory.
func NewLoader(dir string) *Loader {
	return &Loader{
		dir:      dir,
		cache:    make(map[string]*Module),
		errCache: make(map[string]string),
	}
}

// LoadAll reads all module scripts from the loader's directory, hashing them
// and comparing against cached entries. Only files with changed hashes or new
// files are re-parsed for metadata.
//
// File I/O and metadata command execution happen outside the lock; only the
// final cache swap is synchronized.
func (l *Loader) LoadAll(ctx context.Context) ([]Module, error) {
	// Serialize loads so two concurrent callers don't race on the cache.
	l.loadMu.Lock()
	defer l.loadMu.Unlock()

	// Snapshot the current cache so we can compare hashes without holding
	// mu during I/O.
	l.mu.Lock()
	oldCache := l.cache
	l.mu.Unlock()

	// Read all files from the directory
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("reading module directory: %w", err)
	}

	// Build a new cache with modules from the current state of the directory.
	// All file reads and metadata execution happen here, outside the lock.
	newCache := make(map[string]*Module)
	newErrCache := make(map[string]string)

	for _, entry := range entries {
		// Only process regular files, skip directories and special files
		filename := entry.Name()
		if !entry.Type().IsRegular() {
			slog.Debug("skipping non-regular file", "file", filename)
			continue
		}

		// Read file contents
		path := filepath.Join(l.dir, filename)
		contents, err := os.ReadFile(path)
		if err != nil {
			slog.Warn("failed to read module file", "file", filename, "error", err)
			newErrCache[filename] = err.Error()
			continue
		}

		// Compute SHA-256 hash
		hash := sha256.Sum256(contents)
		hashHex := hex.EncodeToString(hash[:])

		// Check if this file is already cached with the same hash
		if cached, exists := oldCache[filename]; exists && cached.hash == hashHex {
			// Reuse cached module (AC4.5 - unchanged files)
			newCache[filename] = cached
		} else {
			// Hash differs or file is new - parse metadata
			module, err := parseModule(ctx, filename, string(contents), hashHex)
			if err != nil {
				slog.Warn("failed to parse module metadata", "file", filename, "error", err)
				newErrCache[filename] = err.Error()
				continue
			}
			newCache[filename] = module
		}
	}

	// Atomically swap both caches (files not in directory are dropped - AC4.4)
	l.mu.Lock()
	l.cache = newCache
	l.errCache = newErrCache
	l.mu.Unlock()

	// Return sorted slice of all modules for deterministic order
	modules := make([]Module, 0, len(newCache))
	for _, module := range newCache {
		modules = append(modules, *module)
	}
	slices.SortFunc(modules, func(a, b Module) int {
		return strings.Compare(a.Filename, b.Filename)
	})

	return modules, nil
}

// parseModule executes the module script with the "metadata" argument to extract
// metadata, then returns a new Module struct.
func parseModule(ctx context.Context, filename, scriptContent, hashHex string) (*Module, error) {
	// Write script to a temporary file
	tmpFile, err := os.CreateTemp("", "anchor-module-*")
	if err != nil {
		return nil, fmt.Errorf("creating temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	defer tmpFile.Close()

	// Write script contents to temp file
	if _, err := tmpFile.WriteString(scriptContent); err != nil {
		return nil, fmt.Errorf("writing temp file: %w", err)
	}

	// Make temp file executable
	if err := tmpFile.Chmod(0755); err != nil {
		return nil, fmt.Errorf("chmod temp file: %w", err)
	}

	if err := tmpFile.Close(); err != nil {
		return nil, fmt.Errorf("closing temp file: %w", err)
	}

	cmd := exec.CommandContext(ctx, tmpPath, "metadata")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("executing metadata command: %w", err)
	}

	// Parse JSON output
	var metadata Metadata
	if err := json.Unmarshal(output, &metadata); err != nil {
		return nil, fmt.Errorf("parsing metadata JSON: %w", err)
	}

	// Create and return the Module
	module := &Module{
		Filename:    filename,
		Name:        metadata.Name,
		Description: metadata.Description,
		Script:      scriptContent,
		hash:        hashHex,
	}

	return module, nil
}

// GetModule looks up a single module by its filename (the canonical identifier
// used in module assignments). Returns the module and a boolean indicating whether
// it was found.
func (l *Loader) GetModule(filename string) (Module, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	cached, exists := l.cache[filename]
	if !exists {
		return Module{}, false
	}
	return *cached, true
}

// LoadErrors returns a sorted list of modules that failed to load.
func (l *Loader) LoadErrors() []ModuleError {
	l.mu.Lock()
	defer l.mu.Unlock()

	errors := make([]ModuleError, 0, len(l.errCache))
	for filename, errMsg := range l.errCache {
		errors = append(errors, ModuleError{Filename: filename, Error: errMsg})
	}
	slices.SortFunc(errors, func(a, b ModuleError) int {
		return strings.Compare(a.Filename, b.Filename)
	})
	return errors
}
