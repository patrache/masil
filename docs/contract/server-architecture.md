# masil 서버 구조 계약 (Cloudflare Workers · Durable Objects · D1 · R2)

- 상태: 확정 계약 v1 (2026-11-05). 상위 출처: 설계서 5.2·5.3, `api.md`, `events.md`, `data-model.md`.
- 목적: 서버 구현 에이전트들이 **파일 단위로 병렬 작업**해도 서로 맞물리도록 파일 경로·책임·공유 함수 시그니처·바인딩 이름을 못 박는다. 여기 적힌 시그니처를 바꾸려면 이 문서를 먼저 고친다.
- 금지: `wrangler deploy`, 원격 D1/R2 생성·마이그레이션 적용은 **오너 건별 승인 후**. 개발·테스트는 `wrangler dev`(로컬)와 vitest 만.

---

## 1. 스택·버전 (2026-11-05 `npm view` 확인)

| 패키지 | 버전 | 비고 |
|---|---|---|
| `hono` | 4.13.16 | 라우팅·미들웨어 |
| `zod` | 4.6.7 | 요청 검증 |
| `@hono/zod-validator` | 0.9.1 | peer: zod ^3.25 \|\| ^4, hono >=4.11.2 |
| `wrangler` | 4.157.0 | 내장 workerd 1.20261103.1 |
| `@cloudflare/vitest-pool-workers` | 0.23.0 | peer: **vitest ^4.1.0** |
| `vitest` | **4.1.12** | pool-workers 0.23.0 이 vitest 5 를 지원하지 않으므로 4.1.x 고정 |
| `typescript` | 7.0.2 | |
| `@cloudflare/workers-types` | 5.20261104.1 | wrangler peer 충족용. 런타임 타입은 `wrangler types` 생성 파일을 쓴다 |

- 참고: `@cloudflare/vitest-pool-workers` 는 `@cloudflare/vitest-plugin` 으로 이름이 바뀌어 0.23.0 이 마지막이다(README 의 deprecated 안내). 설계서·브리프가 pool-workers 를 지정했으므로 이를 쓰고, API(`cloudflareTest`, `readD1Migrations`, `cloudflare:test`)는 두 패키지가 같다. 이름 교체는 오너 결정 사항으로 남긴다.
- ULID 등 유틸은 외부 패키지 없이 직접 구현한다(번들 최소화).
- `compatibility_date: "2026-11-01"` — 2026-04-07 이후라 DO WebSocket 이 Close 프레임에 자동 응답한다(`web_socket_auto_reply_to_close`; `webSocketClose` 에서 `ws.close()` 호출 불필요).

## 2. 디렉터리·파일

```
server/
  package.json
  tsconfig.json
  wrangler.jsonc
  worker-configuration.d.ts      # `npx wrangler types` 생성물(커밋). Cloudflare.Env 정의
  vitest.config.ts
  .dev.vars.example              # 비밀 없음. 형식 안내만 (현재 필요한 비밀 없음)
  migrations/
    0001_init.sql                # data-model.md 3장 그대로
    0002_search.sql              # data-model.md 4장 그대로
  src/
    index.ts                     # export default { fetch, scheduled } + DO 클래스 re-export
    app.ts                       # createApp(): 전역 미들웨어 + 라우트 마운트
    env.ts                       # Env, AppEnv, AuthContext, Runtime 타입 + rt(c)
    types/
      api.ts                     # api.md 2장 객체 타입(TS) — 직렬화 함수의 반환 타입
      events.ts                  # events.md 이벤트 이름 유니온·data 타입 맵
      rows.ts                    # D1 행 타입(UserRow, MessageRow, ...) — data-model.md 컬럼과 1:1
    lib/
      ids.ts                     # ULID, 초대 코드, 시각 하한 ID
      time.ts                    # now(), iso()
      errors.ts                  # ApiError, err.*, onError, notFoundHandler
      validate.ts                # zod 래퍼(json/query) → invalid_request
      db.ts                      # D1 헬퍼, 커서, limit 파싱, json_each 목록
      crypto.ts                  # PBKDF2, 토큰, SHA-256
      auth.ts                    # requireAuth, requireAdmin, getAuth, resolveUserRef
      ratelimit.ts               # limit() 미들웨어, checkLimit()
      permissions.ts             # DM 허용·차단·방 역할·채널 접근·글 열람 규칙 (api.md 3장)
      serialize.ts               # 행 → api.md 객체(UserSummary, Message, Post, Channel, ...)
      text.ts                    # 정규화, 길이, 멘션·해시태그·주사위 파싱, 스니펫
      realtime.ts                # publishEvent, 청중 해석, 팬아웃
      notify.ts                  # 알림함 행 생성 + alert 생성
      uploads.ts                 # 이미지 판별, R2 키, R2 삭제
      search.ts                  # FTS 질의 생성, 하이라이트
      version.ts                 # User-Agent 파싱, min_client_version 검사 미들웨어
      log.ts                     # errFields(): 던져진 값 → 로그용 평범한 JSON 필드 (3.6 로그 형식)
    services/                    # 여러 라우트·운영자·DO 가 같이 쓰는 도메인 로직
      membership.ts              # 방 가입/탈퇴/강퇴, 채널 멤버 추가/제거
      messages.ts                # 메시지·스레드 답글 생성/수정/삭제, 시스템 메시지
      posts.ts                   # 글 생성/삭제, 카운터
      relations.ts               # 친구·차단·팔로우 상태 변경 (차단 효과 포함)
      polls.ts                   # 투표 생성 stmt, 투표/취소/마감
      content.ts                 # 운영자 강제 삭제(대상 종류별 분기)
      users.ts                   # 정지/해제, 세션 폐기
    routes/
      meta.ts                    # GET /v1/meta, /v1/health
      auth.ts                    # /v1/auth/*
      me.ts                      # /v1/me, /me/profile, /me/settings, /me/status, /me/unreads
      presence.ts                # /v1/me/presence, /v1/presence, /v1/me/focus
      users.ts                   # /v1/users, /users/search, /users/{user}, /users/{user}/posts|followers|following
      friends.ts                 # /v1/friends, /v1/friends/requests/*
      blocks.ts                  # /v1/blocks/*
      follows.ts                 # /v1/users/{user}/follow|follower, /v1/me/follow-requests/*
      dms.ts                     # /v1/dms
      spaces.ts                  # /v1/spaces/* (멤버·밴·이전·채널 목록/생성)
      invites.ts                 # /v1/spaces/{id}/invites, /v1/invites/*, direct-invites
      channels.ts                # /v1/channels/{id}, members, prefs, read, pins 목록
      messages.ts                # /v1/channels/{id}/messages, /v1/messages/* (스레드·리액션·고정 포함)
      search.ts                  # /v1/search/*, /v1/hashtags/search
      uploads.ts                 # /v1/uploads, /v1/media/{id}
      posts.ts                   # /v1/posts/*
      timelines.ts               # /v1/timeline/*, /v1/hashtags/{tag}/posts, /v1/trends
      stories.ts                 # /v1/stories/*, /v1/users/{user}/stories
      notifications.ts           # /v1/notifications/*
      polls.ts                   # /v1/polls/*
      guestbook.ts               # /v1/users/{user}/guestbook, /v1/guestbook/{id}
      reports.ts                 # /v1/reports
      admin.ts                   # /v1/admin/*
      ws.ts                      # GET /v1/ws → UserHub 로 넘김
    do/
      user-hub.ts                # class UserHub (사용자별 소켓·이벤트 로그·접속 상태·집중 모드)
      topic-hub.ts               # class TopicHub (토픽별 시청자 임대·팬아웃)
      protocol.ts                # 클라이언트 프레임 zod 스키마, 종료 코드 상수, notify 모드 허용 타입
    cron/
      index.ts                   # handleScheduled: controller.cron 으로 분기
      trends.ts                  # recomputeTrends(env)
      cleanup.ts                 # runCleanup(env)
  test/
    tsconfig.json
    env.d.ts
    apply-migrations.ts          # setupFiles: applyD1Migrations
    helpers.ts                   # signup/login/api/connectWs/uniqueHandle
    unit/      ids.test.ts crypto.test.ts text.test.ts db-cursor.test.ts search-query.test.ts uploads-sniff.test.ts
    api/       auth.test.ts me.test.ts users.test.ts friends.test.ts blocks.test.ts follows.test.ts dms.test.ts
               spaces.test.ts invites.test.ts channels.test.ts messages.test.ts threads.test.ts reactions-pins.test.ts
               search.test.ts uploads.test.ts posts.test.ts timelines.test.ts stories.test.ts notifications.test.ts
               polls.test.ts guestbook.test.ts reports-admin.test.ts ratelimit.test.ts permissions.test.ts
    realtime/  ws-handshake.test.ts ws-resume.test.ts fanout.test.ts presence-focus.test.ts topic-hub.test.ts notify-mode.test.ts
    cron/      trends.test.ts cleanup.test.ts
```

