package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/andrew-d/anchor/internal/db"
)

// TestListAgentsHealthyStatus tests AC7.3: agent with recent check-in and
// module results with status "ok" or "changed" appears as "healthy".
func TestListAgentsHealthyStatus(t *testing.T) {
	s, store, _ := newTestServer(t)

	// Create agent with recent check-in
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-1",
		Hostname:   "web-01",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	// Add module results with status "ok" and "changed"
	err = store.InsertModuleResult(context.Background(), db.ModuleResult{
		AgentID:    "agent-1",
		ModuleName: "module-a",
		Status:     "ok",
		Stdout:     "ok output",
		Stderr:     "",
		ExecutedAt: now,
	})
	if err != nil {
		t.Fatalf("Failed to insert module result: %v", err)
	}

	err = store.InsertModuleResult(context.Background(), db.ModuleResult{
		AgentID:    "agent-1",
		ModuleName: "module-b",
		Status:     "changed",
		Stdout:     "changed output",
		Stderr:     "",
		ExecutedAt: now,
	})
	if err != nil {
		t.Fatalf("Failed to insert module result: %v", err)
	}

	// Make request
	ts := httptest.NewServer(http.HandlerFunc(s.handleListAgents))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListAgentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(result.Agents))
	}

	agent_resp := result.Agents[0]
	if agent_resp.Health != "healthy" {
		t.Errorf("Expected health 'healthy', got '%s'", agent_resp.Health)
	}
	if agent_resp.ModuleCount != 2 {
		t.Errorf("Expected module_count 2, got %d", agent_resp.ModuleCount)
	}
	if agent_resp.ErrorCount != 0 {
		t.Errorf("Expected error_count 0, got %d", agent_resp.ErrorCount)
	}
}

// TestListAgentsUnhealthyStatus tests AC7.1: agent with recent check-in and
// module result with status "error" appears as "unhealthy".
func TestListAgentsUnhealthyStatus(t *testing.T) {
	s, store, _ := newTestServer(t)

	// Create agent with recent check-in
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-2",
		Hostname:   "web-02",
		RemoteIP:   "10.0.0.2",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	// Add module result with error status
	err = store.InsertModuleResult(context.Background(), db.ModuleResult{
		AgentID:    "agent-2",
		ModuleName: "module-a",
		Status:     "error",
		Stdout:     "error output",
		Stderr:     "error message",
		ExecutedAt: now,
	})
	if err != nil {
		t.Fatalf("Failed to insert module result: %v", err)
	}

	// Make request
	ts := httptest.NewServer(http.HandlerFunc(s.handleListAgents))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListAgentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(result.Agents))
	}

	agent_resp := result.Agents[0]
	if agent_resp.Health != "unhealthy" {
		t.Errorf("Expected health 'unhealthy', got '%s'", agent_resp.Health)
	}
	if agent_resp.ErrorCount != 1 {
		t.Errorf("Expected error_count 1, got %d", agent_resp.ErrorCount)
	}
}

// TestListAgentsStaleStatus tests AC7.2: agent not seen within 2x poll interval
// appears as "stale".
func TestListAgentsStaleStatus(t *testing.T) {
	s, store, _ := newTestServer(t)

	// Create agent with old check-in (older than 2x poll interval)
	now := time.Now().Unix()
	oldTime := now - int64(2*s.pollInterval) - 100 // older than threshold
	agent := db.Agent{
		ID:         "agent-3",
		Hostname:   "web-03",
		RemoteIP:   "10.0.0.3",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: oldTime,
	}
	err := store.UpsertAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	// Make request
	ts := httptest.NewServer(http.HandlerFunc(s.handleListAgents))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListAgentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(result.Agents))
	}

	agent_resp := result.Agents[0]
	if agent_resp.Health != "stale" {
		t.Errorf("Expected health 'stale', got '%s'", agent_resp.Health)
	}
}

// TestListAgentsHealthyNoModules tests agent with no module results and
// recent check-in is "healthy".
func TestListAgentsHealthyNoModules(t *testing.T) {
	s, store, _ := newTestServer(t)

	// Create agent with recent check-in and no module results
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-4",
		Hostname:   "web-04",
		RemoteIP:   "10.0.0.4",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	// Make request
	ts := httptest.NewServer(http.HandlerFunc(s.handleListAgents))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListAgentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Errorf("Expected 1 agent, got %d", len(result.Agents))
	}

	agent_resp := result.Agents[0]
	if agent_resp.Health != "healthy" {
		t.Errorf("Expected health 'healthy', got '%s'", agent_resp.Health)
	}
	if agent_resp.ModuleCount != 0 {
		t.Errorf("Expected module_count 0, got %d", agent_resp.ModuleCount)
	}
}

