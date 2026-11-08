# masil REST API 계약 v1

- 상태: 확정 계약 v1 (2026-11-05). 상위 출처: `docs/superpowers/specs/2026-11-05-masil-design.md` (충돌 시 설계서 우선).
- 짝 문서: `events.md`(WebSocket), `data-model.md`(D1·R2), `server-architecture.md`(서버 코드 구조).
- 이 문서가 서버(`server/`)와 Go 클라이언트(`internal/api`)의 **단일 출처**다. 필드 이름·타입·오류 코드를 바꾸려면 이 문서를 먼저 고친다.
- 표기: 객체 스키마는 TypeScript 표기. `ID` = ULID 문자열, `Timestamp` = RFC 3339 UTC 밀리초 문자열(`"2026-11-05T02:03:04.567Z"`). `?` 가 붙은 필드는 요청에서 생략 가능. 응답 필드는 **항상 존재**하며 값이 없으면 `null`(빈 배열은 `[]`).

---

## 0. 기본 규약

### 0.1 주소·버전

- 기본 주소: `https://masil.<account>.workers.dev` (클라이언트에 기본값 내장, `MASIL_SERVER` 환경변수 또는 `~/.config/masil/config.toml` 의 `server` 로 변경). 로컬 개발: `http://localhost:8787`.
- 모든 경로는 `/v1` 접두사. 아래 표기에서 `/v1` 을 포함해 쓴다.
- 요청·응답 본문은 `application/json; charset=utf-8` (업로드·미디어 제외).
- 클라이언트는 모든 요청에 `User-Agent: masil/<semver> (<os>; <arch>)` 를 보낸다(예: `masil/0.1.0 (darwin; arm64)`).
- 응답 헤더 `X-Request-Id: <ULID>` — 오류 보고용.
- 응답 본문의 알 수 없는 필드는 클라이언트가 무시한다(추가는 하위 호환). 요청의 알 수 없는 필드는 서버가 무시한다.

### 0.2 사용자 참조 `{user}`

경로 매개변수 `{user}` 및 요청 본문의 `user` 필드는 다음 세 형식 중 하나다.
- `@<handle>` — 예: `/v1/users/@minji` (`@` 는 경로에 그대로 써도 된다. RFC 3986 pchar)
- `<user_id>` — ULID 26자
- `@me` — 나 자신

없는 사용자 → `404 not_found`.

### 0.3 시각·길이·문자열

- 문자열 길이는 **NFC 정규화 후 유니코드 코드 포인트 수**. 서버는 앞뒤 공백을 제거하고 저장한다.
- 주요 길이 제한(위반 시 `400 invalid_request`, `details.fields.<필드>="too_long"|"too_short"|"invalid"`):

| 필드 | 제한 |
|---|---|
| handle | `^[a-z0-9_]{3,20}$` (입력 대문자는 소문자로 바꿔 저장), 예약어 불가: `me, admin, masil, here, system, root, support` |
| password | 8..128 |
| display_name | 1..32 |
| bio | 0..160 |
| status.text | 0..64, status.emoji 이모지 1개 |
| message body | 1..4000 |
| post body | 0..500 (미디어·투표·인용이 없으면 1..500) |
| story body | 0..200 |
| guestbook body | 1..300 |
| space name | 1..50, description 0..200 |
| channel name (text) | `^[a-z0-9가-힣_-]{1,32}$` |
| channel topic | 0..250 |
| group_dm name | 0..50 |
| poll question | 1..200, 선택지 2..10개, 각 1..80 |
| report note | 0..1000 |
| client_nonce | 1..64, `[A-Za-z0-9_-]` |
| 검색어 q | 1..100 |

### 0.4 멱등성

- `POST /v1/channels/{id}/messages`, `POST /v1/messages/{id}/thread`, `POST /v1/posts` 는 `client_nonce` **필수**. 같은 작성자가 같은 `client_nonce` 로 다시 보내면 새로 만들지 않고 기존 객체를 `200` 으로 돌려준다(처음은 `201`). 클라이언트는 재시도 시 같은 nonce를 쓴다.
- `PUT` 토글(좋아요·리액션·고정·팔로우·차단 등)은 본래 멱등이다.

### 0.5 오류 봉투

모든 오류(4xx/5xx)는 다음 형태:

```json
{
  "error": {
    "code": "invalid_request",
    "message": "body: too_long",
    "details": { "fields": { "body": "too_long" } }
  }
}
```

- `code`: 아래 표의 기계용 문자열. 클라이언트는 `code` 로 분기한다.
- `message`: 사람용 영어 한 줄(UI에 그대로 띄우지 않는다. 클라이언트가 `code` 별 한국어 문구를 갖는다).
- `details`: 코드별 추가 정보 객체, 없으면 `{}`.

| HTTP | code | 의미 / details |
|---|---|---|
| 400 | `invalid_json` | 본문이 JSON이 아님 |
| 400 | `invalid_request` | 검증 실패. `details.fields: Record<string, "required"\|"too_long"\|"too_short"\|"invalid"\|"not_allowed">` |
| 401 | `unauthorized` | 토큰 없음/무효/만료/폐기 |
| 401 | `invalid_credentials` | 로그인 실패(handle 없음과 비밀번호 틀림을 구분하지 않음) |
| 403 | `account_suspended` | 정지된 계정. `details: { until: Timestamp\|null, reason: string }` |
| 403 | `forbidden` | 권한 부족(역할·소유자 아님) |
| 403 | `not_admin` | 전역 운영자 전용 |
| 403 | `dm_not_allowed` | DM 허용 규칙 불충족(차단 여부를 드러내지 않음) |
| 403 | `action_not_allowed` | 차단·비공개 등으로 상호작용 불가(사유를 드러내지 않음) |
| 403 | `read_only` | 읽기 전용 대화(DM 조건 소멸) |
| 403 | `private_account` | 비공개 계정의 글/팔로워 목록 열람 불가 |
| 403 | `banned_from_space` | 방에서 차단(ban)된 사용자 |
| 404 | `not_found` | 없음 **또는 볼 권한 없음**(존재 여부를 숨김) |
| 409 | `handle_taken` | 가입 시 handle 중복 |
| 409 | `already_exists` | 이미 존재(대기 중인 친구 요청, 열린 신고 등) |
| 409 | `already_member` | 이미 방/채널 멤버 |
| 409 | `conflict` | 상태 충돌. `details.reason` (예: `"owner_cannot_leave"`, `"default_channel"`, `"self"`) |
| 409 | `poll_closed` | 마감된 투표 |
| 410 | `invite_invalid` | 초대 무효. `details.reason: "expired"\|"exhausted"\|"revoked"` |
| 413 | `payload_too_large` | 업로드 5MB 초과 또는 JSON 본문 64KB 초과 |
| 415 | `unsupported_media_type` | 허용되지 않은 이미지 형식 |
| 422 | `limit_exceeded` | 개수 한도 초과. `details: { what: string, limit: number }` (예: `{what:"group_dm_members", limit:10}`) |
| 426 | `upgrade_required` | 클라이언트 버전이 `min_client_version` 미만. `details.min_client_version` |
| 429 | `rate_limited` | 속도 제한. `details: { limiter: string, retry_after_sec: number }` + `Retry-After` 헤더 |
| 500 | `internal_error` | 서버 오류 |
| 503 | `service_unavailable` | 일시 장애. 재시도 가능 |

`limit_exceeded.details.what` 값: `group_dm_members, post_media, reactions_per_message, pins_per_channel, channels_per_space, owned_spaces, pending_friend_requests, pending_uploads, active_stories, watch_topics, poll_options, space_members, search_scope`.

### 0.6 페이지네이션 (커서)

- 목록 엔드포인트 공통: 쿼리 `limit`(기본 30, 최대 100), `cursor`(이전 응답의 `next_cursor`).
- 응답: `{ "items": [...], "next_cursor": string | null }`. `null` 이면 끝.
- 커서는 **불투명 문자열**(서버 내부: base64url JSON `{k: 정렬키, i: 마지막 ID}`). 클라이언트는 해석·생성하지 않는다. 손상된 커서 → `400 invalid_request` (`fields.cursor="invalid"`).
- 기본 정렬은 최신순(ID 내림차순). 예외는 각 엔드포인트에 명시.
- **메시지 목록만** 커서 대신 ID 앵커(`before`/`after`/`around`)를 쓴다(5.10 참조).

### 0.7 속도 제한

- Workers Rate Limiting 바인딩 사용. 한도는 **Cloudflare 위치(PoP)별 근사치**이며 전 세계 합산이 아니다(공식 문서). 정확한 상한이 아니라 남용 방지용이다.
- 초과 시 `429 rate_limited`, `Retry-After: <limiter의 period 초>` 헤더, `details.retry_after_sec` 동일 값. 남은 횟수 헤더는 제공하지 않는다(바인딩이 제공하지 않음).
- 클라이언트는 429를 받으면 `retry_after_sec` 동안 같은 종류의 요청을 멈추고 상태줄에 표시한다. 자동 재시도는 메시지 전송에 한해 1회.

| 리미터(바인딩) | 한도 | 키 | 적용 엔드포인트 |
|---|---|---|---|
| `RL_SIGNUP` | 5회 / 60초 | `ip:<CF-Connecting-IP>` | `POST /v1/auth/signup` |
| `RL_LOGIN` | 10회 / 60초 | `ip:<IP>` **그리고** `handle:<handle>` (둘 다 검사) | `POST /v1/auth/login`, `POST /v1/auth/password`(현재 비밀번호 확인 — 같은 버킷을 공유) |
| `RL_MESSAGE` | 20회 / 10초 | `u:<user_id>` | 메시지·스레드 답글 전송, 메시지 수정 |
| `RL_POST` | 10회 / 60초 | `u:<user_id>` | 글·답글·인용·스토리·방명록 작성 |
| `RL_REACT` | 30회 / 10초 | `u:<user_id>` | 리액션, 좋아요, 리포스트, 투표, 고정 (읽음 표시는 제외) |
| `RL_UPLOAD` | 20회 / 60초 | `u:<user_id>` | `POST /v1/uploads` |
| `RL_SOCIAL` | 30회 / 60초 | `u:<user_id>` | 친구 요청, 팔로우, 차단, DM 생성, 채널 멤버 추가, 초대 생성·직접 초대, 방 생성, 신고 |
| `RL_SEARCH` | 30회 / 60초 | `u:<user_id>` | `/v1/search/*`, `/v1/users/search`, `/v1/hashtags/search` |
| `RL_WS` | 30회 / 60초 | `u:<user_id>` | `GET /v1/ws` 연결 시도 |
| `RL_API` | 600회 / 60초 | `u:<user_id>` | 인증된 모든 요청(위 전용 리미터와 **추가로**) |

---

## 1. 인증

### 1.1 모델

- 가입 수단: handle + 비밀번호. 이메일 없음, 비밀번호 재설정 없음(설계서 논골).
- 로그인 성공 시 **불투명 세션 토큰** 발급. 서버에는 토큰의 SHA-256 해시만 저장(`sessions.token_hash`)하므로 폐기 가능하고 DB 유출 시에도 토큰 원문은 없다.
- 모든 인증 요청: `Authorization: Bearer <token>`. WebSocket 업그레이드 요청도 같은 헤더(`events.md` 1장). 토큰을 URL 쿼리에 넣지 않는다.
- 클라이언트 저장: `~/.config/masil/credentials.json` (권한 0600), TUI와 데몬이 공유. 형식은 `client-architecture.md` 가 정한다.

### 1.2 비밀번호 해시

- 알고리즘: **PBKDF2-HMAC-SHA256** (WebCrypto `crypto.subtle.deriveBits`), **100,000회**, 솔트 16바이트(`crypto.getRandomValues`), 출력 32바이트.
  - 100,000 은 Workers 런타임이 허용하는 PBKDF2 반복 상한이다(workerd `checkPbkdfLimits`: 상한 초과 시 `NotSupportedError`). 상한 안에서 최대값을 쓴다.
- 저장 형식(`users.password_hash`): `pbkdf2_sha256$100000$<salt base64url 무패딩>$<hash base64url 무패딩>`. 반복 횟수를 문자열에 담아 나중에 바꿀 수 있게 한다(검증 시 저장된 값 사용).
- 비교는 `crypto.subtle.timingSafeEqual`. 없는 handle 로 로그인 시에도 더미 해시를 한 번 계산해 응답 시간을 맞춘다.
- 비밀번호 규칙: 8..128 코드 포인트, handle 과 같으면 안 됨(`fields.password="invalid"`).

### 1.3 토큰·세션

- 토큰 형식: `msl_` + base64url(무패딩) 32바이트 난수 = 총 47자. 예: `msl_Q2xhdWRlIGlzIGEgZ29vZCBib3kgYW5kIHdyaXRlcyBnb29k`.
- `sessions.token_hash` = 토큰 원문(접두사 포함) UTF-8 바이트의 SHA-256 소문자 hex.
- 만료: 미사용 90일(슬라이딩). 요청 시 `last_used_at` 이 1시간 이상 지났으면 `last_used_at=now`, `expires_at=now+90일` 로 갱신.
- 인증 실패(`401 unauthorized`): 헤더 없음, 형식 오류, 해시 불일치, `revoked_at` 존재, `expires_at < now`.
- 정지 계정: 정지 시 모든 세션 폐기(`revoke_reason='suspended'`). 그 뒤 로그인 시도 → `403 account_suspended`.
- 401 을 받은 클라이언트는 저장된 토큰을 지우고 로그인 화면으로. 데몬은 조용히 종료 대기(재로그인 시 TUI가 새 토큰 저장).

### 1.4 엔드포인트

#### `POST /v1/auth/signup`
- 인증 없음 · `RL_SIGNUP`
- 요청:
```json
{ "handle": "minji", "password": "correct horse battery", "display_name": "민지", "client": "tui", "device_name": "minji-mbp" }
```
  - `display_name?` 생략 시 handle. `client?`: `"tui"|"daemon"|"admin"|"other"` (기본 `"tui"`). `device_name?` 0..64.
- 응답 `201`: `{ "token": string, "session": Session, "user": Me }`
- 오류: `400 invalid_request`, `409 handle_taken`, `429`.
- 부수효과: `users`, `user_settings`(기본값), `user_presence` 행 생성.

#### `POST /v1/auth/login`
- 인증 없음 · `RL_LOGIN`
- 요청: `{ "handle": "minji", "password": "...", "client"?: "tui", "device_name"?: "minji-mbp" }`
- 응답 `200`: `{ "token": string, "session": Session, "user": Me }`
- 오류: `401 invalid_credentials`, `403 account_suspended`, `429`.

#### `POST /v1/auth/logout`
- 인증 필요. 현재 세션 폐기(`revoke_reason='logout'`), 이 세션의 WebSocket 은 코드 `4001` 로 닫힘.
- 응답 `204`.

#### `GET /v1/auth/sessions`
- 응답 `200`: `{ "items": Session[] }` (내 활성 세션 전부, 최신순, 페이지네이션 없음)

#### `DELETE /v1/auth/sessions/{session_id}`
- 내 세션만. 해당 세션의 소켓 `4001` 로 닫힘. 응답 `204`. 남의 세션 → `404`.

