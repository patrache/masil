-- v0.1.3 P2 (data-model.md 3.15, api.md 5.26): workspace share metadata only. The share content (files, paths,
-- commands, screens) never reaches the server — it is ciphertext relayed by the WorkspaceHub DO
-- (docs/contract/workspace.md). Number fixed by the plan index: 0006 is P1, 0007 is P2.
CREATE TABLE workspace_shares (
  id          TEXT PRIMARY KEY,
  owner_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       TEXT NOT NULL,
  status      TEXT NOT NULL CHECK (status IN ('live','ended')),
  created_at  INTEGER NOT NULL,
  ended_at    INTEGER,
  end_reason  TEXT CHECK (end_reason IN ('ended','timeout'))
);
CREATE INDEX idx_workspace_shares_owner ON workspace_shares(owner_id, status, created_at);
CREATE INDEX idx_workspace_shares_ended ON workspace_shares(status, ended_at);

CREATE TABLE workspace_share_members (
  share_id    TEXT NOT NULL REFERENCES workspace_shares(id) ON DELETE CASCADE,
  user_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  seat        INTEGER NOT NULL CHECK (seat BETWEEN 1 AND 8),
  invited_at  INTEGER NOT NULL,
  joined_at   INTEGER,
  left_at     INTEGER,
  removed_at  INTEGER,
  max_level   TEXT CHECK (max_level IN ('viewer','prompter','editor','keyboard')),
  verified    INTEGER NOT NULL DEFAULT 0 CHECK (verified IN (0,1)),
  PRIMARY KEY (share_id, user_id),
  UNIQUE (share_id, seat)
);
CREATE INDEX idx_workspace_share_members_user ON workspace_share_members(user_id, share_id);

ALTER TABLE user_settings ADD COLUMN notify_workspace INTEGER NOT NULL DEFAULT 1 CHECK (notify_workspace IN (0,1));
