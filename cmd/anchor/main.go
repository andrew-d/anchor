package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"

	"github.com/andrew-d/anchor"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(1)
	}

	switch os.Args[1] {
	case "serve":
		cmdServe(os.Args[2:])
	case "get":
		cmdGet(os.Args[2:])
	case "set":
		cmdSet(os.Args[2:])
	case "delete":
		cmdDelete(os.Args[2:])
	case "list":
		cmdList(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "Usage: anchor <command> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  serve    Start a node")
	fmt.Fprintln(os.Stderr, "  get      Get a value")
	fmt.Fprintln(os.Stderr, "  set      Set a value (reads JSON from stdin)")
	fmt.Fprintln(os.Stderr, "  delete   Delete a value")
	fmt.Fprintln(os.Stderr, "  list     List all values of a kind")
}

func cmdServe(args []string) {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "Data directory (required)")
	listen := fs.String("listen", ":12000", "Raft listen address")
	httpAddr := fs.String("http", ":11000", "HTTP API address")
	nodeID := fs.String("node-id", "", "Node ID (required)")
	bootstrap := fs.Bool("bootstrap", false, "Bootstrap a new cluster")
	join := fs.String("join", "", "HTTP address of existing node to join")
	fs.Parse(args)

	if *dataDir == "" || *nodeID == "" {
		fmt.Fprintln(os.Stderr, "error: --data-dir and --node-id are required")
		fs.Usage()
		os.Exit(1)
	}
	if *bootstrap && *join != "" {
		fmt.Fprintln(os.Stderr, "error: --bootstrap and --join are mutually exclusive")
		os.Exit(1)
	}

	app := anchor.New(anchor.Config{
		DataDir:    *dataDir,
		ListenAddr: *listen,
		HTTPAddr:   *httpAddr,
		NodeID:     *nodeID,
		Bootstrap:  *bootstrap,
		JoinAddr:   *join,
	})

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := app.Start(ctx); err != nil {
		slog.Error("failed to start", "err", err)
		os.Exit(1)
	}

	<-ctx.Done()
	slog.Info("shutting down")

	if err := app.Shutdown(context.Background()); err != nil {
		slog.Error("shutdown error", "err", err)
		os.Exit(1)
	}
}

func cmdGet(args []string) {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	server := fs.String("server", "localhost:11000", "HTTP server address")
	fs.Parse(args)

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: anchor get [--server ADDR] <kind> <key>")
		os.Exit(1)
	}

	kind := fs.Arg(0)
	key := fs.Arg(1)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/%s/%s", *server, kind, key))
	if err != nil {
		slog.Error("request failed", "err", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s %s\n", resp.Status, string(body))
		os.Exit(1)
	}

	io.Copy(os.Stdout, resp.Body)
	fmt.Println()
}

func cmdSet(args []string) {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	server := fs.String("server", "localhost:11000", "HTTP server address")
	fs.Parse(args)

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: anchor set [--server ADDR] <kind> <key> < value.json")
		os.Exit(1)
	}

	kind := fs.Arg(0)
	key := fs.Arg(1)

	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		slog.Error("failed to read stdin", "err", err)
		os.Exit(1)
	}
	if !json.Valid(body) {
		slog.Error("stdin does not contain valid JSON")
		os.Exit(1)
	}

	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("http://%s/api/v1/%s/%s", *server, kind, key),
		bytes.NewReader(body),
	)
	if err != nil {
		slog.Error("failed to create request", "err", err)
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("request failed", "err", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s %s\n", resp.Status, string(respBody))
		os.Exit(1)
	}
}

func cmdDelete(args []string) {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	server := fs.String("server", "localhost:11000", "HTTP server address")
	fs.Parse(args)

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: anchor delete [--server ADDR] <kind> <key>")
		os.Exit(1)
	}

	kind := fs.Arg(0)
	key := fs.Arg(1)

	req, err := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("http://%s/api/v1/%s/%s", *server, kind, key),
		nil,
	)
	if err != nil {
		slog.Error("failed to create request", "err", err)
		os.Exit(1)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("request failed", "err", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s %s\n", resp.Status, string(body))
		os.Exit(1)
	}
}

func cmdList(args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	server := fs.String("server", "localhost:11000", "HTTP server address")
	fs.Parse(args)

	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: anchor list [--server ADDR] <kind>")
		os.Exit(1)
	}

	kind := fs.Arg(0)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/%s", *server, kind))
	if err != nil {
		slog.Error("request failed", "err", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s %s\n", resp.Status, string(body))
		os.Exit(1)
	}

	io.Copy(os.Stdout, resp.Body)
}
