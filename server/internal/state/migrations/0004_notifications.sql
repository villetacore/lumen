-- In-app notifications (LUM-067). Rows hold references only — no titles or
-- comment text — so nothing of an issue is stored here; the list is built by
-- looking issues up with the reader's own access, at read time.
CREATE TABLE notifications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    recipient  TEXT NOT NULL,             -- preferred_username
    kind       TEXT NOT NULL,             -- assigned | mentioned | commented | status
    issue      TEXT NOT NULL,             -- issue key
    ref        TEXT NOT NULL DEFAULT '',  -- comment id or new status
    actor      TEXT NOT NULL DEFAULT '',
    dedup      TEXT NOT NULL UNIQUE,      -- a repeated event inserts nothing
    created_at TEXT NOT NULL,
    read_at    TEXT
);
CREATE INDEX notifications_by_recipient ON notifications(recipient, id DESC);
