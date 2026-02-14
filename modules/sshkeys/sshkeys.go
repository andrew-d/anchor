// Package sshkeys provides an Anchor module that manages SSH authorized_keys
// files. It watches for changes to SSH key configuration and writes the
// appropriate authorized_keys file for each configured user.
//
// The configuration kind is "sshkeys.Config" with entries keyed by username:
//
//	{
//	  "keys": ["ssh-rsa AAAA...", "ssh-ed25519 AAAA..."]
//	}
//
// When keys change, the module rewrites ~/.ssh/authorized_keys for the user,
// looking up the home directory from the system's user database. It refuses to
// remove all keys from a user to protect against accidental lockout.
package sshkeys

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/andrew-d/anchor"
)

// Config holds SSH key entries for a single user.
type Config struct {
	Keys []string `json:"keys"`
}

func (Config) Kind() string { return "sshkeys.Config" }

// userInfo holds the resolved information about a system user.
type userInfo struct {
	homeDir string
	uid     uint32
	gid     uint32
}

// Module manages authorized_keys files based on replicated configuration.
type Module struct {
	// lookupUserFn resolves a username to user info (home directory, uid, gid).
	// If nil, defaults to os/user.Lookup.
	lookupUserFn func(username string) (userInfo, error)

	// enumerateUsersFn returns all system users. If nil, defaults to
	// enumerateSystemUsers.
	enumerateUsersFn func(ctx context.Context) ([]enumeratedUser, error)

	// nowFn returns the current time. If nil, defaults to time.Now.
	nowFn func() time.Time

	deploymentID string
	logger       *slog.Logger
	problems     *anchor.ProblemReporter
	store        *anchor.TypedStore[Config]
}

func (m *Module) Name() string { return "ssh_authorized_keys" }

func (m *Module) Init(_ context.Context, ic anchor.InitContext) error {
	m.deploymentID = ic.App.DeploymentID()
	m.logger = ic.Logger
	m.problems = ic.Problems
	m.store = anchor.Register[Config](ic.App)

	ic.Go(func(ctx context.Context) {
		m.watchLoop(ctx, m.store)
	})
	return nil
}

func (m *Module) lookupUser(username string) (userInfo, error) {
	if m.lookupUserFn != nil {
		return m.lookupUserFn(username)
	}
	u, err := user.Lookup(username)
	if err != nil {
		return userInfo{}, err
	}
	uid, err := strconv.ParseUint(u.Uid, 10, 32)
	if err != nil {
		return userInfo{}, fmt.Errorf("parse uid %q: %w", u.Uid, err)
	}
	gid, err := strconv.ParseUint(u.Gid, 10, 32)
	if err != nil {
		return userInfo{}, fmt.Errorf("parse gid %q: %w", u.Gid, err)
	}
	return userInfo{homeDir: u.HomeDir, uid: uint32(uid), gid: uint32(gid)}, nil
}

func (m *Module) enumerateUsers(ctx context.Context) ([]enumeratedUser, error) {
	if m.enumerateUsersFn != nil {
		return m.enumerateUsersFn(ctx)
	}
	return enumerateSystemUsers(ctx)
}

func (m *Module) now() time.Time {
	if m.nowFn != nil {
		return m.nowFn()
	}
	return time.Now()
}

func (m *Module) watchLoop(ctx context.Context, store *anchor.TypedStore[Config]) {
	w := anchor.WatchStore(store)
	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case state, ok := <-w.State():
			if !ok {
				return
			}
			pass := m.problems.Begin()
			for username, cfg := range state {
				if err := m.writeAuthorizedKeys(username, cfg.Keys); err != nil {
					pass.Error("write:"+username, "failed to write authorized_keys",
						"username", username, "err", err)
				} else {
					m.logger.Info("updated authorized_keys",
						"username", username, "num_keys", len(cfg.Keys))
				}
			}
			m.reconcile(ctx, state, pass)
			pass.Commit()
		}
	}
}

// writeAuthorizedKeys resolves the user's home directory, validates it, and
// writes the authorized_keys file.
func (m *Module) writeAuthorizedKeys(username string, keys []string) error {
	// Refuse to write an empty key list.
	if len(keys) == 0 {
		return fmt.Errorf("refusing to write empty authorized_keys for %q: would lock out the user", username)
	}

	// Look up the user.
	info, err := m.lookupUser(username)
	if err != nil {
		return fmt.Errorf("user lookup failed for %q: %w", username, err)
	}

	// Validate the home directory exists and is writable by the user.
	if err := validateHomeDir(info.homeDir, username, info.uid, info.gid); err != nil {
		return err
	}

	// Deduplicate and sort keys.
	keys = deduplicateAndSort(keys)

	sshDir := filepath.Join(info.homeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("create .ssh dir for %q: %w", username, err)
	}
	if err := os.Chown(sshDir, int(info.uid), int(info.gid)); err != nil {
		return fmt.Errorf("chown .ssh dir for %q: %w", username, err)
	}

	path := filepath.Join(sshDir, "authorized_keys")

	// Skip the write if the key lines on disk already match. This avoids
	// redundant disk writes (and mtime changes) when the watcher delivers
	// state that hasn't changed for this user.
	keyBlock := strings.Join(keys, "\n") + "\n"
	if existing, err := os.ReadFile(path); err == nil && extractKeys(string(existing)) == keyBlock {
		return nil
	}

	content := fmt.Sprintf("# Managed by anchor - do not edit manually\n# Deployment: %s\n# Last updated: %s\n%s",
		m.deploymentID,
		m.now().UTC().Format(time.RFC3339),
		keyBlock,
	)

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write authorized_keys for %q: %w", username, err)
	}
	if err := os.Chown(path, int(info.uid), int(info.gid)); err != nil {
		return fmt.Errorf("chown authorized_keys for %q: %w", username, err)
	}
	return nil
}

