#!/usr/bin/env bash
# Open the admin console on the running LAN container. docker exec targets
# the binary directly (the distroless image has no shell); ssh -t chains
# the TTY through so Bubble Tea gets one end to end.
set -euo pipefail

TARGET="${DEPLOY_TARGET:-guillermo@192.168.1.130}"
REMOTE_DIR="dnsshimple"

exec ssh -t "${TARGET}" "cd ${REMOTE_DIR} && docker compose exec dnsshimple /dnsshimple admin"