라우트 파일 하나 = 한 에이전트의 작업 단위가 되도록 나눴다. 라우트끼리 직접 import 하지 않는다. 공유 로직은 `lib/` 또는 `services/` 로.

## 3. Durable Objects 와 팬아웃

### 3.1 왜 이렇게 나눴나

- "클라이언트당 소켓 1개로 모든 방·DM 이벤트"(설계서 5.2) → 소켓은 방이 아니라 **사용자** 단위 DO 에 붙어야 한다(한 소켓이 여러 DO 에 붙을 수 없으므로).
- 방이 수백 명이어도 무리 없는 팬아웃 → 발행 시 D1 로 **TUI(full 연결)가 떠 있는 수신자만** 골라(`user_presence.online_until > now`) 그들의 UserHub 에만 RPC. 오프라인 사용자, 그리고 **데몬(notify 연결)만 떠 있는 사용자**의 DO 는 메시지·리액션·입력 중·SNS 이벤트로 깨우지 않는다(데몬은 OS 자동 시작으로 늘 떠 있으므로, 이를 온라인으로 치면 사실상 전원이 대상이 되어 아래 계산이 무너진다). 데몬만 떠 있는 사용자는 alert·알림함·집중 모드 이벤트만 `notify_online_until` 로 받는다(`events.md` 6.2). 500명 방에 TUI 를 켠 사람이 200명이면 메시지 1건 = D1 쿼리 1회 + DO RPC 200회(50개씩 병렬) + 그중 alert 대상(멘션·`notify:"all"`)에게 가는 alert RPC. Workers Paid 의 호출당 subrequest 한도는 기본 10,000 이고 `waitUntil` 은 응답 후 30초까지 이어지므로 충분하다.
- 피드 카운터처럼 "보고 있는 사람"이 청중인 것은 **토픽 DO(TopicHub)** 가 시청자 목록을 들고 있다.
- DO 클래스는 둘뿐이다. 전역 단일 DO 는 없다(병목 금지).

```
            ┌──────────── Worker (Hono) ────────────┐
REST 요청 → │ route → D1 batch(쓰기) → publishEvent │
            │    resolveAudience: D1 (online만)     │
            └──────┬───────────────────────┬────────┘
                   │ RPC deliver()          │ RPC publish()
          ┌────────▼────────┐      ┌───────▼────────┐
          │ UserHub(user A) │ ...  │ TopicHub(post:X)│── deliver() → UserHub(watchers)
          │ seq/log, sockets│      │ watchers lease │
          └──┬──────────┬───┘      └────────────────┘
        TUI 소켓   데몬 소켓
```

### 3.2 `UserHub` (`src/do/user-hub.ts`)

- 바인딩 `USER_HUB`, 이름 = `user_id` (`env.USER_HUB.getByName(userId)`).
- SQLite 저장소(생성자에서 `ctx.blockConcurrencyWhile` 로 `CREATE TABLE IF NOT EXISTS`):

```sql
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
-- keys: user_id, seq,
--   epoch_full, epoch_notify          (per-mode log generation; hello.epoch of a connection = its mode's epoch; events.md 6.2)
--   full_until, notify_until          (ms; watermarks mirroring D1 user_presence.online_until / notify_online_until;
--                                      raised only AFTER the D1 write succeeds, lowered immediately)
--   zero_since (ms; '' while any socket connected), mode, focus_started_at, focus_ends_at, last_status
CREATE TABLE IF NOT EXISTS events (seq INTEGER PRIMARY KEY, type TEXT NOT NULL, ts INTEGER NOT NULL, frame TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS device_acks (device_id TEXT NOT NULL, mode TEXT NOT NULL CHECK (mode IN ('full','notify')), epoch TEXT NOT NULL, seq INTEGER NOT NULL, updated_at INTEGER NOT NULL, PRIMARY KEY (device_id, mode));
CREATE TABLE IF NOT EXISTS focus_alerts (id TEXT PRIMARY KEY, kind TEXT NOT NULL, alert TEXT NOT NULL, created_at INTEGER NOT NULL);
```
  - `events.frame` = 클라이언트에 보낼 완성 JSON 문자열(seq·ts 포함). 재전송 때 그대로 보낸다.
- 소켓 수락: `this.ctx.acceptWebSocket(server, tags)` — 태그(최대 10개, 각 256자): `["mode:full"|"mode:notify", "dev:<device_id>", "sess:<session_id>"]`. `getWebSockets("mode:full")`, `getWebSockets("dev:<id>")`, `getWebSockets("sess:<id>")` 로 고른다.
- 연결별 상태는 `ws.serializeAttachment(att)` (JSON, 최대 16,384바이트):

```ts
interface ConnAttachment {
  connId: string; sessionId: string; userId: string;
  mode: "full" | "notify"; deviceId: string; connectedAt: number;
  ready: boolean;                 // resume 처리 완료
  idle: boolean;
  topics: string[];               // watch 중인 토픽(<=50)
  typingAt: Record<string, number>; // channelId(+":"+threadRootId) -> last forwarded ms (<=20 keys)
  win: { start: number; count: number }; // frame rate window
}
```
- 생성자에서 `this.ctx.setWebSocketAutoResponse(new WebSocketRequestResponsePair('{"type":"ping"}', '{"type":"pong"}'))`.
- 공개 메서드(RPC; 인자·반환은 구조화 복제 가능한 값만):

```ts
export class UserHub extends DurableObject<Env> {
  /** WebSocket upgrade only. The Worker has already authenticated; it passes AuthContext in header "X-Masil-Auth" (JSON). */
  fetch(request: Request): Promise<Response>;

  /** Deliver events to this user's sockets. Logged events get seq + go to the log. */
  deliver(batch: DeliveryBatch): Promise<void>;

  setPresenceMode(userId: string, mode: "auto" | "away" | "dnd"): Promise<PresenceState>;
  getPresenceState(userId: string): Promise<PresenceState>;
  startFocus(userId: string, durationSec: number): Promise<Focus>;
  endFocus(userId: string): Promise<FocusSummary | null>;   // null if not focusing

  /** Close sockets of given sessions ("all" = every socket) with code 4001/4003 after sending session.revoked. */
  revokeSessions(userId: string, sessionIds: string[] | "all", reason: "logout" | "revoked" | "password_changed" | "suspended"): Promise<void>;

  /** Test/debug only. */
  inspect(): Promise<{ userId: string | null; epochFull: string; epochNotify: string; seq: number; oldestSeq: number; connections: number; fullUntil: number; notifyUntil: number; status: PresenceStatus }>;

  webSocketMessage(ws: WebSocket, message: string | ArrayBuffer): Promise<void>;
  webSocketClose(ws: WebSocket, code: number, reason: string, wasClean: boolean): Promise<void>;
  webSocketError(ws: WebSocket, error: unknown): Promise<void>;
  alarm(): Promise<void>;
}

export interface OutboundEvent {
  type: ServerEventType;          // events.md 8장
  data: unknown;
  ts: number;                     // ms
  logged: boolean;                // true => seq + log
  topic?: string;                 // set for topic events; delivered only to sockets watching it
  onlyDevice?: string;            // device.tui_state
}
export interface DeliveryBatch { userId: string; events: OutboundEvent[] }
```

- `deliver` 처리(한 번의 동기 SQL 트랜잭션 구간에서):
  1. `meta.user_id` 가 없으면 저장.
  2. 각 이벤트: `type==="alert"` 이면 집중/dnd 상태로 `suppressed`·`suppressed_reason` 을 채우고 집중 중이면 `focus_alerts` 에 INSERT.
  3. `logged` 면 `seq+1`, frame 생성, `events` INSERT. 아니면 `seq:null` frame.
  4. 소켓 전송: `ready` 인 소켓에만. notify 소켓은 `events.md` 7장 허용 타입만. `topic` 이벤트는 `att.topics` 에 그 토픽이 있는 소켓만. `ws.send` 예외는 무시(닫히는 중).
