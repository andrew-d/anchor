# Anchor MVP Implementation Plan — Phase 2: Database Layer

**Goal:** SQLite database with schema, migrations, and a tested Store implementation covering agents, tags, assignments, and module results.

**Architecture:** `internal/db/` package with a `Store` interface and `SQLiteStore` implementation using `modernc.org/sqlite` (pure Go, no CGO). Schema created on first open. All methods tested against real on-disk SQLite via `t.TempDir()`.

**Tech Stack:** Go 1.26, `modernc.org/sqlite`, `database/sql`

**Scope:** 7 phases from original design (phase 2 of 7)

**Codebase verified:** 2026-03-03 — confirmed greenfield, Phase 1 creates project scaffolding

---

## Acceptance Criteria Coverage

This phase implements and tests:

### anchor-mvp.AC1: Agent Registration
- **anchor-mvp.AC1.1 Success:** First check-in from a new UUID creates an agent record with hostname, OS, arch, distro, remote IP, and last_seen_at
- **anchor-mvp.AC1.2 Success:** Subsequent check-ins from an existing UUID update system info and last_seen_at
- **anchor-mvp.AC1.3 Edge:** Agent with changed hostname (e.g., machine rebuilt, same UUID file) updates correctly

### anchor-mvp.AC3: Tags & Assignment
- **anchor-mvp.AC3.1 Success:** Tags can be created and assigned to agents
- **anchor-mvp.AC3.2 Success:** Modules can be assigned to an individual agent
- **anchor-mvp.AC3.3 Success:** Modules can be assigned to a tag
- **anchor-mvp.AC3.4 Success:** Agent's effective module set is the union of direct assignments + tag-inherited assignments
- **anchor-mvp.AC3.5 Edge:** Module assigned to two tags that both belong to the same agent appears only once in the module set
- **anchor-mvp.AC3.6 Failure:** Assignment with both agent_id and tag_id set is rejected (CHECK constraint)

### anchor-mvp.AC5: Module Result History
- **anchor-mvp.AC5.1 Success:** Every module execution inserts a new row (no upsert)
- **anchor-mvp.AC5.2 Success:** Latest result per agent per module is queryable for dashboard display
- **anchor-mvp.AC5.3 Success:** Full history per agent per module is queryable for detail view

---

<!-- START_TASK_1 -->
### Task 1: Add modernc.org/sqlite dependency

**Files:**
- Modify: `go.mod`

**Step 1: Add the dependency**

Run:
```bash
cd /home/andrew/repos/anchor && go get modernc.org/sqlite
```

**Step 2: Verify**

Run: `go mod tidy`
Expected: `go.mod` and `go.sum` updated with `modernc.org/sqlite` and transitive deps

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add modernc.org/sqlite dependency"
```
<!-- END_TASK_1 -->

<!-- START_SUBCOMPONENT_A (tasks 2-4) -->
<!-- START_TASK_2 -->
### Task 2: Create Store interface and types

**Files:**
- Create: `internal/db/db.go`

**Implementation:**

Define the `Store` interface and all domain types used by the store. Types needed:

```go
package db

import "context"

type Agent struct {
	ID         string
	Hostname   string
	RemoteIP   string
	OS         string
	Arch       string
	Distro     string
	LastSeenAt int64 // Unix timestamp
}

type Tag struct {
	ID   int64
	Name string
}

type ModuleAssignment struct {
	ID         int64
	ModuleName string
	AgentID    *string // nullable
	TagID      *int64  // nullable
}

type ModuleResult struct {
	ID         int64
	AgentID    string
	ModuleName string
	Status     string // "ok", "changed", "error"
	Stdout     string
	Stderr     string
	ExecutedAt int64 // Unix timestamp
}

