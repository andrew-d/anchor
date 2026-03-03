# Anchor MVP Design

## Summary

Anchor is a lightweight configuration management system designed for homelabs and small infrastructure where operational simplicity matters more than enterprise features. It follows a server-agent model: a central server holds module definitions (shell scripts), tracks which modules are assigned to which agents, and presents a health-focused web dashboard. Agents run on managed hosts, check in periodically, receive their assigned scripts, execute them, and report results back. The entire system ships as a single Go binary — `anchor server` and `anchor agent` are subcommands of the same executable.

The implementation is deliberately minimal. Modules are plain shell scripts stored on disk; the server detects additions, changes, and removals between check-ins without any reload mechanism. State is persisted in SQLite using a pure-Go driver that requires no CGO, keeping cross-compilation straightforward. The web UI is a small Preact+HTM single-page application embedded directly in the binary — no build pipeline, no CDN dependencies. The agent is nearly stateless: it holds only a UUID file, gathers system info at startup, and otherwise operates purely in response to what the server provides. This design avoids daemons, sidecars, and persistent local state on managed hosts.

## Definition of Done

- A single Go binary that runs as either server or agent based on subcommand
- **Server:** Serves a health-focused Preact+HTM web UI, exposes an API for agents, stores state in SQLite, reads module definitions (shell scripts) from a config directory
- **Agent:** Generates/persists a UUID, polls the server, syncs assigned modules, executes them periodically, reports results (pass/fail/changed + stdout/stderr)
- **Assignment system:** Modules assignable to individual agents or tags; tags assignable to agents; all managed via the web UI
- **Dashboard:** Agents grouped by health status (healthy/unhealthy/stale), drill-down to module-level detail
- **Explicitly out of scope:** Notifications, authentication, git-sync for modules, any migration from the existing implementation on main

## Acceptance Criteria

### anchor-mvp.AC1: Agent Registration
- **anchor-mvp.AC1.1 Success:** First check-in from a new UUID creates an agent record with hostname, OS, arch, distro, remote IP, and last_seen_at
- **anchor-mvp.AC1.2 Success:** Subsequent check-ins from an existing UUID update system info and last_seen_at
- **anchor-mvp.AC1.3 Edge:** Agent with changed hostname (e.g., machine rebuilt, same UUID file) updates correctly

### anchor-mvp.AC2: Module Sync & Execution
- **anchor-mvp.AC2.1 Success:** Checkin response contains full script content for all assigned modules
- **anchor-mvp.AC2.2 Success:** Agent executes modules in sorted name order (00_base before 10_users)
- **anchor-mvp.AC2.3 Success:** Exit code 0 recorded as "ok", exit code 80 as "changed"
- **anchor-mvp.AC2.4 Success:** Agent reports each module result individually via POST /api/report
- **anchor-mvp.AC2.5 Failure:** Non-zero/non-80 exit code recorded as "error" with captured stderr
- **anchor-mvp.AC2.6 Failure:** If report call fails (network broken), agent stops executing remaining modules and logs the failure

### anchor-mvp.AC3: Tags & Assignment
- **anchor-mvp.AC3.1 Success:** Tags can be created and assigned to agents
- **anchor-mvp.AC3.2 Success:** Modules can be assigned to an individual agent
- **anchor-mvp.AC3.3 Success:** Modules can be assigned to a tag
- **anchor-mvp.AC3.4 Success:** Agent's effective module set is the union of direct assignments + tag-inherited assignments
- **anchor-mvp.AC3.5 Edge:** Module assigned to two tags that both belong to the same agent appears only once in the module set
- **anchor-mvp.AC3.6 Failure:** Assignment with both agent_id and tag_id set is rejected (CHECK constraint)

### anchor-mvp.AC4: Module Loading
- **anchor-mvp.AC4.1 Success:** Server reads all scripts from config directory and extracts metadata
- **anchor-mvp.AC4.2 Success:** Adding a new script file is detected on next check-in without restart
- **anchor-mvp.AC4.3 Success:** Modifying a script file triggers re-parsing of metadata
- **anchor-mvp.AC4.4 Success:** Removing a script file drops it from the available module set
- **anchor-mvp.AC4.5 Success:** Unchanged files reuse cached metadata (hash comparison)

### anchor-mvp.AC5: Module Result History
- **anchor-mvp.AC5.1 Success:** Every module execution inserts a new row (no upsert)
- **anchor-mvp.AC5.2 Success:** Latest result per agent per module is queryable for dashboard display
- **anchor-mvp.AC5.3 Success:** Full history per agent per module is queryable for detail view

