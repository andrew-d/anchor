# Anchor MVP Implementation Plan — Phase 6: Web UI & Dashboard

**Goal:** Embedded SPA with health-focused dashboard and agent detail view, served from Go binary via `embed.FS`.

**Architecture:** Preact + HTM vendored locally (no build step, no CDN). Hash-based routing for SPA navigation. Go `embed.FS` bundles static files into the binary. UI-facing API endpoints serve agent data with computed health status.

**Tech Stack:** Go 1.26 (`embed`, `net/http`), Preact 10.x, HTM 3.x, vanilla CSS

**Scope:** 7 phases from original design (phase 6 of 7)

**Codebase verified:** 2026-03-03 — confirmed greenfield. Phase 4 creates server with API routes. Research confirms Preact 10.28.x and HTM 3.1.1 are current stable.

**External dependency findings:**
- Preact 10.28.4 stable, HTM 3.1.1 stable
- Use import maps for bare specifier resolution (`"preact"` → `/static/vendor/preact.js`)
- Download ESM builds from esm.sh or unpkg with `?raw` for vendoring
- No build pipeline needed — `htm.bind(h)` creates the `html` tagged template function

---

## Acceptance Criteria Coverage

This phase implements and tests:

### anchor-mvp.AC7: Health Dashboard
- **anchor-mvp.AC7.1 Success:** Agents with modules in "error" status appear in the "unhealthy" group
- **anchor-mvp.AC7.2 Success:** Agents not seen within 2x polling interval appear in the "stale" group
- **anchor-mvp.AC7.3 Success:** Agents with all modules "ok" or "changed" and recent check-in appear in "healthy" group
- **anchor-mvp.AC7.4 Success:** Unhealthy displayed first, stale second, healthy last

### anchor-mvp.AC8: Agent Detail View
- **anchor-mvp.AC8.1 Success:** Shows hostname, remote IP, OS/arch/distro, last seen time
- **anchor-mvp.AC8.2 Success:** Shows assigned tags
- **anchor-mvp.AC8.3 Success:** Shows per-module latest status with expandable stdout/stderr

---

<!-- START_TASK_1 -->
### Task 1: Vendor Preact and HTM

**Files:**
- Create: `static/vendor/preact.js`
- Create: `static/vendor/preact-hooks.js`
- Create: `static/vendor/htm.js`

**Step 1: Download vendor files**

Download the ESM module files for local serving. Use esm.sh with `?raw` to get the actual source (not a redirect):

```bash
mkdir -p static/vendor
curl -sL 'https://esm.sh/preact@10.25.4?raw' -o static/vendor/preact.js
curl -sL 'https://esm.sh/preact@10.25.4/hooks?raw' -o static/vendor/preact-hooks.js
curl -sL 'https://esm.sh/htm@3.1.1?raw' -o static/vendor/htm.js
```

Note: If esm.sh URLs return redirects or wrapper modules, try alternative URLs from unpkg:
```bash
curl -sL 'https://unpkg.com/preact@10.25.4/dist/preact.module.js' -o static/vendor/preact.js
curl -sL 'https://unpkg.com/preact@10.25.4/hooks/dist/hooks.module.js' -o static/vendor/preact-hooks.js
curl -sL 'https://unpkg.com/htm@3.1.1/dist/htm.module.js' -o static/vendor/htm.js
```

**Step 2: Verify files are valid JavaScript**

Each file should be non-empty and contain ESM export syntax. Check with:
```bash
head -5 static/vendor/preact.js
head -5 static/vendor/htm.js
```

If the downloaded files contain import statements referencing external URLs (like `https://esm.sh/...`), they need to be rewritten to use local paths. The import map in `index.html` (Task 2) handles bare specifier mapping, but if vendor files import from each other using full URLs, those imports must be changed to bare specifiers (e.g., `"preact"` instead of `"https://esm.sh/preact@10.25.4"`).

**Step 3: Commit**

```bash
git add static/vendor/
git commit -m "chore: vendor Preact 10.25.4 and HTM 3.1.1"
```
<!-- END_TASK_1 -->