type Store interface {
	UpsertAgent(ctx context.Context, agent Agent) error
	GetAgent(ctx context.Context, id string) (Agent, error)
	ListAgents(ctx context.Context) ([]Agent, error)

	CreateTag(ctx context.Context, name string) (Tag, error)
	DeleteTag(ctx context.Context, id int64) error
	ListTags(ctx context.Context) ([]Tag, error)

	SetAgentTags(ctx context.Context, agentID string, tagIDs []int64) error
	GetAgentTags(ctx context.Context, agentID string) ([]Tag, error)

	AssignModule(ctx context.Context, moduleName string, agentID *string, tagID *int64) error
	UnassignModule(ctx context.Context, id int64) error
	GetAgentModules(ctx context.Context, agentID string) ([]string, error)

	InsertModuleResult(ctx context.Context, result ModuleResult) error
	GetLatestModuleResults(ctx context.Context, agentID string) ([]ModuleResult, error)
	GetModuleHistory(ctx context.Context, agentID string, moduleName string) ([]ModuleResult, error)
}
```

**Verification:**

Run: `go build ./...`
Expected: Compiles (interface is not yet implemented)

**Commit:** `feat: add Store interface and domain types`
<!-- END_TASK_2 -->

<!-- START_TASK_3 -->
### Task 3: Implement SQLiteStore with schema migration

**Files:**
- Create: `internal/db/sqlite.go`

**Implementation:**

Create `SQLiteStore` that implements `Store`. On `Open()`, it:
1. Opens the SQLite database file using `modernc.org/sqlite` driver (registered as `"sqlite"`)
2. Enables foreign keys: `PRAGMA foreign_keys = ON`
3. Sets WAL journal mode: `PRAGMA journal_mode = WAL`
4. Sets busy timeout: `PRAGMA busy_timeout = 5000`
5. Creates all five STRICT tables if they don't exist

The driver import is:
```go
import _ "modernc.org/sqlite"
```

And opening is:
```go
db, err := sql.Open("sqlite", filepath)
```

Schema (execute in a single `db.ExecContext` call with all statements):

```sql
CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    remote_ip TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    arch TEXT NOT NULL DEFAULT '',
    distro TEXT NOT NULL DEFAULT '',
    last_seen_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
) STRICT;

CREATE TABLE IF NOT EXISTS agent_tags (
    agent_id TEXT NOT NULL REFERENCES agents(id),
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, tag_id)
) STRICT;

CREATE TABLE IF NOT EXISTS module_assignments (
    id INTEGER PRIMARY KEY,
    module_name TEXT NOT NULL,
    agent_id TEXT REFERENCES agents(id),
    tag_id INTEGER REFERENCES tags(id) ON DELETE CASCADE,
    CHECK ((agent_id IS NOT NULL AND tag_id IS NULL) OR (agent_id IS NULL AND tag_id IS NOT NULL))
) STRICT;

CREATE TABLE IF NOT EXISTS module_results (
    id INTEGER PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id),
    module_name TEXT NOT NULL,
    status TEXT NOT NULL,
    stdout TEXT NOT NULL DEFAULT '',
    stderr TEXT NOT NULL DEFAULT '',
    executed_at INTEGER NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_module_results_lookup
    ON module_results (agent_id, module_name, executed_at);
```

Implement all Store interface methods:

- **UpsertAgent:** Use `INSERT ... ON CONFLICT(id) DO UPDATE SET ...` to handle both create and update cases. Set all fields including `last_seen_at`.
- **GetAgent:** `SELECT * FROM agents WHERE id = ?`
- **ListAgents:** `SELECT * FROM agents ORDER BY hostname`
- **CreateTag:** `INSERT INTO tags (name) VALUES (?)`, return the tag with `LastInsertId()`
- **DeleteTag:** `DELETE FROM tags WHERE id = ?` (CASCADE handles agent_tags and module_assignments)
- **ListTags:** `SELECT * FROM tags ORDER BY name`
- **SetAgentTags:** Delete all existing `agent_tags` for this agent, then insert the new set. Use a transaction.
- **GetAgentTags:** `SELECT t.* FROM tags t JOIN agent_tags at ON t.id = at.tag_id WHERE at.agent_id = ? ORDER BY t.name`
- **AssignModule:** `INSERT INTO module_assignments (module_name, agent_id, tag_id) VALUES (?, ?, ?)`. The CHECK constraint enforces exactly-one-of.
- **UnassignModule:** `DELETE FROM module_assignments WHERE id = ?`
- **GetAgentModules:** Union of direct assignments and tag-inherited assignments, deduplicated:
  ```sql
  SELECT DISTINCT module_name FROM module_assignments
  WHERE agent_id = ?
  UNION
  SELECT DISTINCT ma.module_name FROM module_assignments ma
  JOIN agent_tags at ON ma.tag_id = at.tag_id
  WHERE at.agent_id = ?
  ORDER BY module_name
  ```
- **InsertModuleResult:** `INSERT INTO module_results (agent_id, module_name, status, stdout, stderr, executed_at) VALUES (?, ?, ?, ?, ?, ?)`
- **GetLatestModuleResults:** Get the most recent result for each module for an agent:
  ```sql
  SELECT mr.* FROM module_results mr
  INNER JOIN (
      SELECT agent_id, module_name, MAX(executed_at) as max_ts
      FROM module_results
      WHERE agent_id = ?
      GROUP BY agent_id, module_name
  ) latest ON mr.agent_id = latest.agent_id
      AND mr.module_name = latest.module_name
      AND mr.executed_at = latest.max_ts
  ORDER BY mr.module_name
  ```
- **GetModuleHistory:** `SELECT * FROM module_results WHERE agent_id = ? AND module_name = ? ORDER BY executed_at DESC`

Also provide an `Open(filepath string) (*SQLiteStore, error)` constructor and a `Close() error` method.

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: implement SQLiteStore with schema and all Store methods`
<!-- END_TASK_3 -->

