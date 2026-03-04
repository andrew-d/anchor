package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"
)

// UIAgentResponse represents an agent in the list response.
type UIAgentResponse struct {
	ID           string      `json:"id"`
	Hostname     string      `json:"hostname"`
	RemoteIP     string      `json:"remote_ip"`
	OS           string      `json:"os"`
	Arch         string      `json:"arch"`
	Distro       string      `json:"distro"`
	LastSeenAt   int64       `json:"last_seen_at"`
	Health       string      `json:"health"` // "healthy", "stale", or "unhealthy"
	ModuleCount  int         `json:"module_count"`
	ErrorCount   int         `json:"error_count"`
	Tags         []UITagResponse `json:"tags"`
}

// UITagResponse represents a tag in responses.
type UITagResponse struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ListAgentsResponse is the response for GET /api/agents.
type ListAgentsResponse struct {
	PollIntervalSeconds int                `json:"poll_interval_seconds"`
	Agents              []UIAgentResponse  `json:"agents"`
}

// AgentDetailResponse is the response for GET /api/agents/{id}.
type AgentDetailResponse struct {
	Agent          UIAgentDetailResponse `json:"agent"`
	Tags           []UITagResponse       `json:"tags"`
	ModuleResults  []UIModuleResultResponse `json:"module_results"`
}

// UIAgentDetailResponse represents an agent in the detail response.
type UIAgentDetailResponse struct {
	ID         string `json:"id"`
	Hostname   string `json:"hostname"`
	RemoteIP   string `json:"remote_ip"`
	OS         string `json:"os"`
	Arch       string `json:"arch"`
	Distro     string `json:"distro"`
	LastSeenAt int64  `json:"last_seen_at"`
	Health     string `json:"health"`
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
	// Check if stale
	if agent.LastSeenAt < now-staleThreshold {
		agent.Health = "stale"
		return
	}

	// Check if unhealthy (has error)
	if agent.ErrorCount > 0 {
		agent.Health = "unhealthy"
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
	staleThreshold := int64(2 * s.pollInterval)

	// Convert to response format
	var agentResponses []UIAgentResponse
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

		var tagResponses []UITagResponse
		for _, tag := range tags {
			tagResponses = append(tagResponses, UITagResponse{
				ID:   tag.ID,
				Name: tag.Name,
			})
		}

		uiAgent := UIAgentResponse{
			ID:          agent.ID,
			Hostname:    agent.Hostname,
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
		PollIntervalSeconds: s.pollInterval,
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
		slog.Debug("agent not found", "agent_id", agentID, "error", err)
		http.Error(w, "not found", http.StatusNotFound)
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
	staleThreshold := int64(2 * s.pollInterval)
	errorCount := 0
	for _, mr := range moduleResults {
		if mr.Status == "error" {
			errorCount++
		}
	}

	uiAgent := UIAgentResponse{
		ID:          agent.ID,
		Hostname:    agent.Hostname,
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
	var tagResponses []UITagResponse
	for _, tag := range tags {
		tagResponses = append(tagResponses, UITagResponse{
			ID:   tag.ID,
			Name: tag.Name,
		})
	}

	// Convert module results
	var moduleResultResponses []UIModuleResultResponse
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
		ID:         uiAgent.ID,
		Hostname:   uiAgent.Hostname,
		RemoteIP:   uiAgent.RemoteIP,
		OS:         uiAgent.OS,
		Arch:       uiAgent.Arch,
		Distro:     uiAgent.Distro,
		LastSeenAt: uiAgent.LastSeenAt,
		Health:     uiAgent.Health,
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
