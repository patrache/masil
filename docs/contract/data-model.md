# masil 데이터 모델 계약 (D1 · R2 · 보존 정책)

> **v0.1.3 더하기 (2026-11-09, P0 Task 2)**: 마이그레이션 번호 예약(2장), `messages.card_json`(3.12), `collab_docs`·`collab_versions`·`collab_comments`(3.13). P1(`leet_*`, `0006` — 3.14)·P2(`workspace_*`, `0007`)는 자기 표 절을 더한다.

- 상태: 확정 계약 v1 (2026-11-05). 상위 출처: `docs/superpowers/specs/2026-11-05-masil-design.md`.
- 짝 문서: `api.md`(REST), `events.md`(WebSocket), `server-architecture.md`(서버 코드 구조).
- 이 문서의 SQL은 **그대로** `server/migrations/0001_init.sql`, `0002_search.sql`, 그리고 이후 추가 마이그레이션(`0003_...`, 해당 표 바로 아래에 따로 적음)에 옮긴다. 테이블·컬럼 이름을 바꾸려면 이 문서를 먼저 고친다.

## 1. 공통 규칙

### 1.1 ID

- 모든 엔티티 ID는 **ULID** (26자, Crockford base32, 대문자. 예: `01JC8Z3K4M5N6P7Q8R9S0TVWXY`).
  - 앞 48비트가 밀리초 시각이라 **사전순 정렬 = 생성 시각 순**. 메시지·글·알림의 정렬과 커서는 ID로 한다.
  - 서버는 `src/lib/ids.ts` 의 `newId()` 로만 생성한다(같은 isolate 안에서 단조 증가 보장, 같은 ms 안에서는 랜덤부 +1).
  - 서로 다른 isolate 에서 만든 ID는 커밋 순서와 다를 수 있다(시계 차이·처리 지연으로 더 작은 ID 가 더 늦게 커밋될 수 있음). 그래서:
    - 클라이언트는 이벤트 도착 순서가 아니라 **ID 순으로 끼워 넣는다**.
    - "이 ID 이후" 조회(`after=`, `since=`)는 가진 마지막 ID 가 아니라 **5초 겹친 기준 ID** 로 하고 ID 로 중복 제거한다(`events.md` 5.3 "겹쳐 받기"). 그렇지 않으면 늦게 커밋된 작은 ID 를 영영 놓칠 수 있다.
    - "최신 ID" 를 담는 컬럼(`channels.last_message_id/last_message_at`, `channel_reads.last_read_message_id`)은 **조건부로만 갱신**한다: `UPDATE ... SET last_message_id = ?1, last_message_at = ?2 WHERE id = ?3 AND (last_message_id IS NULL OR last_message_id < ?1)`. 동시 전송 때 값이 뒤로 가지 않게.
- 예외: `space_invites.code` (초대 코드, 8자 `[A-Za-z0-9]`, 혼동 문자 `0 O o 1 l I` 제외), `sessions.token_hash`.
- 클라이언트는 ID를 **불투명 문자열**로 취급한다(파싱 금지, 단 정렬 비교는 허용).

### 1.2 시각

- D1 저장: **INTEGER, Unix epoch 밀리초 (UTC)**. 컬럼 이름은 `*_at`.
- API·이벤트 JSON: **RFC 3339 UTC, 밀리초 3자리, `Z` 접미사** (예: `"2026-11-05T02:03:04.567Z"`). 변환은 `src/lib/time.ts` 의 `iso(ms)`.
- 길이(기간) 값: REST는 초 단위 `*_sec`, WebSocket 제어값은 밀리초 `*_ms`.

### 1.3 불리언·JSON·문자열

