package agent

import (
	"crypto/ed25519"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/andrew-d/anchor/internal/api"
	"github.com/andrew-d/anchor/internal/signing"
	"golang.org/x/crypto/ssh"
)

// handlerTransport is an http.RoundTripper that calls an http.Handler
// directly in-process, avoiding real network I/O. This allows tests
// to run inside a synctest bubble where network-blocked goroutines
// would prevent fake time from advancing.
type handlerTransport struct {
	handler http.Handler
}

func (t *handlerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}

// TestReadOrCreateUUID_GeneratesNewUUID verifies that a new UUID is generated and persisted
// when the agent-id file doesn't exist (AC6.1).
func TestReadOrCreateUUID_GeneratesNewUUID(t *testing.T) {
	tmpDir := t.TempDir()
	uuid, err := readOrCreateUUID(tmpDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify UUID is non-empty
	if uuid == "" {
		t.Fatal("UUID is empty")
	}

	// Verify UUID format is v4 (8-4-4-4-12 hex with version/variant bits)
	uuidRegex := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuidRegex.MatchString(uuid) {
		t.Fatalf("UUID does not match v4 format: %s", uuid)
	}

	// Verify the file was created
	filePath := filepath.Join(tmpDir, "agent-id")
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		t.Fatalf("agent-id file was not created at %s", filePath)
	}

	// Verify the file contains the same UUID
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != uuid {
		t.Fatalf("file content does not match returned UUID: got %s, expected %s", string(content), uuid)
	}
}

// TestReadOrCreateUUID_ReadsExistingUUID verifies that an existing UUID is read from file
// and not overwritten (AC6.2).
func TestReadOrCreateUUID_ReadsExistingUUID(t *testing.T) {
	tmpDir := t.TempDir()
	knownUUID := "12345678-1234-4234-8234-123456789012"

	// Write a known UUID to the file
	filePath := filepath.Join(tmpDir, "agent-id")
	if err := os.WriteFile(filePath, []byte(knownUUID), 0644); err != nil {
		t.Fatalf("failed to write test UUID: %v", err)
	}

	// Call readOrCreateUUID
	uuid, err := readOrCreateUUID(tmpDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify it returned the existing UUID
	if uuid != knownUUID {
		t.Fatalf("readOrCreateUUID did not return existing UUID: got %s, expected %s", uuid, knownUUID)
	}

	// Verify the file was not modified
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != knownUUID {
		t.Fatalf("file was modified: got %s, expected %s", string(content), knownUUID)
	}
}

// TestReadOrCreateUUID_CreatesParentDir verifies that parent directories are created if they don't exist.
func TestReadOrCreateUUID_CreatesParentDir(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "nested", "path", "to", "datadir")

	uuid, err := readOrCreateUUID(nestedDir)
	if err != nil {
		t.Fatalf("readOrCreateUUID failed: %v", err)
	}

	// Verify UUID is non-empty
	if uuid == "" {
		t.Fatal("UUID is empty")
	}

	// Verify the directory was created
	if _, err := os.Stat(nestedDir); os.IsNotExist(err) {
		t.Fatalf("parent directories were not created at %s", nestedDir)
	}

	// Verify the file exists and contains the UUID
	filePath := filepath.Join(nestedDir, "agent-id")
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read agent-id file: %v", err)
	}
	if string(content) != uuid {
		t.Fatalf("file content does not match returned UUID: got %s, expected %s", string(content), uuid)
	}
}

// TestAgentPollingLoop_CheckinServerError verifies that the agent retries gracefully
// when the server returns a non-200 status on checkin, without attempting to JSON-decode
// the error body.
func TestAgentPollingLoop_CheckinServerError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		var checkinCount int
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				checkinCount++
				if checkinCount <= 2 {
					// Plain text error (not JSON) — tests that agent doesn't try to JSON-decode error bodies
					http.Error(w, "database is locked", http.StatusInternalServerError)
					return
				}
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules:             []api.CheckinModule{},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent := New("http://fake", dataDir, VerifyConfig{})
		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Sleep past both 5s retry delays; fake time advances instantly.
		time.Sleep(15 * time.Second)
		synctest.Wait()

		if checkinCount < 3 {
			t.Fatalf("expected at least 3 checkin attempts, got %d", checkinCount)
		}
	})
}

