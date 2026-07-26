#!/usr/bin/env bash
# Pull an encrypted logical backup of the LAN host's database. The dump
# travels through SSH, never touches the remote disk, and is encrypted
# with age before it touches the local one. The vault key is deliberately
# NOT in the dump (it lives in the host's .env and your password manager);
# a leaked backup alone cannot decrypt anyone's token.
#
# Run through mise so the recipient flows in from mise.local.toml:
#   mise exec -- ./deploy/backup.sh
# Nightly, via cron (crontab -e):
#   15 3 * * * cd ~/src/dnsimple/dnsshimple && mise exec -- ./deploy/backup.sh >> ~/backups/dnsshimple/backup.log 2>&1
set -euo pipefail

TARGET="${DEPLOY_TARGET:-guillermo@192.168.1.130}"
REMOTE_DIR="dnsshimple"
BACKUP_DIR="${DNSSHIMPLE_BACKUP_DIR:-$HOME/backups/dnsshimple}"
RETENTION_DAYS="${DNSSHIMPLE_BACKUP_RETENTION_DAYS:-30}"
RECIPIENT="${DNSSHIMPLE_BACKUP_AGE_RECIPIENT:?set DNSSHIMPLE_BACKUP_AGE_RECIPIENT (age public key) in mise.local.toml}"

command -v age >/dev/null || { echo "!! age not found; run 'mise install'" >&2; exit 1; }

mkdir -p "${BACKUP_DIR}"
STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DUMP="${BACKUP_DIR}/dnsshimple-${STAMP}.sql.gz.age"

echo "==> dumping ${TARGET} into ${DUMP}"
ssh "${TARGET}" "cd ${REMOTE_DIR} && docker compose exec -T postgres pg_dump -U dnsshimple --no-owner dnsshimple" |
  gzip | age -r "${RECIPIENT}" > "${DUMP}"

# An empty or tiny dump means the pipeline broke somewhere silent.
SIZE=$(wc -c < "${DUMP}")
if [ "${SIZE}" -lt 1024 ]; then
  echo "!! dump is ${SIZE} bytes - refusing to trust it; removing" >&2
  rm -f "${DUMP}"
  exit 1
fi
echo "==> ${SIZE} bytes, encrypted for ${RECIPIENT%%[[:space:]]*}"

echo "==> pruning dumps older than ${RETENTION_DAYS} days"
find "${BACKUP_DIR}" -name 'dnsshimple-*.sql.gz.age' -mtime "+${RETENTION_DAYS}" -delete

echo "done: $(ls "${BACKUP_DIR}" | grep -c '\.sql\.gz\.age$') dumps retained in ${BACKUP_DIR}"
