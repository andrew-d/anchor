package anchor

import (
	"context"
	"testing"
	"time"
)

// testApp creates a bootstrapped single-node App for testing.
func testApp(t *testing.T) *App {
	t.Helper()
	dataDir := t.TempDir()

	app := New(Config{
		DataDir:    dataDir,
		ListenAddr: "127.0.0.1:0",
		HTTPAddr:   "127.0.0.1:0",
		NodeID:     "test-node",
		Bootstrap:  true,
	})

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := app.Shutdown(context.Background()); err != nil {
			t.Logf("shutdown error: %v", err)
		}
	})

	// Wait for leadership.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if app.raft.State().String() == "Leader" {
			return app
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("node did not become leader in time")
	return nil
}

type testUser struct {
	Name string   `json:"name"`
	Keys []string `json:"keys"`
}

func (testUser) Kind() string { return "anchor.testUser" }

func TestIntegration_TypedStore_SetGet(t *testing.T) {
	app := testApp(t)
	store := Register[testUser](app)

	if err := store.Set("alice", testUser{Name: "Alice", Keys: []string{"ssh-rsa AAA"}}); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "Alice" {
		t.Fatalf("expected Name=Alice, got %q", got.Name)
	}
	if len(got.Keys) != 1 || got.Keys[0] != "ssh-rsa AAA" {
		t.Fatalf("unexpected keys: %v", got.Keys)
	}
}

func TestIntegration_TypedStore_List(t *testing.T) {
	app := testApp(t)
	store := Register[testUser](app)

	if err := store.Set("alice", testUser{Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Set("bob", testUser{Name: "Bob"}); err != nil {
		t.Fatal(err)
	}

	items, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items["alice"].Name != "Alice" {
		t.Fatalf("unexpected alice: %+v", items["alice"])
	}
	if items["bob"].Name != "Bob" {
		t.Fatalf("unexpected bob: %+v", items["bob"])
	}
}

func TestIntegration_TypedStore_Delete(t *testing.T) {
	app := testApp(t)
	store := Register[testUser](app)

	if err := store.Set("alice", testUser{Name: "Alice"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Delete("alice"); err != nil {
		t.Fatal(err)
	}

	got, err := store.Get("alice")
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != "" {
		t.Fatalf("expected zero value after delete, got %+v", got)
	}
}

func TestIntegration_TypedStore_Watch(t *testing.T) {
	app := testApp(t)
	store := Register[testUser](app)

	w := store.Watch()
	defer w.Stop()

	if err := store.Set("alice", testUser{Name: "Alice"}); err != nil {
		t.Fatal(err)
	}

	select {
	case e := <-w.Events():
		if e.Err != nil {
			t.Fatal(e.Err)
		}
		if e.Key != "alice" {
			t.Fatalf("expected key=alice, got %q", e.Key)
		}
		if e.Value.Name != "Alice" {
			t.Fatalf("expected Name=Alice, got %q", e.Value.Name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watch event")
	}
}
