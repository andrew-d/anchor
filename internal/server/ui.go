package server

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/andrew-d/anchor/internal/db"
)

// UIAgentResponse represents an agent in the list response.
type UIAgentResponse struct {
	ID          string          `json:"id"`
	Hostname    string          `json:"hostname"`
	DisplayName *string         `json:"display_name"`
	RemoteIP    string          `json:"remote_ip"`
	OS          string          `json:"os"`
	Arch        string          `json:"arch"`
	Distro      string          `json:"distro"`
	LastSeenAt  int64           `json:"last_seen_at"`
	Health      string          `json:"health"` // "healthy", "stale", or "unhealthy"
	ModuleCount int             `json:"module_count"`
	ErrorCount  int             `json:"error_count"`
	Tags        []UITagResponse `json:"tags"`
}

// UITagResponse represents a tag in responses.
type UITagResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ListAgentsResponse is the response for GET /api/agents.
type ListAgentsResponse struct {
	PollIntervalSeconds int               `json:"poll_interval_seconds"`
	Agents              []UIAgentResponse `json:"agents"`
}

// AgentDetailResponse is the response for GET /api/agents/{id}.
type AgentDetailResponse struct {
	Agent         UIAgentDetailResponse    `json:"agent"`
	Tags          []UITagResponse          `json:"tags"`
	ModuleResults []UIModuleResultResponse `json:"module_results"`
}

// UIAgentDetailResponse represents an agent in the detail response.
type UIAgentDetailResponse struct {
	ID          string  `json:"id"`
	Hostname    string  `json:"hostname"`
	DisplayName *string `json:"display_name"`
	RemoteIP    string  `json:"remote_ip"`
	OS          string  `json:"os"`
	Arch        string  `json:"arch"`
	Distro      string  `json:"distro"`
	LastSeenAt  int64   `json:"last_seen_at"`
	Health      string  `json:"health"`
}

// UIModuleResultResponse represents a module result in the response.
type UIModuleResultResponse struct {
	ModuleName string `json:"module_name"`
	Status     string `json:"status"` // "ok", "changed", "error"
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExecutedAt int64  `json:"executed_at"`
}

// computeHealth computes the health status for an agent.
// - "unhealthy": has error in recent module results
// - "stale": hasn't checked in within 2x poll interval
// - "healthy": checked in recently and no errors
func (s *Server) computeHealth(agent *UIAgentResponse, now int64, staleThreshold int64) {
	// Check if unhealthy first — errors should not be masked by staleness.
	if agent.ErrorCount > 0 {
		agent.Health = "unhealthy"
		return
	}

	// Check if stale
	if agent.LastSeenAt < now-staleThreshold {
		agent.Health = "stale"
		return
	}

	// Otherwise healthy
	agent.Health = "healthy"
}

