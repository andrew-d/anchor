#!/bin/sh
set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
EXAMPLE_DIR="$SCRIPT_DIR"
MODULES_DIR="$EXAMPLE_DIR/modules.d"

# Temp directories for runtime data (cleaned up on exit)
DATA_DIR=$(mktemp -d "${TMPDIR:-/tmp}/anchor-example.XXXXXX")
SERVER_DATA="$DATA_DIR/server"
AGENT1_DATA="$DATA_DIR/agent1"
AGENT2_DATA="$DATA_DIR/agent2"
mkdir -p "$SERVER_DATA" "$AGENT1_DATA" "$AGENT2_DATA"

PORT=9090
PIDS=""

cleanup() {
    echo ""
    echo "Shutting down..."

    # Kill agents first, then server
    for pid in $PIDS; do
        if kill -0 "$pid" 2>/dev/null; then
            kill "$pid" 2>/dev/null || true
        fi
    done

    # Wait for all children
    wait 2>/dev/null || true

    # Clean up temp data
    rm -rf "$DATA_DIR"
    echo "Done."
}

trap cleanup INT TERM EXIT

# Build
echo "Building anchor..."
(cd "$REPO_DIR" && go build -o "$DATA_DIR/anchor" ./cmd/anchor)

ANCHOR="$DATA_DIR/anchor"

echo "Starting server on port $PORT..."
echo "  modules dir: $MODULES_DIR"
echo "  data dir:    $SERVER_DATA"
echo ""
$ANCHOR server -port "$PORT" -modules-dir "$MODULES_DIR" -data-dir "$SERVER_DATA" &
SERVER_PID=$!
PIDS="$SERVER_PID"

# Give the server a moment to start
sleep 1

if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "Server failed to start." >&2
    exit 1
fi

echo "Starting agent 1..."
echo "  data dir: $AGENT1_DATA"
$ANCHOR agent -server "http://localhost:$PORT" -data-dir "$AGENT1_DATA" &
AGENT1_PID=$!
PIDS="$AGENT1_PID $PIDS"

echo "Starting agent 2..."
echo "  data dir: $AGENT2_DATA"
$ANCHOR agent -server "http://localhost:$PORT" -data-dir "$AGENT2_DATA" &
AGENT2_PID=$!
PIDS="$AGENT2_PID $PIDS"

echo ""
echo "=== Anchor example running ==="
echo "  Server:  http://localhost:$PORT"
echo "  Agent 1: PID $AGENT1_PID (data: $AGENT1_DATA)"
echo "  Agent 2: PID $AGENT2_PID (data: $AGENT2_DATA)"
echo ""
echo "Press Ctrl-C to stop."
echo ""

# Wait for any child to exit (or for signal)
wait
