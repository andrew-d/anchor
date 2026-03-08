# Module Signing - Human Test Plan

## Prerequisites
- Go 1.26.x installed
- `make ci` passes (confirms all automated tests pass)
- Two terminal windows available (one for server, one for agent)
- A Linux machine (or VM) for agent execution

## Phase 1: Key Generation and Signing CLI

| Step | Action | Expected |
|------|--------|----------|
| 1.1 | Run `go run ./cmd/anchor keygen -o /tmp/test-signing/mykey` | Exit code 0. Files `/tmp/test-signing/mykey.key` and `/tmp/test-signing/mykey.pub` created |
| 1.2 | Run `ls -la /tmp/test-signing/mykey.key` | Permissions show `-rw-------` (0600) |
| 1.3 | Run `ls -la /tmp/test-signing/mykey.pub` | Permissions show `-rw-r--r--` (0644) |
| 1.4 | Run `cat /tmp/test-signing/mykey.key` | Output begins with `-----BEGIN ANCHOR SIGNING KEY-----` and ends with `-----END ANCHOR SIGNING KEY-----` |
| 1.5 | Run `cat /tmp/test-signing/mykey.pub` | Output begins with `-----BEGIN ANCHOR PUBLIC KEY-----` and ends with `-----END ANCHOR PUBLIC KEY-----` |
| 1.6 | Create a test module file: `echo '#!/bin/sh\necho hello' > /tmp/test-signing/testmod` | File created |
| 1.7 | Run `go run ./cmd/anchor sign -k /tmp/test-signing/mykey.key /tmp/test-signing/testmod` | Exit code 0. File `/tmp/test-signing/testmod.sig` created |
| 1.8 | Run `cat /tmp/test-signing/testmod.sig` | Output is 128 hex characters (representing 64 bytes) |
| 1.9 | Run `go run ./cmd/anchor sign -k /nonexistent/key /tmp/test-signing/testmod` | Exit code 1 with error message about missing key file |
| 1.10 | Generate an OpenSSH ed25519 key: `ssh-keygen -t ed25519 -f /tmp/test-signing/sshkey -N ""` then run `go run ./cmd/anchor sign -k /tmp/test-signing/sshkey /tmp/test-signing/testmod` | Exit code 0, `.sig` file updated (OpenSSH key format accepted) |

## Phase 2: Server Signature Loading

| Step | Action | Expected |
|------|--------|----------|
| 2.1 | Set up a modules directory with a module `00_base` and a valid `00_base.sig` file. Start the server with that modules directory. | Server starts without errors, logs show module loaded |
| 2.2 | Place an orphan `.sig` file (e.g., `phantom.sig`) with no matching module script in the modules directory. Restart the server. | Server starts without errors. The orphan `.sig` is silently ignored. The module list does not include "phantom" |
| 2.3 | Create a module `01_broken` with a `.sig` file containing `ZZZZ` (invalid hex). Restart the server. | Server logs a warning about malformed signature for `01_broken`. The module appears in load errors, not in available modules |
| 2.4 | From a configured agent, trigger a checkin (or use curl against `/api/checkin`). Inspect the JSON response for a signed module. | Response includes `"signature":"<hex>"` for the signed module. Unsigned modules have no `signature` field in JSON |

## Phase 3: Agent Signature Verification

| Step | Action | Expected |
|------|--------|----------|
| 3.1 | Start agent with no `--verify-key` or `--verify-key-url` flags. Assign a mix of signed and unsigned modules. | All modules execute normally regardless of signature state. Agent logs show successful execution for all |
| 3.2 | Start agent with `--verify-key /tmp/test-signing/mykey.pub`. Assign a module signed with `mykey.key`. | Module executes successfully. Agent reports status "ok" or "changed" |
| 3.3 | Start agent with `--verify-key /tmp/test-signing/mykey.pub`. Assign an unsigned module (no `.sig` file). | Module reports error with "missing signature" in stderr. Module does not execute |
| 3.4 | Generate a second keypair (`mykey2`). Sign a module with `mykey2.key` but start agent with `--verify-key /tmp/test-signing/mykey.pub` (wrong key). | Module reports error with "signature verification failed" in stderr |
| 3.5 | Start agent with both `--verify-key /tmp/test-signing/mykey.pub` and `--verify-key /tmp/test-signing/mykey2.pub`. Assign a module signed with either key. | Module executes successfully (any trusted key in the set is sufficient) |

## Phase 4: URL Key Fetching

| Step | Action | Expected |
|------|--------|----------|
| 4.1 | Host the public key in authorized_keys format on an HTTP server (e.g., `python3 -m http.server`). Start agent with `--verify-key-url http://localhost:8000/mykey.pub`. | Agent fetches keys, logs success. Signed modules verified and executed. Cache file created at `{dataDir}/trusted-keys-cache` |
| 4.2 | Stop the HTTP key server. Restart the agent. | Agent logs a warning about fetch failure but falls back to cached keys. Signed modules still execute using cached keys |
| 4.3 | Delete the cache file and stop the HTTP key server. Restart the agent. | Agent logs warning about fetch failure and no cache available. All modules rejected with "no trusted keys available" |

## End-to-End: Full Signing Pipeline