- `fetch`(업그레이드):
  1. `X-Masil-Auth` 파싱(`{userId, sessionId, handle, role}`), 쿼리 `mode`, `device_id`.
  2. 현재 소켓이 10개면 가장 오래된 것에 `close(4009,"replaced")`.
  3. epoch 검사(`events.md` 6.2, `zero_since` 와 무관): epoch 가 없거나 `now >= meta.full_until` 이면 새 `epoch_full`(ULID). epoch 가 없거나 `now >= max(full_until, notify_until)` 이면 새 `epoch_notify`.
  4. 이 연결 모드의 D1 기한을 쓰고 **끝까지 await**: full → `online_until = now+10분`, notify → `notify_online_until = now+40분`(이미 그보다 길면 그대로). 성공하면 워터마크(`full_until`/`notify_until`) 갱신. 실패하면 `1011` 로 거절(연결을 받지 않음). 3~4 는 `ctx.blockConcurrencyWhile` 안에서 처리해 동시에 들어온 연결이 epoch 를 두 번 바꾸지 않게 한다.
  5. `acceptWebSocket`, attachment 저장, `hello` 전송(그 모드의 epoch), presence 재계산, full 연결이 0→1 이면 같은 기기 notify 소켓에 `device.tui_state`, 알람 예약.
  - 4 를 `hello` 전에 끝내는 이유: 그 뒤 커밋되는 이벤트의 발행자가 반드시 이 사용자를 대상으로 보게 하려는 것. 그 전에 커밋된 것은 클라이언트가 `ready` 뒤에 하는 REST 조회가 잡는다.
- `webSocketMessage`: `do/protocol.ts` 의 zod 스키마로 파싱 → `resume|ack|typing|idle|watch`. 속도 제한(10초 30, 60 넘으면 4008). `resume` 이 10초 안에 안 오면 알람이 4000 으로 닫는다.
  - `typing`: 채널 쓰기 권한을 D1 로 확인(`permissions.canWriteChannel`; 결과를 attachment 에 5분 캐시해도 됨) → `publishEvent(..., {channel}, "typing", ..., {ephemeral:true, exclude:[userId]})`.
  - `watch`: 이전/새 토픽 비교 → 이 사용자의 다른 소켓도 안 보는 토픽은 `TopicHub.leave`, 새 목록 전부 `TopicHub.join(userId, 300_000)`(갱신 겸).
  - `ack`: `device_acks` upsert, 키 (`device_id`, 이 연결의 `mode`), 그 모드의 현재 epoch 와 함께. `resume{use_device_ack}` 는 같은 모드의 행만 읽는다(TUI 와 데몬이 같은 `device_id` 를 써도 서로 덮어쓰지 않음).
- `webSocketClose`/`webSocketError`: presence 재계산. full 연결이 1→0 이면 워터마크 `full_until=now+120_000` 즉시 반영 + D1 `online_until=now+120_000` + 같은 기기 notify 소켓에 `device.tui_state{tui_connected:false}`. notify 연결이 1→0 이면 `notify_until`·D1 `notify_online_until` 을 같은 방식으로 `now+120_000`. 남은 소켓 0이면 `zero_since=now`, `last_seen_at=now`.
- `alarm`: 알람은 DO 당 1개이므로 다음 시각 = min(집중 종료, full 소켓이 있으면 now+60초 / notify 소켓만 있으면 now+15분, resume 대기 소켓의 10초 기한).
  - 집중 종료 처리: `focus.ended` + `alert{kind:"focus_summary"}`(기록), presence 재계산, `focus_alerts` 비움.
  - 하트비트: `ctx.getWebSocketAutoResponseTimestamp(ws)`(없으면 connectedAt) 가 120초 넘은 소켓 `close(4002)`. full 소켓은 60초마다 검사하므로 죽은 TUI 는 최대 약 180초 뒤 정리된다(`events.md` 3.4).
  - 놓친 close 처리: `getWebSockets("mode:full")` 이 비었는데 `full_until > now + 120_000` 이면(코드 배포로 인한 강제 끊김, 응답 없는 상대에게 보낸 close, 퇴거 등으로 `webSocketClose` 가 안 돈 경우) 위 full 1→0 처리를 여기서 한다. notify 도 같은 방식. 모든 소켓이 없고 `zero_since` 가 비어 있으면 `zero_since=now`, `last_seen_at=now`.
  - 기한 연장: full 소켓이 있고 `full_until - now < 5분` 이면 D1 `online_until=now+10분` → 성공 후 워터마크 반영(D1 쓰기 약 5분에 1번). notify 소켓이 있으면 D1 `notify_online_until=now+40분` → 성공 후 워터마크 반영(15분에 1번).
  - 연장이 계속 실패해 full 소켓이 있는데 `now >= full_until` 이 되면 full 소켓을 전부 `close(1011)` 한다(발행 대상에서 빠진 채 조용히 놓치지 않게. 재접속 시 3번 규칙으로 새 epoch → 재동기화).
  - 로그 정리: `DELETE FROM events WHERE seq <= (SELECT MAX(seq)-2000 FROM events) OR ts < now-86_400_000`.
- 접속 상태 계산·D1 반영·`presence.updated`(청중 `presenceWatchers`, 휘발)·`presence.self` 발행은 `events.md` 6.3 그대로. 바뀔 때만.
- 정지·로그아웃: Worker 가 D1 에서 세션을 폐기한 **뒤** `revokeSessions` 호출.

### 3.3 `TopicHub` (`src/do/topic-hub.ts`)

- 바인딩 `TOPIC_HUB`, 이름 = 토픽 문자열(`post:<post_id>`).
- SQLite: `CREATE TABLE IF NOT EXISTS watchers (user_id TEXT PRIMARY KEY, expires_at INTEGER NOT NULL)`.

```ts
export class TopicHub extends DurableObject<Env> {
  join(userId: string, ttlMs: number): Promise<void>;          // upsert expires_at = now + ttlMs
  leave(userId: string): Promise<void>;
  publish(topic: string, events: OutboundEvent[], opts?: { visibleTo?: VisibilitySpec }): Promise<number>; // returns delivered user count
  inspect(): Promise<{ watchers: number }>;
  alarm(): Promise<void>;                                       // purge expired; if empty -> ctx.storage.deleteAll()
}
```
- `publish`: 만료 안 된 시청자 → (`visibleTo` 가 있으면) D1 질의 1회로 가시성 필터(`events.md` 4.8, 아래 4.11 `VisibilitySpec`; 시청자 ID 목록은 `jsonList` 파라미터 1개) → 통과한 각 UserHub `deliver({userId, events: events.map(e => ({...e, topic}))})` 를 50개씩 병렬(`Promise.allSettled`). `post.reply_created` 는 반드시 `visibleTo` 를 넘긴다.
- `join` 시 알람이 없으면 `now + ttl` 로 예약.

### 3.4 Worker → UserHub 소켓 넘기기 (`src/routes/ws.ts`)

```ts
// GET /v1/ws
// 1) requireAuth (Bearer) 2) limit("RL_WS") 3) validate query {mode, device_id}
// 4) const headers = new Headers(c.req.raw.headers); headers.delete("X-Masil-Auth");  // never trust client copy
//    headers.set("X-Masil-Auth", JSON.stringify(getAuth(c)));
// 5) return env.USER_HUB.getByName(auth.userId).fetch(new Request(c.req.raw.url, { method: "GET", headers }));
```
- `Upgrade: websocket` 이 아니면 `400 invalid_request`. DO 에는 공개 경로가 없으므로(Worker 를 거쳐서만 도달) `X-Masil-Auth` 를 신뢰한다. 전역 미들웨어는 모든 들어오는 요청에서 `X-Masil-*` 헤더를 지운다.

### 3.5 발행 흐름 규칙

1. 라우트/서비스는 D1 쓰기를 **`db.batch` 로 커밋한 뒤** `publishEvent` 를 부른다(커밋 전 발행 금지).
2. `publishEvent` 는 내부적으로 Promise 를 만들어 `rt.waitUntil()` 에 넘기고 즉시 반환한다(응답 지연 없음). **`env.APP_ENV === "test"` 면 끝까지 await** 해서 테스트가 결정적이 되게 한다.
3. 팬아웃 실패(DO 오류)는 `console.error` 만 하고 재시도하지 않는다 — 클라이언트 재동기화가 메운다.
4. 같은 요청에서 여러 이벤트를 같은 청중에 보내면 `publishEvents` 로 묶어 UserHub RPC 를 1회로 줄인다.

### 3.6 로그 형식

