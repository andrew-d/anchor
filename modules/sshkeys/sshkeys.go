// Package sshkeys provides an Anchor module that manages SSH authorized_keys
// files. It watches for changes to SSH key configuration and writes the
// appropriate authorized_keys file for each configured user.
//
// The configuration kind is "ssh_keys" with entries keyed by username:
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
	"log"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/andrew-d/anchor"
)

const kind = "ssh_keys"

// Config holds SSH key entries for a single user.
type Config struct {
	Keys []string `json:"keys"`
}

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

	// nowFn returns the current time. If nil, defaults to time.Now.
	nowFn func() time.Time

	store *anchor.TypedStore[Config]
}

func (m *Module) Name() string { return "ssh_authorized_keys" }

func (m *Module) Init(ctx context.Context, app *anchor.App) error {
	m.store = anchor.Register[Config](app, kind)

	go m.watchLoop(ctx, m.store)
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

func (m *Module) now() time.Time {
	if m.nowFn != nil {
		return m.nowFn()
	}
	return time.Now()
}

func (m *Module) watchLoop(ctx context.Context, store *anchor.TypedStore[Config]) {
	w := store.Watch()
	defer w.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-w.Events():
			if !ok {
				return
			}
			if e.Err != nil {
				log.Printf("[ssh_keys] error deserializing event for %q: %v", e.Key, e.Err)
				continue
			}

			username := e.Key
			switch e.Change {
			case anchor.ChangeSet:
				if err := m.writeAuthorizedKeys(username, e.Value.Keys); err != nil {
					log.Printf("[ssh_keys] failed to write authorized_keys for %q: %v", username, err)
				} else {
					log.Printf("[ssh_keys] updated authorized_keys for %q (%d keys)", username, len(e.Value.Keys))
				}
			case anchor.ChangeDelete:
				log.Printf("[ssh_keys] refusing to remove all keys for %q (delete event); to remove keys, set an explicit list instead", username)
			}
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

	path := filepath.Join(sshDir, "authorized_keys")
	content := fmt.Sprintf("# Managed by anchor - do not edit manually\n# Last updated: %s\n%s\n",
		m.now().UTC().Format(time.RFC3339),
		strings.Join(keys, "\n"),
	)

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write authorized_keys for %q: %w", username, err)
	}
	return nil
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
