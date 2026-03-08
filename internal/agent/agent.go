package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/andrew-d/anchor/internal/api"
	"github.com/andrew-d/anchor/internal/signing"
	"github.com/andrew-d/anchor/internal/sysinfo"
)

// Agent is the anchor agent that checks in with the server.
type Agent struct {
	serverURL  string
	dataDir    string
	retryDelay time.Duration
	httpClient *http.Client

	// reload is signalled to interrupt a poll sleep and trigger an
	// immediate checkin cycle (e.g. on SIGHUP).
	reload chan struct{}

	// verifyKeys are inline or file-based public keys for signature verification.
	// Empty if no verification flags specified (verification disabled).
	verifyKeys []string

	// verifyKeyURL is a URL to fetch SSH authorized_keys format keys from.
	// Empty if not configured.
	verifyKeyURL string

	// verifyEnabled is true if any --verify-key or --verify-key-url flag
	// was specified. Determines whether verification is active.
	verifyEnabled bool
}

// VerifyConfig holds optional signature verification settings.
type VerifyConfig struct {
	Keys   []string // inline keys or file paths
	KeyURL string   // URL to fetch keys from
}

// New creates a new Agent.
func New(serverURL string, dataDir string, verify VerifyConfig) *Agent {
	return &Agent{
		serverURL:     serverURL,
		dataDir:       dataDir,
		retryDelay:    retryDelay,
		httpClient:    http.DefaultClient,
		reload:        make(chan struct{}, 1),
		verifyKeys:    verify.Keys,
		verifyKeyURL:  verify.KeyURL,
		verifyEnabled: len(verify.Keys) > 0 || verify.KeyURL != "",
	}
}

// postReport sends a module result report to the server. Returns true on success,
// false on failure (logged internally).
func (a *Agent) postReport(reportReq api.ReportRequest) bool {
	reportURL := a.serverURL + "/api/report"
	reportBody, err := json.Marshal(reportReq)
	if err != nil {
		slog.Error("failed to marshal report request", "error", err)
		return false
	}

	reportResp, err := a.httpClient.Post(reportURL, "application/json", bytes.NewReader(reportBody))
	if err != nil {
		slog.Error("report request failed", "error", err, "url", reportURL, "module", reportReq.ModuleName)
		return false
	}

	io.Copy(io.Discard, reportResp.Body)
	reportResp.Body.Close()

	if reportResp.StatusCode != http.StatusOK && reportResp.StatusCode != http.StatusCreated {
		slog.Error("report request returned non-OK status", "status", reportResp.StatusCode, "module", reportReq.ModuleName)
		return false
	}

	return true
}

// sleep waits for the given duration or until the context is cancelled.
// Returns true if the duration elapsed, false if the context was cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-time.After(d):
		return true
	}
}

const retryDelay = 5 * time.Second

// pollSleep waits for the given duration, returning early if the context is
// cancelled or the reload channel is signalled. Returns false only when the
// context is cancelled (meaning the agent should shut down).
func (a *Agent) pollSleep(ctx context.Context, d time.Duration) bool {
	select {
	case <-ctx.Done():
		return false
	case <-a.reload:
		slog.Info("reload signal received, running immediately")
		return true
	case <-time.After(d):
		return true
	}
}

// TriggerRun interrupts the current poll sleep and causes the agent to
// perform an immediate checkin/execute cycle. Safe to call from any goroutine.
// If a trigger is already pending, the call is a no-op.
func (a *Agent) TriggerRun() {
	select {
	case a.reload <- struct{}{}:
	default:
	}
}

