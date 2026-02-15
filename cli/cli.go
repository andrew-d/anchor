package cli

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

// CLI holds registered modules and provides the anchor command-line interface.
type CLI struct {
	modules []anchor.Module
}

// New returns a new CLI instance.
func New() *CLI {
	return &CLI{}
}

// RegisterModule adds a module that will be initialized when the serve
// command starts.
func (c *CLI) RegisterModule(m anchor.Module) {
	c.modules = append(c.modules, m)
}

// Run parses args and executes the appropriate subcommand.
// It returns an exit code (0 for success, non-zero for failure).
func (c *CLI) Run(args []string) int {
	if len(args) < 1 {
		c.usage()
		return 1
	}

	switch args[0] {
	case "serve":
		return c.cmdServe(args[1:])
	case "get":
		return cmdGet(args[1:])
	case "set":
		return cmdSet(args[1:])
	case "delete":
		return cmdDelete(args[1:])
	case "list":
		return cmdList(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", args[0])
		c.usage()
		return 1
	}
}

func (c *CLI) usage() {
	fmt.Fprintln(os.Stderr, "Usage: anchor <command> [options]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Commands:")
	fmt.Fprintln(os.Stderr, "  serve    Start a node")
	fmt.Fprintln(os.Stderr, "  get      Get a value")
	fmt.Fprintln(os.Stderr, "  set      Set a value (reads JSON from stdin)")
	fmt.Fprintln(os.Stderr, "  delete   Delete a value")
	fmt.Fprintln(os.Stderr, "  list     List all values of a kind")
}

func (c *CLI) cmdServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "Data directory (required)")
	listen := fs.String("listen", "127.0.0.1:12000", "Raft listen address")
	httpAddr := fs.String("http", "127.0.0.1:11000", "HTTP API address")
	nodeID := fs.String("node-id", "", "Node ID (required)")
	bootstrap := fs.Bool("bootstrap", false, "Bootstrap a new cluster")
	join := fs.String("join", "", "HTTP address of existing node to join")
	fs.Parse(args)

	if *dataDir == "" || *nodeID == "" {
		fmt.Fprintln(os.Stderr, "error: --data-dir and --node-id are required")
		fs.Usage()
		return 1
	}
	if *bootstrap && *join != "" {
		fmt.Fprintln(os.Stderr, "error: --bootstrap and --join are mutually exclusive")
		return 1
	}

	app := anchor.New(anchor.Config{
		DataDir:    *dataDir,
		ListenAddr: *listen,
		HTTPAddr:   *httpAddr,
		NodeID:     *nodeID,
		Bootstrap:  *bootstrap,
		JoinAddr:   *join,
	})
	for _, m := range c.modules {
		app.RegisterModule(m)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := app.Start(ctx); err != nil {
		slog.Error("failed to start", "err", err)
		return 1
	}

	<-ctx.Done()
	slog.Info("shutting down")

	if err := app.Shutdown(context.Background()); err != nil {
		slog.Error("shutdown error", "err", err)
		return 1
	}
	return 0
}

func cmdGet(args []string) int {
	fs := flag.NewFlagSet("get", flag.ExitOnError)
	server := fs.String("server", "localhost:11000", "HTTP server address")
	fs.Parse(args)

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: anchor get [--server ADDR] <kind> <key>")
		return 1
	}

	kind := fs.Arg(0)
	key := fs.Arg(1)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/%s/%s", *server, kind, key))
	if err != nil {
		slog.Error("request failed", "err", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s %s\n", resp.Status, string(body))
		return 1
	}

	io.Copy(os.Stdout, resp.Body)
	fmt.Println()
	return 0
}

func cmdSet(args []string) int {
	fs := flag.NewFlagSet("set", flag.ExitOnError)
	server := fs.String("server", "localhost:11000", "HTTP server address")
	fs.Parse(args)

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: anchor set [--server ADDR] <kind> <key> < value.json")
		return 1
	}

	kind := fs.Arg(0)
	key := fs.Arg(1)

	body, err := io.ReadAll(os.Stdin)
	if err != nil {
		slog.Error("failed to read stdin", "err", err)
		return 1
	}
	if !json.Valid(body) {
		slog.Error("stdin does not contain valid JSON")
		return 1
	}

	req, err := http.NewRequest(http.MethodPut,
		fmt.Sprintf("http://%s/api/v1/%s/%s", *server, kind, key),
		bytes.NewReader(body),
	)
	if err != nil {
		slog.Error("failed to create request", "err", err)
		return 1
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("request failed", "err", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		respBody, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s %s\n", resp.Status, string(respBody))
		return 1
	}
	return 0
}

func cmdDelete(args []string) int {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	server := fs.String("server", "localhost:11000", "HTTP server address")
	fs.Parse(args)

	if fs.NArg() != 2 {
		fmt.Fprintln(os.Stderr, "Usage: anchor delete [--server ADDR] <kind> <key>")
		return 1
	}

	kind := fs.Arg(0)
	key := fs.Arg(1)

	req, err := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("http://%s/api/v1/%s/%s", *server, kind, key),
		nil,
	)
	if err != nil {
		slog.Error("failed to create request", "err", err)
		return 1
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		slog.Error("request failed", "err", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s %s\n", resp.Status, string(body))
		return 1
	}
	return 0
}

func cmdList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	server := fs.String("server", "localhost:11000", "HTTP server address")
	fs.Parse(args)

	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "Usage: anchor list [--server ADDR] <kind>")
		return 1
	}

	kind := fs.Arg(0)

	resp, err := http.Get(fmt.Sprintf("http://%s/api/v1/%s", *server, kind))
	if err != nil {
		slog.Error("request failed", "err", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		fmt.Fprintf(os.Stderr, "error: %s %s\n", resp.Status, string(body))
		return 1
	}

	io.Copy(os.Stdout, resp.Body)
	return 0
}
