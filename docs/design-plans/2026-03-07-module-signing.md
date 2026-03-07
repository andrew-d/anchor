# Module Signing Design

## Summary

Module signing adds an opt-in cryptographic layer that lets operators prove a module script has not been modified since they authored it. The signing key never leaves the operator's workstation — the server acts as a dumb relay, passing opaque signature bytes from the module directory through to agents without inspecting them. Agents are the enforcement point: when configured with one or more trusted public keys (supplied inline, as files, or fetched from a URL such as a GitHub `.keys` endpoint), an agent will refuse to execute any module whose signature is missing or does not match a trusted key.

The implementation spans four areas of the codebase: a new `internal/signing` package that owns all cryptographic primitives; two new CLI subcommands (`anchor keygen` and `anchor sign`) for workstation-side key and signature management; minor additions to the server-side module loader and checkin API to carry signatures as pass-through data; and verification logic in the agent that resolves a trust set before each run and gates module execution on it. Ed25519 is used throughout. Both an Anchor-native PEM format and standard OpenSSH key formats are accepted so operators can reuse existing SSH keys.

## Definition of Done

1. `anchor keygen` subcommand generates an ed25519 keypair in PEM format (ANCHOR SIGNING KEY / ANCHOR PUBLIC KEY).
2. `anchor sign` subcommand signs a module script, producing a hex-encoded `.sig` sidecar file. Accepts both anchor-format and SSH-format private keys.
3. Server-side module loader picks up `.sig` files and passes signatures through to agents via the checkin API (server does not verify).
4. Agent accepts `--verify-key` (repeatable; inline SSH pubkey, anchor pubkey, or path to key file) and `--verify-key-url` (URL returning SSH-format keys, e.g. GitHub `.keys` endpoint).
5. Agent verifies module signatures before execution: valid signature = execute, invalid/missing signature = error. No keys configured = allow all modules.
6. URL-fetched keys are cached in the data directory, refreshed before each run with a 10-second timeout, and the cached version is used on fetch failure.

**Success criteria:** An operator can sign modules on their workstation, deploy them to the server, configure agents to trust their key (directly or via GitHub URL), and only signed modules execute.

**Out of scope:** Artifact signing, key rotation CLI, signature revocation, signing manifests, SSH signature format (SSHSIG).

## Acceptance Criteria

### module-signing.AC1: Key generation produces valid keypair
- **module-signing.AC1.1 Success:** `anchor keygen` produces private key file (mode 0600) and public key file (mode 0644) in PEM format
- **module-signing.AC1.2 Success:** Generated keys round-trip through PEM encode/decode
- **module-signing.AC1.3 Success:** Generated public key can verify signatures made with the generated private key
- **module-signing.AC1.4 Failure:** Output path that cannot be written to returns exit code 1 with error message

### module-signing.AC2: Signing produces valid hex-encoded signature files
- **module-signing.AC2.1 Success:** `anchor sign -k keyfile module` produces `module.sig` containing hex-encoded ed25519 signature
- **module-signing.AC2.2 Success:** `anchor sign` accepts anchor PEM private key format
- **module-signing.AC2.3 Success:** `anchor sign` accepts OpenSSH ed25519 private key format
- **module-signing.AC2.4 Success:** Signing multiple modules in one invocation produces one `.sig` per module
- **module-signing.AC2.5 Failure:** Non-ed25519 SSH private key is rejected with descriptive error
- **module-signing.AC2.6 Failure:** Missing or unreadable key file returns exit code 1

### module-signing.AC3: Server loader discovers and passes through signatures
- **module-signing.AC3.1 Success:** `.sig` files are not treated as modules by the loader
- **module-signing.AC3.2 Success:** Valid `.sig` sidecar is read and attached to the corresponding module
- **module-signing.AC3.3 Success:** Module without `.sig` file has nil/empty signature (not an error)
- **module-signing.AC3.4 Success:** Checkin response includes hex-encoded signature in `signature` field
- **module-signing.AC3.5 Failure:** Malformed `.sig` file (invalid hex, wrong length) treated as module load error with warning log
- **module-signing.AC3.6 Edge:** `.sig` file with no matching module is silently ignored

