package contrib_test

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// runHelper runs a shell script that sources helpers.sh and executes the given
// body. Returns the exit code (0, 80, or other) and combined output.
func runHelper(t *testing.T, dir, body string) (int, string) {
	t.Helper()

	helpersPath, err := filepath.Abs("helpers.sh")
	if err != nil {
		t.Fatalf("resolving helpers.sh path: %v", err)
	}

	script := fmt.Sprintf("#!/bin/sh\nset -eu\n. %q\n%s\n", helpersPath, body)
	scriptPath := filepath.Join(dir, "test_script.sh")
	if err := os.WriteFile(scriptPath, []byte(script), 0o755); err != nil {
		t.Fatalf("writing test script: %v", err)
	}

	cmd := exec.Command("sh", scriptPath)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			t.Fatalf("running script: %v\n%s", err, out)
		}
	}
	return exitCode, string(out)
}

func TestAnchorFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "source.txt")
	dest := filepath.Join(dir, "sub", "dest.txt")
	os.WriteFile(src, []byte("hello\n"), 0o644)

	body := fmt.Sprintf("anchor_file %q %q\nanchor_exit", src, dest)

	// First run: file doesn't exist, should change
	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("reading dest: %v", err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("dest contents: got %q, want %q", got, "hello\n")
	}

	// Second run: file matches, no change
	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}

	// Third run: change source, should change again
	os.WriteFile(src, []byte("world\n"), 0o644)
	code, _ = runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("third run: want exit 80, got %d", code)
	}
	got, _ = os.ReadFile(dest)
	if string(got) != "world\n" {
		t.Fatalf("dest contents after update: got %q, want %q", got, "world\n")
	}
}

func TestAnchorFileMissingSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	body := fmt.Sprintf("anchor_file %q %q", filepath.Join(dir, "nope"), filepath.Join(dir, "dest"))
	code, _ := runHelper(t, dir, body)
	if code == 0 || code == 80 {
		t.Fatalf("missing source: want error exit, got %d", code)
	}
}

func TestAnchorLink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	target := "/some/target"
	link := filepath.Join(dir, "sub", "mylink")

	body := fmt.Sprintf("anchor_link %q %q\nanchor_exit", target, link)

	// First run: link doesn't exist
	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	got, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("readlink: %v", err)
	}
	if got != target {
		t.Fatalf("link target: got %q, want %q", got, target)
	}

	// Second run: link exists and correct
	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}

	// Third run: link points elsewhere, should fix
	os.Remove(link)
	os.Symlink("/wrong/target", link)
	code, _ = runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("third run (wrong target): want exit 80, got %d", code)
	}
	got, _ = os.Readlink(link)
	if got != target {
		t.Fatalf("link target after fix: got %q, want %q", got, target)
	}
}

func TestAnchorLinkExistingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	// A regular file at the link path should cause an error
	link := filepath.Join(dir, "mylink")
	os.WriteFile(link, []byte("not a symlink"), 0o644)

	body := fmt.Sprintf("anchor_link /target %q", link)
	code, _ := runHelper(t, dir, body)
	if code == 0 || code == 80 {
		t.Fatalf("existing file: want error exit, got %d", code)
	}
}

func TestAnchorLine(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file := filepath.Join(dir, "config.txt")
	os.WriteFile(file, []byte("existing line\n"), 0o644)

	body := fmt.Sprintf("anchor_line %q 'new line'\nanchor_exit", file)

	// First run: line missing
	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	got, _ := os.ReadFile(file)
	if string(got) != "existing line\nnew line\n" {
		t.Fatalf("file contents: got %q", got)
	}

	// Second run: line present
	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}
}

func TestAnchorLineCreatesFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file := filepath.Join(dir, "sub", "new.txt")

	body := fmt.Sprintf("anchor_line %q 'hello'\nanchor_exit", file)

	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	got, _ := os.ReadFile(file)
	if string(got) != "hello\n" {
		t.Fatalf("file contents: got %q", got)
	}
}

func TestAnchorDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	target := filepath.Join(dir, "a", "b", "c")

	body := fmt.Sprintf("anchor_dir %q 0750\nanchor_exit", target)

	// First run: directory doesn't exist
	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("not a directory")
	}
	if perm := info.Mode().Perm(); perm != 0o750 {
		t.Fatalf("mode: got %04o, want 0750", perm)
	}

	// Second run: directory exists with correct mode
	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}
}

