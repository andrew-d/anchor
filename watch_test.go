package anchor

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"github.com/neilotoole/slogt"
	_ "modernc.org/sqlite"
)

// testWatchDB opens an on-disk SQLite database and creates the event/cursor tables.
func testWatchDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "watch.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	for _, p := range []string{
		"PRAGMA busy_timeout=5000;",
		"PRAGMA journal_mode=WAL;",
	} {
		if _, err := db.Exec(p); err != nil {
			t.Fatal(err)
		}
	}

	if _, err = db.Exec(fsmSchema); err != nil {
		t.Fatal(err)
	}
	return db
}

// insertEvent inserts a row into fsm_events.
func insertEvent(t *testing.T, db *sql.DB, raftIndex int64, kind string, change ChangeType, key, value string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO fsm_events(raft_index, kind, change, key, value) VALUES(?, ?, ?, ?, ?)`,
		raftIndex, kind, int(change), key, value,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func TestWatcher_Delivery(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(db, slogt.New(t))
	w := hub.subscribe("users", "test-delivery")
	defer w.Stop()

	insertEvent(t, db, 1, "users", ChangeSet, "alice", `{"role":"admin"}`)
	hub.signal("users")

	select {
	case e := <-w.Events():
		if e.Key != "alice" {
			t.Fatalf("expected key=alice, got %q", e.Key)
		}
		if e.Change != ChangeSet {
			t.Fatalf("expected ChangeSet, got %d", e.Change)
		}
		if string(e.Value) != `{"role":"admin"}` {
			t.Fatalf("unexpected value: %s", e.Value)
		}
		e.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestWatcher_MultipleEvents(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(db, slogt.New(t))
	w := hub.subscribe("servers", "test-multi")
	defer w.Stop()

	for i := range 5 {
		insertEvent(t, db, int64(i+1), "servers", ChangeSet, string(rune('a'+i)), `"ok"`)
	}
	hub.signal("servers")

	for i := range 5 {
		select {
		case e := <-w.Events():
			expected := string(rune('a' + i))
			if e.Key != expected {
				t.Fatalf("event %d: expected key=%q, got %q", i, expected, e.Key)
			}
			e.Ack()
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
}

func TestWatcher_KindFiltering(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(db, slogt.New(t))
	usersW := hub.subscribe("users", "test-filter")
	defer usersW.Stop()

	// Insert event for a different kind.
	insertEvent(t, db, 1, "servers", ChangeSet, "web1", `"ok"`)
	// Insert event for the watched kind.
	insertEvent(t, db, 2, "users", ChangeSet, "alice", `"ok"`)
	hub.signal("users")

	select {
	case e := <-usersW.Events():
		if e.Key != "alice" {
			t.Fatalf("expected key=alice, got %q", e.Key)
		}
		e.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for users event")
	}
}

func TestWatcher_DeleteEvent(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(db, slogt.New(t))
	w := hub.subscribe("users", "test-delete")
	defer w.Stop()

	insertEvent(t, db, 1, "users", ChangeDelete, "alice", "")
	hub.signal("users")

	select {
	case e := <-w.Events():
		if e.Change != ChangeDelete {
			t.Fatalf("expected ChangeDelete, got %d", e.Change)
		}
		if e.Key != "alice" {
			t.Fatalf("expected key=alice, got %q", e.Key)
		}
		if e.Value != nil {
			t.Fatalf("expected nil value for delete, got %s", e.Value)
		}
		e.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete event")
	}
}

func TestTypedWatcher(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(db, slogt.New(t))
	w := hub.subscribe("users", "test-typed")
	tw := newTypedWatcher[map[string]string](w)
	defer tw.Stop()

	insertEvent(t, db, 1, "users", ChangeSet, "alice", `{"role":"admin"}`)
	hub.signal("users")

	select {
	case e := <-tw.Events():
		if e.Err != nil {
			t.Fatal(e.Err)
		}
		if e.Key != "alice" {
			t.Fatalf("expected key=alice, got %q", e.Key)
		}
		if e.Value["role"] != "admin" {
			t.Fatalf("expected role=admin, got %q", e.Value["role"])
		}
		e.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typed event")
	}
}

func TestWatcher_Stop(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(db, slogt.New(t))
	w := hub.subscribe("users", "test-stop")
	w.Stop()

	// Channel should eventually close.
	select {
	case _, ok := <-w.Events():
		if ok {
			select {
			case _, ok := <-w.Events():
				if ok {
					t.Fatal("expected channel to be closed after Stop")
				}
			case <-time.After(2 * time.Second):
				t.Fatal("channel did not close after Stop")
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after Stop")
	}
}

func TestWatcher_CursorPersistence(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(db, slogt.New(t))

	// Insert 3 events.
	insertEvent(t, db, 1, "users", ChangeSet, "a", `"1"`)
	insertEvent(t, db, 2, "users", ChangeSet, "b", `"2"`)
	insertEvent(t, db, 3, "users", ChangeSet, "c", `"3"`)

	// First watcher: ack events 1 and 2.
	w1 := hub.subscribe("users", "persist-test")
	hub.signal("users")

	for i := range 2 {
		select {
		case e := <-w1.Events():
			expected := string(rune('a' + i))
			if e.Key != expected {
				t.Fatalf("w1 event %d: expected key=%q, got %q", i, expected, e.Key)
			}
			e.Ack()
		case <-time.After(2 * time.Second):
			t.Fatalf("w1 timed out waiting for event %d", i)
		}
	}
	w1.Stop()

	// Second watcher with same name: should only see event 3.
	w2 := hub.subscribe("users", "persist-test")
	hub.signal("users")

	select {
	case e := <-w2.Events():
		if e.Key != "c" {
			t.Fatalf("expected key=c, got %q", e.Key)
		}
		e.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event c")
	}
	w2.Stop()
}

func TestWatcher_RedeliveryWithoutAck(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(db, slogt.New(t))

	insertEvent(t, db, 1, "users", ChangeSet, "alice", `"v1"`)

	// First watcher: receive without ack.
	w1 := hub.subscribe("users", "redeliver-test")
	hub.signal("users")

	select {
	case e := <-w1.Events():
		if e.Key != "alice" {
			t.Fatalf("expected key=alice, got %q", e.Key)
		}
		// No ack!
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	w1.Stop()

	// Second watcher with same name: should see the same event again.
	w2 := hub.subscribe("users", "redeliver-test")
	hub.signal("users")

	select {
	case e := <-w2.Events():
		if e.Key != "alice" {
			t.Fatalf("expected key=alice on redeliver, got %q", e.Key)
		}
		e.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for redelivered event")
	}
	w2.Stop()
}


