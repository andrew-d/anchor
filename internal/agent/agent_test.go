package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"testing"
	"time"
)

// TestReadOrCreateUUID_GeneratesNewUUID verifies that a new UUID is generated and persisted
// when the agent-id file doesn't exist (AC6.1).
func TestReadOrCreateUUID_GeneratesNewUUID(t *testing.T) {
	tmpDir := t.TempDir()
	uuid, err := readOrCreateUUID(tmpDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify UUID is non-empty
	if uuid == "" {
		t.Fatal("UUID is empty")
	}

	// Verify UUID format is v4 (8-4-4-4-12 hex with version/variant bits)
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRegex.MatchString(uuid) {
		t.Fatalf("UUID does not match v4 format: %s", uuid)
	}

	// Verify the file was created
	filePath := filepath.Join(tmpDir, "agent-id")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("agent-id file was not created at %s", filePath)
	}

	// Verify the file contains the same UUID
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != uuid {
		t.Fatalf("file content does not match returned UUID: got %s, expected %s", string(content), uuid)
	}
}

// TestReadOrCreateUUID_ReadsExistingUUID verifies that an existing UUID is read from file
// and not overwritten (AC6.2).
func TestReadOrCreateUUID_ReadsExistingUUID(t *testing.T) {
	tmpDir := t.TempDir()
	knownUUID := "12345678-1234-4234-8234-123456789012"

	// Write a known UUID to the file
	filePath := filepath.Join(tmpDir, "agent-id")
	if err := os.WriteFile(filePath, []byte(knownUUID), 0644); err != nil {
		t.Fatalf("failed to write test UUID: %v", err)
	}

	// Call readOrCreateUUID
	uuid, err := readOrCreateUUID(tmpDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify it returned the existing UUID
	if uuid != knownUUID {
		t.Fatalf("readOrCreateUUID did not return existing UUID: got %s, expected %s", uuid, knownUUID)
	}

	// Verify the file was not modified
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != knownUUID {
		t.Fatalf("file was modified: got %s, expected %s", string(content), knownUUID)
	}
}

// TestReadOrCreateUUID_CreatesParentDir verifies that parent directories are created if they don't exist.
func TestReadOrCreateUUID_CreatesParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "path", "to", "datadir")

	uuid, err := readOrCreateUUID(nestedDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify UUID is non-empty
	if uuid == "" {
		t.Fatal("UUID is empty")
	}

	// Verify the directory was created
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Fatalf("parent directories were not created at %s", nestedDir)
	}

	// Verify the file exists and contains the UUID
	filePath := filepath.Join(nestedDir, "agent-id")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != uuid {
		t.Fatalf("file content does not match returned UUID: got %s, expected %s", string(content), uuid)
	}
}

// TestAgentPollingLoop_CheckinServerError verifies that the agent retries gracefully
// when the server returns a non-200 status on checkin, without attempting to JSON-decode
// the error body.
func TestAgentPollingLoop_CheckinServerError(t *testing.T) {
	dataDir := t.TempDir()

	var checkinCount int
	var mu sync.Mutex
	retryChan := make(chan struct{}, 3)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/checkin" && r.Method == "POST" {
			mu.Lock()
			checkinCount++
			count := checkinCount
			mu.Unlock()

			retryChan <- struct{}{}

			if count <= 2 {
				// First two checkins fail with plain text error (not JSON)
				http.Error(w, "database is locked", http.StatusInternalServerError)
				return
			}
			// Third checkin succeeds with no modules
			resp := CheckinResponse{
				PollIntervalSeconds: 300,
				Modules:             []CheckinModule{},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	agent := New(server.URL, dataDir)
	agent.retryDelay = 10 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error)
	go func() {
		done <- agent.Run(ctx)
	}()

	// Wait for the third checkin (2 failures + 1 success)
	for i := 0; i < 3; i++ {
		select {
		case <-retryChan:
		case <-time.After(15 * time.Second):
			t.Fatalf("timeout waiting for checkin attempt %d", i+1)
		}
	}
	cancel()
	<-done

	mu.Lock()
	finalCount := checkinCount
	mu.Unlock()

	if finalCount < 3 {
		t.Fatalf("expected at least 3 checkin attempts, got %d", finalCount)
	}
}

