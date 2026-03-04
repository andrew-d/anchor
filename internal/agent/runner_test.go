//go:build unix

package agent

import (
	"testing"
)

func TestRunModuleExitCode0(t *testing.T) {
	script := `#!/bin/sh
echo "test output"
exit 0
`
	result := runModule("test_module", script)

	if result.ModuleName != "test_module" {
		t.Errorf("ModuleName: got %q, want %q", result.ModuleName, "test_module")
	}
	if result.Status != "ok" {
		t.Errorf("Status: got %q, want %q", result.Status, "ok")
	}
	if result.Stdout != "test output\n" {
		t.Errorf("Stdout: got %q, want %q", result.Stdout, "test output\n")
	}
}

func TestRunModuleExitCode80(t *testing.T) {
	script := `#!/bin/sh
echo "changed output"
exit 80
`
	result := runModule("test_changed", script)

	if result.Status != "changed" {
		t.Errorf("Status: got %q, want %q", result.Status, "changed")
	}
	if result.Stdout != "changed output\n" {
		t.Errorf("Stdout: got %q, want %q", result.Stdout, "changed output\n")
	}
}

func TestRunModuleExitCodeError(t *testing.T) {
	script := `#!/bin/sh
echo "stdout output"
echo "stderr output" >&2
exit 1
`
	result := runModule("test_error", script)

	if result.Status != "error" {
		t.Errorf("Status: got %q, want %q", result.Status, "error")
	}
	if result.Stdout != "stdout output\n" {
		t.Errorf("Stdout: got %q, want %q", result.Stdout, "stdout output\n")
	}
	if result.Stderr != "stderr output\n" {
		t.Errorf("Stderr: got %q, want %q", result.Stderr, "stderr output\n")
	}
}

func TestRunModuleTableDriven(t *testing.T) {
	tests := []struct {
		name           string
		script         string
		expectedStatus string
		expectedStdout string
		expectedStderr string
	}{
		{
			name:           "exit_0",
			script:         "#!/bin/sh\necho \"ok\"\nexit 0\n",
			expectedStatus: "ok",
			expectedStdout: "ok\n",
			expectedStderr: "",
		},
		{
			name:           "exit_80",
			script:         "#!/bin/sh\necho \"changed\"\nexit 80\n",
			expectedStatus: "changed",
			expectedStdout: "changed\n",
			expectedStderr: "",
		},
		{
			name:           "exit_1_with_stderr",
			script:         "#!/bin/sh\necho \"out\"\necho \"err\" >&2\nexit 1\n",
			expectedStatus: "error",
			expectedStdout: "out\n",
			expectedStderr: "err\n",
		},
		{
			name:           "exit_127_with_stderr",
			script:         "#!/bin/sh\necho \"failed\" >&2\nexit 127\n",
			expectedStatus: "error",
			expectedStdout: "",
			expectedStderr: "failed\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := runModule(tt.name, tt.script)

			if result.Status != tt.expectedStatus {
				t.Errorf("Status: got %q, want %q", result.Status, tt.expectedStatus)
			}
			if result.Stdout != tt.expectedStdout {
				t.Errorf("Stdout: got %q, want %q", result.Stdout, tt.expectedStdout)
			}
			if result.Stderr != tt.expectedStderr {
				t.Errorf("Stderr: got %q, want %q", result.Stderr, tt.expectedStderr)
			}
		})
	}
}

func TestModuleExecutionOrdering(t *testing.T) {
	modules := []Module{
		{"10_users", "#!/bin/sh\nexit 0\n"},
		{"00_base", "#!/bin/sh\nexit 0\n"},
		{"05_packages", "#!/bin/sh\nexit 0\n"},
	}

	// Sort modules by name using SortModules
	sortedModules := SortModules(modules)

	expectedOrder := []string{"00_base", "05_packages", "10_users"}
	for i, expected := range expectedOrder {
		if sortedModules[i].Name != expected {
			t.Errorf("Module at index %d: got %q, want %q", i, sortedModules[i].Name, expected)
		}
	}
}
