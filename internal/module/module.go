package module

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"golang.org/x/sync/singleflight"
)

// Artifact represents a file in a module's .d directory.
type Artifact struct {
	RelPath  string // path relative to .d dir, e.g. "sites/default.conf"
	Hash     string // SHA-256 hex
	Size     int64
	Mode     os.FileMode // permission bits from disk
	DiskPath string      // absolute path on server for serving
}

// Module represents a loaded module script with its metadata.
// IMPORTANT: The module naming convention uses the FILENAME (e.g., "00_base")
// as the canonical identifier across the system — in module_assignments, API
// responses, and cache keys. The metadata Name/Description are display-only fields.
type Module struct {
	Filename    string     // Canonical identifier (the script filename, e.g., "00_base")
	Name        string     // Display name from metadata JSON
	Description string     // Display description from metadata JSON
	Script      string     // Full script content
	Artifacts   []Artifact // Associated files from .d directory, sorted by RelPath
	hash        string     // SHA-256 hex digest of file contents
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

// cachedError records a load failure together with the file hash so we can
// suppress duplicate warnings when the file hasn't changed on disk.
type cachedError struct {
	hash    string
	message string
}

// Loader reads module scripts from a directory and caches their metadata.
type Loader struct {
	dir      string
	group    singleflight.Group      // deduplicates concurrent LoadAll calls
	mu       sync.Mutex              // protects cache reads/writes
	cache    map[string]*Module      // keyed by filename
	errCache map[string]*cachedError // keyed by filename
	artIndex map[string]Artifact     // reverse index: hash -> Artifact
}

// NewLoader creates a Loader that reads from the given directory.
func NewLoader(dir string) *Loader {
	return &Loader{
		dir:      dir,
		cache:    make(map[string]*Module),
		errCache: make(map[string]*cachedError),
		artIndex: make(map[string]Artifact),
	}
}

// LoadAll reads all module scripts from the loader's directory, hashing them
// and comparing against cached entries. Only files with changed hashes or new
// files are re-parsed for metadata.
//
// File I/O and metadata command execution happen outside the lock; only the
// final cache swap is synchronized.
func (l *Loader) LoadAll(ctx context.Context) ([]Module, error) {
	v, err, _ := l.group.Do("load", func() (any, error) {
		return l.loadAll(ctx)
	})
	if err != nil {
		return nil, err
	}
	return v.([]Module), nil
}

// loadAll is the actual implementation called via singleflight.
func (l *Loader) loadAll(ctx context.Context) ([]Module, error) {
	// Snapshot the current caches so we can compare hashes without holding
	// mu during I/O.
	l.mu.Lock()
	oldCache := l.cache
	oldErrCache := l.errCache
	l.mu.Unlock()

	// Read all files from the directory
	entries, err := os.ReadDir(l.dir)
	if err != nil {
		return nil, fmt.Errorf("reading module directory: %w", err)
	}

	// Build a new cache with modules from the current state of the directory.
	// All file reads and metadata execution happen here, outside the lock.
	newCache := make(map[string]*Module)
	newErrCache := make(map[string]*cachedError)

	// Collect directory names so we can match .d dirs to module files.
	dirSet := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() {
			dirSet[entry.Name()] = true
		}
	}

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
			newErrCache[filename] = &cachedError{message: err.Error()}
			continue
		}

		// Compute SHA-256 hash
		hash := sha256.Sum256(contents)
		hashHex := hex.EncodeToString(hash[:])

		// Check if this file is already cached with the same hash
		if cached, exists := oldCache[filename]; exists && cached.hash == hashHex {
			// Reuse cached module (AC4.5 - unchanged files)
			newCache[filename] = cached
		} else if ce, exists := oldErrCache[filename]; exists && ce.hash == hashHex {
			// File unchanged and previously failed — reuse cached error silently
			newErrCache[filename] = ce
			continue
		} else {
			// Hash differs or file is new - parse metadata
			module, err := parseModule(ctx, filename, string(contents), hashHex)
			if err != nil {
				slog.Warn("failed to parse module metadata", "file", filename, "error", err)
				newErrCache[filename] = &cachedError{hash: hashHex, message: err.Error()}
				continue
			}
			newCache[filename] = module
		}

		// Walk .d directory for artifacts if it exists
		dDir := filename + ".d"
		if dirSet[dDir] {
			artifacts, err := walkArtifacts(filepath.Join(l.dir, dDir))
			if err != nil {
				slog.Warn("failed to walk artifact directory", "file", filename, "dir", dDir, "error", err)
				delete(newCache, filename)
				newErrCache[filename] = &cachedError{hash: hashHex, message: fmt.Sprintf("walking artifact directory: %v", err)}
				continue
			}
			newCache[filename].Artifacts = artifacts
		}
	}

	// Build artifact reverse index
	newArtIndex := make(map[string]Artifact)
	for _, mod := range newCache {
		for _, art := range mod.Artifacts {
			newArtIndex[art.Hash] = art
		}
	}

	// Atomically swap caches (files not in directory are dropped - AC4.4)
	l.mu.Lock()
	l.cache = newCache
	l.errCache = newErrCache
	l.artIndex = newArtIndex
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

// GetArtifactByHash looks up an artifact by its SHA-256 hash.
func (l *Loader) GetArtifactByHash(hash string) (Artifact, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	art, ok := l.artIndex[hash]
	return art, ok
}

// walkArtifacts walks a .d directory and returns sorted artifacts with hashes.
func walkArtifacts(dDir string) ([]Artifact, error) {
	var artifacts []Artifact
	err := filepath.WalkDir(dDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.Type().IsRegular() {
			return nil
		}

		relPath, err := filepath.Rel(dDir, path)
		if err != nil {
			return err
		}

		info, err := d.Info()
		if err != nil {
			return err
		}

		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer f.Close()

		h := sha256.New()
		size, err := io.Copy(h, f)
		if err != nil {
			return err
		}

		artifacts = append(artifacts, Artifact{
			RelPath:  relPath,
			Hash:     hex.EncodeToString(h.Sum(nil)),
			Size:     size,
			Mode:     info.Mode().Perm(),
			DiskPath: path,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}

	slices.SortFunc(artifacts, func(a, b Artifact) int {
		return strings.Compare(a.RelPath, b.RelPath)
	})
	return artifacts, nil
}

// LoadErrors returns a sorted list of modules that failed to load.
func (l *Loader) LoadErrors() []ModuleError {
	l.mu.Lock()
	defer l.mu.Unlock()

	errors := make([]ModuleError, 0, len(l.errCache))
	for filename, ce := range l.errCache {
		errors = append(errors, ModuleError{Filename: filename, Error: ce.message})
	}
	slices.SortFunc(errors, func(a, b ModuleError) int {
		return strings.Compare(a.Filename, b.Filename)
	})
	return errors
}