#### `POST /v1/auth/password`
- 인증 필요 · `RL_LOGIN`(`ip:<IP>` 와 `handle:<내 handle>`, 로그인과 **같은 버킷**). 현재 비밀번호 확인 전에 검사한다 — 세션 토큰을 가진 사람이 현재 비밀번호를 무제한 대입하지 못하게. 한도 초과 시 요청은 비밀번호를 바꾸지 않으며, 세션도 폐기하지 않는다(`handle:` 버킷은 로그인과 공유라 남이 소진시킬 수 있으므로, 폐기하면 남이 소유자를 로그아웃시킬 수 있다).
- 요청: `{ "current_password": string, "new_password": string }`
- 현재 세션을 제외한 모든 세션 폐기(`password_changed`). 응답 `204`.
- 오류: `401 invalid_credentials`(현재 비밀번호 틀림), `400`, `429 rate_limited`(`RL_LOGIN`).

---

## 2. 공통 객체 스키마

```ts
type ID = string;          // ULID
type Timestamp = string;   // RFC 3339 UTC, ms precision

type PresenceStatus = "online" | "away" | "dnd" | "offline";
type SpaceRole = "owner" | "admin" | "member";

interface UserStatus {               // custom status message ("🎧 듣는 중…")
  emoji: string | null;
  text: string;
  expires_at: Timestamp | null;
}

interface UserSummary {              // embedded everywhere
  id: ID;
  handle: string;
  display_name: string;
  avatar_url: string | null;         // "/v1/media/<upload_id>" (relative; prefix server base URL); null while suspended (3.9)
  status: UserStatus | null;         // null if unset or expired; null while suspended (3.9)
  is_private: boolean;
  suspended: boolean;
}

interface Relationship {             // viewer -> target
  is_friend: boolean;
  friend_request: { id: ID; direction: "incoming" | "outgoing" } | null;  // pending only
  following: "none" | "pending" | "active";     // me -> them
  followed_by: "none" | "pending" | "active";   // them -> me
  blocked: boolean;                  // I blocked them (never reveals whether they blocked me)
  can_dm: boolean;                   // DM eligibility now (3.1)
  can_view_posts: boolean;           // 3.4
  shared_space_ids: ID[];            // up to 10
}

interface Profile extends UserSummary {
  bio: string;                       // "" for a suspended account seen by others (3.9)
  created_at: Timestamp;
  follower_count: number;            // the three counts are 0 for a suspended account seen by others (3.9)
  following_count: number;
  post_count: number;
  presence: PresenceStatus | null;   // null if not visible to viewer (3.6)
  last_seen_at: Timestamp | null;    // null if not visible
  relationship: Relationship | null; // null when viewing self
}

interface Settings {
  dm_policy: "friends_and_spaces" | "friends_only";
  is_private: boolean;
  notify: {                          // desktop alert toggles (events.md "alert")
    dm: boolean;                     // default true
    mention: boolean;                // default true
    invite: boolean;                 // default true
    friend_request: boolean;         // default true
    thread_reply: boolean;           // default true
    social: boolean;                 // likes/reposts/follows/post replies/quotes/guestbook; default false
  };
  focus_default_min: number;         // 1..720, default 25
}

interface Focus {
  started_at: Timestamp;             // when this focus began; extending it (POST /me/focus while focusing) keeps it.
                                     // Exact on every path, the D1 mirror included (data-model.md user_presence.focus_started_at);
                                     // only a row written before migration 0003 (start NULL) reports its last write, clamped to ends_at
  ends_at: Timestamp;
}

interface PresenceState {
  status: PresenceStatus;            // effective
  mode: "auto" | "away" | "dnd";     // user's manual choice
  focus: Focus | null;
}

interface Me extends UserSummary {
  bio: string;
  created_at: Timestamp;
  role: "user" | "admin";
  follower_count: number;
  following_count: number;
  post_count: number;
  settings: Settings;
  presence: PresenceState;
}

interface Session {
  id: ID;
  client: "tui" | "daemon" | "admin" | "other";
  device_name: string;
  created_at: Timestamp;
  last_used_at: Timestamp;
  expires_at: Timestamp;
  current: boolean;                  // the session making this request
}

interface Media {
  id: ID;                            // upload id
  url: string;                       // "/v1/media/<id>"
  content_type: "image/png" | "image/jpeg" | "image/gif" | "image/webp";
  size: number;                      // bytes
  width: number | null;
  height: number | null;
}

interface Upload extends Media {
  purpose: "avatar" | "post" | "story";
  status: "pending" | "attached" | "removed";
  created_at: Timestamp;
}

interface FriendRequest {
  id: ID;
  from: UserSummary;
  to: UserSummary;
  status: "pending" | "accepted" | "declined" | "canceled";
  created_at: Timestamp;
}

interface Friend { user: UserSummary; since: Timestamp; presence: PresenceStatus }

interface SpacePreview { id: ID; name: string; description: string; icon_emoji: string | null; member_count: number }

interface Space extends SpacePreview {
  owner_id: ID;
  default_channel_id: ID;
  created_at: Timestamp;
  my_role: SpaceRole;
}

interface SpaceDetail extends Space { channels: Channel[] }   // channels visible to me, ordered by position

interface SpaceMember { user: UserSummary; role: SpaceRole; joined_at: Timestamp }

interface Invite {                   // invite link
  code: string;                      // 8 chars
  space: SpacePreview;
  created_by: UserSummary;
  max_uses: number | null;
  use_count: number;
  expires_at: Timestamp | null;
  revoked: boolean;
  created_at: Timestamp;
}

interface InvitePreview {            // what a non-member sees for a code
  code: string;
  space: SpacePreview;
  created_by: UserSummary;
  expires_at: Timestamp | null;
  already_member: boolean;
}

interface DirectInvite {
  id: ID;
  space: SpacePreview;
  inviter: UserSummary;
  invitee: UserSummary;
  status: "pending" | "accepted" | "declined" | "canceled";
  created_at: Timestamp;
}

interface ChannelMyState {
  notify: "all" | "mentions" | "none";
  muted: boolean;
  last_read_message_id: ID | null;   // null = no read row yet (baseline: see below)
  unread_count: number;              // top-level non-system messages by others after the read pointer; capped at 999
  mention_count: number;             // TOP-LEVEL messages after the read pointer that mention me (explicit or @here expansion);
                                     // thread-reply mentions are NOT counted (inbox/alert only); capped at 999
}
// Read pointer = channel_reads.last_read_message_id, or, if there is no row, the baseline
// idFloor(joined_at): channel_members.joined_at for private text/dm/group_dm, space_members.joined_at for public text.
// Messages from before I joined never count as unread.

interface Channel {
  id: ID;
  kind: "text" | "dm" | "group_dm";
  space_id: ID | null;
  name: string | null;               // text: channel name (no '#'); group_dm: optional name; dm: null
  topic: string;
  is_private: boolean;               // text only; dm/group_dm: false
  position: number;
  owner_id: ID | null;               // group_dm creator
  member_count: number;              // public text: space member_count
  members: UserSummary[] | null;     // dm/group_dm: all members (<=10); text: null
  last_message_id: ID | null;
  last_message_at: Timestamp | null;
  created_at: Timestamp;
  read_only: boolean;
  read_only_reason: "not_eligible" | "blocked" | null;  // "blocked" only shown to the blocker
  my: ChannelMyState;
}

interface ChannelRef { id: ID; kind: "text" | "dm" | "group_dm"; name: string | null; space_id: ID | null; space_name: string | null }

interface UnreadState {
  channel_id: ID;
  unread_count: number;
  mention_count: number;
  last_message_id: ID | null;
  last_read_message_id: ID | null;
}

interface MentionRef { user_id: ID; handle: string }

interface ReactionSummary { emoji: string; count: number; me: boolean }

interface ThreadSummary {
  reply_count: number;
  last_reply_at: Timestamp | null;
  participant_ids: ID[];             // most recent first, <= 5
}

interface RollResult { expr: string; rolls: number[]; modifier: number; total: number }

interface SystemEvent {
  event: "member_joined" | "member_left" | "member_kicked" | "channel_created" | "channel_renamed"
       | "channel_topic_changed" | "message_pinned" | "group_renamed" | "group_member_added"
       | "group_member_removed";
  actor_id: ID;
  target_user_id: ID | null;
  data: { name?: string; topic?: string; message_id?: ID };
}

interface PollOption { idx: number; text: string; vote_count: number }

interface Poll {
  id: ID;
  context_type: "message" | "post";
  context_id: ID;
  creator_id: ID;
  question: string;
  multi: boolean;
  options: PollOption[];
  voter_count: number;
  closes_at: Timestamp | null;
  closed: boolean;                   // closed_at set OR closes_at <= now
  my_votes: number[];                // option idx list
}

interface PollInput {
  question: string;
  options: string[];                 // 2..10
  multi?: boolean;                   // default false
  duration_sec?: number | null;      // 60..604800; null/omitted = until closed manually
}

interface Message {
  id: ID;
  channel_id: ID;
  author: UserSummary;
  kind: "text" | "me" | "roll" | "poll" | "system";
  body: string;                      // raw markdown subset; "" if deleted; roll: the expression; poll: ""
  mentions: MentionRef[];
  mention_here: boolean;
  thread_root_id: ID | null;         // set => this is a thread reply
  thread: ThreadSummary | null;      // roots with >= 1 reply
  reactions: ReactionSummary[];      // ordered by first reaction time
  pinned: boolean;
  poll: Poll | null;
  roll: RollResult | null;
  system: SystemEvent | null;
  client_nonce: string | null;
  created_at: Timestamp;
  edited_at: Timestamp | null;
  deleted: boolean;                  // tombstone (only returned when it has thread replies)
}

type QuotedPost =
  | { available: true; post: Post }                       // post.quote is always null here (1 level)
  | { available: false; id: ID; reason: "deleted" | "not_visible" };

interface Post {
  id: ID;
  author: UserSummary;
  body: string;                      // "" if deleted
  media: Media[];                    // 0..4, ordered
  poll: Poll | null;
  reply_to_id: ID | null;
  root_id: ID | null;
  reply_to_author: UserSummary | null;
  quote: QuotedPost | null;
  hashtags: string[];                // normalized, no '#'
  mentions: MentionRef[];
  like_count: number;
  repost_count: number;
  reply_count: number;
  quote_count: number;
  viewer: { liked: boolean; reposted: boolean };
  created_at: Timestamp;
  deleted: boolean;
}

interface TimelineItem {
  id: ID;                            // post id (type=post) or repost id (type=repost); cursor anchor
  type: "post" | "repost";
  post: Post;
  reposted_by: UserSummary | null;
  at: Timestamp;                     // post.created_at or repost time
}

interface Story {
  id: ID;
  author: UserSummary;
  body: string;
  media: Media | null;
  bg: "accent" | "accent2" | "accent3" | "accent4" | "plain";  // theme color slot
  created_at: Timestamp;
  expires_at: Timestamp;
  seen: boolean;
  view_count: number | null;         // author only, else null
}

interface StoryGroup { user: UserSummary; stories: Story[]; has_unseen: boolean; latest_at: Timestamp }

type NotificationType =
  | "follow" | "follow_request" | "follow_accepted"
  | "friend_request" | "friend_accepted"
  | "space_invite"
  | "post_like" | "post_repost" | "post_reply" | "post_quote" | "post_mention"
  | "message_mention" | "thread_reply"
  | "guestbook_entry";

interface Notification {
  id: ID;
  type: NotificationType;
  actor: UserSummary | null;
  subject: { type: "user" | "post" | "message" | "space" | "friend_request" | "space_direct_invite" | "guestbook_entry"; id: ID };
  preview: string;                   // <= 100 code points, e.g. post/message snippet; "" if none
  context: {
    post_id: ID | null;
    channel_id: ID | null;
    message_id: ID | null;
    thread_root_id: ID | null;
    space_id: ID | null;
    space_name: string | null;
  };
  created_at: Timestamp;
  read: boolean;
}

type AlertKind = "dm" | "mention" | "thread_reply" | "invite" | "friend_request"
               | "channel_message" | "social" | "focus_summary";

interface Alert {                    // desktop-notification candidate (events.md 4.7)
  id: ID;
  kind: AlertKind;
  title: string;                     // ready to display, <= 80 code points (e.g. "민지 · #개발")
  body: string;                      // <= 200 code points, plain text (markdown stripped)
  actor: UserSummary | null;
  target: {
    type: "channel" | "message" | "post" | "space" | "user" | "notification";
    id: ID;
    channel_id: ID | null;
    message_id: ID | null;
    thread_root_id: ID | null;
    post_id: ID | null;
    space_id: ID | null;
  };
  created_at: Timestamp;
  suppressed: boolean;               // true during focus/dnd: do not pop an OS notification
  suppressed_reason: "focus" | "dnd" | null;
}

interface FocusSummary {
  started_at: Timestamp;
  ended_at: Timestamp;
  counts: { dm: number; mention: number; thread_reply: number; invite: number; friend_request: number; other: number };
  items: Alert[];                    // suppressed alerts during focus, newest first, <= 20
}

interface GuestbookEntry {
  id: ID;
  owner_id: ID;
  author: UserSummary | null;        // null when hidden
  body: string;                      // "" when hidden or deleted
  is_secret: boolean;
  hidden: boolean;                   // secret entry viewed by someone other than owner/author
  created_at: Timestamp;
  deleted: boolean;
}

interface SearchHighlight { start: number; length: number }   // in code points of body

interface AdminUserView extends UserSummary {
  role: "user" | "admin";
  created_at: Timestamp;
  suspended_at: Timestamp | null;
  suspended_until: Timestamp | null;
  suspend_reason: string | null;
  open_report_count: number;
}

interface Report {
  id: ID;
  reporter: UserSummary;
  target_type: "user" | "message" | "post" | "story" | "guestbook_entry" | "space";
  target_id: ID;
  target_user: UserSummary | null;
  reason: "spam" | "harassment" | "illegal" | "sexual" | "violence" | "other";
  note: string;
  snapshot: Record<string, unknown>; // copy at report time: {body?, media_urls?, name?, author_id?, channel_id?, ...}
  target_exists: boolean;            // false if already removed
  open_reports_on_target: number;
  status: "open" | "resolved" | "dismissed";
  resolution: "content_removed" | "user_suspended" | "no_action" | null;
  resolution_note: string | null;
  resolved_by: UserSummary | null;
  resolved_at: Timestamp | null;
  created_at: Timestamp;
}

interface AdminAction {
  id: ID;
  admin: UserSummary;
  action: "remove_content" | "suspend_user" | "unsuspend_user" | "resolve_report" | "dismiss_report";
  target_type: string;
  target_id: ID;
  report_id: ID | null;
  reason: string;
  created_at: Timestamp;
}
```

### 2.1 본문 문법 (메시지·글 공통)

