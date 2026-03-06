package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/andrew-d/anchor/internal/db"
	"github.com/andrew-d/anchor/internal/module"
)

// newTestServer creates a Server with injected store and loader for testing.
// It sets up a real SQLite database in a temporary directory and returns
// the server, store, loader, and cleanup function.
func newTestServer(t *testing.T) (*Server, db.Store, *module.Loader) {
	t.Helper()

	// Create temporary directories for database and modules
	dbDir := t.TempDir()
	modulesDir := t.TempDir()

	// Open database
	dbPath := filepath.Join(dbDir, "test.db")
	store, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	// Create loader
	loader := module.NewLoader(modulesDir)

	// Create server with injected store and loader
	s := &Server{
		port:         0, // Not used in tests
		modulesDir:   modulesDir,
		dataDir:      dbDir,
		store:        store,
		loader:       loader,
		pollInterval: 300,
	}

	// Register cleanup
	t.Cleanup(func() {
		store.Close()
	})

	return s, store, loader
}

// writeTestModule writes a sample module script to a temp directory.
// The script implements the module interface with "metadata" and "apply" commands.
func writeTestModule(t *testing.T, dir, filename, name string) {
	t.Helper()
	script := fmt.Sprintf(`#!/bin/sh
case "$1" in
    metadata)
        echo '{"name": "%s", "description": "test module"}'
        ;;
    apply)
        echo "applying %s"
        exit 0
        ;;
    *)
        echo "unknown command: $1" >&2
        exit 1
        ;;
esac
`, name, name)
	path := filepath.Join(dir, filename)
	err := os.WriteFile(path, []byte(script), 0755)
	if err != nil {
		t.Fatalf("Failed to write test module: %v", err)
	}
}

// TestCheckinNewAgent tests AC1.1: First check-in from a new UUID creates agent record
func TestCheckinNewAgent(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	// Create test HTTP server
	ts := httptest.NewServer(http.HandlerFunc(s.handleCheckin))
	defer ts.Close()

	// Make checkin request
	reqBody := CheckinRequest{
		ID:       "agent-uuid-123",
		Hostname: "web-server-1",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	reqJSON, _ := json.Marshal(reqBody)

	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify agent was created in store
	agent, err := store.GetAgent(t.Context(), "agent-uuid-123")
	if err != nil {
		t.Fatalf("Failed to get agent: %v", err)
	}

	if agent.ID != "agent-uuid-123" {
		t.Errorf("Agent ID mismatch: got %s, want agent-uuid-123", agent.ID)
	}
	if agent.Hostname != "web-server-1" {
		t.Errorf("Hostname mismatch: got %s, want web-server-1", agent.Hostname)
	}
	if agent.OS != "linux" {
		t.Errorf("OS mismatch: got %s, want linux", agent.OS)
	}
	if agent.Arch != "amd64" {
		t.Errorf("Arch mismatch: got %s, want amd64", agent.Arch)
	}
	if agent.Distro != "ubuntu-22.04" {
		t.Errorf("Distro mismatch: got %s, want ubuntu-22.04", agent.Distro)
	}
	if agent.RemoteIP != "127.0.0.1" {
		t.Errorf("RemoteIP mismatch: got %s, want 127.0.0.1", agent.RemoteIP)
	}
	if agent.LastSeenAt == 0 {
		t.Errorf("LastSeenAt should be set")
	}
}

// TestCheckinUpdateAgent tests AC1.2: Subsequent check-ins update system info and last_seen_at
func TestCheckinUpdateAgent(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, store, _ := newTestServer(t)

		// First check-in
		reqBody1 := CheckinRequest{
			ID:       "agent-uuid-456",
			Hostname: "web-server-1",
			OS:       "linux",
			Arch:     "amd64",
			Distro:   "ubuntu-22.04",
		}
		reqJSON1, _ := json.Marshal(reqBody1)
		req1 := httptest.NewRequest("POST", "/api/checkin", bytes.NewReader(reqJSON1))
		req1.Header.Set("Content-Type", "application/json")
		rec1 := httptest.NewRecorder()
		s.handleCheckin(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("First checkin failed: status %d", rec1.Code)
		}

		agent1, _ := store.GetAgent(t.Context(), "agent-uuid-456")
		lastSeenAt1 := agent1.LastSeenAt

		// Advance fake clock past the Unix-second boundary
		time.Sleep(1 * time.Second)

		// Second check-in with same UUID
		reqJSON2, _ := json.Marshal(reqBody1)
		req2 := httptest.NewRequest("POST", "/api/checkin", bytes.NewReader(reqJSON2))
		req2.Header.Set("Content-Type", "application/json")
		rec2 := httptest.NewRecorder()
		s.handleCheckin(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("Second checkin failed: status %d", rec2.Code)
		}

		agent2, _ := store.GetAgent(t.Context(), "agent-uuid-456")

		if agent2.LastSeenAt <= lastSeenAt1 {
			t.Errorf("LastSeenAt should be updated: old=%d, new=%d", lastSeenAt1, agent2.LastSeenAt)
		}
	})
}