// Run performs the agent polling loop, checking in with the server, executing modules, and reporting results.
// It accepts a context for cancellation (allows clean shutdown and testability).
func (a *Agent) Run(ctx context.Context) error {
	slog.Info("agent starting", "server_url", a.serverURL, "data_dir", a.dataDir)

	// Read or create UUID
	agentID, err := readOrCreateUUID(a.dataDir)
	if err != nil {
		slog.Error("failed to read or create agent UUID", "error", err)
		return err
	}
	shortID := agentID[len(agentID)-8:]
	slog.SetDefault(slog.Default().With("agent_id", shortID))
	slog.Info("agent UUID", "id", agentID)

	// Gather system info
	sysInfo := sysinfo.GatherSystemInfo()
	slog.Debug("gathered system info", "hostname", sysInfo.Hostname, "os", sysInfo.OS, "arch", sysInfo.Arch, "distro", sysInfo.Distro)

	// Polling loop
	for ctx.Err() == nil {
		// Build checkin request
		checkinReq := api.CheckinRequest{
			ID:       agentID,
			Hostname: sysInfo.Hostname,
			OS:       sysInfo.OS,
			Arch:     sysInfo.Arch,
			Distro:   sysInfo.Distro,
		}

		// POST to server /api/checkin
		checkinURL := a.serverURL + "/api/checkin"
		reqBody, err := json.Marshal(checkinReq)
		if err != nil {
			slog.Error("failed to marshal checkin request", "error", err)
			if !sleep(ctx, a.retryDelay) {
				break
			}
			continue
		}

		resp, err := a.httpClient.Post(checkinURL, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			slog.Error("checkin request failed", "error", err, "url", checkinURL)
			if !sleep(ctx, a.retryDelay) {
				break
			}
			continue
		}

		// Check for non-OK status before decoding JSON
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			slog.Error("checkin returned non-OK status", "status", resp.StatusCode, "body", strings.TrimSpace(string(body)))
			if !sleep(ctx, a.retryDelay) {
				break
			}
			continue
		}

		// Decode checkin response
		var checkinResp api.CheckinResponse
		if err := json.NewDecoder(resp.Body).Decode(&checkinResp); err != nil {
			resp.Body.Close()
			slog.Error("failed to decode checkin response", "error", err)
			if !sleep(ctx, a.retryDelay) {
				break
			}
			continue
		}
		resp.Body.Close()

		// Download artifacts for all modules
		artifactDir := filepath.Join(a.dataDir, "artifacts")
		if err := os.MkdirAll(artifactDir, 0755); err != nil {
			slog.Error("failed to create artifact cache dir", "error", err)
			if !sleep(ctx, a.retryDelay) {
				break
			}
			continue
		}

		wantedHashes := make(map[string]bool)
		moduleArtifacts := make(map[string][]cachedArtifact) // keyed by module name

		artifactErr := false
		for _, m := range checkinResp.Modules {
			var cached []cachedArtifact
			for _, art := range m.Artifacts {
				cleaned, err := validRelPath(art.RelPath)
				if err != nil {
					slog.Error("artifact has invalid relative path", "rel_path", art.RelPath, "module", m.Name, "error", err)
					artifactErr = true
					break
				}

				wantedHashes[art.Hash] = true
				cachePath := filepath.Join(artifactDir, art.Hash)
				mode := os.FileMode(art.Mode)
				if mode == 0 {
					mode = 0640 // default to unreadable if server sends no mode, in case it contains secrets
				}
				if _, err := os.Stat(cachePath); err == nil {
					cached = append(cached, cachedArtifact{RelPath: cleaned, CachePath: cachePath, Mode: mode})
					continue
				}
				if err := a.downloadArtifact(ctx, art.Hash, cachePath); err != nil {
					slog.Error("failed to download artifact", "hash", art.Hash, "module", m.Name, "error", err)
					artifactErr = true
					break
				}
				cached = append(cached, cachedArtifact{RelPath: cleaned, CachePath: cachePath, Mode: mode})
			}
			if artifactErr {
				break
			}
			moduleArtifacts[m.Name] = cached
		}

		if artifactErr {
			if !sleep(ctx, a.retryDelay) {
				break
			}
			continue
		}

		// Clean stale artifacts
		a.cleanStaleArtifacts(artifactDir, wantedHashes)

		// Create run directory on the same filesystem as the artifact cache
		// so that reflink copies can be used.
		runDir := filepath.Join(a.dataDir, "run")
		if err := os.MkdirAll(runDir, 0755); err != nil {
			slog.Error("failed to create run dir", "error", err)
			if !sleep(ctx, a.retryDelay) {
				break
			}
			continue
		}

		// Sort modules by name
		modules := make([]Module, len(checkinResp.Modules))
		for i, m := range checkinResp.Modules {
			modules[i] = Module{Name: m.Name, Script: m.Script, Critical: m.Critical, Signature: m.Signature}
		}
		modules = SortModules(modules)

		// Resolve trust set for signature verification
		var trustSet []ed25519.PublicKey
		if a.verifyEnabled {
			trustSet = a.resolveTrustSet()
			if len(trustSet) == 0 {
				slog.Error("no trusted keys available, rejecting all modules")
			}
			slog.Debug("trust set resolved", "key_count", len(trustSet))
		}

		// Execute each module, audit, and report results
		auditLog := newAuditLog(filepath.Join(a.dataDir, "audit.log"))
		reportFailed := false
		for _, mod := range modules {
			if ctx.Err() != nil {
				break
			}

			// Verify signature if verification is enabled
			if a.verifyEnabled {
				verifyResult := a.verifyModule(mod, trustSet)
				if verifyResult != "" {
					slog.Error("module signature verification failed", "module", mod.Name, "reason", verifyResult)
					auditLog.log(mod.Name, mod.Script, "error")

					reportReq := api.ReportRequest{
						AgentID:    agentID,
						ModuleName: mod.Name,
						Status:     "error",
						Stderr:     verifyResult,
						ExecutedAt: time.Now().Unix(),
					}
					if !a.postReport(reportReq) {
						reportFailed = true
						break
					}

					if mod.Critical {
						slog.Warn("critical module failed verification, skipping remaining modules", "module", mod.Name)
						break
					}
					continue
				}
			}

			// Execute module
			moduleResult := runModule(ctx, runDir, mod.Name, mod.Script, moduleArtifacts[mod.Name])
			auditLog.log(mod.Name, mod.Script, moduleResult.Status)
			slog.Info("module executed", "module", mod.Name, "status", moduleResult.Status)

			// Build report request
			reportReq := api.ReportRequest{
				AgentID:    agentID,
				ModuleName: moduleResult.ModuleName,
				Status:     moduleResult.Status,
				Stdout:     moduleResult.Stdout,
				Stderr:     moduleResult.Stderr,
				ExecutedAt: time.Now().Unix(),
			}

			if !a.postReport(reportReq) {
				reportFailed = true
				break
			}

			if moduleResult.Status == "error" && mod.Critical {
				slog.Warn("critical module failed, skipping remaining modules", "module", mod.Name)
				break
			}
		}

		if reportFailed {
			slog.Error("report failed, stopping module execution")
			if !sleep(ctx, a.retryDelay) {
				break
			}
			continue
		}

		// Sleep before next poll
		pollInterval := checkinResp.PollIntervalSeconds
		if pollInterval <= 0 {
			pollInterval = 60 // default to 60 seconds if not set
		}
		slog.Debug("sleeping before next poll", "interval_seconds", pollInterval)
		if !a.pollSleep(ctx, time.Duration(pollInterval)*time.Second) {
			break
		}
	}

	slog.Info("agent shutdown requested")
	return nil
}