- 불리언은 `INTEGER NOT NULL CHECK (x IN (0,1))`. API로 나갈 때 `true/false`.
- 구조 데이터는 `TEXT` 에 JSON 문자열(컬럼 이름 `meta`, `extra`, `snapshot`). D1의 JSON 함수(`json_each`, `json_extract`) 사용 가능.
- 모든 사용자 입력 문자열은 저장 전 **NFC 정규화 + 앞뒤 공백 제거**(`src/lib/text.ts` 의 `normalizeText`). 길이 제한은 **유니코드 코드 포인트 수**(Go의 `utf8.RuneCountInString`, JS의 `[...s].length`)로 센다.
- handle 은 저장 시 소문자. 정규식 `^[a-z0-9_]{3,20}$`.

### 1.4 삭제 정책

- 사용자가 지운 메시지·글·방명록·스토리는 **소프트 삭제**(`deleted_at`, `deleted_by`) + 본문을 빈 문자열로 덮어쓴다(검색 색인에서도 빠짐). 스레드/답글 트리 구조 유지를 위해 행은 남긴다.
- 운영자 강제 삭제도 같은 소프트 삭제 + 첨부 이미지는 R2에서 **즉시 실제 삭제**.
- 관계 테이블(좋아요, 리액션, 팔로우 등)은 실제 삭제(DELETE).
- D1은 외래 키 제약을 기본으로 강제한다. FK는 아래 SQL 대로 선언하며, 행을 실제 삭제하는 경우만 `ON DELETE CASCADE` 를 쓴다.

### 1.5 D1 제약 (Workers Paid, 2026-11 공식 문서 확인)

| 항목 | 한도 | 설계상 대응 |
|---|---|---|
| DB 크기 | 10 GB | 이미지는 R2, 이벤트 로그는 DO 저장소 |
| 쿼리당 바인딩 파라미터 | 100 | 가변 길이 ID 목록은 `IN (SELECT value FROM json_each(?))` 로 **파라미터 1개**에 JSON 배열로 넘긴다 |
| SQL 문 길이 | 100,000 바이트 | — |
| 행/문자열/BLOB | 2 MB | 본문 길이 제한으로 충분 |
| 테이블당 컬럼 | 100 | — |
| 호출당 쿼리 수 | 1,000 (`batch()` 는 1개로 셈) | 다건 쓰기는 `db.batch([...])` |
| 쿼리 시간 | 30초 | 인덱스 필수, 전체 스캔 금지 |

- `db.batch()` 는 **하나의 트랜잭션**으로 실행된다(전부 성공 또는 전부 롤백). 여러 테이블을 같이 바꾸는 쓰기(메시지 삽입 + 채널 last_message 갱신 + 멘션 삽입 등)는 반드시 하나의 batch 로 묶는다.
- D1 읽기 복제(Sessions API)는 쓰지 않는다(쓰기 직후 읽기 일관성이 필요하므로).

## 2. 마이그레이션

- 위치: `server/migrations/`. 이름: `NNNN_snake_case.sql` (4자리 0채움, 1부터). wrangler 의 `d1 migrations create` 규칙과 같다.
  - `0001_init.sql` — 3장의 모든 테이블·인덱스
  - `0002_search.sql` — 4장의 FTS5 가상 테이블·트리거
  - `0003_focus_started_at.sql` — 3.9 `user_presence.focus_started_at` 열 추가(집중 시작 시각)
  - 이후 변경은 `0004_...` 부터 **추가만** 한다. 적용된 마이그레이션 파일은 고치지 않는다.
  - **v0.1.3 번호 예약** (계획 색인 2장): `0004_cards.sql`(P0 — 3.12 `messages.card_json`), `0005_collab.sql`(P0 — 3.13 `collab_*`), `0006_leet.sql`(P1 — 3.14 `leet_*` + `user_settings.leet_share_with_friends`), `0007_workspace.sql`(P2). 그 뒤 번호가 필요하면 `0008` 부터 머지 순서대로, 먼저 이 목록과 색인을 고친다.
- 로컬 적용: `npx wrangler d1 migrations apply DB --local`. 테스트는 `readD1Migrations()` + `applyD1Migrations()` 로 같은 파일을 적용(`server-architecture.md` 7장).
- 원격 적용은 **오너 건별 승인 후**에만.
- 전역 운영자 지정(최초 1회, 오너가 직접): `UPDATE users SET role = 'admin' WHERE handle = '<handle>';` — 운영자 승격 API는 두지 않는다.

