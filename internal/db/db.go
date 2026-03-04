package db

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a requested entity is not found.
var ErrNotFound = errors.New("not found")

// Agent represents a single agent that checks in with the server.
type Agent struct {
	ID          string // UUID of the agent
	Hostname    string
	DisplayName *string // Optional display name; nil means use hostname
	RemoteIP    string  // IP address of the checking-in agent
	OS          string  // e.g., "linux"
	Arch        string  // e.g., "amd64"
	Distro      string  // e.g., "ubuntu-22.04"
	LastSeenAt  int64   // Unix timestamp
}

// Tag is a label that can be assigned to agents for grouping and module assignment.
type Tag struct {
	ID   int64
	Name string
}

// ModuleAssignment represents a module assigned to either an agent or a tag (exclusive).
type ModuleAssignment struct {
	ID         int64
	ModuleName string
	AgentID    *string // nullable; if set, this is a direct agent assignment
	TagID      *int64  // nullable; if set, this is a tag assignment (inherited by tag members)
}

// AgentModuleDetail represents a module with its source information for an agent.
type AgentModuleDetail struct {
	ModuleName   string
	Source       string // "direct" or "tag:<tagname>"
	AssignmentID int64  // ID from module_assignments, for deletion
}

// ModuleResult represents the outcome of a module execution.
type ModuleResult struct {
	ID         int64
	AgentID    string
	ModuleName string
	Status     string // "ok", "changed", "error"
	Stdout     string
	Stderr     string
	ExecutedAt int64 // Unix timestamp
}

// Store is the interface for all data persistence operations.
type Store interface {
	// Agent operations
	UpsertAgent(ctx context.Context, agent Agent) error
	GetAgent(ctx context.Context, id string) (Agent, error)
	ListAgents(ctx context.Context) ([]Agent, error)
	SetAgentDisplayName(ctx context.Context, agentID string, name *string) error

	// Tag operations
	CreateTag(ctx context.Context, name string) (Tag, error)
	DeleteTag(ctx context.Context, id int64) error
	ListTags(ctx context.Context) ([]Tag, error)

	// Agent-tag relationships
	SetAgentTags(ctx context.Context, agentID string, tagIDs []int64) error
	GetAgentTags(ctx context.Context, agentID string) ([]Tag, error)

	// Module assignments
	AssignModule(ctx context.Context, moduleName string, agentID *string, tagID *int64) (int64, error)
	UnassignModule(ctx context.Context, id int64) error
	GetAgentModules(ctx context.Context, agentID string) ([]string, error)
	ListAssignments(ctx context.Context) ([]ModuleAssignment, error)
	GetAgentModuleDetails(ctx context.Context, agentID string) ([]AgentModuleDetail, error)

	// Module results
	InsertModuleResult(ctx context.Context, result ModuleResult) error
	GetLatestModuleResults(ctx context.Context, agentID string) ([]ModuleResult, error)
	GetModuleHistory(ctx context.Context, agentID string, moduleName string) ([]ModuleResult, error)
}
