package version

import "testing"

func TestLongReturnsNonEmpty(t *testing.T) {
	got := Long()
	if got == "" {
		t.Fatal("Long() returned empty string")
	}
}