## 3. 스키마 — `0001_init.sql`

> SQLite 는 아직 정의되지 않은 테이블을 FK 로 참조하는 선언을 허용한다(검사는 DML 시점). 아래 순서 그대로 실행한다.

### 3.1 계정·세션

```sql
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
```

### 3.2 관계 (친구·차단·팔로우)

```sql
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
```

### 3.3 방(Space)·초대

```sql
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
```

### 3.4 채널·DM (하나의 `channels` 테이블)

채널 종류(`kind`):
- `text` — 방 안의 채널. `space_id` 필수. `is_private=0` 이면 방 멤버 전원이 접근, `1` 이면 `channel_members` 에 있으면서 **그 방의 현재 멤버**인 사람만(방이 삭제되면 아무도). 방에서 나가기·강퇴·밴 시 같은 batch 에서 그 방 비공개 채널의 `channel_members` 행을 지운다(`api.md` 3.5).
- `dm` — 1:1 DM. `space_id IS NULL`, `channel_members` 정확히 2행, `dm_key = '<작은 user_id>:<큰 user_id>'` 로 유일.
- `group_dm` — 그룹 DM(최대 10명, 만든 사람 포함). `owner_id` = 만든 사람.

```sql
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
```

- 안 읽음·멘션 수(`api.md` `ChannelMyState`, `GET /v1/me/unreads`)는 이 기준점보다 큰 **최상위** 메시지만 센다. 계산은 ① `channels.last_message_id > 기준점` 인 채널만 고르고 ② 채널마다 `idx_messages_channel_top`·`idx_message_mentions_user` 범위 스캔을 `LIMIT 1000` 하위 질의로 끊는다(배지 상한 999). 호출당 읽는 행 ≤ 안 읽은 채널 수 × 1,000.

### 3.5 메시지·스레드·리액션·고정

```sql
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
```

### 3.6 업로드 (R2 메타데이터)

```sql
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
```

### 3.7 SNS (글·좋아요·리포스트·해시태그·트렌드·스토리)

```sql
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
```

### 3.8 투표·방명록

> 삽입 순서(FK): 한 batch 안에서 `polls` → `poll_options` → `messages`/`posts`(이때 `poll_id` 설정). 메시지/글 ID 는 batch 전에 `newId()` 로 미리 만들어 `polls.context_id` 에 넣는다. 방 생성도 같은 방식: `spaces`(미리 만든 `default_channel_id`) → `channels` → `space_members`.

```sql
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
```

### 3.9 알림·접속 상태

```sql
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
```

`0003_focus_started_at.sql` — D1 미러에 집중 **시작** 시각도 둔다. UserHub 가 `focus_ends_at` 과 **함께** 쓴다(집중 중이면 둘 다 값, 아니면 둘 다 NULL; 집중을 연장해도 시작 시각은 그대로). D1 에서 `PresenceState` 를 읽는 경로(`Me.presence` 의 가입 응답, UserHub 에 닿지 못할 때의 대체 읽기)는 이 값을 `Focus.started_at` 으로 그대로 쓴다.

```sql
ALTER TABLE user_presence ADD COLUMN focus_started_at INTEGER; -- ms; non-NULL exactly when focus_ends_at is
```

### 3.10 신고·운영

```sql
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
```

### 3.11 테이블 → 기능 대조표

