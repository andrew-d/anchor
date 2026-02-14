package anchor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/hashicorp/raft"
	"github.com/neilotoole/slogt"
	_ "modernc.org/sqlite"
)

// testEnv wraps a test FSM with a per-instance raft index counter.
type testEnv struct {
	*fsm
	nextIndex atomic.Uint64
}

// newTestEnv creates a SQLite-backed FSM for testing.
func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	logger := slogt.New(t)
	app := &App{
		db:      db,
		watches: newWatchHub(context.Background(), logger),
		kinds:   make(map[string]kindInfo),
		logger:  logger,
	}
	f := (*fsm)(app)
	if err := f.initTable(); err != nil {
		t.Fatal(err)
	}
	return &testEnv{fsm: f}
}

func (e *testEnv) applySet(t *testing.T, kind, key string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	cmd := Command{Type: CmdSet, Kind: kind, Key: key, Value: data}
	b, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	idx := e.nextIndex.Add(1)
	resp := e.Apply(&raft.Log{Index: idx, Data: b})
	if err, ok := resp.(error); ok {
		t.Fatal(err)
	}
}

func (e *testEnv) applyDelete(t *testing.T, kind, key string) {
	t.Helper()
	cmd := Command{Type: CmdDelete, Kind: kind, Key: key}
	b, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	idx := e.nextIndex.Add(1)
	resp := e.Apply(&raft.Log{Index: idx, Data: b})
	if err, ok := resp.(error); ok {
		t.Fatal(err)
	}
}

func TestFSM_ApplySet(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "users", "alice", map[string]string{"role": "admin"})

	raw, err := f.fsmGet("users", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		t.Fatal("expected value, got nil")
	}

	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["role"] != "admin" {
		t.Fatalf("expected role=admin, got %q", got["role"])
	}
}

func TestFSM_ApplyOverwrite(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "users", "alice", map[string]string{"role": "viewer"})
	f.applySet(t, "users", "alice", map[string]string{"role": "admin"})

	raw, err := f.fsmGet("users", "alice")
	if err != nil {
		t.Fatal(err)
	}

	var got map[string]string
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got["role"] != "admin" {
		t.Fatalf("expected role=admin after overwrite, got %q", got["role"])
	}
}

func TestFSM_ApplyDelete(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "users", "alice", "hello")
	f.applyDelete(t, "users", "alice")

	raw, err := f.fsmGet("users", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Fatalf("expected nil after delete, got %s", raw)
	}
}

func TestFSM_List(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "servers", "web1", "10.0.0.1")
	f.applySet(t, "servers", "web2", "10.0.0.2")
	f.applySet(t, "other", "x", "unrelated")

	items, err := f.fsmList("servers")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if string(items["web1"]) != `"10.0.0.1"` {
		t.Fatalf("unexpected web1 value: %s", items["web1"])
	}
	if string(items["web2"]) != `"10.0.0.2"` {
		t.Fatalf("unexpected web2 value: %s", items["web2"])
	}
}

func TestFSM_SnapshotRestore(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "users", "alice", map[string]string{"role": "admin"})
	f.applySet(t, "users", "bob", map[string]string{"role": "viewer"})
	f.applySet(t, "servers", "web1", map[string]string{"ip": "10.0.0.1"})

	// Take snapshot.
	snap, err := f.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	sink := &memSink{buf: &buf}
	if err := snap.Persist(sink); err != nil {
		t.Fatal(err)
	}

	// Create a fresh FSM and restore into it.
	f2 := newTestEnv(t)

	// Put some data that should be wiped by restore.
	f2.applySet(t, "users", "charlie", map[string]string{"role": "ghost"})

	if err := f2.Restore(io.NopCloser(&buf)); err != nil {
		t.Fatal(err)
	}

	// Verify restored state matches original.
	items, err := f2.fsmList("users")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 users after restore, got %d", len(items))
	}
	if _, ok := items["charlie"]; ok {
		t.Fatal("charlie should have been wiped by restore")
	}

	raw, err := f2.fsmGet("servers", "web1")
	if err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		t.Fatal("expected servers/web1 after restore")
	}
}

// memSink is a minimal raft.SnapshotSink backed by a buffer.
type memSink struct {
	buf *bytes.Buffer
}

func (s *memSink) Write(p []byte) (int, error) { return s.buf.Write(p) }
func (s *memSink) Close() error                { return nil }
func (s *memSink) ID() string                  { return "test" }
func (s *memSink) Cancel() error               { return nil }
