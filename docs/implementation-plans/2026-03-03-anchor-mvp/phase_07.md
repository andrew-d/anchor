# Anchor MVP Implementation Plan — Phase 7: Management UI

**Goal:** Web UI for managing tags, assigning modules to agents and tags, and viewing the effective module set per agent.

**Architecture:** Additional Preact components in the existing SPA with corresponding CRUD API endpoints in the Go server. Hash routes for tag management, module assignment, and module list views.

**Tech Stack:** Go 1.26, `net/http`, Preact + HTM (vendored), existing `db.Store` and `module.Loader`

**Scope:** 7 phases from original design (phase 7 of 7)

**Codebase verified:** 2026-03-03 — confirmed greenfield. Phase 6 creates SPA shell, dashboard, and UI API endpoints.

---

## Acceptance Criteria Coverage

This phase implements and tests:

### anchor-mvp.AC3: Tags & Assignment
- **anchor-mvp.AC3.1 Success:** Tags can be created and assigned to agents
- **anchor-mvp.AC3.2 Success:** Modules can be assigned to an individual agent
- **anchor-mvp.AC3.3 Success:** Modules can be assigned to a tag

### anchor-mvp.AC9: Management UI
- **anchor-mvp.AC9.1 Success:** Tags can be created and deleted from the UI
- **anchor-mvp.AC9.2 Success:** Tags can be assigned to and removed from agents
- **anchor-mvp.AC9.3 Success:** Modules can be assigned to agents or tags from the UI
- **anchor-mvp.AC9.4 Success:** Effective module set per agent is visible (showing which come from direct assignment vs tags)
- **anchor-mvp.AC9.5 Success:** Module list shows all loaded modules with metadata (name, description)

---

<!-- START_TASK_0 -->
### Task 0: Extend Store interface for management operations

**Files:**
- Modify: `internal/db/db.go` (add new methods to Store interface)
- Modify: `internal/db/sqlite.go` (implement new methods)
- Modify: `internal/db/db_test.go` (add tests for new methods)

**Implementation:**

The management UI requires Store operations not yet defined. Add these to the `Store` interface:

```go
// Add to Store interface:
ListAssignments(ctx context.Context) ([]ModuleAssignment, error)
GetAgentModuleDetails(ctx context.Context, agentID string) ([]AgentModuleDetail, error)
```

Add a new type for the detailed module view:

```go
type AgentModuleDetail struct {
	ModuleName   string
	Source       string // "direct" or "tag:<tagname>"
	AssignmentID int64  // ID from module_assignments, for deletion
}
```

**`ListAssignments`:** `SELECT * FROM module_assignments ORDER BY module_name`

**`GetAgentModuleDetails`:** Query direct assignments and tag-based assignments separately, merge with deduplication:
- Direct: `SELECT id, module_name FROM module_assignments WHERE agent_id = ?`
- Via tags: `SELECT ma.id, ma.module_name, t.name FROM module_assignments ma JOIN tags t ON ma.tag_id = t.id JOIN agent_tags at ON at.tag_id = t.id WHERE at.agent_id = ?`
- When a module appears in both, prefer the "direct" entry (include both assignment IDs or just the direct one)

**Testing:**

- `ListAssignments` returns all assignments
- `GetAgentModuleDetails` with direct assignment returns source "direct" with assignment_id
- `GetAgentModuleDetails` with tag assignment returns source "tag:<name>" with assignment_id
- `GetAgentModuleDetails` with both returns deduplicated list, direct preferred

**Verification:**

Run: `go test ./internal/db/ -count=1 -v`
Expected: All tests pass

**Commit:** `feat: extend Store interface for management operations`
<!-- END_TASK_0 -->

<!-- START_TASK_1 -->
### Task 1: Add management API endpoints

**Files:**
- Modify: `internal/server/ui.go`
- Modify: `internal/server/server.go` (register routes)

**Implementation:**

Add CRUD API endpoints consumed by the management UI:

