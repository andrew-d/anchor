package server

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/andrew-d/anchor/internal/api"
	"github.com/andrew-d/anchor/internal/db"
)

// handleCheckin handles POST /api/checkin requests.
// Implemented in Task 3.
func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1 MB to prevent OOM from oversized requests.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Decode JSON request body
	var req api.CheckinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("checkin request decode error", "error", err)
		http.Error(w, "malformed JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ID == "" {
		slog.Warn("checkin request missing id")
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}

	// Extract remote IP from r.RemoteAddr
	remoteIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		// If SplitHostPort fails, use the whole RemoteAddr
		remoteIP = r.RemoteAddr
	}

	// Upsert agent into store
	agent := db.Agent{
		ID:         req.ID,
		Hostname:   req.Hostname,
		RemoteIP:   remoteIP,
		OS:         req.OS,
		Arch:       req.Arch,
		Distro:     req.Distro,
		LastSeenAt: time.Now().Unix(),
	}
	if err := s.store.UpsertAgent(r.Context(), agent); err != nil {
		slog.Error("upsert agent error", "agent_id", req.ID, "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Get agent's effective module set
	assignedNames, err := s.store.GetAgentModules(r.Context(), req.ID)
	if err != nil {
		slog.Error("get agent modules error", "agent_id", req.ID, "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Load current modules
	if _, err := s.loader.LoadAll(r.Context()); err != nil {
		slog.Error("load modules error", "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Build response modules by matching assigned names against loaded modules
	modules := make([]api.CheckinModule, 0)
	for _, name := range assignedNames {
		if mod, ok := s.loader.GetModule(name); ok {
			cm := api.CheckinModule{Name: name, Script: mod.Script}
			for _, art := range mod.Artifacts {
				cm.Artifacts = append(cm.Artifacts, api.CheckinArtifact{
					RelPath: art.RelPath,
					Hash:    art.Hash,
					Size:    art.Size,
					Mode:    uint32(art.Mode),
				})
			}
			modules = append(modules, cm)
		}
	}

	// Return JSON response
	resp := api.CheckinResponse{
		PollIntervalSeconds: s.opts.PollInterval,
		Modules:             modules,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode checkin response", "error", err)
	}
}

// handleReport handles POST /api/report requests.
// Implemented in Task 4.
func (s *Server) handleReport(w http.ResponseWriter, r *http.Request) {
	// Limit request body to 1 MB to prevent OOM from oversized requests.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	// Decode JSON request body
	var req api.ReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("report request decode error", "error", err)
		http.Error(w, "malformed JSON", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.AgentID == "" {
		slog.Warn("report request missing agent_id")
		http.Error(w, "missing agent_id", http.StatusBadRequest)
		return
	}

	if req.ModuleName == "" {
		slog.Warn("report request missing module_name")
		http.Error(w, "missing module_name", http.StatusBadRequest)
		return
	}

	if req.Status == "" {
		slog.Warn("report request missing status")
		http.Error(w, "missing status", http.StatusBadRequest)
		return
	}

	// Validate status is one of "ok", "changed", "error"
	validStatus := req.Status == "ok" || req.Status == "changed" || req.Status == "error"
	if !validStatus {
		slog.Warn("report request invalid status", "status", req.Status)
		http.Error(w, "invalid status", http.StatusBadRequest)
		return
	}

	// Insert module result
	result := db.ModuleResult{
		AgentID:    req.AgentID,
		ModuleName: req.ModuleName,
		Status:     req.Status,
		Stdout:     req.Stdout,
		Stderr:     req.Stderr,
		ExecutedAt: req.ExecutedAt,
	}
	if err := s.store.InsertModuleResult(r.Context(), result); err != nil {
		slog.Error("insert module result error", "agent_id", req.AgentID, "module_name", req.ModuleName, "error", err)
		http.Error(w, "server error", http.StatusInternalServerError)
		return
	}

	// Return success response
	resp := api.ReportResponse{OK: true}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode report response", "error", err)
	}
}

var hexHashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// handleGetArtifact handles GET /api/artifacts/{hash} requests.
func (s *Server) handleGetArtifact(w http.ResponseWriter, r *http.Request) {
	hash := r.PathValue("hash")
	if !hexHashPattern.MatchString(hash) {
		http.Error(w, "invalid hash", http.StatusBadRequest)
		return
	}

	art, ok := s.loader.GetArtifactByHash(hash)
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	http.ServeFile(w, r, art.DiskPath)
}

// handleHealthz handles GET /healthz requests.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if err := s.store.Ping(r.Context()); err != nil {
		slog.Error("health check failed", "error", err)
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{
			"status": "error",
			"error":  err.Error(),
		})
		return
	}

	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
	})
}
