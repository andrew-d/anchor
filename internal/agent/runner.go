//go:build unix

package agent

import (
	"bytes"
	"os"
	"os/exec"
)

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
func runModule(name string, script string) ModuleResult {
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

	if _, err := tmpFile.WriteString(script); err != nil {
		tmpFile.Close()
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}
	tmpFile.Close()

	// Make executable
	if err := os.Chmod(tmpFile.Name(), 0755); err != nil {
		result.Status = "error"
		result.Stderr = err.Error()
		return result
	}

	// Execute with "apply" argument
	cmd := exec.Command(tmpFile.Name(), "apply")
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
	} else if exitErr, ok := err.(*exec.ExitError); ok {
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