**Tags:**
- `GET /api/tags` — List all tags. Response: `{"tags": [{"id": 1, "name": "webservers"}, ...]}`
- `POST /api/tags` — Create a tag. Request: `{"name": "webservers"}`. Response: `{"id": 1, "name": "webservers"}`
- `DELETE /api/tags/{id}` — Delete a tag. Response: `{"ok": true}`

**Agent Tags:**
- `PUT /api/agents/{id}/tags` — Set agent's tags (replace all). Request: `{"tag_ids": [1, 2]}`. Response: `{"ok": true}`. Calls `store.SetAgentTags`.

**Module Assignments:**
- `GET /api/assignments` — List all module assignments. Response: `{"assignments": [{"id": 1, "module_name": "00_base", "agent_id": "uuid", "tag_id": null}, ...]}`.
- `POST /api/assignments` — Create an assignment. Request: `{"module_name": "00_base", "agent_id": "uuid"}` or `{"module_name": "00_base", "tag_id": 1}`. Exactly one of `agent_id` or `tag_id` must be set. Response: the created assignment.
- `DELETE /api/assignments/{id}` — Remove an assignment. Response: `{"ok": true}`

**Effective Modules:**
- `GET /api/agents/{id}/modules` — Get agent's effective module set with source info. Uses `store.GetAgentModuleDetails()` (added in Task 0). Response:
  ```json
  {
      "modules": [
          {"name": "00_base", "source": "direct", "assignment_id": 5},
          {"name": "10_users", "source": "tag:webservers", "assignment_id": 8}
      ]
  }
  ```
  The `assignment_id` is needed by the UI to call `DELETE /api/assignments/{id}` for direct assignments.

**Modules List:**
- `GET /api/modules` — List all loaded modules with metadata. Calls `loader.LoadAll()`. Response: `{"modules": [{"name": "Base Config", "description": "Installs base packages", "filename": "00_base"}, ...]}`

Register all new routes in `server.go`.

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: add management CRUD API endpoints`
<!-- END_TASK_1 -->

<!-- START_TASK_2 -->
### Task 2: Test management API endpoints

**Verifies:** anchor-mvp.AC3.1, anchor-mvp.AC3.2, anchor-mvp.AC3.3, anchor-mvp.AC9.4

**Files:**
- Modify: `internal/server/ui_test.go`
- Test file: `internal/server/ui_test.go` (integration)

**Testing:**

Add tests using the existing test server setup from Phase 6:

- **anchor-mvp.AC3.1 (via API):** POST `/api/tags` to create a tag. GET `/api/tags` to verify it appears. PUT `/api/agents/{id}/tags` to assign it. GET `/api/agents/{id}` to verify tag appears on agent.
- **anchor-mvp.AC3.2 (via API):** POST `/api/assignments` with `agent_id` set. GET `/api/agents/{id}/modules` to verify the module appears with source "direct".
- **anchor-mvp.AC3.3 (via API):** POST `/api/assignments` with `tag_id` set. Assign tag to agent. GET `/api/agents/{id}/modules` to verify module appears with source "tag:<name>".
- **anchor-mvp.AC9.4 (via API):** Assign module "mod_a" directly to agent, assign "mod_b" to a tag on the agent. GET `/api/agents/{id}/modules`. Verify both appear with correct source annotations.

Also test:
- DELETE `/api/tags/{id}` removes the tag and cascades
- DELETE `/api/assignments/{id}` removes the assignment
- POST `/api/assignments` with both agent_id and tag_id returns 400
- GET `/api/modules` returns loaded modules with metadata

**Verification:**

Run: `go test ./internal/server/ -count=1 -v`
Expected: All tests pass

Run: `go test ./... -count=1`
Expected: All tests pass

**Commit:** `test: add management API endpoint tests`
<!-- END_TASK_2 -->

<!-- START_TASK_3 -->
### Task 3: Add tag management UI component

**Files:**
- Modify: `static/app.js`

**Implementation:**

Add a Tags management page accessible via `/#/tags`. The component:

