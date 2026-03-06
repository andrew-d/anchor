# Anchor

anchor is a basic, homelab-friendly configuration management system: designed for simplicity and minimal operational overhead, written in Go and distributed as a single binary.

## Environment

- Go: `1.26.x`, specified in `go.mod`
- Frontend: Preact + HTM, no build step, no CDN/external resources

## Development

We are still developing this application. There is no need to migrate data or worry about breaking backwards compatibility.

After making changes, verify with `make ci`.

## Architecture

Anchor consists of two parts: a server that shows the current status and allows distributing configuration to endpoints, and a very minimal agent. Both are compiled into the same binary (`cmd/anchor/main.go` with `server` and `agent` subcommands).

The server can be connected to by any agent, and stores information provided by that agent. It maintains a list of modules assigned to each agent, and has a basic web UI that shows the status of each module and allows assigning modules to agents. It stores data in SQLite, and modules are read from a configuration directory (defaulting to `/etc/anchor/modules.d`).

An agent gathers very basic information about the host system (hostname, architecture, Linux distribution) and checks in with the server specified. It synchronizes configuration from the server, each of which is a single self-contained shell script. On synchronization and periodically, each script is run and the outcome is reported back to the server.

### Modules

Each module is a shell script that accepts a single command argument:
- `metadata`: outputs information about the module as JSON (`{"name": "...", "description": "..."}`)
- `apply`: applies the configuration idempotently

Module exit codes for `apply`: 0 = "ok" (no changes needed), 80 = "changed" (changes were applied), anything else = "error". stdout and stderr are captured and reported to the server.

The module **filename** (e.g., `00_base`) is the canonical identifier used in assignments, API responses, and database records. The metadata `name` and `description` are display-only. Modules are sorted and executed in filename order.

#### Artifacts

A module may have an associated `<filename>.d/` directory containing arbitrary files (config templates, binaries, etc.). These are distributed to agents via a content-addressable cache (`{dataDir}/artifacts/{sha256}`). During execution, artifacts are copied into a `files/` subdirectory and the module script's working directory is set there, so scripts access artifacts via relative paths.

File permissions are preserved through the pipeline (server loader → checkin response → agent cache → execution directory). The server discovers artifacts by walking `.d` directories; if a `.d` directory exists but cannot be read, the module is treated as a load error.

Path traversal safety: the agent uses `os.Root` to confine all file writes (both the script and artifact copies) within the temporary execution directory.

### SQLite

Uses `modernc.org/sqlite` (pure Go, no CGO required). Never use in-memory SQLite databases, even in tests. Always use on-disk databases (use `t.TempDir()` in tests).

### Web UI

The web UI uses vendored Preact + HTM (no build step, no CDN). Static files live in `static/` and are embedded into the binary via `//go:embed` in `static/embed.go`. The UI is a single-page app with hash-based routing, served at `/` with assets under `/static/`.

### CLI and library boundaries

- Library packages must never call `os.Exit()`. Return exit codes or errors instead. Only `main()` in `cmd/` should call `os.Exit()`.
- `cmd/*/main.go` files should be thin wrappers. CLI and business logic belongs in library packages.

## Style

### Logging

Use `log/slog` with structured key-value pairs. Log messages should be static strings; dynamic values go in key-value arguments with `snake_case` keys. Never use `log.Fatal` or `log.Fatalf`.

### Platform-specific code

Use `//go:build` constraints (e.g. `//go:build unix`) for code that relies on platform-specific syscall fields.
