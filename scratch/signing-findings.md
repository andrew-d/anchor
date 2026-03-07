# Module Signing Design Findings

## Threat Model

Primary threat: **compromised server process/VM**. Transport security is handled by Tailscale, so plain HTTP is fine. The signing key lives on the operator's workstation, never on the server.

## Research Summary

### What existing tools do

- **Ansible Automation Controller**: GPG + SHA256 checksum manifest via `ansible-sign`. If signature is invalid or files changed, project refuses to update and jobs won't launch.
- **SaltStack**: RSA keypair-based PKI between master/minion. Optional master public key signing for minions to verify. Manual key acceptance (`salt-key --accept`).
- **Puppet/Chef**: mTLS-based trust only. No code signing for modules/manifests/cookbooks.
- **No major tool prevents a compromised server from pushing arbitrary code.** Signing is the closest mitigation, and only Ansible has it.

### Lightweight signing options

- **Minisign/Ed25519**: Recommended for small projects. Tiny keys, simple API, no external dependencies. Go's `crypto/ed25519` is in the stdlib.
- **GPG**: What Ansible uses. Heavier, requires external tooling, more complex key management.
- **SSH signatures**: Emerging option, could reuse existing SSH keys. Less established.

**Recommendation: Ed25519** — pure Go, no CGO, no external tools, tiny keys (32 bytes public), simple to implement.

## Proposed Design

### Key Generation

```
anchor keygen [-o keyfile]
```

Generates an Ed25519 keypair. Writes private key to `keyfile` (default: `anchor-signing.key`), prints public key to stdout. Private key stays on operator's workstation.

### Signing

```
anchor sign -k anchor-signing.key <module-file>
```

Produces `<module-file>.sig` containing the Ed25519 signature of the file contents. Sidecar `.sig` files live alongside modules in the modules directory.

### Server-Side Changes

- `module.Loader` discovers `.sig` files when walking the module directory (skip `.sig` as module candidates, associate them with their module)
- `module.Module` gets a `Signature []byte` field
- `api.CheckinModule` gets a `Signature string` field (hex or base64 encoded)
- Server passes signatures through to agents — it does not verify them itself (it doesn't need the public key)

### Agent-Side Changes

- Agent config gets a `verify_key` field (Ed25519 public key, hex or base64)
- If `verify_key` is set, agent verifies each module's script content against its signature before execution
- Missing or invalid signature → skip module, report "error" status with descriptive stderr
- If `verify_key` is not set, agent runs modules without verification (opt-in, backwards compatible)

### Why Sidecar `.sig` Files (Not a Manifest)

- Fits Anchor's existing per-module model
- Loader already walks the directory — picking up `.sig` files is natural
- Adding/removing individual modules doesn't require re-signing everything
- `ls` shows you what's signed at a glance
- Simpler implementation than a manifest approach

### What This Protects Against

- Compromised server process injecting rogue modules ✓
- Tampering with module directory by less-privileged process ✓
- MITM between server and agent (even without TLS) ✓

### What This Does NOT Protect Against

- Compromised signing key (mitigate with key rotation, keeping key off servers)
- Replay attacks (old valid modules re-served) — acceptable for config management
- A malicious operator with the signing key — out of scope

### Implementation Complexity

- ~100-150 lines for keygen/sign CLI commands
- ~20 lines in Loader to pick up .sig files
- ~10 lines in API types
- ~20 lines in agent to verify before execution
- Tests for each layer

### Open Questions

1. Signature encoding in the wire format: hex vs base64? (hex is consistent with existing hash fields)
2. Should the server reject modules with missing `.sig` files, or pass them through unsigned? (Recommend: pass through, let the agent decide)
3. Should artifacts also be signed, or just module scripts? (Recommend: sign the script only for v1 — artifacts are content-addressed by hash already)