// TestAgentPollingLoop_ReportsModulesIndividually verifies that the agent checks in,
// receives modules, executes them in sorted order, and reports each result individually (AC2.4).
func TestAgentPollingLoop_ReportsModulesIndividually(t *testing.T) {
	// Set up test data directory
	dataDir := t.TempDir()

	// Track requests to the server
	var checkinCount int
	var reportCount int
	var reportedModules []string
	var reportsMutex sync.Mutex
	cancelChan := make(chan struct{})

	// Create a test HTTP server that mimics the anchor server API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/checkin" && r.Method == "POST" {
			checkinCount++
			// Return 2 modules with a short poll interval
			resp := CheckinResponse{
				PollIntervalSeconds: 10, // Long interval so we don't get multiple polls
				Modules: []CheckinModule{
					{Name: "02_pkg", Script: "#!/bin/sh\necho 'install packages'\nexit 0"},
					{Name: "01_base", Script: "#!/bin/sh\necho 'configure base'\nexit 0"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else if r.URL.Path == "/api/report" && r.Method == "POST" {
			reportsMutex.Lock()
			reportCount++
			var req ReportRequest
			json.NewDecoder(r.Body).Decode(&req)
			reportedModules = append(reportedModules, req.ModuleName)
			// After 2 reports, signal to cancel the context
			if reportCount == 2 {
				close(cancelChan)
			}
			reportsMutex.Unlock()

			resp := ReportResponse{OK: true}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Create agent and run it with a context that we'll cancel after both modules are reported
	agent := New(server.URL, dataDir)
	ctx, cancel := context.WithCancel(context.Background())

	// Run in a goroutine so we can cancel after receiving the signal
	done := make(chan error)
	go func() {
		done <- agent.Run(ctx)
	}()

	// Wait for both reports to be received
	select {
	case <-cancelChan:
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for both reports")
		cancel()
	}

	// Wait for agent to finish
	<-done

	// Verify checkin was called
	if checkinCount == 0 {
		t.Fatal("checkin was never called")
	}

	// Verify both modules were reported
	if reportCount != 2 {
		t.Fatalf("expected 2 reports, got %d", reportCount)
	}

	// Verify modules were reported in the order they were returned (but they should have been sorted)
	// The server returns [02_pkg, 01_base], but after sorting should be [01_base, 02_pkg]
	expectedOrder := []string{"01_base", "02_pkg"}
	for i, expectedModule := range expectedOrder {
		if i >= len(reportedModules) {
			t.Fatalf("not enough modules reported: expected %v, got %v", expectedOrder, reportedModules)
		}
		if reportedModules[i] != expectedModule {
			t.Fatalf("module order mismatch: expected %v at index %d, got %s", expectedOrder, i, reportedModules[i])
		}
	}
}

// TestAgentPollingLoop_StopsOnReportFailure verifies that the agent stops executing
// remaining modules if a report request fails (AC2.6).
func TestAgentPollingLoop_StopsOnReportFailure(t *testing.T) {
	// Set up test data directory
	dataDir := t.TempDir()

	// Track requests to the server
	var checkinCount int
	var reportCount int
	var reportedModules []string
	var reportsMutex sync.Mutex
	failChan := make(chan struct{})

	// Create a test HTTP server that fails on the second report
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/checkin" && r.Method == "POST" {
			checkinCount++
			// Return 3 modules
			resp := CheckinResponse{
				PollIntervalSeconds: 10, // Long interval so we don't get multiple polls
				Modules: []CheckinModule{
					{Name: "01_first", Script: "#!/bin/sh\necho 'first'\nexit 0"},
					{Name: "02_second", Script: "#!/bin/sh\necho 'second'\nexit 0"},
					{Name: "03_third", Script: "#!/bin/sh\necho 'third'\nexit 0"},
				},
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else if r.URL.Path == "/api/report" && r.Method == "POST" {
			reportsMutex.Lock()
			reportCount++
			var req ReportRequest
			json.NewDecoder(r.Body).Decode(&req)
			reportedModules = append(reportedModules, req.ModuleName)
			reportsMutex.Unlock()

			// Fail on the second report
			if reportCount == 2 {
				close(failChan)
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}

			resp := ReportResponse{OK: true}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(resp)
		} else {
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	// Create agent and run it with a context that we'll cancel after the failure
	agent := New(server.URL, dataDir)
	ctx, cancel := context.WithCancel(context.Background())

	// Run in a goroutine so we can cancel after the failure
	done := make(chan error)
	go func() {
		done <- agent.Run(ctx)
	}()

	// Wait for the second report to fail
	select {
	case <-failChan:
		// Give the agent time to realize the report failed and stop
		time.Sleep(200 * time.Millisecond)
		cancel()
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for failure")
		cancel()
	}

	// Wait for agent to finish
	<-done

	// Verify only 2 reports were attempted (first succeeded, second failed, so third was not attempted)
	if reportCount != 2 {
		t.Fatalf("expected 2 reports (first succeeded, second failed), got %d", reportCount)
	}

	// Verify only first module was successfully reported (second failed, third not attempted)
	if len(reportedModules) != 2 {
		t.Fatalf("expected 2 modules to have been attempted to report, got %d: %v", len(reportedModules), reportedModules)
	}

	if reportedModules[0] != "01_first" {
		t.Fatalf("expected first module to be '01_first', got %s", reportedModules[0])
	}

	if reportedModules[1] != "02_second" {
		t.Fatalf("expected second module to be '02_second', got %s", reportedModules[1])
	}

	// The key thing is that we never reported the third module
	// If we had, we'd have reportCount >= 3 or reportedModules containing "03_third"
	for _, moduleName := range reportedModules {
		if moduleName == "03_third" {
			t.Fatalf("third module should not have been reported after second report failed")
		}
	}
}
