package anchor

import (
	"context"
	"database/sql"
	"path/filepath"
	"sync/atomic"
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

	ctx := t.Context()
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

func TestStart_PragmasAppliedToAllConnections(t *testing.T) {
	app := testApp(t)

	// Force the pool to create several connections by holding them open
	// simultaneously, then verify each one has the expected pragma values.
	const numConns = 4
	conns := make([]*sql.Conn, numConns)
	for i := range conns {
		c, err := app.db.Conn(t.Context())
		if err != nil {
			t.Fatalf("Conn[%d]: %v", i, err)
		}
		conns[i] = c
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()

	type pragmaCheck struct {
		pragma string
		want   string
	}
	checks := []pragmaCheck{
		{"busy_timeout", "10000"},
		{"journal_mode", "wal"},
		{"synchronous", "2"},      // FULL = 2
		{"auto_vacuum", "2"},      // INCREMENTAL = 2
	}

	for i, c := range conns {
		for _, check := range checks {
			var got string
			err := c.QueryRowContext(t.Context(), "PRAGMA "+check.pragma).Scan(&got)
			if err != nil {
				t.Fatalf("conn[%d] PRAGMA %s: %v", i, check.pragma, err)
			}
			if got != check.want {
				t.Errorf("conn[%d] PRAGMA %s = %q, want %q", i, check.pragma, got, check.want)
			}
		}
	}
}

// TestStart_PragmasSetViaDSN verifies that the DSN approach works for a
// fresh database without a running App, confirming the pragmas are
// baked into the DSN rather than set after Open.
func TestStart_PragmasSetViaDSN(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// Open with the same DSN format used in App.Start.
	db, err := sql.Open("sqlite", dbPath+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Acquire two separate connections and verify both have the pragma.
	for i := 0; i < 2; i++ {
		c, err := db.Conn(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		var timeout string
		if err := c.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&timeout); err != nil {
			t.Fatal(err)
		}
		if timeout != "5000" {
			t.Errorf("conn[%d] busy_timeout = %q, want %q", i, timeout, "5000")
		}
		var jm string
		if err := c.QueryRowContext(t.Context(), "PRAGMA journal_mode").Scan(&jm); err != nil {
			t.Fatal(err)
		}
		if jm != "wal" {
			t.Errorf("conn[%d] journal_mode = %q, want %q", i, jm, "wal")
		}
		c.Close()
	}
}

func TestShutdown_WaitsForModuleGoroutines(t *testing.T) {
	mod := &shutdownTestModule{}

	app := New(Config{
		DataDir:    t.TempDir(),
		ListenAddr: "127.0.0.1:0",
		HTTPAddr:   "127.0.0.1:0",
		NodeID:     "test-node",
		Bootstrap:  true,
	})
	app.RegisterModule(mod)

	if err := app.Start(t.Context()); err != nil {
		t.Fatal(err)
	}

	// Wait for leadership.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if app.raft.State().String() == "Leader" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if err := app.Shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}

	// After Shutdown returns, all module goroutines should have exited.
	if !mod.exited.Load() {
		t.Fatal("module goroutine was still running after Shutdown returned")
	}
}

// shutdownTestModule is a test module whose goroutine sets exited=true when
// the context passed to Init is canceled.
type shutdownTestModule struct {
	exited atomic.Bool
}

func (m *shutdownTestModule) Name() string { return "shutdown-test" }

func (m *shutdownTestModule) Init(_ context.Context, ic InitContext) error {
	ic.Go(func(ctx context.Context) {
		<-ctx.Done()
		m.exited.Store(true)
	})
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

func TestIntegration_TypedStore_WatchStore(t *testing.T) {
	app := testApp(t)
	store := Register[testUser](app)

	w := WatchStore(store)
	defer w.Stop()

	// Consume initial (empty) state.
	select {
	case state := <-w.State():
		if len(state) != 0 {
			t.Fatalf("expected empty initial state, got %d entries", len(state))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for initial state")
	}

	if err := store.Set("alice", testUser{Name: "Alice"}); err != nil {
		t.Fatal(err)
	}

	select {
	case state := <-w.State():
		if len(state) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(state))
		}
		if state["alice"].Name != "Alice" {
			t.Fatalf("expected Name=Alice, got %q", state["alice"].Name)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for watch state")
	}
}