func TestAnchorDirFixesMode(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	target := filepath.Join(dir, "mydir")
	os.Mkdir(target, 0o700)

	body := fmt.Sprintf("anchor_dir %q 0755\nanchor_exit", target)

	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("want exit 80 (mode change), got %d", code)
	}
	info, _ := os.Stat(target)
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("mode: got %04o, want 0755", perm)
	}

	// Idempotent
	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}
}

func TestAnchorAbsent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file := filepath.Join(dir, "remove_me.txt")
	os.WriteFile(file, []byte("bye"), 0o644)

	body := fmt.Sprintf("anchor_absent %q\nanchor_exit", file)

	// First run: file exists
	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("file still exists")
	}

	// Second run: already absent
	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}
}

func TestAnchorAbsentDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	target := filepath.Join(dir, "remove_dir")
	os.MkdirAll(filepath.Join(target, "nested"), 0o755)
	os.WriteFile(filepath.Join(target, "nested", "file.txt"), []byte("data"), 0o644)

	body := fmt.Sprintf("anchor_absent %q\nanchor_exit", target)

	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatal("directory still exists")
	}
}

func TestAnchorAbsentBrokenSymlink(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	link := filepath.Join(dir, "dead_link")
	os.Symlink("/nonexistent", link)

	body := fmt.Sprintf("anchor_absent %q\nanchor_exit", link)

	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	if _, err := os.Lstat(link); !os.IsNotExist(err) {
		t.Fatal("symlink still exists")
	}
}

func TestAnchorChmod(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file := filepath.Join(dir, "myfile")
	os.WriteFile(file, []byte("data"), 0o600)

	body := fmt.Sprintf("anchor_chmod 0644 %q\nanchor_exit", file)

	// First run: mode differs
	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	info, _ := os.Stat(file)
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("mode: got %04o, want 0644", perm)
	}

	// Second run: mode matches
	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}
}

func TestAnchorChmodDirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	target := filepath.Join(dir, "mydir")
	os.Mkdir(target, 0o700)

	body := fmt.Sprintf("anchor_chmod 0755 %q\nanchor_exit", target)

	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}
	info, _ := os.Stat(target)
	if perm := info.Mode().Perm(); perm != 0o755 {
		t.Fatalf("mode: got %04o, want 0755", perm)
	}

	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}
}

func TestAnchorChmodMissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	body := fmt.Sprintf("anchor_chmod 0644 %q", filepath.Join(dir, "nope"))
	code, _ := runHelper(t, dir, body)
	if code == 0 || code == 80 {
		t.Fatalf("missing path: want error exit, got %d", code)
	}
}

func TestAnchorChownIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file := filepath.Join(dir, "myfile")
	os.WriteFile(file, []byte("data"), 0o644)

	u, err := user.Current()
	if err != nil {
		t.Fatalf("getting current user: %v", err)
	}

	// Chown to current user:group — should be no-op
	g, err := user.LookupGroupId(u.Gid)
	if err != nil {
		t.Fatalf("looking up group: %v", err)
	}

	owner := u.Username + ":" + g.Name
	body := fmt.Sprintf("anchor_chown %q %q\nanchor_exit", owner, file)

	code, _ := runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("idempotent run: want exit 0, got %d", code)
	}
}

func TestAnchorChownNumericIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file := filepath.Join(dir, "myfile")
	os.WriteFile(file, []byte("data"), 0o644)

	u, err := user.Current()
	if err != nil {
		t.Fatalf("getting current user: %v", err)
	}

	// Use numeric uid:gid
	owner := u.Uid + ":" + u.Gid
	body := fmt.Sprintf("anchor_chown %q %q\nanchor_exit", owner, file)

	code, _ := runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("idempotent run: want exit 0, got %d", code)
	}
}

func TestAnchorChownUserOnly(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	file := filepath.Join(dir, "myfile")
	os.WriteFile(file, []byte("data"), 0o644)

	u, err := user.Current()
	if err != nil {
		t.Fatalf("getting current user: %v", err)
	}

	body := fmt.Sprintf("anchor_chown %q %q\nanchor_exit", u.Username, file)

	code, _ := runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("idempotent run: want exit 0, got %d", code)
	}
}

func TestAnchorChownMissingPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	body := fmt.Sprintf("anchor_chown root %q", filepath.Join(dir, "nope"))
	code, _ := runHelper(t, dir, body)
	if code == 0 || code == 80 {
		t.Fatalf("missing path: want error exit, got %d", code)
	}
}

func TestAnchorExitNoChanges(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	code, _ := runHelper(t, dir, "anchor_exit")
	if code != 0 {
		t.Fatalf("want exit 0, got %d", code)
	}
}

func TestMultipleHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "src.conf")
	os.WriteFile(src, []byte("[config]\n"), 0o644)
	settings := filepath.Join(dir, "etc", "settings.txt")

	body := fmt.Sprintf(`
anchor_dir %q
anchor_file %q %q
anchor_line %q 'key=value'
anchor_link %q %q
anchor_exit
`,
		filepath.Join(dir, "etc"),
		src, filepath.Join(dir, "etc", "app.conf"),
		settings,
		filepath.Join(dir, "etc", "app.conf"), filepath.Join(dir, "etc", "current.conf"),
	)

	// First run: everything created
	code, _ := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("first run: want exit 80, got %d", code)
	}

	// Verify state
	got, _ := os.ReadFile(filepath.Join(dir, "etc", "app.conf"))
	if string(got) != "[config]\n" {
		t.Fatalf("app.conf contents: got %q", got)
	}
	settingsGot, _ := os.ReadFile(settings)
	if string(settingsGot) != "key=value\n" {
		t.Fatalf("settings.txt contents: got %q", settingsGot)
	}
	linkTarget, _ := os.Readlink(filepath.Join(dir, "etc", "current.conf"))
	if linkTarget != filepath.Join(dir, "etc", "app.conf") {
		t.Fatalf("symlink target: got %q", linkTarget)
	}

	// Second run: idempotent
	code, _ = runHelper(t, dir, body)
	if code != 0 {
		t.Fatalf("second run: want exit 0, got %d", code)
	}
}

func TestNoNamespacePollution(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	src := filepath.Join(dir, "src.txt")
	os.WriteFile(src, []byte("data\n"), 0o644)

	file := filepath.Join(dir, "lines.txt")
	linkpath := filepath.Join(dir, "mylink")
	subdir := filepath.Join(dir, "sub")

	// Snapshot variable names before and after exercising every helper.
	// Any new variable that doesn't start with _anchor_ / _ANCHOR_ is a leak.
	body := fmt.Sprintf(`
# Snapshot variable names before sourcing did anything.
# (helpers.sh is already sourced by runHelper, so _ANCHOR_CHANGED exists,
# but no helper has been called yet.)
set | sed 's/=.*//' | sort > %[1]q

anchor_file %[2]q %[3]q
anchor_link /some/target %[4]q
anchor_line %[5]q 'test line'
anchor_dir %[6]q 0755
anchor_chmod 0644 %[2]q
anchor_absent %[3]q

# Snapshot after.
set | sed 's/=.*//' | sort > %[7]q

# New variables = lines only in the "after" snapshot.
comm -13 %[1]q %[7]q | while read -r name; do
    case "$name" in
        _ANCHOR_*|_anchor_*) ;;   # our namespace — ok
        anchor_*) ;;              # our function names — ok
        *) echo "LEAKED: $name" ;;
    esac
done
anchor_exit
`,
		filepath.Join(dir, "vars_before.txt"), // 1
		src,                                   // 2
		filepath.Join(dir, "dest.txt"),        // 3
		linkpath,                              // 4
		file,                                  // 5
		subdir,                                // 6
		filepath.Join(dir, "vars_after.txt"),  // 7
	)

	code, output := runHelper(t, dir, body)
	if code != 80 {
		t.Fatalf("want exit 80, got %d; output:\n%s", code, output)
	}

	// Fail on any LEAKED lines.
	for _, line := range strings.Split(output, "\n") {
		if strings.HasPrefix(line, "LEAKED:") {
			t.Errorf("namespace pollution: %s", line)
		}
	}
}