// validRelPath checks that a relative path is safe to use: not empty, not
// absolute, and does not escape upward via "..". Returns the cleaned path
// or an error.
func validRelPath(relPath string) (string, error) {
	cleaned := filepath.Clean(relPath)
	if cleaned == "" || filepath.IsAbs(cleaned) || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("invalid relative path: %s", relPath)
	}
	return cleaned, nil
}

// downloadArtifact downloads a single artifact by hash, verifies integrity, and
// atomically places it in the cache.
func (a *Agent) downloadArtifact(ctx context.Context, hash, cachePath string) error {
	url := a.serverURL + "/api/artifacts/" + hash
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return err
	}
	resp, err := a.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("artifact download returned status %d", resp.StatusCode)
	}

	tmpPath := cachePath + ".tmp"
	f, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	defer func() {
		f.Close()
		os.Remove(tmpPath) // clean up on failure; no-op after successful rename
	}()

	h := sha256.New()
	if _, err := io.Copy(f, io.TeeReader(resp.Body, h)); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}

	gotHash := hex.EncodeToString(h.Sum(nil))
	if gotHash != hash {
		return fmt.Errorf("artifact hash mismatch: expected %s, got %s", hash, gotHash)
	}

	return os.Rename(tmpPath, cachePath)
}

// cleanStaleArtifacts removes cached artifacts that are not in the wanted set.
func (a *Agent) cleanStaleArtifacts(artifactDir string, wanted map[string]bool) {
	entries, err := os.ReadDir(artifactDir)
	if err != nil {
		slog.Warn("failed to read artifact cache dir", "error", err)
		return
	}
	for _, e := range entries {
		if !wanted[e.Name()] {
			path := filepath.Join(artifactDir, e.Name())
			if err := os.Remove(path); err != nil {
				slog.Warn("failed to remove stale artifact", "path", path, "error", err)
			} else {
				slog.Debug("removed stale artifact", "hash", e.Name())
			}
		}
	}
}

// generateUUID generates a UUID v4 string in the format 8-4-4-4-12 hex.
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

