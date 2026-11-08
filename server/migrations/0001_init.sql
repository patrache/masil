CREATE TABLE users (
  id                TEXT PRIMARY KEY,
  handle            TEXT NOT NULL UNIQUE,                 -- lowercase ^[a-z0-9_]{3,20}$
  display_name      TEXT NOT NULL,                        -- 1..32 code points
  bio               TEXT NOT NULL DEFAULT '',             -- 0..160
  avatar_upload_id  TEXT REFERENCES uploads(id) ON DELETE SET NULL,
  status_emoji      TEXT,                                 -- one emoji grapheme, <= 16 bytes
  status_text       TEXT,                                 -- 0..64
  status_expires_at INTEGER,                              -- NULL = no expiry
  password_hash     TEXT NOT NULL,                        -- see api.md 2.2
  role              TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user','admin')),
  is_private        INTEGER NOT NULL DEFAULT 0 CHECK (is_private IN (0,1)),
  suspended_at      INTEGER,
  suspended_until   INTEGER,                              -- NULL with suspended_at set = indefinite
  suspend_reason    TEXT,
  follower_count    INTEGER NOT NULL DEFAULT 0,           -- active followers only
  following_count   INTEGER NOT NULL DEFAULT 0,
  post_count        INTEGER NOT NULL DEFAULT 0,           -- non-deleted top-level posts + replies
  created_at        INTEGER NOT NULL,
  updated_at        INTEGER NOT NULL
);
CREATE INDEX idx_users_display_name ON users(display_name);

CREATE TABLE user_settings (
  user_id               TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  dm_policy             TEXT NOT NULL DEFAULT 'friends_and_spaces'
                          CHECK (dm_policy IN ('friends_and_spaces','friends_only')),
  notify_dm             INTEGER NOT NULL DEFAULT 1 CHECK (notify_dm IN (0,1)),
  notify_mention        INTEGER NOT NULL DEFAULT 1 CHECK (notify_mention IN (0,1)),
  notify_invite         INTEGER NOT NULL DEFAULT 1 CHECK (notify_invite IN (0,1)),
  notify_friend_request INTEGER NOT NULL DEFAULT 1 CHECK (notify_friend_request IN (0,1)),
  notify_thread_reply   INTEGER NOT NULL DEFAULT 1 CHECK (notify_thread_reply IN (0,1)),
  notify_social         INTEGER NOT NULL DEFAULT 0 CHECK (notify_social IN (0,1)),
  focus_default_min     INTEGER NOT NULL DEFAULT 25 CHECK (focus_default_min BETWEEN 1 AND 720),
  updated_at            INTEGER NOT NULL
);

CREATE TABLE sessions (
  id            TEXT PRIMARY KEY,
  user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash    TEXT NOT NULL UNIQUE,                     -- lowercase hex SHA-256 of the raw token
  client        TEXT NOT NULL DEFAULT 'tui' CHECK (client IN ('tui','daemon','admin','other')),
  device_name   TEXT NOT NULL DEFAULT '',                 -- 0..64, e.g. hostname
  ip            TEXT,                                     -- CF-Connecting-IP at login
  user_agent    TEXT,
  created_at    INTEGER NOT NULL,
  last_used_at  INTEGER NOT NULL,
  expires_at    INTEGER NOT NULL,                         -- sliding: last_used_at + 90 days
  revoked_at    INTEGER,
  revoke_reason TEXT CHECK (revoke_reason IN ('logout','revoked','password_changed','suspended'))
);
CREATE INDEX idx_sessions_user ON sessions(user_id, created_at);
CREATE INDEX idx_sessions_expires ON sessions(expires_at);

CREATE TABLE friend_requests (
  id           TEXT PRIMARY KEY,
  from_user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  to_user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','accepted','declined','canceled')),
  created_at   INTEGER NOT NULL,
  responded_at INTEGER,
  CHECK (from_user_id <> to_user_id)
);
CREATE UNIQUE INDEX uq_friend_requests_pending
  ON friend_requests(from_user_id, to_user_id) WHERE status = 'pending';
CREATE INDEX idx_friend_requests_to ON friend_requests(to_user_id, status, created_at);
CREATE INDEX idx_friend_requests_from ON friend_requests(from_user_id, status, created_at);

