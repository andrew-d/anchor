package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/andrew-d/anchor/internal/db"
)

// TestListAgentsHealthyStatus tests AC7.3: agent with recent check-in and
// module results with status "ok" or "changed" appears as "healthy".
func TestListAgentsHealthyStatus(t *testing.T) {
	t.Parallel()
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

	agentResp := result.Agents[0]
	if agentResp.Health != "healthy" {
		t.Errorf("Expected health 'healthy', got '%s'", agentResp.Health)
	}
	if agentResp.ModuleCount != 2 {
		t.Errorf("Expected module_count 2, got %d", agentResp.ModuleCount)
	}
	if agentResp.ErrorCount != 0 {
		t.Errorf("Expected error_count 0, got %d", agentResp.ErrorCount)
	}
}

// TestListAgentsUnhealthyStatus tests AC7.1: agent with recent check-in and
// module result with status "error" appears as "unhealthy".
func TestListAgentsUnhealthyStatus(t *testing.T) {
	t.Parallel()
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

	agentResp := result.Agents[0]
	if agentResp.Health != "unhealthy" {
		t.Errorf("Expected health 'unhealthy', got '%s'", agentResp.Health)
	}
	if agentResp.ErrorCount != 1 {
		t.Errorf("Expected error_count 1, got %d", agentResp.ErrorCount)
	}
}

// TestListAgentsStaleStatus tests AC7.2: agent not seen within 2x poll interval
// appears as "stale".
func TestListAgentsStaleStatus(t *testing.T) {
	t.Parallel()
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

	agentResp := result.Agents[0]
	if agentResp.Health != "stale" {
		t.Errorf("Expected health 'stale', got '%s'", agentResp.Health)
	}
}

// TestListAgentsHealthyNoModules tests agent with no module results and
// recent check-in is "healthy".
func TestListAgentsHealthyNoModules(t *testing.T) {
	t.Parallel()
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

	agentResp := result.Agents[0]
	if agentResp.Health != "healthy" {
		t.Errorf("Expected health 'healthy', got '%s'", agentResp.Health)
	}
	if agentResp.ModuleCount != 0 {
		t.Errorf("Expected module_count 0, got %d", agentResp.ModuleCount)
	}
}

// TestGetAgentDetail tests GET /api/agents/{id} returns correct agent detail
// with tags and module results.
func TestGetAgentDetail(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	err := store.UpsertAgent(context.Background(), agent1)
	if err != nil {
		t.Fatalf("Failed to upsert healthy agent: %v", err)
	}

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
	err = store.UpsertAgent(context.Background(), agent2)
	if err != nil {
		t.Fatalf("Failed to upsert unhealthy agent: %v", err)
	}
	err = store.InsertModuleResult(context.Background(), db.ModuleResult{
		AgentID:    "unhealthy-agent",
		ModuleName: "module-a",
		Status:     "error",
		Stdout:     "",
		Stderr:     "error",
		ExecutedAt: now,
	})
	if err != nil {
		t.Fatalf("Failed to insert module result: %v", err)
	}

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
	err = store.UpsertAgent(context.Background(), agent3)
	if err != nil {
		t.Fatalf("Failed to upsert stale agent: %v", err)
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

// TestCreateTag tests AC3.1: POST /api/tags creates a tag and GET /api/tags lists it.
func TestCreateTag(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/tags", s.handleCreateTag)
	mux.HandleFunc("GET /api/tags", s.handleListTags)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Create a tag
	createReq := CreateTagRequest{Name: "webservers"}
	createReqBody, err := json.Marshal(createReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(ts.URL+"/api/tags", "application/json", bytes.NewReader(createReqBody))
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, resp.StatusCode)
	}

	var tagResp UITagResponse
	err = json.NewDecoder(resp.Body).Decode(&tagResp)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if tagResp.Name != "webservers" {
		t.Errorf("Expected tag name 'webservers', got '%s'", tagResp.Name)
	}
	if tagResp.ID == 0 {
		t.Errorf("Expected tag ID > 0, got %d", tagResp.ID)
	}

	// List tags and verify it appears
	resp, err = http.Get(ts.URL + "/api/tags")
	if err != nil {
		t.Fatalf("Failed to list tags: %v", err)
	}
	defer resp.Body.Close()

	var listResp ListTagsResponse
	err = json.NewDecoder(resp.Body).Decode(&listResp)
	if err != nil {
		t.Fatalf("Failed to decode list response: %v", err)
	}

	if len(listResp.Tags) != 1 {
		t.Errorf("Expected 1 tag, got %d", len(listResp.Tags))
	} else if listResp.Tags[0].Name != "webservers" {
		t.Errorf("Expected tag name 'webservers', got '%s'", listResp.Tags[0].Name)
	}
}

// TestDeleteTag tests DELETE /api/tags/{id} removes a tag.
func TestDeleteTag(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	tag, err := store.CreateTag(ctx, "test-tag")
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/tags/{id}", s.handleDeleteTag)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	req, err := http.NewRequest("DELETE", ts.URL+"/api/tags/"+strconv.FormatInt(tag.ID, 10), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	var result OKResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if !result.OK {
		t.Errorf("Expected ok true, got %v", result.OK)
	}
}

