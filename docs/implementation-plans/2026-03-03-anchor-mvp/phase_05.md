# Anchor MVP Implementation Plan — Phase 5: Agent

**Goal:** Functional agent that checks in with the server, executes assigned modules in sorted order, and reports results per-module.

**Architecture:** `internal/agent/` package with UUID persistence, platform-specific system info gathering via `//go:build` constraints, a polling loop, and a module runner that executes scripts and captures output.

**Tech Stack:** Go 1.26, stdlib only (`net/http`, `os/exec`, `runtime`, `encoding/json`)

**Scope:** 7 phases from original design (phase 5 of 7)

**Codebase verified:** 2026-03-03 — confirmed greenfield. Phase 4 creates the server API endpoints the agent talks to.

---

## Acceptance Criteria Coverage

This phase implements and tests:

### anchor-mvp.AC2: Module Sync & Execution
- **anchor-mvp.AC2.2 Success:** Agent executes modules in sorted name order (00_base before 10_users)
- **anchor-mvp.AC2.3 Success:** Exit code 0 recorded as "ok", exit code 80 as "changed"
- **anchor-mvp.AC2.4 Success:** Agent reports each module result individually via POST /api/report
- **anchor-mvp.AC2.5 Failure:** Non-zero/non-80 exit code recorded as "error" with captured stderr
- **anchor-mvp.AC2.6 Failure:** If report call fails (network broken), agent stops executing remaining modules and logs the failure

### anchor-mvp.AC6: Agent Identity & System Info
- **anchor-mvp.AC6.1 Success:** Agent generates a UUID and persists it to a file on first run
- **anchor-mvp.AC6.2 Success:** Agent reads existing UUID from file on subsequent runs
- **anchor-mvp.AC6.3 Success:** Linux agent detects OS, arch, and distro from /etc/os-release
- **anchor-mvp.AC6.4 Success:** macOS agent detects OS, arch, and version from sw_vers

---

<!-- START_SUBCOMPONENT_A (tasks 1-3B) -->
<!-- START_TASK_1 -->
### Task 1: Implement UUID persistence

**Files:**
- Modify: `internal/agent/agent.go`

**Implementation:**

Add UUID management to the Agent. On startup:

1. Try to read UUID from `<dataDir>/agent-id` file
2. If file exists and contains a valid UUID string, use it (AC6.2)
3. If file does not exist, generate a new UUID using `crypto/rand` (format: 8-4-4-4-12 hex), write it to the file, create parent directory if needed (AC6.1)

Use a simple UUID v4 generator rather than importing a UUID library:

```go
func generateUUID() (string, error) {
	var uuid [16]byte
	if _, err := rand.Read(uuid[:]); err != nil {
		return "", err
	}
	uuid[6] = (uuid[6] & 0x0f) | 0x40 // Version 4
	uuid[8] = (uuid[8] & 0x3f) | 0x80 // Variant 1
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		uuid[0:4], uuid[4:6], uuid[6:8], uuid[8:10], uuid[10:16]), nil
}
```

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: add agent UUID persistence`
<!-- END_TASK_1 -->

<!-- START_TASK_2 -->
### Task 2: Implement platform-specific system info

**Files:**
- Create: `internal/agent/sysinfo.go` (shared types/interface)
- Create: `internal/agent/sysinfo_linux.go` (with `//go:build linux`)
- Create: `internal/agent/sysinfo_darwin.go` (with `//go:build darwin`)
- Create: `internal/agent/sysinfo_other.go` (with `//go:build !linux && !darwin`)

**Implementation:**

Define a `SystemInfo` struct and a `gatherSystemInfo()` function:

```go
// sysinfo.go
package agent

type SystemInfo struct {
	Hostname string
	OS       string
	Arch     string
	Distro   string
}
```

