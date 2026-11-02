-- The instance row names this state database, so a backup or a restore can be
-- matched to the installation it came from.
CREATE TABLE instance (
    id         TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);

INSERT INTO instance(id, created_at)
VALUES (lower(hex(randomblob(16))), strftime('%Y-%m-%dT%H:%M:%SZ', 'now'));
