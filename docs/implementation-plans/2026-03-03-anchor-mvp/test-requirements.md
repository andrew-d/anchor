# Test Requirements

This document maps each acceptance criterion from the Anchor MVP design to specific automated tests and, where applicable, identifies criteria requiring human verification.

## Test Matrix

### AC1: Agent Registration

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC1.1 | First check-in creates agent record with hostname, OS, arch, distro, remote IP, last_seen_at | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC1.1 | First check-in creates agent record via HTTP checkin endpoint | integration | `internal/server/api_test.go` | Phase 4 |
| anchor-mvp.AC1.2 | Subsequent check-ins update system info and last_seen_at | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC1.2 | Subsequent check-ins update via HTTP checkin endpoint | integration | `internal/server/api_test.go` | Phase 4 |
| anchor-mvp.AC1.3 | Changed hostname on same UUID updates correctly | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC1.3 | Changed hostname via HTTP checkin endpoint | integration | `internal/server/api_test.go` | Phase 4 |

### AC2: Module Sync & Execution

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC2.1 | Checkin response contains full script content for all assigned modules | integration | `internal/server/api_test.go` | Phase 4 |
| anchor-mvp.AC2.2 | Agent executes modules in sorted name order | unit | `internal/agent/runner_test.go` | Phase 5 |
| anchor-mvp.AC2.3 | Exit code 0 recorded as "ok", exit code 80 as "changed" | unit | `internal/agent/runner_test.go` | Phase 5 |
| anchor-mvp.AC2.4 | Agent reports each module result individually via POST /api/report | unit | `internal/server/api_test.go` | Phase 4 |
| anchor-mvp.AC2.4 | Agent reports each module result individually in polling loop | integration | `internal/agent/agent_test.go` | Phase 5 |
| anchor-mvp.AC2.5 | Non-zero/non-80 exit code recorded as "error" with captured stderr | unit | `internal/agent/runner_test.go` | Phase 5 |
| anchor-mvp.AC2.6 | Report failure causes agent to stop executing remaining modules | integration | `internal/agent/agent_test.go` | Phase 5 |

### AC3: Tags & Assignment

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC3.1 | Tags can be created and assigned to agents (Store layer) | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC3.1 | Tags can be created and assigned to agents (API layer) | integration | `internal/server/ui_test.go` | Phase 7 |
| anchor-mvp.AC3.2 | Modules can be assigned to an individual agent (Store layer) | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC3.2 | Modules can be assigned to an individual agent (API layer) | integration | `internal/server/ui_test.go` | Phase 7 |
| anchor-mvp.AC3.3 | Modules can be assigned to a tag (Store layer) | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC3.3 | Modules can be assigned to a tag (API layer) | integration | `internal/server/ui_test.go` | Phase 7 |
| anchor-mvp.AC3.4 | Effective module set is union of direct + tag-inherited assignments (Store layer) | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC3.4 | Effective module set is union of direct + tag-inherited assignments (API layer) | integration | `internal/server/api_test.go` | Phase 4 |
| anchor-mvp.AC3.5 | Duplicate module from two tags appears only once (Store layer) | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC3.5 | Duplicate module from two tags appears only once (API layer) | integration | `internal/server/api_test.go` | Phase 4 |
| anchor-mvp.AC3.6 | Assignment with both agent_id and tag_id set is rejected (CHECK constraint) | unit | `internal/db/db_test.go` | Phase 2 |

### AC4: Module Loading

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC4.1 | Server reads all scripts from config directory and extracts metadata | integration | `internal/module/module_test.go` | Phase 3 |
| anchor-mvp.AC4.2 | Adding a new script file is detected on next call without restart | integration | `internal/module/module_test.go` | Phase 3 |
| anchor-mvp.AC4.3 | Modifying a script file triggers re-parsing of metadata | integration | `internal/module/module_test.go` | Phase 3 |
| anchor-mvp.AC4.4 | Removing a script file drops it from the available module set | integration | `internal/module/module_test.go` | Phase 3 |
| anchor-mvp.AC4.5 | Unchanged files reuse cached metadata (hash comparison) | integration | `internal/module/module_test.go` | Phase 3 |