| 기능(설계서 3장) | 테이블 |
|---|---|
| 가입·로그인·세션 | users, user_settings, sessions |
| 친구·차단·DM 허용 | friend_requests, friendships, blocks, user_settings.dm_policy |
| 방·권한·초대·강퇴 | spaces, space_members, space_bans, space_invites, space_direct_invites |
| 채널·DM·그룹 DM | channels, channel_members, channel_prefs |
| 메시지·스레드·리액션·멘션·고정·안 읽음 | messages, thread_participants, message_reactions, message_mentions, message_pins, channel_reads |
| 검색 | messages_fts, posts_fts (4장) |
| 피드·답글·좋아요·리포스트·인용 | posts, post_likes, reposts, post_mentions |
| 사진 | uploads, post_media (+ R2) |
| 해시태그·트렌드 | post_hashtags, trends |
| 스토리 | stories, story_views |
| 팔로우(비공개 승인) | follows |
| 알림함 | notifications |
| 투표 | polls, poll_options, poll_votes |
| 방명록 | guestbook_entries |
| 상태 메시지 | users.status_* |
| 접속 상태·집중 모드 | user_presence (+ UserHub DO) |
| 신고·운영자 | reports, admin_actions, users.role / suspended_* |
| 채팅 카드 (v0.1.3) | messages.card_json (3.12) |
| 방 문서·버전·댓글 (v0.1.3) | collab_docs, collab_versions, collab_comments (3.13) + CollabHub DO SQLite |
| LeetCode 오늘의 문제 (v0.1.3) | leet_daily, leet_space_settings, leet_daily_roots, leet_solves, user_settings.leet_share_with_friends (3.14) |

### 3.12 채팅 카드 — `0004_cards.sql` (v0.1.3)

```sql
ALTER TABLE messages ADD COLUMN card_json TEXT;   -- JSON {type, v, data} (api.md 2장 Card); NULL = not a card
```

- DB 의 `kind` 는 `'text'` 로 둔다. 응답 직렬화는 `card_json` 이 있으면 `kind:"card"`, `card = JSON.parse(card_json)`, 없으면 `card: null`. 기존 `kind` CHECK 제약에 `'card'` 를 넣으려고 **테이블을 다시 만들지 않는다** — SQLite 의 테이블 재생성은 `ON DELETE CASCADE` 로 이어진 자식 행(리액션·멘션·고정·스레드 참여자)을 지운다.
- 검색 색인(`messages_fts`)은 `body`(대체 요약)를 그대로 쓴다. 카드 `data` 는 색인하지 않는다.
- 카드 메시지 삭제는 기존 메시지 삭제 규칙(1.4) 그대로이며 `card_json` 도 비운다(`NULL`).

### 3.13 방 문서 — `0005_collab.sql` (v0.1.3, v0.1.3 설계서 C1.4)

```sql
CREATE TABLE collab_docs (
  id          TEXT PRIMARY KEY,
  space_id    TEXT NOT NULL REFERENCES spaces(id) ON DELETE CASCADE,
  type        TEXT NOT NULL CHECK (type IN ('arch','apicol')),
  title       TEXT NOT NULL,                              -- 1..80
  created_by  TEXT NOT NULL REFERENCES users(id),
  created_at  INTEGER NOT NULL,
  updated_at  INTEGER NOT NULL,                           -- last edit reflected by the DO (batched, <= 30s late)
  head_seq    INTEGER NOT NULL DEFAULT 0,                 -- mirrored from the CollabHub DO
  locked      INTEGER NOT NULL DEFAULT 0 CHECK (locked IN (0,1)),
  deleted_at  INTEGER
);
CREATE INDEX idx_collab_docs_space ON collab_docs(space_id, type, updated_at) WHERE deleted_at IS NULL;

CREATE TABLE collab_versions (                            -- metadata; the snapshot body lives in the DO
  doc_id        TEXT NOT NULL REFERENCES collab_docs(id) ON DELETE CASCADE,
  no            INTEGER NOT NULL,                         -- 1.. per doc
  seq           INTEGER NOT NULL,
  name          TEXT,                                     -- 1..60 for named versions; NULL for auto / restore versions
  auto          INTEGER NOT NULL DEFAULT 0 CHECK (auto IN (0,1)),
  restored_from INTEGER,                                  -- version no a restore copied
  created_by    TEXT REFERENCES users(id),                -- NULL for auto versions
  created_at    INTEGER NOT NULL,
  PRIMARY KEY (doc_id, no)
);

CREATE TABLE collab_comments (
  id          TEXT PRIMARY KEY,
  doc_id      TEXT NOT NULL REFERENCES collab_docs(id) ON DELETE CASCADE,
  anchor      TEXT NOT NULL,                              -- 1..128
  author_id   TEXT NOT NULL REFERENCES users(id),
  body        TEXT NOT NULL,                              -- 1..2000; '' when deleted
  root_id     TEXT REFERENCES collab_comments(id),
  resolved_at INTEGER,
  created_at  INTEGER NOT NULL,
  deleted     INTEGER NOT NULL DEFAULT 0 CHECK (deleted IN (0,1))
);
CREATE INDEX idx_collab_comments_doc ON collab_comments(doc_id, created_at);
```