- 마크다운 부분 집합(렌더는 클라이언트): `**굵게**`, `*기울임*`/`_기울임_`, `` `인라인 코드` ``, 펜스 코드 블록(```` ```lang ```` … ```` ``` ````), `[텍스트](https://…)`, 맨 URL 자동 링크. 그 외 마크다운은 일반 텍스트로 보인다(백슬래시 이스케이프도 없다 — 다만 백슬래시 바로 뒤의 `*`·`_` 는 강조 기호가 아니다, CA 4.8). 서버는 본문을 변형하지 않는다(정규화·trim 제외).
- 멘션: `@handle` — 정규식 `(?<![A-Za-z0-9_])@([a-z0-9_]{3,20})(?![a-z0-9_])` (대소문자 무시 후 소문자). 코드 블록·인라인 코드 안의 `@` 는 멘션 아님. 클라이언트 입력기는 표시 이름 자동완성 후 `@handle` 을 삽입한다.
  - **메시지 멘션 해석**: 서버는 `@handle` 을 다음을 모두 만족하는 사용자 U 로만 해석한다 — U 가 존재하고, U 가 그 채널을 **읽을 수 있고**(`requireChannelRead(channel, U)` 통과, 3.7), 작성자↔U 사이에 어느 방향으로도 차단이 없다. 조건을 못 넘는 `@handle` 은 그냥 글자로 남고 `mentions[]`·`message_mentions`·알림함·alert 어디에도 들어가지 않는다(비공개 채널·다른 방·그룹 DM 밖 사용자에게 내용이 새거나 아무나 호출하는 것을 막음). 작성자 자신은 `mentions[]` 에는 들어가도 알림·배지 대상은 아니다.
  - **`@here`**: 메시지를 보내는 순간 그 채널을 읽을 수 있는 사용자 가운데 작성자를 빼고, 작성자와 차단 관계가 아니며, `user_presence.status` 가 `online` 또는 `away` 인 사람으로 확장한다. 즉 **TUI(full 연결)가 떠 있는 사람만** 해당하고(데몬만 떠 있으면 `offline`), `dnd`(수동 방해금지·집중 모드)인 사람은 빠진다. 1:1 DM 에서는 확장하지 않는다(상대가 유일한 독자이므로 의미 없음). 확장된 사람은 `message_mentions.is_here=1` 로만 기록되고 **`Message.mentions[]` 에는 명시적 `@handle` 멘션만** 들어간다(`mention_here:true` 로 `@here` 사용 여부만 표시).
  - 글의 멘션은 그 글을 볼 수 있는 사람만(5.15).
- 해시태그(글·스토리만): `(?<![\p{L}\p{N}_])#([\p{L}\p{N}_]{1,50})` → NFC + 소문자로 정규화. 글당 최대 10개(초과분 무시). 메시지에서는 해시태그를 해석하지 않는다.
- 주사위 식(`kind:"roll"`): `^(\d{1,2})d(\d{1,4})([+-]\d{1,4})?$`, 개수 1..20, 면 2..1000, 보정 -1000..1000. 서버가 `crypto.getRandomValues` 로 굴린다.

---

## 3. 권한 규칙

### 3.1 DM 허용 (설계서 2장)

`can_dm(A, B)` 는 아래가 **모두** 참일 때 참:
1. `A ≠ B`, B 가 정지 상태가 아님.
2. A↔B 어느 방향으로도 차단(`blocks`)이 없음.
3. 다음 중 하나:
   - A 와 B 가 친구(`friendships`), 또는
   - A 와 B 가 같은 방(삭제되지 않은 space)의 멤버이고, **A 와 B 둘 다** `dm_policy = "friends_and_spaces"` (어느 한쪽이라도 `friends_only` 면 친구만).

적용:
- 1:1 DM 생성·메시지 전송은 매번 `can_dm` 을 검사. 기존 1:1 DM 에서 조건이 사라지면 대화는 남되 `read_only=true`. 전송 시 `403 read_only`. `read_only_reason` 은 차단한 쪽에게만 `"blocked"`, 그 외 `"not_eligible"`.
- 그룹 DM 생성: 만든 사람 C 에 대해 모든 참여자 X 에 `can_dm(C, X)` 이어야 하고, **참여자끼리도 어느 쌍이든 차단 관계(어느 방향이든)가 없어야** 함. 참여자 수(만든 사람 포함) 3..10. 2명이면 1:1 DM 으로 처리. 실패 응답은 `403 dm_not_allowed` 하나로, 두 경우를 이 순서로 판정한다(오너 결정 D2, 2026-11-06 — 0.5 "차단 여부를 드러내지 않음"):
  - `can_dm(C, X)` 실패가 하나라도 있으면 `details.user_ids` 에 그 X 들(요청 순서대로). 1:1 DM 과 같은 규칙이라 그 실패가 차단 때문인지는 드러나지 않는다.
  - 그렇지 않고 참여자 사이 차단 쌍만 있으면 `details: {}` — **누가 누구를 차단했는지(어느 ID 인지, 어느 방향인지) 응답 어디에도 담지 않는다**(`message` 도 일반 문구). 클라이언트 문구: "함께 대화할 수 없는 참여자가 있어요".
- 그룹 DM 멤버 추가: 추가하는 사람 M 에 대해 `can_dm(M, X)`, 그리고 기존 멤버 누구와도, 함께 추가되는 다른 사람과도 X 가 차단 관계가 아니어야 함(아래). 총원 10 초과 → `422 limit_exceeded`.
- 그룹 DM 안에서의 전송은 멤버이기만 하면 가능(쌍별 조건 재검사 안 함). **[내 결정 — 설계서 2장과 다름, 오너가 뒤집을 수 있음]** 설계서는 "친구 끊기/방 탈퇴로 조건이 사라지면 기존 대화는 읽기 전용"이라고 하지만, 이 계약은 그 규칙을 **1:1 DM 에만** 적용한다. 그룹 DM 은 여러 사람의 대화라서 두 사람 사이 관계가 끊겼다고 방 전체를 읽기 전용으로 만들면 나머지 멤버까지 막히기 때문이다. 대신:
  - 관계가 끊긴 쪽은 언제든 스스로 나갈 수 있고(`DELETE .../members/@me`), 만든 사람은 멤버를 뺄 수 있다(3.7).
  - **차단은 그룹 DM 안에서도 효과가 있다**(설계서 "차단당한 쪽은 새 메시지·멘션 불가"): 차단당한 B 의 `@a` 는 멘션으로 해석되지 않고(2.1) A 에게 알림·alert·배지가 가지 않는다. A 의 클라이언트는 B 의 메시지를 접어 보인다(3.2). B 가 그룹에 보내는 메시지 자체는 다른 멤버를 위해 막지 않는다.
  - 차단 관계인 사람을 그룹 DM 에 새로 넣을 수 없다(생성·추가 같은 규칙): 넣는 사람 M 에 대한 `can_dm(M, X)` 에 더해, 기존 멤버 또는 함께 들어가는 다른 사람 중 X 와 차단 관계인 사람이 있으면 `403 dm_not_allowed`(`details: {}` — 생성과 같이 `can_dm(M, X)` 실패만 `details.user_ids` 로 알리고, 차단 쌍은 어느 ID 도 담지 않는다). 동시 추가 경쟁에서 진 쪽도 같은 응답. 그래서 A 가 B 를 차단했으면 제3자 C 도 A·B 를 한 그룹 DM 에 넣을 수 없다.

### 3.2 차단 효과 (A 가 B 를 차단)

차단 순간(같은 batch):
- 친구 관계 삭제, 양방향 대기 친구 요청 `canceled`, 양방향 팔로우(대기 포함) 삭제, 양방향 대기 직접 초대 `canceled`.
- 위 정리가 지우는 관계를 새로 만드는 쓰기 — 팔로우(카운터 포함), 친구 요청, 친구 요청 수락(친구 관계), 방 직접 초대 — 는 차단(어느 방향이든)·상대 정지(3.9, `suspended_until <= 지금` 이면 정지 아님) 확인을 사전 확인뿐 아니라 **쓰기 문 자체에도** 넣는다(직접 초대는 친구 관계도). 사전 확인과 쓰기 사이에 차단·정지가 끼어들면 아무것도 만들어지지 않고(카운터도 그대로) `403 action_not_allowed`. 이미 팔로우 중이던 멱등 경우는 지금 상태를 그대로 돌려준다. 메시지·리액션·DM 생성·방명록·글 상호작용처럼 차단 정리가 지우지 않는 쓰기는 사전 확인만 한다(끼어든 차단 직전에 쓴 것과 결과가 같다).

이후:
- B → A: DM·그룹 DM 초대·친구 요청·팔로우·방 직접 초대·방명록 작성·글에 답글/인용/리포스트/좋아요 불가(`403 action_not_allowed` 또는 `dm_not_allowed`). 1:1 DM 은 양쪽 모두 읽기 전용.
- B 의 멘션(`@a`)·답글·스레드 답글은 A 에게 알림·데스크톱 알림을 만들지 않는다.
- SNS 는 **양방향 숨김**: A 와 B 는 서로의 글·스토리·방명록·팔로워 목록을 볼 수 없다(`404 not_found`), 타임라인·검색·해시태그 피드·답글 트리에서 서로의 글이 빠진다(답글 트리에서 빠진 자리는 하위 답글과 함께 생략).
- 같은 방·채널·그룹 DM 안의 메시지는 서버가 그대로 보낸다. **A 의 클라이언트가** 차단 목록(`GET /v1/blocks` + `block.updated` 이벤트)으로 B 의 메시지를 "차단한 사용자의 메시지" 한 줄로 접는다. 메시지 검색 결과에서는 서버가 A 기준으로 B 의 메시지를 뺀다.
- A 의 `GET /v1/dms` 에서 B 와의 1:1 DM 이 빠진다. 알림함에서 B 가 행위자인 알림이 빠진다.
- 차단 해제해도 친구·팔로우 관계는 복구되지 않는다.

### 3.3 비공개 계정 (P 가 `is_private=true`)

- P 의 글·리포스트·스토리·팔로워/팔로잉 목록·방명록은 **P 본인과 `active` 팔로워만** 본다. 다른 사람이 요청하면 `403 private_account` (프로필 자체는 공개: 이름·소개·카운트).
- P 를 팔로우하면 `pending` → P 가 승인해야 `active`.
- P 의 글은 발견 타임라인·해시태그 피드·트렌드 집계·검색(비팔로워)에서 빠진다. P 의 글은 **누구도 리포스트·인용할 수 없다**(`403 action_not_allowed`). 답글은 볼 수 있는 사람만.
- P 가 공개로 바꾸면 대기 중 팔로우 요청은 모두 `active` 로 바뀌고 각 요청자에게 `follow_accepted` 알림.

### 3.4 글 열람 규칙 `can_view_post(V, post)`

다음을 모두 만족:
1. `post.deleted_at IS NULL` (답글 트리·인용에서는 삭제된 글도 `deleted:true` 자리표시로 나간다).
2. 작성자 비정지(정지 계정 글은 운영자만 봄).
3. V↔작성자 차단 없음.
4. 작성자 공개 계정 **또는** V = 작성자 **또는** V 가 작성자의 `active` 팔로워.

스토리·방명록·투표(글 첨부)·미디어(`/v1/media`)도 같은 규칙을 따른다. 방명록 비밀글은 추가로 주인·작성자만 본문을 본다.

### 3.5 방(Space) 권한

| 행동 | owner | admin | member |
|---|:-:|:-:|:-:|
| 방 정보 수정(이름·설명·아이콘) | ✓ | ✓ | |
| 방 삭제, 방장 위임 | ✓ | | |
| 역할 변경(admin↔member) | ✓ | | |
| 채널 생성·수정·삭제·순서 변경 | ✓ | ✓ | |
| 비공개 채널 멤버 추가/제거 | ✓ | ✓ | 그 비공개 채널의 멤버는 **추가만** 가능 |
| 강퇴·밴 | ✓(본인 외 전원) | ✓(member 만) | |
| 초대 링크 생성·폐기·목록 | ✓ | ✓ | |
| 친구 직접 초대(초대받는 사람이 **초대하는 사람의 친구**여야 함 — 역할 무관, 5.8) | ✓ | ✓ | ✓ |
| 메시지 고정/해제 | ✓ | ✓ | |
| 남의 메시지 삭제 | ✓ | ✓ | |
| 메시지 작성·리액션·스레드 | ✓ | ✓ | ✓ |
| 방 나가기 | 위임 후 | ✓ | ✓ |

- 초대는 낸 사람의 권한이 남아 있는 동안만 유효하다. 방에서 빠지면(**나가기·강퇴·밴 모두**) 같은 batch 안에서 그 사람이 그 방에 보낸 대기 중 직접 초대는 `canceled`, 만든 초대 링크는 폐기(`revoked`)된다. admin→member 로 강등되면(링크 권한 상실) 그 사람이 만든 링크도 같은 batch 에서 폐기된다(다시 admin 이 돼도 되살아나지 않음). 직접 초대 수락은 초대한 사람이 아직 방 멤버인지 사전 확인과 조건부 UPDATE 양쪽에서 다시 본다. 링크도 쓸 때마다(조회·수락의 사전 확인·조건부 UPDATE) 만든 사람이 아직 그 방 owner·admin 인지 다시 보며, 아니면 `revoked` 로 취급한다(폐기가 빠진 경로가 있어도 링크로 들어올 수 없음). 만들 때도 링크·직접 초대의 INSERT 자체가 권한(링크: owner·admin, 직접 초대: 멤버)을 다시 확인하므로, 권한 확인과 INSERT 사이에 강등·제거가 끼어들면 아무것도 만들어지지 않는다.
- **친구 끊기 → 직접 초대 취소 [오너 결정 D3]**: 직접 초대는 초대받는 사람이 초대한 사람의 친구인 동안만 유효하다. 친구를 끊으면(`DELETE /v1/friends/{user}`, 5.3) 같은 batch 안에서 두 사람 사이의 대기 중 직접 초대가 **양방향 모두**(어느 방이든) `canceled` 되고, 커밋 뒤 취소된 초대마다 `space.invite_removed`(`reason:"canceled"`, 초대받은 사람 + 초대한 사람)를 보낸다. 차단도 같은 정리를 한다(3.2). 직접 초대 수락은 초대한 사람의 방 멤버십과 함께 **두 사람이 아직 친구이고 어느 방향으로도 차단이 없는지**를 사전 확인과 조건부 UPDATE 양쪽에서 다시 보며, 아니면 `404`(아무것도 쓰지 않음). 이 조건을 못 채우는 대기 초대(정리가 빠진 경로가 있더라도)는 받은 초대 목록에서 빠지고, 새 초대의 `already_exists` 로 세지 않는다(새 초대가 그 묵은 초대를 `canceled` 로 바꾼다, 5.8).
- **이미 방에 들어온 사람에게 걸린 직접 초대 [대기 목록 9-b]**: 직접 초대는 초대받는 사람이 방 밖에 있는 동안만 뜻이 있다. 어떤 경로로든(초대 링크·다른 가입 경로) 방에 들어오면 가입과 같은 batch 안에서 그 사람에게 걸린 그 방의 대기 중 직접 초대가 `canceled` 되고, 커밋 뒤 취소된 초대마다 `space.invite_removed`(`reason:"canceled"`, 초대받은 사람 + 초대한 사람)를 보낸다. 직접 초대의 INSERT 도 초대받는 사람이 아직 방 밖인지 다시 보므로, 사전 확인 뒤 INSERT 전에 그 사람이 들어오면 아무것도 만들어지지 않고 `409 already_member`. 정리가 빠진 경로로 멤버에게 대기 초대가 남아 있어도 받은 초대 목록에서 빠지고(수락은 `409 already_member`), 그 사람이 방에서 빠질 때(나가기·강퇴·밴) 같은 batch 에서 `canceled` 되므로, 옛 초대로 강퇴된 사람이 다시 들어올 수 없다.
- 방 멤버가 아니면(또는 방이 삭제됐으면) 방·채널·메시지 모두 `404 not_found`. text 채널의 읽기 검사는 비공개 채널이라도 **방 멤버십과 방 미삭제를 함께** 확인한다(`channel_members` 행만으로 통과하지 않음). 실시간 청중도 같다(`events.md` 6.1).
- 비공개 채널은 그 채널 멤버만 보고(방장·관리자라도 멤버가 아니면 목록에 안 보임), 공개 채널은 방 멤버 전원.
- 방에서 빠지면(**스스로 나가기·강퇴·밴 모두**) 같은 batch 안에서 그 방의 모든 비공개 채널의 `channel_members` 행도 지운다(`channels.member_count` 감소 포함). 밴은 강퇴 + 초대 수락 차단.
- 방이 삭제되면 그 방의 채널은 모두 소프트 삭제되고, 위 읽기 검사의 "방 미삭제" 조건 때문에 `channel_members` 행이 남아 있어도 접근·청중에서 빠진다.
- 기본 채널(`default_channel_id`, 생성 시 `일반`)은 삭제 불가(`409 conflict`, `reason:"default_channel"`).
- 한도: 사용자당 소유 방 100개, 방당 채널 100개, 방당 멤버 5,000명.

