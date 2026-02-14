package anchor

import (
	"bytes"
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

var testRaftIndex atomic.Uint64

// testFSM creates a SQLite-backed FSM for testing.
func testFSM(t *testing.T) *fsm {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	logger := slogt.New(t)
	app := &App{
		db:      db,
		watches: newWatchHub(db, logger),
		kinds:   make(map[string]kindInfo),
		logger:  logger,
	}
	f := (*fsm)(app)
	if err := f.initTable(); err != nil {
		t.Fatal(err)
	}
	return f
}

func applySet(t *testing.T, f *fsm, kind, key string, value any) {
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
	idx := testRaftIndex.Add(1)
	resp := f.Apply(&raft.Log{Index: idx, Data: b})
	if err, ok := resp.(error); ok {
		t.Fatal(err)
	}
}

func applyDelete(t *testing.T, f *fsm, kind, key string) {
	t.Helper()
	cmd := Command{Type: CmdDelete, Kind: kind, Key: key}
	b, err := json.Marshal(cmd)
	if err != nil {
		t.Fatal(err)
	}
	idx := testRaftIndex.Add(1)
	resp := f.Apply(&raft.Log{Index: idx, Data: b})
	if err, ok := resp.(error); ok {
		t.Fatal(err)
	}
}

func TestFSM_ApplySet(t *testing.T) {
	f := testFSM(t)

	applySet(t, f, "users", "alice", map[string]string{"role": "admin"})

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
	f := testFSM(t)

	applySet(t, f, "users", "alice", map[string]string{"role": "viewer"})
	applySet(t, f, "users", "alice", map[string]string{"role": "admin"})

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
	f := testFSM(t)

	applySet(t, f, "users", "alice", "hello")
	applyDelete(t, f, "users", "alice")

	raw, err := f.fsmGet("users", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Fatalf("expected nil after delete, got %s", raw)
	}
}

func TestFSM_List(t *testing.T) {
	f := testFSM(t)

	applySet(t, f, "servers", "web1", "10.0.0.1")
	applySet(t, f, "servers", "web2", "10.0.0.2")
	applySet(t, f, "other", "x", "unrelated")

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
	f := testFSM(t)

	applySet(t, f, "users", "alice", map[string]string{"role": "admin"})
	applySet(t, f, "users", "bob", map[string]string{"role": "viewer"})
	applySet(t, f, "servers", "web1", map[string]string{"ip": "10.0.0.1"})

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
	f2 := testFSM(t)

	// Put some data that should be wiped by restore.
	applySet(t, f2, "users", "charlie", map[string]string{"role": "ghost"})

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

func TestFSM_SnapshotRestore_EventsAndCursors(t *testing.T) {
	f := testFSM(t)

	// Apply some entries to create events.
	applySet(t, f, "users", "alice", "v1")
	applySet(t, f, "users", "bob", "v2")

	// Simulate a cursor (as if a watcher acked the first event).
	_, err := f.db.Exec(
		`INSERT INTO fsm_cursors(name, pos) VALUES(?, ?)`,
		"test-watcher", 1,
	)
	if err != nil {
		t.Fatal(err)
	}

	// Take snapshot.
	snap, err := f.Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := snap.Persist(&memSink{buf: &buf}); err != nil {
		t.Fatal(err)
	}

	// Restore into a fresh FSM.
	f2 := testFSM(t)
	if err := f2.Restore(io.NopCloser(&buf)); err != nil {
		t.Fatal(err)
	}

	// Verify events survived the restore.
	var eventCount int
	if err := f2.db.QueryRow(`SELECT COUNT(*) FROM fsm_events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if eventCount < 1 {
		t.Fatal("expected events after restore, got 0")
	}

	// Verify cursor survived the restore.
	var pos int64
	if err := f2.db.QueryRow(
		`SELECT pos FROM fsm_cursors WHERE name = ?`, "test-watcher",
	).Scan(&pos); err != nil {
		t.Fatal(err)
	}
	if pos != 1 {
		t.Fatalf("expected cursor pos=1, got %d", pos)
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
