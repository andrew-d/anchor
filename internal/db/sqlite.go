package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"

	_ "modernc.org/sqlite"
)

// SQLiteStore implements Store using a SQLite database.
type SQLiteStore struct {
	db *sql.DB
}

// Open opens or creates a SQLite database at the given filepath and returns a ready-to-use SQLiteStore.
func Open(filepath string) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", filepath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable foreign keys
	if _, err := db.ExecContext(context.Background(), "PRAGMA foreign_keys = ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	// Set WAL mode for concurrency
	if _, err := db.ExecContext(context.Background(), "PRAGMA journal_mode = WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	// Set busy timeout
	if _, err := db.ExecContext(context.Background(), "PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to set busy timeout: %w", err)
	}

	// Create schema if it doesn't exist
	schema := `
CREATE TABLE IF NOT EXISTS agents (
    id TEXT PRIMARY KEY,
    hostname TEXT NOT NULL,
    remote_ip TEXT NOT NULL DEFAULT '',
    os TEXT NOT NULL DEFAULT '',
    arch TEXT NOT NULL DEFAULT '',
    distro TEXT NOT NULL DEFAULT '',
    last_seen_at INTEGER NOT NULL
) STRICT;

CREATE TABLE IF NOT EXISTS tags (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL UNIQUE
) STRICT;

CREATE TABLE IF NOT EXISTS agent_tags (
    agent_id TEXT NOT NULL REFERENCES agents(id),
    tag_id INTEGER NOT NULL REFERENCES tags(id) ON DELETE CASCADE,
    PRIMARY KEY (agent_id, tag_id)
) STRICT;

CREATE TABLE IF NOT EXISTS module_assignments (
    id INTEGER PRIMARY KEY,
    module_name TEXT NOT NULL,
    agent_id TEXT REFERENCES agents(id),
    tag_id INTEGER REFERENCES tags(id) ON DELETE CASCADE,
    CHECK ((agent_id IS NOT NULL AND tag_id IS NULL) OR (agent_id IS NULL AND tag_id IS NOT NULL))
) STRICT;

CREATE TABLE IF NOT EXISTS module_results (
    id INTEGER PRIMARY KEY,
    agent_id TEXT NOT NULL REFERENCES agents(id),
    module_name TEXT NOT NULL,
    status TEXT NOT NULL,
    stdout TEXT NOT NULL DEFAULT '',
    stderr TEXT NOT NULL DEFAULT '',
    executed_at INTEGER NOT NULL
) STRICT;

CREATE INDEX IF NOT EXISTS idx_module_results_lookup
    ON module_results (agent_id, module_name, executed_at);
`

	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to create schema: %w", err)
	}

	return &SQLiteStore{db: db}, nil
}

// Close closes the database connection.
func (s *SQLiteStore) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// UpsertAgent inserts or updates an agent.
func (s *SQLiteStore) UpsertAgent(ctx context.Context, agent Agent) error {
	query := `
INSERT INTO agents (id, hostname, remote_ip, os, arch, distro, last_seen_at)
VALUES (?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(id) DO UPDATE SET
    hostname = excluded.hostname,
    remote_ip = excluded.remote_ip,
    os = excluded.os,
    arch = excluded.arch,
    distro = excluded.distro,
    last_seen_at = excluded.last_seen_at
`
	_, err := s.db.ExecContext(ctx, query, agent.ID, agent.Hostname, agent.RemoteIP, agent.OS, agent.Arch, agent.Distro, agent.LastSeenAt)
	return err
}

// GetAgent retrieves a single agent by ID.
func (s *SQLiteStore) GetAgent(ctx context.Context, id string) (Agent, error) {
	var agent Agent
	query := `SELECT id, hostname, remote_ip, os, arch, distro, last_seen_at FROM agents WHERE id = ?`
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&agent.ID, &agent.Hostname, &agent.RemoteIP, &agent.OS, &agent.Arch, &agent.Distro, &agent.LastSeenAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Agent{}, ErrNotFound
		}
		return Agent{}, err
	}
	return agent, nil
}

// ListAgents retrieves all agents, ordered by hostname.
func (s *SQLiteStore) ListAgents(ctx context.Context) ([]Agent, error) {
	query := `SELECT id, hostname, remote_ip, os, arch, distro, last_seen_at FROM agents ORDER BY hostname`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var agents []Agent
	for rows.Next() {
		var agent Agent
		if err := rows.Scan(
			&agent.ID, &agent.Hostname, &agent.RemoteIP, &agent.OS, &agent.Arch, &agent.Distro, &agent.LastSeenAt,
		); err != nil {
			return nil, err
		}
		agents = append(agents, agent)
	}
	return agents, rows.Err()
}

