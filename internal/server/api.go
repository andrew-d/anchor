package server

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/andrew-d/anchor/internal/db"
)

// CheckinRequest is the JSON body of POST /api/checkin.
type CheckinRequest struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Distro   string `json:"distro"`
}

// CheckinResponse is the JSON response from POST /api/checkin.
type CheckinResponse struct {
	PollIntervalSeconds int             `json:"poll_interval_seconds"`
	Modules             []CheckinModule `json:"modules"`
}

// CheckinModule is a module entry in the checkin response.
type CheckinModule struct {
	Name   string `json:"name"`
	Script string `json:"script"`
}

// ReportRequest is the JSON body of POST /api/report.
type ReportRequest struct {
	AgentID    string `json:"agent_id"`
	ModuleName string `json:"module_name"`
	Status     string `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExecutedAt int64  `json:"executed_at"`
}

// ReportResponse is the JSON response from POST /api/report.
type ReportResponse struct {
	OK bool `json:"ok"`
}

// handleCheckin handles POST /api/checkin requests.
// Implemented in Task 3.
func (s *Server) handleCheckin(w http.ResponseWriter, r *http.Request) {
	// Decode JSON request body
	var req CheckinRequest
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
	modules := make([]CheckinModule, 0)
	for _, name := range assignedNames {
		if mod, ok := s.loader.GetModule(name); ok {
			modules = append(modules, CheckinModule{Name: name, Script: mod.Script})
		}
	}

	// Return JSON response
	resp := CheckinResponse{
		PollIntervalSeconds: s.pollInterval,
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
	// Decode JSON request body
	var req ReportRequest
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
	resp := ReportResponse{OK: true}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode report response", "error", err)
	}
}