- 문서 **내용**(편집 기록·최신 스냅숏·버전 스냅숏 본문·잠금/삭제 표시)은 D1 이 아니라 문서마다 `CollabHub` DO 의 SQLite 에 있다: `ops(seq INTEGER PRIMARY KEY, author TEXT, ts INTEGER, client_op_id TEXT UNIQUE, ops TEXT)`, `snapshot(id INTEGER PRIMARY KEY CHECK(id=1), seq, body)`, `versions(no INTEGER PRIMARY KEY, seq, body)`, `meta(k PRIMARY KEY, v)`. 접속자 커서·선택(presence)은 메모리에만. 형식은 `docs/contract/collab.md`.
- 문서 삭제는 소프트(`deleted_at`). 방 삭제도 소프트(`spaces.deleted_at`, api.md 5.7)라 `ON DELETE CASCADE` 는 실행되지 않고 문서 행은 `deleted_at IS NULL` 로 남는다. 그래서 삭제된 방의 문서는 모든 문서 경로(REST·`/ws`)가 `spaces.deleted_at` 을 보고 `404` 로 숨기고(api.md 5.24), 열린 소켓은 방 삭제 batch 커밋 뒤 `kickFromSpaceDocs(…, "space_deleted")` 로 닫는다(collab.md 4.1). `ON DELETE CASCADE` 는 행을 실제로 지울 때(운영자 정리 등)만 작동한다. 기록 버전(자동·되돌리기)의 보관 정리(30일·문서당 200개, api.md 5.24)는 DO alarm 이 하고 D1 `collab_versions` 행도 함께 지운다.
- R2 는 쓰지 않는다(JC2 — 문서 한도가 작아 DO SQLite 로 충분).

### 3.14 LeetCode 오늘의 문제 — `0006_leet.sql` (v0.1.3, P1 · 설계서 A10.3)

```sql
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
```

- **문제 본문·HTML·코드 스니펫·예제는 어떤 표에도 없다.** 서버는 LeetCode 를 부르지 않는다(fetch·Cron 없음). 저장하는 것은 사실 정보(`leet_daily`)와 풀이 기록(`leet_solves`)뿐이고, 사용자가 고른 공유 코드만 풀이 카드의 `messages.card_json`(3.12, `data.code.text`)에 들어간다. LeetCode 가 사실 정보 저장도 문제 삼으면 `leet_daily` 하나만 비우면 된다.
- `leet_daily` 는 처음 보고한 값을 지킨다. 같은 날 다른 값의 보고는 `mismatch_count` 만 올린다(운영자 `PUT /v1/admin/leet/daily/{date}` 만 덮어쓴다).
- `leet_daily_roots` 는 채널·날짜마다 #daily 루트를 하나로 묶는 경합 장치다: `INSERT OR IGNORE`(`message_id` NULL)로 먼저 차지한 요청만 루트 카드를 만들고 `message_id` 를 채운다. 만들다 실패하면 그 NULL 행을 지운다. NULL 로 30초 넘게 남은 행은 다음 요청이 `claimed_at` 비교로 넘겨받는다(api.md 5.25).
- `leet_solves.passed_at` 은 첫 기록의 서버 시각을 그대로 둔다. 연속 풀이는 `utcDate(passed_at) = date` 인 날만 채운다(유예 없음, 설계서 JA26).
- 삭제(1.4 와의 관계): `leet_*` 는 메시지가 아니라 관계·기록 표라 **실제 삭제**한다 — `DELETE /v1/leet/solves/{date}` 는 `leet_solves` 행을 지우고 풀이 카드 메시지는 1.4 의 소프트 삭제를 따른다. 방이 지워져도(소프트) `leet_space_settings` 는 남지만 방이 살아 있는 동안만 읽힌다. 채널을 지우면(소프트) 그 방의 #daily 는 꺼진 것과 같이 취급되고 루트를 만들지 않는다.
### 3.15 작업공간 공유 — `0007_workspace.sql` (v0.1.3 P2, 설계서 B10)

