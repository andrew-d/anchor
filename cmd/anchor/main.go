package main

import (
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/andrew-d/anchor/internal/agent"
	"github.com/andrew-d/anchor/internal/server"
	"github.com/andrew-d/anchor/internal/signing"
	"github.com/andrew-d/anchor/internal/version"
)

// stringSlice implements flag.Value for repeatable string flags.
type stringSlice []string

func (s *stringSlice) String() string { return strings.Join(*s, ", ") }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}

const usage = `Usage: anchor <command> [flags]

Commands:
  server    Start the anchor server
  agent     Start the anchor agent
  keygen    Generate a signing keypair
  sign      Sign module scripts
  version   Print the anchor version

Run 'anchor <command> -help' for details on a specific command.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(1)
	}

	switch os.Args[1] {
	case "server":
		os.Exit(runServer(os.Args[2:]))
	case "agent":
		os.Exit(runAgent(os.Args[2:]))
	case "keygen":
		os.Exit(runKeygen(os.Args[2:]))
	case "sign":
		os.Exit(runSign(os.Args[2:]))
	case "version", "-version", "--version", "-v":
		fmt.Println("anchor", version.Long())
		os.Exit(0)
	case "-help", "--help", "-h", "help":
		fmt.Print(usage)
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n%s", os.Args[1], usage)
		os.Exit(1)
	}
}

func runServer(args []string) int {
	defaultModulesDir := "/etc/anchor-server/modules.d"
	if v := os.Getenv("CONFIGURATION_DIRECTORY"); v != "" {
		defaultModulesDir = filepath.Join(v, "modules.d")
	}
	defaultDataDir := envOrDefault("STATE_DIRECTORY", "/var/lib/anchor-server")

	fs := flag.NewFlagSet("server", flag.ContinueOnError)
	port := fs.Int("port", 8080, "HTTP listen port")
	modulesDir := fs.String("modules-dir", defaultModulesDir, "directory containing module scripts")
	dataDir := fs.String("data-dir", defaultDataDir, "directory for persistent data (e.g. SQLite database)")
	pollInterval := fs.Int("poll-interval", 300, "poll interval in seconds sent to agents")
	keepResults := fs.Int("keep-results", 100, "number of module results to keep per agent+module pair")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := server.New(*port, *modulesDir, *dataDir, server.Options{
		PollInterval: *pollInterval,
		KeepResults:  *keepResults,
	})
	if err := srv.Run(ctx); err != nil {
		slog.Error("server error", "error", err)
		return 1
	}
	return 0
}

func runAgent(args []string) int {
	defaultDataDir := envOrDefault("STATE_DIRECTORY", "/var/lib/anchor")

	fs := flag.NewFlagSet("agent", flag.ContinueOnError)
	serverURL := fs.String("server", "", "server URL")
	dataDir := fs.String("data-dir", defaultDataDir, "directory for persistent data")

	var verifyKeys stringSlice
	fs.Var(&verifyKeys, "verify-key", "trusted public key (inline ssh-ed25519, anchor PEM file, or SSH pubkey file; repeatable)")
	verifyKeyURL := fs.String("verify-key-url", "", "URL returning SSH authorized_keys format keys (e.g. https://github.com/user.keys)")

	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		return 1
	}

	if *serverURL == "" {
		slog.Error("server URL is required")
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	a := agent.New(*serverURL, *dataDir, agent.VerifyConfig{
		Keys:   verifyKeys,
		KeyURL: *verifyKeyURL,
	})

	// SIGHUP triggers an immediate checkin cycle
	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)
	go func() {
		for range sighup {
			slog.Info("received SIGHUP, triggering immediate run")
			a.TriggerRun()
		}
	}()
	defer signal.Stop(sighup)

	if err := a.Run(ctx); err != nil {
		slog.Error("agent error", "error", err)
		return 1
	}
	return 0
}

// envOrDefault returns the value of the environment variable named by key if
// set and non-empty, otherwise it returns fallback. This is used to support
// systemd's StateDirectory= and ConfigurationDirectory= directives, which set
// $STATE_DIRECTORY and $CONFIGURATION_DIRECTORY respectively. Under systemd,
// each unit has its own environment so there is no collision between the server
// and agent both reading $STATE_DIRECTORY.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func runKeygen(args []string) int {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	output := fs.String("o", "anchor", "output file base name (produces <name>.key and <name>.pub)")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		return 1
	}

	priv, pub, err := signing.GenerateKey()
	if err != nil {
		slog.Error("generating keypair", "error", err)
		return 1
	}

	keyPath := *output + ".key"
	pubPath := *output + ".pub"

	if err := os.WriteFile(keyPath, signing.MarshalPrivateKey(priv), 0600); err != nil {
		slog.Error("writing private key", "path", keyPath, "error", err)
		return 1
	}
	if err := os.WriteFile(pubPath, signing.MarshalPublicKey(pub), 0644); err != nil {
		slog.Error("writing public key", "path", pubPath, "error", err)
		return 1
	}

	slog.Info("generated signing keypair", "private_key", keyPath, "public_key", pubPath)
	return 0
}

func runSign(args []string) int {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	keyFile := fs.String("k", "", "private key file (anchor PEM or OpenSSH format)")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		return 1
	}

	if *keyFile == "" {
		slog.Error("key file is required (-k flag)")
		return 1
	}

	modules := fs.Args()
	if len(modules) == 0 {
		slog.Error("at least one module file is required")
		return 1
	}

	keyData, err := os.ReadFile(*keyFile)
	if err != nil {
		slog.Error("reading key file", "path", *keyFile, "error", err)
		return 1
	}

	privKey, err := signing.ParsePrivateKey(keyData)
	if err != nil {
		slog.Error("parsing private key", "path", *keyFile, "error", err)
		return 1
	}

	for _, mod := range modules {
		content, err := os.ReadFile(mod)
		if err != nil {
			slog.Error("reading module file", "path", mod, "error", err)
			return 1
		}

		sig := signing.Sign(privKey, content)
		sigPath := mod + ".sig"
		if err := os.WriteFile(sigPath, []byte(hex.EncodeToString(sig)), 0644); err != nil {
			slog.Error("writing signature file", "path", sigPath, "error", err)
			return 1
		}

		slog.Info("signed module", "module", mod, "signature", sigPath)
	}

	return 0
}
