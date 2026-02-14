//go:build unix

package sshkeys

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParsePasswdData(t *testing.T) {
	tests := []struct {
		name string
		data string
		want []enumeratedUser
	}{
		{
			name: "normal entries",
			data: "root:x:0:0:root:/root:/bin/bash\nalice:x:1000:1000:Alice:/home/alice:/bin/bash\n",
			want: []enumeratedUser{
				{username: "root", homeDir: "/root"},
				{username: "alice", homeDir: "/home/alice"},
			},
		},
		{
			name: "skips comments",
			data: "# this is a comment\nalice:x:1000:1000:Alice:/home/alice:/bin/bash\n",
			want: []enumeratedUser{
				{username: "alice", homeDir: "/home/alice"},
			},
		},
		{
			name: "skips blank lines",
			data: "\nalice:x:1000:1000:Alice:/home/alice:/bin/bash\n\n",
			want: []enumeratedUser{
				{username: "alice", homeDir: "/home/alice"},
			},
		},
		{
			name: "skips short lines",
			data: "short:line:only\nalice:x:1000:1000:Alice:/home/alice:/bin/bash\n",
			want: []enumeratedUser{
				{username: "alice", homeDir: "/home/alice"},
			},
		},
		{
			name: "empty input",
			data: "",
			want: nil,
		},
		{
			name: "multiple users",
			data: "root:x:0:0:root:/root:/bin/bash\nnobody:x:65534:65534:nobody:/nonexistent:/usr/sbin/nologin\nbob:x:1001:1001:Bob:/home/bob:/bin/zsh\n",
			want: []enumeratedUser{
				{username: "root", homeDir: "/root"},
				{username: "nobody", homeDir: "/nonexistent"},
				{username: "bob", homeDir: "/home/bob"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parsePasswdData(tt.data)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("got %d users, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Fatalf("user %d: got %+v, want %+v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestEnumerateViaPasswdFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "passwd")

	content := "root:x:0:0:root:/root:/bin/bash\nalice:x:1000:1000:Alice:/home/alice:/bin/bash\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	users, err := enumerateViaPasswdFile(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("got %d users, want 2", len(users))
	}
	if users[0].username != "root" || users[0].homeDir != "/root" {
		t.Fatalf("user 0: got %+v", users[0])
	}
	if users[1].username != "alice" || users[1].homeDir != "/home/alice" {
		t.Fatalf("user 1: got %+v", users[1])
	}
}

func TestEnumerateViaPasswdFile_NotFound(t *testing.T) {
	_, err := enumerateViaPasswdFile("/nonexistent/passwd")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}