// TestAgentPollingLoop_ReportsModulesIndividually verifies that the agent checks in,
// receives modules, executes them in sorted order, and reports each result individually (AC2.4).
func TestAgentPollingLoop_ReportsModulesIndividually(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		var checkinCount int
		var reportCount int
		var reportedModules []string

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				checkinCount++
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{Name: "02_pkg", Script: "#!/bin/sh\necho 'install packages'\nexit 0"},
						{Name: "01_base", Script: "#!/bin/sh\necho 'configure base'\nexit 0"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				reportCount++
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

		agent := New("http://fake", dataDir, VerifyConfig{})
		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Wait for the agent to complete checkin + module execution + reports,
		// then block on the 300s poll sleep.
		synctest.Wait()

		if checkinCount == 0 {
			t.Fatal("checkin was never called")
		}
		if reportCount != 2 {
			t.Fatalf("expected 2 reports, got %d", reportCount)
		}

		// The server returns [02_pkg, 01_base], but after sorting should be [01_base, 02_pkg]
		expectedOrder := []string{"01_base", "02_pkg"}
		for i, expectedModule := range expectedOrder {
			if i >= len(reportedModules) {
				t.Fatalf("not enough modules reported: expected %v, got %v", expectedOrder, reportedModules)
			}
			if reportedModules[i] != expectedModule {
				t.Fatalf("module order mismatch: expected %v at index %d, got %s", expectedOrder, i, reportedModules[i])
			}
		}
	})
}

// TestAgentPollingLoop_StopsOnReportFailure verifies that the agent stops executing
// remaining modules if a report request fails (AC2.6).
func TestAgentPollingLoop_StopsOnReportFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		dataDir := t.TempDir()

		var reportCount int
		var reportedModules []string

		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/checkin" && r.Method == "POST" {
				resp := api.CheckinResponse{
					PollIntervalSeconds: 300,
					Modules: []api.CheckinModule{
						{Name: "01_first", Script: "#!/bin/sh\necho 'first'\nexit 0"},
						{Name: "02_second", Script: "#!/bin/sh\necho 'second'\nexit 0"},
						{Name: "03_third", Script: "#!/bin/sh\necho 'third'\nexit 0"},
					},
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else if r.URL.Path == "/api/report" && r.Method == "POST" {
				reportCount++
				var req api.ReportRequest
				json.NewDecoder(r.Body).Decode(&req)
				reportedModules = append(reportedModules, req.ModuleName)

				// Fail on the second report
				if reportCount == 2 {
					http.Error(w, "internal server error", http.StatusInternalServerError)
					return
				}

				resp := api.ReportResponse{OK: true}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(resp)
			} else {
				http.NotFound(w, r)
			}
		})

		agent := New("http://fake", dataDir, VerifyConfig{})
		agent.httpClient = &http.Client{Transport: &handlerTransport{handler: handler}}

		go agent.Run(t.Context())

		// Agent does checkin, runs modules, reports #1 (ok), reports #2 (fail),
		// then enters retryDelay sleep.
		synctest.Wait()

		// Verify only 2 reports were attempted (first succeeded, second failed, third not attempted)
		if reportCount != 2 {
			t.Fatalf("expected 2 reports (first succeeded, second failed), got %d", reportCount)
		}

		if len(reportedModules) != 2 {
			t.Fatalf("expected 2 modules to have been attempted to report, got %d: %v", len(reportedModules), reportedModules)
		}

		if reportedModules[0] != "01_first" {
			t.Fatalf("expected first module to be '01_first', got %s", reportedModules[0])
		}
		if reportedModules[1] != "02_second" {
			t.Fatalf("expected second module to be '02_second', got %s", reportedModules[1])
		}

		// The key assertion: third module must not have been reported after second report failed
		for _, moduleName := range reportedModules {
			if moduleName == "03_third" {
				t.Fatalf("third module should not have been reported after second report failed")
			}
		}
	})
}