### 3.6 접속 상태 공개 범위

`presence`, `last_seen_at` 은 본인, 친구, 같은 방 멤버, 같은 DM/그룹 DM 멤버에게만 보인다. 그 외에는 `null`.
- 예외: 정지 중인 계정(3.9)은 이 범위 안의 사람에게도 `offline`·`last_seen_at: null` 로만 보인다(프로필은 `presence`·`last_seen_at` 모두 `null`).

### 3.7 DM·채널 메시지 권한 요약

| 행동 | 조건 |
|---|---|
| 읽기 | 채널 접근 가능(공개 text: 삭제 안 된 방의 멤버 / 비공개 text: `channel_members` + 삭제 안 된 방의 멤버 / dm·group_dm: `channel_members`) |
| 쓰기 | 읽기 + (1:1 DM 이면 `can_dm`) |
| 수정 | 작성자 본인, `kind` 가 `text`/`me` |
| 삭제 | 작성자 본인 / 방 owner·admin(그 방 채널) / 전역 운영자(`/v1/admin`) |
| 고정 | text 채널: owner·admin / dm·group_dm: 멤버 누구나 |
| 그룹 DM 이름·주제 변경 | 멤버 누구나 |
| 그룹 DM 멤버 제거 | 만든 사람(owner_id), 또는 본인 나가기 |

### 3.8 전역 운영자

- `users.role='admin'`. `/v1/admin/*` 만 추가 권한을 준다(일반 API 에서 비공개 채널을 읽을 권한은 없다).
- 지정은 오너가 D1 SQL 로 직접(`data-model.md` 2장).

### 3.9 정지 계정

- 로그인·API·소켓 불가(`403 account_suspended`). 정지 순간 세션 폐기 + 소켓 `4003` 종료.
- 다른 사람 화면: `UserSummary.suspended=true`. 정지 계정의 글·스토리·방명록 글은 피드·검색·트렌드에서 빠진다. 채팅 메시지는 남는다.
- **정지 계정은 핸들·표시 이름만 [오너 결정 D1]**: 정지 중에는 본인 외 누구에게나 그 계정이 핸들·표시 이름·`suspended=true` 로만 보인다.
  - 프로필(`GET /v1/users/{user}`, 5.2): `bio: ""`, `status: null`, `avatar_url: null`, `follower_count`·`following_count`·`post_count` 는 `0`, `presence`·`last_seen_at` 은 `null`. `id`·`created_at`·`is_private` 와 `relationship`(열람자 자신의 상태 — 내가 차단했는지 등; 정지 중이므로 `can_dm=false`, `can_view_posts=false`)은 그대로.
  - 그 밖의 모든 `UserSummary`(사용자 목록·`GET /v1/users?ids=`·친구·멤버·메시지 작성자·이벤트 등): `status: null`, `avatar_url: null`.
  - 접속 상태(3.6 범위 안의 사람에게도): `GET /v1/presence`·`presence.snapshot`·`presence.updated` 의 항목은 `status: "offline"`, `last_seen_at: null`(결과에서 빠지지는 않는다), `Friend.presence` 는 `offline`. 정지 순간 소켓이 끊겨 나가는 `presence.updated` 도 같다.
  - 친구 목록(`GET /v1/friends`, 5.3)의 정렬·`next_cursor` 도 정지 중인 친구를 offline 으로 친다(실제 `dnd` 여도 offline 무리에 이름순). 정지가 끝나면 실제 상태로 다시 정렬된다.
  - 정지·정지 해제(`/v1/admin/users/{user}/suspend`·`/unsuspend`) 때:
    - 그 계정의 `summaryWatchers`(친구 + DM/그룹 DM 상대 + 같은 방 멤버, `events.md` 6.1)에 `user.updated`(`suspended`·`status`·`avatar_url` 이 바뀐 `UserSummary`)가 나간다. 기록 이벤트라서, 끊겼다가 `resume` 하는 사람은 정지 전 이벤트(가리지 않은 작성자 요약)를 재생받은 **뒤에** 이 `user.updated` 를 받는다.
    - 그 계정의 `presenceWatchers` 에 `presence.updated` 가 상태 변화와 상관없이 나간다 — 정지는 `offline`·`last_seen_at: null`, 해제는 실제 항목. 수동 `dnd`·집중 모드는 소켓이 없어도 유지되므로(`events.md` 6.3) 정지 순간의 연결 종료만으로는 바뀌지 않기 때문이다.
    - `suspended_until` 이 지나 저절로 풀릴 때는 이벤트가 없다(다음 조회에서 보인다).
  - 그 계정의 아바타 이미지(`GET /v1/media/{upload_id}`, 5.14)는 본인·운영자 외 `404`.
  - 운영자 화면(`/v1/admin/*` 의 `AdminUserView`·`Report.reporter`·`Report.target_user`·`AdminAction.admin`)은 가리지 않는다.
  - 정지가 끝나면(`suspended_until <= 지금`) 모두 다시 보인다. 판정 경계는 3.4 규칙 2 와 같다.
- 남의 글에 박힌 정지 계정 글도 숨는다(REST·이벤트 공통): 인용은 `quote: { available:false, reason:"not_visible" }`, 정지 계정 글에 단 답글은 `reply_to_author: null`. 정지가 끝나면 다시 나온다.
- 정지 중에는 그 계정이 행위자인 알림함 행(미리보기 포함)도 목록·`unread_count` 에서 빠진다(5.18). 정지가 끝나면(`suspended_until` 경과) 다시 보인다.
- **재생 로그도 같은 규칙 [대기 목록 4-a]**: UserHub 로그(최대 24시간/2,000건, `events.md` 6.2)에 정지 **전에** 기록된 그 계정의 `notification.created`·`alert` 는 `resume` 재전송에서 빠지고, 재전송되는 `focus.ended` 요약의 `items` 에서도 빠진다(`events.md` 3.3 — 로그 자체는 소급 삭제하지 않으므로 정지가 끝난 뒤의 재개에서는 다시 나온다). 집중 모드가 끝날 때 만드는 요약(`focus.ended`·`DELETE /v1/me/focus` 응답·`focus_summary` 의 숫자)도 그때 정지 중인 행위자의 alert 를 `items`·`counts` 에서 뺀다. 판정 기준·경계는 알림함과 같다(5.18). 재전송되는 `friend.request_received` 는 요청자가 그때 정지 중이면 `request.from` 을 위 D1 규칙대로 가린다(요청자는 대개 `summaryWatchers` 밖이라 `user.updated` 가 가지 않는다 — `events.md` 3.3).
- 사용자 목록(남의 팔로워·팔로잉, 글의 좋아요·리포스트 목록)에는 정지 계정이 `suspended=true` 로 남는다. 사용자 검색(5.2)에서는 빠진다.
- 정지된 사용자에게는 DM·친구 요청·팔로우·초대 불가.

---

## 4. 메타

#### `GET /v1/meta`
- 인증 없음.
- 응답 `200`:
```json
{
  "api_version": 1,
  "server_time": "2026-11-05T02:03:04.567Z",
  "min_client_version": "0.1.0",
  "limits": {
    "message_max_chars": 4000, "post_max_chars": 500, "post_max_media": 4,
    "upload_max_bytes": 5242880, "group_dm_max_members": 10, "story_ttl_sec": 86400,
    "poll_max_options": 10, "reactions_per_message": 20, "watch_topics_max": 50
  }
}
```

#### `GET /v1/health`
- 인증 없음. `200 { "ok": true }`.

---

## 5. 엔드포인트

> 별도 표기가 없으면 **인증 필요**, 리미터는 `RL_API` 만. 성공 응답 코드는 각 항목에 명시. 이벤트 이름은 `events.md` 4장.

### 5.1 나(me)·설정·상태

#### `GET /v1/me`
- `200 { "user": Me }`

#### `PATCH /v1/me/profile`
- 요청: `{ "display_name"?: string, "bio"?: string, "avatar_upload_id"?: ID | null }`
  - `avatar_upload_id`: 내 `purpose:"avatar"`, `status:"pending"` 업로드여야 함. `null` 이면 아바타 제거(이전 업로드는 `removed` + R2 삭제).
- `200 { "user": Me }` · 이벤트: `user.updated`(청중 `presenceWatchers`: 친구 + DM/그룹 DM 상대 + 나의 다른 연결, `events.md` 4.5)

#### `GET /v1/me/settings`
- `200 { "settings": Settings }`

#### `PATCH /v1/me/settings`
- 요청: `{ "dm_policy"?: ..., "is_private"?: boolean, "notify"?: Partial<Settings["notify"]>, "focus_default_min"?: number }`
- `200 { "settings": Settings }` · 이벤트: `settings.updated`(나의 다른 연결)
- `is_private: true → false` 이면 3.3 의 대기 요청 일괄 승인.

#### `PUT /v1/me/status`
- 요청: `{ "emoji"?: string | null, "text": string, "expires_in_sec"?: number | null }` (만료 60..2592000, 생략/null = 무기한)
- `200 { "status": UserStatus }` · 이벤트: `user.updated`

#### `DELETE /v1/me/status`
- `204` · 이벤트: `user.updated`

#### `PUT /v1/me/presence`
- 요청: `{ "mode": "auto" | "away" | "dnd" }` — `auto` 는 접속 여부·유휴 상태로 자동 결정.
- `200 { "presence": PresenceState }` · 이벤트: `presence.updated`
- 실제 상태 계산은 UserHub DO 가 한다(`events.md` 6.3).

#### `GET /v1/presence?user_ids=<id>,<id>,...`
- 최대 100개. 공개 범위(3.6) 밖의 사용자는 결과에서 빠진다. 정지 중인 계정은 `status: "offline"`, `last_seen_at: null`(3.9).
- `200 { "items": [ { "user_id": ID, "status": PresenceStatus, "last_seen_at": Timestamp | null } ] }`
- 용도: 방 멤버 목록처럼 푸시 대상이 아닌 사용자의 상태를 화면에 보일 때(60초 주기로 다시 조회).

#### `POST /v1/me/focus`
- 요청: `{ "duration_sec"?: number }` — 60..43200, 생략 시 `settings.focus_default_min*60`. 이미 집중 중이면 종료 시각을 새 값으로 바꾼다(`started_at` 은 처음 시작한 시각 그대로).
- `Focus.started_at` 은 어느 응답에서나 실제 시작 시각이다 — `GET /me/focus`·`focus.started`·`Me.presence.focus`(UserHub 상태, 닿지 못하면 D1 `user_presence.focus_started_at`)가 모두 같은 값.
- `200 { "focus": Focus }` · 이벤트: `focus.started`, `presence.updated`
- 집중 중: 실질 상태 `dnd`, 모든 `alert` 가 `suppressed:true, suppressed_reason:"focus"` 로 나가고 요약에 쌓인다. 알림함·안 읽음 수는 평소대로 쌓인다.

#### `GET /v1/me/focus`
- `200 { "focus": Focus | null }`

#### `DELETE /v1/me/focus`
- 집중 조기 종료. `200 { "summary": FocusSummary }` · 이벤트: `focus.ended`(요약 포함), `presence.updated`. 집중 중이 아니면 `409 conflict`(`reason:"not_focusing"`).
- 자연 종료 시에도 서버가 `focus.ended` 와 `alert{kind:"focus_summary"}` 를 보낸다.

#### `GET /v1/me/unreads`
- 접근 가능한 채널 중 `unread_count>0` 또는 `mention_count>0` 인 것만.
- 기준점: 읽음 행이 없으면 내가 그 채널(공개 text 는 그 방)에 들어온 시각의 ID 하한(2장 `ChannelMyState` 주석). 가입 전 메시지는 세지 않는다.
- 계산·비용 규칙(서버): ① 먼저 `channels.last_message_id > 기준점` 인 채널만 고른다(메시지 행을 읽지 않는 채널 행 비교). ② 고른 채널마다 `idx_messages_channel_top`·`idx_message_mentions_user` 범위 스캔으로 세되 `LIMIT 1000` 하위 질의로 끊는다. 따라서 한 번의 호출이 읽는 메시지 행은 최대 (안 읽은 채널 수 × 1,000)이고, 다 읽은 채널은 비용이 거의 없다. 클라이언트는 이 API 를 시작·재동기화 때만 부르고 주기적으로 부르지 않는다(평소엔 이벤트로 증감).
- `200 { "items": UnreadState[] }`

### 5.2 사용자·프로필

#### `GET /v1/users/{user}`
- `200 { "user": Profile }` · 차단 관계(어느 방향이든)여도 프로필은 200 으로 주되 `can_dm=false`, `can_view_posts=false`.
- 정지 중인 계정(본인 외): 200 이지만 핸들·표시 이름·`suspended=true` 만 — `bio: ""`, `status`·`avatar_url`·`presence`·`last_seen_at` 은 `null`, 카운트 3개는 `0` [오너 결정 D1, 3.9].

#### `GET /v1/users?ids=<id>,<id>,...`
- 최대 100개, 없는 ID 는 빠짐. `200 { "items": UserSummary[] }`

#### `GET /v1/users/search?q=<text>&limit=<n>`
- `RL_SEARCH`. handle 접두 일치 + display_name 부분 일치, `limit` 기본 10 최대 20. 차단 관계 사용자 제외, 정지 사용자 제외.
- `200 { "items": UserSummary[] }`

#### `GET /v1/users/{user}/posts?filter=posts|replies|media&cursor=&limit=`
- 열람 규칙 3.4. `posts`(기본): 본인 최상위 글 + 인용 + 리포스트, `replies`: 답글만, `media`: 미디어 있는 글만.
- `200 { "items": TimelineItem[], "next_cursor": string | null }`
- 오류: `403 private_account`, `404`(없는 사용자, 또는 정지 중인 계정 — 본인 외, 3.4 규칙 2·3.9).

