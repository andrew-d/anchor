package anchor

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/andrew-d/anchor/internal/hclogslog"
	raftsqlite "github.com/andrew-d/raft-sqlite"
	"github.com/hashicorp/raft"
	_ "modernc.org/sqlite"
)

const (
	retainSnapshotCount = 2
)

// Config holds the configuration for an [App].
type Config struct {
	// DataDir is the directory where the SQLite database is stored.
	DataDir string

	// ListenAddr is the Raft TCP address (default ":12000").
	ListenAddr string

	// HTTPAddr is the HTTP API address (default ":11000").
	HTTPAddr string

	// NodeID is the unique identifier for this node.
	NodeID string

	// Bootstrap indicates this node should bootstrap a new cluster.
	// Mutually exclusive with JoinAddr.
	Bootstrap bool

	// JoinAddr is the HTTP address of an existing node to join.
	// Mutually exclusive with Bootstrap.
	JoinAddr string

	// Logger is the structured logger for the App. If nil, [slog.Default] is
	// used.
	Logger *slog.Logger
}

// App is the central coordinator. It wires together Raft, the FSM, the HTTP
// API, and user-defined modules.
type App struct {
	config  Config
	db      *sql.DB
	raft    *raft.Raft
	watches *watchHub
	kinds   map[string]kindInfo
	modules []Module

	httpServer *http.Server
	httpAddr   string // actual bound address from listener
	transport  *raft.NetworkTransport
	logStore   *raftsqlite.SQLiteStore
	snapStore  *raftsqlite.SnapshotStore

	logger *slog.Logger

	// ctx is a context derived from the one passed to Start. It is canceled
	// during Shutdown to signal module goroutines to stop.
	ctx    context.Context
	cancel context.CancelFunc

	// wg tracks goroutines started via InitContext.Go so that Shutdown can
	// wait for them to finish.
	wg sync.WaitGroup
}

// New creates a new App with the given configuration.
func New(config Config) *App {
	if config.ListenAddr == "" {
		config.ListenAddr = ":12000"
	}
	if config.HTTPAddr == "" {
		config.HTTPAddr = ":11000"
	}
	logger := config.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &App{
		config: config,
		kinds:  make(map[string]kindInfo),
		logger: logger,
	}
}

// RegisterModule registers a module to be initialized at startup.
func (a *App) RegisterModule(m Module) {
	a.modules = append(a.modules, m)
}

// IsLeaderForTest returns true if this node is the current Raft
// leader.
//
// Note that the cluster state can change immediately after this
// function returns, making it unsafe to use in production. This
// should only be used in tests.
func (a *App) IsLeaderForTest() bool {
	return a.raft.State() == raft.Leader
}

// HTTPAddrForTest returns the actual bound HTTP address. This may differ
// from Config.HTTPAddr when using port 0.
//
// This should only be used in tests.
func (a *App) HTTPAddrForTest() string {
	return a.httpAddr
}

