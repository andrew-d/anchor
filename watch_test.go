package anchor

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/neilotoole/slogt"
	_ "modernc.org/sqlite"
)

// testWatchDB opens an on-disk SQLite database and creates the FSM tables.
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

// insertKV inserts a row into fsm_kv at global scope.
func insertKV(t *testing.T, db *sql.DB, kind, key, value string) {
	t.Helper()
	_, err := db.Exec(
		`INSERT INTO fsm_kv(kind, scope, key, value) VALUES(?, '', ?, ?)
		 ON CONFLICT(kind, scope, key) DO UPDATE SET value = excluded.value`,
		kind, key, value,
	)
	if err != nil {
		t.Fatal(err)
	}
}

// deleteKV deletes a row from fsm_kv at global scope.
func deleteKV(t *testing.T, db *sql.DB, kind, key string) {
	t.Helper()
	_, err := db.Exec(`DELETE FROM fsm_kv WHERE kind = ? AND scope = '' AND key = ?`, kind, key)
	if err != nil {
		t.Fatal(err)
	}
}

// testStoreWatcher creates a Watcher that reads all entries for a kind from
// the given database, suitable for unit tests that don't need a full App.
func testStoreWatcher(hub *watchHub, db *sql.DB, kind string) *Watcher[map[string]json.RawMessage] {
	entry := hub.subscribe(kind)
	readFn := func() (map[string]json.RawMessage, error) {
		rows, err := db.Query(`SELECT key, value FROM fsm_kv WHERE kind = ?`, kind)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		result := make(map[string]json.RawMessage)
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				return nil, err
			}
			result[k] = json.RawMessage(v)
		}
		return result, rows.Err()
	}
	return newWatcher(hub, entry, readFn)
}

