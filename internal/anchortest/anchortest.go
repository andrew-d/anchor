// Package anchortest provides test helpers for spinning up multi-node
// anchor clusters. It handles Raft bootstrapping, node joining, and leader
// election so that module integration tests can focus on behavior.
package anchortest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/andrew-d/anchor"
	"github.com/neilotoole/slogt"
)

// Cluster is a running test cluster of anchor nodes.
type Cluster struct {
	// Nodes contains all nodes in the cluster, indexed by their creation
	// order. Node 0 is always the initial bootstrap node.
	Nodes []*anchor.App
}

// NodeOptions holds per-node configuration overrides for a test cluster.
type NodeOptions struct {
	// Nonvoter, when true, causes this node to join as a non-voting
	// member. Ignored for node 0 (the bootstrap node is always a voter).
	Nonvoter bool

	// OS overrides the detected operating system for this node.
	// If empty, the node uses runtime.GOOS.
	OS string
}

// ClusterOptions configures a test cluster created by NewWithOptions.
type ClusterOptions struct {
	// NumNodes is the number of nodes in the cluster (must be >= 1).
	NumNodes int

	// NodeOptions maps a node index to per-node overrides. Entries for
	// indices outside [0, NumNodes) are ignored.
	NodeOptions map[int]NodeOptions

	// ModsFn, if non-nil, is called once per node with the node's index
	// to create modules for that node.
	ModsFn func(nodeIndex int) []anchor.Module
}

// New creates and starts a test cluster with numNodes nodes. Node 0 is
// bootstrapped as the initial leader, and the remaining nodes join via
// node 0's HTTP API. The cluster is shut down when t completes.
//
// modsFn, if non-nil, is called once per node with the node's index to
// create modules for that node.
func New(t *testing.T, numNodes int, modsFn func(nodeIndex int) []anchor.Module) *Cluster {
	t.Helper()
	return NewWithOptions(t, ClusterOptions{
		NumNodes: numNodes,
		ModsFn:   modsFn,
	})
}

// NewWithOptions creates and starts a test cluster with the given options.
func NewWithOptions(t *testing.T, opts ClusterOptions) *Cluster {
	t.Helper()
	if opts.NumNodes < 1 {
		t.Fatal("anchortest.NewWithOptions: NumNodes must be >= 1")
	}

	ctx, cancel := context.WithCancel(context.Background())

	apps := make([]*anchor.App, opts.NumNodes)

	logger := slogt.New(t)

	// Bootstrap node 0.
	node0Cfg := anchor.Config{
		DataDir:    t.TempDir(),
		ListenAddr: "127.0.0.1:0",
		HTTPAddr:   "127.0.0.1:0",
		NodeID:     "node-0",
		Bootstrap:  true,
		Logger:     logger,
	}
	if no, ok := opts.NodeOptions[0]; ok {
		node0Cfg.OS = no.OS
	}
	apps[0] = anchor.New(node0Cfg)
	if opts.ModsFn != nil {
		for _, m := range opts.ModsFn(0) {
			apps[0].RegisterModule(m)
		}
	}
	if err := apps[0].Start(ctx); err != nil {
		cancel()
		t.Fatalf("start node-0: %v", err)
	}

	// Wait for node 0 to become leader before accepting joins.
	waitForLeader(t, apps[0], 10*time.Second)

	joinAddr := apps[0].HTTPAddrForTest()

	// Start and join remaining nodes.
	for i := 1; i < opts.NumNodes; i++ {
		cfg := anchor.Config{
			DataDir:    t.TempDir(),
			ListenAddr: "127.0.0.1:0",
			HTTPAddr:   "127.0.0.1:0",
			NodeID:     fmt.Sprintf("node-%d", i),
			JoinAddr:   joinAddr,
			Logger:     logger,
		}
		if no, ok := opts.NodeOptions[i]; ok {
			cfg.Nonvoter = no.Nonvoter
			cfg.OS = no.OS
		}
		apps[i] = anchor.New(cfg)
		if opts.ModsFn != nil {
			for _, m := range opts.ModsFn(i) {
				apps[i].RegisterModule(m)
			}
		}
		if err := apps[i].Start(ctx); err != nil {
			cancel()
			t.Fatalf("start node-%d: %v", i, err)
		}
	}

	t.Cleanup(func() {
		cancel()
		for i := range apps {
			if err := apps[i].Shutdown(context.Background()); err != nil {
				t.Logf("shutdown node-%d: %v", i, err)
			}
		}
	})

	return &Cluster{Nodes: apps}
}

// Leader returns the current leader node, or nil if there is no leader.
func (c *Cluster) Leader() *anchor.App {
	for _, app := range c.Nodes {
		if app.IsLeaderForTest() {
			return app
		}
	}
	return nil
}

// LeaderIndex returns the index of the current leader in Nodes, or -1
// if there is no leader.
func (c *Cluster) LeaderIndex() int {
	for i, app := range c.Nodes {
		if app.IsLeaderForTest() {
			return i
		}
	}
	return -1
}

// WaitForLeader waits until any node in the cluster reports as leader.
// It returns the leader's index in Nodes, or fails the test on timeout.
func (c *Cluster) WaitForLeader(t *testing.T, timeout time.Duration) int {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for i, app := range c.Nodes {
			if app.IsLeaderForTest() {
				return i
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("no leader elected within timeout")
	return -1
}

func waitForLeader(t *testing.T, app *anchor.App, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if app.IsLeaderForTest() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("node did not become leader within timeout")
}
