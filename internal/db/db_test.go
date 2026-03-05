package db

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func TestUpsertAgent_NewAgent(t *testing.T) {
	// AC1.1: First check-in from a new UUID creates an agent record with all fields
	store := newTestStore(t)
	defer store.Close()

	agent := Agent{
		ID:         "agent-123",
		Hostname:   "web-server-1",
		RemoteIP:   "192.168.1.100",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-22.04",
		LastSeenAt: 1000,
	}

	if err := store.UpsertAgent(context.Background(), agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	retrieved, err := store.GetAgent(context.Background(), agent.ID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}

	if retrieved.ID != agent.ID {
		t.Errorf("ID mismatch: got %s, want %s", retrieved.ID, agent.ID)
	}
	if retrieved.Hostname != agent.Hostname {
		t.Errorf("Hostname mismatch: got %s, want %s", retrieved.Hostname, agent.Hostname)
	}
	if retrieved.RemoteIP != agent.RemoteIP {
		t.Errorf("RemoteIP mismatch: got %s, want %s", retrieved.RemoteIP, agent.RemoteIP)
	}
	if retrieved.OS != agent.OS {
		t.Errorf("OS mismatch: got %s, want %s", retrieved.OS, agent.OS)
	}
	if retrieved.Arch != agent.Arch {
		t.Errorf("Arch mismatch: got %s, want %s", retrieved.Arch, agent.Arch)
	}
	if retrieved.Distro != agent.Distro {
		t.Errorf("Distro mismatch: got %s, want %s", retrieved.Distro, agent.Distro)
	}
	if retrieved.LastSeenAt != agent.LastSeenAt {
		t.Errorf("LastSeenAt mismatch: got %d, want %d", retrieved.LastSeenAt, agent.LastSeenAt)
	}
}

func TestUpsertAgent_UpdateExisting(t *testing.T) {
	// AC1.2: Subsequent check-ins update system info and last_seen_at
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	agent := Agent{
		ID:         "agent-123",
		Hostname:   "web-server-1",
		RemoteIP:   "192.168.1.100",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-22.04",
		LastSeenAt: 1000,
	}

	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("First UpsertAgent failed: %v", err)
	}

	// Update the agent with new last_seen_at
	updatedAgent := agent
	updatedAgent.LastSeenAt = 2000

	if err := store.UpsertAgent(ctx, updatedAgent); err != nil {
		t.Fatalf("Second UpsertAgent failed: %v", err)
	}

	retrieved, err := store.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}

	if retrieved.LastSeenAt != 2000 {
		t.Errorf("LastSeenAt not updated: got %d, want 2000", retrieved.LastSeenAt)
	}
}

func TestUpsertAgent_ChangedHostname(t *testing.T) {
	// AC1.3: Agent with changed hostname updates correctly
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	agent := Agent{
		ID:         "agent-123",
		Hostname:   "web-server-1",
		RemoteIP:   "192.168.1.100",
		OS:         "linux",
		Arch:       "amd64",
		Distro:     "ubuntu-22.04",
		LastSeenAt: 1000,
	}

	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("First UpsertAgent failed: %v", err)
	}

	// Same agent, different hostname
	updatedAgent := agent
	updatedAgent.Hostname = "web-server-1-rebuilt"
	updatedAgent.LastSeenAt = 2000

	if err := store.UpsertAgent(ctx, updatedAgent); err != nil {
		t.Fatalf("Second UpsertAgent failed: %v", err)
	}

	retrieved, err := store.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}

	if retrieved.Hostname != "web-server-1-rebuilt" {
		t.Errorf("Hostname not updated: got %s, want web-server-1-rebuilt", retrieved.Hostname)
	}
}

func TestGetAgent_NotFound(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	_, err := store.GetAgent(context.Background(), "nonexistent")
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Expected ErrNotFound, got %v", err)
	}
}

