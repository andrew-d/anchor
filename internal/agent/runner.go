package agent

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
)

// Module represents a module to be executed.
type Module struct {
	Name   string
	Script string
}

// ModuleResult contains the result of running a single module.
type ModuleResult struct {
	ModuleName string
	Status     string // "ok", "changed", "error"
	Stdout     string
	Stderr     string
}

// runModule executes a module script and returns its result.
// The script is written to a temporary file, made executable, and run with the "apply" argument.
// Exit code 0 is mapped to "ok", exit code 80 to "changed", and all other codes to "error".
func runModule(ctx context.Context, name string, script string) ModuleResult {
	result := ModuleResult{
		ModuleName: name,
	}

	// Write script to temp file
	tmpFile, err := os.CreateTemp("", "anchor-module-*")
	if err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(script); err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}

	// Make executable
	if err := tmpFile.Chmod(0755); err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}

	if err := tmpFile.Close(); err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}

	// Execute with "apply" argument
	cmd := exec.CommandContext(ctx, tmpFile.Name(), "apply")
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

// SortModules returns a copy of the modules sorted by name.
func SortModules(modules []Module) []Module {
	sorted := make([]Module, len(modules))
	copy(sorted, modules)
	slices.SortFunc(sorted, func(a, b Module) int {
		return strings.Compare(a.Name, b.Name)
	})
	return sorted
}
