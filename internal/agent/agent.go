package agent

import (
	"bytes"
	"context"
	"crypto/rand"
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

	"github.com/andrew-d/anchor/internal/sysinfo"
)

// Agent is the anchor agent that checks in with the server.
type Agent struct {
	serverURL string
	dataDir   string
}

// New creates a new Agent.
func New(serverURL string, dataDir string) *Agent {
	return &Agent{
		serverURL: serverURL,
		dataDir:   dataDir,
	}
}

// CheckinRequest is the JSON body for POST /api/checkin.
type CheckinRequest struct {
	ID       string `json:"id"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
	Arch     string `json:"arch"`
	Distro   string `json:"distro"`
}

// CheckinResponse is the JSON response from POST /api/checkin.
type CheckinResponse struct {
	PollIntervalSeconds int             `json:"poll_interval_seconds"`
	Modules             []CheckinModule `json:"modules"`
}

// CheckinModule is a module entry in the checkin response.
type CheckinModule struct {
	Name   string `json:"name"`
	Script string `json:"script"`
}

// ReportRequest is the JSON body for POST /api/report.
type ReportRequest struct {
	AgentID    string `json:"agent_id"`
	ModuleName string `json:"module_name"`
	Status     string `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExecutedAt int64  `json:"executed_at"`
}

// ReportResponse is the JSON response from POST /api/report.
type ReportResponse struct {
	OK bool `json:"ok"`
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
	slog.Debug("agent UUID", "id", agentID)

	// Gather system info
	sysInfo := sysinfo.GatherSystemInfo()
	slog.Debug("gathered system info", "hostname", sysInfo.Hostname, "os", sysInfo.OS, "arch", sysInfo.Arch, "distro", sysInfo.Distro)

	// Polling loop
	for ctx.Err() == nil {
		// Build checkin request
		checkinReq := CheckinRequest{
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
			if !sleep(ctx, retryDelay) {
				break
			}
			continue
		}

		resp, err := http.Post(checkinURL, "application/json", bytes.NewReader(reqBody))
		if err != nil {
			slog.Error("checkin request failed", "error", err, "url", checkinURL)
			if !sleep(ctx, retryDelay) {
				break
			}
			continue
		}

		// Check for non-OK status before decoding JSON
		if resp.StatusCode != http.StatusOK {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			slog.Error("checkin returned non-OK status", "status", resp.StatusCode, "body", strings.TrimSpace(string(body)))
			if !sleep(ctx, retryDelay) {
				break
			}
			continue
		}

		// Decode checkin response
		var checkinResp CheckinResponse
		if err := json.NewDecoder(resp.Body).Decode(&checkinResp); err != nil {
			resp.Body.Close()
			slog.Error("failed to decode checkin response", "error", err)
			if !sleep(ctx, retryDelay) {
				break
			}
			continue
		}
		resp.Body.Close()

		// Sort modules by name
		modules := make([]Module, len(checkinResp.Modules))
		for i, m := range checkinResp.Modules {
			modules[i] = Module{Name: m.Name, Script: m.Script}
		}
		modules = SortModules(modules)

		// Execute each module and report results
		reportFailed := false
		for _, mod := range modules {
			if ctx.Err() != nil {
				break
			}

			// Execute module
			moduleResult := runModule(ctx, mod.Name, mod.Script)
			slog.Info("module executed", "module", mod.Name, "status", moduleResult.Status)

			// Build report request
			reportReq := ReportRequest{
				AgentID:    agentID,
				ModuleName: moduleResult.ModuleName,
				Status:     moduleResult.Status,
				Stdout:     moduleResult.Stdout,
				Stderr:     moduleResult.Stderr,
				ExecutedAt: time.Now().Unix(),
			}

			// POST to server /api/report
			reportURL := a.serverURL + "/api/report"
			reportBody, err := json.Marshal(reportReq)
			if err != nil {
				slog.Error("failed to marshal report request", "error", err)
				reportFailed = true
				break
			}

			reportResp, err := http.Post(reportURL, "application/json", bytes.NewReader(reportBody))
			if err != nil {
				slog.Error("report request failed", "error", err, "url", reportURL, "module", mod.Name)
				reportFailed = true
				break
			}

			// Read response to ensure connection is drained
			io.Copy(io.Discard, reportResp.Body)
			reportResp.Body.Close()

			if reportResp.StatusCode != http.StatusOK && reportResp.StatusCode != http.StatusCreated {
				slog.Error("report request returned non-OK status", "status", reportResp.StatusCode, "module", mod.Name)
				reportFailed = true
				break
			}
		}

		if reportFailed {
			slog.Error("report failed, stopping module execution")
			if !sleep(ctx, retryDelay) {
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
		if !sleep(ctx, time.Duration(pollInterval)*time.Second) {
			break
		}
	}

	slog.Info("agent shutdown requested")
	return nil
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
