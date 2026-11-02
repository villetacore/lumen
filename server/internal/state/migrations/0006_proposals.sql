-- Changes proposed through MCP and applied only after a person confirms them
-- in Lumen (ADR-0013 §6). Arguments and preview carry content, so they are
-- sealed in an encrypted vault.
CREATE TABLE proposals (
    id           TEXT PRIMARY KEY,
    requester    TEXT NOT NULL,             -- preferred_username of the caller
    principal_id TEXT NOT NULL,             -- subject: only the same person confirms
    kind         TEXT NOT NULL,             -- create_issue | update_issue | change_status | add_comment
    target       TEXT NOT NULL DEFAULT '',  -- issue key or project key
    base_hash    TEXT NOT NULL DEFAULT '',  -- issue file version the preview was made from
    args         BLOB NOT NULL,
    preview      BLOB NOT NULL,
    state        TEXT NOT NULL,             -- pending | applying | applied | rejected | expired | stale | failed
    result       TEXT NOT NULL DEFAULT '',  -- e.g. the created issue key
    error        TEXT NOT NULL DEFAULT '',
    request_id   TEXT NOT NULL DEFAULT '',
    created_at   TEXT NOT NULL,
    expires_at   TEXT NOT NULL,
    decided_at   TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX proposals_request ON proposals(requester, request_id) WHERE request_id <> '';
CREATE INDEX proposals_by_requester ON proposals(requester, created_at);
