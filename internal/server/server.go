package server

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"path/filepath"
	"time"

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
	resultsKeep  int
}

// New creates a new Server.
func New(port int, modulesDir string, dataDir string) *Server {
	return &Server{
		port:         port,
		modulesDir:   modulesDir,
		dataDir:      dataDir,
		pollInterval: 300, // default 300 seconds
		resultsKeep:  100, // default: keep 100 results per agent+module
	}
}

// SetResultsKeep sets the number of module results to keep per agent+module pair.
func (s *Server) SetResultsKeep(n int) {
	s.resultsKeep = n
}

// Run starts the HTTP server and blocks until the context is cancelled or it returns an error.
func (s *Server) Run(ctx context.Context) error {
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
	mux.HandleFunc("PUT /api/agents/{id}/name", s.handleSetAgentDisplayName)
	mux.HandleFunc("GET /api/assignments", s.handleListAssignments)
	mux.HandleFunc("POST /api/assignments", s.handleCreateAssignment)
	mux.HandleFunc("DELETE /api/assignments/{id}", s.handleDeleteAssignment)
	mux.HandleFunc("GET /api/agents/{id}/modules", s.handleGetAgentModules)
	mux.HandleFunc("GET /api/modules", s.handleListModules)
	mux.HandleFunc("GET /api/artifacts/{hash}", s.handleGetArtifact)
	mux.HandleFunc("GET /healthz", s.handleHealthz)

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

	// Start background pruning of old module results
	go s.pruneLoop(ctx)

	addr := fmt.Sprintf(":%d", s.port)
	slog.Info("starting server", "addr", addr, "modules_dir", s.modulesDir, "data_dir", s.dataDir, "results_keep", s.resultsKeep)

	httpServer := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Shut down gracefully when context is cancelled
	go func() {
		<-ctx.Done()
		slog.Info("server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		httpServer.Shutdown(shutdownCtx)
	}()

	err = httpServer.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// pruneLoop periodically deletes old module results.
func (s *Server) pruneLoop(ctx context.Context) {
	// Run once at startup to clean up any existing excess rows.
	s.pruneOnce(ctx)

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.pruneOnce(ctx)
		}
	}
}

func (s *Server) pruneOnce(ctx context.Context) {
	deleted, err := s.store.PruneModuleResults(ctx, s.resultsKeep)
	if err != nil {
		slog.Error("failed to prune module results", "error", err)
		return
	}
	if deleted > 0 {
		slog.Info("pruned old module results", "deleted", deleted)
	}
}