#### `GET /v1/users/{user}/followers?cursor=&limit=` / `GET /v1/users/{user}/following?cursor=&limit=`
- `active` 관계만, 최신순. 비공개 계정은 3.3. 정지 중인 계정의 목록은 본인 외 `404`(3.9).
- `200 { "items": UserSummary[], "next_cursor": string | null }`

### 5.3 친구

#### `GET /v1/friends?cursor=&limit=`
- 정렬: 접속 중 먼저, 그다음 display_name. 정지 중인 친구는 `Friend.presence` 처럼 offline 으로 친다(3.9). `limit` 최대 100.
- `200 { "items": Friend[], "next_cursor": string | null }`

#### `DELETE /v1/friends/{user}`
- 친구 끊기. `204` · 이벤트: `friend.removed`(양쪽). 상대와의 1:1 DM 은 조건 소멸 시 읽기 전용.
- 부수효과(같은 batch, D3): 두 사람 사이의 대기 중 방 직접 초대(양방향) `canceled` — 취소된 초대마다 `space.invite_removed`(`reason:"canceled"`, 양쪽)(3.5).

#### `GET /v1/friends/requests?direction=incoming|outgoing`
- 대기(`pending`) 만, 최신순, 최대 200. `200 { "items": FriendRequest[] }`

#### `POST /v1/friends/requests`
- `RL_SOCIAL`. 요청: `{ "user": "@junho" }`
- 결과:
  - 상대가 이미 나에게 대기 요청을 보냈으면 **자동 수락**: `200 { "request": FriendRequest(status:"accepted"), "friend": Friend }` — 수락과 같은 규칙(차단·상대 정지를 수락 batch 안에서도 다시 확인, 3.2)
    - 읽은 뒤 수락 batch 전에 상대 요청이 취소·거절·다른 경로로 수락되면(수락 batch 가 직접 읽음) `404` 가 아니라, 그 일이 먼저 일어났을 때와 같은 답을 처음부터 다시 정해 준다: 새 요청 `201`, 이미 친구면 `409 already_exists`(`already_friends`) 등.
    - 다시 정하기는 모두 3번까지다. 세 번째 판정에서는 상대의 대기 요청이 또 새로 보여도 자동 수락하지 않고 내 요청을 새로 연다(`201`, 서로 엇갈린 대기 요청 두 개). 둘 중 어느 쪽을 수락해도 엇갈린 요청은 함께 `accepted` 가 되고 친구가 된다.
  - 아니면 `201 { "request": FriendRequest, "friend": null }`
- 오류: `409 already_exists`(이미 대기/이미 친구 — `details.reason:"pending"|"already_friends"`), `403 action_not_allowed`(차단/정지), `409 conflict`(`reason:"self"`), `422 limit_exceeded`(`pending_friend_requests`, 보낸 대기 100).
- 이벤트·알림: 받는 사람에게 `friend.request_received` + 알림 `friend_request` + alert `friend_request`.

#### `POST /v1/friends/requests/{request_id}/accept`
- 받는 사람만. `200 { "friend": Friend }` · 이벤트: `friend.request_accepted`(보낸 사람), `friend.added`(양쪽) · 알림 `friend_accepted`(보낸 사람)
- 오류: `404`(대기 중이 아님), `403 action_not_allowed`(어느 방향이든 차단·보낸 사람 정지(3.9) — 수락 batch 안에서도 다시 확인, 3.2. 사전 확인 뒤 batch 전에 차단·정지가 끼어들어도 같고, 친구 관계는 만들어지지 않음)

#### `POST /v1/friends/requests/{request_id}/decline`
- 받는 사람만. `204` · 이벤트: `friend.request_removed`(양쪽, `reason:"declined"`). 보낸 사람에게 알림 없음.

#### `DELETE /v1/friends/requests/{request_id}`
- 보낸 사람이 취소. `204` · 이벤트: `friend.request_removed`(`reason:"canceled"`)

### 5.4 차단

#### `GET /v1/blocks`
- `200 { "items": [ { "user": UserSummary, "created_at": Timestamp } ] }` (전부, 최신순)

#### `PUT /v1/blocks/{user}`
- `RL_SOCIAL`. 3.2 효과 적용. `204` · 이벤트: `block.updated {user_id, blocked:true}`(나의 연결만), 상대에게는 아무 이벤트도 가지 않는다(관계 정리로 생긴 `friend.removed` 도 상대에게 보내지 않음).

#### `DELETE /v1/blocks/{user}`
- `204` · 이벤트: `block.updated {blocked:false}`

### 5.5 팔로우

#### `PUT /v1/users/{user}/follow`
- `RL_SOCIAL`. `200 { "state": "active" | "pending" }`
- 공개 계정 → `active`, 알림 `follow`. 비공개 → `pending`, 알림 `follow_request`. 이미 있으면 현재 상태 반환.
- 오류: `403 action_not_allowed`, `409 conflict`(`self`).

#### `DELETE /v1/users/{user}/follow`
- 언팔로우 또는 요청 취소. `204`

#### `DELETE /v1/users/{user}/follower`
- 나를 팔로우하는 사람 제거(대기 요청 거절 포함). `204`

#### `GET /v1/me/follow-requests?cursor=&limit=`
- 나에게 온 대기 요청. `200 { "items": [ { "user": UserSummary, "created_at": Timestamp } ], "next_cursor": string | null }`

#### `POST /v1/me/follow-requests/{user}/accept`
- `204` · 알림 `follow_accepted`(요청자) · 이벤트: 요청자에게 `notification.created`

#### `POST /v1/me/follow-requests/{user}/decline`
- `204` (요청자에게 알리지 않음)

### 5.6 DM·그룹 DM

DM 도 `Channel`(`kind:"dm"|"group_dm"`) 이다. 메시지·고정·읽음·설정은 5.9~5.12 의 채널 공통 엔드포인트를 쓴다.

#### `GET /v1/dms?cursor=&limit=`
- 내가 멤버인 dm/group_dm, `last_message_at` 내림차순(메시지 없으면 created_at). 차단한 상대와의 1:1 DM 제외.
- `200 { "items": Channel[], "next_cursor": string | null }`

#### `POST /v1/dms`
- `RL_SOCIAL`. 요청: `{ "users": ["@junho"] }` 또는 `{ "users": ["@junho","@hana", ...], "name"?: "점심팟" }` (나 제외 1..9명)
- 1명: 1:1 DM. 이미 있으면 `200 { "channel": Channel }`, 없으면 `201`. `can_dm` 실패 → `403 dm_not_allowed`.
- 2..9명: 항상 새 그룹 DM `201 { "channel": Channel }`. 3.1 규칙(`403 dm_not_allowed`: `can_dm` 실패는 `details.user_ids`, 참여자 사이 차단 쌍은 `details: {}` — 누구인지 드러내지 않음).
- 이벤트: `channel.created`(모든 참여자). 그룹 DM 생성 시 시스템 메시지 없음.

#### `POST /v1/channels/{channel_id}/members`
- `RL_SOCIAL`. group_dm: 멤버 누구나(3.1 규칙), 비공개 text 채널: 3.5 규칙. 공개 text·dm 채널 → `409 conflict`(`reason:"not_supported"`).
- 요청: `{ "users": ["@hana"] }` (1..9)
- `200 { "channel": Channel }` · 이벤트: `channel.member_added`(기존 멤버), `channel.created`(새 멤버) · 시스템 메시지 `group_member_added`(group_dm 만)
- 오류: `422 limit_exceeded`(`group_dm_members`), `403 dm_not_allowed`(group_dm, 3.1: `can_dm` 실패는 `details.user_ids`, 차단 쌍은 `details: {}`), `409 already_member`, `404`(대상이 그 방 멤버가 아님 — 비공개 채널). 비공개 채널은 사전 확인 뒤 INSERT 가 아무도 넣지 못하면 같은 batch 에서 읽은 사유로 같은 순서(404 먼저, 그다음 409)로 답한다.

#### `DELETE /v1/channels/{channel_id}/members/{user}`
- `{user}=@me` 이면 나가기(group_dm·비공개 text). 남을 빼기: group_dm 은 owner_id 만, 비공개 text 는 방 owner·admin.
- `204` · 이벤트: `channel.member_removed`(남은 멤버), `channel.deleted`(빠진 사람에게, `reason:"removed"`) · 시스템 메시지 `group_member_removed`(group_dm)
- 1:1 DM 에서는 `409 conflict`(`reason:"not_supported"`).

### 5.7 방(Space)

#### `GET /v1/spaces`
- 내가 멤버인 방 전부(가입순). `200 { "items": Space[] }`

#### `POST /v1/spaces`
- `RL_SOCIAL`. 요청: `{ "name": "노바 개발방", "description"?: string, "icon_emoji"?: string }`
- 기본 채널 `일반`(공개) 생성, 만든 사람 owner.
- `201 { "space": SpaceDetail }` · 이벤트: `space.joined`(나의 연결) · 오류: `422 limit_exceeded`(`owned_spaces`)

#### `GET /v1/spaces/{space_id}`
- 멤버만. `200 { "space": SpaceDetail }`

#### `PATCH /v1/spaces/{space_id}`
- owner·admin. 요청: `{ "name"?, "description"?, "icon_emoji"?: string | null }`
- `200 { "space": Space }` · 이벤트: `space.updated`(방 멤버)

#### `DELETE /v1/spaces/{space_id}`
- owner. 소프트 삭제(방·채널). `204` · 이벤트: `space.deleted`(방 멤버). 이후 그 방의 채널은 `channel_members` 행이 남아 있어도 읽기·청중에서 빠진다(3.5).
- 오류: `404`(방 없음·삭제됨·내가 멤버 아님), `403 forbidden`(owner 아님). UPDATE 가 owner 를 다시 확인하고, 사전 확인 뒤 소유권이 넘어가거나 방이 지워져 아무것도 바뀌지 않으면 같은 batch 에서 읽은 사유로 위 코드 중 하나로 답한다(소유권 이전이 먼저면 `403`).

#### `POST /v1/spaces/{space_id}/transfer`
- owner. 요청: `{ "user": "@hana" }` (방 멤버여야 함). 이전 owner 는 admin 이 된다.
- `200 { "space": Space }` · 이벤트: `space.member_updated` ×2, `space.updated`
- 오류: `404`(방 없음·삭제됨·내가 멤버 아님), `403 forbidden`(owner 아님), `409 conflict`(`reason:"self"`), `404`(대상이 방 멤버 아님), `422 limit_exceeded`(`owned_spaces`). UPDATE 가 owner·대상 멤버십을 다시 확인하고, 그 사이 바뀌어 아무것도 바뀌지 않으면 같은 batch 에서 읽은 사유로 위 코드 중 하나로 답한다.

#### `GET /v1/spaces/{space_id}/members?q=&role=&cursor=&limit=`
- 멤버만. 정렬: 역할(owner→admin→member) 후 display_name. `q` 는 handle/display_name 부분 일치.
- `200 { "items": SpaceMember[], "next_cursor": string | null }`
- 접속 상태는 포함하지 않는다 → `GET /v1/presence` 로 따로 조회.

#### `PATCH /v1/spaces/{space_id}/members/{user}`
- owner. 요청: `{ "role": "admin" | "member" }` (owner 로 바꾸려면 transfer)
- 부수효과(같은 batch): 대상이 owner·admin 이 아니게 되면 그 사람이 만든 초대 링크 폐기(3.5). 직접 초대는 그대로(멤버 누구나 보낼 수 있음).
- `200 { "member": SpaceMember }` · 이벤트: `space.member_updated`(방 멤버)
- 오류: `404`(방 없음·삭제됨·내가 멤버 아님), `403 forbidden`(owner 아님), `409 conflict`(`reason:"self"`), `404`(대상이 방 멤버 아님). 역할 UPDATE 자체가 내가 아직 살아 있는 방의 owner 인지 다시 확인하므로, 사전 확인 뒤 소유권이 넘어가거나(대상에게 넘어간 경우 포함) 방이 지워지면 아무것도 바뀌지 않고, 같은 batch 에서 읽은 사유로 위 코드 중 하나로 답한다.

#### `DELETE /v1/spaces/{space_id}/members/{user}?ban=<bool>&reason=<text>`
- `{user}=@me`: 나가기(owner 는 `409 conflict`, `reason:"owner_cannot_leave"`).
- 남: 강퇴(3.5 권한). `ban=true` 면 `space_bans` 추가.
- 부수효과(한 batch, 나가기·강퇴·밴 공통): `space_members` 삭제 + 그 방 **모든 비공개 채널의 `channel_members` 행 삭제**(3.5) + 그 사람이 그 방에 보낸 **대기 중 직접 초대 `canceled`** + 그 사람에게 걸린 그 방의 대기 중 직접 초대 `canceled`(정리가 빠져 남은 것, 3.5 9-b) + 그 사람이 만든 **초대 링크 폐기** + 카운터.
- `204` · 이벤트: `space.member_left`(방 멤버 + 나간 사람, `reason:"left"|"kicked"|"banned"`), 나간 사람에게 `space.removed`, 빠진 비공개 채널마다 남은 채널 멤버에게 `channel.member_removed`, 취소된 직접 초대마다 `space.invite_removed`(`reason:"canceled"`, 초대받은 사람 + 초대한 사람) · 시스템 메시지(기본 채널) `member_left`/`member_kicked`

#### `GET /v1/spaces/{space_id}/bans`
- owner·admin. `200 { "items": [ { "user": UserSummary, "banned_by": UserSummary, "reason": string, "created_at": Timestamp } ] }`

#### `DELETE /v1/spaces/{space_id}/bans/{user}`
- owner·admin. `204`

### 5.8 초대

#### `GET /v1/spaces/{space_id}/invites`
- owner·admin. 폐기되지 않은 링크 전부(만료·소진 포함, 만든 사람이 owner·admin 이 아니게 된 링크는 폐기로 보아 제외 — 3.5). `200 { "items": Invite[] }`

#### `POST /v1/spaces/{space_id}/invites`
- owner·admin, `RL_SOCIAL`. 요청: `{ "max_uses"?: number | null, "expires_in_sec"?: number | null }`
  - `max_uses` 1..1000, `null`/생략 = 무제한. `expires_in_sec` 300..2592000, **생략 시 604800(7일)**, `null` = 무기한.
- `201 { "invite": Invite }` — 클라이언트는 `code` 를 보여 주고, 공유 문자열은 `masil join <code>` 형식으로 안내한다.
- INSERT 가 owner·admin 권한을 다시 확인한다(3.5). 그 사이 방에서 빠졌으면 `404`, member 로 강등됐으면 `403 forbidden` 이고 링크는 생기지 않는다.

#### `DELETE /v1/invites/{code}`
- 그 방 owner·admin. `204`
- 만든 사람이 방에서 빠지거나 member 로 강등되면 링크는 자동 폐기된다(3.5) — 이후 조회·수락은 `410 invite_invalid`(`reason:"revoked"`).