### anchor-mvp.AC6: Agent Identity & System Info
- **anchor-mvp.AC6.1 Success:** Agent generates a UUID and persists it to a file on first run
- **anchor-mvp.AC6.2 Success:** Agent reads existing UUID from file on subsequent runs
- **anchor-mvp.AC6.3 Success:** Linux agent detects OS, arch, and distro from /etc/os-release
- **anchor-mvp.AC6.4 Success:** macOS agent detects OS, arch, and version from sw_vers

### anchor-mvp.AC7: Health Dashboard
- **anchor-mvp.AC7.1 Success:** Agents with modules in "error" status appear in the "unhealthy" group
- **anchor-mvp.AC7.2 Success:** Agents not seen within 2× polling interval appear in the "stale" group
- **anchor-mvp.AC7.3 Success:** Agents with all modules "ok" or "changed" and recent check-in appear in "healthy" group
- **anchor-mvp.AC7.4 Success:** Unhealthy displayed first, stale second, healthy last

### anchor-mvp.AC8: Agent Detail View
- **anchor-mvp.AC8.1 Success:** Shows hostname, remote IP, OS/arch/distro, last seen time
- **anchor-mvp.AC8.2 Success:** Shows assigned tags
- **anchor-mvp.AC8.3 Success:** Shows per-module latest status with expandable stdout/stderr

### anchor-mvp.AC9: Management UI
- **anchor-mvp.AC9.1 Success:** Tags can be created and deleted from the UI
- **anchor-mvp.AC9.2 Success:** Tags can be assigned to and removed from agents
- **anchor-mvp.AC9.3 Success:** Modules can be assigned to agents or tags from the UI
- **anchor-mvp.AC9.4 Success:** Effective module set per agent is visible (showing which come from direct assignment vs tags)
- **anchor-mvp.AC9.5 Success:** Module list shows all loaded modules with metadata (name, description)

## Glossary

- **Agent**: The anchor process running on a managed host. Checks in with the server, receives and executes assigned modules, and reports results. Nearly stateless — persists only a UUID file.
- **Module**: A shell script stored in the server's config directory. Implements two subcommands: `metadata` (returns name and description as JSON) and `apply` (applies configuration idempotently and signals whether anything changed via exit code).
- **Check-in**: The periodic `POST /api/checkin` call an agent makes to the server. Sends system info, receives the full list of assigned module scripts and the polling interval.
- **Module assignment**: A record linking a module name to either an individual agent or a tag. An agent's effective module set is the union of its direct assignments and all assignments belonging to its tags.
- **Tag**: A label that can be applied to one or more agents. Modules can be assigned to a tag, causing all agents with that tag to inherit those modules.
- **Effective module set**: The deduplicated union of modules an agent receives — combining direct assignments and tag-inherited assignments.
- **Health status**: A per-agent classification computed at display time. "Healthy" means recently checked in with no errored modules; "unhealthy" means recently checked in but one or more modules returned an error; "stale" means the agent has not checked in within 2× the polling interval.
- **Polling interval**: The number of seconds between agent check-ins, returned by the server in the checkin response. Allows the server to control check-in frequency without reconfiguring agents.
- **Store interface**: The Go interface (`internal/db`) that abstracts all SQLite operations. Server handlers depend on this interface; tests use the real SQLite implementation against temporary on-disk databases.
- **`modernc.org/sqlite`**: A pure-Go SQLite driver that requires no CGO, enabling straightforward cross-compilation to Linux and macOS on amd64 and arm64.
- **Preact + HTM**: A lightweight JavaScript UI library (Preact) paired with a tagged-template syntax (HTM) that allows writing JSX-like component trees without a compile step. Vendored directly into the binary via `embed.FS`.
- **`embed.FS`**: A Go standard library feature that bundles static files (HTML, JS, CSS) into the compiled binary at build time, eliminating runtime file system dependencies.
- **Hash-based routing**: A client-side navigation pattern where the URL fragment (`/#/path`) encodes the current view. The browser-side JavaScript reads the fragment to decide what to render. Supports browser back/forward without server round-trips.
- **STRICT tables**: A SQLite table mode that enforces declared column types strictly, rejecting values that do not conform. Prevents implicit type coercion bugs.
- **`//go:build` constraints**: Go source file directives that restrict compilation to specific platforms (e.g., `//go:build unix`). Used to isolate Linux-specific (`/etc/os-release`) and macOS-specific (`sw_vers`) system info gathering.
- **Idempotent**: A property of an operation meaning it can be applied multiple times without changing the outcome beyond the first application. Module `apply` scripts are expected to be idempotent.
- **Exit code 80**: The anchor convention for a module signaling that it made a change during `apply`. Chosen to avoid collision with standard shell exit codes (1, 2, 126, 127, 128+).