// TestCheckinChangedHostname tests AC1.3: Agent with changed hostname updates correctly
func TestCheckinChangedHostname(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	ts := httptest.NewServer(http.HandlerFunc(s.handleCheckin))
	defer ts.Close()

	// First check-in with hostname1
	reqBody1 := CheckinRequest{
		ID:       "agent-uuid-789",
		Hostname: "web-server-old",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	reqJSON1, _ := json.Marshal(reqBody1)
	resp1, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON1))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp1.Body.Close()

	agent1, _ := store.GetAgent(t.Context(), "agent-uuid-789")
	if agent1.Hostname != "web-server-old" {
		t.Errorf("First hostname should be web-server-old, got %s", agent1.Hostname)
	}

	// Second check-in with changed hostname
	reqBody2 := CheckinRequest{
		ID:       "agent-uuid-789",
		Hostname: "web-server-new",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	reqJSON2, _ := json.Marshal(reqBody2)
	resp2, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON2))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	resp2.Body.Close()

	agent2, _ := store.GetAgent(t.Context(), "agent-uuid-789")
	if agent2.Hostname != "web-server-new" {
		t.Errorf("Updated hostname should be web-server-new, got %s", agent2.Hostname)
	}
}

// TestCheckinWithModules tests AC2.1: Checkin response contains full script content for assigned modules
func TestCheckinWithModules(t *testing.T) {
	t.Parallel()
	s, store, loader := newTestServer(t)

	// Create a test module
	modulesDir := s.modulesDir
	writeTestModule(t, modulesDir, "test_module", "test_module")

	// Load modules
	if _, err := loader.LoadAll(t.Context()); err != nil {
		t.Fatalf("Failed to load modules: %v", err)
	}

	// First create the agent in the database
	agent := db.Agent{
		ID:       "agent-uuid-mod1",
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	if err := store.UpsertAgent(t.Context(), agent); err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Assign module to agent
	_, err := store.AssignModule(t.Context(), "test_module", new("agent-uuid-mod1"), nil)
	if err != nil {
		t.Fatalf("Failed to assign module: %v", err)
	}

	// Make checkin request
	ts := httptest.NewServer(http.HandlerFunc(s.handleCheckin))
	defer ts.Close()

	reqBody := CheckinRequest{
		ID:       "agent-uuid-mod1",
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	reqJSON, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Decode response
	var respBody CheckinResponse
	json.NewDecoder(resp.Body).Decode(&respBody)

	if len(respBody.Modules) != 1 {
		t.Errorf("Expected 1 module, got %d", len(respBody.Modules))
	}
	if respBody.Modules[0].Name != "test_module" {
		t.Errorf("Module name mismatch: got %s, want test_module", respBody.Modules[0].Name)
	}
	if respBody.Modules[0].Script == "" {
		t.Errorf("Module script should not be empty")
	}
}

// TestReport tests AC2.4: Agent reports each module result via POST /api/report
func TestReport(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	// First create the agent in the database
	agent := db.Agent{
		ID:       "agent-uuid-report1",
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	if err := store.UpsertAgent(t.Context(), agent); err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()

	reqBody := ReportRequest{
		AgentID:    "agent-uuid-report1",
		ModuleName: "test_module",
		Status:     "ok",
		Stdout:     "test output",
		Stderr:     "",
		ExecutedAt: time.Now().Unix(),
	}
	reqJSON, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	// Verify result was stored
	results, err := store.GetLatestModuleResults(t.Context(), "agent-uuid-report1")
	if err != nil {
		t.Fatalf("Failed to get results: %v", err)
	}
	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}
	if results[0].Status != "ok" {
		t.Errorf("Status mismatch: got %s, want ok", results[0].Status)
	}
}

// TestReportMultipleResults tests AC5.1: Every module execution inserts a new row (no upsert)
func TestReportMultipleResults(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, store, _ := newTestServer(t)

		// First create the agent in the database
		agent := db.Agent{
			ID:       "agent-uuid-multi",
			Hostname: "test-host",
			OS:       "linux",
			Arch:     "amd64",
			Distro:   "ubuntu-22.04",
		}
		if err := store.UpsertAgent(t.Context(), agent); err != nil {
			t.Fatalf("Failed to create agent: %v", err)
		}

		// Report same module twice
		reqBody1 := ReportRequest{
			AgentID:    "agent-uuid-multi",
			ModuleName: "test_module",
			Status:     "ok",
			Stdout:     "first run",
			Stderr:     "",
			ExecutedAt: time.Now().Unix(),
		}
		reqJSON1, _ := json.Marshal(reqBody1)
		req1 := httptest.NewRequest("POST", "/api/report", bytes.NewReader(reqJSON1))
		req1.Header.Set("Content-Type", "application/json")
		rec1 := httptest.NewRecorder()
		s.handleReport(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Fatalf("First report failed: status %d", rec1.Code)
		}

		time.Sleep(10 * time.Millisecond)

		reqBody2 := ReportRequest{
			AgentID:    "agent-uuid-multi",
			ModuleName: "test_module",
			Status:     "changed",
			Stdout:     "second run",
			Stderr:     "",
			ExecutedAt: time.Now().Unix(),
		}
		reqJSON2, _ := json.Marshal(reqBody2)
		req2 := httptest.NewRequest("POST", "/api/report", bytes.NewReader(reqJSON2))
		req2.Header.Set("Content-Type", "application/json")
		rec2 := httptest.NewRecorder()
		s.handleReport(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Fatalf("Second report failed: status %d", rec2.Code)
		}

		// Verify both results exist
		history, _ := store.GetModuleHistory(t.Context(), "agent-uuid-multi", "test_module")
		if len(history) != 2 {
			t.Errorf("Expected 2 results, got %d", len(history))
		}
	})
}