<!-- START_TASK_2 -->
### Task 2: Create index.html with import map and SPA shell

**Files:**
- Create: `static/index.html`

**Implementation:**

Create the main HTML entry point. Uses an import map to resolve bare module specifiers to vendored local files:

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>Anchor</title>
    <link rel="stylesheet" href="/static/style.css">
</head>
<body>
    <div id="app"></div>

    <script type="importmap">
    {
        "imports": {
            "preact": "/static/vendor/preact.js",
            "preact/hooks": "/static/vendor/preact-hooks.js",
            "htm": "/static/vendor/htm.js"
        }
    }
    </script>

    <script type="module" src="/static/app.js"></script>
</body>
</html>
```

**Verification:**

File exists and is valid HTML.

**Commit:** `feat: add index.html with import map for vendored Preact/HTM`
<!-- END_TASK_2 -->

<!-- START_TASK_3 -->
### Task 3: Create app.js with hash-based routing

**Files:**
- Create: `static/app.js`

**Implementation:**

Create the SPA shell with hash-based routing. Structure:

```javascript
import { h, render } from 'preact';
import { useState, useEffect } from 'preact/hooks';
import htm from 'htm';

const html = htm.bind(h);

// --- Router ---

function useHashRoute() {
    const [route, setRoute] = useState(window.location.hash.slice(1) || '/');

    useEffect(() => {
        const onHashChange = () => setRoute(window.location.hash.slice(1) || '/');
        window.addEventListener('hashchange', onHashChange);
        return () => window.removeEventListener('hashchange', onHashChange);
    }, []);

    return route;
}

// --- API helpers ---

async function fetchJSON(url) {
    const res = await fetch(url);
    if (!res.ok) throw new Error(`${res.status} ${res.statusText}`);
    return res.json();
}

// --- Components (Dashboard, AgentDetail) ---
// Implemented in subsequent tasks, imported here

// --- App Shell ---

function App() {
    const route = useHashRoute();

    // Route matching
    if (route.startsWith('/agents/')) {
        const id = route.slice('/agents/'.length);
        return html`<${AgentDetail} id=${id} />`;
    }
    return html`<${Dashboard} />`;
}

render(html`<${App} />`, document.getElementById('app'));
```

The Dashboard and AgentDetail components will be defined in this same file (Tasks 5 and 6). Keep everything in one file for simplicity — the SPA is small enough not to need code splitting.

**Verification:**

File exists and is valid JavaScript with ESM imports.

**Commit:** `feat: add SPA shell with hash-based routing`
<!-- END_TASK_3 -->

<!-- START_TASK_4 -->
### Task 4: Create style.css

**Files:**
- Create: `static/style.css`

**Implementation:**

Create minimal, functional CSS for the dashboard. Focus on readability and health status visibility:

```css
* { box-sizing: border-box; margin: 0; padding: 0; }

body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', system-ui, sans-serif;
    background: #f5f5f5;
    color: #333;
    line-height: 1.5;
}

.container { max-width: 960px; margin: 0 auto; padding: 1rem; }

header {
    background: #1a1a2e;
    color: white;
    padding: 0.75rem 1rem;
}
header h1 { font-size: 1.25rem; font-weight: 600; }
header a { color: white; text-decoration: none; }