## Architecture

Single Go binary with two subcommands: `anchor server` and `anchor agent`. Both compile into one binary using stdlib `flag.FlagSet` for CLI parsing.

### Server

HTTP server using `net/http.ServeMux` with three route groups:

- **Agent API** (`POST /api/checkin`, `POST /api/report`) — JSON over HTTP. Agents check in with system info, receive their full module set. After executing each module, agents report results individually.
- **UI API** (`GET/POST /api/agents`, `/api/tags`, `/api/assignments`, etc.) — CRUD endpoints consumed by the web dashboard.
- **Static files** (`GET /`) — Serves the embedded Preact+HTM SPA from `embed.FS`.

State stored in SQLite via `modernc.org/sqlite` (pure Go, no CGO — enables trivial cross-compilation to Linux and macOS on amd64/arm64).

**Module loading** is dynamic: on every agent check-in, the server reads the modules config directory, SHA-256 hashes each file, and compares against an in-memory cache. Changed or new files get their `metadata` command executed to extract module info. Removed files are dropped from cache. No restart or reload signal needed — `rsync` a file in and the next check-in picks it up.

**Module resolution** for an agent: union of modules directly assigned to the agent + modules assigned to any of the agent's tags.

**Health classification:**
- **Healthy** — checked in within 2× polling interval, no modules in "error" status
- **Unhealthy** — checked in recently, but one or more modules in "error" status
- **Stale** — has not checked in within 2× polling interval

### Agent

Stateless beyond a persisted UUID file (e.g., `/var/lib/anchor/agent-id`). On startup, reads or generates the UUID, gathers system info (hostname, OS, arch, distro), then enters the main loop:

1. `POST /api/checkin` with UUID + system info → receives assigned modules (name + full script content) and polling interval
2. For each module in **sorted name order** (e.g., `00_base` before `10_users`):
   - Write script to a temp file, chmod +x
   - Execute with `apply` argument, capture stdout/stderr, check exit code
   - `POST /api/report` with this module's result (status, stdout, stderr)
   - If report fails (network broken by a prior module), log and stop
3. Sleep for polling interval, repeat

System info gathering uses `//go:build` constraints: `/etc/os-release` on Linux, `sw_vers` on macOS.

### Web UI

Single-page application using vendored Preact + HTM — no build step, no external network requests. All JS and CSS embedded in the Go binary via `embed.FS`.

Hash-based routing (`/#/`, `/#/agents/{id}`, `/#/tags`, etc.) for browser history support.

**Dashboard** (main view) groups agents by health status: unhealthy first (most prominent), stale second, healthy last (collapsed or minimal). Designed for "glance and know if something needs attention."

**Agent detail** view shows hostname, remote IP, OS/arch/distro, last seen time, assigned tags, and per-module results with expandable stdout/stderr.

**Management views** for tag CRUD, module-to-agent assignment, module-to-tag assignment, and a read-only module list showing metadata from the config directory.

### Data Model

Six STRICT SQLite tables:

**`agents`** — `id` TEXT PK (UUID), `hostname` TEXT, `remote_ip` TEXT, `os` TEXT, `arch` TEXT, `distro` TEXT, `last_seen_at` INTEGER (Unix timestamp).

**`tags`** — `id` INTEGER PK, `name` TEXT UNIQUE.

**`agent_tags`** — `agent_id` TEXT, `tag_id` INTEGER, PK (agent_id, tag_id). Foreign keys to agents and tags.

**`module_assignments`** — `id` INTEGER PK, `module_name` TEXT, `agent_id` TEXT (nullable), `tag_id` INTEGER (nullable). CHECK constraint: exactly one of agent_id/tag_id is non-null.

**`module_results`** — `id` INTEGER PK (auto-increment, stores full history), `agent_id` TEXT, `module_name` TEXT, `status` TEXT ("ok", "changed", "error"), `stdout` TEXT, `stderr` TEXT, `executed_at` INTEGER (Unix timestamp). Index on `(agent_id, module_name, executed_at)`.