<!-- START_TASK_4 -->
### Task 4: Test all Store methods

**Verifies:** anchor-mvp.AC1.1, anchor-mvp.AC1.2, anchor-mvp.AC1.3, anchor-mvp.AC3.1, anchor-mvp.AC3.2, anchor-mvp.AC3.3, anchor-mvp.AC3.4, anchor-mvp.AC3.5, anchor-mvp.AC3.6, anchor-mvp.AC5.1, anchor-mvp.AC5.2, anchor-mvp.AC5.3

**Files:**
- Create: `internal/db/db_test.go`
- Test file: `internal/db/db_test.go` (integration — real SQLite on disk)

**Testing:**

Use `t.TempDir()` for each test's database file. Each test opens a fresh `SQLiteStore`. Use table-driven tests where appropriate (follow Go community conventions from the `go-table-driven-tests` skill).

Tests must verify each AC listed above:

- **anchor-mvp.AC1.1:** UpsertAgent with a new UUID creates the record; GetAgent returns all fields correctly (hostname, OS, arch, distro, remote_ip, last_seen_at)
- **anchor-mvp.AC1.2:** UpsertAgent twice with same UUID but updated last_seen_at; GetAgent returns the updated timestamp and system info
- **anchor-mvp.AC1.3:** UpsertAgent with same UUID but changed hostname; GetAgent returns the new hostname
- **anchor-mvp.AC3.1:** CreateTag succeeds, ListTags returns it; SetAgentTags assigns tag to agent; GetAgentTags returns the tag
- **anchor-mvp.AC3.2:** AssignModule with agent_id set and tag_id nil succeeds; GetAgentModules returns the module
- **anchor-mvp.AC3.3:** AssignModule with tag_id set and agent_id nil succeeds; after setting the tag on an agent, GetAgentModules returns the module
- **anchor-mvp.AC3.4:** Assign module "mod_a" directly to agent, assign module "mod_b" to a tag, set the tag on the agent; GetAgentModules returns both "mod_a" and "mod_b"
- **anchor-mvp.AC3.5:** Create two tags, assign same module "mod_x" to both tags, assign both tags to same agent; GetAgentModules returns "mod_x" exactly once
- **anchor-mvp.AC3.6:** AssignModule with both agent_id and tag_id set returns an error (CHECK constraint violation); also test with both nil (both null also violates the CHECK)
- **anchor-mvp.AC5.1:** InsertModuleResult twice for same agent+module; GetModuleHistory returns 2 rows (not 1)
- **anchor-mvp.AC5.2:** Insert multiple results for different modules; GetLatestModuleResults returns only the most recent per module
- **anchor-mvp.AC5.3:** Insert 3 results for same agent+module; GetModuleHistory returns all 3 in descending executed_at order

Also test:
- DeleteTag cascades: deleting a tag removes it from agent_tags and module_assignments
- SetAgentTags replaces: setting new tag set removes old assignments
- GetAgent with nonexistent ID returns appropriate error
- ListAgents returns agents ordered by hostname

**Verification:**

Run: `go test ./internal/db/ -count=1 -v`
Expected: All tests pass

**Commit:** `test: add comprehensive Store implementation tests`
<!-- END_TASK_4 -->
<!-- END_SUBCOMPONENT_A -->