### module-signing.AC4: Agent verifies signatures before execution
- **module-signing.AC4.1 Success:** No verification flags → all modules execute regardless of signature presence
- **module-signing.AC4.2 Success:** Valid signature matching a trusted key → module executes normally
- **module-signing.AC4.3 Success:** Module signed by any one of multiple trusted keys → module executes
- **module-signing.AC4.4 Failure:** Missing signature when verification enabled → module errors with "missing signature"
- **module-signing.AC4.5 Failure:** Invalid signature (wrong key, tampered content) → module errors with "signature verification failed"
- **module-signing.AC4.6 Failure:** Verification flags specified but trust set is empty → all modules error with "no trusted keys available"

### module-signing.AC5: Agent accepts multiple key formats and sources
- **module-signing.AC5.1 Success:** `--verify-key` with inline `ssh-ed25519 AAAA...` string is parsed correctly
- **module-signing.AC5.2 Success:** `--verify-key` with path to anchor PEM public key file is parsed correctly
- **module-signing.AC5.3 Success:** `--verify-key` with path to SSH public key file is parsed correctly
- **module-signing.AC5.4 Success:** Multiple `--verify-key` flags combine into a single trust set
- **module-signing.AC5.5 Edge:** File containing mixed key types (RSA, ed25519) → only ed25519 keys are used, others silently skipped

### module-signing.AC6: URL key fetching with caching
- **module-signing.AC6.1 Success:** `--verify-key-url` fetches keys and adds ed25519 keys to trust set
- **module-signing.AC6.2 Success:** Fetched keys are cached to `{dataDir}/trusted-keys-cache`
- **module-signing.AC6.3 Success:** On fetch failure, cached keys are loaded and used
- **module-signing.AC6.4 Success:** On fetch success, cache file is updated with fresh content
- **module-signing.AC6.5 Failure:** Fetch timeout exceeding 10 seconds falls back to cache
- **module-signing.AC6.6 Edge:** URL returns zero ed25519 keys → warning logged, zero keys contributed (not an error)
- **module-signing.AC6.7 Edge:** Fetch fails with no cache file and no `--verify-key` → trust set empty, all modules rejected

## Glossary

- **ed25519**: A public-key signature algorithm based on elliptic-curve cryptography. Each keypair consists of a 32-byte private seed and a 32-byte public key.
- **PEM**: Privacy Enhanced Mail — a Base64-encoded data format wrapped in `-----BEGIN <TYPE>-----` / `-----END <TYPE>-----` header lines. Used here to store ed25519 keys on disk in a human-readable, clearly labeled form.
- **Sidecar file**: A companion file placed next to a primary file, sharing the same base name with a different extension (e.g., `module_file.sig` alongside `module_file`).
- **Trust set**: The collection of public keys an agent considers authoritative. A signature is accepted if it verifies against any single key in the set.
- **OpenSSH private key format**: The `-----BEGIN OPENSSH PRIVATE KEY-----` format produced by `ssh-keygen`. Distinct from PKCS#8 or standard PEM RSA formats.
- **authorized\_keys format**: The single-line SSH public key format (`ssh-ed25519 AAAA... comment`) used in `~/.ssh/authorized_keys` and returned by GitHub's `https://github.com/<user>.keys` endpoint.
- **`golang.org/x/crypto/ssh`**: The Go extended-library package for SSH protocol support, used here to parse OpenSSH private and public key formats.
- **Content-addressable cache**: A storage scheme where files are named by a hash of their contents. Used in Anchor for artifact distribution.
- **Checkin API**: The HTTP endpoint the agent calls to report its status and receive module assignments from the server.
- **`os.Root`**: A Go standard-library construct that scopes filesystem operations to a specific directory, preventing path traversal. Used by the agent when writing module scripts and artifacts to a temporary execution directory.

## Architecture

Module signing adds an opt-in cryptographic verification layer between the server and agents. The signing key lives on the operator's workstation; the server never sees it. Signatures flow as opaque data through the server to agents, which verify them against a configured trust set.

### Package Structure