### AC5: Module Result History

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC5.1 | Every module execution inserts a new row (no upsert) (Store layer) | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC5.1 | Every module execution inserts a new row (API layer) | integration | `internal/server/api_test.go` | Phase 4 |
| anchor-mvp.AC5.2 | Latest result per agent per module is queryable for dashboard display | unit | `internal/db/db_test.go` | Phase 2 |
| anchor-mvp.AC5.3 | Full history per agent per module is queryable for detail view | unit | `internal/db/db_test.go` | Phase 2 |

### AC6: Agent Identity & System Info

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC6.1 | Agent generates a UUID and persists it to a file on first run | unit | `internal/agent/agent_test.go` | Phase 5 |
| anchor-mvp.AC6.2 | Agent reads existing UUID from file on subsequent runs | unit | `internal/agent/agent_test.go` | Phase 5 |
| anchor-mvp.AC6.3 | Linux agent detects OS, arch, and distro from /etc/os-release | unit | `internal/agent/sysinfo_test.go` | Phase 5 |
| anchor-mvp.AC6.4 | macOS agent detects OS, arch, and version from sw_vers | unit | `internal/agent/sysinfo_test.go` | Phase 5 |

### AC7: Health Dashboard

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC7.1 | Agents with modules in "error" status appear in "unhealthy" group | integration | `internal/server/ui_test.go` | Phase 6 |
| anchor-mvp.AC7.2 | Agents not seen within 2x polling interval appear in "stale" group | integration | `internal/server/ui_test.go` | Phase 6 |
| anchor-mvp.AC7.3 | Agents with all modules "ok"/"changed" and recent check-in appear in "healthy" group | integration | `internal/server/ui_test.go` | Phase 6 |
| anchor-mvp.AC7.4 | Unhealthy displayed first, stale second, healthy last | human | N/A | Phase 6 |

### AC8: Agent Detail View

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC8.1 | Shows hostname, remote IP, OS/arch/distro, last seen time | human | N/A | Phase 6 |
| anchor-mvp.AC8.2 | Shows assigned tags | human | N/A | Phase 6 |
| anchor-mvp.AC8.3 | Shows per-module latest status with expandable stdout/stderr | human | N/A | Phase 6 |

### AC9: Management UI

| AC ID | AC Description | Test Type | Test File Path | Phase |
|-------|---------------|-----------|----------------|-------|
| anchor-mvp.AC9.1 | Tags can be created and deleted from the UI | human | N/A | Phase 7 |
| anchor-mvp.AC9.2 | Tags can be assigned to and removed from agents | human | N/A | Phase 7 |
| anchor-mvp.AC9.3 | Modules can be assigned to agents or tags from the UI | human | N/A | Phase 7 |
| anchor-mvp.AC9.4 | Effective module set per agent is visible (direct vs tag source) (API layer) | integration | `internal/server/ui_test.go` | Phase 7 |
| anchor-mvp.AC9.4 | Effective module set per agent is visible (UI rendering) | human | N/A | Phase 7 |
| anchor-mvp.AC9.5 | Module list shows all loaded modules with metadata (name, description) | human | N/A | Phase 7 |

## Criteria Requiring Human Verification

The following acceptance criteria involve front-end rendering, visual layout, or interactive UI behavior that cannot be verified through Go backend tests alone. These require manual smoke testing in a browser.

### anchor-mvp.AC7.4: Unhealthy displayed first, stale second, healthy last

**Justification:** The ordering of health groups is implemented in client-side JavaScript (the Dashboard Preact component filters and renders groups in a fixed array order: unhealthy, stale, healthy). The Go API returns agents with their computed health status but does not enforce display order. No backend test can verify DOM rendering order.