```sql
CREATE TABLE workspace_shares (
  id          TEXT PRIMARY KEY,
  owner_id    TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  title       TEXT NOT NULL,                              -- 1..40 code points (user data)
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
  joined_at   INTEGER,                                    -- last share-socket connect
  left_at     INTEGER,                                    -- last share-socket close
  removed_at  INTEGER,                                    -- the owner removed them, or they left (DELETE .../participants/@me)
  max_level   TEXT CHECK (max_level IN ('viewer','prompter','editor','keyboard')),
  verified    INTEGER NOT NULL DEFAULT 0 CHECK (verified IN (0,1)),
  PRIMARY KEY (share_id, user_id),
  UNIQUE (share_id, seat)
);
CREATE INDEX idx_workspace_share_members_user ON workspace_share_members(user_id, share_id);

ALTER TABLE user_settings ADD COLUMN notify_workspace INTEGER NOT NULL DEFAULT 1 CHECK (notify_workspace IN (0,1));
```

- 이 두 표에는 위 열만 있다(서버 시험이 `PRAGMA table_info` 로 확인). 경로·파일 이름·명령·화면·키·확인 숫자는 D1 에도 DO 저장소에도 쓰지 않는다. `WorkspaceHub` DO 는 공유가 살아 있는 동안 공유 메타(id·오너·제목)와 단계표·확인 여부(평문 메타정보)만 DO 저장소에 두고, 공유가 끝나면 이를 지우고 끝남 표시만 남긴다. 최근 키프레임 묶음 암호문은 메모리에만 둔다(`docs/contract/workspace.md` 4장).
- "60분에 10회" 공유 만들기 한도는 `idx_workspace_shares_owner` 로 `created_at` 을 센다(api.md 0.7).
- `max_level`·`verified` 는 오너가 보낸 `ctl_levels` 의 최고 단계·확인 여부를 DO 가 옮겨 적는다. `joined_at`·`left_at` 은 DO 가 연결·종료 때 적는다.

## 4. 검색 — `0002_search.sql` (FTS5)

D1은 SQLite FTS5 모듈을 지원한다(공식 문서 확인). 한국어는 띄어쓰기 단위에 조사가 붙어(`배포는`, `배포를`) 단어 토크나이저로는 잘 안 맞으므로 **`trigram` 토크나이저**를 쓴다 — 3글자 이상 부분 문자열 일치, 대소문자 무시.