func TestListAgents_Order(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	agents := []Agent{
		{ID: "a1", Hostname: "zulu", LastSeenAt: 1000},
		{ID: "a2", Hostname: "alpha", LastSeenAt: 1000},
		{ID: "a3", Hostname: "bravo", LastSeenAt: 1000},
	}

	for _, agent := range agents {
		if err := store.UpsertAgent(ctx, agent); err != nil {
			t.Fatalf("UpsertAgent failed: %v", err)
		}
	}

	retrieved, err := store.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}

	if len(retrieved) != 3 {
		t.Errorf("Expected 3 agents, got %d", len(retrieved))
	}

	expectedOrder := []string{"alpha", "bravo", "zulu"}
	for i, exp := range expectedOrder {
		if retrieved[i].Hostname != exp {
			t.Errorf("Agent %d: expected hostname %s, got %s", i, exp, retrieved[i].Hostname)
		}
	}
}

func TestCreateTag_Success(t *testing.T) {
	// AC3.1: Tags can be created and assigned to agents
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create a tag
	tag, err := store.CreateTag(ctx, "production")
	if err != nil {
		t.Fatalf("CreateTag failed: %v", err)
	}

	if tag.Name != "production" {
		t.Errorf("Tag name mismatch: got %s, want production", tag.Name)
	}
	if tag.ID == 0 {
		t.Error("Tag ID should be set")
	}

	// Verify it appears in ListTags
	tags, err := store.ListTags(ctx)
	if err != nil {
		t.Fatalf("ListTags failed: %v", err)
	}

	if len(tags) != 1 {
		t.Errorf("Expected 1 tag, got %d", len(tags))
	}
	if tags[0].Name != "production" {
		t.Errorf("Tag name mismatch in list: got %s, want production", tags[0].Name)
	}
}

func TestSetAgentTags_And_GetAgentTags(t *testing.T) {
	// AC3.1 continued: tags can be assigned to agents
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Create agent and tags
	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag1, _ := store.CreateTag(ctx, "tag1")
	tag2, _ := store.CreateTag(ctx, "tag2")

	// Assign tags to agent
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag1.ID, tag2.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	// Retrieve tags
	retrieved, err := store.GetAgentTags(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentTags failed: %v", err)
	}

	if len(retrieved) != 2 {
		t.Errorf("Expected 2 tags, got %d", len(retrieved))
	}
}

func TestSetAgentTags_Replace(t *testing.T) {
	// SetAgentTags replaces: setting new tag set removes old assignments
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag1, _ := store.CreateTag(ctx, "tag1")
	tag2, _ := store.CreateTag(ctx, "tag2")
	tag3, _ := store.CreateTag(ctx, "tag3")

	// Set tags 1 and 2
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag1.ID, tag2.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	tags, err := store.GetAgentTags(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentTags failed: %v", err)
	}
	if len(tags) != 2 {
		t.Errorf("Expected 2 tags after first set, got %d", len(tags))
	}

	// Replace with tag 3 only
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag3.ID}); err != nil {
		t.Fatalf("SetAgentTags (replacement) failed: %v", err)
	}

	tags, err = store.GetAgentTags(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentTags after replacement failed: %v", err)
	}

	if len(tags) != 1 {
		t.Errorf("Expected 1 tag after replacement, got %d", len(tags))
	}
	if tags[0].ID != tag3.ID {
		t.Errorf("Expected tag3, got tag %d", tags[0].ID)
	}
}

func TestAssignModule_DirectAgent(t *testing.T) {
	// AC3.2: Modules can be assigned to an individual agent
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	// Assign module directly to agent
	_, err := store.AssignModule(ctx, "mod_a", &agent.ID, nil)
	if err != nil {
		t.Fatalf("AssignModule failed: %v", err)
	}

	modules, err := store.GetAgentModules(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModules failed: %v", err)
	}

	if len(modules) != 1 || modules[0] != "mod_a" {
		t.Errorf("Expected [mod_a], got %v", modules)
	}
}

