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
// When keys change, the module rewrites ~/.ssh/authorized_keys for the
// specified user (resolved via the configured home directory root).
package sshkeys

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/andrew-d/anchor"
)

const kind = "ssh_keys"

// Config holds SSH key entries for a single user.
type Config struct {
	Keys []string `json:"keys"`
}

// Module manages authorized_keys files based on replicated configuration.
type Module struct {
	// HomeDir is the root directory containing user home directories.
	// For example, "/home" on Linux means user "alice" gets
	// /home/alice/.ssh/authorized_keys.
	HomeDir string
}

func (m *Module) Name() string { return "ssh_authorized_keys" }

func (m *Module) Init(ctx context.Context, app *anchor.App) error {
	store := anchor.Register[Config](app, kind)

	go m.watchLoop(ctx, store)
	return nil
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
				if err := m.writeAuthorizedKeys(username, nil); err != nil {
					log.Printf("[ssh_keys] failed to clear authorized_keys for %q: %v", username, err)
				} else {
					log.Printf("[ssh_keys] cleared authorized_keys for %q", username)
				}
			}
		}
	}
}

func (m *Module) writeAuthorizedKeys(username string, keys []string) error {
	sshDir := filepath.Join(m.HomeDir, username, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		return fmt.Errorf("create .ssh dir: %w", err)
	}

	path := filepath.Join(sshDir, "authorized_keys")

	var content string
	if len(keys) > 0 {
		content = "# Managed by anchor - do not edit manually\n" +
			strings.Join(keys, "\n") + "\n"
	}

	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}
	return nil
}

// Verify interface compliance.
var _ anchor.Module = (*Module)(nil)
