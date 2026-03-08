package agent

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"testing/synctest"

	"github.com/andrew-d/anchor/internal/api"
	"github.com/andrew-d/anchor/internal/signing"
	"golang.org/x/crypto/ssh"
)

// inlineSSHPubKey converts an ed25519.PublicKey to an inline SSH authorized_keys string
// suitable for passing as a --verify-key value.
func inlineSSHPubKey(t *testing.T, pub ed25519.PublicKey) string {
	t.Helper()
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub)))
}

// TestIntegrationSigningWorkflow_SignedModuleExecutes verifies that a validly signed
// module executes successfully (AC4.2, AC3.2, AC3.4).
func TestIntegrationSigningWorkflow_SignedModuleExecutes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		// Generate keypair
		privKey, pubKey, err := signing.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}

		// Create a module script
		script := "#!/bin/sh\necho 'hello'\nexit 0"

		// Sign the script
		sig := signing.Sign(privKey, []byte(script))
		sigHex := strings.ToUpper(hex.EncodeToString(sig))

		// Create agent with verify key configured
		keyStr := inlineSSHPubKey(t, pubKey)
		agent := New("http://fake", dataDir, VerifyConfig{
			Keys: []string{keyStr},
		})

		// Set up handler that returns signed module
		var reportedModules []string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{
							Name:      "test_module",
							Script:    script,
							Signature: sigHex,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				var req api.ReportRequest
				json.NewDecoder(r.Body).Decode(&req)
				reportedModules = append(reportedModules, req.ModuleName)

				resp := api.ReportResponse{OK: true}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Wait for checkin + verification + execution + report
		synctest.Wait()

		// Verify module was reported
		if len(reportedModules) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reportedModules))
		}
		if reportedModules[0] != "test_module" {
			t.Fatalf("expected module 'test_module', got %s", reportedModules[0])
		}
	})
}

// TestIntegrationSigningWorkflow_NoVerifyFlagsExecutesAll verifies that with empty
// VerifyConfig, all modules execute regardless of signature state (AC4.1).
func TestIntegrationSigningWorkflow_NoVerifyFlagsExecutesAll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		// Create agent with NO verify config (verification disabled)
		agent := New("http://fake", dataDir, VerifyConfig{})

		var reportedModules []string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{
							Name:      "signed_module",
							Script:    "#!/bin/sh\necho 'signed'\nexit 0",
							Signature: "deadbeef",
						},
						{
							Name:      "unsigned_module",
							Script:    "#!/bin/sh\necho 'unsigned'\nexit 0",
							Signature: "",
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				var req api.ReportRequest
				json.NewDecoder(r.Body).Decode(&req)
				reportedModules = append(reportedModules, req.ModuleName)

				resp := api.ReportResponse{OK: true}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Wait for completion
		synctest.Wait()

		// Both modules should execute and report (signed and unsigned)
		if len(reportedModules) != 2 {
			t.Fatalf("expected 2 reports, got %d", len(reportedModules))
		}

		// Check order (sorted by name)
		if reportedModules[0] != "signed_module" || reportedModules[1] != "unsigned_module" {
			t.Fatalf("expected [signed_module, unsigned_module], got %v", reportedModules)
		}
	})
}

// TestIntegrationSigningWorkflow_MissingSignatureRejected verifies that a module
// with empty signature is rejected when verification is enabled (AC4.4).
func TestIntegrationSigningWorkflow_MissingSignatureRejected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		// Generate keypair
		_, pubKey, err := signing.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}

		// Create agent with verify key configured
		keyStr := inlineSSHPubKey(t, pubKey)
		agent := New("http://fake", dataDir, VerifyConfig{
			Keys: []string{keyStr},
		})

		var reportedModules []string
		var reportErrors []string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{
							Name:      "unsigned_module",
							Script:    "#!/bin/sh\necho 'test'\nexit 0",
							Signature: "", // Empty signature
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				var req api.ReportRequest
				json.NewDecoder(r.Body).Decode(&req)
				reportedModules = append(reportedModules, req.ModuleName)
				reportErrors = append(reportErrors, req.Stderr)

				resp := api.ReportResponse{OK: true}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Wait for completion
		synctest.Wait()

		// Module should be reported with error status
		if len(reportedModules) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reportedModules))
		}
		if reportedModules[0] != "unsigned_module" {
			t.Fatalf("expected module 'unsigned_module', got %s", reportedModules[0])
		}

		// Error should mention "missing signature"
		if len(reportErrors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(reportErrors))
		}
		if !strings.Contains(reportErrors[0], "missing signature") {
			t.Fatalf("expected error containing 'missing signature', got: %s", reportErrors[0])
		}
	})
}