-- one row per friendship; user_a < user_b (string compare of ULIDs)
CREATE TABLE friendships (
  user_a     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  user_b     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (user_a, user_b),
  CHECK (user_a < user_b)
);
CREATE INDEX idx_friendships_b ON friendships(user_b);

CREATE TABLE blocks (
  blocker_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  blocked_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (blocker_id, blocked_id),
  CHECK (blocker_id <> blocked_id)
);
CREATE INDEX idx_blocks_blocked ON blocks(blocked_id);

CREATE TABLE follows (
  follower_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  followee_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      TEXT NOT NULL CHECK (status IN ('active','pending')),  -- pending = private account approval
  created_at  INTEGER NOT NULL,
  accepted_at INTEGER,
  PRIMARY KEY (follower_id, followee_id),
  CHECK (follower_id <> followee_id)
);
CREATE INDEX idx_follows_followee ON follows(followee_id, status, created_at);
CREATE INDEX idx_follows_follower ON follows(follower_id, status, created_at);

CREATE TABLE spaces (
  id                 TEXT PRIMARY KEY,
  name               TEXT NOT NULL,                       -- 1..50
  description        TEXT NOT NULL DEFAULT '',            -- 0..200
  icon_emoji         TEXT,                                -- one emoji grapheme or NULL
  owner_id           TEXT NOT NULL REFERENCES users(id),
  default_channel_id TEXT,                                -- channels.id, set in same batch as creation
  member_count       INTEGER NOT NULL DEFAULT 0,
  created_at         INTEGER NOT NULL,
  updated_at         INTEGER NOT NULL,
  deleted_at         INTEGER
);
CREATE INDEX idx_spaces_owner ON spaces(owner_id);

CREATE TABLE space_members (
  space_id  TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  user_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  role      TEXT NOT NULL DEFAULT 'member' CHECK (role IN ('owner','admin','member')),
  joined_at INTEGER NOT NULL,
  PRIMARY KEY (space_id, user_id)
);
CREATE INDEX idx_space_members_user ON space_members(user_id, space_id);
CREATE INDEX idx_space_members_role ON space_members(space_id, role);

CREATE TABLE space_bans (
  space_id   TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  banned_by  TEXT NOT NULL REFERENCES users(id),
  reason     TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL,
  PRIMARY KEY (space_id, user_id)
);

-- invite links (codes)
CREATE TABLE space_invites (
  id         TEXT PRIMARY KEY,
  space_id   TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  code       TEXT NOT NULL UNIQUE,                        -- 8 chars, see 1.1
  created_by TEXT NOT NULL REFERENCES users(id),
  max_uses   INTEGER CHECK (max_uses IS NULL OR max_uses BETWEEN 1 AND 1000),
  use_count  INTEGER NOT NULL DEFAULT 0,
  expires_at INTEGER,                                     -- NULL = never
  revoked_at INTEGER,
  created_at INTEGER NOT NULL
);
CREATE INDEX idx_space_invites_space ON space_invites(space_id, created_at);

-- direct invites (member invites a friend)
CREATE TABLE space_direct_invites (
  id           TEXT PRIMARY KEY,
  space_id     TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  inviter_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  invitee_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status       TEXT NOT NULL DEFAULT 'pending'
                 CHECK (status IN ('pending','accepted','declined','canceled')),
  created_at   INTEGER NOT NULL,
  responded_at INTEGER
);
CREATE UNIQUE INDEX uq_space_direct_invites_pending
  ON space_direct_invites(space_id, invitee_id) WHERE status = 'pending';
CREATE INDEX idx_space_direct_invites_invitee ON space_direct_invites(invitee_id, status, created_at);

