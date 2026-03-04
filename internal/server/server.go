package server

import (
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/andrew-d/anchor/internal/db"
	"github.com/andrew-d/anchor/internal/module"
	anchostatic "github.com/andrew-d/anchor/static"
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
	mux.HandleFunc("GET /api/tags", s.handleListTags)
	mux.HandleFunc("POST /api/tags", s.handleCreateTag)
	mux.HandleFunc("DELETE /api/tags/{id}", s.handleDeleteTag)
	mux.HandleFunc("PUT /api/agents/{id}/tags", s.handleSetAgentTags)
	mux.HandleFunc("GET /api/assignments", s.handleListAssignments)
	mux.HandleFunc("POST /api/assignments", s.handleCreateAssignment)
	mux.HandleFunc("DELETE /api/assignments/{id}", s.handleDeleteAssignment)
	mux.HandleFunc("GET /api/agents/{id}/modules", s.handleGetAgentModules)
	mux.HandleFunc("GET /api/modules", s.handleListModules)

	// Serve static files from embedded filesystem
	staticSub, err := fs.Sub(anchostatic.FS, ".")
	if err != nil {
		return fmt.Errorf("creating static filesystem sub: %w", err)
	}
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))

	// Serve index.html for the SPA entry point
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		data, err := anchostatic.FS.ReadFile("index.html")
		if err != nil {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if _, err := w.Write(data); err != nil {
			slog.Error("failed to write response", "error", err)
		}
	})

	addr := fmt.Sprintf(":%d", s.port)
	slog.Info("starting server", "addr", addr, "modules_dir", s.modulesDir, "data_dir", s.dataDir)
	return http.ListenAndServe(addr, mux)
}