// TestSetAgentTags tests PUT /api/agents/{id}/tags assigns tags to an agent.
func TestSetAgentTags(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()

	// Create agent
	agent := db.Agent{
		ID:         "agent-tags-1",
		Hostname:   "test-agent",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	// Create tag
	tag, err := store.CreateTag(ctx, "webservers")
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/agents/{id}/tags", s.handleSetAgentTags)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Set agent tags
	setReq := SetAgentTagsRequest{TagIDs: []int64{tag.ID}}
	setReqBody, err := json.Marshal(setReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	req, err := http.NewRequest("PUT", ts.URL+"/api/agents/agent-tags-1/tags", bytes.NewReader(setReqBody))
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	// Verify tags were assigned
	tags, err := store.GetAgentTags(ctx, "agent-tags-1")
	if err != nil {
		t.Fatalf("Failed to get agent tags: %v", err)
	}

	if len(tags) != 1 {
		t.Errorf("Expected 1 tag, got %d", len(tags))
	} else if tags[0].Name != "webservers" {
		t.Errorf("Expected tag name 'webservers', got '%s'", tags[0].Name)
	}
}

// TestListAssignments tests GET /api/assignments lists all module assignments.
func TestListAssignments(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()

	// Create agent and assign module
	agent := db.Agent{
		ID:         "agent-assign-1",
		Hostname:   "test-agent",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	agentID := "agent-assign-1"
	_, err = store.AssignModule(ctx, "00_base", &agentID, nil)
	if err != nil {
		t.Fatalf("Failed to assign module: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/assignments", s.handleListAssignments)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/assignments")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListAssignmentsResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Assignments) != 1 {
		t.Errorf("Expected 1 assignment, got %d", len(result.Assignments))
	} else {
		assign := result.Assignments[0]
		if assign.ModuleName != "00_base" {
			t.Errorf("Expected module name '00_base', got '%s'", assign.ModuleName)
		}
		if assign.AgentID == nil || *assign.AgentID != agentID {
			t.Errorf("Expected agent ID '%s', got %v", agentID, assign.AgentID)
		}
		if assign.TagID != nil {
			t.Errorf("Expected tag_id nil, got %v", assign.TagID)
		}
	}
}

// TestCreateAssignmentWithAgentID tests POST /api/assignments creates direct agent assignment (AC3.2).
func TestCreateAssignmentWithAgentID(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()

	// Create agent
	agent := db.Agent{
		ID:         "agent-direct-1",
		Hostname:   "test-agent",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/assignments", s.handleCreateAssignment)
	mux.HandleFunc("GET /api/agents/{id}/modules", s.handleGetAgentModules)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Create assignment
	agentID := "agent-direct-1"
	createReq := CreateAssignmentRequest{
		ModuleName: "00_base",
		AgentID:    &agentID,
		TagID:      nil,
	}
	createReqBody, err := json.Marshal(createReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(ts.URL+"/api/assignments", "application/json", bytes.NewReader(createReqBody))
	if err != nil {
		t.Fatalf("Failed to create assignment: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, resp.StatusCode)
	}

	var assignResp ModuleAssignmentResponse
	err = json.NewDecoder(resp.Body).Decode(&assignResp)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if assignResp.ModuleName != "00_base" {
		t.Errorf("Expected module name '00_base', got '%s'", assignResp.ModuleName)
	}

	// Verify in effective modules
	resp, err = http.Get(ts.URL + "/api/agents/agent-direct-1/modules")
	if err != nil {
		t.Fatalf("Failed to get effective modules: %v", err)
	}
	defer resp.Body.Close()

	var effectiveResp AgentEffectiveModulesResponse
	err = json.NewDecoder(resp.Body).Decode(&effectiveResp)
	if err != nil {
		t.Fatalf("Failed to decode effective modules: %v", err)
	}

	if len(effectiveResp.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(effectiveResp.Modules))
	} else {
		mod := effectiveResp.Modules[0]
		if mod.Name != "00_base" {
			t.Errorf("Expected module name '00_base', got '%s'", mod.Name)
		}
		if mod.Source != "direct" {
			t.Errorf("Expected source 'direct', got '%s'", mod.Source)
		}
	}
}

// TestCreateAssignmentWithTagID tests POST /api/assignments creates tag assignment (AC3.3).
func TestCreateAssignmentWithTagID(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()

	// Create tag
	tag, err := store.CreateTag(ctx, "webservers")
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	// Create agent and assign tag
	agent := db.Agent{
		ID:         "agent-tag-assign-1",
		Hostname:   "test-agent",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err = store.UpsertAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	err = store.SetAgentTags(ctx, "agent-tag-assign-1", []int64{tag.ID})
	if err != nil {
		t.Fatalf("Failed to set agent tags: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/assignments", s.handleCreateAssignment)
	mux.HandleFunc("GET /api/agents/{id}/modules", s.handleGetAgentModules)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Create assignment to tag
	createReq := CreateAssignmentRequest{
		ModuleName: "10_users",
		AgentID:    nil,
		TagID:      &tag.ID,
	}
	createReqBody, err := json.Marshal(createReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(ts.URL+"/api/assignments", "application/json", bytes.NewReader(createReqBody))
	if err != nil {
		t.Fatalf("Failed to create assignment: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Expected status %d, got %d", http.StatusCreated, resp.StatusCode)
	}

	// Verify in effective modules with tag source
	resp, err = http.Get(ts.URL + "/api/agents/agent-tag-assign-1/modules")
	if err != nil {
		t.Fatalf("Failed to get effective modules: %v", err)
	}
	defer resp.Body.Close()

	var effectiveResp AgentEffectiveModulesResponse
	err = json.NewDecoder(resp.Body).Decode(&effectiveResp)
	if err != nil {
		t.Fatalf("Failed to decode effective modules: %v", err)
	}

	if len(effectiveResp.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(effectiveResp.Modules))
	} else {
		mod := effectiveResp.Modules[0]
		if mod.Name != "10_users" {
			t.Errorf("Expected module name '10_users', got '%s'", mod.Name)
		}
		if mod.Source != "tag:webservers" {
			t.Errorf("Expected source 'tag:webservers', got '%s'", mod.Source)
		}
	}
}

// TestCreateAssignmentBothAgentAndTag tests POST /api/assignments with both agent_id and tag_id returns 400.
func TestCreateAssignmentBothAgentAndTag(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/assignments", s.handleCreateAssignment)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	agentID := "agent-1"
	tagID := int64(1)
	createReq := CreateAssignmentRequest{
		ModuleName: "00_base",
		AgentID:    &agentID,
		TagID:      &tagID,
	}
	createReqBody, err := json.Marshal(createReq)
	if err != nil {
		t.Fatalf("Failed to marshal request: %v", err)
	}

	resp, err := http.Post(ts.URL+"/api/assignments", "application/json", bytes.NewReader(createReqBody))
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status %d, got %d", http.StatusBadRequest, resp.StatusCode)
	}
}

// TestDeleteAssignment tests DELETE /api/assignments/{id} removes an assignment.
func TestDeleteAssignment(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()

	// Create agent and assign module
	agent := db.Agent{
		ID:         "agent-delete-1",
		Hostname:   "test-agent",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err := store.UpsertAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	agentID := "agent-delete-1"
	_, err = store.AssignModule(ctx, "00_base", &agentID, nil)
	if err != nil {
		t.Fatalf("Failed to assign module: %v", err)
	}

	assignments, err := store.ListAssignments(ctx)
	if err != nil {
		t.Fatalf("Failed to list assignments: %v", err)
	}

	if len(assignments) == 0 {
		t.Fatalf("No assignments created")
	}

	assignmentID := assignments[0].ID

	mux := http.NewServeMux()
	mux.HandleFunc("DELETE /api/assignments/{id}", s.handleDeleteAssignment)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	req, err := http.NewRequest("DELETE", ts.URL+"/api/assignments/"+strconv.FormatInt(assignmentID, 10), nil)
	if err != nil {
		t.Fatalf("Failed to create request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, resp.StatusCode)
	}
}

// TestListModules tests GET /api/modules returns loaded modules with metadata (AC9.5).
func TestListModules(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	// Create a test module
	moduleScript := `#!/bin/sh
case "$1" in
    metadata) echo '{"name": "Base Config", "description": "Installs base packages"}' ;;
    apply) echo "applied"; exit 0 ;;
esac
`
	modulePath := filepath.Join(s.modulesDir, "00_base")
	err := os.WriteFile(modulePath, []byte(moduleScript), 0755)
	if err != nil {
		t.Fatalf("Failed to write module: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/modules", s.handleListModules)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/modules")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListModulesResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(result.Modules))
	} else {
		mod := result.Modules[0]
		if mod.Name != "Base Config" {
			t.Errorf("Expected name 'Base Config', got '%s'", mod.Name)
		}
		if mod.Description != "Installs base packages" {
			t.Errorf("Expected description 'Installs base packages', got '%s'", mod.Description)
		}
		if mod.Filename != "00_base" {
			t.Errorf("Expected filename '00_base', got '%s'", mod.Filename)
		}
	}
}

// TestEffectiveModulesWithMixed tests AC9.4: effective module set shows both direct and tag-based modules.
func TestEffectiveModulesWithMixed(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()

	// Create tag
	tag, err := store.CreateTag(ctx, "webservers")
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	// Create agent and assign tag
	agent := db.Agent{
		ID:         "agent-mixed-1",
		Hostname:   "test-agent",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	err = store.UpsertAgent(ctx, agent)
	if err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	err = store.SetAgentTags(ctx, "agent-mixed-1", []int64{tag.ID})
	if err != nil {
		t.Fatalf("Failed to set agent tags: %v", err)
	}

	// Assign mod_a directly
	agentID := "agent-mixed-1"
	_, err = store.AssignModule(ctx, "mod_a", &agentID, nil)
	if err != nil {
		t.Fatalf("Failed to assign module directly: %v", err)
	}

	// Assign mod_b to tag
	_, err = store.AssignModule(ctx, "mod_b", nil, &tag.ID)
	if err != nil {
		t.Fatalf("Failed to assign module to tag: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/agents/{id}/modules", s.handleGetAgentModules)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/agents/agent-mixed-1/modules")
	if err != nil {
		t.Fatalf("Failed to get effective modules: %v", err)
	}
	defer resp.Body.Close()

	var result AgentEffectiveModulesResponse
	err = json.NewDecoder(resp.Body).Decode(&result)
	if err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Modules) != 2 {
		t.Errorf("Expected 2 modules, got %d", len(result.Modules))
		return
	}

	// Find each module and check source
	sourceMap := make(map[string]string)
	for _, mod := range result.Modules {
		sourceMap[mod.Name] = mod.Source
	}

	if source, ok := sourceMap["mod_a"]; !ok || source != "direct" {
		t.Errorf("Expected mod_a with source 'direct', got source '%s'", source)
	}

	if source, ok := sourceMap["mod_b"]; !ok || source != "tag:webservers" {
		t.Errorf("Expected mod_b with source 'tag:webservers', got source '%s'", source)
	}
}

// TestSetAgentDisplayName tests PUT /api/agents/{id}/name sets the display name.
func TestSetAgentDisplayName(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-name-1",
		Hostname:   "web-01",
		RemoteIP:   "10.0.0.1",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/agents/{id}/name", s.handleSetAgentDisplayName)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	name := "Production Web Server"
	reqBody, _ := json.Marshal(SetDisplayNameRequest{DisplayName: &name})
	req, _ := http.NewRequest("PUT", ts.URL+"/api/agents/agent-name-1/name", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify stored
	retrieved, err := store.GetAgent(ctx, "agent-name-1")
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if retrieved.DisplayName == nil || *retrieved.DisplayName != "Production Web Server" {
		t.Errorf("Expected display name 'Production Web Server', got %v", retrieved.DisplayName)
	}
}

// TestSetAgentDisplayName_Empty tests PUT /api/agents/{id}/name with empty string returns 400.
func TestSetAgentDisplayName_Empty(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-name-2",
		Hostname:   "web-02",
		RemoteIP:   "10.0.0.2",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/agents/{id}/name", s.handleSetAgentDisplayName)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	empty := ""
	reqBody, _ := json.Marshal(SetDisplayNameRequest{DisplayName: &empty})
	req, _ := http.NewRequest("PUT", ts.URL+"/api/agents/agent-name-2/name", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

// TestSetAgentDisplayName_Null tests PUT /api/agents/{id}/name with null resets to nil.
func TestSetAgentDisplayName_Null(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-name-3",
		Hostname:   "web-03",
		RemoteIP:   "10.0.0.3",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	// Set a name first
	name := "My Server"
	if err := store.SetAgentDisplayName(ctx, "agent-name-3", &name); err != nil {
		t.Fatalf("Failed to set display name: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("PUT /api/agents/{id}/name", s.handleSetAgentDisplayName)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	reqBody, _ := json.Marshal(SetDisplayNameRequest{DisplayName: nil})
	req, _ := http.NewRequest("PUT", ts.URL+"/api/agents/agent-name-3/name", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify reset
	retrieved, err := store.GetAgent(ctx, "agent-name-3")
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}
	if retrieved.DisplayName != nil {
		t.Errorf("Expected nil display name, got %v", *retrieved.DisplayName)
	}
}

// TestListModulesIncludesErrors tests that GET /api/modules returns both valid
// and errored modules, with the error field set on broken ones.
func TestListModulesIncludesErrors(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	// Create a valid module
	validScript := `#!/bin/sh
case "$1" in
    metadata) echo '{"name": "Base Config", "description": "Installs base packages"}' ;;
    apply) echo "applied"; exit 0 ;;
esac
`
	if err := os.WriteFile(filepath.Join(s.modulesDir, "00_base"), []byte(validScript), 0755); err != nil {
		t.Fatalf("Failed to write valid module: %v", err)
	}

	// Create a broken module (invalid JSON output)
	brokenScript := "#!/bin/sh\ncase \"$1\" in\n    metadata) echo 'not json' ;;\nesac\n"
	if err := os.WriteFile(filepath.Join(s.modulesDir, "99_broken"), []byte(brokenScript), 0755); err != nil {
		t.Fatalf("Failed to write broken module: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/modules", s.handleListModules)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/modules")
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListModulesResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Modules) != 2 {
		t.Fatalf("Expected 2 modules (1 valid + 1 error), got %d", len(result.Modules))
	}

	// Should be sorted by filename: 00_base, 99_broken
	if result.Modules[0].Filename != "00_base" {
		t.Errorf("Expected first module '00_base', got '%s'", result.Modules[0].Filename)
	}
	if result.Modules[0].Error != "" {
		t.Errorf("Expected no error for valid module, got '%s'", result.Modules[0].Error)
	}
	if result.Modules[0].Name != "Base Config" {
		t.Errorf("Expected name 'Base Config', got '%s'", result.Modules[0].Name)
	}

	if result.Modules[1].Filename != "99_broken" {
		t.Errorf("Expected second module '99_broken', got '%s'", result.Modules[1].Filename)
	}
	if result.Modules[1].Error == "" {
		t.Error("Expected error field set for broken module")
	}
}

// TestListAgentsIncludesDisplayName tests that GET /api/agents includes display_name.
func TestListAgentsIncludesDisplayName(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ctx := context.Background()
	now := time.Now().Unix()
	agent := db.Agent{
		ID:         "agent-name-4",
		Hostname:   "web-04",
		RemoteIP:   "10.0.0.4",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-24.04",
		LastSeenAt: now,
	}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("Failed to upsert agent: %v", err)
	}

	name := "Custom Name"
	if err := store.SetAgentDisplayName(ctx, "agent-name-4", &name); err != nil {
		t.Fatalf("Failed to set display name: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(s.handleListAgents))
	defer ts.Close()

	resp, err := http.Get(ts.URL)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	var result ListAgentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if len(result.Agents) != 1 {
		t.Fatalf("Expected 1 agent, got %d", len(result.Agents))
	}

	agentResp := result.Agents[0]
	if agentResp.DisplayName == nil || *agentResp.DisplayName != "Custom Name" {
		t.Errorf("Expected display_name 'Custom Name', got %v", agentResp.DisplayName)
	}
}