**Approach:** Start the server with test data seeded to produce agents in all three health states. Open the dashboard in a browser. Verify visually that the "Unhealthy" group header appears above "Stale," which appears above "Healthy." Repeat with subsets (e.g., no unhealthy agents present) to confirm empty groups are omitted.

### anchor-mvp.AC8.1: Shows hostname, remote IP, OS/arch/distro, last seen time

**Justification:** The `GET /api/agents/{id}` endpoint is tested in `internal/server/ui_test.go` to return all agent fields correctly. The rendering of those fields in the AgentDetail Preact component is pure front-end presentation with no backend analog.

**Approach:** Navigate to an agent detail page (`/#/agents/{id}`) for an agent with known system info. Verify that hostname, remote IP, OS, arch, distro, and last seen time are all displayed and match the expected values.

### anchor-mvp.AC8.2: Shows assigned tags

**Justification:** The API response for agent detail includes tags, tested at the API layer. Rendering tags in the UI is a front-end concern.

**Approach:** Assign one or more tags to an agent via the API or management UI. Navigate to the agent detail page. Verify that all assigned tags are displayed.

### anchor-mvp.AC8.3: Shows per-module latest status with expandable stdout/stderr

**Justification:** The API response for agent detail includes module results, tested at the API layer. The expandable/collapsible stdout/stderr interaction is a Preact component behavior with no backend test surface.

**Approach:** Ensure an agent has at least two module results, one with stdout and one with stderr. Navigate to the agent detail page. Verify each module shows its status badge. Click a module header to expand it and verify stdout/stderr content is visible. Click again to collapse.

### anchor-mvp.AC9.1: Tags can be created and deleted from the UI

**Justification:** The `POST /api/tags` and `DELETE /api/tags/{id}` endpoints are tested in `internal/server/ui_test.go`. The form input, button click handling, list refresh after mutations, and delete confirmation are all front-end behaviors.

**Approach:** Navigate to `/#/tags`. Create a new tag using the form. Verify it appears in the list. Click the delete button. Verify it is removed from the list.

### anchor-mvp.AC9.2: Tags can be assigned to and removed from agents

**Justification:** The `PUT /api/agents/{id}/tags` endpoint is tested at the API layer. The dropdown interaction for selecting tags and the visual feedback of tag assignment/removal on the agent detail page are front-end concerns.

**Approach:** Navigate to an agent detail page. Use the tag dropdown to assign a tag. Verify the tag appears in the agent's tag list. Remove the tag. Verify it is removed.

### anchor-mvp.AC9.3: Modules can be assigned to agents or tags from the UI

**Justification:** The `POST /api/assignments` and `DELETE /api/assignments/{id}` endpoints are tested at the API layer. The UI interaction (dropdown population from loaded modules, assignment creation, removal) is front-end behavior.

**Approach:** Navigate to an agent detail page. Use the module assignment dropdown to assign a module directly. Verify it appears in the effective module set with source "direct." Navigate to a tag detail page. Assign a module to the tag. Verify it appears in the tag's module list.

### anchor-mvp.AC9.4: Effective module set per agent is visible (UI rendering)

**Justification:** The `GET /api/agents/{id}/modules` endpoint is tested in `internal/server/ui_test.go` to return modules with correct source annotations. The visual distinction between "direct" and "tag:name" sources in the UI is a rendering concern.

**Approach:** Assign one module directly to an agent and another via a tag. Navigate to the agent detail page. Verify the effective module list shows both modules with clearly distinguished source labels ("direct" vs. "tag:<name>").

### anchor-mvp.AC9.5: Module list shows all loaded modules with metadata

**Justification:** The `GET /api/modules` endpoint is tested to return module filenames, names, and descriptions. The rendering of this list in the Modules page is a front-end concern.

**Approach:** Place two or more module scripts in the modules directory. Navigate to `/#/modules`. Verify all modules are listed with their display name and description as defined in their `metadata` output.
