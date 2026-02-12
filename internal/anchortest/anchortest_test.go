package anchortest

import (
	"context"
	"testing"
	"time"

	"github.com/andrew-d/anchor"
)

// testModule is a minimal module that registers a typed store for testing.
type testModule struct {
	store *anchor.TypedStore[testValue]
}

type testValue struct {
	Name string `json:"name"`
}

func (m *testModule) Name() string { return "test" }
func (m *testModule) Init(_ context.Context, app *anchor.App) error {
	m.store = anchor.Register[testValue](app, "test_kind")
	return nil
}

func TestCluster_ThreeNodes(t *testing.T) {
	mods := make([]*testModule, 3)
	cluster := New(t, 3, func(i int) []anchor.Module {
		mods[i] = &testModule{}
		return []anchor.Module{mods[i]}
	})

	if len(cluster.Nodes) != 3 {
		t.Fatalf("expected 3 nodes, got %d", len(cluster.Nodes))
	}

	idx := cluster.LeaderIndex()
	if idx < 0 {
		t.Fatal("expected a leader")
	}

	// Write through the leader's store.
	leader := cluster.Leader()
	if leader == nil {
		t.Fatal("Leader() returned nil")
	}
	if leader != cluster.Nodes[idx] {
		t.Fatal("Leader() and LeaderIndex() disagree")
	}

	if err := mods[idx].store.Set("alice", testValue{Name: "Alice"}); err != nil {
		t.Fatalf("set: %v", err)
	}

	got, err := mods[idx].store.Get("alice")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Name != "Alice" {
		t.Fatalf("expected Name=Alice, got %q", got.Name)
	}

	// Verify followers eventually see the write.
	for i, mod := range mods {
		if i == idx {
			continue
		}
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			v, err := mod.store.Get("alice")
			if err == nil && v.Name == "Alice" {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		v, err := mod.store.Get("alice")
		if err != nil {
			t.Fatalf("follower node-%d get: %v", i, err)
		}
		if v.Name != "Alice" {
			t.Fatalf("follower node-%d expected Name=Alice, got %q", i, v.Name)
		}
	}
}

func TestCluster_SingleNode(t *testing.T) {
	cluster := New(t, 1, nil)

	if len(cluster.Nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(cluster.Nodes))
	}
	if cluster.LeaderIndex() != 0 {
		t.Fatal("single node should be leader")
	}
}

func TestCluster_WaitForLeader(t *testing.T) {
	cluster := New(t, 3, nil)

	idx := cluster.WaitForLeader(t, 5*time.Second)
	if idx < 0 || idx >= len(cluster.Nodes) {
		t.Fatalf("WaitForLeader returned invalid index %d", idx)
	}
	if !cluster.Nodes[idx].IsLeaderForTest() {
		t.Fatal("WaitForLeader returned a non-leader node")
	}
}