- 모든 `console.*` 호출은 **구조화 객체 하나**만 넘기고, 그 첫 키는 상수 메시지 `msg` 다:
  `console.error({ msg: "TopicHub deliver failed", topic, target, err: errFields(e) })`.
  Workers Logs 문서가 필드로 색인된다고 보여 주는 모양이 이 단일 객체 형식이다(workers-logs 문서 "log in JSON format").
  `console.error("msg", {...})` 같은 두 인자 형식이 어떻게 색인되는지는 문서에 없으므로 쓰지 않는다.
- 값(토픽·사용자 ID·요청 경로·라벨·오류 문자열)을 `msg` 에 넣지 않는다 — 템플릿 리터럴·문자열 연결·객체 뒤 위치 인자 모두 금지.
  메시지에 섞인 값은 줄바꿈 등으로 로그 줄을 위조·분할할 수 있다. `msg` 는 이스케이프 없는 큰따옴표 상수 문자열이고,
  객체에 `msg` 를 다시 쓰거나 펼침(`...`)·계산된 키를 쓰지 않는다(`msg` 를 덮어쓸 수 있으므로).
- `console` 은 `console.<log|info|warn|error|debug>(` 호출로만 쓴다 — 별칭(`const log = console.error`, `const { error } = console`),
  `.call`/`.apply`, `console?.`, `console["error"]` 금지(검사기가 모양을 볼 수 없다).
- 오류는 `err: errFields(e)` 로 넣는다(`src/lib/log.ts`). Error 의 name·message·stack 은 열거되지 않는 속성이라 필드 객체 안에
  그대로 두면 JSON 으로 `{}` 가 된다. `errFields` 는 **절대 던지지 않는다** — `blockConcurrencyWhile` 안의 catch 와 `.catch`
  처리기에서 불리므로, 문자열 변환이 실패하는 값(`Object.create(null)` 등)은 `"[unprintable thrown value]"` 로 남긴다.
- `test/unit/log-format.test.ts` 가 `src/**/*.ts` 전체를 (주석·문자열을 걷어 낸 코드에서) 훑어 이 형식을 강제한다.
- 범위: Workers 로그로 가는 `server/src` 만이다. `server/scripts/`(예: `smoke.mjs`)는 개발자 터미널에 사람이 읽을 출력을 내는
  로컬 Node CLI 라 Workers Logs 에 닿지 않으므로 이 규칙에서 뺀다.

## 4. 공유 헬퍼 시그니처

### 4.1 `src/env.ts`

```ts
import type { Context } from "hono";
export type Env = Cloudflare.Env;     // generated by `wrangler types` from wrangler.jsonc

export interface AuthContext {
  userId: string;
  sessionId: string;
  handle: string;
  role: "user" | "admin";
}

export type AppEnv = {
  Bindings: Env;
  Variables: { auth: AuthContext; requestId: string };
};
export type AppContext = Context<AppEnv>;

/** What helpers need to run outside a request too (DO, cron). */
export interface Runtime {
  env: Env;
  waitUntil(p: Promise<unknown>): void;
}
export function rt(c: AppContext): Runtime;            // { env: c.env, waitUntil: p => c.executionCtx.waitUntil(p) }
```

`Cloudflare.Env` 에 생기는 바인딩 이름(6장 wrangler.jsonc 와 일치): `DB: D1Database`, `MEDIA: R2Bucket`, `USER_HUB: DurableObjectNamespace<UserHub>`, `TOPIC_HUB: DurableObjectNamespace<TopicHub>`, `RL_SIGNUP | RL_LOGIN | RL_MESSAGE | RL_POST | RL_REACT | RL_UPLOAD | RL_SOCIAL | RL_SEARCH | RL_WS | RL_API: RateLimit`, `APP_ENV: string`, `MIN_CLIENT_VERSION: string`.

### 4.2 `src/lib/errors.ts`

```ts
export type ErrorCode =
  | "invalid_json" | "invalid_request" | "unauthorized" | "invalid_credentials"
  | "account_suspended" | "forbidden" | "not_admin" | "dm_not_allowed" | "action_not_allowed"
  | "read_only" | "private_account" | "banned_from_space" | "not_found"
  | "handle_taken" | "already_exists" | "already_member" | "conflict" | "poll_closed"
  | "invite_invalid" | "payload_too_large" | "unsupported_media_type" | "limit_exceeded"
  | "upgrade_required" | "rate_limited" | "internal_error" | "service_unavailable";

export class ApiError extends Error {
  constructor(
    readonly status: 400 | 401 | 403 | 404 | 409 | 410 | 413 | 415 | 422 | 426 | 429 | 500 | 503,
    readonly code: ErrorCode,
    message: string,
    readonly details: Record<string, unknown> = {},
  );
}

export const err: {
  invalid(fields: Record<string, "required" | "too_long" | "too_short" | "invalid" | "not_allowed">): ApiError;
  unauthorized(): ApiError;
  forbidden(code?: "forbidden" | "not_admin" | "dm_not_allowed" | "action_not_allowed" | "read_only"
                  | "private_account" | "banned_from_space", message?: string, details?: Record<string, unknown>): ApiError;
  suspended(until: number | null, reason: string): ApiError;
  notFound(what?: string): ApiError;
  conflict(code: "handle_taken" | "already_exists" | "already_member" | "conflict" | "poll_closed", reason?: string): ApiError;
  inviteInvalid(reason: "expired" | "exhausted" | "revoked"): ApiError;
  limit(what: string, limit: number): ApiError;
  rateLimited(limiter: string, retryAfterSec: number): ApiError;   // also sets Retry-After in onError
};

export function onError(e: unknown, c: AppContext): Response;      // ApiError -> envelope; others -> 500 internal_error (logged)
export function notFoundHandler(c: AppContext): Response;          // 404 not_found envelope
```

### 4.3 `src/lib/validate.ts`

```ts
import type { ZodType } from "zod";
/** zValidator wrapper: on failure throws err.invalid({ <path>: "required"|"too_long"|"too_short"|"invalid" }). */
export function vJson<T extends ZodType>(schema: T): MiddlewareHandler<AppEnv>;
export function vQuery<T extends ZodType>(schema: T): MiddlewareHandler<AppEnv>;
// shared zod pieces
export const zId: ZodType<string>;            // ULID
export const zUserRef: ZodType<string>;       // "@handle" | ULID | "@me"
export const zNonce: ZodType<string>;
export function zText(min: number, max: number): ZodType<string>;   // NFC + trim, code-point length
```
조회 값은 `c.req.valid("json")` / `c.req.valid("query")`.

### 4.4 `src/lib/db.ts`

```ts
export function stmt(db: D1Database, sql: string, ...params: unknown[]): D1PreparedStatement;
export function first<T>(db: D1Database, sql: string, ...params: unknown[]): Promise<T | null>;
export function all<T>(db: D1Database, sql: string, ...params: unknown[]): Promise<T[]>;
export function run(db: D1Database, sql: string, ...params: unknown[]): Promise<D1Result>;
export function batch(db: D1Database, stmts: D1PreparedStatement[]): Promise<D1Result[]>;   // atomic
/** For IN-lists: `WHERE id IN (SELECT value FROM json_each(?))` with jsonList(ids) as the single param. */
export function jsonList(ids: readonly string[]): string;

export interface Cursor { k: string | number; i: string }
export function encodeCursor(c: Cursor): string;            // base64url(JSON)
export function decodeCursor(s: string | undefined): Cursor | null;   // throws err.invalid({cursor:"invalid"})
export function parseLimit(raw: string | undefined, def?: number, max?: number): number; // def 30, max 100
export interface Page<T> { items: T[]; next_cursor: string | null }
```
- `undefined` 파라미터는 `null` 로 바꿔 바인딩한다(D1 은 undefined 를 거부).
- 바인딩 파라미터 100개 한도 → 가변 목록은 반드시 `jsonList`.

### 4.5 `src/lib/ids.ts`, `src/lib/time.ts`, `src/lib/crypto.ts`

```ts
export function newId(nowMs?: number): string;               // monotonic ULID
export function isId(s: string): boolean;
export function idFloor(ms: number): string;                 // smallest ULID at time ms (for "since N days" bounds)
export function newInviteCode(): string;                     // 8 chars, unambiguous alphabet

export function now(): number;                               // Date.now()
export function iso(ms: number): string;
export function isoOrNull(ms: number | null | undefined): string | null;

export function hashPassword(password: string): Promise<string>;              // "pbkdf2_sha256$100000$salt$hash"
export function verifyPassword(password: string, stored: string): Promise<boolean>;
export function dummyVerify(): Promise<void>;                                 // timing parity for unknown handles
export function newSessionToken(): string;                                    // "msl_" + 43 chars
export function sha256Hex(s: string): Promise<string>;
```

