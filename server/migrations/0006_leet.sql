-- v0.1.3 P1 (A10.3, data-model.md 3.14): LeetCode daily problem — facts, per-space #daily switch, one root per channel
-- per day, and each user's solve record. No problem body / HTML / code snippet / example is stored anywhere.

CREATE TABLE leet_daily (
  date              TEXT PRIMARY KEY,                       -- UTC YYYY-MM-DD
  frontend_id       TEXT NOT NULL,                          -- ^[0-9]{1,6}$
  slug              TEXT NOT NULL,                          -- ^[a-z0-9-]{1,100}$
  title             TEXT NOT NULL,                          -- 1..200
  difficulty        TEXT NOT NULL CHECK (difficulty IN ('Easy','Medium','Hard')),
  tags_json         TEXT NOT NULL DEFAULT '[]',             -- JSON string array, <= 20 x 1..50
  is_paid_only      INTEGER NOT NULL CHECK (is_paid_only IN (0,1)),
  first_reporter_id TEXT REFERENCES users(id) ON DELETE SET NULL,
  first_reported_at INTEGER NOT NULL,
  mismatch_count    INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE leet_space_settings (
  space_id   TEXT PRIMARY KEY REFERENCES spaces(id) ON DELETE CASCADE,
  enabled    INTEGER NOT NULL CHECK (enabled IN (0,1)),
  channel_id TEXT REFERENCES channels(id),
  enabled_by TEXT REFERENCES users(id) ON DELETE SET NULL,
  enabled_at INTEGER,
  updated_at INTEGER NOT NULL
);

CREATE TABLE leet_daily_roots (
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  date       TEXT NOT NULL,
  message_id TEXT REFERENCES messages(id),                  -- NULL = a request claimed the day and is creating the root
  claimed_at INTEGER NOT NULL,
  PRIMARY KEY (channel_id, date)
);

CREATE TABLE leet_solves (
  user_id            TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  date               TEXT NOT NULL,                         -- the problem's UTC day
  slug               TEXT NOT NULL,
  lang               TEXT NOT NULL,
  passed_at          INTEGER NOT NULL,                      -- server time of the first record (kept on re-PUT)
  runtime_percentile REAL,
  submission_id      TEXT,
  PRIMARY KEY (user_id, date)
);
CREATE INDEX idx_leet_solves_date ON leet_solves(date);

ALTER TABLE user_settings ADD COLUMN leet_share_with_friends INTEGER NOT NULL DEFAULT 1 CHECK (leet_share_with_friends IN (0,1));
