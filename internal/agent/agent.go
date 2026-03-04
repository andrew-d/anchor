package agent

import "log/slog"

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