### 4.6 `src/lib/auth.ts`

```ts
export const requireAuth: MiddlewareHandler<AppEnv>;   // Bearer -> session -> user; 401/403; sets c.var.auth; then RL_API check; touches last_used_at (>=1h)
export const requireAdmin: MiddlewareHandler<AppEnv>;  // after requireAuth; 403 not_admin
export function getAuth(c: AppContext): AuthContext;
export function authenticateToken(env: Env, token: string): Promise<AuthContext | null>;  // throws err.suspended for suspended users
/** "@handle" | ULID | "@me" -> user row; 404 if missing. */
export function resolveUserRef(db: D1Database, ref: string, me: AuthContext): Promise<UserRow>;
```

### 4.7 `src/lib/ratelimit.ts`

```ts
export type LimiterName =
  | "RL_SIGNUP" | "RL_LOGIN" | "RL_MESSAGE" | "RL_POST" | "RL_REACT"
  | "RL_UPLOAD" | "RL_SOCIAL" | "RL_SEARCH" | "RL_WS" | "RL_API";
export const LIMITER_PERIOD_SEC: Record<LimiterName, 10 | 60>;    // must equal wrangler.jsonc simple.period
/** Default key: `u:${auth.userId}`; use after requireAuth. */
export function limit(name: LimiterName, key?: (c: AppContext) => string): MiddlewareHandler<AppEnv>;
export function checkLimit(env: Env, name: LimiterName, key: string): Promise<void>;  // throws err.rateLimited
export function clientIp(c: AppContext): string;                 // CF-Connecting-IP header, "0.0.0.0" if absent
```
사용 예: `messages.post("/channels/:id/messages", limit("RL_MESSAGE"), vJson(SendMessage), handler)`.

### 4.8 `src/lib/permissions.ts` (api.md 3장 구현)

```ts
export type SpaceRole = "owner" | "admin" | "member";

export function areFriends(db: D1Database, a: string, b: string): Promise<boolean>;
export function isBlockedEither(db: D1Database, a: string, b: string): Promise<boolean>;
export function hasBlocked(db: D1Database, blocker: string, blocked: string): Promise<boolean>;
export function shareSpace(db: D1Database, a: string, b: string): Promise<boolean>;
export function canDm(db: D1Database, a: string, b: string): Promise<boolean>;                    // 3.1
export function canDmMany(db: D1Database, a: string, others: string[]): Promise<{ ok: string[]; denied: string[] }>;

export function getSpaceRole(db: D1Database, spaceId: string, userId: string): Promise<SpaceRole | null>;
/** 404 if not a member (or space deleted); 403 forbidden if below `min`. Returns actual role. */
export function requireSpaceRole(db: D1Database, spaceId: string, userId: string, min: SpaceRole): Promise<SpaceRole>;

export interface ChannelAccess {
  channel: ChannelRow;
  spaceRole: SpaceRole | null;
  canRead: boolean;
  canWrite: boolean;
  readOnlyReason: "not_eligible" | "blocked" | null;
  otherUserId: string | null;          // 1:1 dm partner
}
/** text channels (public AND private) also require: space not deleted + user is a current space member (api.md 3.5/3.7). */
export function getChannelAccess(db: D1Database, channelId: string, userId: string): Promise<ChannelAccess | null>;
export function requireChannelRead(db: D1Database, channelId: string, userId: string): Promise<ChannelAccess>;   // 404
export function requireChannelWrite(db: D1Database, channelId: string, userId: string): Promise<ChannelAccess>;  // 404 / 403 read_only
export function canModerateChannel(access: ChannelAccess): boolean;          // owner/admin of the space (text) ; pin rule for dm handled separately
export function accessibleChannelIds(db: D1Database, userId: string, opts?: { spaceId?: string }): Promise<string[]>;

/** SQL fragment + params enforcing api.md 3.4 for alias `p` (posts) and viewer. Use in every post query. */
export function postVisibilitySql(viewerId: string, alias?: string): { sql: string; params: unknown[] };
export function canViewPost(db: D1Database, viewerId: string, post: PostRow): Promise<boolean>;
/** Owner's SNS content (posts tab, stories, guestbook, follow lists, story media, reports of them) for anyone but the
 *  owner: missing owner, owner suspended now (isSuspendedNow — suspended_until <= now counts as active again, same
 *  boundary as postVisibilitySql) or block either way -> "hidden" (404); private + not active follower -> "private". */
export function canViewUserContent(db: D1Database, viewerId: string, ownerId: string): Promise<"ok" | "private" | "hidden">;
/** SQL fragment + params for guestbook alias `g`: not deleted, author not suspended now, no block either way with the
 *  viewer. The owner side is checked separately with canViewUserContent. */
export function guestbookEntryVisibilitySql(viewerId: string, alias?: string): { sql: string; params: unknown[] };
export function presenceVisible(db: D1Database, viewerId: string, targetIds: string[]): Promise<Set<string>>;  // 3.6
```

### 4.9 `src/lib/serialize.ts`

`viewerId === null` 이면 "이벤트용" 기본값(`events.md` 4.4)으로 채운다.

```ts
export function toUserSummary(row: UserRow, nowMs?: number): UserSummary;
export function loadUserSummaries(db: D1Database, ids: readonly string[]): Promise<Map<string, UserSummary>>;
export function serializeMe(db: D1Database, userId: string): Promise<Me>;
export function serializeProfile(db: D1Database, viewerId: string, row: UserRow): Promise<Profile>;
export function serializeSession(row: SessionRow, currentSessionId: string): Session;
export function serializeMedia(row: UploadRow): Media;
export function serializeUpload(row: UploadRow): Upload;
export function serializeChannels(db: D1Database, viewerId: string | null, rows: ChannelRow[]): Promise<Channel[]>;
export function serializeSpace(db: D1Database, viewerId: string | null, row: SpaceRow): Promise<Space>;
export function serializeSpaceDetail(db: D1Database, viewerId: string, row: SpaceRow): Promise<SpaceDetail>;
export function serializeMessages(db: D1Database, viewerId: string | null, rows: MessageRow[]): Promise<Message[]>;  // batch-loads authors, reactions, pins, polls, mentions
export function serializePolls(db: D1Database, viewerId: string | null, pollIds: string[]): Promise<Map<string, Poll>>;
export function serializePosts(db: D1Database, viewerId: string | null, rows: PostRow[]): Promise<Post[]>;      // media, quote (1 level), viewer flags
export function serializeTimelineItems(db: D1Database, viewerId: string, rows: TimelineRow[]): Promise<TimelineItem[]>;
export function serializeStories(db: D1Database, viewerId: string, rows: StoryRow[]): Promise<Story[]>;
export function serializeNotifications(db: D1Database, rows: NotificationRow[]): Promise<Notification[]>;
export function serializeGuestbook(db: D1Database, viewerId: string, rows: GuestbookRow[]): Promise<GuestbookEntry[]>;
export function serializeReports(db: D1Database, rows: ReportRow[]): Promise<Report[]>;
```
N+1 금지: 목록 직렬화는 관련 행을 `json_each` IN 쿼리로 한 번에 읽는다.

### 4.10 `src/lib/text.ts`

```ts
export function normalizeText(s: string): string;                   // NFC + trim
export function charCount(s: string): number;                       // code points
export function extractMentions(body: string): string[];            // lowercase handles, unique, max 50, ignores code spans/blocks
// Resolution to users is NOT done here: services/messages.ts keeps only handles whose user passes requireChannelRead
// for the channel and has no block either way with the author (api.md 2.1). @here expands to readers with
// user_presence.status IN ('online','away'), never in 1:1 dm.
export function hasHereMention(body: string): boolean;
export function extractHashtags(body: string): string[];            // normalized, unique, max 10
export function parseDice(expr: string): { count: number; sides: number; modifier: number } | null;
export function rollDice(spec: { count: number; sides: number; modifier: number }): RollResult;
export function plainSnippet(body: string, maxChars: number): string; // strip markdown, collapse whitespace
```

### 4.11 `src/lib/realtime.ts`

