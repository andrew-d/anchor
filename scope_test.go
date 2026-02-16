package anchor

import "testing"

func TestParseScope(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"", "", false},
		{"os:linux", "os:linux", false},
		{"os:darwin", "os:darwin", false},
		{"node:host1", "node:host1", false},
		{"node:my-node-2", "node:my-node-2", false},

		// Errors.
		{"os:", "", true},
		{"node:", "", true},
		{"badtype:val", "", true},
		{"nocolon", "", true},
		{"os", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseScope(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseScope(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err != nil {
				return
			}
			if got.String() != tt.want {
				t.Fatalf("ParseScope(%q).String() = %q, want %q", tt.input, got.String(), tt.want)
			}
		})
	}
}

func TestScope_Priority(t *testing.T) {
	tests := []struct {
		scope string
		want  int
	}{
		{"", 0},
		{"os:linux", 1},
		{"node:host1", 2},
	}
	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			s, err := ParseScope(tt.scope)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Priority(); got != tt.want {
				t.Fatalf("Scope(%q).Priority() = %d, want %d", tt.scope, got, tt.want)
			}
		})
	}
}

func TestScope_Matches(t *testing.T) {
	info := NodeInfo{ID: "node-1", OS: "linux"}

	tests := []struct {
		scope string
		want  bool
	}{
		{"", true},
		{"os:linux", true},
		{"os:darwin", false},
		{"node:node-1", true},
		{"node:node-2", false},
	}
	for _, tt := range tests {
		t.Run(tt.scope, func(t *testing.T) {
			s, err := ParseScope(tt.scope)
			if err != nil {
				t.Fatal(err)
			}
			if got := s.Matches(info); got != tt.want {
				t.Fatalf("Scope(%q).Matches(%+v) = %v, want %v", tt.scope, info, got, tt.want)
			}
		})
	}
}

func TestScope_IsGlobal(t *testing.T) {
	global, _ := ParseScope("")
	if !global.IsGlobal() {
		t.Fatal("expected empty scope to be global")
	}
	os, _ := ParseScope("os:linux")
	if os.IsGlobal() {
		t.Fatal("expected os scope to not be global")
	}
}
