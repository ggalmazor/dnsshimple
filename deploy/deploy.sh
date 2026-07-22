#!/usr/bin/env bash
# Deploy DNSShimple to a LAN host over SSH: build the amd64 image locally,
# ship it with docker save | docker load (no registry), and compose it up.
#
# Run through mise so the DNSimple credentials flow into the first .env:
#   mise exec -- ./deploy/deploy.sh
set -euo pipefail

TARGET="${DEPLOY_TARGET:-guillermo@192.168.1.130}"
REMOTE_DIR="dnsshimple"
HOST="${TARGET#*@}"

echo "==> building dnsshimple:latest for linux/amd64"
docker buildx build --platform linux/amd64 -t dnsshimple:latest --load .

echo "==> shipping the image to ${TARGET}"
docker save dnsshimple:latest | gzip | ssh "${TARGET}" 'gunzip | docker load'

echo "==> shipping compose.yaml"
ssh "${TARGET}" "mkdir -p ${REMOTE_DIR}"
scp -q deploy/compose.yaml "${TARGET}:${REMOTE_DIR}/compose.yaml"

# The .env is created once and then left alone: it holds the vault key, and
# replacing that key would make every sealed token unreadable.
if ! ssh "${TARGET}" "test -f ${REMOTE_DIR}/.env"; then
  echo "==> creating the first .env (fresh vault key)"
  : "${DNSIMPLE_CLIENT_ID:?DNSIMPLE_CLIENT_ID must be set for the first deploy}"
  : "${DNSIMPLE_CLIENT_SECRET:?DNSIMPLE_CLIENT_SECRET must be set for the first deploy}"
  ssh "${TARGET}" "umask 177 && cat > ${REMOTE_DIR}/.env" <<ENV
DNSIMPLE_CLIENT_ID=${DNSIMPLE_CLIENT_ID}
DNSIMPLE_CLIENT_SECRET=${DNSIMPLE_CLIENT_SECRET}
DNSSHIMPLE_VAULT_KEY=$(openssl rand -base64 32)
ENV
fi

echo "==> starting the stack"
ssh "${TARGET}" "cd ${REMOTE_DIR} && docker compose up -d --wait"

echo "==> verifying"
curl -fsS "http://${HOST}:9090/health" >/dev/null && echo "health: ok"
echo "done: ssh -p 2222 ${HOST}"