```ts
export type Audience =
  | { users: string[] }
  | { channel: string }
  | { space: string }
  | { followers: string }           // active followers of userId (blocked pairs removed)
  | { presenceWatchers: string }    // friends + dm/group_dm co-members of userId
  | { topic: string };              // "post:<id>" -> TopicHub

/** Per-recipient post visibility (events.md 4.8, api.md 3.4). A recipient R passes iff
 *  - no block in either direction between R and every id in `authors`, and
 *  - for every id P in `privateAuthors`: R === P or R is an active follower of P.
 *  Required for timeline.item_created (authors = post author + quoted author + reposter)
 *  and post.reply_created (authors = reply author + parent author). */
export interface VisibilitySpec { authors: string[]; privateAuthors: string[] }

export interface PublishOptions {
  ephemeral?: boolean;              // default false (logged). events.md marks each type
  exclude?: string[];               // user ids to skip
  includeSelf?: string;             // add this user even if not in audience (e.g. author for followers audience)
  visibleTo?: VisibilitySpec;       // SNS content events: filter recipients (followers audience in SQL, topic audience inside TopicHub)
}

export function publishEvent<T extends ServerEventType>(
  rt: Runtime, audience: Audience, type: T, data: ServerEventData<T>, opts?: PublishOptions,
): Promise<void>;

export function publishEvents(
  rt: Runtime, audience: Audience, events: Array<{ type: ServerEventType; data: unknown; ephemeral?: boolean }>, opts?: Omit<PublishOptions, "ephemeral">,
): Promise<void>;

/** Online recipients for an audience. notifyOk=false (default): online_until > now (full/TUI only).
 *  notifyOk=true: online_until > now OR notify_online_until > now. Exposed for tests and alert targeting. */
export function resolveOnlineAudience(
  env: Env, audience: Exclude<Audience, { topic: string }>,
  opts?: { exclude?: string[]; notifyOk?: boolean; visibleTo?: VisibilitySpec },
): Promise<string[]>;

/** Low-level: deliver to explicit users regardless of audience rules (still online-filtered by the same notifyOk rule). */
export function deliverToUsers(rt: Runtime, userIds: string[], events: OutboundEvent[]): Promise<void>;
```
- `ServerEventType`, `ServerEventData<T>` 는 `src/types/events.ts` 에 events.md 8장 목록 그대로 정의.
- **온라인 판정 모드**: `publishEvent(s)`·`deliverToUsers` 는 보낼 이벤트 타입이 **전부** `NOTIFY_ALLOWED_TYPES`(`do/protocol.ts`, `events.md` 7장의 서버→클라이언트 허용 타입) 안이면 `notifyOk=true`, 하나라도 밖이면 `notifyOk=false` 로 대상을 고른다. 섞인 묶음은 둘로 나눠 보낸다. 데몬만 떠 있는 사용자의 UserHub 를 메시지·입력 중·SNS 이벤트로 깨우지 않기 위함(`events.md` 6.2).
- 온라인 필터 SQL 예(공개 text 채널): `SELECT sm.user_id FROM space_members sm JOIN user_presence p ON p.user_id = sm.user_id WHERE sm.space_id = ?1 AND p.online_until > ?2`. notify 허용 타입이면 조건이 `(p.online_until > ?2 OR p.notify_online_until > ?2)`.
- 비공개 text 채널 청중: `channel_members` ∩ 그 방의 `space_members`, 방 `deleted_at IS NULL`(`events.md` 6.1). 공개 text 도 방 미삭제 조건.
- `visibleTo` 필터 SQL(수신자 후보 `r.user_id`): `NOT EXISTS (SELECT 1 FROM blocks b, json_each(?authors) a WHERE (b.blocker_id = r.user_id AND b.blocked_id = a.value) OR (b.blocker_id = a.value AND b.blocked_id = r.user_id)) AND NOT EXISTS (SELECT 1 FROM json_each(?private) pa WHERE pa.value <> r.user_id AND NOT EXISTS (SELECT 1 FROM follows f WHERE f.follower_id = r.user_id AND f.followee_id = pa.value AND f.status = 'active'))`.

### 4.12 `src/lib/notify.ts`

```ts
export interface NotifyInput {
  userId: string;                    // recipient
  type: NotificationType;
  actorId: string | null;
  subject: { type: Notification["subject"]["type"]; id: string };
  preview?: string;
  context?: Partial<Notification["context"]>;
  dedupeKey?: string;
  alert?: { kind: AlertKind; title: string; body: string; target: Alert["target"] } | null;  // null = no desktop alert
}
/** Skips if actor === recipient or blocked either way. INSERT OR IGNORE by dedupe_key.
 *  Publishes notification.created (logged) and, if settings allow, alert (logged). */
export function notify(rt: Runtime, input: NotifyInput): Promise<void>;
export function notifyMany(rt: Runtime, inputs: NotifyInput[]): Promise<void>;

/** The one "inbox row is shown" rule (api.md 5.18) for notifications alias `n`: actor not blocked by the recipient and
 *  not suspended now (same boundary as postVisibilitySql). Used by GET /v1/notifications and every unread_count. */
export function inboxRowVisibleSql(userExpr: string, nowExpr: string): string;
/** Unread rows passing inboxRowVisibleSql (single source for the inbox routes, notification.created, notifications.read). */
export function unreadNotificationCount(db: D1Database, userId: string): Promise<number>;

/** Alert without an inbox row (DMs, channel_message). Applies settings, channel prefs, blocks. */
export function sendAlerts(rt: Runtime, items: Array<{ userId: string; kind: AlertKind; actorId: string | null; title: string; body: string; target: Alert["target"]; channelId?: string }>): Promise<void>;

/** Computes per-recipient alerts for a new message (dm > mention > thread_reply > channel_message) and inbox rows for mentions/thread replies. */
export function fanoutMessageNotifications(rt: Runtime, message: Message, channel: ChannelRow): Promise<void>;
```
집중 모드·dnd 에 따른 `suppressed` 는 UserHub 가 채운다(notify 는 모름).

### 4.13 `src/lib/uploads.ts`, `src/lib/search.ts`, `src/lib/version.ts`

```ts
export function sniffImage(head: Uint8Array): { contentType: Media["content_type"]; ext: "png" | "jpg" | "gif" | "webp"; width: number | null; height: number | null } | null;
export function r2Key(purpose: "avatar" | "post" | "story", ownerId: string, uploadId: string, ext: string): string;
export function removeUploads(env: Env, uploadIds: string[]): Promise<void>;   // R2 delete + status='removed'

export function buildMessageSearch(q: string): { mode: "fts"; match: string; terms: string[] } | { mode: "like"; terms: string[] };
export function highlights(body: string, terms: string[]): SearchHighlight[];

export const clientVersionGate: MiddlewareHandler<AppEnv>;   // UA "masil/x.y.z" < MIN_CLIENT_VERSION -> 426; non-masil UA passes
```

### 4.14 `src/services/*` (요지)