// TestIntegrationSigningWorkflow_WrongKeyRejected verifies that a module signed
// with one key is rejected when verification is configured with a different key (AC4.5).
func TestIntegrationSigningWorkflow_WrongKeyRejected(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		// Generate two different key pairs
		privKey1, _, err := signing.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey 1 failed: %v", err)
		}

		_, pubKey2, err := signing.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey 2 failed: %v", err)
		}

		// Create and sign module with key1
		script := "#!/bin/sh\necho 'test'\nexit 0"
		sig := signing.Sign(privKey1, []byte(script))
		sigHex := strings.ToUpper(hex.EncodeToString(sig))

		// Create agent with key2 (wrong key)
		keyStr := inlineSSHPubKey(t, pubKey2)
		agent := New("http://fake", dataDir, VerifyConfig{
			Keys: []string{keyStr},
		})

		var reportedModules []string
		var reportErrors []string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{
							Name:      "test_module",
							Script:    script,
							Signature: sigHex,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				var req api.ReportRequest
				json.NewDecoder(r.Body).Decode(&req)
				reportedModules = append(reportedModules, req.ModuleName)
				reportErrors = append(reportErrors, req.Stderr)

				resp := api.ReportResponse{OK: true}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Wait for completion
		synctest.Wait()

		// Module should be reported with error
		if len(reportedModules) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reportedModules))
		}

		// Error should mention "signature verification failed"
		if len(reportErrors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(reportErrors))
		}
		if !strings.Contains(reportErrors[0], "signature verification failed") {
			t.Fatalf("expected error containing 'signature verification failed', got: %s", reportErrors[0])
		}
	})
}

// TestIntegrationSigningWorkflow_MixedSignedUnsigned verifies that when verification
// is enabled, signed modules execute and unsigned modules error (with both reported).
func TestIntegrationSigningWorkflow_MixedSignedUnsigned(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		// Generate keypair
		privKey, pubKey, err := signing.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}

		// Create signed script
		signedScript := "#!/bin/sh\necho 'signed'\nexit 0"
		sig := signing.Sign(privKey, []byte(signedScript))
		sigHex := strings.ToUpper(hex.EncodeToString(sig))

		// Create agent with verify key
		keyStr := inlineSSHPubKey(t, pubKey)
		agent := New("http://fake", dataDir, VerifyConfig{
			Keys: []string{keyStr},
		})

		var reportedModules []string
		var reportStatuses []string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{
							Name:      "signed_module",
							Script:    signedScript,
							Signature: sigHex,
						},
						{
							Name:      "unsigned_module",
							Script:    "#!/bin/sh\necho 'unsigned'\nexit 0",
							Signature: "",
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				var req api.ReportRequest
				json.NewDecoder(r.Body).Decode(&req)
				reportedModules = append(reportedModules, req.ModuleName)
				reportStatuses = append(reportStatuses, req.Status)

				resp := api.ReportResponse{OK: true}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Wait for completion
		synctest.Wait()

		// Both modules should be reported
		if len(reportedModules) != 2 {
			t.Fatalf("expected 2 reports, got %d: %v", len(reportedModules), reportedModules)
		}

		// Verify order and statuses (sorted by name)
		if reportedModules[0] != "signed_module" || reportedModules[1] != "unsigned_module" {
			t.Fatalf("expected [signed_module, unsigned_module], got %v", reportedModules)
		}

		// Signed module should succeed, unsigned should fail
		if reportStatuses[0] != "ok" && reportStatuses[0] != "changed" {
			t.Fatalf("expected signed_module to succeed, got status: %s", reportStatuses[0])
		}
		if reportStatuses[1] != "error" {
			t.Fatalf("expected unsigned_module to error, got status: %s", reportStatuses[1])
		}
	})
}

