package static

import (
	"os/exec"
	"testing"
)

func TestJavaScript(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not available, skipping JavaScript tests")
	}

	cmd := exec.Command("node", "--test", "app_test.js")
	cmd.Dir = "."
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("JavaScript tests failed:\n%s", out)
	}
	t.Logf("JavaScript tests passed:\n%s", out)
}