#### `GET /v1/invites/{code}`
- 로그인한 누구나. 무효 코드 → `410 invite_invalid`, 없는 코드 → `404`. 만든 사람이 더 이상 그 방 owner·admin 이 아니면 `410`(`reason:"revoked"`, 3.5).
- `200 { "invite": InvitePreview }`

#### `POST /v1/invites/{code}/accept`
- 가입. `use_count+1`(같은 batch, 조건부 UPDATE 로 초과 방지). 사전 확인과 조건부 UPDATE 모두 만든 사람이 아직 owner·admin 인지 본다(아니면 `410`, `reason:"revoked"`).
- `200 { "space": SpaceDetail }` · 이벤트: `space.joined`(나), `space.member_joined`(기존 멤버), 나에게 걸려 있던 그 방의 대기 중 직접 초대가 있었으면 같은 batch 에서 `canceled` 되고 `space.invite_removed`(`reason:"canceled"`, 나 + 초대한 사람)(3.5) · 시스템 메시지(기본 채널) `member_joined`
- 오류: `410 invite_invalid`, `409 already_member`, `403 banned_from_space`, `422 limit_exceeded`(`space_members`).

#### `POST /v1/spaces/{space_id}/direct-invites`
- 방 멤버 누구나(owner·admin 도 같은 조건), `RL_SOCIAL`. 요청: `{ "user": "@hana" }` — **역할과 무관하게 초대하는 사람의 친구**여야 함(3.5 표), 상대가 이미 멤버/밴/차단 관계면 실패. 친구가 아닌 사람은 초대 링크로 들인다.
- `201 { "invite": DirectInvite }` · 이벤트: `space.invite_received`(상대) · 알림 `space_invite` · alert `invite`
- 오류: `403 action_not_allowed`(친구 아님·차단·상대 정지 — 확인 뒤 INSERT 전에 그렇게 돼도 같고, 아무것도 만들어지지 않음, 3.2), `409 already_member`(사전 확인 뒤 INSERT 전에 상대가 다른 경로로 방에 들어와도 같고, 아무것도 만들어지지 않음 — 3.5), `409 already_exists`(대기 중), `403 banned_from_space`, `404`(권한 확인 뒤 INSERT 전에 초대하는 사람이 방에서 빠짐 — 아무것도 만들어지지 않고 상대에게 이벤트·알림도 없음).
- INSERT 가 커밋된 뒤 이벤트·알림을 보내기 전에 상대가 다른 경로로 방에 들어오거나, 친구가 끊기거나, 차단되면 그쪽 batch 가 새 초대를 `canceled` 로 바꾸고 `space.invite_removed` 를 보낸다(3.5). 서버는 보내기 직전에 초대가 아직 대기 중이고 수락할 수 있는지(`GET /v1/me/direct-invites` 와 같은 조건) 다시 읽어, 아니면 `space.invite_received`·알림·alert 를 보내지 않고 그 경쟁이 INSERT 전에 일어난 것과 같은 오류(`409 already_member` / `403 action_not_allowed` / `404`)로 답한다. 그래도 남는 틈(다시 읽은 뒤 보내기 전)은 클라이언트가 막는다: 이미 들어가 있는 방의 초대, 이미 `space.invite_removed` 로 지운 초대 id 의 `space.invite_received` 는 무시한다.
- 반대로 새 초대의 id 를 알아야만 일어나는 일 — 상대가 `GET /v1/me/direct-invites` 로 보고 거절·수락하거나, 초대한 사람·방 owner·admin 이 id 로 취소함 — 은 어느 직렬 순서에서도 INSERT 뒤이므로 `201 { "invite" }`(만들 때의 `pending`)로 답한다. 다만 이미 대기 중이 아니므로 `space.invite_received`·알림·alert 는 보내지 않는다(그 일의 `space.invite_removed` 는 이미 갔다). 취소된 초대는, 다시 읽을 때 위의 취소 사유(상대가 방 안·친구 아님·차단·초대한 사람이 방 밖)가 남아 있으면 그 오류로, 없으면 id 로 취소된 것으로 본다.
- 같은 사람에게 온 대기 초대라도 수락할 수 없는 것(초대한 사람이 이미 방 멤버가 아님, 또는 두 사람이 더 이상 친구가 아니거나 차단 관계 — 3.5)은 `already_exists` 로 세지 않는다. 새 초대와 같은 batch 에서 그 묵은 초대를 `canceled` 로 바꾸고, 커밋 뒤 `space.invite_removed`(`reason:"canceled"`, 초대받은 사람 + 묵은 초대를 보낸 사람)를 보낸다.

#### `GET /v1/me/direct-invites`
- 나에게 온 대기 초대(초대한 사람이 아직 그 방 멤버이고 아직 나와 친구이며 어느 방향으로도 차단이 없고, 내가 아직 그 방 멤버가 아닌 것만 — 나머지는 수락할 수 없음, 3.5). `200 { "items": DirectInvite[] }`

#### `POST /v1/direct-invites/{invite_id}/accept`
- 초대받은 사람. `200 { "space": SpaceDetail }` · 이벤트는 초대 링크 수락과 같음 + `space.invite_removed`(`reason:"accepted"`).
- 초대한 사람이 더 이상 방 멤버가 아니거나, 두 사람이 더 이상 친구가 아니거나, 어느 방향으로든 차단이 있으면 `404`(3.5 — 방에서 빠질 때·친구를 끊을 때·차단할 때 이미 `canceled` 로 바뀌지만, 수락 쪽도 사전 확인과 조건부 UPDATE 에서 다시 본다).

#### `POST /v1/direct-invites/{invite_id}/decline`
- `204` · 이벤트: `space.invite_removed`(`reason:"declined"`)

#### `DELETE /v1/direct-invites/{invite_id}`
- 초대한 사람 또는 방 owner·admin 이 취소. `204` · 이벤트: `space.invite_removed`(`reason:"canceled"`)

### 5.9 채널

#### `GET /v1/spaces/{space_id}/channels`
- 멤버만. 내가 볼 수 있는 text 채널, `position` 오름차순. `200 { "items": Channel[] }`

#### `POST /v1/spaces/{space_id}/channels`
- owner·admin. 요청: `{ "name": "배포알림", "topic"?: string, "is_private"?: boolean, "members"?: string[] }`
  - `members`: 비공개 채널 초기 멤버(방 멤버만, 만든 사람은 자동 포함).
- `201 { "channel": Channel }` · 이벤트: 공개 → `channel.created`(방 멤버), 비공개 → `channel.created`(채널 멤버) · 시스템 메시지 `channel_created`(새 채널 안)
- 오류: `409 already_exists`(같은 이름), `422 limit_exceeded`(`channels_per_space`).

#### `GET /v1/channels/{channel_id}`
- 읽기 권한. `200 { "channel": Channel }`

#### `PATCH /v1/channels/{channel_id}`
- text: owner·admin, 요청 `{ "name"?, "topic"?, "position"? }` (공개/비공개 전환 불가). group_dm: 멤버 누구나, 요청 `{ "name"?, "topic"? }`. dm → `409 conflict`.
- `200 { "channel": Channel }` · 이벤트: `channel.updated`(채널 청중) · 시스템 메시지 `channel_renamed`/`channel_topic_changed`/`group_renamed`

#### `DELETE /v1/channels/{channel_id}`
- text 만, owner·admin, 기본 채널 불가. 소프트 삭제. `204` · 이벤트: `channel.deleted`(`reason:"deleted"`)

#### `GET /v1/channels/{channel_id}/members?cursor=&limit=`
- 읽기 권한. dm/group_dm/비공개: `channel_members`. 공개 text: 방 멤버와 동일.
- `200 { "items": [ { "user": UserSummary, "joined_at": Timestamp } ], "next_cursor": string | null }`

#### `PUT /v1/channels/{channel_id}/prefs`
- 요청: `{ "notify"?: "all" | "mentions" | "none", "muted"?: boolean }`
- `200 { "my": ChannelMyState }` · 이벤트: `channel.prefs_updated`(나의 연결)

#### `PUT /v1/channels/{channel_id}/read`
- 요청: `{ "message_id": ID }` — 현재 값보다 뒤일 때만 전진(뒤로 가지 않음). 그 채널의 최상위 메시지 ID여야 함. 읽음 기준점은 최상위 메시지만 다루며, `unread_count`·`mention_count` 도 최상위 메시지만 세므로 이 기준점으로 둘 다 0 이 될 수 있다(스레드 답글 속 멘션은 배지에 들어가지 않고 알림함에서 읽음 처리한다).
- `200 { "state": UnreadState }` · 이벤트: `read_state.updated`(나의 연결)

### 5.10 메시지

#### `GET /v1/channels/{channel_id}/messages?before=<id>|after=<id>|around=<id>&limit=<n>`
- 최상위 메시지만(스레드 답글 제외). `limit` 기본 50, 최대 100. `before`/`after`/`around` 는 최대 하나.
  - 없음: 최신 `limit` 개.
  - `before`: 그 ID 보다 오래된 것(무한 스크롤 위로).
  - `after`: 그 ID 보다 새 것(아래로 스크롤해 이어 받기). 클라이언트는 가진 마지막 ID 가 아니라 **5초 겹친 기준 ID** 를 쓰고 ID 로 중복 제거한다(`events.md` 5.3 "겹쳐 받기"). 재동기화 때는 `after=` 로 이어 붙이지 않고 창을 통째로 다시 받는다(`events.md` 5.3).
  - `around`: 그 ID 를 가운데에 두고 앞뒤 `limit/2` (검색·알림에서 점프).
- **항상 ID 오름차순**으로 반환.
- 삭제된 메시지는 스레드 답글이 있을 때만 `deleted:true` 묘비로 포함, 아니면 생략.
- `200`:
```json
{
  "items": [ { "id": "01JC...", "channel_id": "01JB...", "author": { "id": "01JA...", "handle": "minji", "display_name": "민지", "avatar_url": null, "status": null, "is_private": false, "suspended": false },
               "kind": "text", "body": "오늘 배포 몇 시에 해요?", "mentions": [], "mention_here": false,
               "thread_root_id": null, "thread": { "reply_count": 3, "last_reply_at": "2026-11-05T02:10:00.000Z", "participant_ids": ["01JA...","01JD..."] },
               "reactions": [ { "emoji": "👍", "count": 2, "me": true } ], "pinned": false, "poll": null, "roll": null, "system": null,
               "client_nonce": "01JC9...", "created_at": "2026-11-05T02:02:00.000Z", "edited_at": null, "deleted": false } ],
  "has_more_before": true,
  "has_more_after": false
}
```

#### `POST /v1/channels/{channel_id}/messages`
- 쓰기 권한, `RL_MESSAGE`.
- 요청:
```json
{ "client_nonce": "01JC9X...", "kind": "text", "body": "@minji 점심 먹고 ㄱㄱ" }
```
  - `kind?`: `"text"`(기본) | `"me"`(`/me` 동작 메시지) | `"roll"`(`body` = 주사위 식) | `"poll"`(`body` 무시, `poll: PollInput` 필수).
  - `/shrug` 는 클라이언트가 본문 끝에 `¯\_(ツ)_/¯` 를 붙여 `text` 로 보낸다.
- `201 { "message": Message }` (중복 nonce → `200` 기존 메시지)
- 부수효과(한 batch): 메시지 삽입, `channels.last_message_*`(**조건부**: `WHERE last_message_id IS NULL OR last_message_id < ?새ID` — 동시 전송 때 뒤로 가지 않게), `message_mentions`(2.1 해석 규칙을 통과한 명시적 멘션 + `@here` 확장 대상), 내 `channel_reads` 를 이 메시지로 전진(역시 더 클 때만), 투표 생성.
- 이벤트: `message.created`(채널 청중), 멘션 대상(2.1 규칙 통과자만)에게 알림 `message_mention`(1:1 DM 제외) + alert `mention`, DM/그룹 DM 상대에게 alert `dm`, `notify:"all"` 인 채널 멤버에게 alert `channel_message`. 차단 관계·`muted`·`notify:"none"` 은 alert 생략.
- 오류: `403 read_only`, `403 dm_not_allowed`, `404`, `400`(`body` 빈 값, 주사위 식 오류 `fields.body="invalid"`).

#### `GET /v1/messages/{message_id}`
- 읽기 권한. 스레드 답글도 가능. `200 { "message": Message }`

#### `PATCH /v1/messages/{message_id}`
- 작성자, `kind` text/me, `RL_MESSAGE`. 요청: `{ "body": string }` — 멘션 재해석(2.1 규칙 그대로, 새로 추가된 멘션만 알림. `@here` 는 다시 확장하지 않는다).
- `200 { "message": Message }` · 이벤트: `message.updated`

#### `DELETE /v1/messages/{message_id}`
- 3.7 규칙. 소프트 삭제 + 본문 비움, 리액션·고정·멘션 행 삭제.
- `204` · 이벤트: `message.deleted` (루트 삭제 시 스레드 답글은 남음; 답글 삭제 시 루트 `thread.updated`)

### 5.11 스레드

스레드는 한 단계다. 답글에 답글을 달면 같은 루트의 스레드에 들어간다(`thread_root_id` 는 항상 루트).

#### `GET /v1/messages/{message_id}/thread?before=|after=&limit=`
- `{message_id}` 는 루트(답글 ID 를 주면 그 루트로 해석). `limit` 기본 50, 최대 100, ID 오름차순.
- `200 { "root": Message, "items": Message[], "has_more_before": boolean, "has_more_after": boolean }`

#### `POST /v1/messages/{message_id}/thread`
- 쓰기 권한, `RL_MESSAGE`. 요청: `{ "client_nonce": string, "kind"?: "text"|"me"|"roll", "body": string }` (투표 불가)
- `201 { "message": Message }` (중복 nonce `200`)
- 부수효과: 루트 `reply_count+1`, `last_reply_at`, `thread_participants` upsert(첫 답글이면 루트 작성자도 추가).
- 이벤트: `message.created`(채널 청중, `thread_root_id` 설정됨) + `thread.updated`(채널 청중). 알림 `thread_reply` + alert `thread_reply` → 루트 작성자와 이전 참여자(작성자 본인 제외). 멘션 처리 동일.

### 5.12 리액션·고정

#### `PUT /v1/messages/{message_id}/reactions/{emoji}`
- `{emoji}` 는 URL 인코딩된 이모지 한 글자(그래핌 1개, UTF-8 32바이트 이하). 읽기 권한(+1:1 DM 은 `can_dm`), `RL_REACT`.
- `204` · 이벤트: `reaction.added` · 오류: `422 limit_exceeded`(`reactions_per_message`, 서로 다른 이모지 20)

#### `DELETE /v1/messages/{message_id}/reactions/{emoji}`
- `204` · 이벤트: `reaction.removed`

#### `GET /v1/messages/{message_id}/reactions/{emoji}?cursor=&limit=`
- `200 { "items": UserSummary[], "next_cursor": string | null }`

#### `PUT /v1/messages/{message_id}/pin` / `DELETE /v1/messages/{message_id}/pin`
- 3.7 고정 권한, 최상위 메시지만, `RL_REACT`. 채널당 50개.
- `204` · 이벤트: `message.pinned` / `message.unpinned` · 고정 시 시스템 메시지 `message_pinned`
- 오류: `422 limit_exceeded`(`pins_per_channel`)