// TestVerifyConfigMultipleKeys verifies that multiple verify keys in VerifyConfig
// combine into a single trust set with verifyEnabled=true (AC5.4).
func TestVerifyConfigMultipleKeys(t *testing.T) {
	dataDir := t.TempDir()
	keys := []string{"key1", "key2", "key3"}

	a := New("http://fake", dataDir, VerifyConfig{
		Keys: keys,
	})

	if !a.verifyEnabled {
		t.Fatal("expected verifyEnabled to be true when keys are provided")
	}

	if len(a.verifyKeys) != 3 {
		t.Fatalf("expected 3 keys, got %d", len(a.verifyKeys))
	}

	for i, key := range a.verifyKeys {
		if key != keys[i] {
			t.Fatalf("key mismatch at index %d: expected %s, got %s", i, keys[i], key)
		}
	}
}

// TestVerifyConfigKeyURL verifies that setting KeyURL enables verification
// (AC5.4).
func TestVerifyConfigKeyURL(t *testing.T) {
	dataDir := t.TempDir()

	a := New("http://fake", dataDir, VerifyConfig{
		KeyURL: "https://example.com/keys",
	})

	if !a.verifyEnabled {
		t.Fatal("expected verifyEnabled to be true when KeyURL is provided")
	}

	if a.verifyKeyURL != "https://example.com/keys" {
		t.Fatalf("expected KeyURL to be set, got %s", a.verifyKeyURL)
	}
}

// TestVerifyConfigDisabled verifies that with empty config, verification is disabled
// (AC4.1 prerequisite).
func TestVerifyConfigDisabled(t *testing.T) {
	dataDir := t.TempDir()

	a := New("http://fake", dataDir, VerifyConfig{})

	if a.verifyEnabled {
		t.Fatal("expected verifyEnabled to be false when no config is provided")
	}

	if len(a.verifyKeys) != 0 {
		t.Fatalf("expected 0 keys, got %d", len(a.verifyKeys))
	}

	if a.verifyKeyURL != "" {
		t.Fatalf("expected empty KeyURL, got %s", a.verifyKeyURL)
	}
}

