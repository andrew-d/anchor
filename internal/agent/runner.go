package agent

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

// Module represents a module to be executed.
type Module struct {
	Name   string
	Script string
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
// The script is written to a temporary directory alongside a files/ subdirectory
// containing any artifacts. The working directory is set to files/.
// Exit code 0 is mapped to "ok", exit code 80 to "changed", and all other codes to "error".
func runModule(ctx context.Context, name string, script string, artifacts []cachedArtifact) ModuleResult {
	result := ModuleResult{
		ModuleName: name,
	}

	// Create temp directory for this execution
	tmpDir, err := os.MkdirTemp("", "anchor-run-*")
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

	// Copy artifacts into files/ preserving relative paths
	for _, art := range artifacts {
		// MkdirAll + OpenFile via Root — rejects any path that escapes
		if dir := filepath.Dir(art.RelPath); dir != "." {
			if err := filesRoot.MkdirAll(dir, 0750); err != nil {
				result.Status = "error"
				result.Stderr = err.Error()
				return result
			}
		}
		// TODO: use reflinks (copy_file_range / FICLONE) for faster copying on supported filesystems
		if err := copyFileToRoot(art.CachePath, filesRoot, art.RelPath, art.Mode); err != nil {
			result.Status = "error"
			result.Stderr = err.Error()
			return result
		}
	}

	// Execute with "apply" argument; CWD is files/
	scriptPath := filepath.Join(tmpDir, name)
	cmd := exec.CommandContext(ctx, scriptPath, "apply")
	cmd.Dir = filesDir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

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

// copyFileToRoot copies src into the given Root at relPath with the given mode.
// The Root ensures relPath cannot escape its directory tree.
func copyFileToRoot(src string, root *os.Root, relPath string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := root.OpenFile(relPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
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
