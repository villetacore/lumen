-- People who have signed in through OIDC (LUM-066). Rows are refreshed from
-- the identity provider on use; Keycloak stays the owner of accounts.
CREATE TABLE users (
    id         TEXT PRIMARY KEY,              -- OIDC subject
    username   TEXT NOT NULL UNIQUE,          -- preferred_username
    name       TEXT NOT NULL DEFAULT '',
    email      TEXT NOT NULL DEFAULT '',
    role       TEXT NOT NULL DEFAULT 'member', -- workspace role at last sign-in
    first_seen TEXT NOT NULL,
    last_seen  TEXT NOT NULL
);

CREATE TABLE user_groups (
    user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    group_name TEXT NOT NULL,
    PRIMARY KEY (user_id, group_name)
);
CREATE INDEX user_groups_by_group ON user_groups(group_name);