```
internal/signing/       # All cryptography primitives
  keys.go               # Key generation, PEM encode/decode (ANCHOR SIGNING KEY / ANCHOR PUBLIC KEY)
  ssh.go                # SSH authorized_keys parsing, SSH private key reading
  sign.go               # ed25519 signing
  verify.go             # ed25519 verification

internal/module/        # Existing module loader (modified)
  module.go             # Module struct gains Signature field; Loader skips .sig, reads sidecar

internal/agent/         # Existing agent (modified)
  agent.go              # Key management, URL fetching/caching, verification before execution

internal/api/           # Existing API types (modified)
  types.go              # CheckinModule gains Signature field

cmd/anchor/
  main.go               # New runKeygen() and runSign() subcommands
```

### Key Format

Anchor-native keys use PEM encoding with custom type strings:

```
-----BEGIN ANCHOR SIGNING KEY-----
[base64-encoded 32-byte ed25519 seed]
-----END ANCHOR SIGNING KEY-----
```

```
-----BEGIN ANCHOR PUBLIC KEY-----
[base64-encoded 32-byte ed25519 public key]
-----END ANCHOR PUBLIC KEY-----
```

The custom type names prevent confusion with SSH or TLS keys.

### Key Interoperability

The `signing` package accepts multiple key formats:

- **Private keys:** Anchor PEM (`ANCHOR SIGNING KEY`) or OpenSSH private key format (parsed via `golang.org/x/crypto/ssh.ParseRawPrivateKey`). Auto-detected by PEM type.
- **Public keys:** Anchor PEM (`ANCHOR PUBLIC KEY`) or SSH `authorized_keys` lines (`ssh-ed25519 AAAA...`), parsed via `ssh.ParseAuthorizedKey`. Non-ed25519 key types, comments, and blank lines are silently skipped.

### Data Flow

1. **Operator signs** modules on workstation: `anchor sign -k mykey module_file` produces `module_file.sig`
2. **Server loader** walks modules directory, skips `.sig` as module candidates, reads `.sig` sidecar for each module if present, stores raw signature bytes on `Module.Signature`
3. **Server checkin handler** includes hex-encoded signature in `CheckinModule.Signature` field (passthrough, no verification)
4. **Agent** resolves trust set from `--verify-key` and `--verify-key-url` flags at startup and before each run
5. **Agent verification** before execution: if any verification flag was specified, verify each module's signature against all trusted keys; any single key match = trusted

### Trust Logic

| Configuration | Behavior |
|---|---|
| No flags at all | Verification disabled, all modules execute |
| Any flag specified, trust set non-empty | Modules must have valid signature matching a trusted key |
| Any flag specified, trust set empty | All modules fail ("no trusted keys available") |

The flag presence is the opt-in signal, not the key count. A misconfigured URL cannot silently degrade to "trust everything."

### URL Key Fetching

When `--verify-key-url` is configured:

- Before each run loop iteration, fetch the URL with a dedicated HTTP client using a 10-second timeout
- On success: parse response with `signing.ParsePublicKeys()`, write raw response body to `{dataDir}/trusted-keys-cache`, merge parsed keys into trust set
- On failure: log warning, read `{dataDir}/trusted-keys-cache` if it exists, parse and merge cached keys
- URL returning zero ed25519 keys (e.g., user only has RSA keys): log warning, contribute zero keys to trust set (not an error)

## Existing Patterns

### CLI Subcommand Pattern

Investigation found the existing pattern in `cmd/anchor/main.go`: a switch on `os.Args[1]` dispatching to `run<Command>(args []string) int` functions. Each function creates a `flag.FlagSet`, parses args, and returns 0/1. `keygen` and `sign` follow this exactly.

### Module Loader Pattern

The loader in `internal/module/module.go` uses `os.ReadDir()` to scan the modules directory, skips non-regular files, and uses hash-based caching to avoid re-parsing unchanged modules. Signature loading follows the same adjacency pattern as artifact directories (`{filename}.d`): check if `{filename}.sig` exists, read if present.

### Error Handling Pattern

The loader uses tiered error handling with hash-based duplicate suppression — file read errors and metadata parse errors are logged once (via `slog.Warn`), cached, and suppressed on subsequent loads if the file hasn't changed. Malformed `.sig` files follow this same pattern.

### API Type Pattern

`CheckinModule` and `CheckinArtifact` use `json:"...,omitempty"` for optional fields. The signature field follows suit: `Signature string \`json:"signature,omitempty"\``.

## Implementation Phases

