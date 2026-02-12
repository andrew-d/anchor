package sshkeys

import (
	"io/fs"
	"testing"
)

func TestUserCanWrite(t *testing.T) {
	tests := []struct {
		name    string
		uid     uint32
		gid     uint32
		dirUID  uint32
		dirGID  uint32
		mode    fs.FileMode
		want    bool
	}{
		{
			name: "owner with write",
			uid: 1000, gid: 1000, dirUID: 1000, dirGID: 1000,
			mode: 0o755, want: true,
		},
		{
			name: "owner without write",
			uid: 1000, gid: 1000, dirUID: 1000, dirGID: 1000,
			mode: 0o555, want: false,
		},
		{
			name: "group with write",
			uid: 1001, gid: 1000, dirUID: 1000, dirGID: 1000,
			mode: 0o770, want: true,
		},
		{
			name: "group without write",
			uid: 1001, gid: 1000, dirUID: 1000, dirGID: 1000,
			mode: 0o750, want: false,
		},
		{
			name: "other with write",
			uid: 2000, gid: 2000, dirUID: 1000, dirGID: 1000,
			mode: 0o757, want: true,
		},
		{
			name: "other without write",
			uid: 2000, gid: 2000, dirUID: 1000, dirGID: 1000,
			mode: 0o750, want: false,
		},
		{
			name: "root always writes",
			uid: 0, gid: 0, dirUID: 1000, dirGID: 1000,
			mode: 0o000, want: true,
		},
		{
			name: "var/empty pattern (owned by root, mode 0555, user is not root)",
			uid: 1000, gid: 1000, dirUID: 0, dirGID: 0,
			mode: 0o555, want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := userCanWrite(tt.uid, tt.gid, tt.dirUID, tt.dirGID, tt.mode)
			if got != tt.want {
				t.Fatalf("userCanWrite(uid=%d, gid=%d, dirUID=%d, dirGID=%d, mode=%o) = %v, want %v",
					tt.uid, tt.gid, tt.dirUID, tt.dirGID, tt.mode, got, tt.want)
			}
		})
	}
}