#### `GET /v1/channels/{channel_id}/pins`
- `200 { "items": Message[] }` (고정 시각 최신순)

### 5.13 검색

#### `GET /v1/search/messages?q=&channel_id=&space_id=&author=&cursor=&limit=`
- `RL_SEARCH`. `limit` 기본 20, 최대 50, 최신순. `author` 는 `{user}` 형식.
- 범위: `channel_id`(채널 내 검색) > `space_id` > 전체(내가 접근 가능한 모든 채널). 전체 범위의 채널이 500개를 넘으면 `422 limit_exceeded`(`search_scope`) → 범위를 좁히라는 뜻.
- 방법은 `data-model.md` 4장.
- `200 { "items": [ { "message": Message, "channel": ChannelRef, "highlights": SearchHighlight[] } ], "next_cursor": string | null }`

#### `GET /v1/search/posts?q=&author=&cursor=&limit=`
- `RL_SEARCH`. 열람 규칙 3.4 적용. `200 { "items": [ { "post": Post, "highlights": SearchHighlight[] } ], "next_cursor": string | null }`

#### `GET /v1/hashtags/search?q=&limit=`
- `RL_SEARCH`. 태그 접두 일치(최근 7일 사용분), `limit` 최대 20.
- `200 { "items": [ { "tag": string, "post_count_24h": number } ] }`

### 5.14 업로드·미디어

#### `POST /v1/uploads?purpose=avatar|post|story`
- `RL_UPLOAD`. **본문은 이미지 원본 바이트**(JSON 아님).
- 헤더: `Content-Type: image/png|image/jpeg|image/gif|image/webp`, `Content-Length` 필수.
- 서버는 앞부분 바이트(매직 넘버)로 실제 형식을 판별하고 `Content-Type` 과 다르면 `415`. 크기 1..5,242,880 바이트(초과 `413`). 가로·세로는 헤더에서 읽는다(실패 시 `null`).
- 크기는 `Content-Length` 값이 아니라 **실제로 받은 바이트**로 판정한다. `Content-Length` 가 5,242,880 을 넘으면 본문을 읽기 전에 `413`, 값이 작게 위조돼도 받는 중 상한을 넘는 순간 읽기를 멈추고 `413`. (JSON 본문 64KB 상한도 같은 방식 — chunked·위조 `Content-Length` 무관. GET/HEAD 요청에 붙은 본문·`Content-Length`는 읽지 않고 무시한다 — `Content-Length: 0` 이 붙은 GET 도 평소와 같이 응답.)
- 메타데이터 제거(EXIF 등)는 **클라이언트 책임**: 클라이언트는 JPEG·PNG 를 다시 인코딩하고, WebP·GIF 는 픽셀·애니메이션 블록만 남기고 다시 써서 보낸다(아래 허용 목록. WebP 는 VP8X 의 ICC·EXIF·XMP 플래그도 끔, GIF 는 일반 텍스트 확장(0x01)과 그 그래픽 제어 확장도 지움). 구조를 정확히 읽을 수 없는 WebP·GIF 는 그대로 보내지 않고 업로드를 거부한다. 서버는 바이트를 그대로 저장한다.
- 단, 서버는 WebP·GIF 를 검사한다: WebP 는 `VP8 `·`VP8L`·`VP8X`·`ALPH`·`ANIM`·`ANMF` 이외의 청크(`EXIF`·`XMP `·`ICCP`·알 수 없는 청크)나, `ANMF` 프레임(16바이트 프레임 머리 뒤의 하위 청크) 안의 `ALPH`·`VP8 `·`VP8L` 이외의 하위 청크, GIF 는 주석 확장(0xFE)·일반 텍스트 확장(0x01)·`NETSCAPE2.0`/`ANIMEXTS1.0` 이외의 애플리케이션 확장·알 수 없는 확장, 4바이트 하위 블록 하나가 아닌 그래픽 제어 확장(0xF9), 루프 하위 블록(id 1, 3바이트) 하나가 아닌 `NETSCAPE2.0`/`ANIMEXTS1.0` 확장이 있으면 `400 invalid_request`, `details.fields.body="not_allowed"`. 청크 크기·RIFF 크기·블록 구조가 깨졌거나(`ANMF` 가 16바이트보다 짧거나 하위 청크가 프레임 크기와 맞지 않는 경우 포함) GIF 끝(0x3B)이 없으면 `400`, `body="invalid"`. 클라이언트는 이런 블록을 지운 뒤 보낸다(클라이언트가 만드는 결과는 언제나 이 검사를 통과한다).
- 업로드는 `pending` 으로 시작하고, 글/스토리/아바타에 붙으면 `attached`. 24시간 안에 안 붙으면 cron 이 지운다. 사용자당 `pending` 20개 한도(`422 limit_exceeded`, `pending_uploads`).
- `201 { "upload": Upload }`

#### `DELETE /v1/uploads/{upload_id}`
- 소유자, `pending` 만. `204`

#### `GET /v1/media/{upload_id}`
- 인증 필요. 열람 권한: 아바타는 누구나(단 정지 중인 계정의 아바타는 본인·운영자만, 3.9), 글 이미지는 그 글의 열람 규칙(3.4), 스토리 이미지는 스토리 열람 규칙 + 미만료, `pending` 은 소유자만. 권한 없음/삭제됨 → `404`.
- `200` 이미지 바이트. 헤더: `Content-Type`, `Content-Length`, `ETag`, `Cache-Control: private, max-age=31536000, immutable`. `If-None-Match` → `304`.

### 5.15 글(피드)

#### `POST /v1/posts`
- `RL_POST`. 요청:
```json
{
  "client_nonce": "01JC9Y...",
  "body": "오늘 첫 눈 ❄️ #첫눈",
  "media_upload_ids": ["01JD..."],
  "reply_to_id": null,
  "quote_of_id": null,
  "poll": null
}
```
  - `media_upload_ids?` 0..4, 내 `purpose:"post"`·`pending` 업로드. `reply_to_id?` 와 `quote_of_id?` 는 동시에 쓸 수 없다. `poll?` 은 답글에 불가.
  - 대상 글(`reply_to_id`/`quote_of_id`)은 열람 가능해야 하고 차단 관계가 없어야 함. 인용 대상이 비공개 계정 글이면 `403 action_not_allowed`.
- `201 { "post": Post }` (중복 nonce `200`)
- 부수효과: `post_media`, `post_hashtags`, `post_mentions`, 부모 `reply_count`/인용 대상 `quote_count`, 작성자 `post_count`.
- 이벤트: 최상위 글·인용 → `timeline.item_created`(접속 중인 `active` 팔로워 + 나), 답글 → `post.reply_created`(토픽 `post:<parent_id>`), 카운트 변경 → `post.stats`(토픽). 앞의 두 이벤트는 받는 사람마다 3.4 열람 규칙에 맞는지 거르는 가시성 필터를 거친다(`events.md` 4.8 — 답글 작성자가 비공개 계정이거나 시청자와 차단 관계면 그 시청자에게 안 감). 알림: `post_reply`(부모 작성자), `post_quote`(원글 작성자), `post_mention`(멘션 대상, 열람 가능한 사람만) — 각 대상의 `notify.social`/`notify.mention` 에 따라 alert.
- 오류: `400`, `403 action_not_allowed`, `404`(대상 글), `422 limit_exceeded`(`post_media`).

#### `GET /v1/posts/{post_id}`
- 3.4. `200 { "post": Post }`

#### `GET /v1/posts/{post_id}/context`
- 조상 체인(루트부터, 최대 20). 볼 수 없는 조상은 `deleted:true` 자리표시로.
- `200 { "ancestors": Post[], "post": Post }`

#### `GET /v1/posts/{post_id}/replies?cursor=&limit=`
- 직접 답글만, **ID 오름차순**(오래된 것부터). 각 답글의 `reply_count` 로 하위 트리를 펼친다.
- 차단 관계 작성자의 답글과 그 하위는 빠진다. 삭제된 답글은 하위 답글이 있으면 자리표시.
- `200 { "items": Post[], "next_cursor": string | null }`

#### `DELETE /v1/posts/{post_id}`
- 작성자. 소프트 삭제 + 미디어 R2 삭제 + 리포스트 행 삭제. `204` · 이벤트: `post.deleted`(토픽 + 접속 중 팔로워)

#### `PUT /v1/posts/{post_id}/like` / `DELETE /v1/posts/{post_id}/like`
- `RL_REACT`. `200 { "post_id": ID, "liked": boolean, "like_count": number }`
- 이벤트: `post.stats`(토픽) · 알림 `post_like`(작성자, `dedupe_key=post_like:<post>:<actor>`; 취소해도 알림은 지우지 않음)

#### `GET /v1/posts/{post_id}/likes?cursor=&limit=`
- `200 { "items": UserSummary[], "next_cursor": string | null }`

#### `PUT /v1/posts/{post_id}/repost` / `DELETE /v1/posts/{post_id}/repost`
- `RL_REACT`. 내 글도 리포스트할 수 있다. 비공개 계정의 글은 누구도 리포스트할 수 없다(`403 action_not_allowed`).
- `200 { "post_id": ID, "reposted": boolean, "repost_count": number }`
- 이벤트: `post.stats`(토픽), 리포스트 시 `timeline.item_created`(내 접속 중 팔로워, `type:"repost"`, 원글·인용된 글 작성자 기준 가시성 필터 적용 — `events.md` 4.8) · 알림 `post_repost`

#### `GET /v1/posts/{post_id}/reposts?cursor=&limit=`
- `200 { "items": UserSummary[], "next_cursor": string | null }`

#### `GET /v1/posts/{post_id}/quotes?cursor=&limit=`
- `200 { "items": Post[], "next_cursor": string | null }`

### 5.16 타임라인·해시태그·트렌드

공통 쿼리: `cursor`, `limit`(기본 30, 최대 100), `since=<TimelineItem.id>`.
- `since` 를 주면 그 항목보다 **새로운** 항목을 최신순으로 최대 `limit` 개 돌려주고 `next_cursor=null`. 새 항목이 `limit` 보다 많으면 `gap=true`(클라이언트는 목록을 처음부터 다시 받는다). 클라이언트는 맨 위 항목이 아니라 5초 겹친 기준 항목의 ID 를 `since` 로 주고 ID 로 중복 제거한다(`events.md` 5.3 "겹쳐 받기").
- 응답: `{ "items": TimelineItem[], "next_cursor": string | null, "gap": boolean }` (`since` 없으면 `gap=false`).

#### `GET /v1/timeline/following`
- 나 + 내가 `active` 팔로우하는 사람의 최상위 글·인용 + 그들의 리포스트. 답글 제외. 같은 글이 여러 번 리포스트되면 가장 최근 1개만.
- 실시간: `timeline.item_created` 이벤트로 위에 끼워 넣는다.

#### `GET /v1/timeline/discover`
- 공개·비정지 계정의 최상위 글·인용 전체, 최신순(리포스트 제외). 차단 관계 제외.
- 실시간 푸시 없음: 화면이 보이는 동안 30초마다 `since` 로 새 글 수를 확인해 "새 글 N개" 띠를 보인다.

#### `GET /v1/hashtags/{tag}/posts?cursor=&limit=`
- `{tag}` 는 `#` 없이 URL 인코딩. 서버가 정규화. 열람 규칙 3.4(비공개 계정 글 제외).
- `200 { "items": Post[], "next_cursor": string | null }`

#### `GET /v1/trends`
- `200 { "items": [ { "rank": 1, "tag": "첫눈", "post_count": 42, "author_count": 30 } ], "computed_at": Timestamp | null }`
- 5분마다 cron 이 계산(`data-model.md` 6장). 클라이언트는 5분마다 다시 조회.

### 5.17 스토리

#### `POST /v1/stories`
- `RL_POST`. 요청: `{ "body"?: string, "media_upload_id"?: ID, "bg"?: "accent"|"accent2"|"accent3"|"accent4"|"plain" }` — 본문·미디어 중 하나 이상. 미디어는 `purpose:"story"`.
- 만료 = 생성 + 24시간. 사용자당 활성 스토리 30개(`422 limit_exceeded`, `active_stories`).
- `201 { "story": Story }` · 이벤트: `story.created`(접속 중 `active` 팔로워 + 나)

#### `GET /v1/stories/feed`
- 나 + `active` 팔로우 중이고 열람 가능한 사람의 미만료 스토리. 그룹 정렬: 나(있으면) 먼저, 그다음 `has_unseen` 인 그룹, 각 묶음 안에서 `latest_at` 내림차순. 최대 100그룹, 그룹 안 스토리는 오래된 것부터.
- `200 { "items": StoryGroup[] }`

#### `GET /v1/users/{user}/stories`
- 3.4. `200 { "items": Story[] }`

#### `POST /v1/stories/{story_id}/view`
- 본 것으로 표시(멱등). `204`

#### `GET /v1/stories/{story_id}/viewers?cursor=&limit=`
- 작성자만. `200 { "items": [ { "user": UserSummary, "viewed_at": Timestamp } ], "next_cursor": string | null }`

#### `DELETE /v1/stories/{story_id}`
- 작성자. `204` · 이벤트: `story.deleted`(접속 중 팔로워 + 나)

### 5.18 알림함

알림 생성 기준(받는 사람 기준; 행위자가 나 자신이거나 차단 관계면 만들지 않음):

| type | 언제 | subject | context |
|---|---|---|---|
| `follow` | 누가 나를 팔로우(공개 계정) | user(행위자) | — |
| `follow_request` | 비공개 계정인 나에게 팔로우 요청 | user | — |
| `follow_accepted` | 내 팔로우 요청이 승인됨 | user | — |
| `friend_request` | 친구 요청 받음 | friend_request | — |
| `friend_accepted` | 내 친구 요청이 수락됨 | user | — |
| `space_invite` | 직접 초대 받음 | space_direct_invite | space_id, space_name |
| `post_like` | 내 글 좋아요 | post | post_id |
| `post_repost` | 내 글 리포스트 | post | post_id |
| `post_reply` | 내 글에 직접 답글 | post(답글) | post_id(답글) |
| `post_quote` | 내 글 인용 | post(인용 글) | post_id |
| `post_mention` | 글에서 나를 멘션 | post | post_id |
| `message_mention` | 채널·그룹 DM 메시지(스레드 답글 포함)에서 나를 멘션(`@here` 확장 포함, 1:1 DM 제외). 2.1 해석 규칙을 통과한 경우만 — 내가 그 채널을 읽을 수 있고 작성자와 차단 관계가 아님. `preview` 는 읽을 수 있는 사람에게만 만들어진다 | message | channel_id, message_id, thread_root_id, space_id |
| `thread_reply` | 내가 루트 작성자/참여자인 스레드에 새 답글 | message(답글) | channel_id, message_id, thread_root_id, space_id |
| `guestbook_entry` | 내 방명록에 새 글 | guestbook_entry | — |

중복 방지: `post_like`/`post_repost`/`follow` 는 `dedupe_key` 로 같은 (행위자, 대상) 당 1개.

