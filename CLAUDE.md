## Development

We are still developing this application. There is no need to migrate data or worry about breaking backwards compatibility.

After making changes, verify with `go build ./...`, `go vet ./...`, and `go test ./... -count=1`.

## Architecture

### Modules

- Modules register their configuration types during `Init`, not during construction or in main.
- Background goroutines in modules must be started via the `Go()` helper on the init context, not with bare `go` statements. This ensures shutdown waits for all module goroutines to finish.
- Use the logger provided by the init context. Never use global loggers or create new root loggers in module code.

### Configuration types

- The "kind" string is derived from the type itself (via a `Kind()` method), not passed as a separate parameter.
- Registering the same Go type twice should panic.

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

### Permissions

When validating filesystem permissions for a specific user, check the target user's uid/gid ownership and mode bits — not whether the current process can access the resource. The application may run as root.