```ts
// membership.ts
/** SQL fragment over space_invites `t`: its creator is still owner·admin of its space (api.md 3.5). Preview, listing and joinSpace all apply it. */
export function linkIssuerCanInvite(t: string): string;
/** Via a link: pre-check + conditional UPDATE both require the creator to still be owner·admin (else 410 revoked); via a direct invite: the inviter still a member (else 404). */
export function joinSpace(rt: Runtime, spaceId: string, userId: string, via: { inviteCode?: string; directInviteId?: string }): Promise<SpaceDetail>;
/** One batch for every reason: delete space_members row + this user's channel_members rows in all private channels of the space + cancel this user's pending direct invites to the space + revoke their invite links (+ counters). */
export function removeFromSpace(rt: Runtime, spaceId: string, userId: string, actorId: string, reason: "left" | "kicked" | "banned", banReason?: string): Promise<void>;
export function addChannelMembers(rt: Runtime, channelId: string, actorId: string, userIds: string[]): Promise<Channel>;
export function removeChannelMember(rt: Runtime, channelId: string, actorId: string, userId: string): Promise<void>;

// messages.ts
export interface CreateMessageInput { channelId: string; authorId: string; kind: "text" | "me" | "roll" | "poll"; body: string; clientNonce: string; threadRootId?: string | null; poll?: PollInput }
export function createMessage(rt: Runtime, input: CreateMessageInput): Promise<{ message: Message; created: boolean }>;
export function editMessage(rt: Runtime, messageId: string, actorId: string, body: string): Promise<Message>;
export function deleteMessage(rt: Runtime, messageId: string, actorId: string, opts?: { asAdmin?: boolean }): Promise<void>;
export function postSystemMessage(rt: Runtime, channelId: string, actorId: string, system: SystemEvent): Promise<void>;

// posts.ts
export interface CreatePostInput { authorId: string; body: string; clientNonce: string; mediaUploadIds?: string[]; replyToId?: string | null; quoteOfId?: string | null; poll?: PollInput | null }
export function createPost(rt: Runtime, input: CreatePostInput): Promise<{ post: Post; created: boolean }>;
export function deletePost(rt: Runtime, postId: string, actorId: string, opts?: { asAdmin?: boolean }): Promise<void>;

// relations.ts
export function applyBlock(rt: Runtime, blockerId: string, blockedId: string): Promise<void>;     // api.md 3.2 cleanup batch
export function removeBlock(rt: Runtime, blockerId: string, blockedId: string): Promise<void>;
export function makeFriends(rt: Runtime, a: string, b: string, requestId: string): Promise<void>;
export function unfriend(rt: Runtime, a: string, b: string, opts?: { silentTo?: string }): Promise<void>;
export function follow(rt: Runtime, followerId: string, followeeId: string): Promise<"active" | "pending">;
export function approveAllPendingFollows(rt: Runtime, userId: string): Promise<void>;

// polls.ts
export function pollInsertStmts(db: D1Database, pollId: string, creatorId: string, ctx: { type: "message" | "post"; id: string }, input: PollInput, nowMs: number): D1PreparedStatement[];
export function vote(rt: Runtime, pollId: string, userId: string, optionIdxs: number[] | null): Promise<Poll>;  // null = retract
export function closePoll(rt: Runtime, pollId: string, userId: string): Promise<Poll>;

// content.ts
export function removeContent(rt: Runtime, adminId: string, target: { type: "message" | "post" | "story" | "guestbook_entry" | "upload" | "space"; id: string }, reason: string, reportId?: string): Promise<AdminAction>;

// users.ts
export function revokeSessions(rt: Runtime, userId: string, sessionIds: string[] | "all", reason: "logout" | "revoked" | "password_changed" | "suspended", exceptSessionId?: string): Promise<void>; // D1 then UserHub
export function suspendUser(rt: Runtime, adminId: string, userId: string, reason: string, durationSec: number | null, reportId?: string): Promise<AdminUserView>;
export function unsuspendUser(rt: Runtime, adminId: string, userId: string): Promise<AdminUserView>;
```

## 5. 앱 조립·라우트 모듈 규칙

`src/app.ts`:
```ts
export function createApp(): Hono<AppEnv> {
  const app = new Hono<AppEnv>();
  app.use("*", requestId);            // sets c.var.requestId + X-Request-Id header
  app.use("*", stripInternalHeaders); // delete X-Masil-* from incoming requests
  app.use("*", clientVersionGate);
  app.use("*", jsonBodyLimit);        // 64KB, counted on received bytes (lib/body.ts readBodyCapped; Content-Length is
                                      // not trusted, so not hono/body-limit); skipped for POST /v1/uploads (5MB guard inside route);
                                      // GET/HEAD: any body is dropped unread (never rebuilt into a Request -> no 500 on `Content-Length: 0`)
  app.use("*", jsonParseCheck);       // JSON-typed body that does not parse -> 400 invalid_json; reads <= 64KB itself; skips uploads
  app.onError(onError);
  app.notFound(notFoundHandler);
  app.route("/v1", metaRoutes);
  app.route("/v1", authRoutes);
  app.route("/v1", meRoutes);
  // ... every routes/*.ts in the order of section 2
  return app;
}
```

라우트 모듈 형식(예: `src/routes/blocks.ts`):
```ts
import { Hono } from "hono";
import type { AppEnv } from "../env";
export const blockRoutes = new Hono<AppEnv>();
blockRoutes.use("/blocks/*", requireAuth);
blockRoutes.use("/blocks", requireAuth);
blockRoutes.get("/blocks", async (c) => { /* ... */ return c.json({ items }); });
blockRoutes.put("/blocks/:user", limit("RL_SOCIAL"), async (c) => { /* ... */ return c.body(null, 204); });
```
- 각 모듈은 `<name>Routes` 하나만 export. 인증이 필요한 경로마다 `requireAuth` 를 명시(meta·signup·login 만 예외).
- 응답 JSON 은 반드시 `serialize.ts` 함수로 만든다(필드 누락 방지). 성공 코드는 api.md 그대로(`201`, `204` 등).
- 경로 매개변수 이름: `:user`, `:space_id`, `:channel_id`, `:message_id`, `:post_id`, `:story_id`, `:poll_id`, `:entry_id`, `:report_id`, `:request_id`, `:invite_id`, `:code`, `:session_id`, `:upload_id`, `:tag`, `:emoji`.

`src/index.ts`:
```ts
import { createApp } from "./app";
import { handleScheduled } from "./cron";
export { UserHub } from "./do/user-hub";
export { TopicHub } from "./do/topic-hub";
const app = createApp();
export default {
  fetch: app.fetch,
  scheduled: handleScheduled,
} satisfies ExportedHandler<Env>;
```

`src/cron/index.ts`: `export async function handleScheduled(controller: ScheduledController, env: Env, ctx: ExecutionContext): Promise<void>` — `controller.cron === "*/5 * * * *"` → `recomputeTrends(env)`, `"17 * * * *"` → `runCleanup(env)`.

## 6. `server/wrangler.jsonc`

```jsonc
{
  "$schema": "node_modules/wrangler/config-schema.json",
  "name": "masil",
  "main": "src/index.ts",
  "compatibility_date": "2026-11-01",
  "observability": { "enabled": true },

  "d1_databases": [
    {
      "binding": "DB",
      "database_name": "masil",
      // placeholder for local dev; replaced with the real id only after the owner approves creating the remote DB
      "database_id": "00000000-0000-0000-0000-000000000000",
      "migrations_dir": "migrations"
    }
  ],

  "r2_buckets": [
    { "binding": "MEDIA", "bucket_name": "masil-media" }
  ],

  "durable_objects": {
    "bindings": [
      { "name": "USER_HUB", "class_name": "UserHub" },
      { "name": "TOPIC_HUB", "class_name": "TopicHub" }
    ]
  },
  "migrations": [
    { "tag": "v1", "new_sqlite_classes": ["UserHub", "TopicHub"] }
  ],

  // Workers Rate Limiting binding: period must be 10 or 60; namespace_id is an account-unique integer string.
  "ratelimits": [
    { "name": "RL_SIGNUP",  "namespace_id": "7101", "simple": { "limit": 5,   "period": 60 } },
    { "name": "RL_LOGIN",   "namespace_id": "7102", "simple": { "limit": 10,  "period": 60 } },
    { "name": "RL_MESSAGE", "namespace_id": "7103", "simple": { "limit": 20,  "period": 10 } },
    { "name": "RL_POST",    "namespace_id": "7104", "simple": { "limit": 10,  "period": 60 } },
    { "name": "RL_REACT",   "namespace_id": "7105", "simple": { "limit": 30,  "period": 10 } },
    { "name": "RL_UPLOAD",  "namespace_id": "7106", "simple": { "limit": 20,  "period": 60 } },
    { "name": "RL_SOCIAL",  "namespace_id": "7107", "simple": { "limit": 30,  "period": 60 } },
    { "name": "RL_SEARCH",  "namespace_id": "7108", "simple": { "limit": 30,  "period": 60 } },
    { "name": "RL_WS",      "namespace_id": "7109", "simple": { "limit": 30,  "period": 60 } },
    { "name": "RL_API",     "namespace_id": "7110", "simple": { "limit": 600, "period": 60 } }
  ],

  "triggers": { "crons": ["*/5 * * * *", "17 * * * *"] },

  "vars": {
    "APP_ENV": "development",
    "MIN_CLIENT_VERSION": "0.1.0"
  }
}
```

- 비밀(secret)은 현재 없다. 생기면 `.dev.vars`(gitignore) / `wrangler secret put`(오너 승인) 으로만.
- 바인딩을 바꾸면 `npx wrangler types` 로 `worker-configuration.d.ts` 를 다시 만든다.

## 7. 테스트 (`@cloudflare/vitest-pool-workers` 0.23.0 + vitest 4.1.12)

### 7.1 설정 파일

`server/vitest.config.ts`:
```ts
import path from "node:path";
import { defineConfig } from "vitest/config";
import { cloudflareTest, readD1Migrations } from "@cloudflare/vitest-pool-workers";

export default defineConfig({
  plugins: [
    cloudflareTest(async () => {
      const migrations = await readD1Migrations(path.join(__dirname, "migrations"));
      return {
        wrangler: { configPath: "./wrangler.jsonc" },
        miniflare: {
          bindings: { TEST_MIGRATIONS: migrations, APP_ENV: "test" },
        },
      };
    }),
  ],
  test: {
    setupFiles: ["./test/apply-migrations.ts"],
  },
});
```