CREATE TABLE channels (
  id              TEXT PRIMARY KEY,
  kind            TEXT NOT NULL CHECK (kind IN ('text','dm','group_dm')),
  space_id        TEXT REFERENCES spaces(id) ON DELETE CASCADE,
  name            TEXT,                                   -- text: ^[a-z0-9가-힣_-]{1,32}$ ; group_dm: optional 1..50 free text ; dm: NULL
  topic           TEXT NOT NULL DEFAULT '',               -- 0..250
  is_private      INTEGER NOT NULL DEFAULT 0 CHECK (is_private IN (0,1)),
  position        INTEGER NOT NULL DEFAULT 0,
  owner_id        TEXT REFERENCES users(id),              -- group_dm creator; NULL otherwise
  created_by      TEXT NOT NULL REFERENCES users(id),
  dm_key          TEXT UNIQUE,                            -- only kind='dm'
  member_count    INTEGER NOT NULL DEFAULT 0,             -- rows in channel_members (dm/group_dm/private text)
  last_message_id TEXT,
  last_message_at INTEGER,
  created_at      INTEGER NOT NULL,
  updated_at      INTEGER NOT NULL,
  deleted_at      INTEGER,
  CHECK ((kind = 'text' AND space_id IS NOT NULL AND name IS NOT NULL)
      OR (kind IN ('dm','group_dm') AND space_id IS NULL))
);
CREATE UNIQUE INDEX uq_channels_space_name ON channels(space_id, name) WHERE kind = 'text' AND deleted_at IS NULL;
CREATE INDEX idx_channels_space ON channels(space_id, position) WHERE deleted_at IS NULL;

-- explicit membership: dm, group_dm, private text channels
CREATE TABLE channel_members (
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  added_by   TEXT REFERENCES users(id),
  joined_at  INTEGER NOT NULL,
  PRIMARY KEY (channel_id, user_id)
);
CREATE INDEX idx_channel_members_user ON channel_members(user_id, channel_id);

CREATE TABLE channel_prefs (
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  notify     TEXT NOT NULL CHECK (notify IN ('all','mentions','none')),
  muted      INTEGER NOT NULL DEFAULT 0 CHECK (muted IN (0,1)),  -- muted = no unread badge emphasis, no alerts
  updated_at INTEGER NOT NULL,
  PRIMARY KEY (user_id, channel_id)
);
-- row absent => defaults: dm/group_dm notify='all', text notify='mentions', muted=0

CREATE TABLE channel_reads (
  user_id              TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel_id           TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  last_read_message_id TEXT NOT NULL,                     -- top-level message id; only moves forward
  updated_at           INTEGER NOT NULL,
  PRIMARY KEY (user_id, channel_id)
);
-- row absent => read pointer = idFloor(joined_at): channel_members.joined_at (private text / dm / group_dm)
--               or space_members.joined_at (public text). Messages before joining never count as unread.

CREATE TABLE messages (
  id             TEXT PRIMARY KEY,                        -- implicit rowid kept (needed by FTS5)
  channel_id     TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  author_id      TEXT NOT NULL REFERENCES users(id),
  kind           TEXT NOT NULL DEFAULT 'text' CHECK (kind IN ('text','me','roll','poll','system')),
  body           TEXT NOT NULL DEFAULT '',                -- 1..4000 code points for text/me ; '' when deleted
  meta           TEXT,                                    -- JSON: roll result / system event (api.md 4.6)
  thread_root_id TEXT REFERENCES messages(id),            -- NULL = top-level
  reply_count    INTEGER NOT NULL DEFAULT 0,              -- roots only
  last_reply_at  INTEGER,                                 -- roots only
  mention_here   INTEGER NOT NULL DEFAULT 0 CHECK (mention_here IN (0,1)),
  poll_id        TEXT REFERENCES polls(id),
  client_nonce   TEXT,                                    -- idempotency key from client, <= 64 chars
  created_at     INTEGER NOT NULL,
  edited_at      INTEGER,
  deleted_at     INTEGER,
  deleted_by     TEXT REFERENCES users(id)
);
CREATE INDEX idx_messages_channel_top ON messages(channel_id, id) WHERE thread_root_id IS NULL;
CREATE INDEX idx_messages_thread ON messages(thread_root_id, id) WHERE thread_root_id IS NOT NULL;
CREATE INDEX idx_messages_author ON messages(author_id, id);
CREATE UNIQUE INDEX uq_messages_nonce ON messages(author_id, client_nonce) WHERE client_nonce IS NOT NULL;

CREATE TABLE thread_participants (
  root_id       TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  last_reply_at INTEGER NOT NULL,
  PRIMARY KEY (root_id, user_id)
);
-- root author is inserted when the first reply arrives.