// TestCheckinWithDirectAndTagModules tests AC3.4: Agent's effective module set is union of direct + tag-inherited assignments
func TestCheckinWithDirectAndTagModules(t *testing.T) {
	t.Parallel()
	s, store, loader := newTestServer(t)

	// Create two test modules
	modulesDir := s.modulesDir
	writeTestModule(t, modulesDir, "modA", "modA")
	writeTestModule(t, modulesDir, "modB", "modB")

	// Load modules
	if _, err := loader.LoadAll(t.Context()); err != nil {
		t.Fatalf("Failed to load modules: %v", err)
	}

	// First create the agent in the database
	agent := db.Agent{
		ID:       "agent-uuid-tag",
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	if err := store.UpsertAgent(t.Context(), agent); err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Create a tag and assign agent to it
	tag, err := store.CreateTag(t.Context(), "test-tag")
	if err != nil {
		t.Fatalf("Failed to create tag: %v", err)
	}

	if err := store.SetAgentTags(t.Context(), "agent-uuid-tag", []int64{tag.ID}); err != nil {
		t.Fatalf("Failed to set agent tags: %v", err)
	}

	// Assign modA directly, modB to tag
	_, err = store.AssignModule(t.Context(), "modA", new("agent-uuid-tag"), nil)
	if err != nil {
		t.Fatalf("Failed to assign modA: %v", err)
	}
	_, err = store.AssignModule(t.Context(), "modB", nil, &tag.ID)
	if err != nil {
		t.Fatalf("Failed to assign modB: %v", err)
	}

	// Make checkin request
	ts := httptest.NewServer(http.HandlerFunc(s.handleCheckin))
	defer ts.Close()

	reqBody := CheckinRequest{
		ID:       "agent-uuid-tag",
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	reqJSON, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	var respBody CheckinResponse
	json.NewDecoder(resp.Body).Decode(&respBody)

	if len(respBody.Modules) != 2 {
		t.Errorf("Expected 2 modules, got %d", len(respBody.Modules))
	}

	moduleNames := map[string]bool{}
	for _, mod := range respBody.Modules {
		moduleNames[mod.Name] = true
	}
	if !moduleNames["modA"] {
		t.Errorf("modA should be in modules")
	}
	if !moduleNames["modB"] {
		t.Errorf("modB should be in modules")
	}
}

// TestCheckinDeduplicateTagModules tests AC3.5: Module assigned to two tags both on same agent appears only once
func TestCheckinDeduplicateTagModules(t *testing.T) {
	t.Parallel()
	s, store, loader := newTestServer(t)

	// Create test module
	modulesDir := s.modulesDir
	writeTestModule(t, modulesDir, "sharedMod", "sharedMod")

	// Load modules
	if _, err := loader.LoadAll(t.Context()); err != nil {
		t.Fatalf("Failed to load modules: %v", err)
	}

	// First create the agent in the database
	agent := db.Agent{
		ID:       "agent-uuid-dedup",
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	if err := store.UpsertAgent(t.Context(), agent); err != nil {
		t.Fatalf("Failed to create agent: %v", err)
	}

	// Create two tags
	tag1, err := store.CreateTag(t.Context(), "tag1")
	if err != nil {
		t.Fatalf("Failed to create tag1: %v", err)
	}
	tag2, err := store.CreateTag(t.Context(), "tag2")
	if err != nil {
		t.Fatalf("Failed to create tag2: %v", err)
	}

	// Assign agent to both tags
	if err := store.SetAgentTags(t.Context(), "agent-uuid-dedup", []int64{tag1.ID, tag2.ID}); err != nil {
		t.Fatalf("Failed to set agent tags: %v", err)
	}

	// Assign same module to both tags
	_, err = store.AssignModule(t.Context(), "sharedMod", nil, &tag1.ID)
	if err != nil {
		t.Fatalf("Failed to assign sharedMod to tag1: %v", err)
	}
	_, err = store.AssignModule(t.Context(), "sharedMod", nil, &tag2.ID)
	if err != nil {
		t.Fatalf("Failed to assign sharedMod to tag2: %v", err)
	}

	// Make checkin request
	ts := httptest.NewServer(http.HandlerFunc(s.handleCheckin))
	defer ts.Close()

	reqBody := CheckinRequest{
		ID:       "agent-uuid-dedup",
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	reqJSON, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	var respBody CheckinResponse
	json.NewDecoder(resp.Body).Decode(&respBody)

	if len(respBody.Modules) != 1 {
		t.Errorf("Expected 1 module (deduplicated), got %d", len(respBody.Modules))
	}
	if len(respBody.Modules) > 0 && respBody.Modules[0].Name != "sharedMod" {
		t.Errorf("Module name mismatch: got %s, want sharedMod", respBody.Modules[0].Name)
	}
}

// Error cases

// TestCheckinEmptyBody tests that POST checkin with empty body returns 400
func TestCheckinEmptyBody(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	ts := httptest.NewServer(http.HandlerFunc(s.handleCheckin))
	defer ts.Close()

	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader([]byte("")))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

// TestCheckinMissingID tests that POST checkin with missing ID returns 400
func TestCheckinMissingID(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	ts := httptest.NewServer(http.HandlerFunc(s.handleCheckin))
	defer ts.Close()

	reqBody := CheckinRequest{
		Hostname: "test-host",
		OS:       "linux",
		Arch:     "amd64",
		Distro:   "ubuntu-22.04",
	}
	reqJSON, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

// TestReportInvalidStatus tests that POST report with invalid status returns 400
func TestReportInvalidStatus(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	ts := httptest.NewServer(http.HandlerFunc(s.handleReport))
	defer ts.Close()

	reqBody := ReportRequest{
		AgentID:    "agent-uuid",
		ModuleName: "test_module",
		Status:     "invalid_status",
		Stdout:     "",
		Stderr:     "",
		ExecutedAt: time.Now().Unix(),
	}
	reqJSON, _ := json.Marshal(reqBody)
	resp, err := http.Post(ts.URL, "application/json", bytes.NewReader(reqJSON))
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("Expected status 400, got %d", resp.StatusCode)
	}
}

// TestCheckinMethodNotAllowed tests that GET /api/checkin returns 405 (method not allowed)
func TestCheckinMethodNotAllowed(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	// Create a full server mux with the same route registrations as the real server
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/checkin", s.handleCheckin)
	mux.HandleFunc("POST /api/report", s.handleReport)

	ts := httptest.NewServer(mux)
	defer ts.Close()

	// Send a GET request to /api/checkin
	resp, err := http.Get(ts.URL + "/api/checkin")
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	defer resp.Body.Close()

	// Assert 405 Method Not Allowed
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("Expected status 405, got %d", resp.StatusCode)
	}
}

// TestHealthzOK tests that GET /healthz returns 200 with a healthy database
func TestHealthzOK(t *testing.T) {
	t.Parallel()
	s, _, _ := newTestServer(t)

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	s.handleHealthz(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if body["status"] != "ok" {
		t.Errorf("Expected status ok, got %s", body["status"])
	}

	ct := rec.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Expected Content-Type application/json, got %s", ct)
	}
}

// TestHealthzDBClosed tests that GET /healthz returns 503 when the database is closed
func TestHealthzDBClosed(t *testing.T) {
	t.Parallel()
	s, store, _ := newTestServer(t)

	// Close the store to simulate a failed database
	store.(*db.SQLiteStore).Close()

	req := httptest.NewRequest("GET", "/healthz", nil)
	rec := httptest.NewRecorder()
	s.handleHealthz(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status 503, got %d", rec.Code)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if body["status"] != "error" {
		t.Errorf("Expected status error, got %s", body["status"])
	}
	if body["error"] == "" {
		t.Error("Expected error message to be non-empty")
	}
}