```sql
CREATE VIRTUAL TABLE messages_fts USING fts5(
  body, content='messages', content_rowid='rowid', tokenize='trigram'
);
CREATE TRIGGER messages_fts_ai AFTER INSERT ON messages BEGIN
  INSERT INTO messages_fts(rowid, body) VALUES (new.rowid, new.body);
END;
CREATE TRIGGER messages_fts_ad AFTER DELETE ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
END;
CREATE TRIGGER messages_fts_au AFTER UPDATE OF body ON messages BEGIN
  INSERT INTO messages_fts(messages_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
  INSERT INTO messages_fts(rowid, body) VALUES (new.rowid, new.body);
END;

CREATE VIRTUAL TABLE posts_fts USING fts5(
  body, content='posts', content_rowid='rowid', tokenize='trigram'
);
CREATE TRIGGER posts_fts_ai AFTER INSERT ON posts BEGIN
  INSERT INTO posts_fts(rowid, body) VALUES (new.rowid, new.body);
END;
CREATE TRIGGER posts_fts_ad AFTER DELETE ON posts BEGIN
  INSERT INTO posts_fts(posts_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
END;
CREATE TRIGGER posts_fts_au AFTER UPDATE OF body ON posts BEGIN
  INSERT INTO posts_fts(posts_fts, rowid, body) VALUES ('delete', old.rowid, old.body);
  INSERT INTO posts_fts(rowid, body) VALUES (new.rowid, new.body);
END;
```

검색 실행 규칙 (`src/lib/search.ts`):
1. 질의 정규화: NFC, 앞뒤 공백 제거, 길이 1..100. 공백으로 나눈 각 낱말을 `"..."` 로 감싸고(내부 `"` 는 `""` 로) `AND` 로 잇는다 → FTS 문법 주입 차단.
2. **모든 낱말이 3글자 이상**이면: `SELECT m.* FROM messages_fts f JOIN messages m ON m.rowid = f.rowid WHERE messages_fts MATCH ?1 AND <접근 범위 조건> AND m.deleted_at IS NULL ORDER BY m.id DESC LIMIT ?`.
3. 3글자 미만 낱말이 있으면: FTS 대신 `m.body LIKE '%' || ?낱말 || '%'` (낱말마다 AND) 를 **접근 범위 조건과 함께**, 최근 메시지부터 최대 5,000행만 훑는다(`m.id >= <90일 전 ULID 하한>` 조건 추가).
4. 접근 범위(메시지): 내가 멤버인 (삭제 안 된) 방의 공개 `text` 채널 + 내가 `channel_members` 에 있는 채널(비공개 text 는 그 방의 현재 멤버일 때만, DM·그룹 DM). `json_each` 로 채널 ID 목록을 넘긴다(최대 500개, 넘으면 `422 limit_exceeded`, `details: {what:"search_scope", limit:500}` — 클라이언트는 `channel_id`/`space_id` 로 범위를 좁힌다. `api.md` 5.13 과 같음).
5. 접근 범위(글): `api.md` 3.4 "글 열람 규칙"과 동일(비공개 계정·차단·정지 제외).
6. 하이라이트는 서버가 본문에서 각 낱말의 위치를 대소문자 무시로 찾아 `highlights: [{start, length}]`(코드 포인트 단위)로 돌려준다.

주의:
- `trigram` 토크나이저는 SQLite 3.34+ 기능이다. 서버 팀은 `test/search.test.ts` 의 첫 테스트로 `0002_search.sql` 적용과 한글 3글자 검색을 검증한다. 만약 D1(로컬 workerd 포함)이 `trigram` 을 거부하면 `tokenize='unicode61'` + 접두 질의(`"낱말"*`)로 바꾸고 이 문서를 고친다.
- `wrangler d1 export` 는 가상 테이블이 있는 DB를 내보내지 못한다. 백업은 D1 Time Travel(Paid 기본 제공)에 의존한다.

## 5. R2 키 규칙

- 버킷 바인딩: `MEDIA` (bucket_name `masil-media`, 로컬은 wrangler가 자동 시뮬레이션).
- 키: `<purpose 복수형>/<owner_user_id>/<upload_id>.<ext>`
  - `avatars/01JC.../01JD....png`
  - `posts/01JC.../01JD....jpg`
  - `stories/01JC.../01JD....webp`
  - `ext` 는 실제 바이트로 판별한 형식: `png | jpg | gif | webp`.
- `put` 옵션: `httpMetadata.contentType` = 판별된 MIME, `customMetadata = { upload_id, owner_id, purpose }`.
- 객체는 **불변**. 수정은 새 업로드. 삭제는 `uploads.status='removed'` + `MEDIA.delete(key)`.
- 이미지는 공개 버킷 URL로 노출하지 않는다. 항상 `GET /v1/media/{upload_id}` (인증 필요, 열람 권한 검사) 를 통해 Worker가 스트리밍한다.