-- rows only for users who pass requireChannelRead for the channel and have no block (either way) with the author
-- (api.md 2.1); other @handle text stays plain and gets no row.
CREATE TABLE message_mentions (
  message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  is_here    INTEGER NOT NULL DEFAULT 0 CHECK (is_here IN (0,1)),    -- 1 = only via @here expansion (not in Message.mentions[])
  in_thread  INTEGER NOT NULL DEFAULT 0 CHECK (in_thread IN (0,1)),  -- 1 = thread reply; excluded from mention_count
  PRIMARY KEY (message_id, user_id)
);
CREATE INDEX idx_message_mentions_user ON message_mentions(user_id, channel_id, in_thread, message_id);

CREATE TABLE message_reactions (
  message_id TEXT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
  emoji      TEXT NOT NULL,                               -- one emoji grapheme, <= 32 bytes UTF-8
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (message_id, emoji, user_id)
);

CREATE TABLE message_pins (
  channel_id TEXT NOT NULL REFERENCES channels(id) ON DELETE CASCADE,
  message_id TEXT NOT NULL UNIQUE REFERENCES messages(id) ON DELETE CASCADE,
  pinned_by  TEXT NOT NULL REFERENCES users(id),
  pinned_at  INTEGER NOT NULL,
  PRIMARY KEY (channel_id, message_id)
);

CREATE TABLE uploads (
  id           TEXT PRIMARY KEY,
  owner_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  purpose      TEXT NOT NULL CHECK (purpose IN ('avatar','post','story')),
  r2_key       TEXT NOT NULL UNIQUE,                      -- see 5.
  content_type TEXT NOT NULL CHECK (content_type IN ('image/png','image/jpeg','image/gif','image/webp')),
  size         INTEGER NOT NULL CHECK (size BETWEEN 1 AND 5242880),
  width        INTEGER,                                   -- parsed from header; NULL if unknown
  height       INTEGER,
  status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','attached','removed')),
  attached_to  TEXT,                                      -- 'post:<id>' | 'story:<id>' | 'avatar:<user_id>'
  created_at   INTEGER NOT NULL,
  removed_at   INTEGER
);
CREATE INDEX idx_uploads_owner ON uploads(owner_id, created_at);
CREATE INDEX idx_uploads_status ON uploads(status, created_at);

CREATE TABLE posts (
  id           TEXT PRIMARY KEY,                          -- implicit rowid kept (FTS5)
  author_id    TEXT NOT NULL REFERENCES users(id),
  body         TEXT NOT NULL DEFAULT '',                  -- 0..500 code points ('' allowed only with media/poll/quote)
  reply_to_id  TEXT REFERENCES posts(id),                 -- direct parent
  root_id      TEXT REFERENCES posts(id),                 -- top of reply tree; NULL for top-level
  quote_of_id  TEXT REFERENCES posts(id),
  poll_id      TEXT REFERENCES polls(id),
  like_count   INTEGER NOT NULL DEFAULT 0,
  repost_count INTEGER NOT NULL DEFAULT 0,
  reply_count  INTEGER NOT NULL DEFAULT 0,                -- direct replies only
  quote_count  INTEGER NOT NULL DEFAULT 0,
  client_nonce TEXT,                                      -- idempotency key from client, <= 64 chars
  created_at   INTEGER NOT NULL,
  deleted_at   INTEGER,
  deleted_by   TEXT REFERENCES users(id)
);
CREATE INDEX idx_posts_author ON posts(author_id, id);
CREATE UNIQUE INDEX uq_posts_nonce ON posts(author_id, client_nonce) WHERE client_nonce IS NOT NULL;
CREATE INDEX idx_posts_reply_to ON posts(reply_to_id, id) WHERE reply_to_id IS NOT NULL;
CREATE INDEX idx_posts_quote_of ON posts(quote_of_id, id) WHERE quote_of_id IS NOT NULL;
CREATE INDEX idx_posts_top ON posts(id) WHERE reply_to_id IS NULL AND deleted_at IS NULL;

CREATE TABLE post_media (
  post_id   TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  position  INTEGER NOT NULL CHECK (position BETWEEN 0 AND 3),
  upload_id TEXT NOT NULL UNIQUE REFERENCES uploads(id),
  PRIMARY KEY (post_id, position)
);

