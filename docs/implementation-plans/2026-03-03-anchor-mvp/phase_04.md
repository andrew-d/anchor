# Anchor MVP Implementation Plan — Phase 4: Server & Agent API

**Goal:** Working server with checkin and report endpoints. Agents can check in, receive modules, and report results.

**Architecture:** HTTP handlers in `internal/server/` wired to the `Store` interface and `module.Loader`. The checkin endpoint upserts the agent, resolves its effective module set, and returns full script content. The report endpoint inserts a module result row.

**Tech Stack:** Go 1.26, `net/http`, `encoding/json`, stdlib only

**Scope:** 7 phases from original design (phase 4 of 7)

**Codebase verified:** 2026-03-03 — confirmed greenfield. Phase 1 creates scaffolding, Phase 2 creates Store, Phase 3 creates Loader.

---

## Acceptance Criteria Coverage

This phase implements and tests:

### anchor-mvp.AC1: Agent Registration
- **anchor-mvp.AC1.1 Success:** First check-in from a new UUID creates an agent record with hostname, OS, arch, distro, remote IP, and last_seen_at
- **anchor-mvp.AC1.2 Success:** Subsequent check-ins from an existing UUID update system info and last_seen_at
- **anchor-mvp.AC1.3 Edge:** Agent with changed hostname (e.g., machine rebuilt, same UUID file) updates correctly

### anchor-mvp.AC2: Module Sync & Execution
- **anchor-mvp.AC2.1 Success:** Checkin response contains full script content for all assigned modules
- **anchor-mvp.AC2.4 Success:** Agent reports each module result individually via POST /api/report

### anchor-mvp.AC3: Tags & Assignment
- **anchor-mvp.AC3.4 Success:** Agent's effective module set is the union of direct assignments + tag-inherited assignments
- **anchor-mvp.AC3.5 Edge:** Module assigned to two tags that both belong to the same agent appears only once in the module set

### anchor-mvp.AC5: Module Result History
- **anchor-mvp.AC5.1 Success:** Every module execution inserts a new row (no upsert)

---

<!-- START_TASK_1 -->
### Task 1: Define API request/response types

**Files:**
- Create: `internal/server/api.go`

**Implementation:**

Define JSON-serializable types for the agent API protocol:

```go
package server

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
	PollIntervalSeconds int              `json:"poll_interval_seconds"`
	Modules             []CheckinModule  `json:"modules"`
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
```

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: add API request/response types`
<!-- END_TASK_1 -->

<!-- START_TASK_2 -->
### Task 2: Wire up server with Store and Loader

**Files:**
- Modify: `internal/server/server.go`

**Implementation:**

Update the `Server` struct to hold a `db.Store` and `module.Loader`. Update `New()` to accept these or create them internally. Update `Run()` to:

1. Open the SQLite database (at `dataDir/anchor.db`)
2. Create a `module.Loader` for the modules directory
3. Register routes on the mux:
   - `POST /api/checkin` → `s.handleCheckin`
   - `POST /api/report` → `s.handleReport`
   - `GET /` → placeholder (will be replaced with SPA in Phase 6)
4. Start HTTP server

The server should store a `pollInterval` field (default 300 seconds) that it returns in checkin responses.

Ensure the database is closed when the server shuts down (defer `store.Close()` in `Run()`).

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: wire server with Store and Loader`
<!-- END_TASK_2 -->

<!-- START_TASK_3 -->
### Task 3: Implement checkin handler

**Files:**
- Modify: `internal/server/api.go`

**Implementation:**

Implement `handleCheckin(w http.ResponseWriter, r *http.Request)`:

1. Decode JSON request body into `CheckinRequest`
2. Extract remote IP from `r.RemoteAddr` (strip port with `net.SplitHostPort`)
3. Upsert agent into store:
   ```go
   agent := db.Agent{
       ID:         req.ID,
       Hostname:   req.Hostname,
       RemoteIP:   remoteIP,
       OS:         req.OS,
       Arch:       req.Arch,
       Distro:     req.Distro,
       LastSeenAt: time.Now().Unix(),
   }
   s.store.UpsertAgent(r.Context(), agent)
   ```