func TestAssignModule_Tag(t *testing.T) {
	// AC3.3: Modules can be assigned to a tag
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag, _ := store.CreateTag(ctx, "prod")

	// Assign module to tag
	_, err := store.AssignModule(ctx, "mod_b", nil, &tag.ID)
	if err != nil {
		t.Fatalf("AssignModule to tag failed: %v", err)
	}

	// Assign tag to agent
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	// Agent should now have the module via tag
	modules, err := store.GetAgentModules(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModules failed: %v", err)
	}

	if len(modules) != 1 || modules[0] != "mod_b" {
		t.Errorf("Expected [mod_b], got %v", modules)
	}
}

func TestGetAgentModules_UnionOfDirectAndTag(t *testing.T) {
	// AC3.4: Agent's effective module set is union of direct + tag-inherited assignments
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag, _ := store.CreateTag(ctx, "prod")

	// Assign mod_a directly
	_, err := store.AssignModule(ctx, "mod_a", &agent.ID, nil)
	if err != nil {
		t.Fatalf("AssignModule direct failed: %v", err)
	}

	// Assign mod_b to tag
	_, err = store.AssignModule(ctx, "mod_b", nil, &tag.ID)
	if err != nil {
		t.Fatalf("AssignModule tag failed: %v", err)
	}

	// Assign tag to agent
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	modules, err := store.GetAgentModules(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModules failed: %v", err)
	}

	if len(modules) != 2 {
		t.Errorf("Expected 2 modules, got %d", len(modules))
	}

	expectedModules := map[string]bool{"mod_a": false, "mod_b": false}
	for _, m := range modules {
		if _, ok := expectedModules[m]; !ok {
			t.Errorf("Unexpected module: %s", m)
		}
		expectedModules[m] = true
	}

	for mod, found := range expectedModules {
		if !found {
			t.Errorf("Expected module not found: %s", mod)
		}
	}
}

func TestGetAgentModules_Deduplication(t *testing.T) {
	// AC3.5: Module assigned to two tags that both belong to same agent appears once
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag1, _ := store.CreateTag(ctx, "tag1")
	tag2, _ := store.CreateTag(ctx, "tag2")

	// Assign same module to both tags
	_, err := store.AssignModule(ctx, "mod_x", nil, &tag1.ID)
	if err != nil {
		t.Fatalf("AssignModule to tag1 failed: %v", err)
	}
	_, err = store.AssignModule(ctx, "mod_x", nil, &tag2.ID)
	if err != nil {
		t.Fatalf("AssignModule to tag2 failed: %v", err)
	}

	// Assign both tags to agent
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag1.ID, tag2.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	modules, err := store.GetAgentModules(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModules failed: %v", err)
	}

	if len(modules) != 1 || modules[0] != "mod_x" {
		t.Errorf("Expected exactly one mod_x, got %v", modules)
	}
}

func TestAssignModule_BothAgentAndTag_Error(t *testing.T) {
	// AC3.6: Assignment with both agent_id and tag_id is rejected (CHECK constraint)
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag, _ := store.CreateTag(ctx, "prod")

	// Try to assign with both agent and tag
	_, err := store.AssignModule(ctx, "mod_x", &agent.ID, &tag.ID)
	if err == nil {
		t.Error("Expected error for assignment with both agent_id and tag_id, got nil")
	}
}

func TestAssignModule_NeitherAgentNorTag_Error(t *testing.T) {
	// AC3.6 continued: Assignment with both nil is rejected
	store := newTestStore(t)
	defer store.Close()

	_, err := store.AssignModule(context.Background(), "mod_x", nil, nil)
	if err == nil {
		t.Error("Expected error for assignment with neither agent_id nor tag_id, got nil")
	}
}

func TestDeleteTag_Cascades(t *testing.T) {
	// DeleteTag cascades to agent_tags and module_assignments
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag, _ := store.CreateTag(ctx, "prod")

	// Assign module to tag and tag to agent
	_, err := store.AssignModule(ctx, "mod_a", nil, &tag.ID)
	if err != nil {
		t.Fatalf("AssignModule failed: %v", err)
	}
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	// Delete tag
	if err := store.DeleteTag(ctx, tag.ID); err != nil {
		t.Fatalf("DeleteTag failed: %v", err)
	}

	// Agent should have no tags
	tags, err := store.GetAgentTags(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentTags failed: %v", err)
	}
	if len(tags) != 0 {
		t.Errorf("Expected 0 tags after deletion, got %d", len(tags))
	}

	// Agent should have no modules (since the tag was deleted)
	modules, err := store.GetAgentModules(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModules failed: %v", err)
	}
	if len(modules) != 0 {
		t.Errorf("Expected 0 modules after tag deletion, got %d", len(modules))
	}
}