// Start initializes and starts the App. It blocks until the context is
// canceled or an error occurs during startup.
func (a *App) Start(ctx context.Context) error {
	a.ctx, a.cancel = context.WithCancel(ctx)

	// 1. Open single SQLite database and set PRAGMAs.
	if err := os.MkdirAll(a.config.DataDir, 0o700); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	dbPath := filepath.Join(a.config.DataDir, "anchor.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	a.db = db

	pragmas := []string{
		"PRAGMA busy_timeout=10000;",
		"PRAGMA auto_vacuum=INCREMENTAL;",
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=FULL;",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			return fmt.Errorf("set pragma %q: %w", p, err)
		}
	}

	// 2. Create TxFactory and initialize raft-sqlite stores.
	txFactory := func(ctx context.Context, opts *sql.TxOptions) (*sql.Tx, error) {
		return db.BeginTx(ctx, opts)
	}

	logStore, err := raftsqlite.New(raftsqlite.Options{
		TxFactory: txFactory,
	})
	if err != nil {
		return fmt.Errorf("create log store: %w", err)
	}
	a.logStore = logStore

	snapStore, err := raftsqlite.NewSnapshotStore(raftsqlite.SnapshotStoreOptions{
		TxFactory: txFactory,
		Retain:    retainSnapshotCount,
	})
	if err != nil {
		return fmt.Errorf("create snapshot store: %w", err)
	}
	a.snapStore = snapStore

	// Initialize the FSM tables.
	f := (*fsm)(a)
	if err := f.initTable(); err != nil {
		return fmt.Errorf("create fsm table: %w", err)
	}

	// Create the watch hub now that the DB is ready, before modules init
	// so drain goroutines started during Init can query immediately.
	a.watches = newWatchHub(a.db, a.logger)

	// 3. Init modules (they register kinds via Register[T]).
	for _, m := range a.modules {
		ic := InitContext{
			App:    a,
			Logger: a.logger.With("module", m.Name()),
		}
		if err := m.Init(a.ctx, ic); err != nil {
			return fmt.Errorf("module %s init: %w", m.Name(), err)
		}
	}

	// 4. Create TCP transport. Pass nil for the advertise address so the
	// transport uses the listener's actual bound address (important when
	// ListenAddr uses port 0).
	raftLogger := hclogslog.New(a.logger.With("component", "raft"))
	transport, err := raft.NewTCPTransportWithLogger(a.config.ListenAddr, nil, 3, 10*time.Second, raftLogger)
	if err != nil {
		return fmt.Errorf("create transport: %w", err)
	}
	a.transport = transport

	// 5. Create Raft instance.
	raftConfig := raft.DefaultConfig()
	raftConfig.LocalID = raft.ServerID(a.config.NodeID)
	raftConfig.Logger = raftLogger

	ra, err := raft.NewRaft(raftConfig, f, logStore, logStore, snapStore, transport)
	if err != nil {
		return fmt.Errorf("create raft: %w", err)
	}
	a.raft = ra

	// 6. Bootstrap or join.
	if a.config.Bootstrap {
		configuration := raft.Configuration{
			Servers: []raft.Server{
				{
					ID:      raftConfig.LocalID,
					Address: transport.LocalAddr(),
				},
			},
		}
		fut := ra.BootstrapCluster(configuration)
		if err := fut.Error(); err != nil {
			// ErrCantBootstrap means the cluster already has state, which is fine.
			if err != raft.ErrCantBootstrap {
				return fmt.Errorf("bootstrap: %w", err)
			}
		}
	}

	// 7. Start HTTP server.
	if err := a.startHTTP(); err != nil {
		return fmt.Errorf("start HTTP: %w", err)
	}

	// 8. If joining, send join request to existing node.
	if a.config.JoinAddr != "" {
		if err := a.joinCluster(); err != nil {
			return fmt.Errorf("join cluster: %w", err)
		}
	}

	// 9. Store node metadata (Raft addr -> HTTP addr mapping).
	if a.config.Bootstrap {
		// Wait for leadership before writing metadata.
		a.wg.Go(func() { a.storeNodeMeta(a.ctx) })
	}

	a.logger.Info("started node", "node_id", a.config.NodeID, "raft_addr", a.config.ListenAddr, "http_addr", a.config.HTTPAddr)
	return nil
}

func (a *App) storeNodeMeta(ctx context.Context) {
	// Wait until we become leader (or context is canceled).
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		if a.raft.State() == raft.Leader {
			break
		}
	}

	meta := nodeMetaValue{HTTPAddr: a.httpAddr}
	data, err := json.Marshal(meta)
	if err != nil {
		a.logger.Error("failed to marshal node meta", "err", err)
		return
	}
	cmd := Command{
		Type:  CmdSet,
		Kind:  nodeMetaKind,
		Key:   a.config.NodeID,
		Value: data,
	}
	if err := a.applyCommand(cmd); err != nil {
		a.logger.Error("failed to store node meta", "err", err)
	}
}

func (a *App) joinCluster() error {
	body, err := json.Marshal(map[string]string{
		"node_id":   a.config.NodeID,
		"raft_addr": string(a.transport.LocalAddr()),
	})
	if err != nil {
		return err
	}

	resp, err := http.Post(
		fmt.Sprintf("http://%s/internal/join", a.config.JoinAddr),
		"application/json",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("join request returned %s", resp.Status)
	}
	return nil
}

// Shutdown gracefully stops the App.
func (a *App) Shutdown(ctx context.Context) error {
	// Signal all module goroutines to stop, then wait for them.
	if a.cancel != nil {
		a.cancel()
	}
	a.wg.Wait()

	var firstErr error
	saveErr := func(err error) {
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}

	if a.httpServer != nil {
		saveErr(a.httpServer.Shutdown(ctx))
	}
	if a.raft != nil {
		saveErr(a.raft.Shutdown().Error())
	}
	if a.logStore != nil {
		saveErr(a.logStore.Close())
	}
	if a.snapStore != nil {
		saveErr(a.snapStore.Close())
	}
	if a.transport != nil {
		saveErr(a.transport.Close())
	}
	if a.db != nil {
		saveErr(a.db.Close())
	}
	return firstErr
}

// nodeMetaKind is the internal kind used to store node metadata.
const nodeMetaKind = "_node_meta"

type nodeMetaValue struct {
	HTTPAddr string `json:"http_addr"`
}

// leaderHTTPAddr returns the HTTP address of the current leader.
func (a *App) leaderHTTPAddr() (string, error) {
	_, leaderID := a.raft.LeaderWithID()
	if leaderID == "" {
		return "", fmt.Errorf("no leader")
	}

	f := (*fsm)(a)
	raw, err := f.fsmGet(nodeMetaKind, string(leaderID))
	if err != nil {
		return "", err
	}
	if raw == nil {
		return "", fmt.Errorf("no metadata for leader %s", leaderID)
	}

	var meta nodeMetaValue
	if err := json.Unmarshal(raw, &meta); err != nil {
		return "", err
	}
	return meta.HTTPAddr, nil
}
