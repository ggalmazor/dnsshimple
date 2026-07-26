#!/usr/bin/env bash
# Prove a backup restores: decrypt the newest dump (or $1), load it into a
# scratch database on the local dockerized Postgres, check the identity
# tables came back, and drop the scratch. An untested backup is a hope,
# not a backup - run this after changing anything in the backup path, and
# occasionally on principle.
#
#   mise exec -- ./deploy/restore-verify.sh [dump-file]
set -euo pipefail

BACKUP_DIR="${DNSSHIMPLE_BACKUP_DIR:-$HOME/backups/dnsshimple}"
IDENTITY="${DNSSHIMPLE_BACKUP_AGE_IDENTITY:?set DNSSHIMPLE_BACKUP_AGE_IDENTITY (age key path) in mise.local.toml}"
SCRATCH_DB="dnsshimple_restore_check"

command -v age >/dev/null || { echo "!! age not found; run 'mise install'" >&2; exit 1; }

DUMP="${1:-$(ls -t "${BACKUP_DIR}"/dnsshimple-*.sql.gz.age 2>/dev/null | head -1)}"
[ -n "${DUMP}" ] || { echo "!! no dumps in ${BACKUP_DIR}; run deploy/backup.sh first" >&2; exit 1; }
echo "==> verifying ${DUMP}"

# The scratch database lives on the local compose Postgres (:5433), far
# from the LAN host; verification never touches production.
psql_scratch() {
  docker compose exec -T postgres psql -U dnsshimple -v ON_ERROR_STOP=1 "$@"
}

psql_scratch -d postgres -q -c "DROP DATABASE IF EXISTS ${SCRATCH_DB}" \
  -c "CREATE DATABASE ${SCRATCH_DB}"
trap 'psql_scratch -d postgres -q -c "DROP DATABASE IF EXISTS ${SCRATCH_DB}" >/dev/null' EXIT

echo "==> restoring into ${SCRATCH_DB}"
age -d -i "${IDENTITY}" "${DUMP}" | gunzip | psql_scratch -q -d "${SCRATCH_DB}" >/dev/null

echo "==> checking the restored schema"
for table in tenants ssh_keys pending_links; do
  COUNT=$(psql_scratch -d "${SCRATCH_DB}" -t -A -c "SELECT count(*) FROM ${table}")
  echo "    ${table}: ${COUNT} rows"
done

echo "restore: ok"
