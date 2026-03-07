package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/andrew-d/anchor/internal/agent"
	"github.com/andrew-d/anchor/internal/server"
	"github.com/andrew-d/anchor/internal/version"
)

const usage = `Usage: anchor <command> [flags]

Commands:
  server    Start the anchor server
  agent     Start the anchor agent
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

	a := agent.New(*serverURL, *dataDir)

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