// TestGetAgentDetail tests GET /api/agents/{id} returns correct agent detail
// with tags and module results.
func TestGetAgentDetail(t *testing.T) {
	s, store, _ := newTestServer(t)

	// Create agent
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-5",
		Hostname:   "web-05",
		RemoteIP:   "10.0.0.5",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	// Create tag and assign to agent
	tag, err := store.CreateTag(context.Background(), "webservers")
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	err = store.SetAgentTags(context.Background(), "agent-5", []int64{tag.ID})
	if err != nil {
		t.Fatalf("Failed to set agent tags: %v", err)
	}

	// Add module results
	err = store.InsertModuleResult(context.Background(), db.ModuleResult{
		AgentID:    "agent-5",
		ModuleName: "00_base",
		Status:     "ok",
		Stdout:     "ok output",
		Stderr:     "",
		ExecutedAt: now,
	})
	if err != nil {
		t.Fatalf("Failed to insert module result: %v", err)
	}

	// Use mux with path parameters
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agents/{id}", s.handleGetAgent)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents/agent-5")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result AgentDetailResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Verify agent detail
	if result.Agent.ID != "agent-5" {
		t.Errorf("Expected agent ID 'agent-5', got '%s'", result.Agent.ID)
	}
	if result.Agent.Hostname != "web-05" {
		t.Errorf("Expected hostname 'web-05', got '%s'", result.Agent.Hostname)
	}

	// Verify tags
	if len(result.Tags) != 1 {
		t.Errorf("Expected 1 tag, got %d", len(result.Tags))
	} else if result.Tags[0].Name != "webservers" {
		t.Errorf("Expected tag name 'webservers', got '%s'", result.Tags[0].Name)
	}

	// Verify module results
	if len(result.ModuleResults) != 1 {
		t.Errorf("Expected 1 module result, got %d", len(result.ModuleResults))
	} else {
		mr := result.ModuleResults[0]
		if mr.ModuleName != "00_base" {
			t.Errorf("Expected module name '00_base', got '%s'", mr.ModuleName)
		}
		if mr.Status != "ok" {
			t.Errorf("Expected status 'ok', got '%s'", mr.Status)
		}
	}
}

// TestGetAgentNotFound tests GET /api/agents/{nonexistent} returns 404.
func TestGetAgentNotFound(t *testing.T) {
	s, _, _ := newTestServer(t)

	// Use httptest with a real mux to test path params
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agents/{id}", s.handleGetAgent)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents/nonexistent")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}
}

// TestListAgentsResponse tests that the list response includes poll_interval_seconds.
func TestListAgentsResponse(t *testing.T) {
	s, store, _ := newTestServer(t)

	// Create a simple agent
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-6",
		Hostname:   "web-06",
		RemoteIP:   "10.0.0.6",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(context.Background(), agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(s.handleListAgents))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListAgentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if result.PollIntervalSeconds != 300 {
		t.Errorf("Expected poll_interval_seconds 300, got %d", result.PollIntervalSeconds)
	}
}

// TestListAgentsMultipleStatus tests that agents are correctly grouped by health status.
func TestListAgentsMultipleStatus(t *testing.T) {
	s, store, _ := newTestServer(t)

	now := time.Now().Unix()

	// Create healthy agent
	agent1 := db.Agent{
		ID:         "healthy-agent",
		Hostname:   "web-healthy",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	store.UpsertAgent(context.Background(), agent1)

	// Create unhealthy agent
	agent2 := db.Agent{
		ID:         "unhealthy-agent",
		Hostname:   "web-unhealthy",
		RemoteIP:   "10.0.0.2",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	store.UpsertAgent(context.Background(), agent2)
	store.InsertModuleResult(context.Background(), db.ModuleResult{
		AgentID:    "unhealthy-agent",
		ModuleName: "module-a",
		Status:     "error",
		Stdout:     "",
		Stderr:     "error",
		ExecutedAt: now,
	})

	// Create stale agent
	oldTime := now - int64(2*s.pollInterval) - 100
	agent3 := db.Agent{
		ID:         "stale-agent",
		Hostname:   "web-stale",
		RemoteIP:   "10.0.0.3",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: oldTime,
	}
	store.UpsertAgent(context.Background(), agent3)

	ts := httptest.NewServer(http.HandlerFunc(s.handleListAgents))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListAgentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Agents) != 3 {
		t.Errorf("Expected 3 agents, got %d", len(result.Agents))
		return
	}

	healthStates := make(map[string]string)
	for _, a := range result.Agents {
		healthStates[a.Hostname] = a.Health
	}

	if healthStates["web-healthy"] != "healthy" {
		t.Errorf("Expected web-healthy to be healthy, got %s", healthStates["web-healthy"])
	}
	if healthStates["web-unhealthy"] != "unhealthy" {
		t.Errorf("Expected web-unhealthy to be unhealthy, got %s", healthStates["web-unhealthy"])
	}
	if healthStates["web-stale"] != "stale" {
		t.Errorf("Expected web-stale to be stale, got %s", healthStates["web-stale"])
	}
}