func TestInsertModuleResult_MultipleResultsSameModule(t *testing.T) {
	// AC5.1: Every module execution inserts new row (no upsert)
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	result1 := ModuleResult{
		AgentID:    agent.ID,
		ModuleName: "mod_a",
		Status:     "ok",
		Stdout:     "output1",
		Stderr:     "",
		ExecutedAt: 1000,
	}

	result2 := ModuleResult{
		AgentID:    agent.ID,
		ModuleName: "mod_a",
		Status:     "changed",
		Stdout:     "output2",
		Stderr:     "",
		ExecutedAt: 2000,
	}

	if err := store.InsertModuleResult(ctx, result1); err != nil {
		t.Fatalf("InsertModuleResult 1 failed: %v", err)
	}
	if err := store.InsertModuleResult(ctx, result2); err != nil {
		t.Fatalf("InsertModuleResult 2 failed: %v", err)
	}

	history, err := store.GetModuleHistory(ctx, agent.ID, "mod_a")
	if err != nil {
		t.Fatalf("GetModuleHistory failed: %v", err)
	}

	if len(history) != 2 {
		t.Errorf("Expected 2 results, got %d", len(history))
	}
}

func TestGetLatestModuleResults(t *testing.T) {
	// AC5.2: Latest result per agent per module is queryable
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	// Insert multiple results for mod_a, only latest should be returned
	if err := store.InsertModuleResult(ctx, ModuleResult{
		AgentID:    agent.ID,
		ModuleName: "mod_a",
		Status:     "ok",
		Stdout:     "old",
		ExecutedAt: 1000,
	}); err != nil {
		t.Fatalf("InsertModuleResult failed: %v", err)
	}

	if err := store.InsertModuleResult(ctx, ModuleResult{
		AgentID:    agent.ID,
		ModuleName: "mod_a",
		Status:     "changed",
		Stdout:     "new",
		ExecutedAt: 2000,
	}); err != nil {
		t.Fatalf("InsertModuleResult failed: %v", err)
	}

	// Insert result for mod_b
	if err := store.InsertModuleResult(ctx, ModuleResult{
		AgentID:    agent.ID,
		ModuleName: "mod_b",
		Status:     "ok",
		Stdout:     "mod_b_output",
		ExecutedAt: 1500,
	}); err != nil {
		t.Fatalf("InsertModuleResult failed: %v", err)
	}

	latest, err := store.GetLatestModuleResults(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetLatestModuleResults failed: %v", err)
	}

	if len(latest) != 2 {
		t.Errorf("Expected 2 latest results, got %d", len(latest))
	}

	// Find modA in results
	var modA *ModuleResult
	var modB *ModuleResult
	for _, r := range latest {
		if r.ModuleName == "mod_a" {
			modA = &r
		}
		if r.ModuleName == "mod_b" {
			modB = &r
		}
	}

	if modA == nil {
		t.Fatal("mod_a not found in latest results")
	}
	if modA.ExecutedAt != 2000 {
		t.Errorf("mod_a ExecutedAt: expected 2000, got %d", modA.ExecutedAt)
	}
	if modA.Status != "changed" {
		t.Errorf("mod_a Status: expected changed, got %s", modA.Status)
	}
	if modA.Stdout != "new" {
		t.Errorf("mod_a Stdout: expected new, got %s", modA.Stdout)
	}

	if modB == nil {
		t.Fatal("mod_b not found in latest results")
	}
	if modB.ExecutedAt != 1500 {
		t.Errorf("mod_b ExecutedAt: expected 1500, got %d", modB.ExecutedAt)
	}
}

