-- Issue history (LUM-068). The vault holds the current issue; this keeps how
-- it got there. Snapshots let a rescan or restart tell a new change from one
-- already recorded: an unchanged file hash records nothing.
CREATE TABLE issue_snapshots (
    issue  TEXT PRIMARY KEY,      -- issue key
    path   TEXT NOT NULL,
    hash   TEXT NOT NULL,         -- sha256 of the file as last recorded
    fields BLOB NOT NULL,         -- JSON of tracked fields, sealed in an encrypted vault
    seen   TEXT NOT NULL
);
CREATE INDEX issue_snapshots_by_path ON issue_snapshots(path);

CREATE TABLE issue_events (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    issue   TEXT NOT NULL,
    project TEXT NOT NULL DEFAULT '',
    kind    TEXT NOT NULL,        -- created | changed | deleted
    field   TEXT NOT NULL DEFAULT '',
    old     BLOB,                 -- sealed values (ADR-0011 §7)
    new     BLOB,
    actor   TEXT NOT NULL DEFAULT '',  -- "" for edits made outside Lumen
    source  TEXT NOT NULL,        -- lumen | external
    hash    TEXT NOT NULL DEFAULT '',  -- file version the event leads to
    at      TEXT NOT NULL
);
CREATE INDEX issue_events_by_issue ON issue_events(issue, id);
CREATE INDEX issue_events_by_project ON issue_events(project, at);