CREATE TABLE post_likes (
  post_id    TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (post_id, user_id)
);
CREATE INDEX idx_post_likes_user ON post_likes(user_id, created_at);

CREATE TABLE reposts (
  id         TEXT NOT NULL UNIQUE,                        -- ULID; used as timeline item id / cursor
  post_id    TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (post_id, user_id)
);
CREATE INDEX idx_reposts_user ON reposts(user_id, id);

CREATE TABLE post_mentions (
  post_id TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  user_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  PRIMARY KEY (post_id, user_id)
);
CREATE INDEX idx_post_mentions_user ON post_mentions(user_id, post_id);

CREATE TABLE post_hashtags (
  tag        TEXT NOT NULL,                               -- normalized: NFC + lowercase, no '#', 1..50
  post_id    TEXT NOT NULL REFERENCES posts(id) ON DELETE CASCADE,
  author_id  TEXT NOT NULL REFERENCES users(id),
  created_at INTEGER NOT NULL,
  PRIMARY KEY (tag, post_id)
);
CREATE INDEX idx_post_hashtags_created ON post_hashtags(created_at);
CREATE INDEX idx_post_hashtags_post ON post_hashtags(post_id);

CREATE TABLE trends (
  rank         INTEGER PRIMARY KEY CHECK (rank BETWEEN 1 AND 10),
  tag          TEXT NOT NULL,
  post_count   INTEGER NOT NULL,                          -- posts in last 24h
  author_count INTEGER NOT NULL,                          -- distinct authors in last 24h (ranking key)
  computed_at  INTEGER NOT NULL
);

CREATE TABLE stories (
  id              TEXT PRIMARY KEY,
  author_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body            TEXT NOT NULL DEFAULT '',               -- 0..200
  media_upload_id TEXT REFERENCES uploads(id),
  bg              TEXT NOT NULL DEFAULT 'accent' CHECK (bg IN ('accent','accent2','accent3','accent4','plain')),
  created_at      INTEGER NOT NULL,
  expires_at      INTEGER NOT NULL,                       -- created_at + 86_400_000
  deleted_at      INTEGER,
  CHECK (body <> '' OR media_upload_id IS NOT NULL)
);
CREATE INDEX idx_stories_author ON stories(author_id, expires_at);
CREATE INDEX idx_stories_expires ON stories(expires_at);

CREATE TABLE story_views (
  story_id  TEXT NOT NULL REFERENCES stories(id) ON DELETE CASCADE,
  viewer_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  viewed_at INTEGER NOT NULL,
  PRIMARY KEY (story_id, viewer_id)
);

CREATE TABLE polls (
  id           TEXT PRIMARY KEY,
  creator_id   TEXT NOT NULL REFERENCES users(id),
  context_type TEXT NOT NULL CHECK (context_type IN ('message','post')),
  context_id   TEXT NOT NULL,                             -- messages.id or posts.id (same batch insert)
  question     TEXT NOT NULL,                             -- 1..200
  multi        INTEGER NOT NULL DEFAULT 0 CHECK (multi IN (0,1)),
  voter_count  INTEGER NOT NULL DEFAULT 0,
  closes_at    INTEGER,                                   -- NULL = until closed manually; max now+7d
  closed_at    INTEGER,                                   -- set by manual close
  created_at   INTEGER NOT NULL
);
CREATE INDEX idx_polls_context ON polls(context_type, context_id);

CREATE TABLE poll_options (
  poll_id    TEXT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
  idx        INTEGER NOT NULL CHECK (idx BETWEEN 0 AND 9),
  text       TEXT NOT NULL,                               -- 1..80
  vote_count INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (poll_id, idx)
);

CREATE TABLE poll_votes (
  poll_id    TEXT NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
  user_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  option_idx INTEGER NOT NULL,
  created_at INTEGER NOT NULL,
  PRIMARY KEY (poll_id, user_id, option_idx)
);

CREATE TABLE guestbook_entries (
  id         TEXT PRIMARY KEY,
  owner_id   TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,  -- profile owner
  author_id  TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  body       TEXT NOT NULL,                               -- 1..300 ; '' when deleted
  is_secret  INTEGER NOT NULL DEFAULT 0 CHECK (is_secret IN (0,1)), -- visible only to owner & author
  created_at INTEGER NOT NULL,
  deleted_at INTEGER,
  deleted_by TEXT REFERENCES users(id)
);
CREATE INDEX idx_guestbook_owner ON guestbook_entries(owner_id, id);

