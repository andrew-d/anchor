package copyfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCopyFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	content := []byte("hello, world!\n")
	if err := os.WriteFile(src, content, 0o755); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(content) {
		t.Errorf("content mismatch: got %q, want %q", got, content)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Errorf("mode mismatch: got %o, want %o", info.Mode().Perm(), 0o755)
	}
}

func TestCopyFilePreservesModTime(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set a specific modification time on the source.
	modTime := time.Date(2020, 6, 15, 12, 0, 0, 0, time.UTC)
	if err := os.Chtimes(src, modTime, modTime); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !info.ModTime().Equal(modTime) {
		t.Errorf("mod time mismatch: got %v, want %v", info.ModTime(), modTime)
	}
}

func TestCopyFileOverwritesExisting(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")

	// Write initial destination content.
	if err := os.WriteFile(dst, []byte("old content"), 0o644); err != nil {
		t.Fatal(err)
	}

	newContent := []byte("new content")
	if err := os.WriteFile(src, newContent, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := CopyFile(src, dst); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(newContent) {
		t.Errorf("content mismatch: got %q, want %q", got, newContent)
	}
}

func TestCopyFileSourceNotFound(t *testing.T) {
	dir := t.TempDir()
	err := CopyFile(filepath.Join(dir, "nonexistent"), filepath.Join(dir, "dst"))
	if err == nil {
		t.Fatal("expected error for non-existent source")
	}
}

func TestCopyFileBadDestDir(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.WriteFile(src, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := CopyFile(src, filepath.Join(dir, "nodir", "dst"))
	if err == nil {
		t.Fatal("expected error for non-existent destination directory")
	}
}
