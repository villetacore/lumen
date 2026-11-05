-- MCP clients connected through OAuth (ADR-0019). Only hashes of codes and
-- tokens are stored: a copy of this database opens nothing.
CREATE TABLE oauth_clients (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,            -- what the client calls itself; not verified
    redirect_uris TEXT NOT NULL,            -- JSON array
    created_at    TEXT NOT NULL
);
CREATE INDEX oauth_clients_by_age ON oauth_clients(created_at);

-- A person's permission for one client. Disconnecting deletes the row and,
-- with it, every code and token.
CREATE TABLE oauth_grants (
    id           TEXT PRIMARY KEY,
    client_id    TEXT NOT NULL REFERENCES oauth_clients(id) ON DELETE CASCADE,
    principal_id TEXT NOT NULL,             -- OIDC subject
    created_at   TEXT NOT NULL,
    connected_at TEXT NOT NULL DEFAULT '',  -- set when the code is exchanged
    last_used_at TEXT NOT NULL DEFAULT '',
    expires_at   TEXT NOT NULL              -- absolute: refreshing does not extend it
);
CREATE INDEX oauth_grants_by_principal ON oauth_grants(principal_id);
CREATE INDEX oauth_grants_by_client ON oauth_grants(client_id);

CREATE TABLE oauth_codes (
    hash         TEXT PRIMARY KEY,
    grant_id     TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
    redirect_uri TEXT NOT NULL,
    challenge    TEXT NOT NULL,             -- PKCE S256
    expires_at   TEXT NOT NULL,
    used_at      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX oauth_codes_by_grant ON oauth_codes(grant_id);

CREATE TABLE oauth_tokens (
    hash       TEXT PRIMARY KEY,
    grant_id   TEXT NOT NULL REFERENCES oauth_grants(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,               -- access | refresh
    expires_at TEXT NOT NULL,
    used_at    TEXT NOT NULL DEFAULT ''     -- refresh only: when it was rotated
);
CREATE INDEX oauth_tokens_by_grant ON oauth_tokens(grant_id);
