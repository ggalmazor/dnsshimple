-- Multi-account: one SSH key may open several DNSimple accounts. The most
-- recently used binding greets the connection; switching and fresh links
-- bump last_used_at.

ALTER TABLE ssh_keys DROP CONSTRAINT ssh_keys_pkey;
ALTER TABLE ssh_keys ADD PRIMARY KEY (fingerprint, tenant_id);
ALTER TABLE ssh_keys ADD COLUMN last_used_at TIMESTAMPTZ NOT NULL DEFAULT now();