// handleListAgents handles GET /api/agents.
func (s *Server) handleListAgents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// Get all agents
	agents, err := s.store.ListAgents(ctx)
	if err != nil {
		slog.Error("failed to list agents", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	now := time.Now().Unix()
	staleThreshold := int64(2 * s.opts.PollInterval)

	// Convert to response format
	agentResponses := []UIAgentResponse{}
	for _, agent := range agents {
		// Get module results for this agent
		moduleResults, err := s.store.GetLatestModuleResults(ctx, agent.ID)
		if err != nil {
			slog.Error("failed to get module results", "agent_id", agent.ID, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		// Count modules and errors
		moduleCount := len(moduleResults)
		errorCount := 0
		for _, mr := range moduleResults {
			if mr.Status == "error" {
				errorCount++
			}
		}

		// Get agent tags
		tags, err := s.store.GetAgentTags(ctx, agent.ID)
		if err != nil {
			slog.Error("failed to get agent tags", "agent_id", agent.ID, "error", err)
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		tagResponses := []UITagResponse{}
		for _, tag := range tags {
			tagResponses = append(tagResponses, UITagResponse{
				ID:   tag.ID,
				Name: tag.Name,
			})
		}

		uiAgent := UIAgentResponse{
			ID:          agent.ID,
			Hostname:    agent.Hostname,
			DisplayName: agent.DisplayName,
			RemoteIP:    agent.RemoteIP,
			OS:          agent.OS,
			Arch:        agent.Arch,
			Distro:      agent.Distro,
			LastSeenAt:  agent.LastSeenAt,
			ModuleCount: moduleCount,
			ErrorCount:  errorCount,
			Tags:        tagResponses,
		}

		s.computeHealth(&uiAgent, now, staleThreshold)
		agentResponses = append(agentResponses, uiAgent)
	}

	// Prepare response
	resp := ListAgentsResponse{
		PollIntervalSeconds: s.opts.PollInterval,
		Agents:              agentResponses,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// handleGetAgent handles GET /api/agents/{id}.
func (s *Server) handleGetAgent(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := r.PathValue("id")

	// Get agent
	agent, err := s.store.GetAgent(ctx, agentID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			slog.Debug("agent not found", "agent_id", agentID, "error", err)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get agent", "agent_id", agentID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Get module results
	moduleResults, err := s.store.GetLatestModuleResults(ctx, agentID)
	if err != nil {
		slog.Error("failed to get module results", "agent_id", agentID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Get tags
	tags, err := s.store.GetAgentTags(ctx, agentID)
	if err != nil {
		slog.Error("failed to get agent tags", "agent_id", agentID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	// Compute health
	now := time.Now().Unix()
	staleThreshold := int64(2 * s.opts.PollInterval)
	errorCount := 0
	for _, mr := range moduleResults {
		if mr.Status == "error" {
			errorCount++
		}
	}

	uiAgent := UIAgentResponse{
		ID:          agent.ID,
		Hostname:    agent.Hostname,
		DisplayName: agent.DisplayName,
		RemoteIP:    agent.RemoteIP,
		OS:          agent.OS,
		Arch:        agent.Arch,
		Distro:      agent.Distro,
		LastSeenAt:  agent.LastSeenAt,
		ModuleCount: len(moduleResults),
		ErrorCount:  errorCount,
	}
	s.computeHealth(&uiAgent, now, staleThreshold)

	// Convert tags
	tagResponses := []UITagResponse{}
	for _, tag := range tags {
		tagResponses = append(tagResponses, UITagResponse{
			ID:   tag.ID,
			Name: tag.Name,
		})
	}

	// Convert module results
	moduleResultResponses := []UIModuleResultResponse{}
	for _, mr := range moduleResults {
		moduleResultResponses = append(moduleResultResponses, UIModuleResultResponse{
			ModuleName: mr.ModuleName,
			Status:     mr.Status,
			Stdout:     mr.Stdout,
			Stderr:     mr.Stderr,
			ExecutedAt: mr.ExecutedAt,
		})
	}

	agentDetail := UIAgentDetailResponse{
		ID:          uiAgent.ID,
		Hostname:    uiAgent.Hostname,
		DisplayName: uiAgent.DisplayName,
		RemoteIP:    uiAgent.RemoteIP,
		OS:          uiAgent.OS,
		Arch:        uiAgent.Arch,
		Distro:      uiAgent.Distro,
		LastSeenAt:  uiAgent.LastSeenAt,
		Health:      uiAgent.Health,
	}

	resp := AgentDetailResponse{
		Agent:         agentDetail,
		Tags:          tagResponses,
		ModuleResults: moduleResultResponses,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// --- Tag Management API ---

// ListTagsResponse is the response for GET /api/tags.
type ListTagsResponse struct {
	Tags []UITagResponse `json:"tags"`
}

// CreateTagRequest is the request for POST /api/tags.
type CreateTagRequest struct {
	Name string `json:"name"`
}

// OKResponse is used for simple success responses.
type OKResponse struct {
	OK bool `json:"ok"`
}

// handleListTags handles GET /api/tags.
func (s *Server) handleListTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	tags, err := s.store.ListTags(ctx)
	if err != nil {
		slog.Error("failed to list tags", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	tagResponses := []UITagResponse{}
	for _, tag := range tags {
		tagResponses = append(tagResponses, UITagResponse{
			ID:   tag.ID,
			Name: tag.Name,
		})
	}

	resp := ListTagsResponse{
		Tags: tagResponses,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// handleCreateTag handles POST /api/tags.
func (s *Server) handleCreateTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req CreateTagRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate tag name is not empty
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		http.Error(w, "tag name cannot be empty", http.StatusBadRequest)
		return
	}

	tag, err := s.store.CreateTag(ctx, req.Name)
	if err != nil {
		slog.Error("failed to create tag", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := UITagResponse{
		ID:   tag.ID,
		Name: tag.Name,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// handleDeleteTag handles DELETE /api/tags/{id}.
func (s *Server) handleDeleteTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid tag id", http.StatusBadRequest)
		return
	}

	err = s.store.DeleteTag(ctx, id)
	if err != nil {
		slog.Error("failed to delete tag", "tag_id", id, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := OKResponse{OK: true}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// --- Agent Tags API ---

// SetAgentTagsRequest is the request for PUT /api/agents/{id}/tags.
type SetAgentTagsRequest struct {
	TagIDs []int64 `json:"tag_ids"`
}

// handleSetAgentTags handles PUT /api/agents/{id}/tags.
func (s *Server) handleSetAgentTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := r.PathValue("id")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req SetAgentTagsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	err := s.store.SetAgentTags(ctx, agentID, req.TagIDs)
	if err != nil {
		slog.Error("failed to set agent tags", "agent_id", agentID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := OKResponse{OK: true}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// --- Agent Display Name API ---

// SetDisplayNameRequest is the request for PUT /api/agents/{id}/name.
type SetDisplayNameRequest struct {
	DisplayName *string `json:"display_name"`
}

// handleSetAgentDisplayName handles PUT /api/agents/{id}/name.
func (s *Server) handleSetAgentDisplayName(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := r.PathValue("id")

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req SetDisplayNameRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if req.DisplayName != nil {
		trimmed := strings.TrimSpace(*req.DisplayName)
		if trimmed == "" {
			http.Error(w, "display name cannot be empty", http.StatusBadRequest)
			return
		}
		req.DisplayName = &trimmed
	}

	if err := s.store.SetAgentDisplayName(ctx, agentID, req.DisplayName); err != nil {
		slog.Error("failed to set agent display name", "agent_id", agentID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := OKResponse{OK: true}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// --- Module Assignment API ---

// ModuleAssignmentResponse represents a module assignment in API responses.
type ModuleAssignmentResponse struct {
	ID         int64   `json:"id"`
	ModuleName string  `json:"module_name"`
	AgentID    *string `json:"agent_id"`
	TagID      *int64  `json:"tag_id"`
}

// ListAssignmentsResponse is the response for GET /api/assignments.
type ListAssignmentsResponse struct {
	Assignments []ModuleAssignmentResponse `json:"assignments"`
}

// CreateAssignmentRequest is the request for POST /api/assignments.
type CreateAssignmentRequest struct {
	ModuleName string  `json:"module_name"`
	AgentID    *string `json:"agent_id"`
	TagID      *int64  `json:"tag_id"`
}

// handleListAssignments handles GET /api/assignments.
func (s *Server) handleListAssignments(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	assignments, err := s.store.ListAssignments(ctx)
	if err != nil {
		slog.Error("failed to list assignments", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	assignmentResponses := []ModuleAssignmentResponse{}
	for _, assignment := range assignments {
		assignmentResponses = append(assignmentResponses, ModuleAssignmentResponse{
			ID:         assignment.ID,
			ModuleName: assignment.ModuleName,
			AgentID:    assignment.AgentID,
			TagID:      assignment.TagID,
		})
	}

	resp := ListAssignmentsResponse{
		Assignments: assignmentResponses,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// handleCreateAssignment handles POST /api/assignments.
func (s *Server) handleCreateAssignment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req CreateAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	// Validate exactly one of agentID or tagID is set
	if (req.AgentID == nil && req.TagID == nil) || (req.AgentID != nil && req.TagID != nil) {
		http.Error(w, "exactly one of agent_id or tag_id must be set", http.StatusBadRequest)
		return
	}

	// Validate module_name is not empty
	if strings.TrimSpace(req.ModuleName) == "" {
		http.Error(w, "module_name cannot be empty", http.StatusBadRequest)
		return
	}

	assignmentID, err := s.store.AssignModule(ctx, req.ModuleName, req.AgentID, req.TagID)
	if err != nil {
		slog.Error("failed to assign module", "module_name", req.ModuleName, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	createdAssignment := ModuleAssignmentResponse{
		ID:         assignmentID,
		ModuleName: req.ModuleName,
		AgentID:    req.AgentID,
		TagID:      req.TagID,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(createdAssignment); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// handleDeleteAssignment handles DELETE /api/assignments/{id}.
func (s *Server) handleDeleteAssignment(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	idStr := r.PathValue("id")

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		http.Error(w, "invalid assignment id", http.StatusBadRequest)
		return
	}

	err = s.store.UnassignModule(ctx, id)
	if err != nil {
		slog.Error("failed to unassign module", "assignment_id", id, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	resp := OKResponse{OK: true}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// --- Effective Modules API ---

// AgentModuleDetailResponse represents a module with source info.
type AgentModuleDetailResponse struct {
	Name         string `json:"name"`
	Source       string `json:"source"`
	AssignmentID int64  `json:"assignment_id"`
}

// AgentEffectiveModulesResponse is the response for GET /api/agents/{id}/modules.
type AgentEffectiveModulesResponse struct {
	Modules []AgentModuleDetailResponse `json:"modules"`
}

// handleGetAgentModules handles GET /api/agents/{id}/modules.
func (s *Server) handleGetAgentModules(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	agentID := r.PathValue("id")

	moduleDetails, err := s.store.GetAgentModuleDetails(ctx, agentID)
	if err != nil {
		slog.Error("failed to get agent module details", "agent_id", agentID, "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	moduleResponses := []AgentModuleDetailResponse{}
	for _, md := range moduleDetails {
		moduleResponses = append(moduleResponses, AgentModuleDetailResponse{
			Name:         md.ModuleName,
			Source:       md.Source,
			AssignmentID: md.AssignmentID,
		})
	}

	resp := AgentEffectiveModulesResponse{
		Modules: moduleResponses,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// --- Modules List API ---

// ModuleMetadataResponse represents a module in the modules list.
type ModuleMetadataResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Filename    string `json:"filename"`
	Error       string `json:"error,omitempty"`
}

// ListModulesResponse is the response for GET /api/modules.
type ListModulesResponse struct {
	Modules []ModuleMetadataResponse `json:"modules"`
}

// handleListModules handles GET /api/modules.
func (s *Server) handleListModules(w http.ResponseWriter, r *http.Request) {
	modules, err := s.loader.LoadAll(r.Context())
	if err != nil {
		slog.Error("failed to load modules", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	moduleResponses := []ModuleMetadataResponse{}
	for _, mod := range modules {
		moduleResponses = append(moduleResponses, ModuleMetadataResponse{
			Name:        mod.Name,
			Description: mod.Description,
			Filename:    mod.Filename,
		})
	}

	// Append errored modules so the UI can display them
	for _, modErr := range s.loader.LoadErrors() {
		moduleResponses = append(moduleResponses, ModuleMetadataResponse{
			Filename: modErr.Filename,
			Error:    modErr.Error,
		})
	}

	// Sort by filename so errored modules appear in their natural position
	slices.SortFunc(moduleResponses, func(a, b ModuleMetadataResponse) int {
		return strings.Compare(a.Filename, b.Filename)
	})

	resp := ListModulesResponse{
		Modules: moduleResponses,
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}