`server/test/apply-migrations.ts`:
```ts
import { applyD1Migrations, env } from "cloudflare:test";
await applyD1Migrations(env.DB, env.TEST_MIGRATIONS);
```

`server/test/env.d.ts`:
```ts
import type { D1Migration } from "cloudflare:test";
declare global {
  namespace Cloudflare {
    interface Env { TEST_MIGRATIONS: D1Migration[] }
  }
}
export {};
```

`server/test/tsconfig.json`: `"extends": "../tsconfig.json"`, `"compilerOptions": { "types": ["@cloudflare/vitest-pool-workers/types"] }`, `"include": ["./**/*.ts", "../worker-configuration.d.ts", "../src/**/*.ts"]`.

### 7.2 `test/helpers.ts` (모든 테스트가 쓰는 시그니처)

```ts
export const BASE: "http://masil.test";
export function uniqueHandle(prefix?: string): string;      // e.g. "t_k3j9x2" (<=20 chars)
export function uniqueIp(): string;                          // fresh "10.x.y.z" per call -> sidesteps RL_SIGNUP/RL_LOGIN in tests
export interface TestUser { token: string; user: Me; handle: string; password: string }
export function signup(opts?: { handle?: string; password?: string; displayName?: string }): Promise<TestUser>;
export function login(handle: string, password: string): Promise<{ token: string }>;
export interface ApiClient {
  get<T = unknown>(path: string): Promise<{ status: number; body: T; headers: Headers }>;
  post<T = unknown>(path: string, json?: unknown): Promise<{ status: number; body: T; headers: Headers }>;
  put<T = unknown>(path: string, json?: unknown): Promise<{ status: number; body: T; headers: Headers }>;
  patch<T = unknown>(path: string, json?: unknown): Promise<{ status: number; body: T; headers: Headers }>;
  del<T = unknown>(path: string): Promise<{ status: number; body: T; headers: Headers }>;
  raw(path: string, init: RequestInit): Promise<Response>;
}
export function api(token?: string): ApiClient;              // sets Authorization, User-Agent "masil/0.1.0 (test; test)", unique CF-Connecting-IP
export interface WsClient {
  ws: WebSocket;
  frames: Array<{ type: string; seq: number | null; data: any }>;
  send(frame: object | string): void;
  next(type: string, pred?: (data: any) => boolean, timeoutMs?: number): Promise<{ type: string; seq: number | null; data: any }>;
  close(code?: number): void;
}
/** Opens /v1/ws via fetch with Upgrade header, accepts, waits for hello, sends resume, waits for ready. */
export function connectWs(token: string, opts?: { mode?: "full" | "notify"; deviceId?: string; resume?: { epoch: string | null; last_seq: number | null } }): Promise<WsClient>;
export function makeFriends(a: TestUser, b: TestUser): Promise<void>;
export function makeSpace(owner: TestUser, members?: TestUser[]): Promise<{ space: SpaceDetail; generalChannelId: string }>;
```
- 요청은 `SELF.fetch(BASE + path, init)` (0.23 에서 deprecated 표시가 있으나 동작) 또는 `import { exports } from "cloudflare:workers"; exports.default.fetch(...)` 중 하나로 helpers 안에서만 구현한다.
- WebSocket 테스트: `const res = await SELF.fetch(BASE + "/v1/ws?mode=full&device_id=...", { headers: { Upgrade: "websocket", Authorization: "Bearer ..." } }); const ws = res.webSocket!; ws.accept();`
- DO 내부 검증: `runInDurableObject(env.USER_HUB.getByName(id), (inst, state) => ...)`, 알람: `runDurableObjectAlarm(stub)`.
- Cron: `createScheduledController({ cron: "*/5 * * * *", scheduledTime: Date.now() })` + `handleScheduled(controller, env, createExecutionContext())`.
- 테스트는 **파일 간 저장소 격리에 의존하지 않는다**: 매 테스트가 `uniqueHandle()` 로 새 사용자를 만들고 그 사용자 범위만 검증한다.
- 속도 제한 테스트(`ratelimit.test.ts`)만 같은 IP·사용자로 한도를 넘겨 `429` 와 `Retry-After` 를 확인한다. 나머지는 `uniqueIp()` 와 새 사용자로 한도에 걸리지 않게 한다(한 사용자로 10초에 20개 넘는 메시지를 보내지 않는다).

### 7.3 필수 테스트 범위(요약)

- auth: 가입·로그인·해시 형식(`pbkdf2_sha256$100000$`)·틀린 비밀번호·로그아웃 후 401·비밀번호 변경 시 다른 세션 폐기·정지 계정 403.
- permissions: DM 허용 표(친구/같은 방/`friends_only`/차단) 전 조합, 1:1 DM 읽기 전용 전환, 그룹 DM 10명 한도.
- messages/threads: nonce 멱등, `before/after/around` 경계, 스레드 카운트, 멘션·`@here`, 수정·삭제 권한(작성자/방 admin/타인), 고정 권한.
- realtime: hello→resume→ready, 같은 방 두 사용자 간 `message.created` 수신, 재접속 `resume` 재전송, epoch 변경 시 `resync_required`, **close 처리기 없이 워터마크가 지난 뒤 재접속하면 새 epoch**(D1 `online_until` 을 과거로 돌려 재현), 데몬만 떠 있는 사용자는 `message.created` 를 받지 않고 `alert` 는 받음, `device_acks` 모드별 분리, notify 모드 필터, `device.tui_state`, typing 휘발(`seq:null`), ping 자동 응답, 집중 모드 alert suppression 과 `focus.ended` 요약.
- 가시성: 비공개 계정·차단 관계 시청자에게 `post.reply_created` 가 안 감, 원글 작성자에게 차단당한 팔로워에게 리포스트 `timeline.item_created` 가 안 감, 비공개 채널 비멤버·차단 관계 `@handle` 이 `mentions[]`·알림을 만들지 않음, 방을 나간 사람이 그 방 비공개 채널 이벤트를 안 받음.
- sns: 비공개 계정 열람·승인, 차단 양방향 숨김, 리포스트·인용 금지(비공개), 타임라인 `since`/`gap`, 해시태그 정규화(한글), 트렌드 cron, 스토리 만료 cleanup.
- search: `0002_search.sql` 적용 + 한글 3글자 FTS + 2글자 LIKE 폴백 + 접근 범위.
- uploads: 매직 넘버 판별, 5MB 초과 413, 형식 불일치 415, 미디어 열람 권한.
- body-limits: 본문 상한(JSON 64KB·업로드 5MB)은 `Content-Length` 가 아니라 실제로 받은 바이트로 판정 — 위조된 작은 `Content-Length`·chunked 거대 본문 → 413, 상한 직후에서 읽기 중단(전체 버퍼링 없음). GET/HEAD 에 `Content-Length: 0`·`Transfer-Encoding`·거대 본문이 붙어 와도 본문은 읽지 않고 버리며 헤더가 없을 때와 같은 상태 코드(500 금지).
- admin: 신고→강제 삭제(R2 삭제 확인)→신고 자동 처리, 정지 시 소켓 4003.

## 8. 로컬 개발 명령

```
cd server
npm install
npx wrangler types                                   # worker-configuration.d.ts
npx wrangler d1 migrations apply DB --local          # local D1 only
npx wrangler dev --test-scheduled                    # http://localhost:8787 (local D1/R2/DO/ratelimit simulation)
curl "http://localhost:8787/__scheduled?cron=*/5+*+*+*+*"   # trigger the trends cron locally (spaces as '+')
curl "http://localhost:8787/__scheduled?cron=17+*+*+*+*"    # trigger cleanup
npm test                                             # vitest run
npm run typecheck                                    # tsc --noEmit (src) + tsc --noEmit -p test (test files); CI runs the same
```
- 클라이언트 연결: `MASIL_SERVER=http://localhost:8787 masil`. 버전을 넣지 않은 개발 빌드(`version.Version = "0.0.0-dev"`, CA 4.1)는 `MIN_CLIENT_VERSION`(0.1.0) 미만이라 `426 upgrade_required` 를 받는다 — 개발 빌드는 `go run -ldflags "-X github.com/patrache/masil/internal/version.Version=0.1.0-dev" ./cmd/masil` 처럼 버전을 넣어 실행한다(서버 쪽 게이트·기본값은 그대로).
- 원격 명령(`--remote`, `wrangler deploy`, `wrangler d1 create`, `wrangler r2 bucket create`)은 오너 승인 전 실행 금지.
