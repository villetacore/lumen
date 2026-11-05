-- Automation rules and their run log (ADR-0020). A rule is configuration of the
-- workspace, not content of the vault; its definition can quote comment text,
-- so it is sealed in an encrypted vault.
CREATE TABLE automation_rules (
    id             TEXT PRIMARY KEY,
    project        TEXT NOT NULL,
    enabled        INTEGER NOT NULL DEFAULT 1,
    kind           TEXT NOT NULL,             -- event | schedule
    definition     BLOB NOT NULL,             -- name, trigger, conditions, actions
    owner_id       TEXT NOT NULL,             -- subject whose rights the rule runs with
    owner          TEXT NOT NULL DEFAULT '',  -- their username
    after_event    INTEGER NOT NULL DEFAULT 0, -- issue_events id: older events do not fire the rule
    next_run       TEXT NOT NULL DEFAULT '',  -- schedule only, UTC RFC 3339
    revision       INTEGER NOT NULL DEFAULT 1,
    created_at     TEXT NOT NULL,
    updated_at     TEXT NOT NULL
);
CREATE INDEX automation_rules_by_project ON automation_rules(project);

-- One row per rule, occurrence and issue: the unique index is what makes a
-- repeated event or a restart unable to run the actions a second time.
CREATE TABLE automation_runs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_id     TEXT NOT NULL,
    project     TEXT NOT NULL,
    occurrence  TEXT NOT NULL,                -- event:<id> | schedule:<time>
    issue       TEXT NOT NULL DEFAULT '',     -- empty: summary of a scheduled run
    status      TEXT NOT NULL,                -- running | done | failed | blocked | throttled | unknown
    error       TEXT NOT NULL DEFAULT '',
    actions     INTEGER NOT NULL DEFAULT 0,   -- actions completed
    matched     INTEGER NOT NULL DEFAULT 0,   -- issues a scheduled run selected
    started_at  TEXT NOT NULL,
    finished_at TEXT NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX automation_runs_once ON automation_runs(rule_id, occurrence, issue);
CREATE INDEX automation_runs_by_project ON automation_runs(project, id);

CREATE TABLE automation_cursor (
    name  TEXT PRIMARY KEY,
    value INTEGER NOT NULL
);