// TestResolveTrustSet_InlineSSHKey tests AC5.1: inline ssh-ed25519 key parsing.
func TestResolveTrustSet_InlineSSHKey(t *testing.T) {
	dataDir := t.TempDir()

	// Generate a test key
	_, origPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Marshal it to SSH authorized_keys format
	sshPub, err := ssh.NewPublicKey(origPriv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshKeyLine := ssh.MarshalAuthorizedKey(sshPub)

	a := New("http://fake", dataDir, VerifyConfig{
		Keys: []string{string(sshKeyLine)},
	})

	keys := a.resolveTrustSet()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	if !strings.Contains(string(sshKeyLine), "ssh-ed25519") {
		t.Fatal("test setup error: key should be in ssh-ed25519 format")
	}
}

// TestResolveTrustSet_AnchorPEMFile tests AC5.2: anchor PEM file parsing.
func TestResolveTrustSet_AnchorPEMFile(t *testing.T) {
	dataDir := t.TempDir()

	// Generate a test key
	_, pubKey, err := signing.GenerateKey()
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Marshal it to Anchor PEM format
	pubPEM := signing.MarshalPublicKey(pubKey)

	// Write to a temp file
	keyFile := filepath.Join(dataDir, "test.pub")
	if err := os.WriteFile(keyFile, pubPEM, 0644); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	a := New("http://fake", dataDir, VerifyConfig{
		Keys: []string{keyFile},
	})

	keys := a.resolveTrustSet()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	if string(keys[0]) != string(pubKey) {
		t.Fatal("parsed key does not match original key")
	}
}

// TestResolveTrustSet_SSHPublicKeyFile tests AC5.3: SSH public key file parsing.
func TestResolveTrustSet_SSHPublicKeyFile(t *testing.T) {
	dataDir := t.TempDir()

	// Generate a test key
	_, origPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	expectedPubKey := origPriv.Public().(ed25519.PublicKey)

	// Marshal it to SSH authorized_keys format
	sshPub, err := ssh.NewPublicKey(origPriv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshKeyLine := ssh.MarshalAuthorizedKey(sshPub)

	// Write to a temp file (authorized_keys format)
	keyFile := filepath.Join(dataDir, "authorized_keys")
	if err := os.WriteFile(keyFile, sshKeyLine, 0644); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	a := New("http://fake", dataDir, VerifyConfig{
		Keys: []string{keyFile},
	})

	keys := a.resolveTrustSet()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key, got %d", len(keys))
	}

	if string(keys[0]) != string(expectedPubKey) {
		t.Fatal("parsed key does not match original key")
	}
}

// TestResolveTrustSet_MixedKeyTypes tests AC5.5: file with RSA + ed25519 only returns ed25519.
func TestResolveTrustSet_MixedKeyTypes(t *testing.T) {
	dataDir := t.TempDir()

	// Generate an ed25519 key
	_, origPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	expectedPubKey := origPriv.Public().(ed25519.PublicKey)

	// Marshal to SSH format
	sshPub, err := ssh.NewPublicKey(origPriv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshKeyLine := ssh.MarshalAuthorizedKey(sshPub)

	// Create a fake SSH file with RSA and ed25519 keys
	rsaKey := []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC7/test")
	mixedContent := append(rsaKey, '\n')
	mixedContent = append(mixedContent, sshKeyLine...)

	keyFile := filepath.Join(dataDir, "mixed_keys")
	if err := os.WriteFile(keyFile, mixedContent, 0644); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	a := New("http://fake", dataDir, VerifyConfig{
		Keys: []string{keyFile},
	})

	keys := a.resolveTrustSet()
	// Should only get the ed25519 key, RSA silently skipped
	if len(keys) != 1 {
		t.Fatalf("expected 1 key (only ed25519), got %d", len(keys))
	}

	if string(keys[0]) != string(expectedPubKey) {
		t.Fatal("parsed key does not match original ed25519 key")
	}
}

// TestFetchAndCacheKeys_Success tests AC6.1 and AC6.2: fetch succeeds and cache is updated.
func TestFetchAndCacheKeys_Success(t *testing.T) {
	dataDir := t.TempDir()

	// Generate test keys
	_, origPriv1, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	_, origPriv2, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	// Marshal to SSH format
	sshPub1, err := ssh.NewPublicKey(origPriv1.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshPub2, err := ssh.NewPublicKey(origPriv2.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshKey1 := ssh.MarshalAuthorizedKey(sshPub1)
	sshKey2 := ssh.MarshalAuthorizedKey(sshPub2)
	keysContent := append(sshKey1, sshKey2...)

	// Create an httptest server that returns the keys
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(keysContent)
	}))
	defer server.Close()

	a := New(server.URL, dataDir, VerifyConfig{
		KeyURL: server.URL + "/keys",
	})

	keys := a.fetchAndCacheKeys()
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys from fetch, got %d", len(keys))
	}

	// Verify cache file was created and contains the same content
	cachePath := filepath.Join(dataDir, "trusted-keys-cache")
	cacheContent, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not created: %v", err)
	}

	if string(cacheContent) != string(keysContent) {
		t.Fatal("cache content does not match fetched content")
	}
}