var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// readOrCreateUUID reads or creates a UUID from the agent-id file in dataDir.
// If the file exists and contains a valid UUID, it is returned.
// Otherwise a new UUID is generated, written to the file, and returned.
// Parent directories are created if needed.
func readOrCreateUUID(dataDir string) (string, error) {
	// Ensure parent directory exists
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return "", err
	}

	filePath := filepath.Join(dataDir, "agent-id")

	// Try to read existing UUID
	if content, err := os.ReadFile(filePath); err == nil {
		uuid := strings.TrimSpace(string(content))
		if uuidPattern.MatchString(uuid) {
			return uuid, nil
		}
		if uuid != "" {
			slog.Warn("invalid UUID in agent-id file, regenerating", "value", uuid)
		}
	}

	// Generate new UUID
	uuid, err := generateUUID()
	if err != nil {
		return "", err
	}

	// Write to file
	if err := os.WriteFile(filePath, []byte(uuid), 0644); err != nil {
		return "", err
	}

	return uuid, nil
}

// verifyModule checks a module's signature against the trust set.
// Returns empty string on success, or a descriptive error message on failure.
func (a *Agent) verifyModule(mod Module, trustSet []ed25519.PublicKey) string {
	if len(trustSet) == 0 {
		return "no trusted keys available"
	}

	if mod.Signature == "" {
		return "missing signature"
	}

	sig, err := hex.DecodeString(mod.Signature)
	if err != nil {
		return fmt.Sprintf("invalid signature encoding: %v", err)
	}

	message := []byte(mod.Script)
	for _, key := range trustSet {
		if signing.Verify(key, message, sig) {
			return "" // success — matched a trusted key
		}
	}

	return "signature verification failed"
}

// resolveTrustSet resolves all configured key sources into a set of ed25519
// public keys. Called before each run loop iteration.
func (a *Agent) resolveTrustSet() []ed25519.PublicKey {
	var keys []ed25519.PublicKey

	// Process --verify-key values: each is either an inline SSH key or a file path
	for _, v := range a.verifyKeys {
		if strings.HasPrefix(v, "ssh-") {
			// Inline SSH authorized_keys format
			parsed := signing.ParsePublicKeys([]byte(v))
			keys = append(keys, parsed...)
			continue
		}

		// Try reading as file
		data, err := os.ReadFile(v)
		if err != nil {
			slog.Warn("failed to read verify key", "path", v, "error", err)
			continue
		}

		// Try as anchor PEM first
		pub, err := signing.ParsePublicKey(data)
		if err == nil {
			keys = append(keys, pub)
			continue
		}

		// Try as SSH authorized_keys format (may contain multiple keys)
		parsed := signing.ParsePublicKeys(data)
		if len(parsed) > 0 {
			keys = append(keys, parsed...)
			continue
		}

		slog.Warn("no ed25519 keys found in file", "path", v)
	}

	// Process --verify-key-url
	if a.verifyKeyURL != "" {
		urlKeys := a.fetchAndCacheKeys()
		keys = append(keys, urlKeys...)
	}

	return keys
}

// fetchAndCacheKeys fetches keys from the configured URL with a 10-second
// timeout. On success, updates the cache file. On failure, falls back to
// the cached version.
func (a *Agent) fetchAndCacheKeys() []ed25519.PublicKey {
	cachePath := filepath.Join(a.dataDir, "trusted-keys-cache")

	// Create a dedicated HTTP client with 10-second timeout
	client := &http.Client{Timeout: 10 * time.Second}

	resp, err := client.Get(a.verifyKeyURL)
	if err != nil {
		slog.Warn("failed to fetch verify key URL, using cache", "url", a.verifyKeyURL, "error", err)
		return a.loadCachedKeys(cachePath)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("verify key URL returned non-OK status, using cache", "url", a.verifyKeyURL, "status", resp.StatusCode)
		return a.loadCachedKeys(cachePath)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("failed to read verify key URL response, using cache", "url", a.verifyKeyURL, "error", err)
		return a.loadCachedKeys(cachePath)
	}

	// Update cache
	if err := os.WriteFile(cachePath, body, 0644); err != nil {
		slog.Warn("failed to write key cache", "path", cachePath, "error", err)
	}

	keys := signing.ParsePublicKeys(body)
	if len(keys) == 0 {
		slog.Warn("verify key URL returned no ed25519 keys", "url", a.verifyKeyURL)
	}
	return keys
}

// loadCachedKeys reads previously cached key data from disk.
func (a *Agent) loadCachedKeys(cachePath string) []ed25519.PublicKey {
	data, err := os.ReadFile(cachePath)
	if err != nil {
		slog.Warn("no cached keys available", "path", cachePath, "error", err)
		return nil
	}
	return signing.ParsePublicKeys(data)
}