### Agent-Server Protocol

**`POST /api/checkin`**

Request:
```json
{
  "id": "agent-uuid",
  "hostname": "web-01",
  "os": "linux",
  "arch": "amd64",
  "distro": "ubuntu 24.04"
}
```

Response:
```json
{
  "poll_interval_seconds": 300,
  "modules": [
    {"name": "00_base", "script": "#!/bin/bash\n..."},
    {"name": "10_users", "script": "#!/bin/bash\n..."}
  ]
}
```

Server side-effects: upserts agent row, records `last_seen_at` and `remote_ip` from the HTTP request.

**`POST /api/report`**

Request:
```json
{
  "agent_id": "agent-uuid",
  "module_name": "00_base",
  "status": "ok",
  "stdout": "...",
  "stderr": "",
  "executed_at": 1709472000
}
```

Response:
```json
{"ok": true}
```

Server side-effect: inserts a row into `module_results`.

### Module Script Interface

Each module is a shell script in the server's config directory (default `/etc/anchor/modules.d/`). Scripts accept one argument:

- **`metadata`** — prints JSON to stdout: `{"name": "Module Name", "description": "What this module does"}`. Exit code 0.
- **`apply`** — applies configuration idempotently. Exit code 0 = no change ("ok"), exit code 80 = change applied ("changed"), any other nonzero = error.

### Store Interface

```go
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

This interface is the testability boundary — server handlers depend on it, tests use the real SQLite implementation against on-disk databases in `t.TempDir()`.

## Existing Patterns

This is a greenfield project on a new branch (`andrew/simpler`). No existing code patterns to follow.

Conventions established in CLAUDE.md that this design follows:
- Library packages never call `os.Exit()` — `cmd/anchor/main.go` is a thin wrapper
- `log/slog` for structured logging with static messages and `snake_case` keys
- `//go:build` constraints for platform-specific code
- On-disk SQLite only (no in-memory), `t.TempDir()` in tests

## Implementation Phases

<!-- START_PHASE_1 -->
### Phase 1: Project Scaffolding
**Goal:** Buildable Go project with CLI subcommands that route to server and agent entry points.

**Components:**
- `go.mod` with module path and Go version
- `cmd/anchor/main.go` — parses `server` and `agent` subcommands with `flag.FlagSet`, calls into library packages
- `internal/server/server.go` — stub `Server` struct with `Run()` that starts an HTTP server and logs
- `internal/agent/agent.go` — stub `Agent` struct with `Run()` that logs and exits

**Dependencies:** None

**Done when:** `go build ./...` produces a binary, `./anchor server --port 8080` starts and listens, `./anchor agent --server http://localhost:8080` logs and exits, `go vet ./...` passes
<!-- END_PHASE_1 -->

<!-- START_PHASE_2 -->
### Phase 2: Database Layer
**Goal:** SQLite database with schema, migrations, and a tested Store implementation.

**Components:**
- `internal/db/db.go` — `Store` interface definition, `SQLiteStore` struct implementing it, schema migration on open
- `internal/db/db_test.go` — tests for all Store methods against real on-disk SQLite in `t.TempDir()`
- All six STRICT tables with foreign keys and CHECK constraints

**Dependencies:** Phase 1

**Covers:** anchor-mvp.AC1 (agent registration/upsert), anchor-mvp.AC3 (tag and assignment storage), anchor-mvp.AC5 (module result history storage)

**Done when:** All Store interface methods implemented and tested — agents can be upserted/listed, tags CRUD works, module assignments respect the exactly-one-of constraint, module results accumulate as history
<!-- END_PHASE_2 -->

<!-- START_PHASE_3 -->
### Phase 3: Module Loading
**Goal:** Server can read module scripts from a config directory, hash them, cache metadata, and detect changes.

**Components:**
- `internal/module/module.go` — `Loader` struct that scans a directory, hashes files, shells out to `metadata`, caches results. Re-reads on each call, only re-parses changed files.
- `internal/module/module_test.go` — tests with temp directories containing sample scripts

**Dependencies:** Phase 1

**Covers:** anchor-mvp.AC4 (module loading and change detection)

**Done when:** Loader reads a directory of scripts, extracts metadata, detects file additions/changes/removals between calls, caches unchanged modules
<!-- END_PHASE_3 -->

<!-- START_PHASE_4 -->
### Phase 4: Server & Agent API
**Goal:** Working server with checkin and report endpoints. Agents can check in, receive modules, and report results.

