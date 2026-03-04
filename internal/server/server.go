package server

import (
	"fmt"
	"log/slog"
	"net/http"
)

// Server is the anchor HTTP server.
type Server struct {
	port       int
	modulesDir string
	dataDir    string
}

// New creates a new Server.
func New(port int, modulesDir string, dataDir string) *Server {
	return &Server{
		port:       port,
		modulesDir: modulesDir,
		dataDir:    dataDir,
	}
}

// Run starts the HTTP server and blocks until it returns an error.
func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "anchor server")
	})

	addr := fmt.Sprintf(":%d", s.port)
	slog.Info("starting server", "addr", addr, "modules_dir", s.modulesDir)
	return http.ListenAndServe(addr, mux)
}