1. Fetches `GET /api/tags` on mount
2. Displays all tags in a list, each showing its name
3. Has a form to create a new tag (text input + submit button)
4. Each tag has a delete button
5. Each tag name links to a detail view at `/#/tags/{id}`
6. Create calls `POST /api/tags`, delete calls `DELETE /api/tags/{id}`
7. Refreshes list after create/delete

Add a Tag detail page at `/#/tags/{id}` that shows:
1. Tag name
2. **Modules assigned to this tag** — list of module assignments with remove buttons. Fetched by filtering `GET /api/assignments` for entries with this tag_id. (AC9.3)
3. **Add module to tag** — dropdown populated from `GET /api/modules`, calls `POST /api/assignments` with `tag_id`. (AC9.3)
4. Back link to `/#/tags`

Add navigation link to header: Dashboard | Tags | Modules

Add the `/#/tags` and `/#/tags/{id}` routes to the App router.

**Commit:** `feat: add tag management UI with module assignment`
<!-- END_TASK_3 -->

<!-- START_TASK_4 -->
### Task 4: Add module assignment UI to agent detail

**Files:**
- Modify: `static/app.js`

**Implementation:**

Enhance the AgentDetail component (from Phase 6) to show:

1. **Tag assignment section:** Shows currently assigned tags with remove buttons. Has a dropdown to add a tag (populated from `GET /api/tags`). Assign calls `PUT /api/agents/{id}/tags`. (AC9.2)

2. **Module assignment section:** Shows effective module set from `GET /api/agents/{id}/modules` with source annotations ("direct" or "tag:name"). Has a dropdown to add a direct module assignment (populated from `GET /api/modules`). Assign calls `POST /api/assignments`. Remove direct assignments calls `DELETE /api/assignments/{id}`. (AC9.3, AC9.4)

The effective module list should clearly distinguish:
- Modules assigned directly (with a remove button)
- Modules inherited from tags (shown with tag name, no remove button — remove the tag assignment instead)

**Commit:** `feat: add module and tag assignment to agent detail`
<!-- END_TASK_4 -->

<!-- START_TASK_5 -->
### Task 5: Add module list page

**Files:**
- Modify: `static/app.js`

**Implementation:**

Add a Modules list page accessible via `/#/modules`. The component:

1. Fetches `GET /api/modules` on mount
2. Displays all loaded modules with their metadata: name and description (AC9.5)
3. This is read-only — modules are managed as files on disk, not through the UI

Add the `/#/modules` route to the App router.

**Commit:** `feat: add module list page`
<!-- END_TASK_5 -->

<!-- START_TASK_6 -->
### Task 6: Final verification

**Step 1: Build**

Run: `go build ./...`
Expected: No errors

**Step 2: Vet**

Run: `go vet ./...`
Expected: No errors

**Step 3: All tests**

Run: `go test ./... -count=1`
Expected: All tests pass

**Step 4: Manual smoke test**

Start server with a test modules directory:
```bash
mkdir -p /tmp/anchor-test-modules
cat > /tmp/anchor-test-modules/00_test <<'EOF'
#!/bin/sh
case "$1" in
    metadata) echo '{"name": "Test Module", "description": "A test module"}' ;;
    apply) echo "applied"; exit 0 ;;
esac
EOF
chmod +x /tmp/anchor-test-modules/00_test

./anchor server --port 9999 --modules-dir /tmp/anchor-test-modules --data-dir /tmp/anchor-test-data
```

In browser: navigate to `http://localhost:9999/`
- Dashboard loads (empty, no agents yet)
- Navigate to Tags, create a tag
- Navigate to Modules, see "Test Module" listed

**Step 5: Commit (if any final adjustments needed)**

```bash
git add -A
git commit -m "chore: final adjustments for Phase 7"
```
<!-- END_TASK_6 -->
