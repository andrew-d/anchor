package contrib_test

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestIntegrationSystemdUnits(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	podman, err := exec.LookPath("podman")
	if err != nil {
		t.Skip("podman not available, skipping")
	}

	tmpDir := t.TempDir()

	// The test needs a pre-built anchor binary. It tries to build one,
	// but falls back to ANCHOR_BIN if "go" is not in PATH (e.g. when
	// running the compiled test binary under sudo).
	anchorBin := os.Getenv("ANCHOR_BIN")
	if anchorBin == "" {
		anchorBin = filepath.Join(tmpDir, "anchor")
		build := exec.Command("go", "build", "-o", anchorBin, "../cmd/anchor")
		build.Stdout = os.Stdout
		build.Stderr = os.Stderr
		if err := build.Run(); err != nil {
			t.Fatalf("building anchor (set ANCHOR_BIN to skip): %v", err)
		}
	}

	// Resolve the contrib directory (contains unit files).
	contribDir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("resolving contrib dir: %v", err)
	}

	// Copy test fixtures to tmpDir for bind-mounting into the container.
	modulesDir := filepath.Join(tmpDir, "modules.d")
	copyTestdata(t, "00_test", filepath.Join(modulesDir, "00_test"), 0o755)
	setupScript := filepath.Join(tmpDir, "setup.sh")
	copyTestdata(t, "setup.sh", setupScript, 0o755)

	// Build a container image with systemd and curl installed.
	imageName := "anchor-systemd-test"
	buildImg := exec.Command(podman, "build", "-t", imageName, "-f", "testdata/Containerfile", "testdata")
	buildImg.Stdout = os.Stdout
	buildImg.Stderr = os.Stderr
	if err := buildImg.Run(); err != nil {
		t.Fatalf("building container image: %v", err)
	}
	t.Cleanup(func() {
		exec.Command(podman, "rmi", imageName).Run()
	})

	// Verify we can run a privileged container. This requires root or
	// equivalent — skip if it doesn't work.
	probeCmd := exec.Command(podman, "run", "--rm", "--privileged", imageName, "echo", "OK")
	if probeOut, err := probeCmd.CombinedOutput(); err != nil {
		t.Skipf("cannot run privileged container (try running as root): %v\n%s", err, probeOut)
	}

	containerName := "anchor-systemd-test-" + filepath.Base(tmpDir)

	// Start a privileged container with systemd as PID 1. We use
	// --privileged so that the unit files' sandboxing directives
	// (ProtectSystem=strict, DynamicUser=yes, mount namespaces, etc.)
	// work exactly as they would on a real host.
	startCmd := exec.Command(podman, "run",
		"--detach",
		"--privileged",
		"--systemd=true",
		"--name", containerName,
		"-v", anchorBin+":/usr/local/bin/anchor:ro",
		"-v", contribDir+":/contrib:ro",
		"-v", modulesDir+":/modules.d:ro",
		"-v", setupScript+":/setup.sh:ro",
		imageName,
		"/sbin/init",
	)
	out, err := startCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("starting container: %v\n%s", err, out)
	}
	containerID := string(bytes.TrimSpace(out))
	t.Cleanup(func() {
		exec.Command(podman, "rm", "-f", containerID).Run()
	})

	// Wait for systemd to finish booting inside the container.
	waitCmd := exec.Command(podman, "exec", containerName,
		"systemctl", "is-system-running", "--wait")
	// is-system-running returns non-zero for "degraded" which is fine;
	// we just need it to have finished booting.
	waitOut, _ := waitCmd.CombinedOutput()
	t.Logf("systemd state: %s", bytes.TrimSpace(waitOut))

	// Run the setup and verification script inside the container.
	execCmd := exec.Command(podman, "exec", containerName, "bash", "/setup.sh")
	execCmd.Stdout = os.Stdout
	execCmd.Stderr = os.Stderr
	if err := execCmd.Run(); err != nil {
		dumpJournals(podman, containerName)
		t.Fatalf("setup script failed: %v", err)
	}
}

func copyTestdata(t *testing.T, name, dst string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatalf("creating parent dir for %s: %v", dst, err)
	}
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading testdata/%s: %v", name, err)
	}
	if err := os.WriteFile(dst, data, mode); err != nil {
		t.Fatalf("writing %s: %v", dst, err)
	}
}

func dumpJournals(podman, container string) {
	for _, unit := range []string{"anchor-server.service", "anchor-agent.service"} {
		fmt.Fprintf(os.Stderr, "\n=== journal: %s ===\n", unit)
		cmd := exec.Command(podman, "exec", container,
			"journalctl", "-u", unit, "--no-pager", "-n", "50")
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		cmd.Run()
	}
}
