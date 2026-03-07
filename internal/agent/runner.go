package agent

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"

	"github.com/andrew-d/anchor/internal/copyfile"
)

const (
	// maxOutputBytes is the maximum number of bytes captured per output
	// stream (stdout/stderr). This prevents a misbehaving module from
	// consuming unbounded memory. 512KB leaves room for JSON overhead
	// within the server's 1MB request body limit.
	maxOutputBytes = 512 * 1024

	truncationMarker = "\n... (output truncated)\n"
)

// limitedWriter wraps a bytes.Buffer and stops writing after a byte limit.
type limitedWriter struct {
	buf       bytes.Buffer
	remaining int
	truncated bool
}

func newLimitedWriter(limit int) *limitedWriter {
	return &limitedWriter{remaining: limit - len(truncationMarker)}
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		w.truncated = true
		return len(p), nil
	}
	if len(p) > w.remaining {
		w.buf.Write(p[:w.remaining])
		w.remaining = 0
		w.truncated = true
		return len(p), nil
	}
	w.buf.Write(p)
	w.remaining -= len(p)
	return len(p), nil
}

func (w *limitedWriter) String() string {
	if w.truncated {
		return w.buf.String() + truncationMarker
	}
	return w.buf.String()
}

// Module represents a module to be executed.
type Module struct {
	Name     string
	Script   string
	Critical bool
}

// cachedArtifact describes an artifact that has been downloaded to the local cache.
type cachedArtifact struct {
	RelPath   string
	CachePath string
	Mode      os.FileMode
}

// ModuleResult contains the result of running a single module.
type ModuleResult struct {
	ModuleName string
	Status     string // "ok", "changed", "error"
	Stdout     string
	Stderr     string
}

// runModule executes a module script and returns its result.
// runDir is the base directory for temporary execution directories; it should
// be on the same filesystem as the artifact cache so that reflink copies work.
// The script is written to a temporary directory alongside a files/ subdirectory
// containing any artifacts. The working directory is set to files/.
// Exit code 0 is mapped to "ok", exit code 80 to "changed", and all other codes to "error".
func runModule(ctx context.Context, runDir string, name string, script string, artifacts []cachedArtifact) ModuleResult {
	result := ModuleResult{
		ModuleName: name,
	}

	// Create temp directory for this execution under runDir so it shares
	// a filesystem with the artifact cache, enabling reflink copies.
	tmpDir, err := os.MkdirTemp(runDir, "anchor-run-*")
	if err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}
	defer os.RemoveAll(tmpDir)

	// Use os.Root to prevent any path traversal from untrusted name/RelPath values.
	root, err := os.OpenRoot(tmpDir)
	if err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}
	defer root.Close()

	// Write script to tmpDir/{name} — Root rejects traversal in name
	if err := root.WriteFile(name, []byte(script), 0750); err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}

	// Create files/ subdirectory
	filesDir := filepath.Join(tmpDir, "files")
	if err := root.Mkdir("files", 0750); err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}

	// Open a sub-root for files/ so artifact RelPaths are confined
	filesRoot, err := root.OpenRoot("files")
	if err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}
	defer filesRoot.Close()

	// Copy artifacts into files/ preserving relative paths.
	// Files are opened via os.Root for path traversal safety, then copied
	// using copyfile.Clone which tries FICLONE reflink first, falling back
	// to io.Copy (which uses copy_file_range internally).
	for _, art := range artifacts {
		if dir := filepath.Dir(art.RelPath); dir != "." {
			if err := filesRoot.MkdirAll(dir, 0750); err != nil {
				result.Status = "error"
				result.Stderr = err.Error()
				return result
			}
		}
		if err := cloneArtifact(art, filesRoot); err != nil {
			result.Status = "error"
			result.Stderr = err.Error()
			return result
		}
	}

	// Execute with "apply" argument; CWD is files/
	scriptPath := filepath.Join(tmpDir, name)
	cmd := exec.CommandContext(ctx, scriptPath, "apply")
	cmd.Dir = filesDir
	stdout := newLimitedWriter(maxOutputBytes)
	stderr := newLimitedWriter(maxOutputBytes)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Run and capture exit code
	err = cmd.Run()
	result.Stdout = stdout.String()
	result.Stderr = stderr.String()

	// Determine status from exit code
	if err == nil {
		result.Status = "ok"
		return result
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		exitCode := exitErr.ExitCode()
		if exitCode == 80 {
			result.Status = "changed"
		} else {
			result.Status = "error"
		}
	} else {
		result.Status = "error"
	}

	return result
}

// cloneArtifact copies a cached artifact into the execution directory using
// os.Root for path confinement and copyfile.Clone for efficient data copy.
func cloneArtifact(art cachedArtifact, filesRoot *os.Root) error {
	src, err := os.Open(art.CachePath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := filesRoot.OpenFile(art.RelPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, art.Mode)
	if err != nil {
		return err
	}
	defer dst.Close()

	if err := copyfile.Clone(dst, src); err != nil {
		return err
	}
	return dst.Close()
}

// SortModules returns a copy of the modules sorted by name.
func SortModules(modules []Module) []Module {
	sorted := make([]Module, len(modules))
	copy(sorted, modules)
	slices.SortFunc(sorted, func(a, b Module) int {
		return strings.Compare(a.Name, b.Name)
	})
	return sorted
}