// CreateTag creates a new tag.
func (s *SQLiteStore) CreateTag(ctx context.Context, name string) (Tag, error) {
	result, err := s.db.ExecContext(ctx, `INSERT INTO tags (name) VALUES (?)`, name)
	if err != nil {
		return Tag{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return Tag{}, err
	}
	return Tag{ID: id, Name: name}, nil
}

// DeleteTag deletes a tag (cascades to agent_tags and module_assignments).
func (s *SQLiteStore) DeleteTag(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM tags WHERE id = ?`, id)
	return err
}

// ListTags retrieves all tags, ordered by name.
func (s *SQLiteStore) ListTags(ctx context.Context) ([]Tag, error) {
	query := `SELECT id, name FROM tags ORDER BY name`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// SetAgentTags replaces all tags for an agent.
func (s *SQLiteStore) SetAgentTags(ctx context.Context, agentID string, tagIDs []int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete all existing tags for this agent
	if _, err := tx.ExecContext(ctx, `DELETE FROM agent_tags WHERE agent_id = ?`, agentID); err != nil {
		return err
	}

	// Insert new tags
	for _, tagID := range tagIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO agent_tags (agent_id, tag_id) VALUES (?, ?)`, agentID, tagID); err != nil {
			return err
		}
	}

	return tx.Commit()
}

// GetAgentTags retrieves all tags assigned to an agent.
func (s *SQLiteStore) GetAgentTags(ctx context.Context, agentID string) ([]Tag, error) {
	query := `
SELECT t.id, t.name FROM tags t
JOIN agent_tags at ON t.id = at.tag_id
WHERE at.agent_id = ?
ORDER BY t.name
`
	rows, err := s.db.QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var tags []Tag
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name); err != nil {
			return nil, err
		}
		tags = append(tags, tag)
	}
	return tags, rows.Err()
}

