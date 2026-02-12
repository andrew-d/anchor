package typekey

import (
	"strings"
	"testing"

	"github.com/andrew-d/anchor/internal/typekey/testpkga"
	"github.com/andrew-d/anchor/internal/typekey/testpkgb"
)

func TestOf_CrossPackageUniqueness(t *testing.T) {
	a := Of[testpkga.Value]()
	b := Of[testpkgb.Value]()

	if a == b {
		t.Fatalf("expected different type keys, both are %q", a)
	}
	if !strings.Contains(a, "testpkga") {
		t.Fatalf("expected type key to contain package path, got %q", a)
	}
	if !strings.Contains(b, "testpkgb") {
		t.Fatalf("expected type key to contain package path, got %q", b)
	}
}

func TestOf_FullPath(t *testing.T) {
	got := Of[testpkga.Value]()
	const want = "github.com/andrew-d/anchor/internal/typekey/testpkga.Value"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestOf_PanicsOnBuiltin(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic for builtin type")
		}
	}()
	Of[int]()
}
