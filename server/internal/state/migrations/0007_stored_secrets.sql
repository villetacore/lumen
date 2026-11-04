-- Tokens pasted into settings forms (ADR-0017). Settings keep "stored:<id>";
-- the value is encrypted with a key that is not in this database.
CREATE TABLE stored_secrets (
    id         TEXT PRIMARY KEY,
    owner      TEXT NOT NULL DEFAULT '',  -- which setting it belongs to
    value      BLOB NOT NULL,
    created_at TEXT NOT NULL
);