1. Generate a keypair: `go run ./cmd/anchor keygen -o /tmp/e2e/signing-key`
2. Create a module script in the server's modules directory (e.g., `/etc/anchor-server/modules.d/00_hello`)
3. Sign the module: `go run ./cmd/anchor sign -k /tmp/e2e/signing-key.key /etc/anchor-server/modules.d/00_hello`
4. Verify `00_hello.sig` exists alongside the module
5. Start the server: `go run ./cmd/anchor server`
6. Assign the module to an agent via the web UI
7. Start the agent with `--verify-key /tmp/e2e/signing-key.pub`
8. Observe in the web UI that the module executes successfully with status "ok" or "changed"
9. Tamper with the module script content (change a character) without re-signing
10. Wait for next agent poll interval
11. Observe in the web UI that the module now reports "signature verification failed"
12. Re-sign the module: `go run ./cmd/anchor sign -k /tmp/e2e/signing-key.key /etc/anchor-server/modules.d/00_hello`
13. Wait for next agent poll interval
14. Observe in the web UI that the module executes successfully again

## Human Verification Required

| Criterion | Why Manual | Steps |
|-----------|------------|-------|
| AC6.5: Fetch timeout exceeding 10 seconds falls back to cache | Real 10s network timeout incompatible with fast CI; `synctest` fake time does not apply to real HTTP client timeouts | 1. Pre-populate `{dataDir}/trusted-keys-cache` with a valid ed25519 public key in authorized_keys format. 2. Start a listener that accepts connections but never responds: `nc -l -k -p 8888 > /dev/null`. 3. Start the agent with `--verify-key-url http://localhost:8888/keys`. 4. Wait approximately 10-15 seconds. 5. Observe agent logs showing fetch timeout/failure followed by cache fallback. 6. Verify signed modules execute using cached keys. **Note:** `TestFetchAndCacheKeys_Timeout` in `internal/agent/agent_test.go` already exercises this path automatically (waiting for the real 10s timeout), so manual verification is confirmatory. |

## Traceability

| Acceptance Criterion | Automated Test | Manual Step |
|----------------------|----------------|-------------|
| AC1.1 | `TestRunKeygen` | 1.1-1.5 |
| AC1.2 | `TestGenerateKeyAndPEMRoundTrip` | -- |
| AC1.3 | `TestSignAndVerify` | -- |
| AC1.4 | `TestRunKeygenUnwritablePath` | 1.9 |
| AC2.1 | `TestRunSignProducesSigFile` | 1.7-1.8 |
| AC2.2 | `TestParsePrivateKeyAnchorFormat` | 1.7 |
| AC2.3 | `TestParsePrivateKeySSHFormat` | 1.10 |
| AC2.4 | `TestRunSignMultipleModules` | -- |
| AC2.5 | `TestParsePrivateKeyRejectsNonEd25519` | -- |
| AC2.6 | `TestRunSignMissingKeyFile`, `TestRunSignNoKeyFlag` | 1.9 |
| AC3.1 | `TestSignatureLoading_SigFilesSkipped` | 2.2 |
| AC3.2 | `TestSignatureLoading_ValidSignature` | 2.1, 2.4 |
| AC3.3 | `TestSignatureLoading_NoSignatureFile` | 2.4 |
| AC3.4 | `TestCheckinWithSignedModule`, `TestCheckinWithUnsignedModule` | 2.4 |
| AC3.5 | `TestSignatureLoading_MalformedSignature`, `TestSignatureLoading_WrongLengthSignature` | 2.3 |
| AC3.6 | `TestSignatureLoading_OrphanSigFile` | 2.2 |
| AC4.1 | `TestIntegrationSigningWorkflow_NoVerifyFlagsExecutesAll` | 3.1 |
| AC4.2 | `TestIntegrationSigningWorkflow_SignedModuleExecutes` | 3.2 |
| AC4.3 | `TestVerifyModule_AC4_3_MultipleKeys` | 3.5 |
| AC4.4 | `TestIntegrationSigningWorkflow_MissingSignatureRejected` | 3.3 |
| AC4.5 | `TestIntegrationSigningWorkflow_WrongKeyRejected` | 3.4 |
| AC4.6 | `TestIntegrationSigningWorkflow_EmptyTrustSetRejectsAll` | 4.3 |
| AC5.1 | `TestParsePublicKeySSHFormat`, `TestResolveTrustSet_InlineSSHKey` | -- |
| AC5.2 | `TestResolveTrustSet_AnchorPEMFile` | -- |
| AC5.3 | `TestResolveTrustSet_SSHPublicKeyFile` | -- |
| AC5.4 | `TestVerifyConfigMultipleKeys`, `TestIntegrationSigningWorkflow_InlineKeysFromMultipleSources` | 3.5 |
| AC5.5 | `TestParsePublicKeysMixedTypes`, `TestResolveTrustSet_MixedKeyTypes` | -- |
| AC6.1 | `TestFetchAndCacheKeys_Success` | 4.1 |
| AC6.2 | `TestFetchAndCacheKeys_Success` | 4.1 |
| AC6.3 | `TestFetchAndCacheKeys_CacheFallback` | 4.2 |
| AC6.4 | `TestFetchAndCacheKeys_UpdateOnSuccess` | -- |
| AC6.5 | `TestFetchAndCacheKeys_Timeout` (~10s wall-clock) | AC6.5 human steps above |
| AC6.6 | `TestFetchAndCacheKeys_NoED25519Keys` | -- |
| AC6.7 | `TestFetchAndCacheKeys_EmptyTrustSet` | 4.3 |