**Components:**
- `internal/server/server.go` — wires up routes, owns Store and Loader, starts HTTP server
- `internal/server/api.go` — `POST /api/checkin` handler (upserts agent, resolves module set via Store + Loader, returns scripts), `POST /api/report` handler (inserts module result)
- Integration tests: stand up server with real SQLite + temp module directory, hit endpoints with HTTP client

**Dependencies:** Phase 2, Phase 3

**Covers:** anchor-mvp.AC1 (checkin upserts agent with system info and remote IP), anchor-mvp.AC2 (checkin returns full module set, report stores results), anchor-mvp.AC3 (module resolution respects direct + tag assignments)

**Done when:** An HTTP client can POST checkin and receive modules, POST report and have results stored, module resolution correctly unions direct and tag-based assignments
<!-- END_PHASE_4 -->

<!-- START_PHASE_5 -->
### Phase 5: Agent
**Goal:** Functional agent that checks in, executes modules, and reports results per-module.

**Components:**
- `internal/agent/agent.go` — `Agent` struct with `Run()`: UUID management (read/generate/persist), system info gathering, polling loop
- `internal/agent/runner.go` — module execution: write script to temp file, execute with `apply`, capture stdout/stderr, determine status from exit code
- Platform-specific system info files with `//go:build` constraints (Linux: `/etc/os-release`, macOS: `sw_vers`)

**Dependencies:** Phase 4 (needs server to talk to)

**Covers:** anchor-mvp.AC2 (agent executes modules in sorted order, reports per-module), anchor-mvp.AC6 (agent UUID persistence, system info gathering)

**Done when:** Agent generates/persists UUID, gathers system info, checks in with server, receives modules, executes them in sorted order, reports each result individually, sleeps and repeats. If report fails mid-execution, agent logs and stops remaining modules.
<!-- END_PHASE_5 -->

<!-- START_PHASE_6 -->
### Phase 6: Web UI & Dashboard
**Goal:** Embedded SPA with health-focused dashboard and agent detail view.

**Components:**
- `static/index.html` — entry point, loads vendored Preact + HTM
- `static/vendor/` — vendored `preact.min.js`, `htm.min.js`
- `static/app.js` — SPA shell with hash-based routing (`/#/`, `/#/agents/{id}`)
- Dashboard component: agents grouped by unhealthy → stale → healthy
- Agent detail component: system info, tags, per-module results with expandable stdout/stderr
- `internal/server/server.go` — wire up `embed.FS` to serve static files
- `internal/server/ui.go` — UI-facing API endpoints: `GET /api/agents` (with health status), `GET /api/agents/{id}` (detail with latest results)

**Dependencies:** Phase 4

**Covers:** anchor-mvp.AC7 (health-focused dashboard grouping), anchor-mvp.AC8 (agent detail with module results)

**Done when:** Dashboard loads in browser, agents grouped by health status, clicking an agent shows detail view with system info and module results, browser back/forward works via hash routing
<!-- END_PHASE_6 -->

<!-- START_PHASE_7 -->
### Phase 7: Management UI
**Goal:** Web UI for managing tags, assigning modules to agents and tags.

**Components:**
- Tag management component: create/delete tags, assign/remove tags on agents
- Module assignment component: assign modules to individual agents or tags, view effective module set per agent
- Module list component: read-only list of modules with metadata from config directory
- `internal/server/ui.go` — additional UI API endpoints: CRUD for tags, assignments, `GET /api/modules` (loaded modules list)

**Dependencies:** Phase 6

**Covers:** anchor-mvp.AC3 (tag and module assignment management via UI), anchor-mvp.AC9 (effective module set visibility)

**Done when:** Tags can be created/deleted/assigned in UI, modules can be assigned to agents or tags, effective module set per agent is visible, module list shows metadata
<!-- END_PHASE_7 -->

## Additional Considerations

**Exit code convention:** Exit code 80 for "changed" is arbitrary but avoids collision with common shell exit codes (1=general error, 2=misuse, 126=not executable, 127=not found, 128+=signals). This convention must be documented for module authors.

**No graceful degradation on agent:** If the server is unreachable, the agent simply retries on the next poll cycle. No local caching, no offline execution. Modules only run when the server provides them. This keeps the agent truly stateless.

**Future extensibility considered but not implemented:** The polling interval returned by the server to the agent enables future server-side control of check-in frequency without agent reconfiguration. The module result history table enables future analytics or alerting without schema changes.