/* Health status groups */
.status-group { margin-bottom: 1.5rem; }
.status-group h2 {
    font-size: 1rem;
    padding: 0.5rem 0.75rem;
    border-radius: 4px 4px 0 0;
}
.group-unhealthy h2 { background: #fee2e2; color: #991b1b; }
.group-stale h2 { background: #fef3c7; color: #92400e; }
.group-healthy h2 { background: #dcfce7; color: #166534; }

/* Agent cards */
.agent-card {
    background: white;
    border: 1px solid #e5e7eb;
    padding: 0.75rem;
    display: flex;
    justify-content: space-between;
    align-items: center;
}
.agent-card:not(:last-child) { border-bottom: none; }
.agent-card a { color: #2563eb; text-decoration: none; }
.agent-card a:hover { text-decoration: underline; }

/* Agent detail */
.agent-info { background: white; padding: 1rem; border: 1px solid #e5e7eb; margin-bottom: 1rem; border-radius: 4px; }
.agent-info dt { font-weight: 600; color: #6b7280; font-size: 0.875rem; }
.agent-info dd { margin-bottom: 0.5rem; }

/* Module results */
.module-result {
    background: white;
    border: 1px solid #e5e7eb;
    margin-bottom: 0.5rem;
    border-radius: 4px;
}
.module-result-header {
    padding: 0.5rem 0.75rem;
    cursor: pointer;
    display: flex;
    justify-content: space-between;
    align-items: center;
}
.module-result-header:hover { background: #f9fafb; }
.module-output {
    padding: 0.75rem;
    border-top: 1px solid #e5e7eb;
    background: #f9fafb;
}
.module-output pre {
    font-size: 0.8125rem;
    white-space: pre-wrap;
    word-break: break-all;
    font-family: 'SF Mono', Monaco, 'Cascadia Code', monospace;
}

/* Status badges */
.badge {
    display: inline-block;
    padding: 0.125rem 0.5rem;
    border-radius: 9999px;
    font-size: 0.75rem;
    font-weight: 600;
}
.badge-ok { background: #dcfce7; color: #166534; }
.badge-changed { background: #dbeafe; color: #1e40af; }
.badge-error { background: #fee2e2; color: #991b1b; }

/* Tags */
.tag {
    display: inline-block;
    padding: 0.125rem 0.5rem;
    background: #e5e7eb;
    border-radius: 4px;
    font-size: 0.75rem;
    margin-right: 0.25rem;
}
```

**Commit:** `feat: add dashboard CSS`
<!-- END_TASK_4 -->

<!-- START_TASK_5 -->
### Task 5: Implement UI API endpoints

**Files:**
- Create: `internal/server/ui.go`

**Implementation:**

Add UI-facing API endpoints to the server. These are consumed by the JavaScript SPA.

**`GET /api/agents`** — Returns all agents with computed health status.

Health classification logic (per design):
- **Unhealthy:** Agent has checked in recently (within 2x poll interval) AND has any module result with status "error"
- **Stale:** Agent has NOT checked in within 2x poll interval
- **Healthy:** Agent has checked in recently AND all module results are "ok" or "changed" (or no module results)

The poll interval is stored on the server (default 300s). `2 * pollInterval` is the staleness threshold.

Response format:
```json
{
    "poll_interval_seconds": 300,
    "agents": [
        {
            "id": "uuid",
            "hostname": "web-01",
            "remote_ip": "10.0.0.5",
            "os": "linux",
            "arch": "amd64",
            "distro": "ubuntu 24.04",
            "last_seen_at": 1709472000,
            "health": "healthy",
            "module_count": 3,
            "error_count": 0,
            "tags": [{"id": 1, "name": "webservers"}]
        }
    ]
}
```

To compute health: for each agent, call `GetLatestModuleResults`, check for any "error" status, and check `last_seen_at` against `time.Now().Unix() - 2*pollInterval`.

**`GET /api/agents/{id}`** — Returns agent detail with tags and latest module results.

Response format:
```json
{
    "agent": {
        "id": "uuid",
        "hostname": "web-01",
        "remote_ip": "10.0.0.5",
        "os": "linux",
        "arch": "amd64",
        "distro": "ubuntu 24.04",
        "last_seen_at": 1709472000,
        "health": "healthy"
    },
    "tags": [{"id": 1, "name": "webservers"}],
    "module_results": [
        {
            "module_name": "00_base",
            "status": "ok",
            "stdout": "...",
            "stderr": "",
            "executed_at": 1709472000
        }
    ]
}
```

Register these routes in `server.go`'s route setup:
- `GET /api/agents` → `s.handleListAgents`
- `GET /api/agents/{id}` → `s.handleGetAgent` (use Go 1.22+ path parameter: `r.PathValue("id")`)

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: add UI API endpoints for agents with health status`
<!-- END_TASK_5 -->

<!-- START_TASK_6 -->
### Task 6: Test UI API endpoints

**Verifies:** anchor-mvp.AC7.1, anchor-mvp.AC7.2, anchor-mvp.AC7.3

**Files:**
- Create: `internal/server/ui_test.go`
- Test file: `internal/server/ui_test.go` (integration — real SQLite + HTTP)

**Testing:**

Set up test server with real SQLite store. Seed data by calling store methods directly.

- **anchor-mvp.AC7.1:** Insert an agent with recent `last_seen_at`. Insert a module result with status "error". GET `/api/agents`. Verify the agent has `health: "unhealthy"`.
- **anchor-mvp.AC7.2:** Insert an agent with `last_seen_at` older than 2x poll interval. GET `/api/agents`. Verify the agent has `health: "stale"`.
- **anchor-mvp.AC7.3:** Insert an agent with recent `last_seen_at`. Insert module results with status "ok" and "changed". GET `/api/agents`. Verify the agent has `health: "healthy"`.

Also test:
- Agent with no module results and recent checkin is "healthy"
- GET `/api/agents/{id}` returns correct agent detail with tags and module results
- GET `/api/agents/{nonexistent}` returns 404

**Verification:**

Run: `go test ./internal/server/ -count=1 -v`
Expected: All tests pass

**Commit:** `test: add UI API endpoint tests for health classification`
<!-- END_TASK_6 -->

<!-- START_TASK_7 -->
### Task 7: Implement Dashboard component

**Files:**
- Modify: `static/app.js`

**Implementation:**

Add the Dashboard component to `app.js`. It:

1. Fetches `GET /api/agents` on mount
2. Groups agents by health status: unhealthy, stale, healthy (AC7.4 ordering)
3. Renders each group with a colored header
4. Each agent card shows hostname, last seen (relative time), module count, error count
5. Hostname links to `/#/agents/{id}`

```javascript
function Dashboard() {
    const [data, setData] = useState(null);
    const [error, setError] = useState(null);

    useEffect(() => {
        fetchJSON('/api/agents')
            .then(setData)
            .catch(e => setError(e.message));
    }, []);

    if (error) return html`<div class="container"><p>Error: ${error}</p></div>`;
    if (!data) return html`<div class="container"><p>Loading...</p></div>`;

    const groups = [
        { key: 'unhealthy', label: 'Unhealthy', agents: data.agents.filter(a => a.health === 'unhealthy') },
        { key: 'stale', label: 'Stale', agents: data.agents.filter(a => a.health === 'stale') },
        { key: 'healthy', label: 'Healthy', agents: data.agents.filter(a => a.health === 'healthy') },
    ];

    return html`
        <div class="container">
            ${groups.filter(g => g.agents.length > 0).map(g => html`
                <div class="status-group group-${g.key}">
                    <h2>${g.label} (${g.agents.length})</h2>
                    ${g.agents.map(a => html`
                        <div class="agent-card">
                            <div>
                                <a href="#/agents/${a.id}">${a.hostname}</a>
                                ${' '}
                                ${a.tags.map(t => html`<span class="tag">${t.name}</span>`)}
                            </div>
                            <div>
                                <span>${a.module_count} modules</span>
                                ${a.error_count > 0 && html`<span class="badge badge-error">${a.error_count} errors</span>`}
                            </div>
                        </div>
                    `)}
                </div>
            `)}
        </div>
    `;
}
```

**Commit:** `feat: add Dashboard component with health grouping`
<!-- END_TASK_7 -->

<!-- START_TASK_8 -->
### Task 8: Implement AgentDetail component

**Files:**
- Modify: `static/app.js`

**Implementation:**

Add the AgentDetail component. It:

1. Fetches `GET /api/agents/{id}` on mount
2. Displays agent system info: hostname, remote IP, OS/arch/distro, last seen (AC8.1)
3. Displays assigned tags (AC8.2)
4. Lists per-module latest results with expandable stdout/stderr (AC8.3)

Module results should show a status badge and be clickable to expand/collapse the stdout/stderr output.

```javascript
function AgentDetail({ id }) {
    const [data, setData] = useState(null);
    const [error, setError] = useState(null);
    const [expanded, setExpanded] = useState({});

    useEffect(() => {
        fetchJSON(`/api/agents/${id}`)
            .then(setData)
            .catch(e => setError(e.message));
    }, [id]);

    const toggleExpand = (name) => {
        setExpanded(prev => ({ ...prev, [name]: !prev[name] }));
    };

    if (error) return html`<div class="container"><p>Error: ${error}</p></div>`;
    if (!data) return html`<div class="container"><p>Loading...</p></div>`;

    const { agent, tags, module_results } = data;

    return html`
        <div class="container">
            <p><a href="#/">← Dashboard</a></p>
            <h2>${agent.hostname}</h2>

            <dl class="agent-info">
                <dt>Remote IP</dt><dd>${agent.remote_ip}</dd>
                <dt>OS / Arch</dt><dd>${agent.os} / ${agent.arch}</dd>
                <dt>Distro</dt><dd>${agent.distro}</dd>
                <dt>Last Seen</dt><dd>${new Date(agent.last_seen_at * 1000).toLocaleString()}</dd>
                <dt>Tags</dt><dd>${tags.length > 0 ? tags.map(t => html`<span class="tag">${t.name}</span>`) : 'None'}</dd>
            </dl>

            <h3>Modules</h3>
            ${module_results.map(mr => html`
                <div class="module-result">
                    <div class="module-result-header" onClick=${() => toggleExpand(mr.module_name)}>
                        <span>${mr.module_name}</span>
                        <span class="badge badge-${mr.status}">${mr.status}</span>
                    </div>
                    ${expanded[mr.module_name] && html`
                        <div class="module-output">
                            ${mr.stdout && html`<div><strong>stdout:</strong><pre>${mr.stdout}</pre></div>`}
                            ${mr.stderr && html`<div><strong>stderr:</strong><pre>${mr.stderr}</pre></div>`}
                        </div>
                    `}
                </div>
            `)}
        </div>
    `;
}
```

**Commit:** `feat: add AgentDetail component with expandable module results`
<!-- END_TASK_8 -->

<!-- START_TASK_9 -->
### Task 9: Wire up embed.FS to serve static files

**Files:**
- Create: `internal/server/static.go`
- Modify: `internal/server/server.go`

**Implementation:**

Create `static.go` in the server package to embed the static directory:

```go
package server

import "embed"

//go:embed all:static
var staticFS embed.FS
```

Wait — the `static/` directory is at the repo root, not inside `internal/server/`. Go's `embed` directive can only embed files in the same package directory or subdirectories. Two options:

**Option A (recommended):** Move the embed directive to a top-level package. Create `static/embed.go`:
```go
package static

import "embed"

//go:embed *
var FS embed.FS
```

Then in the server, import this package and serve from it.

**Option B:** Keep static files as a field on the Server, passed in from main.go.

Go with Option A. Create `static/embed.go` with the embed directive. The server imports `github.com/andrew-d/anchor/static` and serves files from `static.FS`.

In `server.go`, update route registration:

```go
import (
    "io/fs"
    anchostatic "github.com/andrew-d/anchor/static"
)

// In Run(), route setup:
staticSub, _ := fs.Sub(anchostatic.FS, ".")
mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticSub))))
mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
    // Serve index.html for the root path (SPA entry point)
    data, err := anchostatic.FS.ReadFile("index.html")
    if err != nil {
        http.Error(w, "not found", 404)
        return
    }
    w.Header().Set("Content-Type", "text/html; charset=utf-8")
    w.Write(data)
})
```

The `GET /static/` route serves vendored JS, CSS, and app.js. The `GET /` route serves `index.html` for the SPA shell. All other routes (API endpoints) are registered with higher-priority patterns.

**Verification:**

Run: `go build ./...`
Expected: Compiles with embedded static files

Run: Start server, navigate to `http://localhost:8080/` in browser
Expected: SPA loads, shows dashboard (empty if no agents)

**Commit:** `feat: embed static files and serve SPA from Go binary`
<!-- END_TASK_9 -->
