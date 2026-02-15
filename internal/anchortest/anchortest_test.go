package anchortest

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
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

func (testValue) Kind() string { return "anchortest.testValue" }

func (m *testModule) Name() string { return "test" }
func (m *testModule) Init(_ context.Context, ic anchor.InitContext) error {
	m.store = anchor.Register[testValue](ic.App)
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

func TestNonvoterJoin(t *testing.T) {
	// 3-node cluster: node-0 bootstrap voter, node-1 voter, node-2 nonvoter.
	mods := make([]*testModule, 3)
	cluster := NewWithOptions(t, ClusterOptions{
		NumNodes:    3,
		NodeOptions: map[int]NodeOptions{2: {Nonvoter: true}},
		ModsFn: func(i int) []anchor.Module {
			mods[i] = &testModule{}
			return []anchor.Module{mods[i]}
		},
	})

	leaderIdx := cluster.WaitForLeader(t, 10*time.Second)
	leader := cluster.Nodes[leaderIdx]

	// Verify via the status API that node-2 has "nonvoter" suffrage.
	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/status", leader.HTTPAddrForTest()))
	if err != nil {
		t.Fatalf("status request: %v", err)
	}
	defer resp.Body.Close()

	var status struct {
		Nodes []struct {
			ID       string `json:"id"`
			Suffrage string `json:"suffrage"`
		} `json:"nodes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatalf("decode status: %v", err)
	}

	suffrageByID := make(map[string]string)
	for _, n := range status.Nodes {
		suffrageByID[n.ID] = n.Suffrage
	}
	if suffrageByID["node-0"] != "voter" {
		t.Errorf("node-0 suffrage = %q, want %q", suffrageByID["node-0"], "voter")
	}
	if suffrageByID["node-1"] != "voter" {
		t.Errorf("node-1 suffrage = %q, want %q", suffrageByID["node-1"], "voter")
	}
	if suffrageByID["node-2"] != "nonvoter" {
		t.Errorf("node-2 suffrage = %q, want %q", suffrageByID["node-2"], "nonvoter")
	}

	// Write through leader, read from nonvoter to confirm replication.
	if err := mods[leaderIdx].store.Set("alice", testValue{Name: "Alice"}); err != nil {
		t.Fatalf("set: %v", err)
	}

	// Read from nonvoter (node-2) via HTTP to confirm replication.
	nonvoterAddr := cluster.Nodes[2].HTTPAddrForTest()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		r, err := http.Get(fmt.Sprintf("http://%s/api/v1/anchortest.testValue/alice", nonvoterAddr))
		if err == nil {
			r.Body.Close()
			if r.StatusCode == http.StatusOK {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	resp2, err := http.Get(fmt.Sprintf("http://%s/api/v1/anchortest.testValue/alice", nonvoterAddr))
	if err != nil {
		t.Fatalf("get from nonvoter: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("get from nonvoter returned %s", resp2.Status)
	}
	var got testValue
	if err := json.NewDecoder(resp2.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Alice" {
		t.Fatalf("expected Name=Alice from nonvoter, got %q", got.Name)
	}
}