// AssignModule assigns a module to either an agent or a tag.
func (s *SQLiteStore) AssignModule(ctx context.Context, moduleName string, agentID *string, tagID *int64) (int64, error) {
	query := `INSERT INTO module_assignments (module_name, agent_id, tag_id) VALUES (?, ?, ?)`
	result, err := s.db.ExecContext(ctx, query, moduleName, agentID, tagID)
	if err != nil {
		return 0, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// UnassignModule removes a module assignment.
func (s *SQLiteStore) UnassignModule(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM module_assignments WHERE id = ?`, id)
	return err
}

// GetAgentModules returns all modules assigned to an agent (direct + via tags), deduplicated and sorted.
func (s *SQLiteStore) GetAgentModules(ctx context.Context, agentID string) ([]string, error) {
	query := `
SELECT DISTINCT module_name FROM module_assignments
WHERE agent_id = ?
UNION
SELECT DISTINCT ma.module_name FROM module_assignments ma
JOIN agent_tags at ON ma.tag_id = at.tag_id
WHERE at.agent_id = ?
ORDER BY module_name
`
	rows, err := s.db.QueryContext(ctx, query, agentID, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var modules []string
	for rows.Next() {
		var moduleName string
		if err := rows.Scan(&moduleName); err != nil {
			return nil, err
		}
		modules = append(modules, moduleName)
	}
	return modules, rows.Err()
}

// InsertModuleResult inserts a new module result.
func (s *SQLiteStore) InsertModuleResult(ctx context.Context, result ModuleResult) error {
	query := `
INSERT INTO module_results (agent_id, module_name, status, stdout, stderr, executed_at)
VALUES (?, ?, ?, ?, ?, ?)
`
	_, err := s.db.ExecContext(ctx, query, result.AgentID, result.ModuleName, result.Status, result.Stdout, result.Stderr, result.ExecutedAt)
	return err
}

// GetLatestModuleResults returns the most recent result for each module for an agent.
func (s *SQLiteStore) GetLatestModuleResults(ctx context.Context, agentID string) ([]ModuleResult, error) {
	query := `
SELECT mr.id, mr.agent_id, mr.module_name, mr.status, mr.stdout, mr.stderr, mr.executed_at FROM module_results mr
INNER JOIN (
    SELECT agent_id, module_name, MAX(executed_at) as max_ts
    FROM module_results
    WHERE agent_id = ?
    GROUP BY agent_id, module_name
) latest ON mr.agent_id = latest.agent_id
    AND mr.module_name = latest.module_name
    AND mr.executed_at = latest.max_ts
ORDER BY mr.module_name
`
	rows, err := s.db.QueryContext(ctx, query, agentID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ModuleResult
	for rows.Next() {
		var result ModuleResult
		if err := rows.Scan(
			&result.ID, &result.AgentID, &result.ModuleName, &result.Status,
			&result.Stdout, &result.Stderr, &result.ExecutedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

// GetModuleHistory returns all results for a specific agent+module, most recent first.
func (s *SQLiteStore) GetModuleHistory(ctx context.Context, agentID string, moduleName string) ([]ModuleResult, error) {
	query := `
SELECT id, agent_id, module_name, status, stdout, stderr, executed_at
FROM module_results
WHERE agent_id = ? AND module_name = ?
ORDER BY executed_at DESC
`
	rows, err := s.db.QueryContext(ctx, query, agentID, moduleName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []ModuleResult
	for rows.Next() {
		var result ModuleResult
		if err := rows.Scan(
			&result.ID, &result.AgentID, &result.ModuleName, &result.Status,
			&result.Stdout, &result.Stderr, &result.ExecutedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

// ListAssignments returns all module assignments, ordered by module_name.
func (s *SQLiteStore) ListAssignments(ctx context.Context) ([]ModuleAssignment, error) {
	query := `SELECT id, module_name, agent_id, tag_id FROM module_assignments ORDER BY module_name`
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var assignments []ModuleAssignment
	for rows.Next() {
		var assignment ModuleAssignment
		if err := rows.Scan(&assignment.ID, &assignment.ModuleName, &assignment.AgentID, &assignment.TagID); err != nil {
			return nil, err
		}
		assignments = append(assignments, assignment)
	}
	return assignments, rows.Err()
}

// GetAgentModuleDetails returns all modules for an agent with source information (direct or via tag).
// Deduplicates modules, preferring direct assignments over tag assignments.
func (s *SQLiteStore) GetAgentModuleDetails(ctx context.Context, agentID string) ([]AgentModuleDetail, error) {
	// Query direct assignments
	directQuery := `SELECT id, module_name FROM module_assignments WHERE agent_id = ?`
	directRows, err := s.db.QueryContext(ctx, directQuery, agentID)
	if err != nil {
		return nil, err
	}
	defer directRows.Close()

	// Store direct assignments in a map for deduplication
	directModules := make(map[string]AgentModuleDetail)
	for directRows.Next() {
		var id int64
		var moduleName string
		if err := directRows.Scan(&id, &moduleName); err != nil {
			return nil, err
		}
		directModules[moduleName] = AgentModuleDetail{
			ModuleName:   moduleName,
			Source:       "direct",
			AssignmentID: id,
		}
	}
	if err := directRows.Err(); err != nil {
		return nil, err
	}

	// Query tag-based assignments
	tagQuery := `
SELECT ma.id, ma.module_name, t.name FROM module_assignments ma
JOIN tags t ON ma.tag_id = t.id
JOIN agent_tags at ON at.tag_id = t.id
WHERE at.agent_id = ?
ORDER BY ma.module_name
`
	tagRows, err := s.db.QueryContext(ctx, tagQuery, agentID)
	if err != nil {
		return nil, err
	}
	defer tagRows.Close()

	result := make([]AgentModuleDetail, 0, len(directModules))
	// Add direct modules to result first
	for _, detail := range directModules {
		result = append(result, detail)
	}

	// Process tag assignments, only adding if not already present from direct
	seenTags := make(map[string]bool)
	for moduleName := range directModules {
		seenTags[moduleName] = true
	}

	for tagRows.Next() {
		var id int64
		var moduleName, tagName string
		if err := tagRows.Scan(&id, &moduleName, &tagName); err != nil {
			return nil, err
		}
		// Only add if not already present from direct assignment
		if !seenTags[moduleName] {
			result = append(result, AgentModuleDetail{
				ModuleName:   moduleName,
				Source:       "tag:" + tagName,
				AssignmentID: id,
			})
			seenTags[moduleName] = true
		}
	}
	if err := tagRows.Err(); err != nil {
		return nil, err
	}

	// Sort by module name
	sortAgentModuleDetails(result)

	return result, nil
}

// sortAgentModuleDetails sorts a slice of AgentModuleDetail by ModuleName.
func sortAgentModuleDetails(details []AgentModuleDetail) {
	slices.SortFunc(details, func(a, b AgentModuleDetail) int {
		return strings.Compare(a.ModuleName, b.ModuleName)
	})
}
