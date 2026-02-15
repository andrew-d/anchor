package example

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"testing"

	"github.com/andrew-d/anchor"
	"github.com/andrew-d/anchor/internal/anchortest"
)

// logRecord holds a captured slog record for test assertions.
type logRecord struct {
	Message string
	Attrs   map[string]string
}

// recordHandler is a slog.Handler that captures log records for testing.
type recordHandler struct {
	mu   sync.Mutex
	recs []logRecord
}

func (h *recordHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *recordHandler) WithGroup(string) slog.Handler             { return h }
func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	rec := logRecord{Message: r.Message, Attrs: make(map[string]string)}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value.String()
		return true
	})
	h.mu.Lock()
	h.recs = append(h.recs, rec)
	h.mu.Unlock()
	return nil
}

func (h *recordHandler) snapshot() []logRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return slices.Clone(h.recs)
}

func hasRecord(recs []logRecord, msg, key, val string) bool {
	return slices.ContainsFunc(recs, func(r logRecord) bool {
		return r.Message == msg && r.Attrs["key"] == key &&
			(val == "" || r.Attrs["message"] == val)
	})
}

func countRecords(recs []logRecord, msg, key string) int {
	var n int
	for _, r := range recs {
		if r.Message == msg && r.Attrs["key"] == key {
			n++
		}
	}
	return n
}

func newTestModule(t *testing.T) (*Module, *anchor.App, *recordHandler) {
	t.Helper()
	mod := &Module{applied: make(chan struct{})}
	h := &recordHandler{}
	cluster := anchortest.New(t, 1, func(int) []anchor.Module {
		return []anchor.Module{mod}
	})
	mod.logger = slog.New(h)
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
	if !hasRecord(h.snapshot(), "message changed", "greeting", "hello") {
		t.Fatalf("expected 'message changed' log, got: %v", h.snapshot())
	}

	// Setting the same message again does not log.
	countBefore := countRecords(h.snapshot(), "message changed", "greeting")
	if err := store.Set("greeting", Config{Message: "hello"}); err != nil {
		t.Fatalf("set duplicate: %v", err)
	}
	<-mod.applied
	if got := countRecords(h.snapshot(), "message changed", "greeting"); got != countBefore {
		t.Fatalf("duplicate set logged; count went from %d to %d", countBefore, got)
	}

	// Updating to a different message logs the change.
	if err := store.Set("greeting", Config{Message: "goodbye"}); err != nil {
		t.Fatalf("set update: %v", err)
	}
	<-mod.applied
	if !hasRecord(h.snapshot(), "message changed", "greeting", "goodbye") {
		t.Fatalf("expected 'message changed' log for update, got: %v", h.snapshot())
	}

	// Deleting a key logs the removal.
	if err := store.Delete("greeting"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	<-mod.applied
	if !hasRecord(h.snapshot(), "message removed", "greeting", "") {
		t.Fatalf("expected 'message removed' log, got: %v", h.snapshot())
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
