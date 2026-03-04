package agent

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

// Run performs a single check-in cycle and returns.
func (a *Agent) Run() error {
	slog.Info("agent starting", "server_url", a.serverURL, "data_dir", a.dataDir)
	slog.Info("agent finished (stub)")
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

// readOrCreateUUID reads or creates a UUID from the agent-id file in dataDir.
// If the file exists and contains a valid UUID, it is returned (AC6.2).
// If the file does not exist, a new UUID is generated, the file is created, and the UUID is returned (AC6.1).
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
		if uuid != "" {
			return uuid, nil
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
