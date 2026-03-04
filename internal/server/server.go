package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/andrew-d/anchor/internal/db"
	"github.com/andrew-d/anchor/internal/module"
)

// Server is the anchor HTTP server.
type Server struct {
	port         int
	modulesDir   string
	dataDir      string
	store        db.Store
	loader       *module.Loader
	pollInterval int
}

// New creates a new Server.
func New(port int, modulesDir string, dataDir string) *Server {
	return &Server{
		port:         port,
		modulesDir:   modulesDir,
		dataDir:      dataDir,
		pollInterval: 300, // default 300 seconds
	}
}

// Run starts the HTTP server and blocks until it returns an error.
func (s *Server) Run() error {
	// Open the SQLite database
	dbPath := filepath.Join(s.dataDir, "anchor.db")
	store, err := db.Open(dbPath)
	if err != nil {
		return fmt.Errorf("opening database: %w", err)
	}
	defer store.Close()
	s.store = store

	// Create a module loader
	s.loader = module.NewLoader(s.modulesDir)

	// Create HTTP mux and register routes
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/checkin", s.handleCheckin)
	mux.HandleFunc("POST /api/report", s.handleReport)
	mux.HandleFunc("GET /api/agents", s.handleListAgents)
	mux.HandleFunc("GET /api/agents/{id}", s.handleGetAgent)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "anchor server")
	})

	addr := fmt.Sprintf(":%d", s.port)
	slog.Info("starting server", "addr", addr, "modules_dir", s.modulesDir, "data_dir", s.dataDir)
	return http.ListenAndServe(addr, mux)
}
