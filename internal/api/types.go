// Package api defines the JSON wire types shared between the anchor server and agent.
package api

// CheckinRequest is the JSON body of POST /api/checkin.
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
	Name      string            `json:"name"`
	Script    string            `json:"script"`
	Critical  bool              `json:"critical,omitempty"`
	Artifacts []CheckinArtifact `json:"artifacts,omitempty"`
}

// CheckinArtifact describes a file artifact associated with a module.
type CheckinArtifact struct {
	RelPath string `json:"rel_path"`
	Hash    string `json:"hash"`
	Size    int64  `json:"size"`
	Mode    uint32 `json:"mode"`
}

// ReportRequest is the JSON body of POST /api/report.
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