**Linux implementation** (`//go:build linux`):
- `Hostname`: `os.Hostname()`
- `OS`: `runtime.GOOS` ("linux")
- `Arch`: `runtime.GOARCH` ("amd64", "arm64")
- `Distro`: Parse `/etc/os-release` for `PRETTY_NAME` field. If file doesn't exist or field is missing, use "unknown". Example: "Ubuntu 24.04.1 LTS"

Parsing `/etc/os-release`:
```go
func parseOSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			val := strings.TrimPrefix(line, "PRETTY_NAME=")
			return strings.Trim(val, "\"")
		}
	}
	return "unknown"
}
```

**macOS implementation** (`//go:build darwin`):
- `Hostname`: `os.Hostname()`
- `OS`: `runtime.GOOS` ("darwin")
- `Arch`: `runtime.GOARCH`
- `Distro`: Run `sw_vers` and combine ProductName + ProductVersion. Example: "macOS 15.2"

```go
func parseSwVers() string {
	out, err := exec.Command("sw_vers").Output()
	if err != nil {
		return "unknown"
	}
	var name, version string
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "ProductName:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "ProductName:"))
		}
		if strings.HasPrefix(line, "ProductVersion:") {
			version = strings.TrimSpace(strings.TrimPrefix(line, "ProductVersion:"))
		}
	}
	if name != "" && version != "" {
		return name + " " + version
	}
	return "unknown"
}
```

**Fallback implementation** (`//go:build !linux && !darwin`):
- `Hostname`: `os.Hostname()`
- `OS`: `runtime.GOOS`
- `Arch`: `runtime.GOARCH`
- `Distro`: `"unknown"`

This ensures the project builds on any platform (e.g., FreeBSD, Windows for development).

**Important:** Place the `parseDistroFromOSRelease(content string) string` and `parseDistroFromSwVers(content string) string` functions in the shared `sysinfo.go` file (no build constraints) since they are pure string parsers. This allows `sysinfo_test.go` to test them on any platform.

```go
// In sysinfo.go (shared, no build constraints):
func parseDistroFromOSRelease(content string) string { ... }
func parseDistroFromSwVers(content string) string { ... }

// In sysinfo_linux.go (//go:build linux):
func gatherSystemInfo() SystemInfo {
    data, _ := os.ReadFile("/etc/os-release")
    return SystemInfo{
        Hostname: ...,
        OS:       runtime.GOOS,
        Arch:     runtime.GOARCH,
        Distro:   parseDistroFromOSRelease(string(data)),
    }
}

// In sysinfo_darwin.go (//go:build darwin):
func gatherSystemInfo() SystemInfo {
    out, _ := exec.Command("sw_vers").Output()
    return SystemInfo{
        Hostname: ...,
        OS:       runtime.GOOS,
        Arch:     runtime.GOARCH,
        Distro:   parseDistroFromSwVers(string(out)),
    }
}
```

**Verification:**

Run: `go build ./...`
Expected: Compiles (only one sysinfo file compiles per platform due to build constraints)

**Commit:** `feat: add platform-specific system info gathering`
<!-- END_TASK_2 -->

<!-- START_TASK_3 -->
### Task 3: Test UUID persistence

**Verifies:** anchor-mvp.AC6.1, anchor-mvp.AC6.2

**Files:**
- Create: `internal/agent/agent_test.go`
- Test file: `internal/agent/agent_test.go` (unit)

**Testing:**

Extract the UUID read/write logic into testable functions (e.g., `readOrCreateUUID(dataDir string) (string, error)`).

Tests must verify:

- **anchor-mvp.AC6.1:** Call `readOrCreateUUID` with a fresh `t.TempDir()`. Verify it returns a non-empty UUID string matching UUID v4 format. Verify the file `<dir>/agent-id` exists and contains the same UUID.
- **anchor-mvp.AC6.2:** Write a known UUID to `<dir>/agent-id`. Call `readOrCreateUUID`. Verify it returns the same UUID without modifying the file.

Also test:
- UUID format validation: the generated UUID matches `^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`
- Parent directory is created if it doesn't exist

