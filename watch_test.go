package anchor

import (
	"encoding/json"
	"testing"
	"time"
)

func TestWatcher_Delivery(t *testing.T) {
	hub := newWatchHub()
	w := hub.subscribe("users")
	defer w.Stop()

	hub.notify(Event{
		Change: ChangeSet,
		Kind:   "users",
		Key:    "alice",
		Value:  json.RawMessage(`{"role":"admin"}`),
	})

	select {
	case e := <-w.Events():
		if e.Key != "alice" {
			t.Fatalf("expected key=alice, got %q", e.Key)
		}
		if e.Change != ChangeSet {
			t.Fatalf("expected ChangeSet, got %d", e.Change)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestWatcher_MultipleEvents(t *testing.T) {
	hub := newWatchHub()
	w := hub.subscribe("servers")
	defer w.Stop()

	for i := range 5 {
		hub.notify(Event{
			Change: ChangeSet,
			Kind:   "servers",
			Key:    string(rune('a' + i)),
			Value:  json.RawMessage(`"ok"`),
		})
	}

	for i := range 5 {
		select {
		case e := <-w.Events():
			expected := string(rune('a' + i))
			if e.Key != expected {
				t.Fatalf("event %d: expected key=%q, got %q", i, expected, e.Key)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for event %d", i)
		}
	}
}

func TestWatcher_KindFiltering(t *testing.T) {
	hub := newWatchHub()
	usersW := hub.subscribe("users")
	defer usersW.Stop()

	// Notify on a different kind.
	hub.notify(Event{Change: ChangeSet, Kind: "servers", Key: "web1"})

	// Notify on users kind.
	hub.notify(Event{Change: ChangeSet, Kind: "users", Key: "alice"})

	select {
	case e := <-usersW.Events():
		if e.Key != "alice" {
			t.Fatalf("expected key=alice, got %q", e.Key)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for users event")
	}
}

func TestWatcher_DeleteEvent(t *testing.T) {
	hub := newWatchHub()
	w := hub.subscribe("users")
	defer w.Stop()

	hub.notify(Event{Change: ChangeDelete, Kind: "users", Key: "alice"})

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
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for delete event")
	}
}

func TestTypedWatcher(t *testing.T) {
	hub := newWatchHub()
	w := hub.subscribe("users")
	tw := newTypedWatcher[map[string]string](w)
	defer tw.Stop()

	hub.notify(Event{
		Change: ChangeSet,
		Kind:   "users",
		Key:    "alice",
		Value:  json.RawMessage(`{"role":"admin"}`),
	})

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
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for typed event")
	}
}

func TestWatcher_Stop(t *testing.T) {
	hub := newWatchHub()
	w := hub.subscribe("users")
	w.Stop()

	// Channel should eventually close.
	select {
	case _, ok := <-w.Events():
		if ok {
			// May drain a signal; wait for close.
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
