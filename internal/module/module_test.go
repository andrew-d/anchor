package module

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// writeTestModule writes a sample module script to a temp directory.
// The script implements the module interface with "metadata" and "apply" commands.
func writeTestModule(t *testing.T, dir, filename, name, description string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
    metadata)
        echo '{"name": "%s", "description": "%s"}'
        ;;
    apply)
        echo "applying %s"
        exit 0
        ;;
    *)
        echo "unknown command: $1" >&2
        exit 1
        ;;
esac
`, name, description, name)
	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte(script), 0755)
	if err != nil {
		t.Fatal(err)
	}
}
