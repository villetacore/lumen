-- Members of closed tracker projects (ADR-0012). A project with no rows is
-- open: the workspace role applies.
CREATE TABLE project_members (
    project    TEXT NOT NULL,                 -- project key, "WEB"
    kind       TEXT NOT NULL CHECK (kind IN ('user', 'group')),
    name       TEXT NOT NULL,                 -- preferred_username or group name
    role       TEXT NOT NULL CHECK (role IN ('viewer', 'member', 'admin')),
    added_at   TEXT NOT NULL,
    added_by   TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (project, kind, name)
);
