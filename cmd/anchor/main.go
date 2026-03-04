package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/andrew-d/anchor/internal/agent"
	"github.com/andrew-d/anchor/internal/server"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: anchor <server|agent>\n")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "server":
		os.Exit(runServer(os.Args[2:]))
	case "agent":
		os.Exit(runAgent(os.Args[2:]))
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: anchor <server|agent>\n", os.Args[1])
		os.Exit(1)
	}
}

func runServer(args []string) int {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	port := fs.Int("port", 8080, "HTTP listen port")
	modulesDir := fs.String("modules-dir", "/etc/anchor/modules.d", "Directory containing module scripts")
	dataDir := fs.String("data-dir", "/var/lib/anchor", "Directory for persistent data (SQLite database)")
	if err := fs.Parse(args); err != nil {
		slog.Error("failed to parse flags", "error", err)
		return 1
	}

	srv := server.New(*port, *modulesDir, *dataDir)
	if err := srv.Run(); err != nil {
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

	a := agent.New(*serverURL, *dataDir)
	if err := a.Run(context.Background()); err != nil {
		slog.Error("agent error", "error", err)
		return 1
	}
	return 0
}