## 6. 보존·주기 작업 (Cron Trigger)

`wrangler.jsonc` 의 `triggers.crons` 두 개. `scheduled(controller)` 에서 `controller.cron` 문자열로 분기한다.

| cron | 이름 | 작업 |
|---|---|---|
| `*/5 * * * *` | trends | 최근 24시간 `post_hashtags` 집계 → `trends` 테이블 통째 교체(batch: DELETE + INSERT 10행). 순위 = `COUNT(DISTINCT author_id)` 내림차순, 동점은 `COUNT(*)` 내림차순, 다음은 tag 오름차순. 정지·비공개 계정 글과 삭제된 글은 제외. |
| `17 * * * *` | cleanup | 아래 정리 작업을 순서대로, 각 작업 한 번에 최대 500행씩(남으면 다음 시간에 계속) |

cleanup 작업 목록:
1. **스토리 만료**: `expires_at < now - 3_600_000` 인 스토리 → 첨부 R2 객체 삭제, `uploads.status='removed'`, `story_views` 와 `stories` 행 DELETE. (열람 API는 `expires_at > now` 로 거르므로 만료 즉시 안 보인다. cron 은 실제 삭제만 담당.)
2. **고아 업로드**: `status='pending' AND created_at < now - 86_400_000` → R2 삭제 + `status='removed'`.
3. **세션**: `expires_at < now - 30일` 또는 `revoked_at < now - 30일` → DELETE.
4. **알림**: `created_at < now - 90일` → DELETE.
5. **친구 요청·직접 초대**: `status <> 'pending' AND responded_at < now - 30일` → DELETE. 30일 넘은 `pending` 은 그대로 둔다.
6. **초대 링크**: `revoked_at` 또는 `expires_at` 이 30일 이상 지난 것 → DELETE.
7. **상태 메시지 만료**: `status_expires_at < now` → `status_emoji/status_text/status_expires_at = NULL` (API 응답은 이미 만료분을 null 로 보여 줌).
8. **정지 만료**: `suspended_until < now` → `suspended_at/suspended_until/suspend_reason = NULL` (인증 미들웨어는 이미 만료를 활성으로 취급).
9. **접속 상태 정정**: `status <> 'offline' AND online_until < now` → `status='offline'`.
10. **끝난 작업공간 공유** (v0.1.3 P2): `status='ended' AND ended_at < now - 30일` 인 `workspace_shares` 행 → DELETE(멤버 행은 `ON DELETE CASCADE`).

DO 내부 보존(D1 아님): UserHub 이벤트 로그는 사용자당 최근 2,000건 또는 24시간 중 짧은 쪽(`events.md` 6.2). TopicHub 시청자 임대는 300초.

## 7. 비정규화 카운터 유지 규칙

카운터는 원본 행 변경과 **같은 `db.batch()`** 안에서 `UPDATE ... SET x = x + 1` 로 바꾼다. 음수 방지를 위해 감소는 `MAX(x - 1, 0)`.

| 카운터 | 증가 | 감소 |
|---|---|---|
| users.follower_count / following_count | follows 가 `active` 가 될 때 | active follow 삭제 |
| users.post_count | 글 작성 | 글 삭제 |
| spaces.member_count | space_members INSERT | DELETE |
| channels.member_count | channel_members INSERT | DELETE |
| messages.reply_count / last_reply_at | 스레드 답글 INSERT | 답글 삭제(소프트) 시 감소, last_reply_at 유지 |
| posts.like_count / repost_count / reply_count / quote_count | 각 행 INSERT | 각 행 DELETE 또는 답글·인용 글 소프트 삭제 |
| polls.voter_count, poll_options.vote_count | 투표 | 투표 취소/변경 |
| space_invites.use_count | 초대 수락 | — |