// TestIntegrationSigningWorkflow_EmptyTrustSetRejectsAll verifies that when
// verification is enabled but no trusted keys are available, all modules error (AC4.6).
func TestIntegrationSigningWorkflow_EmptyTrustSetRejectsAll(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		// Generate a valid key pair to sign the module
		privKey, _, err := signing.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey failed: %v", err)
		}

		script := "#!/bin/sh\necho 'test'\nexit 0"
		sig := signing.Sign(privKey, []byte(script))
		sigHex := strings.ToUpper(hex.EncodeToString(sig))

		// Create agent with KeyURL pointing to unreachable server (no inline keys, no cache)
		// This causes verifyEnabled=true but with empty trust set after resolution
		agent := New("http://fake", dataDir, VerifyConfig{
			KeyURL: "http://unreachable.invalid:9999/keys",
		})

		var reportedModules []string
		var reportErrors []string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{
							Name:      "test_module",
							Script:    script,
							Signature: sigHex,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				var req api.ReportRequest
				json.NewDecoder(r.Body).Decode(&req)
				reportedModules = append(reportedModules, req.ModuleName)
				reportErrors = append(reportErrors, req.Stderr)

				resp := api.ReportResponse{OK: true}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Wait for completion
		synctest.Wait()

		// Module should be reported with error
		if len(reportedModules) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reportedModules))
		}

		// Error should mention "no trusted keys available"
		if len(reportErrors) != 1 {
			t.Fatalf("expected 1 error, got %d", len(reportErrors))
		}
		if !strings.Contains(reportErrors[0], "no trusted keys available") {
			t.Fatalf("expected error containing 'no trusted keys available', got: %s", reportErrors[0])
		}
	})
}

// TestIntegrationSigningWorkflow_InlineKeysFromMultipleSources verifies that
// keys from multiple sources (inline SSH keys) work together (AC5.4, AC5.5).
// This is a simpler variant that avoids URL fetching in the synctest bubble.
func TestIntegrationSigningWorkflow_InlineKeysFromMultipleSources(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		// Generate two keypairs
		privKey1, pubKey1, err := signing.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey 1 failed: %v", err)
		}

		_, pubKey2, err := signing.GenerateKey()
		if err != nil {
			t.Fatalf("GenerateKey 2 failed: %v", err)
		}

		// Marshal both keys to SSH format
		sshPub1, err := ssh.NewPublicKey(pubKey1)
		if err != nil {
			t.Fatalf("NewPublicKey 1 failed: %v", err)
		}

		sshPub2, err := ssh.NewPublicKey(pubKey2)
		if err != nil {
			t.Fatalf("NewPublicKey 2 failed: %v", err)
		}

		key1Str := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub1)))
		key2Str := strings.TrimSpace(string(ssh.MarshalAuthorizedKey(sshPub2)))

		// Create and sign module with key1
		script := "#!/bin/sh\necho 'test'\nexit 0"
		sig := signing.Sign(privKey1, []byte(script))
		sigHex := strings.ToUpper(hex.EncodeToString(sig))

		// Create agent with both keys in the verify config
		agent := New("http://fake", dataDir, VerifyConfig{
			Keys: []string{key1Str, key2Str},
		})

		var reportedModules []string
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{
							Name:      "test_module",
							Script:    script,
							Signature: sigHex,
						},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				var req api.ReportRequest
				json.NewDecoder(r.Body).Decode(&req)
				reportedModules = append(reportedModules, req.ModuleName)

				resp := api.ReportResponse{OK: true}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Wait for completion
		synctest.Wait()

		// Module should be reported successfully (key1 matched)
		if len(reportedModules) != 1 {
			t.Fatalf("expected 1 report, got %d", len(reportedModules))
		}
	})
}