<!-- START_PHASE_1 -->
### Phase 1: Signing Package Core
**Goal:** Implement the `internal/signing` package with key generation, PEM encoding/decoding, SSH key parsing, signing, and verification.

**Components:**
- `internal/signing/keys.go` — `GenerateKey()`, `MarshalPrivateKey()`, `MarshalPublicKey()`, `ParsePrivateKey()`, `ParsePublicKey()`, `ParsePublicKeys()`
- `internal/signing/sign.go` — `Sign(privateKey, message) []byte`
- `internal/signing/verify.go` — `Verify(publicKey, message, signature) bool`

**Dependencies:** None (new package, stdlib + `golang.org/x/crypto/ssh`)

**Done when:** Tests verify: key generation round-trips through PEM, SSH key parsing extracts ed25519 keys, sign-then-verify succeeds, verification rejects tampered messages, non-ed25519 keys are skipped
<!-- END_PHASE_1 -->

<!-- START_PHASE_2 -->
### Phase 2: CLI Commands
**Goal:** Add `anchor keygen` and `anchor sign` subcommands.

**Components:**
- `cmd/anchor/main.go` — `runKeygen()` and `runSign()` functions, usage text updates, switch case additions
- Uses `internal/signing` for all crypto operations

**Dependencies:** Phase 1

**Done when:** `anchor keygen` produces valid PEM keypair files with correct permissions (0600/0644), `anchor sign` produces hex-encoded `.sig` files that verify against the generated public key, `anchor sign` works with both anchor PEM and SSH private keys
<!-- END_PHASE_2 -->

<!-- START_PHASE_3 -->
### Phase 3: Server-Side Signature Loading
**Goal:** Module loader discovers `.sig` sidecar files and passes signatures through the checkin API.

**Components:**
- `internal/module/module.go` — `Module` struct gains `Signature []byte` field; `loadAll()` skips `.sig` files as module candidates and reads matching `.sig` sidecar files
- `internal/api/types.go` — `CheckinModule` gains `Signature string` field
- `internal/server/api.go` — checkin handler maps `Module.Signature` to hex-encoded string in response

**Dependencies:** Phase 1 (for hex encoding conventions)

**Done when:** Loader skips `.sig` files as modules, reads valid `.sig` files and attaches to modules, handles malformed `.sig` files as cached errors, checkin response includes signature field, unsigned modules have empty signature field
<!-- END_PHASE_3 -->

<!-- START_PHASE_4 -->
### Phase 4: Agent-Side Verification
**Goal:** Agent verifies module signatures against configured trusted keys before execution.

**Components:**
- `cmd/anchor/main.go` — new `--verify-key` (repeatable) and `--verify-key-url` flags on agent subcommand, key resolution at startup
- `internal/agent/agent.go` — trust set storage, verification step in module execution loop, key URL fetching and caching

**Dependencies:** Phase 1, Phase 3

**Done when:** Agent with no flags executes all modules; agent with `--verify-key` rejects unsigned/invalid modules and accepts validly signed ones; agent with `--verify-key-url` fetches and caches keys; empty trust set (flags specified but no keys resolved) rejects all modules; URL fetch failure falls back to cache; cached version used when URL unreachable
<!-- END_PHASE_4 -->

<!-- START_PHASE_5 -->
### Phase 5: Integration Testing
**Goal:** End-to-end verification of the complete signing workflow.

**Components:**
- Integration test covering: keygen → sign → server load → agent checkin → verification → execution
- Test cases for mixed signed/unsigned modules, key rotation (multiple trusted keys), URL key fetching with mock HTTP server

**Dependencies:** Phases 1-4

**Done when:** Full workflow test passes: operator signs modules, server distributes signatures, agent verifies and executes only signed modules; edge cases covered (malformed sig, wrong key, missing sig with trust enabled)
<!-- END_PHASE_5 -->

## Additional Considerations

**Threat model:** This protects against a compromised server process injecting rogue modules or tampering with the module directory. It does not protect against a compromised signing key or replay of old valid modules (acceptable for configuration management).

**Dependency:** This design adds `golang.org/x/crypto/ssh` as a dependency for SSH key parsing. This is a well-maintained, widely-used Go module with no transitive dependencies beyond the Go standard library.