func TestGetLatestModuleResults_DuplicateTimestamp(t *testing.T) {
	// When two results for the same agent+module have identical executed_at,
	// GetLatestModuleResults must still return exactly one row per module.
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	// Insert two results for mod_a with the same timestamp
	if err := store.InsertModuleResult(ctx, ModuleResult{
		AgentID:    agent.ID,
		ModuleName: "mod_a",
		Status:     "ok",
		Stdout:     "first",
		ExecutedAt: 5000,
	}); err != nil {
		t.Fatalf("InsertModuleResult failed: %v", err)
	}

	if err := store.InsertModuleResult(ctx, ModuleResult{
		AgentID:    agent.ID,
		ModuleName: "mod_a",
		Status:     "changed",
		Stdout:     "second",
		ExecutedAt: 5000,
	}); err != nil {
		t.Fatalf("InsertModuleResult failed: %v", err)
	}

	latest, err := store.GetLatestModuleResults(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetLatestModuleResults failed: %v", err)
	}

	if len(latest) != 1 {
		t.Fatalf("Expected 1 latest result, got %d", len(latest))
	}
}

func TestGetModuleHistory_DescendingOrder(t *testing.T) {
	// AC5.3: Full history is queryable in descending executed_at order
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	timestamps := []int64{1000, 2000, 3000}
	for _, ts := range timestamps {
		if err := store.InsertModuleResult(ctx, ModuleResult{
			AgentID:    agent.ID,
			ModuleName: "mod_a",
			Status:     "ok",
			Stdout:     "output",
			ExecutedAt: ts,
		}); err != nil {
			t.Fatalf("InsertModuleResult failed: %v", err)
		}
	}

	history, err := store.GetModuleHistory(ctx, agent.ID, "mod_a")
	if err != nil {
		t.Fatalf("GetModuleHistory failed: %v", err)
	}

	if len(history) != 3 {
		t.Errorf("Expected 3 results, got %d", len(history))
	}

	// Verify descending order
	expectedOrder := []int64{3000, 2000, 1000}
	for i, expected := range expectedOrder {
		if history[i].ExecutedAt != expected {
			t.Errorf("Result %d: expected timestamp %d, got %d", i, expected, history[i].ExecutedAt)
		}
	}
}

func TestListAssignments_EmptyDatabase(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	assignments, err := store.ListAssignments(context.Background())
	if err != nil {
		t.Fatalf("ListAssignments failed: %v", err)
	}

	if len(assignments) != 0 {
		t.Errorf("Expected 0 assignments, got %d", len(assignments))
	}
}

func TestListAssignments_ReturnsAllAssignments(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag, err := store.CreateTag(ctx, "prod")
	if err != nil {
		t.Fatalf("CreateTag failed: %v", err)
	}

	// Create direct assignment
	_, err = store.AssignModule(ctx, "mod_a", &agent.ID, nil)
	if err != nil {
		t.Fatalf("AssignModule direct failed: %v", err)
	}

	// Create tag assignment
	_, err = store.AssignModule(ctx, "mod_b", nil, &tag.ID)
	if err != nil {
		t.Fatalf("AssignModule tag failed: %v", err)
	}

	assignments, err := store.ListAssignments(ctx)
	if err != nil {
		t.Fatalf("ListAssignments failed: %v", err)
	}

	if len(assignments) != 2 {
		t.Errorf("Expected 2 assignments, got %d", len(assignments))
	}

	// Check that mod_a is direct and mod_b is tag
	for _, a := range assignments {
		if a.ModuleName == "mod_a" {
			if a.AgentID == nil || *a.AgentID != agent.ID {
				t.Errorf("mod_a should be assigned to agent %s", agent.ID)
			}
			if a.TagID != nil {
				t.Error("mod_a should not have tag_id")
			}
		} else if a.ModuleName == "mod_b" {
			if a.AgentID != nil {
				t.Error("mod_b should not have agent_id")
			}
			if a.TagID == nil || *a.TagID != tag.ID {
				t.Errorf("mod_b should be assigned to tag %d", tag.ID)
			}
		}
	}
}