목록과 `unread_count`(목록·`/unread-count`·`notification.created`·`notifications.read` 모두)에서 빠지는 행: 내가 차단한 행위자, 지금 정지 중인 행위자(3.9 — 정지가 끝나면 다시 보인다).
같은 규칙이 실시간 재전송에도 걸린다 [대기 목록 4-a]: `resume` 재전송(`events.md` 3.3)은 그 시점에 위 조건에 걸리는 행위자의 `notification.created`·`alert` 를 보내지 않고, `focus.ended` 요약의 `items` 에서도 뺀다. 집중 모드 종료 요약도 종료 시점에 이 조건에 걸리는 행위자의 alert 를 `items`·`counts` 에서 뺀다(`events.md` 4.6). 재전송된 `notification.created` 의 `unread_count` 는 기록 당시 값이다(최신 값은 다음 이벤트 또는 `/unread-count`).

#### `GET /v1/notifications?cursor=&limit=&unread_only=<bool>`
- `200 { "items": Notification[], "next_cursor": string | null, "unread_count": number }`

#### `GET /v1/notifications/unread-count`
- `200 { "unread_count": number }`

#### `POST /v1/notifications/read`
- 요청은 셋 중 하나: `{ "ids": ID[] }`(1..100) | `{ "all": true }` | `{ "before": ID }`(그 ID 이하 전부)
- `200 { "unread_count": number }` · 이벤트: `notifications.read`(나의 연결)

### 5.19 투표

투표는 메시지(`POST /v1/channels/{id}/messages`, `kind:"poll"`) 또는 글(`POST /v1/posts`, `poll`) 생성 시 같이 만든다. 열람 권한 = 그 메시지/글의 읽기 권한(없으면 `404`).
- 투표·투표 취소·마감은 대화에 `poll.updated` 를 보내는 **쓰기**다.
  - 메시지 투표: 리액션(5.12)과 같은 규칙 — 읽기 권한 + 1:1 DM 은 `can_dm`. 읽기 전용 1:1 DM(차단·조건 소멸, 3.1)에서는 양쪽 모두 `403 read_only`(`details.reason` 은 3.1 과 같음). 열람(`GET`)은 그대로 된다. 채널·그룹 DM·방에서 빠진 사람(만든 사람 포함)은 `404`.
  - 글 투표: 글 상호작용(좋아요·답글) 규칙 — 글을 볼 수 있어야 한다. 차단 관계(어느 방향이든)면 글이 숨겨지므로(3.2 양방향 숨김) 좋아요와 같이 `404`.

#### `GET /v1/polls/{poll_id}`
- `200 { "poll": Poll }`

#### `PUT /v1/polls/{poll_id}/vote`
- `RL_REACT`. 요청: `{ "option_idxs": number[] }` — 단일 선택이면 정확히 1개, 복수면 1..선택지 수. 기존 투표를 **대체**한다.
- `200 { "poll": Poll }` · 이벤트: `poll.updated`(메시지 투표: 채널 청중 / 글 투표: 토픽 `post:<id>`)
- 오류: `409 poll_closed`, `400`, `403 read_only`, `404`

#### `DELETE /v1/polls/{poll_id}/vote`
- 내 투표 취소. 권한은 투표와 같다(`403 read_only`, `404`). `200 { "poll": Poll }` · 이벤트 동일

#### `POST /v1/polls/{poll_id}/close`
- 만든 사람(아니면 `403 forbidden`), 위 쓰기 규칙도 적용(읽기 전용 1:1 DM `403 read_only`, 대화에서 빠졌으면 `404`). `200 { "poll": Poll }` · 이벤트 동일

### 5.20 방명록

#### `GET /v1/users/{user}/guestbook?cursor=&limit=`
- 열람: 프로필 주인의 글 열람 규칙(3.4)과 같음. 최신순. 비밀글은 주인·작성자 외에는 `hidden:true`.
- `200 { "items": GuestbookEntry[], "next_cursor": string | null }`

#### `POST /v1/users/{user}/guestbook`
- `RL_POST`. 요청: `{ "body": string, "is_secret"?: boolean }`. 자기 방명록에도 쓸 수 있다. 차단 관계 `403 action_not_allowed`, 비공개 계정은 `active` 팔로워·친구만(`403 private_account`).
- `201 { "entry": GuestbookEntry }` · 알림 `guestbook_entry`(주인, 본인 글 제외)

#### `DELETE /v1/guestbook/{entry_id}`
- 작성자 또는 방명록 주인. `204`

### 5.21 신고

#### `POST /v1/reports`
- `RL_SOCIAL`. 요청: `{ "target_type": "user"|"message"|"post"|"story"|"guestbook_entry"|"space", "target_id": ID, "reason": "spam"|"harassment"|"illegal"|"sexual"|"violence"|"other", "note"?: string }`
- 신고자는 대상을 볼 수 있어야 함(`404`). 서버가 신고 시점의 내용을 `snapshot` 에 복사.
- `201 { "report_id": ID }` · 오류: `409 already_exists`(같은 대상에 내 열린 신고 존재)
- 신고는 신고자에게 결과를 알리지 않는다.

### 5.22 운영자 (`/v1/admin/*`, `role='admin'` 만, 아니면 `403 not_admin`)

#### `GET /v1/admin/reports?status=open|resolved|dismissed&target_type=&cursor=&limit=`
- 기본 `status=open`, 오래된 것부터(ID 오름차순).
- `200 { "items": Report[], "next_cursor": string | null }`

#### `GET /v1/admin/reports/{report_id}`
- `200 { "report": Report }`

#### `POST /v1/admin/reports/{report_id}/resolve`
- 요청: `{ "resolution": "content_removed" | "user_suspended" | "no_action", "note"?: string }` — `no_action` 이면 `status='dismissed'`, 그 외 `resolved`. 같은 대상의 다른 열린 신고도 함께 처리.
- `200 { "report": Report }` · `admin_actions` 기록

#### `POST /v1/admin/content/remove`
- 요청: `{ "target_type": "message"|"post"|"story"|"guestbook_entry"|"upload"|"space", "target_id": ID, "reason": string, "report_id"?: ID }`
- 소프트 삭제 + 첨부 이미지 R2 즉시 삭제(`upload` 는 그 이미지 하나만 지우고, 붙어 있던 글에서는 빠짐). `report_id` 를 주면 그 신고(와 같은 대상의 열린 신고)를 `resolved/content_removed` 로 처리.
- `200 { "action": AdminAction }` · 이벤트: 일반 삭제와 같은 이벤트(`message.deleted`, `post.deleted`, `space.deleted` 등)

#### `POST /v1/admin/users/{user}/suspend`
- 요청: `{ "reason": string, "duration_sec"?: number | null, "report_id"?: ID }` — `null`/생략 = 무기한. 운영자를 정지할 수 없음(`409 conflict`).
- 세션 전부 폐기, 소켓 `4003` 종료, `report_id` 처리(`user_suspended`).
- `200 { "user": AdminUserView }`

#### `POST /v1/admin/users/{user}/unsuspend`
- `200 { "user": AdminUserView }`

#### `GET /v1/admin/users?q=&suspended=<bool>&cursor=&limit=`
- `200 { "items": AdminUserView[], "next_cursor": string | null }`

#### `GET /v1/admin/actions?target_type=&target_id=&cursor=&limit=`
- `200 { "items": AdminAction[], "next_cursor": string | null }`

### 5.23 WebSocket

#### `GET /v1/ws`
- `Upgrade: websocket`, `Authorization: Bearer <token>`, `RL_WS`.
- 쿼리: `mode=full|notify`(기본 full; 데몬은 notify), `device_id=<클라이언트가 만든 ULID>`(필수, 같은 기기의 TUI·데몬이 공유).
- 성공 `101`. 실패는 업그레이드 전 일반 HTTP 오류 봉투(`401`, `403 account_suspended`, `400`, `426`, `429`).
- 프로토콜 전체는 `events.md`.

---

## 6. 엔드포인트 요약표

| 메서드 | 경로 | 인증 | 추가 리미터 |
|---|---|---|---|
| GET | /v1/meta, /v1/health | 없음 | — |
| POST | /v1/auth/signup | 없음 | RL_SIGNUP |
| POST | /v1/auth/login | 없음 | RL_LOGIN |
| POST | /v1/auth/logout | ✓ | |
| GET | /v1/auth/sessions | ✓ | |
| DELETE | /v1/auth/sessions/{session_id} | ✓ | |
| POST | /v1/auth/password | ✓ | RL_LOGIN |
| GET | /v1/me | ✓ | |
| PATCH | /v1/me/profile | ✓ | |
| GET/PATCH | /v1/me/settings | ✓ | |
| PUT/DELETE | /v1/me/status | ✓ | |
| PUT | /v1/me/presence | ✓ | |
| GET | /v1/presence | ✓ | |
| POST/GET/DELETE | /v1/me/focus | ✓ | |
| GET | /v1/me/unreads | ✓ | |
| GET | /v1/me/follow-requests | ✓ | |
| POST | /v1/me/follow-requests/{user}/accept, /decline | ✓ | |
| GET | /v1/me/direct-invites | ✓ | |
| GET | /v1/users/{user} | ✓ | |
| GET | /v1/users | ✓ | |
| GET | /v1/users/search | ✓ | RL_SEARCH |
| GET | /v1/users/{user}/posts, /followers, /following, /stories, /guestbook | ✓ | |
| PUT/DELETE | /v1/users/{user}/follow | ✓ | RL_SOCIAL(PUT) |
| DELETE | /v1/users/{user}/follower | ✓ | |
| POST | /v1/users/{user}/guestbook | ✓ | RL_POST |
| DELETE | /v1/guestbook/{entry_id} | ✓ | |
| GET | /v1/friends | ✓ | |
| DELETE | /v1/friends/{user} | ✓ | |
| GET/POST | /v1/friends/requests | ✓ | RL_SOCIAL(POST) |
| POST | /v1/friends/requests/{id}/accept, /decline | ✓ | |
| DELETE | /v1/friends/requests/{id} | ✓ | |
| GET | /v1/blocks | ✓ | |
| PUT/DELETE | /v1/blocks/{user} | ✓ | RL_SOCIAL(PUT) |
| GET/POST | /v1/dms | ✓ | RL_SOCIAL(POST) |
| GET/POST | /v1/spaces | ✓ | RL_SOCIAL(POST) |
| GET/PATCH/DELETE | /v1/spaces/{space_id} | ✓ | |
| POST | /v1/spaces/{space_id}/transfer | ✓ | |
| GET | /v1/spaces/{space_id}/members | ✓ | |
| PATCH/DELETE | /v1/spaces/{space_id}/members/{user} | ✓ | |
| GET | /v1/spaces/{space_id}/bans | ✓ | |
| DELETE | /v1/spaces/{space_id}/bans/{user} | ✓ | |
| GET/POST | /v1/spaces/{space_id}/invites | ✓ | RL_SOCIAL(POST) |
| POST | /v1/spaces/{space_id}/direct-invites | ✓ | RL_SOCIAL |
| GET/POST | /v1/spaces/{space_id}/channels | ✓ | |
| GET/DELETE | /v1/invites/{code} | ✓ | |
| POST | /v1/invites/{code}/accept | ✓ | |
| POST | /v1/direct-invites/{id}/accept, /decline | ✓ | |
| DELETE | /v1/direct-invites/{id} | ✓ | |
| GET/PATCH/DELETE | /v1/channels/{channel_id} | ✓ | |
| GET/POST | /v1/channels/{channel_id}/members | ✓ | RL_SOCIAL(POST) |
| DELETE | /v1/channels/{channel_id}/members/{user} | ✓ | |
| PUT | /v1/channels/{channel_id}/prefs | ✓ | |
| PUT | /v1/channels/{channel_id}/read | ✓ | |
| GET/POST | /v1/channels/{channel_id}/messages | ✓ | RL_MESSAGE(POST) |
| GET | /v1/channels/{channel_id}/pins | ✓ | |
| GET/PATCH/DELETE | /v1/messages/{message_id} | ✓ | RL_MESSAGE(PATCH) |
| GET/POST | /v1/messages/{message_id}/thread | ✓ | RL_MESSAGE(POST) |
| PUT/DELETE/GET | /v1/messages/{message_id}/reactions/{emoji} | ✓ | RL_REACT(PUT/DELETE) |
| PUT/DELETE | /v1/messages/{message_id}/pin | ✓ | RL_REACT |
| GET | /v1/search/messages, /v1/search/posts | ✓ | RL_SEARCH |
| GET | /v1/hashtags/search | ✓ | RL_SEARCH |
| POST | /v1/uploads | ✓ | RL_UPLOAD |
| DELETE | /v1/uploads/{upload_id} | ✓ | |
| GET | /v1/media/{upload_id} | ✓ | |
| POST | /v1/posts | ✓ | RL_POST |
| GET/DELETE | /v1/posts/{post_id} | ✓ | |
| GET | /v1/posts/{post_id}/context, /replies, /likes, /reposts, /quotes | ✓ | |
| PUT/DELETE | /v1/posts/{post_id}/like, /repost | ✓ | RL_REACT |
| GET | /v1/timeline/following, /v1/timeline/discover | ✓ | |
| GET | /v1/hashtags/{tag}/posts | ✓ | |
| GET | /v1/trends | ✓ | |
| POST | /v1/stories | ✓ | RL_POST |
| GET | /v1/stories/feed | ✓ | |
| POST | /v1/stories/{story_id}/view | ✓ | |
| GET | /v1/stories/{story_id}/viewers | ✓ | |
| DELETE | /v1/stories/{story_id} | ✓ | |
| GET | /v1/notifications, /v1/notifications/unread-count | ✓ | |
| POST | /v1/notifications/read | ✓ | |
| GET | /v1/polls/{poll_id} | ✓ | |
| PUT/DELETE | /v1/polls/{poll_id}/vote | ✓ | RL_REACT |
| POST | /v1/polls/{poll_id}/close | ✓ | |
| POST | /v1/reports | ✓ | RL_SOCIAL |
| GET | /v1/admin/reports, /v1/admin/reports/{id} | admin | |
| POST | /v1/admin/reports/{id}/resolve | admin | |
| POST | /v1/admin/content/remove | admin | |
| POST | /v1/admin/users/{user}/suspend, /unsuspend | admin | |
| GET | /v1/admin/users, /v1/admin/actions | admin | |
| GET | /v1/ws | ✓ | RL_WS |

## 7. 슬래시 명령 → API 대응 (클라이언트 참고)

| 명령 | 동작 |
|---|---|
| `/me 춤춘다` | 메시지 `kind:"me"`, body `춤춘다` |
| `/shrug [텍스트]` | 메시지 `kind:"text"`, body `텍스트 ¯\_(ツ)_/¯` |
| `/poll "질문" "A" "B" ...` | 채팅: 메시지 `kind:"poll"` / 피드 작성기: `POST /v1/posts` 의 `poll` |
| `/status 🎧 듣는 중` | `PUT /v1/me/status` |
| `/focus 25m` | `POST /v1/me/focus {duration_sec:1500}` ; `/focus off` → `DELETE /v1/me/focus` |
| `/roll 2d6` | 메시지 `kind:"roll"`, body `2d6` |
| `/invite @hana` | 현재 방에 `POST /v1/spaces/{id}/direct-invites` ; `/invite link [7d] [10]` → `POST /v1/spaces/{id}/invites` |
