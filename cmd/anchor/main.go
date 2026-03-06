package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
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
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	port := fs.Int("port", 8080, "HTTP listen port")
	modulesDir := fs.String("modules-dir", "/etc/anchor/modules.d", "Directory containing module scripts")
	dataDir := fs.String("data-dir", "/var/lib/anchor", "Directory for persistent data (SQLite database)")
	pollInterval := fs.Int("poll-interval", 300, "Poll interval in seconds sent to agents")
	resultsKeep := fs.Int("results-keep", 100, "Number of module results to keep per agent+module pair")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	srv := server.New(*port, *modulesDir, *dataDir)
	srv.SetPollInterval(*pollInterval)
	srv.SetResultsKeep(*resultsKeep)
	if err := srv.Run(ctx); err != nil {
		slog.Error("server error", "error", err)
		return 1
	}
	return 0
}

func runAgent(args []string) int {
	fs := flag.NewFlagSet("agent", flag.ExitOnError)
	serverURL := fs.String("server", "http://localhost:8080", "Server URL")
	dataDir := fs.String("data-dir", "/var/lib/anchor", "Directory for persistent data (agent UUID)")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
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