CREATE TABLE notifications (
  id           TEXT PRIMARY KEY,
  user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,   -- recipient
  type         TEXT NOT NULL CHECK (type IN (
                 'follow','follow_request','follow_accepted',
                 'friend_request','friend_accepted',
                 'space_invite',
                 'post_like','post_repost','post_reply','post_quote','post_mention',
                 'message_mention','thread_reply',
                 'guestbook_entry')),
  actor_id     TEXT REFERENCES users(id) ON DELETE CASCADE,
  subject_type TEXT NOT NULL CHECK (subject_type IN
                 ('user','post','message','space','friend_request','space_direct_invite','guestbook_entry')),
  subject_id   TEXT NOT NULL,
  extra        TEXT,                                      -- JSON, see api.md Notification
  dedupe_key   TEXT,                                      -- e.g. 'post_like:<post_id>:<actor_id>'
  created_at   INTEGER NOT NULL,
  read_at      INTEGER
);
CREATE INDEX idx_notifications_user ON notifications(user_id, id);
CREATE INDEX idx_notifications_unread ON notifications(user_id, id) WHERE read_at IS NULL;
CREATE UNIQUE INDEX uq_notifications_dedupe ON notifications(user_id, dedupe_key) WHERE dedupe_key IS NOT NULL;

-- mirror of the UserHub DO state, for REST reads & fan-out filtering.
CREATE TABLE user_presence (
  user_id       TEXT PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  status        TEXT NOT NULL DEFAULT 'offline' CHECK (status IN ('online','away','dnd','offline')),
  mode          TEXT NOT NULL DEFAULT 'auto' CHECK (mode IN ('auto','away','dnd')),
  focus_ends_at INTEGER,                                  -- non-NULL & > now => focus active
  online_until  INTEGER NOT NULL DEFAULT 0,               -- full (TUI) connection liveness: fan-out eligibility for all events (events.md 6.2)
  notify_online_until INTEGER NOT NULL DEFAULT 0,         -- notify (daemon) connection liveness: eligibility for alert/notification/focus events only
  last_seen_at  INTEGER,
  updated_at    INTEGER NOT NULL
);
CREATE INDEX idx_user_presence_online ON user_presence(online_until);

CREATE TABLE reports (
  id              TEXT PRIMARY KEY,
  reporter_id     TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  target_type     TEXT NOT NULL CHECK (target_type IN ('user','message','post','story','guestbook_entry','space')),
  target_id       TEXT NOT NULL,
  target_user_id  TEXT REFERENCES users(id),              -- author/owner of the target
  reason          TEXT NOT NULL CHECK (reason IN ('spam','harassment','illegal','sexual','violence','other')),
  note            TEXT NOT NULL DEFAULT '',               -- 0..1000
  snapshot        TEXT NOT NULL,                          -- JSON copy of target at report time
  status          TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open','resolved','dismissed')),
  resolution      TEXT CHECK (resolution IN ('content_removed','user_suspended','no_action')),
  resolution_note TEXT,
  resolved_by     TEXT REFERENCES users(id),
  resolved_at     INTEGER,
  created_at      INTEGER NOT NULL
);
CREATE INDEX idx_reports_status ON reports(status, id);
CREATE INDEX idx_reports_target ON reports(target_type, target_id);
CREATE UNIQUE INDEX uq_reports_open ON reports(reporter_id, target_type, target_id) WHERE status = 'open';

CREATE TABLE admin_actions (
  id          TEXT PRIMARY KEY,
  admin_id    TEXT NOT NULL REFERENCES users(id),
  action      TEXT NOT NULL CHECK (action IN
                ('remove_content','suspend_user','unsuspend_user','resolve_report','dismiss_report')),
  target_type TEXT NOT NULL,
  target_id   TEXT NOT NULL,
  report_id   TEXT REFERENCES reports(id),
  reason      TEXT NOT NULL DEFAULT '',
  created_at  INTEGER NOT NULL
);
CREATE INDEX idx_admin_actions_target ON admin_actions(target_type, target_id);
CREATE INDEX idx_admin_actions_created ON admin_actions(id);