// testKeyWatcher creates a Watcher that reads a single key from the given
// database.
func testKeyWatcher(hub *watchHub, db *sql.DB, kind, key string) *Watcher[json.RawMessage] {
	entry := hub.subscribe(kind)
	readFn := func() (json.RawMessage, error) {
		var val string
		err := db.QueryRow(`SELECT value FROM fsm_kv WHERE kind = ? AND key = ?`, kind, key).Scan(&val)
		if err == sql.ErrNoRows {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return json.RawMessage(val), nil
	}
	return newWatcher(hub, entry, readFn)
}

func TestWatcher_InitialDelivery(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	insertKV(t, db, "users", "alice", `{"role":"admin"}`)

	w := testStoreWatcher(hub, db, "users")
	defer w.Stop()

	select {
	case state := <-w.State():
		if len(state) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(state))
		}
		if string(state["alice"]) != `{"role":"admin"}` {
			t.Fatalf("unexpected value: %s", state["alice"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial state")
	}
}

func TestWatcher_DeliveryAfterSignal(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	w := testStoreWatcher(hub, db, "users")
	defer w.Stop()

	// Consume initial (empty) delivery.
	select {
	case state := <-w.State():
		if len(state) != 0 {
			t.Fatalf("expected empty initial state, got %d entries", len(state))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial state")
	}

	// Mutate and signal.
	insertKV(t, db, "users", "alice", `{"role":"admin"}`)
	hub.signal("users")

	select {
	case state := <-w.State():
		if len(state) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(state))
		}
		if string(state["alice"]) != `{"role":"admin"}` {
			t.Fatalf("unexpected value: %s", state["alice"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for state after signal")
	}
}

func TestWatcher_KindFiltering(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	w := testStoreWatcher(hub, db, "users")
	defer w.Stop()

	// Consume initial delivery.
	select {
	case <-w.State():
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for initial state")
	}

	// Insert into a different kind and signal that kind.
	insertKV(t, db, "servers", "web1", `"ok"`)
	hub.signal("servers")

	// No delivery should happen for the users watcher.
	select {
	case <-w.State():
		t.Fatal("unexpected delivery for wrong kind")
	case <-time.After(200 * time.Millisecond):
		// Expected: no delivery.
	}
}

func TestWatcher_Stop(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	w := testStoreWatcher(hub, db, "users")
	w.Stop()

	// Channel should be closed.
	select {
	case _, ok := <-w.State():
		if ok {
			// Got initial delivery; next read should show closed.
			select {
			case _, ok := <-w.State():
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

func TestWatcher_StopDuringBlockedDelivery(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	insertKV(t, db, "users", "alice", `"v1"`)
	w := testStoreWatcher(hub, db, "users")

	// Don't consume the initial delivery — the loop is blocked trying to send.
	// Stop should not hang.
	done := make(chan struct{})
	go func() {
		w.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() blocked during delivery")
	}
}

func TestWatcher_MultipleConcurrent(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	insertKV(t, db, "users", "alice", `"v1"`)

	w1 := testStoreWatcher(hub, db, "users")
	defer w1.Stop()
	w2 := testStoreWatcher(hub, db, "users")
	defer w2.Stop()

	// Both should see the same initial state.
	for _, w := range []*Watcher[map[string]json.RawMessage]{w1, w2} {
		select {
		case state := <-w.State():
			if string(state["alice"]) != `"v1"` {
				t.Fatalf("unexpected value: %s", state["alice"])
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for state")
		}
	}
}

func TestWatcher_EmptyState(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	w := testStoreWatcher(hub, db, "users")
	defer w.Stop()

	select {
	case state := <-w.State():
		if len(state) != 0 {
			t.Fatalf("expected empty map, got %d entries", len(state))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for empty state")
	}
}

func TestWatcher_WatchKey(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	insertKV(t, db, "motd", "default", `"Hello, world!"`)

	w := testKeyWatcher(hub, db, "motd", "default")
	defer w.Stop()

	select {
	case val := <-w.State():
		if string(val) != `"Hello, world!"` {
			t.Fatalf("unexpected value: %s", val)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for key state")
	}
}

func TestWatcher_WatchKeyMissing(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	w := testKeyWatcher(hub, db, "motd", "default")
	defer w.Stop()

	select {
	case val := <-w.State():
		if val != nil {
			t.Fatalf("expected nil for missing key, got %s", val)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for key state")
	}
}

func TestWatcher_GatedBlocksUntilOpen(t *testing.T) {
	db := testWatchDB(t)

	synctest.Test(t, func(t *testing.T) {
		hub := newWatchHub(t.Context(), slogt.New(t))

		insertKV(t, db, "users", "alice", `{"role":"admin"}`)

		w := testStoreWatcher(hub, db, "users")
		defer w.Stop()

		// Wait for the watcher goroutine to block on the gate.
		synctest.Wait()

		// Non-blocking: nothing should be available.
		select {
		case <-w.State():
			t.Fatal("watcher delivered state before hub was opened")
		default:
		}

		hub.open()
		synctest.Wait()

		select {
		case state := <-w.State():
			if string(state["alice"]) != `{"role":"admin"}` {
				t.Fatalf("unexpected value: %s", state["alice"])
			}
		default:
			t.Fatal("no state delivered after open")
		}
	})
}

func TestWatcher_GatedSeesPostReplayState(t *testing.T) {
	db := testWatchDB(t)

	synctest.Test(t, func(t *testing.T) {
		hub := newWatchHub(t.Context(), slogt.New(t))

		// Insert "stale" data (simulates state from a previous snapshot).
		insertKV(t, db, "users", "alice", `{"role":"viewer"}`)

		w := testStoreWatcher(hub, db, "users")
		defer w.Stop()

		// Wait for the watcher goroutine to block on the gate.
		synctest.Wait()

		// Simulate Raft log replay: update the data while the hub is gated.
		insertKV(t, db, "users", "alice", `{"role":"admin"}`)
		hub.signal("users")

		// Open the gate after "replay" is done.
		hub.open()
		synctest.Wait()

		// The first delivery must reflect the post-replay state, not the stale
		// snapshot state.
		select {
		case state := <-w.State():
			if string(state["alice"]) != `{"role":"admin"}` {
				t.Fatalf("got stale state %s, want post-replay state", state["alice"])
			}
		default:
			t.Fatal("no state delivered after open")
		}
	})
}

func TestWatcher_SignalAll(t *testing.T) {
	db := testWatchDB(t)
	hub := newWatchHub(t.Context(), slogt.New(t))
	hub.open()

	w1 := testStoreWatcher(hub, db, "users")
	defer w1.Stop()
	w2 := testStoreWatcher(hub, db, "servers")
	defer w2.Stop()

	// Consume initial deliveries.
	for _, w := range []*Watcher[map[string]json.RawMessage]{w1, w2} {
		select {
		case <-w.State():
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for initial state")
		}
	}

	// Insert data for both kinds.
	insertKV(t, db, "users", "alice", `"v1"`)
	insertKV(t, db, "servers", "web1", `"ok"`)
	hub.signalAll()

	// Both should get the updated state.
	select {
	case state := <-w1.State():
		if string(state["alice"]) != `"v1"` {
			t.Fatalf("unexpected users value: %s", state["alice"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for users state after signalAll")
	}

	select {
	case state := <-w2.State():
		if string(state["web1"]) != `"ok"` {
			t.Fatalf("unexpected servers value: %s", state["web1"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for servers state after signalAll")
	}
}