**Verification:**

Run: `go test ./internal/agent/ -count=1 -v`
Expected: All tests pass

**Commit:** `test: add UUID persistence tests`
<!-- END_TASK_3 -->
<!-- START_TASK_3B -->
### Task 3B: Test system info parsing functions

**Verifies:** anchor-mvp.AC6.3, anchor-mvp.AC6.4

**Files:**
- Create: `internal/agent/sysinfo_test.go`
- Test file: `internal/agent/sysinfo_test.go` (unit — runs on any platform)

**Testing:**

Since `parseDistroFromOSRelease` and `parseDistroFromSwVers` accept string input, they can be tested on any platform without build constraints.

Tests must verify:

- **anchor-mvp.AC6.3 (parseDistroFromOSRelease):**
  - Normal input with `PRETTY_NAME="Ubuntu 24.04.1 LTS"` returns `"Ubuntu 24.04.1 LTS"`
  - Input with unquoted `PRETTY_NAME=Arch Linux` returns `"Arch Linux"`
  - Input missing `PRETTY_NAME` returns `"unknown"`
  - Empty string returns `"unknown"`
  - Input with multiple fields (NAME, VERSION_ID, PRETTY_NAME) extracts only PRETTY_NAME

- **anchor-mvp.AC6.4 (parseDistroFromSwVers):**
  - Normal input with `ProductName:\tmacOS` and `ProductVersion:\t15.2` returns `"macOS 15.2"`
  - Input missing ProductVersion returns `"unknown"`
  - Input missing ProductName returns `"unknown"`
  - Empty string returns `"unknown"`

Use table-driven tests for each parser.

**Verification:**

Run: `go test ./internal/agent/ -count=1 -v`
Expected: All tests pass

**Commit:** `test: add system info parsing tests`
<!-- END_TASK_3B -->
<!-- END_SUBCOMPONENT_A -->

<!-- START_SUBCOMPONENT_B (tasks 4-6) -->
<!-- START_TASK_4 -->
### Task 4: Implement module runner

**Files:**
- Create: `internal/agent/runner.go`

**Implementation:**

Create a `runModule` function that executes a single module script:

```go
type ModuleResult struct {
	ModuleName string
	Status     string // "ok", "changed", "error"
	Stdout     string
	Stderr     string
}

func runModule(name string, script string) ModuleResult {
	// 1. Write script to temp file
	// 2. chmod +x
	// 3. Execute with "apply" argument
	// 4. Capture stdout, stderr
	// 5. Determine status from exit code:
	//    - 0 → "ok"
	//    - 80 → "changed"
	//    - anything else → "error"
	// 6. Clean up temp file
}
```