// parseDeploymentID extracts the deployment ID from an authorized_keys file's
// content. Returns "" if the file is not anchor-managed or has no deployment
// header.
func parseDeploymentID(content string) string {
	for _, line := range strings.SplitN(content, "\n", 5) {
		if id, ok := strings.CutPrefix(line, "# Deployment: "); ok {
			return id
		}
		// Stop scanning after the header region.
		if line != "" && !strings.HasPrefix(line, "#") {
			break
		}
	}
	return ""
}

// readDeploymentID reads an authorized_keys file and returns its deployment
// ID. Returns "" for non-existent or non-anchor files.
func readDeploymentID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return parseDeploymentID(string(data)), nil
}

// revokeAuthorizedKeys writes a header-only authorized_keys file for the
// given user, effectively revoking all SSH keys while leaving an audit trail.
func (m *Module) revokeAuthorizedKeys(username string) error {
	info, err := m.lookupUser(username)
	if err != nil {
		return fmt.Errorf("user lookup failed for %q: %w", username, err)
	}
	if err := validateHomeDir(info.homeDir, username, info.uid, info.gid); err != nil {
		return err
	}

	path := filepath.Join(info.homeDir, ".ssh", "authorized_keys")

	// Re-check the deployment ID immediately before writing to narrow the
	// TOCTOU window between the check in reconcile and this write.
	depID, err := readDeploymentID(path)
	if err != nil {
		return fmt.Errorf("re-read deployment id for %q: %w", username, err)
	}
	if depID != m.deploymentID {
		return fmt.Errorf("authorized_keys for %q changed between check and revoke (deployment: %q)", username, depID)
	}

	content := fmt.Sprintf("# Managed by anchor - do not edit manually\n# Deployment: %s\n# Last updated: %s\n# Keys revoked: user removed from configuration\n",
		m.deploymentID,
		m.now().UTC().Format(time.RFC3339),
	)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("revoke authorized_keys for %q: %w", username, err)
	}
	if err := os.Chown(path, int(info.uid), int(info.gid)); err != nil {
		return fmt.Errorf("chown authorized_keys for %q: %w", username, err)
	}
	return nil
}

// reconcile enumerates system users and revokes anchor-managed
// authorized_keys files for users not present in the current state.
func (m *Module) reconcile(ctx context.Context, state map[string]Config, pass *anchor.ProblemPass) {
	users, err := m.enumerateUsers(ctx)
	if err != nil {
		pass.Error("enumerate_users", "failed to enumerate system users", "err", err)
		return
	}

	for _, u := range users {
		if _, ok := state[u.username]; ok {
			continue
		}

		akPath := filepath.Join(u.homeDir, ".ssh", "authorized_keys")
		depID, err := readDeploymentID(akPath)
		if err != nil {
			pass.Error("read_deployment:"+u.username, "failed to read deployment id",
				"username", u.username, "err", err)
			continue
		}
		if depID == "" {
			// Not managed by anchor; leave it alone.
			continue
		}
		if depID != m.deploymentID {
			pass.Warn("different_deployment:"+u.username, "authorized_keys managed by different deployment",
				"username", u.username,
				"file_deployment_id", depID,
				"our_deployment_id", m.deploymentID,
			)
			continue
		}

		if err := m.revokeAuthorizedKeys(u.username); err != nil {
			pass.Error("revoke:"+u.username, "failed to revoke authorized_keys",
				"username", u.username, "err", err)
		} else {
			m.logger.Info("revoked authorized_keys for removed user", "username", u.username)
		}
	}
}

// extractKeys returns everything after the header comment lines in an
// authorized_keys file. This is used to compare key content without being
// affected by timestamp changes in the header.
func extractKeys(content string) string {
	var i int
	for i < len(content) {
		if content[i] != '#' {
			break
		}
		nl := strings.IndexByte(content[i:], '\n')
		if nl < 0 {
			return ""
		}
		i += nl + 1
	}
	return content[i:]
}

// deduplicateAndSort removes duplicate keys and sorts the result for stable output.
func deduplicateAndSort(keys []string) []string {
	seen := make(map[string]bool, len(keys))
	unique := make([]string, 0, len(keys))
	for _, k := range keys {
		if _, ok := seen[k]; !ok {
			seen[k] = true
			unique = append(unique, k)
		}
	}
	slices.Sort(unique)
	return unique
}

// Verify interface compliance.
var _ anchor.Module = (*Module)(nil)
