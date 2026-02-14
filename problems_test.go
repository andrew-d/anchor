package anchor

import (
	"log/slog"
	"slices"
	"testing"
	"time"
)

func TestProblemStore_CommitAndAll(t *testing.T) {
	s := newProblemStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	r := s.reporter("mymod", slog.Default())
	pass := r.Begin()
	pass.Error("key1", "something broke", "detail", "value")
	pass.Warn("key2", "something is off")
	pass.Commit()

	all := s.all()
	if len(all) != 2 {
		t.Fatalf("expected 2 problems, got %d", len(all))
	}

	slices.SortFunc(all, func(a, b Problem) int {
		if a.Key < b.Key {
			return -1
		}
		if a.Key > b.Key {
			return 1
		}
		return 0
	})

	if all[0].Key != "key1" || all[0].Severity != Error || all[0].Module != "mymod" {
		t.Fatalf("unexpected problem[0]: %+v", all[0])
	}
	if all[0].Message != "something broke" {
		t.Fatalf("unexpected message: %q", all[0].Message)
	}
	if !all[0].Since.Equal(now) {
		t.Fatalf("expected Since=%v, got %v", now, all[0].Since)
	}
	if len(all[0].Attrs) != 1 || all[0].Attrs[0].Key != "detail" {
		t.Fatalf("unexpected attrs: %+v", all[0].Attrs)
	}

	if all[1].Key != "key2" || all[1].Severity != Warning {
		t.Fatalf("unexpected problem[1]: %+v", all[1])
	}
}

func TestProblemStore_SincePreserved(t *testing.T) {
	s := newProblemStore()
	t1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	t2 := t1.Add(time.Hour)
	s.now = func() time.Time { return t1 }

	r := s.reporter("mod", slog.Default())

	// First pass: report a problem.
	pass1 := r.Begin()
	pass1.Error("broken", "it broke")
	pass1.Commit()

	// Second pass: same key, different time.
	s.now = func() time.Time { return t2 }
	pass2 := r.Begin()
	pass2.Error("broken", "it broke again")
	pass2.Commit()

	all := s.all()
	if len(all) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(all))
	}
	// Since should be preserved from the first pass.
	if !all[0].Since.Equal(t1) {
		t.Fatalf("expected Since=%v (original), got %v", t1, all[0].Since)
	}
	// But the message should be updated.
	if all[0].Message != "it broke again" {
		t.Fatalf("expected updated message, got %q", all[0].Message)
	}
}

func TestProblemStore_ClearedWhenResolved(t *testing.T) {
	s := newProblemStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	r := s.reporter("mod", slog.Default())

	// First pass: report two problems.
	pass1 := r.Begin()
	pass1.Error("key1", "first problem")
	pass1.Error("key2", "second problem")
	pass1.Commit()

	if len(s.all()) != 2 {
		t.Fatalf("expected 2 problems, got %d", len(s.all()))
	}

	// Second pass: only report one problem.
	pass2 := r.Begin()
	pass2.Error("key1", "first problem")
	pass2.Commit()

	all := s.all()
	if len(all) != 1 {
		t.Fatalf("expected 1 problem after second pass, got %d", len(all))
	}
	if all[0].Key != "key1" {
		t.Fatalf("expected key1, got %q", all[0].Key)
	}
}

func TestProblemStore_EmptyCommitClearsAll(t *testing.T) {
	s := newProblemStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	r := s.reporter("mod", slog.Default())

	pass1 := r.Begin()
	pass1.Error("key1", "problem")
	pass1.Commit()

	if len(s.all()) != 1 {
		t.Fatalf("expected 1 problem, got %d", len(s.all()))
	}

	// Empty pass clears all problems.
	pass2 := r.Begin()
	pass2.Commit()

	if len(s.all()) != 0 {
		t.Fatalf("expected 0 problems after empty commit, got %d", len(s.all()))
	}
}

func TestProblemStore_MultipleModules(t *testing.T) {
	s := newProblemStore()
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }

	r1 := s.reporter("mod1", slog.Default())
	r2 := s.reporter("mod2", slog.Default())

	pass1 := r1.Begin()
	pass1.Error("key1", "mod1 problem")
	pass1.Commit()

	pass2 := r2.Begin()
	pass2.Warn("key1", "mod2 warning")
	pass2.Commit()

	all := s.all()
	if len(all) != 2 {
		t.Fatalf("expected 2 problems across modules, got %d", len(all))
	}

	// Clearing mod1 shouldn't affect mod2.
	pass3 := r1.Begin()
	pass3.Commit()

	all = s.all()
	if len(all) != 1 {
		t.Fatalf("expected 1 problem after clearing mod1, got %d", len(all))
	}
	if all[0].Module != "mod2" {
		t.Fatalf("expected mod2 problem to remain, got %q", all[0].Module)
	}
}

func TestArgsToAttrs(t *testing.T) {
	attrs := argsToAttrs([]any{"key1", "val1", "key2", 42})
	if len(attrs) != 2 {
		t.Fatalf("expected 2 attrs, got %d", len(attrs))
	}
	if attrs[0].Key != "key1" {
		t.Fatalf("expected key1, got %q", attrs[0].Key)
	}
	if attrs[1].Key != "key2" {
		t.Fatalf("expected key2, got %q", attrs[1].Key)
	}
}

func TestArgsToAttrs_WithSlogAttr(t *testing.T) {
	attrs := argsToAttrs([]any{slog.String("explicit", "attr"), "key", "val"})
	if len(attrs) != 2 {
		t.Fatalf("expected 2 attrs, got %d", len(attrs))
	}
	if attrs[0].Key != "explicit" {
		t.Fatalf("expected explicit, got %q", attrs[0].Key)
	}
	if attrs[1].Key != "key" {
		t.Fatalf("expected key, got %q", attrs[1].Key)
	}
}
