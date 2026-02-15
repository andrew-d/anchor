package atomicfile

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func intPtr(v int) *int { return &v }

func TestWriteFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := WriteFile(path, []byte("hello\n"), Options{}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello\n" {
		t.Fatalf("got %q, want %q", got, "hello\n")
	}
}

func TestWriteFileDefaultMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := WriteFile(path, []byte("x"), Options{}); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o644 {
		t.Fatalf("got mode %o, want 0644", fi.Mode().Perm())
	}
}

func TestWriteFileCustomMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "secret.txt")

	if err := WriteFile(path, []byte("secret"), Options{Mode: 0o600}); err != nil {
		t.Fatal(err)
	}

	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("got mode %o, want 0600", fi.Mode().Perm())
	}
}

func TestWriteFileReplacesExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := WriteFile(path, []byte("new"), Options{}); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("got %q, want %q", got, "new")
	}
}

func TestWriteCallbackError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	writeErr := errors.New("boom")

	err := Write(path, func(w io.Writer) error {
		return writeErr
	}, Options{})
	if !errors.Is(err, writeErr) {
		t.Fatalf("got %v, want wrapping of %v", err, writeErr)
	}

	// The target file should not exist.
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected target to not exist, got stat err: %v", err)
	}

	// No temp files should be left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no leftover files, got %v", entries)
	}
}

func TestWriteFileNoTempLeftOnError(t *testing.T) {
	dir := t.TempDir()
	// Target a non-existent subdirectory so CreateTemp fails.
	path := filepath.Join(dir, "nodir", "out.txt")

	err := WriteFile(path, []byte("x"), Options{})
	if err == nil {
		t.Fatal("expected error for missing parent directory")
	}

	// No temp files in dir.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected no leftover files, got %v", entries)
	}
}

func TestWriteChown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	// Chown to current uid/gid should always succeed.
	uid := os.Getuid()
	gid := os.Getgid()

	err := WriteFile(path, []byte("owned"), Options{
		Mode: 0o600,
		UID:  intPtr(uid),
		GID:  intPtr(gid),
	})
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "owned" {
		t.Fatalf("got %q, want %q", got, "owned")
	}
}

func TestWriteStreaming(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stream.txt")

	err := Write(path, func(w io.Writer) error {
		for _, s := range []string{"line1\n", "line2\n", "line3\n"} {
			if _, err := io.WriteString(w, s); err != nil {
				return err
			}
		}
		return nil
	}, Options{})
	if err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "line1\nline2\nline3\n"
	if string(got) != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWriteFileAtomicity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")

	original := "original content"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	// A failed write should leave the original file intact.
	writeErr := errors.New("mid-write failure")
	err := Write(path, func(w io.Writer) error {
		// Write some data then fail.
		w.Write([]byte("partial"))
		return writeErr
	}, Options{})
	if !errors.Is(err, writeErr) {
		t.Fatalf("got %v, want wrapping of %v", err, writeErr)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != original {
		t.Fatalf("original file was corrupted: got %q, want %q", got, original)
	}
}