func TestGetAgentModuleDetails_DirectAssignment(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	// Assign module directly to agent
	_, err := store.AssignModule(ctx, "mod_a", &agent.ID, nil)
	if err != nil {
		t.Fatalf("AssignModule failed: %v", err)
	}

	details, err := store.GetAgentModuleDetails(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModuleDetails failed: %v", err)
	}

	if len(details) != 1 {
		t.Errorf("Expected 1 module, got %d", len(details))
	}

	if details[0].ModuleName != "mod_a" {
		t.Errorf("Expected module mod_a, got %s", details[0].ModuleName)
	}
	if details[0].Source != "direct" {
		t.Errorf("Expected source 'direct', got %s", details[0].Source)
	}
	if details[0].AssignmentID == 0 {
		t.Error("AssignmentID should be set")
	}
}

func TestGetAgentModuleDetails_TagAssignment(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag, err := store.CreateTag(ctx, "prod")
	if err != nil {
		t.Fatalf("CreateTag failed: %v", err)
	}

	// Assign module to tag
	_, err = store.AssignModule(ctx, "mod_b", nil, &tag.ID)
	if err != nil {
		t.Fatalf("AssignModule failed: %v", err)
	}

	// Assign tag to agent
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	details, err := store.GetAgentModuleDetails(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModuleDetails failed: %v", err)
	}

	if len(details) != 1 {
		t.Errorf("Expected 1 module, got %d", len(details))
	}

	if details[0].ModuleName != "mod_b" {
		t.Errorf("Expected module mod_b, got %s", details[0].ModuleName)
	}
	if details[0].Source != "tag:prod" {
		t.Errorf("Expected source 'tag:prod', got %s", details[0].Source)
	}
	if details[0].AssignmentID == 0 {
		t.Error("AssignmentID should be set")
	}
}

func TestGetAgentModuleDetails_BothDirectAndTag(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag, err := store.CreateTag(ctx, "prod")
	if err != nil {
		t.Fatalf("CreateTag failed: %v", err)
	}

	// Assign mod_a directly
	_, err = store.AssignModule(ctx, "mod_a", &agent.ID, nil)
	if err != nil {
		t.Fatalf("AssignModule direct failed: %v", err)
	}

	// Assign mod_b to tag
	_, err = store.AssignModule(ctx, "mod_b", nil, &tag.ID)
	if err != nil {
		t.Fatalf("AssignModule tag failed: %v", err)
	}

	// Assign tag to agent
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	details, err := store.GetAgentModuleDetails(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModuleDetails failed: %v", err)
	}

	if len(details) != 2 {
		t.Errorf("Expected 2 modules, got %d", len(details))
	}

	// Check mod_a is direct
	var modA, modB *AgentModuleDetail
	for i := range details {
		if details[i].ModuleName == "mod_a" {
			modA = &details[i]
		}
		if details[i].ModuleName == "mod_b" {
			modB = &details[i]
		}
	}

	if modA == nil {
		t.Fatal("mod_a not found in details")
	}
	if modA.Source != "direct" {
		t.Errorf("mod_a: expected source 'direct', got %s", modA.Source)
	}

	if modB == nil {
		t.Fatal("mod_b not found in details")
	}
	if modB.Source != "tag:prod" {
		t.Errorf("mod_b: expected source 'tag:prod', got %s", modB.Source)
	}
}

func TestGetAgentModuleDetails_Deduplication(t *testing.T) {
	// When a module is assigned both directly and via tag, only direct appears
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	tag, err := store.CreateTag(ctx, "prod")
	if err != nil {
		t.Fatalf("CreateTag failed: %v", err)
	}

	// Assign same module both directly and to tag
	_, err = store.AssignModule(ctx, "mod_x", &agent.ID, nil)
	if err != nil {
		t.Fatalf("AssignModule direct failed: %v", err)
	}

	_, err = store.AssignModule(ctx, "mod_x", nil, &tag.ID)
	if err != nil {
		t.Fatalf("AssignModule tag failed: %v", err)
	}

	// Assign tag to agent
	if err := store.SetAgentTags(ctx, agent.ID, []int64{tag.ID}); err != nil {
		t.Fatalf("SetAgentTags failed: %v", err)
	}

	details, err := store.GetAgentModuleDetails(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModuleDetails failed: %v", err)
	}

	if len(details) != 1 {
		t.Errorf("Expected 1 module (deduplicated), got %d", len(details))
	}

	if details[0].ModuleName != "mod_x" {
		t.Errorf("Expected module mod_x, got %s", details[0].ModuleName)
	}
	if details[0].Source != "direct" {
		t.Errorf("Expected source 'direct' (not tag), got %s", details[0].Source)
	}
}

