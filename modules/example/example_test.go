package example

import (
	"slices"
	"testing"

	"github.com/andrew-d/anchor"
	"github.com/andrew-d/anchor/internal/anchortest"
	"github.com/andrew-d/anchor/internal/slogrecorder"
)

func hasRecord(recs []slogrecorder.Record, msg, key, val string) bool {
	return slices.ContainsFunc(recs, func(r slogrecorder.Record) bool {
		return r.Message == msg && r.Attrs["key"] == key &&
			(val == "" || r.Attrs["message"] == val)
	})
}

func countRecords(recs []slogrecorder.Record, msg, key string) int {
	var n int
	for _, r := range recs {
		if r.Message == msg && r.Attrs["key"] == key {
			n++
		}
	}
	return n
}

func newTestModule(t *testing.T) (*Module, *anchor.App, *slogrecorder.Handler) {
	t.Helper()
	mod := &Module{applied: make(chan struct{})}
	h := &slogrecorder.Handler{}
	cluster := anchortest.New(t, 1, func(int) []anchor.Module {
		return []anchor.Module{mod}
	})
	mod.logger = h.Logger()
	return mod, cluster.Nodes[0], h
}

func TestWatchLoop(t *testing.T) {
	mod, _, h := newTestModule(t)
	store := mod.store

	// Drain the initial delivery (empty state on startup).
	<-mod.applied

	// Setting a message logs the change.
	if err := store.Set("greeting", Config{Message: "hello"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	<-mod.applied
	if !hasRecord(h.Records(), "message changed", "greeting", "hello") {
		t.Fatalf("expected 'message changed' log, got: %v", h.Records())
	}

	// Setting the same message again does not log.
	countBefore := countRecords(h.Records(), "message changed", "greeting")
	if err := store.Set("greeting", Config{Message: "hello"}); err != nil {
		t.Fatalf("set duplicate: %v", err)
	}
	<-mod.applied
	if got := countRecords(h.Records(), "message changed", "greeting"); got != countBefore {
		t.Fatalf("duplicate set logged; count went from %d to %d", countBefore, got)
	}

	// Updating to a different message logs the change.
	if err := store.Set("greeting", Config{Message: "goodbye"}); err != nil {
		t.Fatalf("set update: %v", err)
	}
	<-mod.applied
	if !hasRecord(h.Records(), "message changed", "greeting", "goodbye") {
		t.Fatalf("expected 'message changed' log for update, got: %v", h.Records())
	}

	// Deleting a key logs the removal.
	if err := store.Delete("greeting"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	<-mod.applied
	if !hasRecord(h.Records(), "message removed", "greeting", "") {
		t.Fatalf("expected 'message removed' log, got: %v", h.Records())
	}
}

func TestWatchLoop_Problem(t *testing.T) {
	mod, app, _ := newTestModule(t)
	store := mod.store

	// Drain the initial delivery.
	<-mod.applied

	// Setting a message with a problem reports a warning.
	if err := store.Set("greeting", Config{Message: "hello", Problem: "something is wrong"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	<-mod.applied

	problems := app.Problems()
	if len(problems) != 1 {
		t.Fatalf("expected 1 problem, got %d: %v", len(problems), problems)
	}
	if problems[0].Key != "greeting" {
		t.Errorf("problem key = %q, want %q", problems[0].Key, "greeting")
	}
	if problems[0].Message != "something is wrong" {
		t.Errorf("problem message = %q, want %q", problems[0].Message, "something is wrong")
	}
	if problems[0].Severity != anchor.Warning {
		t.Errorf("problem severity = %v, want Warning", problems[0].Severity)
	}

	// Clearing the problem field removes the problem.
	if err := store.Set("greeting", Config{Message: "hello"}); err != nil {
		t.Fatalf("set: %v", err)
	}
	<-mod.applied

	if problems := app.Problems(); len(problems) != 0 {
		t.Fatalf("expected 0 problems after clearing, got %d: %v", len(problems), problems)
	}
}
