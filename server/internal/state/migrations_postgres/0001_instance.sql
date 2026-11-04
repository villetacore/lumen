-- The instance row names this state database, so a backup or a restore can be
-- matched to the installation it came from.
CREATE TABLE instance (
    id         TEXT PRIMARY KEY,
    created_at TEXT NOT NULL
);

INSERT INTO instance(id, created_at)
VALUES (md5(random()::text || clock_timestamp()::text), to_char(now() AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"'));
