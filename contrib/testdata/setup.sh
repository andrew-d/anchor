#!/bin/bash
set -euo pipefail

# Install the exact unit files from the repo.
cp /contrib/anchor-server.service /etc/systemd/system/
cp /contrib/anchor-agent.service /etc/systemd/system/

# Create the modules directory and populate it.
mkdir -p /etc/anchor-server/modules.d
cp /modules.d/* /etc/anchor-server/modules.d/

# The only drop-in: override the agent's ExecStart to point at localhost
# (the shipped unit has a placeholder URL).
mkdir -p /etc/systemd/system/anchor-agent.service.d
cat > /etc/systemd/system/anchor-agent.service.d/override.conf <<'CONF'
[Service]
ExecStart=
ExecStart=/usr/local/bin/anchor agent -server http://127.0.0.1:8080
CONF

systemctl daemon-reload

# Start the server, then the agent. systemctl start is synchronous —
# it waits until the service is running (or fails).
systemctl start anchor-server.service
systemctl start anchor-agent.service

# Verify both units are active.
systemctl is-active anchor-server.service
systemctl is-active anchor-agent.service

# Verify the server is healthy. Use --retry-connrefused to handle the
# brief window between systemctl reporting active and the socket being ready.
curl -sf --retry 5 --retry-connrefused --retry-delay 1 http://127.0.0.1:8080/healthz
echo ""
echo "PASS: server is healthy"

echo "PASS: all systemd unit checks passed"
