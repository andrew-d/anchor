# Anchor MVP Implementation Plan — Phase 1: Project Scaffolding

**Goal:** Buildable Go project with CLI subcommands that route to server and agent entry points.

**Architecture:** Single Go binary with `server` and `agent` subcommands parsed via `flag.FlagSet`. CLI parsing lives in `cmd/anchor/main.go` (thin wrapper), business logic in `internal/server/` and `internal/agent/` library packages.

**Tech Stack:** Go 1.26, stdlib only (no external dependencies)

**Scope:** 7 phases from original design (phase 1 of 7)

**Codebase verified:** 2026-03-03 — confirmed greenfield, no Go files exist

---

## Acceptance Criteria Coverage

**Verifies: None** — this is an infrastructure/scaffolding phase. Verified operationally (build succeeds, binary runs).

---

<!-- START_TASK_1 -->
### Task 1: Initialize Go module

**Files:**
- Create: `go.mod`

**Step 1: Create go.mod**

Run:
```bash
cd /home/andrew/repos/anchor && go mod init github.com/andrew-d/anchor
```

**Step 2: Verify**

Run: `cat go.mod`
Expected: Shows `module github.com/andrew-d/anchor` with Go 1.26

**Step 3: Commit**

```bash
git add go.mod
git commit -m "chore: initialize Go module"
```
<!-- END_TASK_1 -->

<!-- START_TASK_2 -->
### Task 2: Create main.go with subcommand routing

**Files:**
- Create: `cmd/anchor/main.go`

**Step 1: Create the file**

```go
package main

import (
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/andrew-d/anchor/internal/agent"
	"github.com/andrew-d/anchor/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: anchor <server|agent>\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "server":
		os.Exit(runServer(os.Args[2:]))
	case "agent":
		os.Exit(runAgent(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: anchor <server|agent>\n", os.Args[1])
		os.Exit(1)
	}
}

func runServer(args []string) int {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	port := fs.Int("port", 8080, "HTTP listen port")
	modulesDir := fs.String("modules-dir", "/etc/anchor/modules.d", "Directory containing module scripts")
	dataDir := fs.String("data-dir", "/var/lib/anchor", "Directory for persistent data (SQLite database)")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		return 1
	}

	srv := server.New(*port, *modulesDir, *dataDir)
	if err := srv.Run(); err != nil {
		slog.Error("server error", "error", err)
		return 1
	}
	return 0
}

func runAgent(args []string) int {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	serverURL := fs.String("server", "http://localhost:8080", "Server URL")
	dataDir := fs.String("data-dir", "/var/lib/anchor", "Directory for persistent data (agent UUID)")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		return 1
	}

	a := agent.New(*serverURL, *dataDir)
	if err := a.Run(); err != nil {
		slog.Error("agent error", "error", err)
		return 1
	}
	return 0
}
```

Note: This file will not compile until Tasks 3 and 4 create the imported packages.

**Step 2: Commit**

```bash
git add cmd/anchor/main.go
git commit -m "chore: add main.go with server/agent subcommand routing"
```
<!-- END_TASK_2 -->

<!-- START_TASK_3 -->
### Task 3: Create server stub package

**Files:**
- Create: `internal/server/server.go`

**Step 1: Create the file**

```go
package server

import (
	"fmt"
	"log/slog"
	"net/http"
)

// Server is the anchor HTTP server.
type Server struct {
	port       int
	modulesDir string
	dataDir    string
}

// New creates a new Server.
func New(port int, modulesDir string, dataDir string) *Server {
	return &Server{
		port:       port,
		modulesDir: modulesDir,
		dataDir:    dataDir,
	}
}

// Run starts the HTTP server and blocks until it returns an error.
func (s *Server) Run() error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "anchor server")
	})

	addr := fmt.Sprintf(":%d", s.port)
	slog.Info("starting server", "addr", addr, "modules_dir", s.modulesDir)
	return http.ListenAndServe(addr, mux)
}
```

**Step 2: Commit**

```bash
git add internal/server/server.go
git commit -m "chore: add server stub package"
```
<!-- END_TASK_3 -->

<!-- START_TASK_4 -->
### Task 4: Create agent stub package

**Files:**
- Create: `internal/agent/agent.go`

**Step 1: Create the file**

```go
package agent

import "log/slog"

// Agent is the anchor agent that checks in with the server.
type Agent struct {
	serverURL string
	dataDir   string
}

// New creates a new Agent.
func New(serverURL string, dataDir string) *Agent {
	return &Agent{
		serverURL: serverURL,
		dataDir:   dataDir,
	}
}

// Run performs a single check-in cycle and returns.
func (a *Agent) Run() error {
	slog.Info("agent starting", "server_url", a.serverURL, "data_dir", a.dataDir)
	slog.Info("agent finished (stub)")
	return nil
}
```

**Step 2: Commit**

```bash
git add internal/agent/agent.go
git commit -m "chore: add agent stub package"
```
<!-- END_TASK_4 -->

<!-- START_TASK_5 -->
### Task 5: Verify build and run

**Step 1: Build**

Run: `go build ./...`
Expected: No errors

**Step 2: Vet**

Run: `go vet ./...`
Expected: No errors

**Step 3: Build the binary**

Run: `go build -o anchor ./cmd/anchor`
Expected: Produces `./anchor` binary

**Step 4: Test server subcommand**

Run: `./anchor server --port 9999 &` then `curl http://localhost:9999/` then `kill %1`
Expected: Server starts, curl returns "anchor server", server stops

**Step 5: Test agent subcommand**

Run: `./anchor agent --server http://localhost:8080`
Expected: Logs "agent starting" and "agent finished (stub)", exits 0

**Step 6: Test no-args usage**

Run: `./anchor 2>&1; echo $?`
Expected: Prints usage message, exits 1

**Step 7: Clean up binary**

Add `anchor` (the built binary) to `.gitignore`:
```
anchor
```

**Step 8: Commit**

```bash
git add .gitignore
git commit -m "chore: add .gitignore for built binary"
```
<!-- END_TASK_5 -->