4. Get agent's effective module set: `s.store.GetAgentModules(r.Context(), req.ID)` — returns deduplicated list of module names
5. Load current modules: `s.loader.LoadAll()` — refreshes from disk
6. Build response modules by matching assigned names against loaded modules:
   ```go
   var modules []CheckinModule
   for _, name := range assignedNames {
       if mod, ok := s.loader.GetModule(name); ok {
           modules = append(modules, CheckinModule{Name: name, Script: mod.Script})
       }
   }
   ```
7. Return JSON response with `PollIntervalSeconds` and `Modules`

Return 400 for malformed JSON or missing ID. Return 500 for store errors.

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: implement POST /api/checkin handler`
<!-- END_TASK_3 -->

<!-- START_TASK_4 -->
### Task 4: Implement report handler

**Files:**
- Modify: `internal/server/api.go`

**Implementation:**

Implement `handleReport(w http.ResponseWriter, r *http.Request)`:

1. Decode JSON request body into `ReportRequest`
2. Validate: `agent_id`, `module_name`, and `status` must be non-empty. `status` must be one of "ok", "changed", "error".
3. Insert module result:
   ```go
   result := db.ModuleResult{
       AgentID:    req.AgentID,
       ModuleName: req.ModuleName,
       Status:     req.Status,
       Stdout:     req.Stdout,
       Stderr:     req.Stderr,
       ExecutedAt: req.ExecutedAt,
   }
   s.store.InsertModuleResult(r.Context(), result)
   ```
4. Return `{"ok": true}`

Return 400 for malformed JSON or invalid fields. Return 500 for store errors.

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: implement POST /api/report handler`
<!-- END_TASK_4 -->

<!-- START_TASK_5 -->
### Task 5: Integration tests for checkin and report endpoints

**Verifies:** anchor-mvp.AC1.1, anchor-mvp.AC1.2, anchor-mvp.AC1.3, anchor-mvp.AC2.1, anchor-mvp.AC2.4, anchor-mvp.AC3.4, anchor-mvp.AC3.5, anchor-mvp.AC5.1

**Files:**
- Create: `internal/server/api_test.go`
- Test file: `internal/server/api_test.go` (integration — real SQLite + real HTTP + temp module directory)

**Testing:**

Each test sets up:
- SQLite database in `t.TempDir()`
- Module scripts in a separate `t.TempDir()`
- A real `Server` instance
- `httptest.NewServer` to get a test HTTP server

Tests must verify each AC listed above:

- **anchor-mvp.AC1.1:** POST checkin with new UUID. Verify agent record created in store with correct hostname, OS, arch, distro, remote_ip, and last_seen_at.
- **anchor-mvp.AC1.2:** POST checkin twice with same UUID, second time with updated last_seen_at. Verify store has updated timestamp.
- **anchor-mvp.AC1.3:** POST checkin with same UUID but different hostname. Verify store has updated hostname.
- **anchor-mvp.AC2.1:** Create a module script in the temp directory, assign it to the agent via store. POST checkin. Verify response contains the module with full script content.
- **anchor-mvp.AC2.4:** POST report with agent_id, module_name, status, stdout, stderr. Verify store has the result.
- **anchor-mvp.AC3.4:** Assign module "mod_a" directly to agent, assign "mod_b" to a tag, set tag on agent. POST checkin. Verify response contains both modules.
- **anchor-mvp.AC3.5:** Assign same module to two tags both on same agent. POST checkin. Verify module appears once in response.
- **anchor-mvp.AC5.1:** POST report twice for same agent+module. Verify store has two result rows.

Also test error cases:
- POST checkin with empty body returns 400
- POST checkin with missing ID returns 400
- POST report with invalid status returns 400
- GET /api/checkin returns 405 (method not allowed, since we use `POST /api/checkin` route pattern)

**Verification:**

Run: `go test ./internal/server/ -count=1 -v`
Expected: All tests pass

Run: `go test ./... -count=1`
Expected: All tests pass (including Phase 2 and Phase 3 tests)

**Commit:** `test: add integration tests for checkin and report endpoints`
<!-- END_TASK_5 -->