Use `os.CreateTemp` to write the script, then `os/exec.Command` to run it. Capture stdout and stderr separately using `cmd.Stdout` and `cmd.Stderr` with `bytes.Buffer`. Check the exit code via `exec.ExitError`.

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: add module runner with exit code handling`
<!-- END_TASK_4 -->

<!-- START_TASK_5 -->
### Task 5: Test module runner

**Verifies:** anchor-mvp.AC2.2, anchor-mvp.AC2.3, anchor-mvp.AC2.5

**Files:**
- Create: `internal/agent/runner_test.go`
- Test file: `internal/agent/runner_test.go` (unit)

**Testing:**

Tests use inline shell scripts as the "module" content:

- **anchor-mvp.AC2.3 (exit 0 = "ok"):** Run a module script that exits 0. Verify status is "ok" and stdout is captured.
- **anchor-mvp.AC2.3 (exit 80 = "changed"):** Run a module script that exits 80. Verify status is "changed".
- **anchor-mvp.AC2.5:** Run a module script that exits 1 with stderr output. Verify status is "error" and stderr is captured.

Also test:
- Stdout and stderr are captured correctly for all exit codes
- Script that writes to both stdout and stderr captures both independently

Use table-driven tests with test scripts like:
```sh
#!/bin/sh
echo "stdout output"
echo "stderr output" >&2
exit <code>
```

**Verification:**

Run: `go test ./internal/agent/ -count=1 -v`
Expected: All tests pass

**Commit:** `test: add module runner tests for exit code handling`
<!-- END_TASK_5 -->

<!-- START_TASK_6 -->
### Task 6: Test module execution ordering

**Verifies:** anchor-mvp.AC2.2

**Files:**
- Modify: `internal/agent/runner_test.go`

**Testing:**

- **anchor-mvp.AC2.2:** Create a function that takes a slice of modules (name + script) and returns them in sorted name order. Verify that given modules `["10_users", "00_base", "05_packages"]`, the execution order is `["00_base", "05_packages", "10_users"]`.

This tests the sorting logic that the polling loop will use. The polling loop (Task 7) will sort modules by name before executing.

**Verification:**

Run: `go test ./internal/agent/ -count=1 -v`
Expected: All tests pass

**Commit:** `test: add module execution ordering test`
<!-- END_TASK_6 -->
<!-- END_SUBCOMPONENT_B -->

<!-- START_SUBCOMPONENT_C (tasks 7-8) -->
<!-- START_TASK_7 -->
### Task 7: Implement polling loop with checkin and reporting

**Files:**
- Modify: `internal/agent/agent.go`
- Modify: `cmd/anchor/main.go` (update `Run()` call to `Run(context.Background())`)

**Implementation:**

Rewrite `Agent.Run()` to implement the full polling loop:

1. Read or create UUID (Task 1)
2. Gather system info (Task 2)
3. Enter polling loop:
   a. Build `CheckinRequest` JSON with UUID + system info
   b. POST to `<serverURL>/api/checkin`, decode `CheckinResponse`
   c. Sort received modules by name
   d. For each module in sorted order:
      - Execute with `runModule` (Task 4)
      - Build `ReportRequest` JSON with result
      - POST to `<serverURL>/api/report`
      - If report POST fails: log the error and stop executing remaining modules (AC2.6)
   e. Sleep for `PollIntervalSeconds` from checkin response
   f. Repeat

The agent should accept a `context.Context` for cancellation (allows clean shutdown and testability). Change `Run()` signature to `Run(ctx context.Context) error`. Update `cmd/anchor/main.go` to pass `context.Background()`.

If checkin fails, log the error and sleep before retrying (no local caching, no offline execution per design).

**Verification:**

Run: `go build ./...`
Expected: Compiles

**Commit:** `feat: implement agent polling loop with checkin and reporting`
<!-- END_TASK_7 -->

<!-- START_TASK_8 -->
### Task 8: Integration test for agent polling loop

**Verifies:** anchor-mvp.AC2.4, anchor-mvp.AC2.6

**Files:**
- Modify: `internal/agent/agent_test.go`
- Test file: `internal/agent/agent_test.go` (integration — real HTTP server + agent)

**Testing:**

Set up a test HTTP server (using `httptest.NewServer`) that mimics the anchor server API:
- `/api/checkin` returns a response with one or two modules and a short poll interval
- `/api/report` records received reports

Use a context with cancel to stop the agent after one poll cycle.

- **anchor-mvp.AC2.4:** Agent checks in, receives 2 modules, executes both, reports each individually. Verify the test server received 2 separate report POSTs with correct module names and statuses.
- **anchor-mvp.AC2.6:** Set up a test server where `/api/report` returns 500 after the first successful report. Agent receives 3 modules. Verify agent reports first module successfully, gets error on second report, does NOT attempt to report third module.

**Verification:**

Run: `go test ./internal/agent/ -count=1 -v`
Expected: All tests pass

Run: `go test ./... -count=1`
Expected: All tests pass

**Commit:** `test: add agent polling loop integration tests`
<!-- END_TASK_8 -->
<!-- END_SUBCOMPONENT_C -->