func TestGetAgentModuleDetails_NoModules(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	details, err := store.GetAgentModuleDetails(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgentModuleDetails failed: %v", err)
	}

	if len(details) != 0 {
		t.Errorf("Expected 0 modules, got %d", len(details))
	}
}

func TestSetAgentDisplayName(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	// Set display name
	name := "My Server"
	if err := store.SetAgentDisplayName(ctx, agent.ID, &name); err != nil {
		t.Fatalf("SetAgentDisplayName failed: %v", err)
	}

	retrieved, err := store.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if retrieved.DisplayName == nil || *retrieved.DisplayName != "My Server" {
		t.Errorf("Expected display name 'My Server', got %v", retrieved.DisplayName)
	}

	// Clear display name
	if err := store.SetAgentDisplayName(ctx, agent.ID, nil); err != nil {
		t.Fatalf("SetAgentDisplayName (nil) failed: %v", err)
	}

	retrieved, err = store.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if retrieved.DisplayName != nil {
		t.Errorf("Expected nil display name, got %v", *retrieved.DisplayName)
	}
}

func TestUpsertAgentPreservesDisplayName(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	agent := Agent{ID: "a1", Hostname: "host1", LastSeenAt: 1000}
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent failed: %v", err)
	}

	// Set display name
	name := "Custom Name"
	if err := store.SetAgentDisplayName(ctx, agent.ID, &name); err != nil {
		t.Fatalf("SetAgentDisplayName failed: %v", err)
	}

	// Upsert again (simulating a checkin)
	agent.LastSeenAt = 2000
	agent.Hostname = "host1-updated"
	if err := store.UpsertAgent(ctx, agent); err != nil {
		t.Fatalf("UpsertAgent (second) failed: %v", err)
	}

	retrieved, err := store.GetAgent(ctx, agent.ID)
	if err != nil {
		t.Fatalf("GetAgent failed: %v", err)
	}
	if retrieved.DisplayName == nil || *retrieved.DisplayName != "Custom Name" {
		t.Errorf("Expected display name 'Custom Name' preserved after upsert, got %v", retrieved.DisplayName)
	}
	if retrieved.Hostname != "host1-updated" {
		t.Errorf("Expected hostname 'host1-updated', got %s", retrieved.Hostname)
	}
}

// TestConcurrentUpsertAgents verifies that concurrent writes do not produce
// SQLITE_BUSY errors, which validates that SetMaxOpenConns(1) serializes access.
func TestConcurrentUpsertAgents(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()
	const goroutines = 10

	var wg sync.WaitGroup
	errs := make(chan error, goroutines)

	for i := range goroutines {
		wg.Add(1)
		go func() {
			defer wg.Done()
			agent := Agent{
				ID:         fmt.Sprintf("agent-%d", i),
				Hostname:   fmt.Sprintf("host-%d", i),
				RemoteIP:   "10.0.0.1",
				OS:         "linux",
				Arch:       "amd64",
				Distro:     "ubuntu",
				LastSeenAt: 1000,
			}
			if err := store.UpsertAgent(ctx, agent); err != nil {
				errs <- fmt.Errorf("goroutine %d: %w", i, err)
			}
		}()
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent UpsertAgent failed: %v", err)
	}

	agents, err := store.ListAgents(ctx)
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != goroutines {
		t.Fatalf("expected %d agents, got %d", goroutines, len(agents))
	}
}

// Helper function to create a test store with a temporary database.
// Marks the test as parallel since each store is fully isolated.
func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	t.Parallel()

	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	store, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open test database: %v", err)
	}

	return store
}
