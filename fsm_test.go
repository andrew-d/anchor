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
	hub := newWatchHub(t.Context(), logger)
	hub.open()
	app := &App{
		db:      db,
		watches: hub,
		kinds:   make(map[string]kindInfo),
		logger:  logger,
	}
	f := (*fsm)(app)
	if err := f.initTable(); err != nil {
		t.Fatal(err)
	}
	return &testEnv{fsm: f}
}

func (e *testEnv) applySet(t *testing.T, kind, scope, key string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	cmd := Command{Type: CmdSet, Kind: kind, Scope: scope, Key: key, Value: data}
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

func (e *testEnv) applyDelete(t *testing.T, kind, scope, key string) {
	t.Helper()
	cmd := Command{Type: CmdDelete, Kind: kind, Scope: scope, Key: key}
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

	f.applySet(t, "users", "", "alice", map[string]string{"role": "admin"})

	raw, err := f.fsmGetExact("users", "", "alice")
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

	f.applySet(t, "users", "", "alice", map[string]string{"role": "viewer"})
	f.applySet(t, "users", "", "alice", map[string]string{"role": "admin"})

	raw, err := f.fsmGetExact("users", "", "alice")
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

	f.applySet(t, "users", "", "alice", "hello")
	f.applyDelete(t, "users", "", "alice")

	raw, err := f.fsmGetExact("users", "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Fatalf("expected nil after delete, got %s", raw)
	}
}

func TestFSM_ListAll(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "servers", "", "web1", "10.0.0.1")
	f.applySet(t, "servers", "", "web2", "10.0.0.2")
	f.applySet(t, "other", "", "x", "unrelated")

	items, err := f.fsmListAll("servers")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
}

func TestFSM_ScopedSetGet(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "config", "", "key1", "global")
	f.applySet(t, "config", "os:linux", "key1", "linux")
	f.applySet(t, "config", "node:host1", "key1", "host1")

	// Each scope is independent.
	raw, err := f.fsmGetExact("config", "", "key1")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `"global"` {
		t.Fatalf("global: got %s", raw)
	}

	raw, err = f.fsmGetExact("config", "os:linux", "key1")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `"linux"` {
		t.Fatalf("os:linux: got %s", raw)
	}

	raw, err = f.fsmGetExact("config", "node:host1", "key1")
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != `"host1"` {
		t.Fatalf("node:host1: got %s", raw)
	}
}

func TestFSM_ScopedDelete(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "config", "", "key1", "global")
	f.applySet(t, "config", "os:linux", "key1", "linux")

	// Delete only the os scope.
	f.applyDelete(t, "config", "os:linux", "key1")

	raw, err := f.fsmGetExact("config", "os:linux", "key1")
	if err != nil {
		t.Fatal(err)
	}
	if raw != nil {
		t.Fatalf("expected nil after scoped delete, got %s", raw)
	}

	// Global should still exist.
	raw, err = f.fsmGetExact("config", "", "key1")
	if err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		t.Fatal("global should still exist after scoped delete")
	}
}

func TestFSM_GetAllScopes(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "config", "", "key1", "global")
	f.applySet(t, "config", "os:linux", "key1", "linux")
	f.applySet(t, "config", "node:host1", "key1", "host1")
	f.applySet(t, "config", "", "key2", "other")

	entries, err := f.fsmGetAllScopes("config", "key1")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestFSM_ListAllWithScopes(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "config", "", "key1", "global")
	f.applySet(t, "config", "os:linux", "key1", "linux")
	f.applySet(t, "config", "", "key2", "global2")

	entries, err := f.fsmListAll("config")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}
}

func TestFSM_SnapshotRestore(t *testing.T) {
	f := newTestEnv(t)

	f.applySet(t, "users", "", "alice", map[string]string{"role": "admin"})
	f.applySet(t, "users", "", "bob", map[string]string{"role": "viewer"})
	f.applySet(t, "servers", "", "web1", map[string]string{"ip": "10.0.0.1"})
	f.applySet(t, "servers", "os:linux", "web1", map[string]string{"ip": "10.0.0.2"})

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
	f2.applySet(t, "users", "", "charlie", map[string]string{"role": "ghost"})

	if err := f2.Restore(io.NopCloser(&buf)); err != nil {
		t.Fatal(err)
	}

	// Verify restored state matches original.
	items, err := f2.fsmListAll("users")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 users after restore, got %d", len(items))
	}

	raw, err := f2.fsmGetExact("servers", "", "web1")
	if err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		t.Fatal("expected servers/web1 after restore")
	}

	// Verify scoped entry was restored.
	raw, err = f2.fsmGetExact("servers", "os:linux", "web1")
	if err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		t.Fatal("expected scoped servers/web1 after restore")
	}
}

func TestFSM_RestoreV1Snapshot(t *testing.T) {
	// Simulate a v1 snapshot (no scope field).
	data := snapshotData{
		Version: 1,
		KV: []snapshotKV{
			{Kind: "users", Key: "alice", Value: json.RawMessage(`{"role":"admin"}`)},
		},
	}
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	f := newTestEnv(t)
	if err := f.Restore(io.NopCloser(bytes.NewReader(b))); err != nil {
		t.Fatal(err)
	}

	// v1 entries should be restored at global scope.
	raw, err := f.fsmGetExact("users", "", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if raw == nil {
		t.Fatal("expected value after v1 restore")
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
