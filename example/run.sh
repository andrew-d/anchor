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
SIGNED_MODULES="$DATA_DIR/modules.d"
mkdir -p "$SERVER_DATA" "$AGENT1_DATA" "$AGENT2_DATA"

# Copy modules to a temp directory so we can sign them without affecting
# the checked-in code.
cp -a "$MODULES_DIR" "$SIGNED_MODULES"

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

# Generate a signing keypair and sign all modules
echo "Generating signing keypair..."
$ANCHOR keygen -o "$DATA_DIR/signing"

echo "Signing modules..."
for mod in "$SIGNED_MODULES"/*; do
    # Skip directories (artifact .d dirs) and non-regular files
    [ -f "$mod" ] || continue
    $ANCHOR sign -k "$DATA_DIR/signing.key" "$mod"
done

# Add an unsigned module AFTER signing. Agent 1 (with verification) will
# reject it; agent 2 (without verification) will run it normally.
cat > "$SIGNED_MODULES/99_unsigned" << 'MODEOF'
#!/bin/sh
case "$1" in
    metadata)
        echo '{"name": "Unsigned Module", "description": "This module has no signature"}'
        ;;
    apply)
        echo "this module is unsigned"
        exit 0
        ;;
    *)
        echo "unknown command: $1" >&2
        exit 1
        ;;
esac
MODEOF
chmod +x "$SIGNED_MODULES/99_unsigned"

echo "Starting server on port $PORT..."
echo "  modules dir: $SIGNED_MODULES"
echo "  data dir:    $SERVER_DATA"
echo ""
$ANCHOR server -port "$PORT" -modules-dir "$SIGNED_MODULES" -data-dir "$SERVER_DATA" -poll-interval 30 &
SERVER_PID=$!
PIDS="$SERVER_PID"

# Give the server a moment to start
sleep 1

if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    echo "Server failed to start." >&2
    exit 1
fi

echo "Starting agent 1 (with signature verification)..."
echo "  data dir: $AGENT1_DATA"
$ANCHOR agent -server "http://localhost:$PORT" -data-dir "$AGENT1_DATA" -verify-key "$DATA_DIR/signing.pub" &
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
echo "  Agent 1: PID $AGENT1_PID (signature verification enabled)"
echo "  Agent 2: PID $AGENT2_PID (no verification)"
echo ""
echo "Press Ctrl-C to stop."
echo ""

# Wait for any child to exit (or for signal)
wait
