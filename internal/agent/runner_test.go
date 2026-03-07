//go:build unix

package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunModuleExitCode0(t *testing.T) {
	script := `#!/bin/sh
echo "test output"
exit 0
`
	result := runModule(t.Context(), t.TempDir(), "test_module", script, nil)

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
	result := runModule(t.Context(), t.TempDir(), "test_changed", script, nil)

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
	result := runModule(t.Context(), t.TempDir(), "test_error", script, nil)

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
			result := runModule(t.Context(), t.TempDir(), tt.name, tt.script, nil)

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

func TestRunModuleWithArtifacts(t *testing.T) {
	// Create a cached artifact file
	cacheDir := t.TempDir()
	artContent := "hello from artifact\n"
	artPath := filepath.Join(cacheDir, "test.txt")
	if err := os.WriteFile(artPath, []byte(artContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create nested artifact
	nestedContent := "nested content\n"
	nestedPath := filepath.Join(cacheDir, "nested.conf")
	if err := os.WriteFile(nestedPath, []byte(nestedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Script that reads artifact files from CWD (files/)
	script := `#!/bin/sh
cat config.txt
cat subdir/app.conf
exit 0
`
	artifacts := []cachedArtifact{
		{RelPath: "config.txt", CachePath: artPath, Mode: 0644},
		{RelPath: "subdir/app.conf", CachePath: nestedPath, Mode: 0644},
	}

	result := runModule(t.Context(), t.TempDir(), "test_artifacts", script, artifacts)

	if result.Status != "ok" {
		t.Errorf("Status: got %q, want %q", result.Status, "ok")
	}
	if result.Stdout != artContent+nestedContent {
		t.Errorf("Stdout: got %q, want %q", result.Stdout, artContent+nestedContent)
	}
}

func TestRunModuleArtifactPermissions(t *testing.T) {
	cacheDir := t.TempDir()

	// Create a non-executable config file (0644)
	configPath := filepath.Join(cacheDir, "config.txt")
	if err := os.WriteFile(configPath, []byte("key=val\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create an executable helper script (0755)
	helperPath := filepath.Join(cacheDir, "helper.sh")
	if err := os.WriteFile(helperPath, []byte("#!/bin/sh\necho helper\n"), 0755); err != nil {
		t.Fatal(err)
	}

	// Script checks file permissions using stat
	script := `#!/bin/sh
# Check config.txt is NOT executable
if [ -x config.txt ]; then
    echo "FAIL: config.txt should not be executable" >&2
    exit 1
fi
# Check helper.sh IS executable
if [ ! -x helper.sh ]; then
    echo "FAIL: helper.sh should be executable" >&2
    exit 1
fi
echo "permissions ok"
exit 0
`
	artifacts := []cachedArtifact{
		{RelPath: "config.txt", CachePath: configPath, Mode: 0644},
		{RelPath: "helper.sh", CachePath: helperPath, Mode: 0755},
	}

	result := runModule(t.Context(), t.TempDir(), "test_perms", script, artifacts)

	if result.Status != "ok" {
		t.Errorf("Status: got %q, want %q\nStderr: %s", result.Status, "ok", result.Stderr)
	}
	if strings.TrimSpace(result.Stdout) != "permissions ok" {
		t.Errorf("Stdout: got %q, want %q", result.Stdout, "permissions ok\n")
	}
}

func TestRunModuleWithNoArtifacts(t *testing.T) {
	// Script that lists CWD to verify files/ dir exists but is empty
	script := `#!/bin/sh
ls | wc -l | tr -d ' '
exit 0
`
	result := runModule(t.Context(), t.TempDir(), "test_no_artifacts", script, nil)

	if result.Status != "ok" {
		t.Errorf("Status: got %q, want %q", result.Status, "ok")
	}
	if strings.TrimSpace(result.Stdout) != "0" {
		t.Errorf("Stdout: got %q, want empty directory (0 files)", result.Stdout)
	}
}

func TestRunModuleRejectsPathTraversalInArtifactRelPath(t *testing.T) {
	cacheDir := t.TempDir()
	artPath := filepath.Join(cacheDir, "evil.txt")
	if err := os.WriteFile(artPath, []byte("pwned\n"), 0644); err != nil {
		t.Fatal(err)
	}

	script := "#!/bin/sh\nexit 0\n"
	artifacts := []cachedArtifact{
		{RelPath: "../../etc/evil.conf", CachePath: artPath, Mode: 0644},
	}

	result := runModule(t.Context(), t.TempDir(), "test_traversal", script, artifacts)

	if result.Status != "error" {
		t.Errorf("expected error status for path traversal RelPath, got %q", result.Status)
	}
}

func TestRunModuleRejectsPathTraversalInModuleName(t *testing.T) {
	script := "#!/bin/sh\nexit 0\n"

	result := runModule(t.Context(), t.TempDir(), "../evil_module", script, nil)

	if result.Status != "error" {
		t.Errorf("expected error status for path traversal module name, got %q", result.Status)
	}
}

func TestRunModuleOutputTruncation(t *testing.T) {
	// Generate a script that outputs more than maxOutputBytes on both stdout and stderr.
	// Use head -c to write 600KB (> 512KB limit) of zeros to each stream.
	script := `#!/bin/sh
head -c 614400 /dev/zero
head -c 614400 /dev/zero >&2
exit 0
`
	result := runModule(t.Context(), t.TempDir(), "test_truncation", script, nil)

	if result.Status != "ok" {
		t.Fatalf("Status: got %q, want %q\nStderr: %s", result.Status, "ok", result.Stderr)
	}

	if len(result.Stdout) > maxOutputBytes {
		t.Errorf("Stdout length %d exceeds maxOutputBytes %d", len(result.Stdout), maxOutputBytes)
	}
	if !strings.HasSuffix(result.Stdout, "\n... (output truncated)\n") {
		t.Errorf("Stdout missing truncation marker, ends with: %q", result.Stdout[len(result.Stdout)-40:])
	}

	if len(result.Stderr) > maxOutputBytes {
		t.Errorf("Stderr length %d exceeds maxOutputBytes %d", len(result.Stderr), maxOutputBytes)
	}
	if !strings.HasSuffix(result.Stderr, "\n... (output truncated)\n") {
		t.Errorf("Stderr missing truncation marker, ends with: %q", result.Stderr[len(result.Stderr)-40:])
	}
}

func TestRunModuleOutputUnderLimit(t *testing.T) {
	script := `#!/bin/sh
echo "short output"
exit 0
`
	result := runModule(t.Context(), t.TempDir(), "test_under_limit", script, nil)

	if result.Status != "ok" {
		t.Fatalf("Status: got %q, want %q", result.Status, "ok")
	}
	if result.Stdout != "short output\n" {
		t.Errorf("Stdout: got %q, want %q", result.Stdout, "short output\n")
	}
	if strings.Contains(result.Stdout, "truncated") {
		t.Error("Stdout should not contain truncation marker for small output")
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