// TestFetchAndCacheKeys_CacheFallback tests AC6.3: fetch failure falls back to cache.
func TestFetchAndCacheKeys_CacheFallback(t *testing.T) {
	dataDir := t.TempDir()

	// Pre-populate the cache with test keys
	_, origPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	expectedPubKey := origPriv.Public().(ed25519.PublicKey)

	// Marshal to SSH format
	sshPub, err := ssh.NewPublicKey(origPriv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshKey := ssh.MarshalAuthorizedKey(sshPub)
	cachePath := filepath.Join(dataDir, "trusted-keys-cache")
	if err := os.WriteFile(cachePath, sshKey, 0644); err != nil {
		t.Fatalf("failed to write cache: %v", err)
	}

	// Create a server that fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	a := New(server.URL, dataDir, VerifyConfig{
		KeyURL: server.URL + "/keys",
	})

	keys := a.fetchAndCacheKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 key from cache fallback, got %d", len(keys))
	}

	if string(keys[0]) != string(expectedPubKey) {
		t.Fatal("cached key does not match original key")
	}
}

// TestFetchAndCacheKeys_UpdateOnSuccess tests AC6.4: cache is updated on successful fetch.
func TestFetchAndCacheKeys_UpdateOnSuccess(t *testing.T) {
	dataDir := t.TempDir()

	// Pre-populate cache with old keys
	_, oldPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	oldSSHPub, err := ssh.NewPublicKey(oldPriv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	oldSSHKey := ssh.MarshalAuthorizedKey(oldSSHPub)
	cachePath := filepath.Join(dataDir, "trusted-keys-cache")
	if err := os.WriteFile(cachePath, oldSSHKey, 0644); err != nil {
		t.Fatalf("failed to write initial cache: %v", err)
	}

	// Generate new keys for server to return
	_, newPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	expectedNewKey := newPriv.Public().(ed25519.PublicKey)

	newSSHPub, err := ssh.NewPublicKey(newPriv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	newSSHKey := ssh.MarshalAuthorizedKey(newSSHPub)

	// Create server that returns new keys
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(newSSHKey)
	}))
	defer server.Close()

	a := New(server.URL, dataDir, VerifyConfig{
		KeyURL: server.URL + "/keys",
	})

	keys := a.fetchAndCacheKeys()
	if len(keys) != 1 {
		t.Fatalf("expected 1 new key, got %d", len(keys))
	}

	if string(keys[0]) != string(expectedNewKey) {
		t.Fatal("fetched key does not match expected new key")
	}

	// Verify cache was updated with new content
	cacheContent, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("failed to read cache: %v", err)
	}

	if string(cacheContent) != string(newSSHKey) {
		t.Fatal("cache was not updated with new content")
	}
}

// TestFetchAndCacheKeys_Timeout tests AC6.5: fetch timeout falls back to cache.
func TestFetchAndCacheKeys_Timeout(t *testing.T) {
	dataDir := t.TempDir()

	// Pre-populate cache with test keys
	_, origPriv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	expectedPubKey := origPriv.Public().(ed25519.PublicKey)

	// Marshal to SSH format
	sshPub, err := ssh.NewPublicKey(origPriv.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshKey := ssh.MarshalAuthorizedKey(sshPub)
	cachePath := filepath.Join(dataDir, "trusted-keys-cache")
	if err := os.WriteFile(cachePath, sshKey, 0644); err != nil {
		t.Fatalf("failed to write cache: %v", err)
	}

	// Create a channel-based handler that simulates a timeout without actually sleeping
	done := make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond, simulating a timeout
		<-done
	})

	server := httptest.NewServer(handler)

	a := New(server.URL, dataDir, VerifyConfig{
		KeyURL: server.URL + "/keys",
	})

	// fetchAndCacheKeys should timeout after 10 seconds and fall back to cache
	keys := a.fetchAndCacheKeys()
	close(done) // Clean up the handler
	server.Close()

	if len(keys) != 1 {
		t.Fatalf("expected 1 key from cache fallback, got %d", len(keys))
	}

	if string(keys[0]) != string(expectedPubKey) {
		t.Fatal("cached key does not match original key")
	}
}

