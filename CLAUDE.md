## Summary

anchor is a basic, homelab-friendly configuration management system: designed for simplicity and minimal operational overhead, written in Go and distributed as a single binary.

## Development

We are still developing this application. There is no need to migrate data or worry about breaking backwards compatibility.

After making changes, verify with `go build ./...`, `go vet ./...`, and `go test ./... -count=1`.

## Architecture

Anchor consists of two parts: a server that shows the current status and allows distributing configuration to endpoints, and a very minimal agent. Both are compiled into the same binary.

The server can be connected to by any agent, and stores information provided by that agent. It maintains a list of modules assigned to each agent, and has a basic Preact + HTM web UI that shows the status of each module and allows assigning modules to agents. It stores data in SQLite, and modules are read from a configuration directory (defaulting to `/etc/anchor/modules.d`).

An agent gathers very basic information about the host system (hostname, architecture, Linux distribution) and checks in with the server specified. It synchronizes configuration from the server, each of which is a single self-contained shell script. On synchronization and periodically, each script is run and the outcome is reported back to the server.

Each module is a shell script; they take the `metadata` argument (outputs information about the module in JSON format), and `apply` which should apply the configuration idempotently and reports back whether anything changed. stdout and stderr are saved and reported to the server.

### SQLite

Never use in-memory SQLite databases, even in tests. Always use on-disk databases (use `t.TempDir()` in tests).

### CLI and library boundaries

- Library packages must never call `os.Exit()`. Return exit codes or errors instead. Only `main()` in `cmd/` should call `os.Exit()`.
- `cmd/*/main.go` files should be thin wrappers. CLI and business logic belongs in library packages.

## Style

### Logging

Use `log/slog` with structured key-value pairs. Log messages should be static strings; dynamic values go in key-value arguments with `snake_case` keys. Never use `log.Fatal` or `log.Fatalf`.

### Platform-specific code

Use `//go:build` constraints (e.g. `//go:build unix`) for code that relies on platform-specific syscall fields.
