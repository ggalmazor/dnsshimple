-- Identity: tenants, their SSH keys, and in-flight link handshakes.

CREATE TABLE tenants (
    id                  BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    dnsimple_account_id BIGINT NOT NULL UNIQUE,
    email               TEXT NOT NULL,
    -- Sealed with NaCl secretbox; the plaintext token never touches disk.
    sealed_token        BYTEA NOT NULL,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE ssh_keys (
    fingerprint TEXT PRIMARY KEY,
    tenant_id   BIGINT NOT NULL REFERENCES tenants (id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE pending_links (
    code        TEXT PRIMARY KEY,
    fingerprint TEXT NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