// TestFetchAndCacheKeys_NoED25519Keys tests AC6.6: URL returns zero ed25519 keys (warning logged, zero keys).
func TestFetchAndCacheKeys_NoED25519Keys(t *testing.T) {
	dataDir := t.TempDir()

	// Create a server that returns only RSA keys (no ed25519)
	rsaKeys := []byte("ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABAQC7/test\n")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write(rsaKeys)
	}))
	defer server.Close()

	a := New(server.URL, dataDir, VerifyConfig{
		KeyURL: server.URL + "/keys",
	})

	keys := a.fetchAndCacheKeys()
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys (no ed25519), got %d", len(keys))
	}

	// Verify cache file was still updated (even with zero ed25519 keys)
	cachePath := filepath.Join(dataDir, "trusted-keys-cache")
	cacheContent, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatalf("cache file not created: %v", err)
	}

	if string(cacheContent) != string(rsaKeys) {
		t.Fatal("cache was not updated with fetched content (even though no ed25519 keys)")
	}
}

// TestFetchAndCacheKeys_EmptyTrustSet tests AC6.7: fetch fails with no cache and no inline keys.
func TestFetchAndCacheKeys_EmptyTrustSet(t *testing.T) {
	dataDir := t.TempDir()

	// Create a server that fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	a := New(server.URL, dataDir, VerifyConfig{
		KeyURL: server.URL + "/keys",
	})

	// No cache exists, no inline keys
	keys := a.fetchAndCacheKeys()
	if len(keys) != 0 {
		t.Fatalf("expected 0 keys (no cache, fetch failed), got %d", len(keys))
	}
}

// TestLoadCachedKeys_Success tests loading cached keys from file.
func TestLoadCachedKeys_Success(t *testing.T) {
	dataDir := t.TempDir()

	// Generate test keys
	_, origPriv1, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	_, origPriv2, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}

	expectedKey1 := origPriv1.Public().(ed25519.PublicKey)
	expectedKey2 := origPriv2.Public().(ed25519.PublicKey)

	// Marshal to SSH format
	sshPub1, err := ssh.NewPublicKey(origPriv1.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshPub2, err := ssh.NewPublicKey(origPriv2.Public())
	if err != nil {
		t.Fatalf("NewPublicKey failed: %v", err)
	}

	sshKey1 := ssh.MarshalAuthorizedKey(sshPub1)
	sshKey2 := ssh.MarshalAuthorizedKey(sshPub2)
	keysContent := append(sshKey1, sshKey2...)

	// Write cache file
	cachePath := filepath.Join(dataDir, "trusted-keys-cache")
	if err := os.WriteFile(cachePath, keysContent, 0644); err != nil {
		t.Fatalf("failed to write cache: %v", err)
	}

	a := New("http://fake", dataDir, VerifyConfig{})
	keys := a.loadCachedKeys(cachePath)

	if len(keys) != 2 {
		t.Fatalf("expected 2 keys from cache, got %d", len(keys))
	}

	if string(keys[0]) != string(expectedKey1) || string(keys[1]) != string(expectedKey2) {
		t.Fatal("cached keys do not match original keys")
	}
}

// TestLoadCachedKeys_FileNotFound tests loading cached keys when file doesn't exist.
func TestLoadCachedKeys_FileNotFound(t *testing.T) {
	dataDir := t.TempDir()

	a := New("http://fake", dataDir, VerifyConfig{})
	cachePath := filepath.Join(dataDir, "nonexistent")
	keys := a.loadCachedKeys(cachePath)

	if len(keys) != 0 {
		t.Fatalf("expected 0 keys when cache file missing, got %d", len(keys))
	}
}
