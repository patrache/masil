# masil 서버 코어 (plan 01) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `server/` 아래에 Cloudflare Worker(Hono) + Durable Object(UserHub·TopicHub) + D1 스키마를 세우고, 계정·세션, 실시간 소켓(핸드셰이크·재개·하트비트·팬아웃·접속 상태·입력 중·집중 모드), 친구·차단, DM·그룹 DM, 방·역할·초대, 채널, 메시지(전송·수정·삭제·고정·리액션·스레드·읽음·안 읽음·멘션·@here), 메시지 검색(FTS), 글 검색·해시태그 검색(`routes/search.ts` 전체 — 글 직렬화 `serializePosts` 포함)까지 로컬 vitest 로 검증된 상태로 만든다.

**Architecture:** 라우트(`src/routes/*.ts`)는 검증 → 권한(`lib/permissions.ts`) → 서비스(`src/services/*.ts`)의 단일 `db.batch` 커밋 → `publishEvent` 순으로만 동작한다. 실시간은 사용자별 `UserHub` DO 가 소켓·seq 로그·접속 상태·집중 모드를 들고, 발행자는 D1 `user_presence` 의 모드별 생존 기한으로 온라인 수신자만 골라 RPC 로 넘긴다. 글 카운터처럼 "보고 있는 사람"이 청중인 이벤트는 `TopicHub` DO 가 임대 목록으로 팬아웃한다.

**Tech Stack:** TypeScript 7.0.2, hono 4.13.16, zod 4.6.7, @hono/zod-validator 0.9.1, wrangler 4.157.0, @cloudflare/vitest-pool-workers 0.23.0 + vitest 4.1.12, D1(SQLite FTS5 trigram), R2, Durable Objects(SQLite storage, WebSocket Hibernation API), Workers Rate Limiting 바인딩.

**Spec:** `docs/superpowers/specs/2026-11-05-masil-design.md` (최우선). 확정 계약: `docs/contract/api.md`, `docs/contract/events.md`, `docs/contract/data-model.md`, `docs/contract/server-architecture.md`. 공통 브리프: `docs/superpowers/plans/00-brief.md`. 이 계획은 TUI 를 만들지 않으므로 골든 스냅샷 시험 대상 작업이 없다.

## Global Constraints

- 금지: `wrangler deploy`, `wrangler d1 create`, `wrangler r2 bucket create`, `--remote` 붙은 모든 명령 — **오너 건별 승인 후**에만. 이 계획의 모든 명령은 로컬(`vitest`, `wrangler dev`, `wrangler types`, `--local`)이다.
- 비밀(토큰·키)을 코드·테스트·문서에 하드코딩 금지. 현재 필요한 비밀은 없다(`.dev.vars.example` 은 형식 안내만).
- 커밋 단계는 실행 세션이 커밋 권한을 받았을 때만 수행하고, `git push` 는 하지 않는다. 커밋 메시지는 영어.
- 버전 고정(정확히): hono 4.13.16, zod 4.6.7, @hono/zod-validator 0.9.1, wrangler 4.157.0, @cloudflare/vitest-pool-workers 0.23.0, vitest·@vitest/runner·@vitest/snapshot 4.1.12(pool-workers peer `^4.1.0`), typescript 7.0.2, @cloudflare/workers-types 5.20261104.1. 외부 유틸 패키지(ULID 등) 추가 금지.
- `compatibility_date: "2026-11-01"`. pool-workers 0.23.0 이 내장한 miniflare(5.20260815) 는 이 날짜보다 오래돼 "Falling back to …" 경고를 낼 수 있다 — 경고는 무시해도 되고, `web_socket_auto_reply_to_close`(2026-04-07) 는 그 전 날짜라 영향 없다.
- 파일 경로·export 이름·시그니처는 `server-architecture.md` 2·4·5장 그대로. 바꾸려면 계약 문서를 먼저 고친다(오너 승인). 라우트끼리 import 금지, 공유 로직은 `lib/`·`services/`.
- 모든 사용자 입력 문자열: NFC + trim 후 저장, 길이는 코드 포인트 수(`[...s].length`). handle 은 소문자 `^[a-z0-9_]{3,20}$`, 예약어 `me, admin, masil, here, system, root, support`.
- D1: 여러 테이블 쓰기는 하나의 `db.batch`; 가변 ID 목록은 `jsonList()` 단일 파라미터(`IN (SELECT value FROM json_each(?))`); `undefined` → `null`; `last_message_id`·`last_read_message_id` 는 "더 클 때만" 조건부 UPDATE; 카운터 감소는 `MAX(x - 1, 0)`.
- 발행: `db.batch` 커밋 **뒤에만** `publishEvent`; `env.APP_ENV === "test"` 면 팬아웃을 끝까지 await, 아니면 `waitUntil`. 팬아웃 실패는 `console.error` 만.
- **삭제 이벤트의 청중**: `{space}`·`{channel}` 청중은 삭제된 방·채널을 걸러 낸다(Task 8). 그래서 대상 자신을 소프트 삭제하는 핸들러(`DELETE /spaces/:id` → `space.deleted`, `DELETE /channels/:id` → `channel.deleted{reason:"deleted"}`)는 **batch 전에** 그 시점 청중의 사용자 ID 전부(온라인 여부 무관 — 공개 text·방 = `space_members`, 비공개 text = `channel_members` ∩ `space_members`)를 읽어 두고, 커밋 뒤 `{users: ids}` 로 발행한다(`deliverToUsers` 가 온라인 필터를 다시 건다). `{space}`/`{channel}` 청중으로 발행하면 아무에게도 가지 않는다.
- **`requireAuth` 는 라우트마다 인자로 단다 — `.use(prefix, requireAuth)` 금지.** Hono 의 `app.route()` 는 하위 앱의 `.use(pattern, mw)` 를 부모에 `ALL` 메서드 라우트로 복사하므로, 접두사를 같이 쓰는 모듈(`/auth`, `/me`(me·presence·plan 02 follows), `/users`(users·plan 02 follows·stories·guestbook), `/channels`(channels·messages), `/messages`, `/spaces`(spaces·invites), `/hashtags`(search·plan 02 timelines)) 중 하나가 `.use('/channels/*', requireAuth)` 를 걸면 다른 모듈의 같은 접두사 요청에서도 미들웨어가 한 번 더 돌아 `RL_API` 를 두 번 센다. `authRoutes.use('/auth/*', requireAuth)` 는 가입·로그인까지 막는다. 이 계획의 라우트 모듈은 접두사 공유 여부와 무관하게 전부 `app.post("/channels/:channel_id/messages", requireAuth, limit("RL_MESSAGE"), vJson(...), handler)` 형태로 쓴다. 앱 전역 미들웨어(`requestId` 등, Task 5)는 `app.use("*", …)` 로만 건다. Task 6 의 `app wiring` 테스트가 이를 고정한다.
- 응답 JSON 은 `lib/serialize.ts` 함수로만 만든다. 응답 필드는 항상 존재(없으면 `null`, 빈 배열 `[]`), 시각은 `iso(ms)`(`2026-11-05T02:03:04.567Z`).
- 테스트 코드 조각은 import 줄을 대부분 생략했다. 각 파일은 `vitest`(globals 미사용), `cloudflare:test`, `../helpers`, 대상 모듈에서 명시적으로 import 한다. "지역 픽스처"(예: `friend`, `blockSql`, `dmPair`, `post`, `settle`, `waitClose`)는 그 테스트 파일 안의 작은 함수이며 `helpers.ts`(계약 7.2 시그니처 고정)에 넣지 않는다. 관계 행을 SQL 로 심는 픽스처는 서비스가 하는 카운터 갱신(`follower_count` 등)도 같이 한다.
- 테스트: 파일 간 저장소 격리에 기대지 않는다 — 매 테스트가 `uniqueHandle()` 새 사용자로 자기 범위만 검증. `ratelimit.test.ts` 외에는 `uniqueIp()`·새 사용자로 한도를 피한다(한 사용자가 10초에 20개 넘게 보내지 않는다).
- **소유 경계(plan 02 와의 약속):** 이 계획은 `types/*` 전부, `lib/*` 중 `uploads.ts`(여기선 `r2Key`·`removeUploads` 만)·`search.ts`(메시지·글 둘 다) 포함 전부, `services/membership.ts`·`messages.ts`·`relations.ts`(`follow` 제외)·`users.ts`(`revokeSessions` 만)·`polls.ts`(`pollInsertStmts` 만), `routes/` 중 meta·auth·me·presence·users(`/users/{user}`·`/users`·`/users/search` 만)·friends·blocks·dms·spaces·invites·channels·messages·search(**전부**: `/search/messages`·`/search/posts`·`/hashtags/search`)·ws, `do/*` 를 만들고, `cron/` 은 만들지 않는다. plan 02 는 같은 파일에 나머지 export·경로를 **덧붙이고**, `app.ts` 에 자기 라우트를 마운트하며, `index.ts` 에 `scheduled: handleScheduled` 를 추가한다.
  - **이 계획이 끝내 두는 것(plan 02 는 다시 만들거나 다시 등록하지 않고, plan 02 경계 규칙 (b) 에 따라 자기 테스트로 고정만 한다):** `PUT|DELETE /me/status`(Task 16, `routes/me.ts`), `POST|GET|DELETE /me/focus`(Task 12, `routes/presence.ts`), `approveAllPendingFollows`(Task 18, `services/relations.ts`), `serializeMedia`·`serializePosts`(Task 29, `lib/serialize.ts`), `GET /search/posts`·`GET /hashtags/search`(Task 29, `routes/search.ts`). plan 02 Task 13 의 status/focus 부분과 Task 3 의 `approveAllPendingFollows` 부분은 이 구현이 그 테스트를 통과하면 Step 3 을 건너뛴다(plan 02 Task 13 Step 2 의 "1부가 이미 구현했다면 … 건너뜀" 과 같은 뜻). 같은 경로를 두 번 등록하거나 같은 export 를 두 번 만들면 안 된다 — 이 목록의 동작 세부(검증·응답·알림 문구)는 plan 02 의 해당 테스트와 맞춰 두었다.
- `serialize.ts` 에는 계약 4.9 함수 외에 이 계획이 덧붙이는 export 가 있다(같은 규칙: 목록은 `json_each` 일괄 로드, N+1 금지): `serializeFriendRequests`, `serializeFriends`, `serializeSpaceMembers`, `serializeInvites`, `serializeInvitePreview`, `serializeDirectInvites`, `loadUnreadStates`, `loadChannelRefs`. 시그니처는 각 작업의 Interfaces 에 있다. 계약 4.9 함수 중 이 계획이 만드는 것: `toUserSummary`, `loadUserSummaries`, `serializeMe`, `serializeProfile`, `serializeSession`, `serializeChannels`, `serializeSpace`, `serializeSpaceDetail`, `serializeMessages`, `serializePolls`, `serializeNotifications`, `serializeMedia`, `serializePosts`. 나머지(`serializeUpload`, `serializeTimelineItems`, `serializeStories`, `serializeGuestbook`, `serializeReports`)는 plan 02 가 덧붙인다.
- **알림 입력 표(`NotifyInput`)는 Task 15 에 있다.** 이 계획이 만드는 알림·alert(`friend_request`, `friend_accepted`, `space_invite`, `message_mention`, `thread_reply`, `follow_accepted`, alert 전용 `dm`·`channel_message`)는 그 표의 subject·context·preview·dedupeKey·alert 문구를 그대로 쓴다.

## Review Focus

1. **NFD 로 들어온 한글**(macOS 붙여넣기 등): `개발` 을 NFD 로 보낸 채널 이름·표시 이름·검색어는 NFC 와 같은 값으로 취급돼야 한다 — 같은 이름 채널 생성은 `409 already_exists`, 검색은 같은 결과. (Task 23, Task 28, Task 29 에 시험)
2. **청중이 100명을 넘는 팬아웃**: 방 멤버 120명이 모두 온라인일 때 `resolveOnlineAudience`(→ 리액션·메시지 등 `{channel}` 청중 팬아웃 전부)·`@here` 확장·멘션 알림 행 생성이 "too many SQL variables"(D1 바인딩 100개 상한) 없이 120명 전원을 다뤄야 한다(`jsonList` 단일 파라미터, `INSERT … SELECT … FROM json_each(?)`). (Task 8 `120 online space members resolve…` — 청중 해석, Task 24 `@here with 120 online members…` — `message_mentions` 119행·알림함 119행)
3. **망가진 클라이언트 프레임**: 바이너리 프레임·JSON 아님·8KB 초과·`hello` 전 `typing`·모르는 `type` 에 대해 DO 가 죽지 않고 계약대로 `4000`/`1009`/`reply{unknown_type}` 로 답해야 한다. (Task 9 에 시험)
4. **ZWJ·피부색 이모지 리액션**: `👍🏽`, `👨‍👩‍👧` 처럼 여러 코드 포인트인 그래핌 1개는 허용되고, `👍👍`(그래핌 2개)·`ab`·32바이트 초과는 `400` 이어야 한다. (Task 26 에 시험)
5. **읽음 포인터·마지막 메시지 역행**: 늦게 도착한 오래된 `PUT /read` 나 동시 전송이 `last_read_message_id`/`last_message_id` 를 뒤로 돌리면 안 되고, 다른 채널 메시지 ID·스레드 답글 ID 로 읽음 처리하면 `400` 이어야 한다. (Task 27 에 시험)

---

## 파일 지도 (이 계획이 만드는 것)

```
server/package.json  tsconfig.json  wrangler.jsonc  worker-configuration.d.ts  vitest.config.ts  .dev.vars.example
server/migrations/0001_init.sql  0002_search.sql
server/src/index.ts  app.ts  env.ts
server/src/types/api.ts  events.ts  rows.ts
server/src/lib/ids.ts time.ts crypto.ts text.ts db.ts errors.ts validate.ts version.ts auth.ts ratelimit.ts
              permissions.ts serialize.ts realtime.ts notify.ts uploads.ts search.ts
server/src/services/users.ts relations.ts membership.ts messages.ts polls.ts
server/src/routes/meta.ts auth.ts me.ts presence.ts users.ts friends.ts blocks.ts dms.ts spaces.ts invites.ts
                 channels.ts messages.ts search.ts ws.ts
server/src/do/protocol.ts user-hub.ts topic-hub.ts
server/test/tsconfig.json env.d.ts apply-migrations.ts helpers.ts
server/test/unit/ids.test.ts crypto.test.ts text.test.ts db-cursor.test.ts search-query.test.ts
server/test/api/auth.test.ts me.test.ts users.test.ts friends.test.ts blocks.test.ts dms.test.ts permissions.test.ts
                spaces.test.ts invites.test.ts channels.test.ts messages.test.ts threads.test.ts reactions-pins.test.ts
                search.test.ts ratelimit.test.ts
server/test/realtime/ws-handshake.test.ts ws-resume.test.ts fanout.test.ts presence-focus.test.ts topic-hub.test.ts notify-mode.test.ts
```

`routes/search.ts` 는 `/search/messages`(Task 28)와 `/search/posts`·`/hashtags/search`(Task 29) 를 모두 담는다.

모든 명령은 `server/` 에서 실행한다(`cd /Users/sam/Develop/test/terminal-sns/server`).

---

### Task 1: 서버 패키지 뼈대·바인딩·마이그레이션

**Files:**
- Create: `server/package.json`, `server/tsconfig.json`, `server/wrangler.jsonc`, `server/vitest.config.ts`, `server/.dev.vars.example`, `server/worker-configuration.d.ts`(생성물)
- Create: `server/migrations/0001_init.sql`, `server/migrations/0002_search.sql`
- Create: `server/src/index.ts`, `server/src/do/user-hub.ts`, `server/src/do/topic-hub.ts` (빈 클래스)
- Create: `server/test/tsconfig.json`, `server/test/env.d.ts`, `server/test/apply-migrations.ts`
- Test: `server/test/api/search.test.ts`

**Interfaces:**
- Consumes: 없음
- Produces: 바인딩 `DB`, `MEDIA`, `USER_HUB`, `TOPIC_HUB`, `RL_SIGNUP|RL_LOGIN|RL_MESSAGE|RL_POST|RL_REACT|RL_UPLOAD|RL_SOCIAL|RL_SEARCH|RL_WS|RL_API`, `APP_ENV`, `MIN_CLIENT_VERSION` 가 든 `Cloudflare.Env`; 테스트 바인딩 `TEST_MIGRATIONS`; `export class UserHub extends DurableObject<Cloudflare.Env> {}`, `export class TopicHub extends DurableObject<Cloudflare.Env> {}` (이후 작업이 채움)

- [ ] **Step 1: 패키지·설정 파일 작성**

`package.json`: `"name": "masil-server"`, `"private": true`, `"type": "module"`, scripts `{"dev": "wrangler dev --test-scheduled", "test": "vitest run", "types": "wrangler types", "typecheck": "tsc --noEmit && tsc --noEmit -p test"}`, dependencies `hono 4.13.16, zod 4.6.7, @hono/zod-validator 0.9.1`, devDependencies 는 Global Constraints 의 나머지(정확한 버전, `^` 없이).
`tsconfig.json`: `strict: true`, `noEmit: true`, `target/lib: ES2024`, `module: ESNext`, `moduleResolution: Bundler`, `types: []`, `skipLibCheck: true`, include `src/**/*.ts`, `worker-configuration.d.ts`.
`wrangler.jsonc`: `server-architecture.md` 6장 블록을 **글자 그대로**. `vitest.config.ts`·`test/apply-migrations.ts`·`test/env.d.ts`·`test/tsconfig.json`: 같은 문서 7.1 블록 그대로.
`migrations/0001_init.sql`: `data-model.md` 3.1~3.10 의 SQL 을 순서 그대로 이어 붙임. `0002_search.sql`: 4장 SQL 그대로.
`src/index.ts`: 이 단계에선 `export { UserHub } from "./do/user-hub"; export { TopicHub } from "./do/topic-hub"; export default { fetch: () => new Response("not found", { status: 404 }) } satisfies ExportedHandler<Cloudflare.Env>;` (Task 5 가 교체).
`.dev.vars.example`: 주석 한 줄 `# No secrets are required yet. Copy to .dev.vars if one is added.`

- [ ] **Step 2: 의존성 설치·타입 생성**

Run: `npm install && npx wrangler types`
Expected: `worker-configuration.d.ts` 생성, 안에 `USER_HUB: DurableObjectNamespace<import("./src/index").UserHub>` 와 `RL_API: RateLimit` 이 보임.

- [ ] **Step 3: 스키마·FTS 스모크 테스트 작성** (`test/api/search.test.ts` — 계약상 이 파일의 첫 테스트)

```ts
import { env } from "cloudflare:test";
import { describe, it, expect } from "vitest";

describe("schema", () => {
  it("applies 0001 and 0002", async () => {
    const { results } = await env.DB.prepare("SELECT name FROM sqlite_master WHERE type IN ('table') ORDER BY name").all<{ name: string }>();
    const names = results.map((r) => r.name);
    for (const t of ["users", "sessions", "channels", "messages", "message_mentions", "user_presence", "messages_fts", "posts_fts", "admin_actions"]) expect(names).toContain(t);
  });
  it("trigram FTS matches a 3-char Korean substring", async () => {
    const id = "01JTESTFTS" + crypto.randomUUID().replace(/-/g, "").slice(0, 16).toUpperCase();
    await env.DB.batch([
      env.DB.prepare("INSERT INTO users (id, handle, display_name, password_hash, created_at, updated_at) VALUES (?1, ?2, 'x', 'x', 0, 0)").bind(id, "fts_" + id.slice(-8).toLowerCase()),
      env.DB.prepare("INSERT INTO channels (id, kind, created_by, created_at, updated_at) VALUES (?1, 'group_dm', ?1, 0, 0)").bind(id),
      env.DB.prepare("INSERT INTO messages (id, channel_id, author_id, body, created_at) VALUES (?1, ?1, ?1, '오늘 배포는 몇 시?', 0)").bind(id),
    ]);
    const row = await env.DB.prepare("SELECT m.id FROM messages_fts f JOIN messages m ON m.rowid = f.rowid WHERE messages_fts MATCH ?1 AND m.id = ?2").bind('"배포는"', id).first();
    expect(row).not.toBeNull();
  });
});
```

- [ ] **Step 4: 실행**

Run: `npx vitest run test/api/search.test.ts`
Expected: `Test Files  1 passed (1)`, `Tests  2 passed (2)`. 만약 `no such tokenizer: trigram` 이면 `data-model.md` 4장 지시대로 `tokenize='unicode61'` + 접두 질의로 바꾸고 그 문서를 같이 고친 뒤 다시 실행.

- [ ] **Step 5: 타입 검사**

Run: `npm run typecheck`
Expected: 오류 없이 종료(코드 0).

- [ ] **Step 6: Commit**

```bash
git add server/package.json server/package-lock.json server/tsconfig.json server/wrangler.jsonc server/worker-configuration.d.ts server/vitest.config.ts server/.dev.vars.example server/migrations server/src server/test
git commit -m "chore(server): scaffold worker, bindings, D1 schema and vitest pool"
```

---

### Task 2: ID·시각·암호 유틸

**Files:**
- Create: `server/src/lib/ids.ts`, `server/src/lib/time.ts`, `server/src/lib/crypto.ts`
- Test: `server/test/unit/ids.test.ts`, `server/test/unit/crypto.test.ts`

**Interfaces:**
- Consumes: 없음
- Produces (계약 4.5):
  `newId(nowMs?: number): string`, `isId(s: string): boolean`, `idFloor(ms: number): string`, `newInviteCode(): string`;
  `now(): number`, `iso(ms: number): string`, `isoOrNull(ms: number | null | undefined): string | null`;
  `hashPassword(password: string): Promise<string>`, `verifyPassword(password: string, stored: string): Promise<boolean>`, `dummyVerify(): Promise<void>`, `newSessionToken(): string`, `sha256Hex(s: string): Promise<string>`

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/unit/ids.test.ts
import { newId, isId, idFloor, newInviteCode } from "../../src/lib/ids";
import { iso, isoOrNull } from "../../src/lib/time";
it("ULID shape and monotonic within the same ms", () => {
  const ids = Array.from({ length: 1000 }, () => newId(1_700_000_000_000));
  expect(ids.every((s) => /^[0-9A-HJKMNP-TV-Z]{26}$/.test(s) && isId(s))).toBe(true);
  expect([...ids].sort()).toEqual(ids);
  expect(new Set(ids).size).toBe(1000);
});
it("idFloor sorts below every id of that ms and above earlier ms", () => {
  // idFloor is pure; never compare against an id made after a larger ms (newId is monotonic and would keep that larger ms)
  expect(idFloor(1_699_999_999_999) < idFloor(1_700_000_000_000)).toBe(true);
  expect(idFloor(1_700_000_000_000).endsWith("0000000000000000")).toBe(true);
  const ms = Date.now();                 // >= every ms this module has seen (tests never pass future ms to newId)
  const id = newId(ms);
  expect(id.slice(0, 10)).toBe(idFloor(ms).slice(0, 10));
  expect(idFloor(ms) < id && id < idFloor(ms + 1)).toBe(true);
});
it("clock going backwards keeps the last timestamp and increments the random part", () => {
  const a = newId(Date.now());
  const b = newId(Date.now() - 5_000);
  expect(b > a).toBe(true);
  expect(b.slice(0, 10)).toBe(a.slice(0, 10));
});
it("invite code: 8 chars without 0 O o 1 l I", () => {
  for (let i = 0; i < 200; i++) expect(newInviteCode()).toMatch(/^[A-HJ-NP-Za-km-np-z2-9]{8}$/);
});
it("iso is RFC3339 ms Z", () => {
  expect(iso(Date.UTC(2026, 10, 5, 2, 3, 4, 567))).toBe("2026-11-05T02:03:04.567Z");
  expect(isoOrNull(null)).toBeNull();
});
```
```ts
// test/unit/crypto.test.ts
import { hashPassword, verifyPassword, newSessionToken, sha256Hex, dummyVerify } from "../../src/lib/crypto";
it("pbkdf2 format and verify", async () => {
  const h = await hashPassword("correct horse battery");
  expect(h).toMatch(/^pbkdf2_sha256\$100000\$[A-Za-z0-9_-]{22}\$[A-Za-z0-9_-]{43}$/);
  expect(await verifyPassword("correct horse battery", h)).toBe(true);
  expect(await verifyPassword("wrong", h)).toBe(false);
  expect(await verifyPassword("x", "garbage")).toBe(false);
  await expect(dummyVerify()).resolves.toBeUndefined();
});
it("token and sha256", async () => {
  expect(newSessionToken()).toMatch(/^msl_[A-Za-z0-9_-]{43}$/);
  expect(await sha256Hex("abc")).toBe("ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad");
});
```

- [ ] **Step 2: 실패 확인**

Run: `npx vitest run test/unit/ids.test.ts test/unit/crypto.test.ts`
Expected: FAIL — `Failed to resolve import "../../src/lib/ids"`.

- [ ] **Step 3: 구현**

`ids.ts`: Crockford base32(`0123456789ABCDEFGHJKMNPQRSTVWXYZ`), 48비트 시각 + 80비트 난수(`crypto.getRandomValues`). 모듈 변수 `lastMs`(초기 0)·`lastRand`(80비트, `bigint` 권장). `newId(ms = Date.now())`: `ms > lastMs` 면 `lastMs = ms`, 새 난수. 그 밖(같은 ms **또는 시계 역행** `ms < lastMs`)이면 **`lastMs` 를 그대로 두고**(시각부는 `lastMs` — 넘겨받은 작은 `ms` 를 쓰지 않는다) `lastRand + 1`; 80비트가 넘치면 `lastMs + 1` 로 올리고 새 난수. 그래서 같은 isolate 의 ID 는 항상 증가한다(data-model 1.1). 결과: `newId(x)` 의 시각부는 `max(x, 지금까지 본 최대 ms)` — 테스트·코드는 `newId` 에 미래 시각을 넘기지 않는다(모듈 상태가 오염돼 이후 모든 ID 가 미래 시각을 갖게 되고, `idFloor(joined_at)` 같은 기준점 비교가 틀어진다). `idFloor` = 시각 10자 + `"0"×16`. 초대 코드 알파벳 `ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnpqrstuvwxyz23456789`(56자), 거절 샘플링으로 편향 없이.
`crypto.ts`: `crypto.subtle.importKey("raw", …, "PBKDF2")` + `deriveBits({name:"PBKDF2", hash:"SHA-256", salt, iterations:100000}, key, 256)`, 솔트 16바이트, base64url 무패딩. 검증은 저장된 반복 수를 쓰고 `crypto.subtle.timingSafeEqual` 로 비교, 형식 오류는 `false`. `dummyVerify` 는 고정 더미 해시에 대해 `verifyPassword` 1회.

- [ ] **Step 4: 통과 확인**

Run: `npx vitest run test/unit/ids.test.ts test/unit/crypto.test.ts`
Expected: `Tests  7 passed (7)`

- [ ] **Step 5: Commit**

```bash
git add server/src/lib/ids.ts server/src/lib/time.ts server/src/lib/crypto.ts server/test/unit/ids.test.ts server/test/unit/crypto.test.ts
git commit -m "feat(server): ulid, time and password/token crypto helpers"
```

---

### Task 3: 텍스트 유틸 (정규화·멘션·해시태그·주사위·스니펫)

**Files:**
- Create: `server/src/lib/text.ts`
- Test: `server/test/unit/text.test.ts`

**Interfaces:**
- Consumes: 없음
- Produces (계약 4.10): `normalizeText(s: string): string`, `charCount(s: string): number`, `extractMentions(body: string): string[]`, `hasHereMention(body: string): boolean`, `extractHashtags(body: string): string[]`, `parseDice(expr: string): { count: number; sides: number; modifier: number } | null`, `rollDice(spec): RollResult`, `plainSnippet(body: string, maxChars: number): string`. 추가: `isSingleEmoji(s: string, maxBytes: number): boolean` (`Intl.Segmenter` 그래핌 1개 + `\p{Extended_Pictographic}` 또는 국기/키캡 포함 + UTF-8 바이트 ≤ maxBytes) — 리액션(32)·상태 이모지(16)·방 아이콘이 쓴다. `RollResult` 는 Task 5 의 `types/api.ts` 에 정의되므로 여기선 `{ expr: string; rolls: number[]; modifier: number; total: number }` 를 지역 타입으로 두고 Task 5 에서 import 로 바꾼다.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
import * as t from "../../src/lib/text";
it("normalize NFD->NFC and trim; count code points", () => {
  expect(t.normalizeText("  각 ")).toBe("각");
  expect(t.charCount("가😀a")).toBe(3);
});
it("mentions: regex, lowercase, unique, skips code", () => {
  expect(t.extractMentions("@Minji hi @minji a@bob `@code` ```\n@blk\n``` @jun_ho.")).toEqual(["minji", "jun_ho"]);
  expect(t.extractMentions(Array.from({ length: 60 }, (_, i) => `@user${i}`).join(" "))).toHaveLength(50);
  expect(t.extractMentions("@ab @here")).toEqual(["here"]); // 'ab' too short; "here" is filtered by caller (reserved)
});
it("@here detection ignores code", () => {
  expect(t.hasHereMention("ping @here now")).toBe(true);
  expect(t.hasHereMention("`@here`")).toBe(false);
  expect(t.hasHereMention("@heres")).toBe(false);
});
it("hashtags NFC lowercase unique max 10", () => {
  expect(t.extractHashtags("#첫눈 #Go #go a#b")).toEqual(["첫눈", "go"]);
  expect(t.extractHashtags(Array.from({ length: 12 }, (_, i) => `#t${i}`).join(" "))).toHaveLength(10);
});
it("dice", () => {
  expect(t.parseDice("2d6+3")).toEqual({ count: 2, sides: 6, modifier: 3 });
  for (const bad of ["0d6", "21d6", "1d1", "1d1001", "1d6+1001", "d6", "2d6 "]) expect(t.parseDice(bad)).toBeNull();
  const r = t.rollDice({ count: 3, sides: 6, modifier: -1 });
  expect(r.rolls).toHaveLength(3); expect(r.total).toBe(r.rolls.reduce((a, b) => a + b, 0) - 1);
  expect(r.expr).toBe("3d6-1");
});
it("snippet strips markdown and collapses whitespace", () => {
  expect(t.plainSnippet("**굵게**  `code`\n\n[링크](https://x.y)", 100)).toBe("굵게 code 링크");
  expect(t.charCount(t.plainSnippet("가".repeat(300), 200))).toBe(200);
});
it("single emoji", () => {
  for (const ok of ["👍", "👍🏽", "👨‍👩‍👧", "🇰🇷", "❤️"]) expect(t.isSingleEmoji(ok, 32)).toBe(true);
  for (const bad of ["👍👍", "ab", "", "가"]) expect(t.isSingleEmoji(bad, 32)).toBe(false);
});
```

- [ ] **Step 2: 실패 확인**

Run: `npx vitest run test/unit/text.test.ts`
Expected: FAIL — `Failed to resolve import "../../src/lib/text"`.

- [ ] **Step 3: 구현** — 멘션 정규식은 api.md 2.1 의 `(?<![A-Za-z0-9_])@([a-z0-9_]{3,20})(?![a-z0-9_])` 를 `i` 플래그로, 해시태그는 `(?<![\p{L}\p{N}_])#([\p{L}\p{N}_]{1,50})` 를 `u` 플래그로. 코드 제외는 펜스 블록 → 인라인 코드 순서로 같은 길이 공백으로 가린 뒤 매칭. `rollDice` 는 `crypto.getRandomValues` 거절 샘플링. 스니펫 자르기는 코드 포인트 단위.

- [ ] **Step 4: 통과 확인**

Run: `npx vitest run test/unit/text.test.ts`
Expected: `Tests  7 passed (7)`

- [ ] **Step 5: Commit**

```bash
git add server/src/lib/text.ts server/test/unit/text.test.ts
git commit -m "feat(server): text normalization, mentions, hashtags, dice, snippets"
```

---

### Task 4: D1 헬퍼·커서 + 행 타입

**Files:**
- Create: `server/src/lib/db.ts`, `server/src/types/rows.ts`
- Test: `server/test/unit/db-cursor.test.ts`

**Interfaces:**
- Consumes: Task 5 의 `err` 가 아직 없으므로 `decodeCursor`/`parseLimit` 는 Task 5 에서 `err.invalid` 로 바꾼다 — 이 작업에선 `throw new Error("invalid_cursor")` 로 두고 Task 5 Step 3 에서 교체.
- Produces (계약 4.4): `stmt`, `first<T>`, `all<T>`, `run`, `batch`, `jsonList(ids: readonly string[]): string`, `interface Cursor { k: string | number; i: string }`, `encodeCursor(c: Cursor): string`, `decodeCursor(s: string | undefined): Cursor | null`, `parseLimit(raw: string | undefined, def = 30, max = 100): number`, `interface Page<T> { items: T[]; next_cursor: string | null }`.
  `types/rows.ts`: `data-model.md` 3장 테이블마다 컬럼 1:1 인터페이스 — `UserRow, UserSettingsRow, SessionRow, FriendRequestRow, FriendshipRow, BlockRow, FollowRow, SpaceRow, SpaceMemberRow, SpaceBanRow, SpaceInviteRow, SpaceDirectInviteRow, ChannelRow, ChannelMemberRow, ChannelPrefsRow, ChannelReadRow, MessageRow, ThreadParticipantRow, MessageMentionRow, MessageReactionRow, MessagePinRow, UploadRow, PostRow, PostMediaRow, PostLikeRow, RepostRow, PostHashtagRow, TrendRow, StoryRow, StoryViewRow, PollRow, PollOptionRow, PollVoteRow, GuestbookRow, NotificationRow, UserPresenceRow, ReportRow, AdminActionRow`, 그리고 `TimelineRow { item_id: string; type: "post" | "repost"; post_id: string; reposter_id: string | null; at: number }`. INTEGER→`number`, nullable→`| null`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
import { env } from "cloudflare:test";
import { encodeCursor, decodeCursor, parseLimit, jsonList, all, first, run } from "../../src/lib/db";
it("cursor roundtrip and opaque base64url", () => {
  const s = encodeCursor({ k: 1730000000000, i: "01JC8Z3K4M5N6P7Q8R9S0TVWXY" });
  expect(s).toMatch(/^[A-Za-z0-9_-]+$/);
  expect(decodeCursor(s)).toEqual({ k: 1730000000000, i: "01JC8Z3K4M5N6P7Q8R9S0TVWXY" });
  expect(decodeCursor(undefined)).toBeNull();
  expect(() => decodeCursor("%%%")).toThrow();
  expect(() => decodeCursor(btoa('{"k":1}'))).toThrow();
});
it("parseLimit", () => {
  expect(parseLimit(undefined)).toBe(30);
  expect(parseLimit("500")).toBe(100);
  expect(parseLimit("7", 50, 100)).toBe(7);
  expect(() => parseLimit("0")).toThrow(); expect(() => parseLimit("abc")).toThrow();
});
it("jsonList passes >100 ids as one parameter; undefined binds as null", async () => {
  const ids = Array.from({ length: 150 }, (_, i) => `x${i}`);
  const rows = await all<{ value: string }>(env.DB, "SELECT value FROM json_each(?1)", jsonList(ids));
  expect(rows).toHaveLength(150);
  expect(await first<{ v: null }>(env.DB, "SELECT ?1 AS v", undefined)).toEqual({ v: null });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/unit/db-cursor.test.ts` · Expected: FAIL `Failed to resolve import`.
- [ ] **Step 3: 구현** — 파라미터의 `undefined` 를 `null` 로 바꿔 `prepare(sql).bind(...)`. 커서는 `{k, i}` 검증(k 는 string|number, i 는 string) 실패 시 throw. `parseLimit`: 정수 아님·1 미만 → throw, `max` 초과는 `max` 로 자름.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/unit/db-cursor.test.ts` · Expected: `Tests  3 passed (3)`
- [ ] **Step 5: Commit**

```bash
git add server/src/lib/db.ts server/src/types/rows.ts server/test/unit/db-cursor.test.ts
git commit -m "feat(server): d1 helpers, cursors and row types"
```

---

### Task 5: 앱 껍데기 — 타입·오류 봉투·검증·버전 게이트·meta

**Files:**
- Create: `server/src/env.ts`, `server/src/types/api.ts`, `server/src/types/events.ts`, `server/src/lib/errors.ts`, `server/src/lib/validate.ts`, `server/src/lib/version.ts`, `server/src/app.ts`, `server/src/routes/meta.ts`
- Modify: `server/src/index.ts` (계약 5장 형태, 단 `scheduled` 는 plan 02 가 추가), `server/src/lib/db.ts` (throw → `err.invalid`), `server/src/lib/text.ts` (`RollResult` import)
- Create: `server/test/helpers.ts` (이 단계: `BASE`, `uniqueHandle`, `uniqueIp`, `api`)
- Test: `server/test/api/auth.test.ts` (`describe("app shell")`)

**Interfaces:**
- Consumes: Task 4 `parseLimit`, `decodeCursor`
- Produces:
  - `env.ts` (계약 4.1): `type Env = Cloudflare.Env`, `interface AuthContext { userId; sessionId; handle; role: "user" | "admin" }`, `type AppEnv`, `type AppContext`, `interface Runtime { env: Env; waitUntil(p: Promise<unknown>): void }`, `rt(c: AppContext): Runtime`.
  - `types/api.ts`: api.md 2장 모든 interface/type 을 같은 이름·필드로(`UserSummary … AdminAction`, `PresenceStatus`, `SpaceRole`, `NotificationType`, `AlertKind`, `PollInput` 포함).
  - `types/events.ts`: `type ServerEventType` = events.md 8장 서버 이벤트 이름 유니온(`pong` 제외), `interface ServerEventDataMap { "message.created": { message: Message }; … }` 4장 표 그대로, `type ServerEventData<T extends ServerEventType> = ServerEventDataMap[T]`, `type ClientFrameType = "ping" | "resume" | "ack" | "typing" | "idle" | "watch"`.
  - `errors.ts` (계약 4.2): `ErrorCode`, `class ApiError`, `err`, `onError(e, c)`, `notFoundHandler(c)`. 상태 매핑: invalid→400, unauthorized→401(`unauthorized`), suspended→403 `account_suspended` details `{until: iso|null, reason}`, forbidden→403(기본 code `forbidden`), notFound→404, conflict→409(details `{reason}` 있으면), inviteInvalid→410 details `{reason}`, limit→422 details `{what, limit}`, rateLimited→429 details `{limiter, retry_after_sec}` + `Retry-After` 헤더. 그 밖 예외는 `console.error` 후 500 `internal_error`.
  - `validate.ts` (계약 4.3): `vJson`, `vQuery`, `zId`, `zUserRef`, `zNonce`, `zText(min, max)`. 반환 타입은 `MiddlewareHandler<AppEnv, string, { in: …; out: { json: z.output<T> } }>` 처럼 입력 타입을 실어 `c.req.valid("json")` 이 추론되게 한다(계약의 `MiddlewareHandler<AppEnv>` 의 구체화). zod 이슈 → 필드값: 값이 없음 → `"required"`, `too_big`(문자열) → `"too_long"`, `too_small`(문자열) → `"too_short"`, 그 외 `"invalid"`. 필드 키는 경로를 `.` 로 이은 것.
  - `version.ts` (계약 4.13): `clientVersionGate` — UA `^masil/(\d+)\.(\d+)\.(\d+)` 가 `MIN_CLIENT_VERSION` 미만이면 426 `upgrade_required` details `{min_client_version}`.
  - `app.ts` (계약 5장): `createApp(): Hono<AppEnv>` — 미들웨어 `requestId`(ULID, `X-Request-Id`), `stripInternalHeaders`(들어온 `X-Masil-*` 삭제), `clientVersionGate`, `jsonBodyLimit`(`hono/body-limit`, 64KB, `POST /v1/uploads` 제외, 초과 → 413 `payload_too_large`), JSON 파싱 실패 → 400 `invalid_json`. 이 전역 미들웨어는 전부 `app.use("*", …)` 로 건다(`"/v1/*"` 아님 — Task 6 `app wiring` 테스트가 `/v1` 아래 와일드카드 `ALL` 라우트를 잡아낸다). 라우트는 계약 2장 순서로 `app.route("/v1", xxxRoutes)` 로 마운트하며 각 작업이 자기 줄을 추가.
  - `routes/meta.ts`: `metaRoutes` — `GET /v1/meta`(api.md 4장 JSON, `limits` 값 그대로), `GET /v1/health` → `{ok:true}`.
  - `test/helpers.ts` (계약 7.2): `BASE = "http://masil.test"`, `uniqueHandle(prefix = "t"): string`(≤20자), `uniqueIp(): string`, `api(token?: string): ApiClient` — `SELF.fetch` 사용, UA `masil/0.1.0 (test; test)`, 호출마다 새 `CF-Connecting-IP`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
import { api, BASE } from "../helpers";
import { SELF } from "cloudflare:test";
describe("app shell", () => {
  it("meta and health", async () => {
    const r = await api().get<any>("/v1/meta");
    expect(r.status).toBe(200);
    expect(r.body).toMatchObject({ api_version: 1, min_client_version: "0.1.0", limits: { message_max_chars: 4000, group_dm_max_members: 10, reactions_per_message: 20, watch_topics_max: 50 } });
    expect(r.headers.get("X-Request-Id")).toMatch(/^[0-9A-Z]{26}$/);
    expect((await api().get("/v1/health")).body).toEqual({ ok: true });
  });
  it("unknown route -> 404 envelope", async () => {
    const r = await api().get<any>("/v1/nope");
    expect(r.status).toBe(404);
    expect(r.body).toEqual({ error: { code: "not_found", message: expect.any(String), details: {} } });
  });
  it("old client -> 426, non-masil UA passes", async () => {
    const old = await SELF.fetch(BASE + "/v1/meta", { headers: { "User-Agent": "masil/0.0.9 (x; y)" } });
    expect(old.status).toBe(426);
    expect((await old.json<any>()).error.details).toEqual({ min_client_version: "0.1.0" });
    expect((await SELF.fetch(BASE + "/v1/meta", { headers: { "User-Agent": "curl/8" } })).status).toBe(200);
  });
  it("bad JSON -> 400 invalid_json; >64KB -> 413", async () => {
    const bad = await api().raw("/v1/auth/signup", { method: "POST", headers: { "Content-Type": "application/json" }, body: "{" });
    expect(bad.status).toBe(400); expect((await bad.json<any>()).error.code).toBe("invalid_json");
    const big = await api().raw("/v1/auth/signup", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ x: "a".repeat(70_000) }) });
    expect(big.status).toBe(413); expect((await big.json<any>()).error.code).toBe("payload_too_large");
  });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/auth.test.ts` · Expected: FAIL (`/v1/meta` 404 또는 import 실패).
- [ ] **Step 3: 구현** — 위 Produces 대로. `invalid_json` 판정은 라우트 이전 공통 미들웨어에서 `Content-Type: application/json` 이고 본문이 있는 요청을 `c.req.json()` 시도로 확인(실패 → `ApiError(400,"invalid_json")`); 본문 검사 순서는 bodyLimit → JSON 파싱.
- [ ] **Step 4: 통과·타입 확인** — Run: `npx vitest run test/api/auth.test.ts && npm run typecheck` · Expected: `Tests  4 passed (4)`, typecheck 코드 0.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/helpers.ts server/test/api/auth.test.ts
git commit -m "feat(server): app shell, api/event types, error envelope, validation, version gate"
```

---

### Task 6: 가입·로그인·인증 미들웨어·속도 제한

**Files:**
- Create: `server/src/lib/ratelimit.ts`, `server/src/lib/auth.ts`, `server/src/lib/serialize.ts`(사용자·세션 부분), `server/src/routes/auth.ts`
- Modify: `server/src/app.ts` (authRoutes 마운트), `server/test/helpers.ts` (`TestUser`, `signup`, `login`)
- Test: `server/test/api/auth.test.ts`, `server/test/api/ratelimit.test.ts`

**Interfaces:**
- Consumes: Task 2 crypto/ids, Task 5 `err`, `vJson`, `rt`
- Produces:
  - `ratelimit.ts` (계약 4.7): `LimiterName`, `LIMITER_PERIOD_SEC` (`RL_MESSAGE`·`RL_REACT` 10, 나머지 60), `limit(name, key?)`, `checkLimit(env, name, key)`, `clientIp(c)`.
  - `auth.ts` (계약 4.6): `requireAuth`, `requireAdmin`, `getAuth`, `authenticateToken(env, token)`, `resolveUserRef(db, ref, me)`. `requireAuth` 는 Bearer → `sha256Hex` → `sessions`(미폐기·미만료) → `users`(정지 중이면 `err.suspended`; `suspended_until < now` 는 활성) → `c.set("auth")` → `checkLimit(env,"RL_API","u:"+userId)` → `last_used_at` 이 1시간 넘었으면 `last_used_at=now, expires_at=now+90일`.
  - `serialize.ts` (계약 4.9 일부): `toUserSummary(row, nowMs?)`(만료된 status → `null`, `avatar_url = "/v1/media/<id>"`, `suspended` = 현재 정지 중), `loadUserSummaries(db, ids)`, `serializeMe(db, userId)`(settings·presence 포함; presence 는 `user_presence` 행에서 `status`, `mode`, `focus_ends_at > now` 면 `focus`), `serializeSession(row, currentSessionId)`.
  - `routes/auth.ts`: `authRoutes` — 이 작업은 `POST /auth/signup`(201), `POST /auth/login`(200). 가입 batch: `users` + `user_settings`(기본) + `user_presence(updated_at=now)` + `sessions`. 로그인: 키 `ip:<IP>` 와 `handle:<handle>` 둘 다 `RL_LOGIN`, 없는 handle 은 `dummyVerify` 후 401 `invalid_credentials`, 정지 → 403.
  - `helpers.ts`: `interface TestUser { token; user: Me; handle; password }`, `signup(opts?)`, `login(handle, password)`.

- [ ] **Step 1: 실패하는 테스트 작성** (`test/api/auth.test.ts` 에 추가)

```ts
describe("signup/login", () => {
  it("signup 201 returns token, session, Me with defaults", async () => {
    const handle = uniqueHandle();
    const r = await api().post<any>("/v1/auth/signup", { handle: handle.toUpperCase(), password: "correct horse battery", display_name: "  민지 ", device_name: "mbp" });
    expect(r.status).toBe(201);
    expect(r.body.token).toMatch(/^msl_[A-Za-z0-9_-]{43}$/);
    expect(r.body.session).toMatchObject({ client: "tui", device_name: "mbp", current: true });
    expect(r.body.user).toMatchObject({ handle, display_name: "민지", role: "user", is_private: false, settings: { dm_policy: "friends_and_spaces", notify: { dm: true, social: false }, focus_default_min: 25 }, presence: { status: "offline", mode: "auto", focus: null } });
    const row = await env.DB.prepare("SELECT password_hash FROM users WHERE handle=?").bind(handle).first<any>();
    expect(row.password_hash.startsWith("pbkdf2_sha256$100000$")).toBe(true);
  });
  it("validation: reserved, short, password==handle, dup", async () => {
    expect((await api().post<any>("/v1/auth/signup", { handle: "admin", password: "12345678" })).body.error.details.fields.handle).toBe("invalid");
    expect((await api().post<any>("/v1/auth/signup", { handle: "abcdef", password: "short" })).body.error.details.fields.password).toBe("too_short");
    expect((await api().post<any>("/v1/auth/signup", { handle: "samesame", password: "samesame" })).body.error.details.fields.password).toBe("invalid");
    const u = await signup();
    expect((await api().post<any>("/v1/auth/signup", { handle: u.handle, password: "another pass" })).status).toBe(409);
  });
  it("login ok / wrong password / unknown handle look identical", async () => {
    const u = await signup();
    expect((await api().post<any>("/v1/auth/login", { handle: u.handle, password: u.password })).status).toBe(200);
    const a = await api().post<any>("/v1/auth/login", { handle: u.handle, password: "nope nope" });
    const b = await api().post<any>("/v1/auth/login", { handle: uniqueHandle(), password: "nope nope" });
    expect([a.status, a.body.error.code, b.status, b.body.error.code]).toEqual([401, "invalid_credentials", 401, "invalid_credentials"]);
  });
  it("auth middleware: missing/garbage token 401; suspended 403", async () => {
    expect((await api().get("/v1/auth/sessions")).status).toBe(401);
    expect((await api("msl_garbage").get("/v1/auth/sessions")).status).toBe(401);
    const u = await signup();
    await env.DB.prepare("UPDATE users SET suspended_at=?1, suspend_reason='spam' WHERE id=?2").bind(Date.now(), u.user.id).run();
    const r = await api().post<any>("/v1/auth/login", { handle: u.handle, password: u.password });
    expect(r.status).toBe(403); expect(r.body.error).toMatchObject({ code: "account_suspended", details: { until: null, reason: "spam" } });
  });
});
describe("app wiring", () => {
  // Hono app.route() copies a sub-app's .use(pattern) into the parent as an ALL route; on a shared prefix that runs
  // requireAuth (and the RL_API count) twice for other modules' routes. requireAuth must be a per-route argument.
  it("no wildcard ALL middleware on shared prefixes (requireAuth runs once per request)", () => {
    const shared = /^\/v1(\/(auth|me|users|channels|messages|spaces|hashtags)(\/:[^/]+)?)?\/\*$/;
    const offenders = createApp().routes.filter((r) => r.method === "ALL" && shared.test(r.path)).map((r) => r.path);
    expect(offenders).toEqual([]);
  });
  it("signup and login stay reachable without a token", async () => {
    const r = await api().post<any>("/v1/auth/login", { handle: uniqueHandle(), password: "nope nope" });
    expect(r.body.error.code).toBe("invalid_credentials"); // not "unauthorized": no auth middleware in front of login
  });
});
```

(`createApp` 은 `../../src/app` 에서 import. 이 테스트는 이후 모든 작업이 라우트를 더할 때마다 Task 29 의 전체 실행에서 다시 검사된다.)
```ts
// test/api/ratelimit.test.ts
it("RL_SIGNUP: 6th signup from one IP in 60s -> 429 with Retry-After", async () => {
  const ip = uniqueIp(); let last: Response | undefined;
  for (let i = 0; i < 6; i++) last = await SELF.fetch(BASE + "/v1/auth/signup", { method: "POST", headers: { "Content-Type": "application/json", "CF-Connecting-IP": ip, "User-Agent": "masil/0.1.0 (test; test)" }, body: JSON.stringify({ handle: uniqueHandle(), password: "password123" }) });
  expect(last!.status).toBe(429);
  expect(last!.headers.get("Retry-After")).toBe("60");
  expect((await last!.json<any>()).error.details).toEqual({ limiter: "RL_SIGNUP", retry_after_sec: 60 });
});
it("RL_LOGIN keys by handle across IPs", async () => {
  const u = await signup(); let status = 0;
  for (let i = 0; i < 11; i++) status = (await api().post("/v1/auth/login", { handle: u.handle, password: "wrong pass" })).status;
  expect(status).toBe(429);
});
```

`GET /v1/auth/sessions` 는 이 작업에서 `requireAuth` 뒤에 내 활성 세션 목록(`serializeSession`, 최신순)을 그대로 돌려주는 형태로 만들고, Task 14 는 폐기 경로만 더한다.

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/auth.test.ts test/api/ratelimit.test.ts` · Expected: FAIL (signup 404).
- [ ] **Step 3: 구현** — 위 Produces 대로. 가입 검증 스키마: handle 은 소문자로 바꾼 뒤 정규식·예약어 검사. `authRoutes` 의 `GET /auth/sessions` 등 인증 경로는 `requireAuth` 를 라우트 인자로 단다(`authRoutes.use("/auth/*", requireAuth)` 는 가입·로그인을 막는다 — Global Constraints).
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/auth.test.ts test/api/ratelimit.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test
git commit -m "feat(server): signup, login, bearer auth middleware and rate limiters"
```

---

### Task 7: 권한 규칙 함수 (`lib/permissions.ts`)

**Files:**
- Create: `server/src/lib/permissions.ts`
- Test: `server/test/api/permissions.test.ts` (`describe("rules")` — SQL 로 관계를 심고 함수를 직접 호출)

**Interfaces:**
- Consumes: Task 4 `first/all/jsonList`, Task 5 `err`, rows
- Produces (계약 4.8 전부): `SpaceRole`, `areFriends`, `isBlockedEither`, `hasBlocked`, `shareSpace`, `canDm`, `canDmMany`, `getSpaceRole`, `requireSpaceRole`, `ChannelAccess`, `getChannelAccess`, `requireChannelRead`, `requireChannelWrite`, `canModerateChannel`, `accessibleChannelIds`, `postVisibilitySql`, `canViewPost`, `canViewUserContent`, `presenceVisible`.
  규칙 요지: `canDm` = api.md 3.1(정지 대상 불가, 양방향 차단 없음, 친구 또는 [미삭제 방 공유 AND 양쪽 `friends_and_spaces`]). `getChannelAccess`: 채널 `deleted_at` 이면 `null`; text 는 방 미삭제 + 방 멤버(+비공개면 `channel_members`); dm/group_dm 은 `channel_members`. `canWrite`: 읽기 + (1:1 dm 이면 `canDm`), 1:1 dm 에서 불가 시 `readOnlyReason` = 내가 상대를 차단했으면 `"blocked"`, 그 외 `"not_eligible"`. `requireChannelWrite` 는 읽기 불가 404, 쓰기 불가 403 `read_only`. `canModerateChannel` = text 이고 `spaceRole ∈ {owner, admin}`. `accessibleChannelIds` 는 data-model 4장 4번 범위. `postVisibilitySql` 은 api.md 3.4 1~4 를 `p` 별칭 기준 SQL 조각으로. `canViewUserContent`: 차단 양방향 → `"hidden"`, 비공개 + 비팔로워 → `"private"`. `presenceVisible`: 본인·친구·같은 방·같은 dm/group_dm.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
import { env } from "cloudflare:test";
import * as P from "../../src/lib/permissions";
// local fixture helpers (in this test file only): friend(a,b), block(a,b), space(ownerId, memberIds) -> spaceId, policy(id,'friends_only'), dm(a,b) -> channelId, privateText(spaceId, memberIds) -> channelId
describe("rules", () => {
  it("canDm truth table", async () => {
    const [a, b, c, d] = await Promise.all([signup(), signup(), signup(), signup()]);
    expect(await P.canDm(env.DB, a.user.id, b.user.id)).toBe(false);          // strangers
    await friend(a, b); expect(await P.canDm(env.DB, a.user.id, b.user.id)).toBe(true);
    await space(a, [c]); expect(await P.canDm(env.DB, a.user.id, c.user.id)).toBe(true);
    await policy(c, "friends_only"); expect(await P.canDm(env.DB, a.user.id, c.user.id)).toBe(false);
    await block(b, a); expect(await P.canDm(env.DB, a.user.id, b.user.id)).toBe(false);   // either direction
    expect(await P.canDm(env.DB, a.user.id, a.user.id)).toBe(false);
    await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), d.user.id).run(); await friend(a, d);
    expect(await P.canDm(env.DB, a.user.id, d.user.id)).toBe(false);
  });
  it("deleted space no longer counts as shared", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); const s = await space(a, [b]);
    await env.DB.prepare("UPDATE spaces SET deleted_at=? WHERE id=?").bind(Date.now(), s).run();
    expect(await P.shareSpace(env.DB, a.user.id, b.user.id)).toBe(false);
  });
  it("private text channel requires current space membership", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); const s = await space(a, [b]); const ch = await privateText(s, [a, b]);
    expect((await P.getChannelAccess(env.DB, ch, b.user.id))?.canRead).toBe(true);
    await env.DB.prepare("DELETE FROM space_members WHERE space_id=? AND user_id=?").bind(s, b.user.id).run();
    expect(await P.getChannelAccess(env.DB, ch, b.user.id)).toBeNull();
    await expect(P.requireChannelRead(env.DB, ch, b.user.id)).rejects.toMatchObject({ status: 404 });
  });
  it("1:1 dm becomes read-only; reason 'blocked' only for the blocker", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); await friend(a, b); const ch = await dm(a, b);
    await block(a, b); await env.DB.prepare("DELETE FROM friendships WHERE user_a IN (?1,?2)").bind(a.user.id, b.user.id).run();
    expect(await P.getChannelAccess(env.DB, ch, a.user.id)).toMatchObject({ canRead: true, canWrite: false, readOnlyReason: "blocked" });
    expect(await P.getChannelAccess(env.DB, ch, b.user.id)).toMatchObject({ canWrite: false, readOnlyReason: "not_eligible" });
    await expect(P.requireChannelWrite(env.DB, ch, b.user.id)).rejects.toMatchObject({ status: 403, code: "read_only" });
  });
  it("requireSpaceRole: 404 non-member, 403 below min", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]); const s = await space(a, [b]);
    await expect(P.requireSpaceRole(env.DB, s, c.user.id, "member")).rejects.toMatchObject({ status: 404 });
    await expect(P.requireSpaceRole(env.DB, s, b.user.id, "admin")).rejects.toMatchObject({ status: 403, code: "forbidden" });
    expect(await P.requireSpaceRole(env.DB, s, a.user.id, "admin")).toBe("owner");
  });
  it("post visibility: private author needs active follow; block hides", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(a.user.id).run();
    const post = await insertPost(a); // fixture: raw INSERT INTO posts
    expect(await P.canViewPost(env.DB, b.user.id, post)).toBe(false);
    await env.DB.prepare("INSERT INTO follows VALUES (?1, ?2, 'active', 0, 0)").bind(b.user.id, a.user.id).run();
    expect(await P.canViewPost(env.DB, b.user.id, post)).toBe(true);
    await block(a, b); expect(await P.canViewPost(env.DB, b.user.id, post)).toBe(false);
    expect(await P.canViewUserContent(env.DB, c.user.id, a.user.id)).toBe("private");
    expect(await P.canViewUserContent(env.DB, b.user.id, a.user.id)).toBe("hidden");
  });
  it("presenceVisible: friends, shared space, shared dm only", async () => {
    const [a, b, c, d] = await Promise.all([signup(), signup(), signup(), signup()]);
    await friend(a, b); await space(a, [c]);
    expect([...(await P.presenceVisible(env.DB, a.user.id, [a.user.id, b.user.id, c.user.id, d.user.id]))].sort()).toEqual([a.user.id, b.user.id, c.user.id].sort());
  });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/permissions.test.ts` · Expected: FAIL `Failed to resolve import "../../src/lib/permissions"`.
- [ ] **Step 3: 구현** — Produces 의 규칙대로. 친구 판정은 `user_a = min(a,b) AND user_b = max(a,b)`.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/permissions.test.ts` · Expected: `Tests  7 passed (7)`
- [ ] **Step 5: Commit**

```bash
git add server/src/lib/permissions.ts server/test/api/permissions.test.ts
git commit -m "feat(server): permission rules for dm, spaces, channels, posts and presence"
```

---

### Task 8: 실시간 프로토콜 상수·청중 해석

**Files:**
- Create: `server/src/do/protocol.ts`, `server/src/lib/realtime.ts` (이 단계: `Audience`, `VisibilitySpec`, `PublishOptions`, `resolveOnlineAudience`)
- Modify: `server/src/do/user-hub.ts` (타입 `OutboundEvent`, `DeliveryBatch`, `ConnAttachment` export 만)
- Test: `server/test/realtime/fanout.test.ts` (`describe("audience")`)

**Interfaces:**
- Consumes: Task 4 db, Task 5 types/events
- Produces:
  - `protocol.ts`: `const CloseCode = { normal: 1000, goingAway: 1001, tooBig: 1009, internal: 1011, protocol: 4000, unauthorized: 4001, heartbeat: 4002, suspended: 4003, rateLimited: 4008, replaced: 4009, upgradeRequired: 4010 } as const`; `NOTIFY_ALLOWED_TYPES: ReadonlySet<ServerEventType>` = `hello, ready, reply, session.revoked, alert, notification.created, notifications.read, focus.started, focus.ended, device.tui_state`; `HEARTBEAT = { intervalMs: 25_000, timeoutMs: 120_000 }`, `LIMITS = { maxFrameBytes: 8192, watchTopicsMax: 50, typingMinIntervalMs: 3000 }`; `ClientFrameSchema`(zod discriminated union on `type`: resume `{epoch: string|null, last_seq: number|null} | {use_device_ack: true}`, ack `{seq:int>=0}`, typing `{channel_id: zId, thread_root_id: zId|null}`, idle `{idle: boolean}`, watch `{topics: string[]}`(각 `^post:[0-9A-Z]{26}$`), 선택 `ref` 1..32자); `type ClientFrame = z.infer<typeof ClientFrameSchema>`.
  - `user-hub.ts` 타입(계약 3.2): `ConnAttachment`, `OutboundEvent`, `DeliveryBatch`.
  - `realtime.ts` (계약 4.11): `Audience`, `VisibilitySpec`, `PublishOptions`, `resolveOnlineAudience(env, audience, opts?)` — 계약 4.11 의 SQL(공개/비공개 text 의 방 미삭제 조건, `followers` 는 active + 차단 제외, `presenceWatchers` = 친구 ∪ dm/group_dm 공동 멤버, `visibleTo` 필터, `exclude`, `notifyOk`). 삭제 필터는 확정: `{channel}` 은 채널 `deleted_at IS NULL` + (text 면) 방 `deleted_at IS NULL`, `{space}` 는 방 `deleted_at IS NULL`. 따라서 `space.deleted`·`channel.deleted{reason:"deleted"}` 는 이 청중으로 보내지 않는다 — 삭제 핸들러가 batch 전에 사용자 ID 를 읽어 `{users}` 로 보낸다(Global Constraints "삭제 이벤트의 청중", Task 21·23).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// fixtures in this file: online(u, {full?: boolean, notify?: boolean}) sets user_presence.online_until/notify_online_until = now+60s
describe("audience", () => {
  it("channel audience: only full-online space members; notifyOk adds daemon-only", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]); const s = await space(a, [b, c]); const ch = await publicText(s);
    await online(a, { full: true }); await online(b, { notify: true });
    expect(await resolveOnlineAudience(env, { channel: ch })).toEqual([a.user.id]);
    expect((await resolveOnlineAudience(env, { channel: ch }, { notifyOk: true })).sort()).toEqual([a.user.id, b.user.id].sort());
    expect(await resolveOnlineAudience(env, { channel: ch }, { exclude: [a.user.id] })).toEqual([]);
  });
  it("left member never in private channel audience; deleted channel / deleted space -> empty", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); const s = await space(a, [b]); const ch = await privateText(s, [a, b]); const pub = await publicText(s);
    await online(a, { full: true }); await online(b, { full: true });
    await env.DB.prepare("DELETE FROM space_members WHERE space_id=? AND user_id=?").bind(s, b.user.id).run();
    expect(await resolveOnlineAudience(env, { channel: ch })).toEqual([a.user.id]);
    await env.DB.prepare("UPDATE channels SET deleted_at=1 WHERE id=?").bind(pub).run();
    expect(await resolveOnlineAudience(env, { channel: pub })).toEqual([]);
    await env.DB.prepare("UPDATE spaces SET deleted_at=1 WHERE id=?").bind(s).run();
    expect(await resolveOnlineAudience(env, { channel: ch })).toEqual([]);
    expect(await resolveOnlineAudience(env, { space: s })).toEqual([]);
  });
  it("followers + visibleTo: blocked or non-follower of private author filtered", async () => {
    const [author, f1, f2, other] = await Promise.all([signup(), signup(), signup(), signup()]);
    for (const f of [f1, f2]) await followActive(f, author); await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(other.user.id).run();
    for (const u of [f1, f2]) await online(u, { full: true }); await block(other, f2);
    const r = await resolveOnlineAudience(env, { followers: author.user.id }, { visibleTo: { authors: [author.user.id, other.user.id], privateAuthors: [other.user.id] } });
    expect(r).toEqual([]); // f1 doesn't follow private 'other'; f2 is blocked by 'other'
  });
  it("presenceWatchers = friends + dm co-members", async () => {
    const [a, b, c] = await Promise.all([signup(), signup(), signup()]); await friend(a, b); await groupDm(a, [c]);
    for (const u of [b, c]) await online(u, { full: true });
    expect((await resolveOnlineAudience(env, { presenceWatchers: a.user.id })).sort()).toEqual([b.user.id, c.user.id].sort());
  });
  it("Review Focus 2: 120 online space members resolve without hitting the bind limit", async () => {
    const owner = await signup(); const members = await Promise.all(Array.from({ length: 119 }, () => signup()));
    const s = await space(owner, members); const ch = await publicText(s);
    for (const u of [owner, ...members]) await online(u, { full: true });
    expect(await resolveOnlineAudience(env, { channel: ch })).toHaveLength(120);
  });
});
```

(120명 가입은 느리므로 이 테스트만 `users` 행을 SQL 로 직접 넣어도 된다 — 비밀번호 해시 계산이 병목.)

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/realtime/fanout.test.ts` · Expected: FAIL import 실패.
- [ ] **Step 3: 구현** — 청중별 SQL 1개 + `user_presence` JOIN, 시각은 파라미터 1개. `visibleTo` 는 계약 4.11 의 NOT EXISTS 조각 그대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/realtime/fanout.test.ts` · Expected: `Tests  5 passed (5)`
- [ ] **Step 5: Commit**

```bash
git add server/src/do/protocol.ts server/src/do/user-hub.ts server/src/lib/realtime.ts server/test/realtime/fanout.test.ts
git commit -m "feat(server): realtime protocol constants and online audience resolution"
```

---

### Task 9: UserHub 연결 — 업그레이드·hello·resume·ready·프레임 검증

**Files:**
- Modify: `server/src/do/user-hub.ts` (생성자, `fetch`, `webSocketMessage`(resume·잘못된 프레임·속도 제한), `webSocketClose`, `webSocketError`, `inspect`)
- Create: `server/src/routes/ws.ts`
- Modify: `server/src/app.ts` (wsRoutes 마운트), `server/test/helpers.ts` (`WsClient`, `connectWs`)
- Test: `server/test/realtime/ws-handshake.test.ts`

**Interfaces:**
- Consumes: Task 6 `requireAuth`, `getAuth`, `limit("RL_WS")`; Task 8 `CloseCode`, `ClientFrameSchema`, `HEARTBEAT`, `LIMITS`
- Produces: `UserHub.fetch(request)`, `UserHub.inspect()`, `UserHub.webSocketMessage/Close/Error` (계약 3.2 시그니처); `protected now(): number`(기본 `Date.now()`, 테스트가 `runInDurableObject` 안에서 덮어씀); `wsRoutes`(계약 3.4 그대로; `device_id` 는 `zId`, `mode` 기본 `full`, Upgrade 아님 → 400). `connectWs(token, opts?)` (계약 7.2).
  DO 저장소: 생성자에서 `ctx.blockConcurrencyWhile` 로 계약 3.2 의 4개 테이블 `CREATE TABLE IF NOT EXISTS`, `setWebSocketAutoResponse(new WebSocketRequestResponsePair('{"type":"ping"}','{"type":"pong"}'))`.
  `fetch` 순서는 계약 3.2 `fetch` 1~5 그대로(epoch 검사·D1 기한 쓰기를 `blockConcurrencyWhile` 안에서, D1 실패 → 1011 거절, 10개 초과 → 가장 오래된 소켓 4009, `acceptWebSocket(server, ["mode:"+mode, "dev:"+deviceId, "sess:"+sessionId])`, attachment 저장, `hello` 전송, 알람 예약). D1 쓰기: `UPDATE user_presence SET online_until = MAX(online_until, ?) …`.
  `webSocketMessage`: 바이너리 → `close(4000)`; 8192바이트 초과 → `close(1009)`; JSON 아님/스키마 불일치 → `ref` 가 있으면 `reply{ok:false, error:{code:"invalid_frame"}}`, 알 수 없는 type → `reply{unknown_type}`; `ready` 전 resume 외 프레임 → `close(4000)`; 두 번째 resume → `close(4000)`; 10초 30프레임 초과 → `reply{rate_limited}`, 60 초과 → `close(4008)`. resume 처리: 계약 events.md 3.3 1~4 (재전송은 Task 10 에서 실데이터로 검증), `ready` 송신, full 이면 `presence.snapshot`(Task 12 가 내용 채움, 여기선 `{items:[]}`).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
describe("ws handshake", () => {
  it("hello -> resume(null) -> ready; ping auto-pong", async () => {
    const u = await signup(); const dev = newId();
    const c = await connectWs(u.token, { deviceId: dev });
    const hello = c.frames.find((f) => f.type === "hello")!;
    expect(hello.seq).toBeNull();
    expect(hello.data).toMatchObject({ user_id: u.user.id, mode: "full", device_id: dev, seq: 0, oldest_seq: 1, heartbeat_interval_ms: 25000, heartbeat_timeout_ms: 120000, local_tui_connected: false, limits: { max_frame_bytes: 8192, watch_topics_max: 50, typing_min_interval_ms: 3000 } });
    expect((await c.next("ready")).data).toMatchObject({ resumed: false, resync_required: false, replayed: 0, epoch: hello.data.epoch });
    c.send('{"type":"ping"}'); await c.next("pong");
    const p = await env.DB.prepare("SELECT online_until FROM user_presence WHERE user_id=?").bind(u.user.id).first<any>();
    expect(p.online_until).toBeGreaterThan(Date.now() + 9 * 60_000);
  });
  it("rejects before upgrade: no token 401, bad device_id 400, not websocket 400", async () => {
    const u = await signup();
    expect((await SELF.fetch(BASE + "/v1/ws?device_id=" + newId(), { headers: { Upgrade: "websocket" } })).status).toBe(401);
    expect((await SELF.fetch(BASE + "/v1/ws?device_id=bad", { headers: { Upgrade: "websocket", Authorization: "Bearer " + u.token } })).status).toBe(400);
    expect((await SELF.fetch(BASE + "/v1/ws?device_id=" + newId(), { headers: { Authorization: "Bearer " + u.token } })).status).toBe(400);
  });
  it("client cannot forge X-Masil-Auth", async () => {
    const [u, v] = await Promise.all([signup(), signup()]);
    const res = await SELF.fetch(BASE + "/v1/ws?device_id=" + newId(), { headers: { Upgrade: "websocket", Authorization: "Bearer " + u.token, "X-Masil-Auth": JSON.stringify({ userId: v.user.id }) } });
    const ws = res.webSocket!; ws.accept(); const hello = await firstFrame(ws);
    expect(hello.data.user_id).toBe(u.user.id);
  });
  it("11th connection replaces the oldest with 4009", async () => {
    const u = await signup(); const cs = []; for (let i = 0; i < 10; i++) cs.push(await connectWs(u.token));
    const closed = waitClose(cs[0].ws); await connectWs(u.token);
    expect((await closed).code).toBe(4009);
  });
  it("Review Focus 3: binary -> 4000, >8KB -> 1009, unknown type -> reply, frame before resume -> 4000", async () => {
    const u = await signup();
    let c = await connectWs(u.token); let cl = waitClose(c.ws); c.ws.send(new Uint8Array([1, 2])); expect((await cl).code).toBe(4000);
    c = await connectWs(u.token); cl = waitClose(c.ws); c.send({ type: "idle", data: { idle: false, pad: "x".repeat(9000) } }); expect((await cl).code).toBe(1009);
    c = await connectWs(u.token); c.send({ type: "dance", ref: "r1", data: {} });
    expect((await c.next("reply", (d) => d.ref === "r1")).data).toMatchObject({ ok: false, error: { code: "unknown_type" } });
    c.send("not json"); c.send({ type: "ack", data: { seq: 0 } }); // still alive
    const raw = await rawConnect(u.token); cl = waitClose(raw); raw.send(JSON.stringify({ type: "typing", data: { channel_id: newId(), thread_root_id: null } }));
    expect((await cl).code).toBe(4000);
  });
  it("frame flood: >60 frames/10s -> 4008", async () => {
    const u = await signup(); const c = await connectWs(u.token); const cl = waitClose(c.ws);
    for (let i = 0; i < 61; i++) c.send({ type: "ack", data: { seq: 0 } });
    expect((await cl).code).toBe(4008);
  });
});
```

(`firstFrame`, `waitClose`, `rawConnect` 는 이 테스트 파일 안 지역 함수; `rawConnect` 는 hello 만 받고 resume 을 보내지 않는 연결.)

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/realtime/ws-handshake.test.ts` · Expected: FAIL (`/v1/ws` 404).
- [ ] **Step 3: 구현** — 위 Interfaces 대로. 이 단계에서 `alarm()` 은 다음 알람 재예약만.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/realtime/ws-handshake.test.ts` · Expected: `Tests  6 passed (6)`
- [ ] **Step 5: Commit**

```bash
git add server/src server/test
git commit -m "feat(server): UserHub websocket upgrade, hello/resume/ready and frame validation"
```

---

### Task 10: UserHub 이벤트 로그·재전송·ack·epoch·알람

**Files:**
- Modify: `server/src/do/user-hub.ts` (`deliver`, resume 재전송, `ack`, `alarm` 전체)
- Test: `server/test/realtime/ws-resume.test.ts`

**Interfaces:**
- Consumes: Task 9 연결·`now()`
- Produces: `UserHub.deliver(batch: DeliveryBatch): Promise<void>` (계약 3.2 처리 1~4; alert suppression 은 Task 12 에서), `UserHub.alarm()` (계약 3.2 alarm 항목 전부: 다음 시각 = min(집중 종료, full 있으면 now+60s / notify 만 있으면 now+15분, resume 대기 10초 기한); 하트비트 4002; 놓친 close 처리; 기한 연장 full 5분 미만이면 +10분 / notify +40분, 성공 후 워터마크; 연장 실패로 `now >= full_until` 이면 full 소켓 1011; 로그 정리 `seq <= MAX(seq)-2000 OR ts < now-86_400_000`; resume 없는 소켓 10초 → 4000). `webSocketClose`: full 1→0 이면 워터마크·D1 `online_until = now+120_000`, notify 1→0 이면 `notify_online_until` 동일, 0개면 `zero_since`, `last_seen_at`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
const hub = (id: string) => env.USER_HUB.getByName(id);
const ev = (n: number) => ({ type: "settings.updated" as const, data: { n }, ts: Date.now(), logged: true });
describe("resume", () => {
  it("replays logged events after last_seq in order; ephemeral has seq null", async () => {
    const u = await signup(); const c1 = await connectWs(u.token); const epoch = c1.frames[0].data.epoch;
    await hub(u.user.id).deliver({ userId: u.user.id, events: [ev(1), ev(2), { type: "typing", data: {}, ts: Date.now(), logged: false }] });
    const t = await c1.next("typing"); expect(t.seq).toBeNull();
    c1.close(1000);
    await hub(u.user.id).deliver({ userId: u.user.id, events: [ev(3)] });  // offline but within grace: logged
    const c2 = await connectWs(u.token, { resume: { epoch, last_seq: 1 } });
    expect(c2.frames.filter((f) => f.type === "settings.updated").map((f) => f.seq)).toEqual([2, 3]);
    expect((c2.frames.find((f) => f.type === "ready"))!.data).toMatchObject({ resumed: true, resync_required: false, replayed: 2, seq: 3 });
  });
  it("watermark passed without close handler -> new epoch -> resync_required", async () => {
    const u = await signup(); const c1 = await connectWs(u.token); const epoch = c1.frames[0].data.epoch;
    await env.DB.prepare("UPDATE user_presence SET online_until=1 WHERE user_id=?").bind(u.user.id).run();
    await runInDurableObject(hub(u.user.id), (_i, st) => { st.storage.sql.exec("UPDATE meta SET v='1' WHERE k='full_until'"); });
    const c2 = await connectWs(u.token, { resume: { epoch, last_seq: 0 } });
    expect(c2.frames[0].data.epoch).not.toBe(epoch);
    expect(c2.frames.find((f) => f.type === "ready")!.data).toMatchObject({ resumed: false, resync_required: true });
  });
  it("use_device_ack is per (device, mode)", async () => {
    const u = await signup(); const dev = newId();
    const full = await connectWs(u.token, { deviceId: dev }); const notify = await connectWs(u.token, { deviceId: dev, mode: "notify" });
    await hub(u.user.id).deliver({ userId: u.user.id, events: [{ type: "alert", data: { alert: fakeAlert() }, ts: Date.now(), logged: true }, ev(2)] });
    full.send({ type: "ack", data: { seq: 2 } }); notify.send({ type: "ack", data: { seq: 0 } }); await settle();
    notify.close(1000);
    const n2 = await connectWs(u.token, { deviceId: dev, mode: "notify", resume: { use_device_ack: true } as any });
    expect(n2.frames.filter((f) => f.type === "alert").map((f) => f.seq)).toEqual([1]);   // settings.updated not allowed in notify
    const rows = await runInDurableObject(hub(u.user.id), (_i, st) => st.storage.sql.exec("SELECT mode, seq FROM device_acks ORDER BY mode").toArray());
    expect(rows).toEqual([{ mode: "full", seq: 2 }, { mode: "notify", seq: 0 }]);
  });
  it("alarm: heartbeat timeout closes 4002; no resume in 10s closes 4000", async () => {
    const u = await signup(); const c = await connectWs(u.token); const cl = waitClose(c.ws);
    await runInDurableObject(hub(u.user.id), (inst: any) => { inst.now = () => Date.now() + 121_000; });
    await runDurableObjectAlarm(hub(u.user.id)); expect((await cl).code).toBe(4002);
    const v = await signup(); const raw = await rawConnect(v.token); const cl2 = waitClose(raw);
    await runInDurableObject(hub(v.user.id), (inst: any) => { inst.now = () => Date.now() + 11_000; });
    await runDurableObjectAlarm(hub(v.user.id)); expect((await cl2).code).toBe(4000);
  });
  it("alarm extends online_until when <5min left and trims log to 2000", async () => {
    const u = await signup(); await connectWs(u.token);
    await runInDurableObject(hub(u.user.id), (_i, st) => { st.storage.sql.exec("UPDATE meta SET v=? WHERE k='full_until'", String(Date.now() + 60_000)); });
    await hub(u.user.id).deliver({ userId: u.user.id, events: Array.from({ length: 2005 }, (_, i) => ev(i)) });
    await runDurableObjectAlarm(hub(u.user.id));
    const s = await hub(u.user.id).inspect();
    expect(s.fullUntil).toBeGreaterThan(Date.now() + 9 * 60_000); expect(s.seq - s.oldestSeq + 1).toBe(2000);
  });
  it("close of last full socket shortens online_until to ~now+120s", async () => {
    const u = await signup(); const c = await connectWs(u.token); c.close(1000); await settle();
    const p = await env.DB.prepare("SELECT online_until, last_seen_at FROM user_presence WHERE user_id=?").bind(u.user.id).first<any>();
    expect(p.online_until).toBeLessThanOrEqual(Date.now() + 120_000); expect(p.last_seen_at).not.toBeNull();
  });
});
```

(`fakeAlert()`, `settle()`(짧은 대기), `waitClose`, `rawConnect` 는 파일 지역 헬퍼.)

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/realtime/ws-resume.test.ts` · Expected: FAIL (`deliver is not a function` 등).
- [ ] **Step 3: 구현** — 계약 3.2 대로. frame 은 `JSON.stringify({type, seq, ts: iso(ts), data})` 를 `events.frame` 에 저장하고 재전송 때 그대로. `deliver` 의 SQL 쓰기는 `ctx.storage.transactionSync` 한 구간. notify 소켓에는 `NOTIFY_ALLOWED_TYPES` 만.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/realtime/ws-resume.test.ts test/realtime/ws-handshake.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src/do/user-hub.ts server/test/realtime/ws-resume.test.ts
git commit -m "feat(server): UserHub seq log, resume replay, device acks, epochs and alarms"
```

---

### Task 11: 발행 함수 — publishEvent·publishEvents·deliverToUsers·notify 모드

**Files:**
- Modify: `server/src/lib/realtime.ts` (발행 함수들), `server/src/do/user-hub.ts` (`device.tui_state`, `hello.local_tui_connected`)
- Test: `server/test/realtime/notify-mode.test.ts`, `server/test/realtime/fanout.test.ts` (`describe("publish")`)

**Interfaces:**
- Consumes: Task 8 `resolveOnlineAudience`, `NOTIFY_ALLOWED_TYPES`; Task 10 `deliver`
- Produces (계약 4.11): `publishEvent<T>(rt, audience, type, data, opts?)`, `publishEvents(rt, audience, events, opts?)`, `deliverToUsers(rt, userIds, events)`. 규칙: 묶음이 전부 notify 허용 타입이면 `notifyOk=true`, 섞이면 둘로 나눠 발행; `includeSelf` 는 그 사용자도 같은 온라인 판정을 거쳐 추가; `{topic}` 청중은 `env.TOPIC_HUB.getByName(topic).publish(topic, events, {visibleTo})`(Task 13 에서 구현, 이 작업에선 경로만); UserHub RPC 는 50개씩 `Promise.allSettled`; `APP_ENV==="test"` 면 await, 아니면 `rt.waitUntil`.
  UserHub: full 연결 0→1 이면 같은 `dev:` 태그 notify 소켓에 `device.tui_state {device_id, tui_connected:true}`(휘발), 1→0 이면 `false`; notify 연결의 `hello.local_tui_connected` 는 같은 기기 full 소켓 존재 여부.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// notify-mode.test.ts
it("daemon-only user gets alert but not message.created", async () => {
  const [a, b] = await Promise.all([signup(), signup()]); const s = await space(a, [b]); const ch = await publicText(s);
  const d = await connectWs(b.token, { mode: "notify" });
  const r = rtFor(env);
  await publishEvent(r, { channel: ch }, "message.created", { message: fakeMessage(ch) } as any);
  await publishEvent(r, { users: [b.user.id] }, "alert", { alert: fakeAlert() });
  await d.next("alert");
  expect(d.frames.some((f) => f.type === "message.created")).toBe(false);
  expect((await env.USER_HUB.getByName(b.user.id).inspect()).seq).toBe(1); // message.created never reached the hub
});
it("device.tui_state toggles and hello.local_tui_connected", async () => {
  const u = await signup(); const dev = newId();
  const d = await connectWs(u.token, { mode: "notify", deviceId: dev }); expect(d.frames[0].data.local_tui_connected).toBe(false);
  const t = await connectWs(u.token, { deviceId: dev });
  expect((await d.next("device.tui_state")).data).toEqual({ device_id: dev, tui_connected: true });
  const d2 = await connectWs(u.token, { mode: "notify", deviceId: dev }); expect(d2.frames[0].data.local_tui_connected).toBe(true);
  t.close(1000); expect((await d.next("device.tui_state", (x) => !x.tui_connected)).data.tui_connected).toBe(false);
});
it("notify mode rejects typing/idle/watch", async () => {
  const u = await signup(); const d = await connectWs(u.token, { mode: "notify" });
  d.send({ type: "idle", ref: "i1", data: { idle: true } });
  expect((await d.next("reply", (x) => x.ref === "i1")).data.error.code).toBe("not_allowed_in_mode");
});
```
```ts
// fanout.test.ts
describe("publish", () => {
  it("two members in one space both receive message.created (self included)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); const s = await space(a, [b]); const ch = await publicText(s);
    const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
    await publishEvent(rtFor(env), { channel: ch }, "message.created", { message: fakeMessage(ch) } as any);
    const [fa, fb] = await Promise.all([ca.next("message.created"), cb.next("message.created")]);
    expect(fa.seq).toBeGreaterThan(0); expect(fb.seq).toBeGreaterThan(0);
  });
  it("exclude skips; ephemeral has seq null", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); const s = await space(a, [b]); const ch = await publicText(s);
    const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
    await publishEvent(rtFor(env), { channel: ch }, "typing", { channel_id: ch, thread_root_id: null, user_id: a.user.id, expires_in_ms: 6000 }, { ephemeral: true, exclude: [a.user.id] });
    expect((await cb.next("typing")).seq).toBeNull(); await settle(); expect(ca.frames.some((f) => f.type === "typing")).toBe(false);
  });
});
```

(`rtFor(env)` = `{ env, waitUntil: () => {} }`, `fakeMessage`, `fakeAlert` 는 테스트 지역.)

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/realtime/notify-mode.test.ts test/realtime/fanout.test.ts` · Expected: FAIL (`publishEvent` 없음).
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: 같은 명령 · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src/lib/realtime.ts server/src/do/user-hub.ts server/test/realtime
git commit -m "feat(server): publishEvent fan-out with notify-mode split and tui_state signal"
```

---

### Task 12: 접속 상태·집중 모드 (UserHub + `routes/presence.ts`)

**Files:**
- Modify: `server/src/do/user-hub.ts` (`setPresenceMode`, `getPresenceState`, `startFocus`, `endFocus`, `idle` 프레임, 상태 재계산, `presence.snapshot`, alert suppression, 집중 종료 알람)
- Create: `server/src/routes/presence.ts`
- Modify: `server/src/app.ts`
- Test: `server/test/realtime/presence-focus.test.ts`

**Interfaces:**
- Consumes: Task 7 `presenceVisible`; Task 11 `publishEvent`, `deliverToUsers`; Task 6 serializer
- Produces: UserHub RPC(계약 3.2) `setPresenceMode(userId, mode): Promise<PresenceState>`, `getPresenceState(userId): Promise<PresenceState>`, `startFocus(userId, durationSec): Promise<Focus>`, `endFocus(userId): Promise<FocusSummary | null>`. 상태 계산은 events.md 6.3 1~6, 바뀔 때만 D1 `user_presence(status, mode, focus_ends_at, last_seen_at)` 갱신 + `presence.updated`(휘발, `presenceWatchers`) + `presence.self`(휘발, 나). `deliver` 의 alert: 집중 중 `suppressed:true, suppressed_reason:"focus"` + `focus_alerts` INSERT, 수동 dnd 는 `"dnd"`(누적 안 함). 집중 종료는 **조기 종료(`endFocus` = `DELETE /me/focus`)와 자연 종료(알람) 모두** 같은 내부 처리 하나(`finishFocus(endedAt)`)를 거친다: 집중 상태·`focus_ends_at` 을 먼저 지우고(그래서 뒤이은 알람은 아무것도 하지 않고, 이 alert 자신도 suppression 대상이 아님) presence 재계산 → `focus.ended{summary}`(기록) → `alert{kind:"focus_summary", title:"집중 모드 끝 — 멘션 N, DM M", suppressed:false, suppressed_reason:null}`(기록, 수동 dnd 중이어도 suppressed 아님 — events.md 4.7 설정 스위치 "항상") 순서로 같은 발행에 담아 보낸 뒤 `focus_alerts` 비움. 그래서 집중 한 번에 `focus_summary` alert 는 **종료 경로와 상관없이 정확히 1번**(events.md 4.7 "집중 모드 종료 시 1회", client-architecture 9.1·10.3 의 요약 오버레이 유일 트리거). 집중 중 `POST /me/focus` 재시작(종료 시각 교체)은 종료가 아니므로 보내지 않는다. `FocusSummary.counts` 는 kind 별(`invite`, `friend_request`, 그 외 → `other`), `items` 최신순 ≤20.
  `presenceRoutes` (api.md 5.1): `PUT /me/presence`, `GET /presence?user_ids=`(≤100, `presenceVisible` 밖 제외), `POST /me/focus`(60..43200, 기본 `focus_default_min*60`, 이미 집중 중이면 종료 시각 교체; `200 {focus}`), `GET /me/focus`(`200 {focus: Focus | null}` = `getPresenceState(me).focus`), `DELETE /me/focus`(`200 {summary}`, 집중 아님 → 409 `conflict` `reason:"not_focusing"`). 모두 라우트별 `requireAuth`. **`/me/focus` 세 경로는 이 작업이 완성본이다** — plan 02 Task 13 은 다시 등록하지 않고 테스트로 고정만 한다(Global Constraints 소유 경계).
  `ready` 직후(full 만) `presence.snapshot{items}` = 친구 + dm/group_dm 상대 전원의 `user_presence`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
it("status: offline -> online on full connect; notify-only stays offline; idle -> away", async () => {
  const [a, b] = await Promise.all([signup(), signup()]); await makeFriendsSql(a, b); const cb = await connectWs(b.token);
  await connectWs(a.token, { mode: "notify" }); await settle();
  expect((await api(a.token).get<any>(`/v1/presence?user_ids=${a.user.id}`)).body.items[0].status).toBe("offline");
  const ca = await connectWs(a.token);
  expect((await cb.next("presence.updated", (d) => d.user_id === a.user.id && d.status === "online")).seq).toBeNull();
  ca.send({ type: "idle", data: { idle: true } });
  await cb.next("presence.updated", (d) => d.user_id === a.user.id && d.status === "away");
});
it("presence.snapshot lists friends after ready", async () => {
  const [a, b] = await Promise.all([signup(), signup()]); await makeFriendsSql(a, b);
  const ca = await connectWs(a.token);
  expect((await ca.next("presence.snapshot")).data.items).toEqual([{ user_id: b.user.id, status: "offline", last_seen_at: null }]);
});
it("manual dnd suppresses alerts with reason dnd; GET /presence hides strangers", async () => {
  const [a, s] = await Promise.all([signup(), signup()]); const c = await connectWs(a.token);
  expect((await api(a.token).put<any>("/v1/me/presence", { mode: "dnd" })).body.presence).toMatchObject({ status: "dnd", mode: "dnd" });
  await deliverToUsers(rtFor(env), [a.user.id], [{ type: "alert", data: { alert: fakeAlert() }, ts: Date.now(), logged: true }]);
  expect((await c.next("alert")).data.alert).toMatchObject({ suppressed: true, suppressed_reason: "dnd" });
  expect((await api(a.token).get<any>(`/v1/presence?user_ids=${s.user.id}`)).body.items).toEqual([]);
});
it("focus: start -> suppressed(focus) -> DELETE returns summary + focus.ended + focus_summary alert exactly once; second DELETE 409", async () => {
  const a = await signup(); const c = await connectWs(a.token, { mode: "notify" });
  const f = await api(a.token).post<any>("/v1/me/focus", { duration_sec: 60 });
  expect(f.status).toBe(200); await c.next("focus.started");
  expect((await api(a.token).get<any>("/v1/me/focus")).body.focus).toEqual(f.body.focus);
  await deliverToUsers(rtFor(env), [a.user.id], [{ type: "alert", data: { alert: fakeAlert("mention") }, ts: Date.now(), logged: true }]);
  expect((await c.next("alert")).data.alert).toMatchObject({ suppressed: true, suppressed_reason: "focus" });
  const end = await api(a.token).del<any>("/v1/me/focus");
  expect(end.body.summary.counts).toEqual({ dm: 0, mention: 1, thread_reply: 0, invite: 0, friend_request: 0, other: 0 });
  expect((await c.next("focus.ended")).data.summary.counts.mention).toBe(1);
  const sum = await c.next("alert", (d) => d.alert.kind === "focus_summary");   // early end also sends the summary alert
  expect(sum.seq).not.toBeNull();
  expect(sum.data.alert).toMatchObject({ title: "집중 모드 끝 — 멘션 1, DM 0", suppressed: false, suppressed_reason: null });
  expect((await api(a.token).del<any>("/v1/me/focus")).body.error).toMatchObject({ code: "conflict", details: { reason: "not_focusing" } });
  // the original end time passing later must not produce a second summary
  await runInDurableObject(env.USER_HUB.getByName(a.user.id), (inst: any) => { inst.now = () => Date.now() + 61_000; });   // < 120s heartbeat timeout, socket stays open
  await runDurableObjectAlarm(env.USER_HUB.getByName(a.user.id)); await settle();
  expect(c.frames.filter((f) => f.type === "alert" && f.data.alert.kind === "focus_summary")).toHaveLength(1);
  expect(c.frames.filter((f) => f.type === "focus.ended")).toHaveLength(1);
  expect((await api(a.token).get<any>("/v1/me/focus")).body.focus).toBeNull();
  expect((await api(a.token).post<any>("/v1/me/focus", { duration_sec: 30 })).status).toBe(400);
});
it("focus natural end via alarm sends focus.ended + focus_summary alert", async () => {
  const a = await signup(); const c = await connectWs(a.token); await api(a.token).post("/v1/me/focus", { duration_sec: 60 });
  await runInDurableObject(env.USER_HUB.getByName(a.user.id), (inst: any) => { inst.now = () => Date.now() + 61_000; });
  await runDurableObjectAlarm(env.USER_HUB.getByName(a.user.id));
  await c.next("focus.ended"); expect((await c.next("alert", (d) => d.alert.kind === "focus_summary")).data.alert.suppressed).toBe(false);
  expect((await api(a.token).del<any>("/v1/me/focus")).status).toBe(409);   // already ended: no second summary path
  await settle(); expect(c.frames.filter((f) => f.type === "alert" && f.data.alert.kind === "focus_summary")).toHaveLength(1);
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/realtime/presence-focus.test.ts` · Expected: FAIL.
- [ ] **Step 3: 구현** — Produces 대로. DO 안의 발행은 `{ env: this.env, waitUntil: (p) => this.ctx.waitUntil(p) }` 런타임으로 `publishEvent` 호출.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/realtime` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/realtime/presence-focus.test.ts
git commit -m "feat(server): presence computation, focus mode and presence routes"
```

---

### Task 13: TopicHub·`watch` 프레임

**Files:**
- Modify: `server/src/do/topic-hub.ts`, `server/src/do/user-hub.ts` (`watch` 처리)
- Test: `server/test/realtime/topic-hub.test.ts`

**Interfaces:**
- Consumes: Task 11 `OutboundEvent`, `VisibilitySpec`; UserHub `deliver`
- Produces (계약 3.3): `TopicHub.join(userId, ttlMs)`, `leave(userId)`, `publish(topic, events, opts?): Promise<number>`, `inspect(): Promise<{watchers:number}>`, `alarm()`(만료 삭제, 비면 `ctx.storage.deleteAll()`), `protected now()`. `publish` 는 만료 안 된 시청자 → `visibleTo` 있으면 D1 1회 필터(시청자 목록 `jsonList`) → 각 UserHub `deliver({userId, events: events.map(e => ({...e, topic}))})` 50개씩. UserHub `watch`: 50개 초과 → `reply{limit_exceeded}`; 이전/새 비교 후 이 사용자의 다른 소켓도 안 보는 토픽만 `leave`, 새 목록 전부 `join(userId, 300_000)`; `deliver` 는 `topic` 이벤트를 그 토픽을 attachment 에 가진 소켓에만.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
it("watch -> publish reaches only watching sockets", async () => {
  const u = await signup(); const c1 = await connectWs(u.token); const c2 = await connectWs(u.token); const topic = `post:${newId()}`;
  c1.send({ type: "watch", ref: "w", data: { topics: [topic] } }); await c1.next("reply", (d) => d.ref === "w");
  const n = await env.TOPIC_HUB.getByName(topic).publish(topic, [{ type: "post.stats", data: { post_id: topic.slice(5), like_count: 1, repost_count: 0, reply_count: 0, quote_count: 0 }, ts: Date.now(), logged: false }]);
  expect(n).toBe(1); expect((await c1.next("post.stats")).seq).toBeNull();
  await settle(); expect(c2.frames.some((f) => f.type === "post.stats")).toBe(false);
});
it("visibleTo filters blocked watcher", async () => {
  const [author, w] = await Promise.all([signup(), signup()]); await blockSql(author, w); const topic = `post:${newId()}`;
  await env.TOPIC_HUB.getByName(topic).join(w.user.id, 300_000);
  expect(await env.TOPIC_HUB.getByName(topic).publish(topic, [replyEvent()], { visibleTo: { authors: [author.user.id], privateAuthors: [] } })).toBe(0);
});
it(">50 topics -> limit_exceeded; empty list leaves; lease expiry purges", async () => {
  const u = await signup(); const c = await connectWs(u.token);
  c.send({ type: "watch", ref: "big", data: { topics: Array.from({ length: 51 }, () => `post:${newId()}`) } });
  expect((await c.next("reply", (d) => d.ref === "big")).data.error.code).toBe("limit_exceeded");
  const topic = `post:${newId()}`; c.send({ type: "watch", ref: "a", data: { topics: [topic] } }); await c.next("reply", (d) => d.ref === "a");
  c.send({ type: "watch", ref: "b", data: { topics: [] } }); await c.next("reply", (d) => d.ref === "b");
  expect(await env.TOPIC_HUB.getByName(topic).inspect()).toEqual({ watchers: 0 });
  const t2 = `post:${newId()}`; const stub = env.TOPIC_HUB.getByName(t2); await stub.join(u.user.id, 1000);
  await runInDurableObject(stub, (inst: any) => { inst.now = () => Date.now() + 2000; }); await runDurableObjectAlarm(stub);
  expect(await stub.inspect()).toEqual({ watchers: 0 });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/realtime/topic-hub.test.ts` · Expected: FAIL.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/realtime/topic-hub.test.ts` · Expected: `Tests  3 passed (3)`
- [ ] **Step 5: Commit**

```bash
git add server/src/do server/test/realtime/topic-hub.test.ts
git commit -m "feat(server): TopicHub watcher leases and watch frame"
```

---

### Task 14: 로그아웃·세션 목록/폐기·비밀번호 변경

**Files:**
- Create: `server/src/services/users.ts` (`revokeSessions` 만)
- Modify: `server/src/routes/auth.ts`, `server/src/do/user-hub.ts` (`revokeSessions`)
- Test: `server/test/api/auth.test.ts`

**Interfaces:**
- Consumes: Task 6 auth, `serializeSession`; Task 9 소켓 태그 `sess:<id>`
- Produces: `services/users.ts` `revokeSessions(rt, userId, sessionIds | "all", reason, exceptSessionId?)` — D1 `revoked_at=now, revoke_reason` 먼저, 그다음 `USER_HUB.getByName(userId).revokeSessions(...)`. UserHub `revokeSessions(userId, sessionIds, reason)`: 대상 소켓에 `session.revoked{session_id, reason}`(휘발) 후 `close(reason === "suspended" ? 4003 : 4001)`. 라우트: `POST /auth/logout`(204), (`GET /auth/sessions` 는 Task 6 에서 만듦), `DELETE /auth/sessions/:session_id`(남의 것 404), `POST /auth/password`(현재 비밀번호 틀림 401 `invalid_credentials`, 새 비밀번호 규칙 = 가입과 동일, 현재 세션 제외 전부 `password_changed`).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
describe("sessions", () => {
  it("logout -> 401 and socket closed 4001 after session.revoked", async () => {
    const u = await signup(); const c = await connectWs(u.token); const cl = waitClose(c.ws);
    expect((await api(u.token).post("/v1/auth/logout")).status).toBe(204);
    expect((await c.next("session.revoked")).data.reason).toBe("logout"); expect((await cl).code).toBe(4001);
    expect((await api(u.token).get("/v1/auth/sessions")).status).toBe(401);
  });
  it("list and revoke other session; foreign session 404", async () => {
    const u = await signup(); const t2 = (await login(u.handle, u.password)).token; const other = await signup();
    const list = (await api(u.token).get<any>("/v1/auth/sessions")).body.items;
    expect(list).toHaveLength(2); expect(list.filter((s: any) => s.current)).toHaveLength(1);
    const target = list.find((s: any) => !s.current).id;
    expect((await api(other.token).del(`/v1/auth/sessions/${target}`)).status).toBe(404);
    expect((await api(u.token).del(`/v1/auth/sessions/${target}`)).status).toBe(204);
    expect((await api(t2).get("/v1/auth/sessions")).status).toBe(401);
  });
  it("password change revokes all other sessions only", async () => {
    const u = await signup(); const t2 = (await login(u.handle, u.password)).token;
    expect((await api(u.token).post<any>("/v1/auth/password", { current_password: "bad bad bad", new_password: "brand new pass" })).body.error.code).toBe("invalid_credentials");
    expect((await api(u.token).post("/v1/auth/password", { current_password: u.password, new_password: "brand new pass" })).status).toBe(204);
    expect((await api(u.token).get("/v1/auth/sessions")).status).toBe(200); expect((await api(t2).get("/v1/auth/sessions")).status).toBe(401);
    expect((await api().post("/v1/auth/login", { handle: u.handle, password: "brand new pass" })).status).toBe(200);
  });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/auth.test.ts` · Expected: FAIL (logout 404).
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/auth.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/auth.test.ts
git commit -m "feat(server): logout, session management and password change with socket revocation"
```

---

### Task 15: 알림함 행·데스크톱 alert 생성 (`lib/notify.ts`)

**Files:**
- Create: `server/src/lib/notify.ts`
- Modify: `server/src/lib/serialize.ts` (`serializeNotifications`)
- Test: `server/test/realtime/fanout.test.ts` (`describe("notify")`)

**Interfaces:**
- Consumes: Task 11 `publishEvent`, Task 7 `isBlockedEither`, Task 3 `plainSnippet`
- Produces (계약 4.12): `NotifyInput`, `notify(rt, input)`, `notifyMany(rt, inputs)`, `sendAlerts(rt, items)`; `fanoutMessageNotifications` 는 Task 24. `serializeNotifications(db, rows): Promise<Notification[]>` (계약 4.9).
  규칙: 행위자 = 수신자 또는 차단(양방향) → 아무것도 안 만듦. `INSERT OR IGNORE`(dedupe) 후 실제로 들어갔을 때만 `notification.created{notification, unread_count}`(기록). alert 는 `input.alert` 가 있고 설정 스위치가 켜졌을 때만 `alert{alert}`(기록) — 스위치 표: dm→`notify_dm`, mention→`notify_mention`, thread_reply→`notify_thread_reply`, invite→`notify_invite`, friend_request→`notify_friend_request`, social→`notify_social`, channel_message→채널 prefs. `sendAlerts` 는 `channelId` 가 있으면 `channel_prefs`(행 없으면 dm/group_dm `all`, text `mentions`) 의 `muted=1` 또는 `notify='none'` 이면 생략. alert `title` ≤80, `body` ≤200 코드 포인트, `id = newId()`, `suppressed:false`(DO 가 채움). `preview` ≤100.

**알림 입력 표 (이 계획이 만드는 모든 알림·alert — 각 작업은 이 값을 그대로 쓴다; plan 02 Task 2 의 테스트가 이 값에 기대고, 문구 형식은 plan 02 Global Constraints 의 alert 문구와 같은 꼴이다).** `{이름}` = 행위자 `display_name`. `title` 은 80 코드 포인트, `body` 는 200, `preview` 는 100 에서 자른다. `context` 에서 표에 없는 필드는 `null`. `target` 에서 표에 없는 필드는 `null`. `msgTitle` = Task 24 의 메시지 alert 제목 규칙(text `"{이름} · {방 이름} #{채널}"`, dm `"{이름}"`, group_dm `"{이름} · {그룹 이름 또는 멤버 이름 3명}"`). `msgTarget` = `{type:"message", id: 메시지 ID, channel_id, message_id: 메시지 ID, thread_root_id, space_id(dm/group_dm 은 null)}`.

| type (호출 위치) | 받는 사람 / actor | subject | context | preview | dedupeKey | alert (kind · title · body · target) |
|---|---|---|---|---|---|---|
| `friend_request` (Task 17 `POST /friends/requests` 201) | 받는 사람 / 보낸 사람 | `{type:"friend_request", id: request.id}` | — | `""` | `friend_request:<request_id>` | `friend_request` · `"{이름} 님의 친구 요청"` · `""` · `{type:"user", id: actor}` |
| `friend_accepted` (Task 17 `makeFriends`, 수동·자동 수락 둘 다) | 요청 보낸 사람 / 수락한 사람 | `{type:"user", id: 수락한 사람}` | — | `""` | `friend_accepted:<request_id>` | 없음(`alert: null`) |
| `space_invite` (Task 22 직접 초대 201) | 초대받은 사람 / 초대한 사람 | `{type:"space_direct_invite", id: invite.id}` | `space_id`, `space_name` | `""` | `space_invite:<invite_id>` | `invite` · `"{이름} 님이 {방 이름} 방에 초대했어요"` · `""` · `{type:"space", id: space_id, space_id}` |
| `message_mention` (Task 24 `fanoutMessageNotifications`, 편집으로 새로 생긴 멘션 포함) | 멘션 대상(`is_here` 포함, 1:1 dm 제외) / 작성자 | `{type:"message", id: message.id}` | `channel_id`, `message_id`, `thread_root_id`, `space_id`(dm/group_dm null) | `plainSnippet(body, 100)` | `message_mention:<message_id>:<user_id>` (편집 재해석이 같은 사람에게 두 번 만들지 않음) | `mention` · `msgTitle` · `plainSnippet(body, 200)` · `msgTarget` |
| `thread_reply` (Task 25, 멘션과 겹치면 생략) | 루트 작성자·이전 참여자 / 답글 작성자 | `{type:"message", id: reply.id}` | `channel_id`, `message_id: reply.id`, `thread_root_id: root.id`, `space_id` | `plainSnippet(reply.body, 100)` | `thread_reply:<reply_id>:<user_id>` | `thread_reply` · `msgTitle` · `plainSnippet(reply.body, 200)` · `msgTarget`(`thread_root_id` = 루트) |
| `follow_accepted` (Task 18 `approveAllPendingFollows`) | 대기 요청자 각자 / 공개로 바꾼 사람 | `{type:"user", id: 공개로 바꾼 사람}` | — | `""` | 없음(다시 요청·승인될 수 있음) | `social` · `"{이름} 님이 팔로우 요청을 수락했어요"` · `""` · `{type:"user", id: actor}` |
| (alert 만) `dm` — `sendAlerts` (Task 24) | 1:1/그룹 DM 의 다른 멤버 / 작성자 | — | — | — | — | `dm` · `msgTitle` · `plainSnippet(body, 200)` · `msgTarget`, `channelId` 지정(prefs 적용) |
| (alert 만) `channel_message` — `sendAlerts` (Task 24) | `notify:"all"` 로 설정한 text 채널 멤버(최상위 메시지만) / 작성자 | — | — | — | — | `channel_message` · `msgTitle` · `plainSnippet(body, 200)` · `msgTarget`, `channelId` 지정 |

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
describe("notify", () => {
  it("creates inbox row + notification.created + alert when switch on", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); const cb = await connectWs(b.token);
    await notify(rtFor(env), { userId: b.user.id, type: "friend_request", actorId: a.user.id, subject: { type: "friend_request", id: newId() }, alert: { kind: "friend_request", title: "t", body: "b", target: target("user", a.user.id) } });
    const n = await cb.next("notification.created"); expect(n.data.unread_count).toBe(1);
    expect(n.data.notification).toMatchObject({ type: "friend_request", actor: { id: a.user.id }, read: false, context: { post_id: null } });
    expect((await cb.next("alert")).data.alert).toMatchObject({ kind: "friend_request", actor: { id: a.user.id } });
  });
  it("switch off -> no alert; self or blocked -> nothing; dedupe", async () => {
    const [a, b] = await Promise.all([signup(), signup()]);
    await env.DB.prepare("UPDATE user_settings SET notify_social=0 WHERE user_id=?").bind(b.user.id).run(); const cb = await connectWs(b.token);
    const inp = { userId: b.user.id, type: "follow" as const, actorId: a.user.id, subject: { type: "user" as const, id: a.user.id }, dedupeKey: `follow:${a.user.id}`, alert: { kind: "social" as const, title: "t", body: "", target: target("user", a.user.id) } };
    await notify(rtFor(env), inp); await notify(rtFor(env), inp); await settle();
    expect(cb.frames.filter((f) => f.type === "notification.created")).toHaveLength(1);
    expect(cb.frames.some((f) => f.type === "alert")).toBe(false);
    await notify(rtFor(env), { ...inp, actorId: b.user.id, dedupeKey: undefined });
    await blockSql(b, a); await notify(rtFor(env), { ...inp, dedupeKey: "other" });
    expect((await env.DB.prepare("SELECT COUNT(*) n FROM notifications WHERE user_id=?").bind(b.user.id).first<any>()).n).toBe(1);
  });
  it("sendAlerts respects muted / notify:none channel prefs", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); const ch = await dmSql(a, b); const cb = await connectWs(b.token);
    await env.DB.prepare("INSERT INTO channel_prefs VALUES (?1, ?2, 'all', 1, 0)").bind(b.user.id, ch).run();
    await sendAlerts(rtFor(env), [{ userId: b.user.id, kind: "dm", actorId: a.user.id, title: "a", body: "hi", target: target("message", newId()), channelId: ch }]);
    await settle(); expect(cb.frames.some((f) => f.type === "alert")).toBe(false);
  });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/realtime/fanout.test.ts` · Expected: FAIL import.
- [ ] **Step 3: 구현** — Produces 대로. 같은 수신자의 두 이벤트는 `publishEvents` 로 RPC 1회.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/realtime/fanout.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src/lib/notify.ts server/src/lib/serialize.ts server/test/realtime/fanout.test.ts
git commit -m "feat(server): inbox notifications and desktop alert generation"
```

---

### Task 16: 나·프로필·설정·상태 메시지·사용자 조회

**Files:**
- Create: `server/src/routes/me.ts`, `server/src/routes/users.ts`, `server/src/lib/uploads.ts` (`r2Key`, `removeUploads` 만)
- Modify: `server/src/lib/serialize.ts` (`serializeProfile`), `server/src/app.ts`
- Test: `server/test/api/me.test.ts`, `server/test/api/users.test.ts`

**Interfaces:**
- Consumes: Task 6 `serializeMe`, `toUserSummary`; Task 7 `canDm`, `canViewUserContent`, `presenceVisible`; Task 11 `publishEvent`
- Produces:
  - `uploads.ts` (계약 4.13 일부): `r2Key(purpose, ownerId, uploadId, ext)` → `avatars|posts|stories/<owner>/<id>.<ext>`; `removeUploads(env, uploadIds)` → `MEDIA.delete(key)` + `status='removed', removed_at=now`.
  - `serializeProfile(db, viewerId, row): Promise<Profile>` — `relationship` 은 본인이면 `null`, 아니면 api.md 2장 `Relationship`(`shared_space_ids` ≤10, `blocked` 는 내가 막은 것만, `can_view_posts` = `canViewUserContent === "ok"`); `presence/last_seen_at` 은 `presenceVisible` 밖이면 `null`.
  - `meRoutes`: `GET /me`; `PATCH /me/profile`(display_name 1..32, bio 0..160, `avatar_upload_id`: 내 avatar·pending 업로드 → `attached`, `attached_to='avatar:<me>'`, 이전 아바타 `removeUploads`; `null` → 제거; 아니면 400 `fields.avatar_upload_id="invalid"`); `GET/PATCH /me/settings`(`is_private true→false` 처리는 Task 18 에서 연결); `PUT /me/status`(zod `{ emoji?: string | null, text: zText(0,64), expires_in_sec?: int 60..2592000 | null }` — emoji 는 생략·null 허용, 있으면 `isSingleEmoji(_,16)` 아니면 `fields.emoji="invalid"`; `expires_in_sec` 범위 밖은 `fields.expires_in_sec="invalid"`; `users.status_emoji/status_text/status_expires_at` 갱신; `200 {status: UserStatus}`(`expires_at` 포함)); `DELETE /me/status`(세 컬럼 NULL, `204`). 이벤트: `user.updated{user}`(`presenceWatchers` + `includeSelf`; profile·status PUT/DELETE 모두), `settings.updated{settings}`(나). **`/me/status` 두 경로는 이 작업이 완성본이다** — plan 02 Task 13 은 다시 등록하지 않고 `status-focus.test.ts` 로 고정만 한다. 모든 경로는 라우트별 `requireAuth`(`/me` 는 me·presence·plan 02 follows 가 같이 쓰는 접두사).
  - `userRoutes` (이 계획 부분): `GET /users/:user`(차단 관계여도 200, `can_dm=false`), `GET /users?ids=`(≤100), `GET /users/search?q=&limit=`(`RL_SEARCH`, handle 접두 + display_name 부분, 기본 10 최대 20, 차단·정지 제외).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// me.test.ts
it("profile patch emits user.updated to friends and self", async () => {
  const [a, b] = await Promise.all([signup(), signup()]); await makeFriendsSql(a, b); const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
  const r = await api(a.token).patch<any>("/v1/me/profile", { display_name: "새 이름", bio: "안녕" });
  expect(r.body.user).toMatchObject({ display_name: "새 이름", bio: "안녕" });
  expect((await cb.next("user.updated")).data.user.display_name).toBe("새 이름"); await ca.next("user.updated");
  expect((await api(a.token).patch<any>("/v1/me/profile", { display_name: "가".repeat(33) })).body.error.details.fields.display_name).toBe("too_long");
});
it("avatar attach replaces and removes previous R2 object", async () => {
  const a = await signup(); const [u1, u2] = [await rawUpload(a, "avatar"), await rawUpload(a, "avatar")]; // fixture: INSERT uploads + MEDIA.put
  await api(a.token).patch("/v1/me/profile", { avatar_upload_id: u1.id });
  const me = (await api(a.token).patch<any>("/v1/me/profile", { avatar_upload_id: u2.id })).body.user;
  expect(me.avatar_url).toBe(`/v1/media/${u2.id}`);
  expect(await env.MEDIA.get(u1.key)).toBeNull();
  expect((await env.DB.prepare("SELECT status FROM uploads WHERE id=?").bind(u1.id).first<any>()).status).toBe("removed");
});
it("settings patch + settings.updated; status with expiry", async () => {
  const a = await signup(); const c = await connectWs(a.token);
  const s = await api(a.token).patch<any>("/v1/me/settings", { dm_policy: "friends_only", notify: { social: true }, focus_default_min: 50 });
  expect(s.body.settings).toMatchObject({ dm_policy: "friends_only", notify: { social: true, dm: true }, focus_default_min: 50 });
  await c.next("settings.updated");
  const st = await api(a.token).put<any>("/v1/me/status", { emoji: "🎧", text: "듣는 중", expires_in_sec: 3600 });
  expect(st.body.status).toMatchObject({ emoji: "🎧", text: "듣는 중" });
  expect(Date.parse(st.body.status.expires_at) - Date.now()).toBeGreaterThan(3_590_000);
  expect((await api(a.token).put<any>("/v1/me/status", { emoji: "🎧🎧", text: "x" })).status).toBe(400);
  expect((await api(a.token).put<any>("/v1/me/status", { text: "🔨 배포 중" })).status).toBe(200); // emoji optional
  expect((await api(a.token).del("/v1/me/status")).status).toBe(204);
  expect((await api(a.token).get<any>("/v1/me")).body.user.status).toBeNull();
});
```
```ts
// users.test.ts
it("profile relationship, presence visibility, block keeps 200", async () => {
  const [a, b, c] = await Promise.all([signup(), signup(), signup()]); await makeFriendsSql(a, b);
  const pb = (await api(a.token).get<any>(`/v1/users/@${b.handle}`)).body.user;
  expect(pb.relationship).toMatchObject({ is_friend: true, can_dm: true, blocked: false, following: "none", can_view_posts: true });
  expect(pb.presence).toBe("offline");
  expect((await api(a.token).get<any>(`/v1/users/${c.user.id}`)).body.user.presence).toBeNull();
  expect((await api(a.token).get<any>("/v1/users/@me")).body.user.relationship).toBeNull();
  await blockSql(c, a); const pc = (await api(a.token).get<any>(`/v1/users/@${c.handle}`));
  expect(pc.status).toBe(200); expect(pc.body.user.relationship).toMatchObject({ can_dm: false, can_view_posts: false, blocked: false });
  expect((await api(a.token).get(`/v1/users/@nobody_${Date.now() % 1e6}`)).status).toBe(404);
});
it("search excludes blocked and suspended; ids lookup drops unknown", async () => {
  const pre = uniqueHandle("sr").slice(0, 8); const [me, x, y, z] = [await signup(), await signup({ handle: pre + "_x" }), await signup({ handle: pre + "_y" }), await signup({ handle: pre + "_z" })];
  await blockSql(y, me); await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), z.user.id).run();
  expect((await api(me.token).get<any>(`/v1/users/search?q=${pre}`)).body.items.map((u: any) => u.id)).toEqual([x.user.id]);
  expect((await api(me.token).get<any>(`/v1/users?ids=${x.user.id},${newId()}`)).body.items).toHaveLength(1);
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/me.test.ts test/api/users.test.ts` · Expected: FAIL 404.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/me.test.ts test/api/users.test.ts test/api/auth.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/me.test.ts server/test/api/users.test.ts
git commit -m "feat(server): me, profile, settings, status and user lookup routes"
```

---

### Task 17: 친구·친구 요청

**Files:**
- Create: `server/src/services/relations.ts` (`makeFriends`, `unfriend`), `server/src/routes/friends.ts`
- Modify: `server/src/lib/serialize.ts` (덧붙임 export), `server/src/app.ts`, `server/test/helpers.ts` (`makeFriends`)
- Test: `server/test/api/friends.test.ts`

**Interfaces:**
- Consumes: Task 15 `notify`; Task 7 `isBlockedEither`; Task 11 `publishEvent`
- Produces:
  - `relations.ts` (계약 4.14): `makeFriends(rt, a, b, requestId): Promise<void>` (batch: `friendships` INSERT(user_a<user_b) + 요청 `accepted`; 이벤트 `friend.request_accepted{request}`(보낸 사람), `friend.added{friend}`(각자에게 상대 정보); 알림 `friend_accepted`(보낸 사람)), `unfriend(rt, a, b, opts?: { silentTo?: string })` (`friend.removed{user_id}` 각자, `silentTo` 에게는 안 보냄).
  - 덧붙임 serialize: `serializeFriendRequests(db: D1Database, rows: FriendRequestRow[]): Promise<FriendRequest[]>`, `serializeFriends(db: D1Database, rows: Array<{ user_id: string; created_at: number }>): Promise<Friend[]>`(presence 포함).
  - `friendRoutes` (api.md 5.3): `GET /friends`(접속 중 먼저, 그다음 display_name; 커서 페이지), `DELETE /friends/:user`, `GET /friends/requests?direction=`(pending, ≤200), `POST /friends/requests`(`RL_SOCIAL`; 상대의 대기 요청 있으면 자동 수락 200, 아니면 201 + `friend.request_received` + 알림 `friend_request` + alert `friend_request`; 오류: 이미 친구/대기 409 `already_exists` `reason:"already_friends"|"pending"`, 자기 자신 409 `conflict` `reason:"self"`, 차단·정지 403 `action_not_allowed`, 보낸 대기 100 → 422 `pending_friend_requests`), `POST /friends/requests/:request_id/accept`(받는 사람만, 아니면 404), `/decline`(204, `friend.request_removed{reason:"declined"}` 양쪽, 알림 없음), `DELETE /friends/requests/:request_id`(보낸 사람, `canceled`).
  - 알림 `friend_request`·`friend_accepted` 의 입력값(subject·dedupeKey·alert 문구)은 Task 15 표 그대로(`friend_accepted` 는 alert 없음).
  - `helpers.makeFriends(a, b)`: 요청 + 수락 API 호출.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
it("request -> accept -> friends both sides with events", async () => {
  const [a, b] = await Promise.all([signup(), signup()]); const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
  const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  expect(r.status).toBe(201); expect(r.body).toMatchObject({ request: { status: "pending", from: { id: a.user.id }, to: { id: b.user.id } }, friend: null });
  await cb.next("friend.request_received"); expect((await cb.next("alert")).data.alert.kind).toBe("friend_request");
  const acc = await api(b.token).post<any>(`/v1/friends/requests/${r.body.request.id}/accept`);
  expect(acc.body.friend.user.id).toBe(a.user.id);
  await ca.next("friend.request_accepted"); expect((await ca.next("friend.added")).data.friend.user.id).toBe(b.user.id);
  expect((await api(a.token).get<any>("/v1/friends")).body.items.map((f: any) => f.user.id)).toEqual([b.user.id]);
});
it("crossing requests auto-accept with 200", async () => {
  const [a, b] = await Promise.all([signup(), signup()]);
  await api(a.token).post("/v1/friends/requests", { user: `@${b.handle}` });
  const r = await api(b.token).post<any>("/v1/friends/requests", { user: `@${a.handle}` });
  expect(r.status).toBe(200); expect(r.body.request.status).toBe("accepted"); expect(r.body.friend.user.id).toBe(a.user.id);
});
it("errors: self, duplicate, already friends, blocked, wrong accepter", async () => {
  const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
  expect((await api(a.token).post<any>("/v1/friends/requests", { user: "@me" })).body.error).toMatchObject({ code: "conflict", details: { reason: "self" } });
  const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  expect((await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` })).body.error.details.reason).toBe("pending");
  expect((await api(c.token).post(`/v1/friends/requests/${r.body.request.id}/accept`)).status).toBe(404);
  await blockSql(c, a); expect((await api(a.token).post<any>("/v1/friends/requests", { user: `@${c.handle}` })).body.error.code).toBe("action_not_allowed");
});
it("decline and cancel emit request_removed; unfriend emits friend.removed both sides", async () => {
  const [a, b, c] = await Promise.all([signup(), signup(), signup()]); const ca = await connectWs(a.token);
  const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  expect((await api(b.token).post(`/v1/friends/requests/${r.body.request.id}/decline`)).status).toBe(204);
  expect((await ca.next("friend.request_removed")).data).toEqual({ request_id: r.body.request.id, reason: "declined" });
  const r2 = await api(a.token).post<any>("/v1/friends/requests", { user: `@${c.handle}` }); const cc = await connectWs(c.token);
  expect((await api(c.token).del(`/v1/friends/requests/${r2.body.request.id}`)).status).toBe(404);      // only the sender cancels
  expect((await api(a.token).del(`/v1/friends/requests/${r2.body.request.id}`)).status).toBe(204);
  expect((await cc.next("friend.request_removed")).data).toEqual({ request_id: r2.body.request.id, reason: "canceled" });
  expect((await api(c.token).get<any>("/v1/friends/requests?direction=incoming")).body.items).toEqual([]);
  await makeFriends(a, b); const cb = await connectWs(b.token);
  expect((await api(a.token).del(`/v1/friends/@${b.handle}`)).status).toBe(204);
  expect((await cb.next("friend.removed")).data.user_id).toBe(a.user.id);
});
it("request lists by direction; 101st outgoing pending -> 422", async () => {
  const [a, b, c] = await Promise.all([signup(), signup(), signup()]);
  const out = (await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` })).body.request;
  const inc = (await api(c.token).post<any>("/v1/friends/requests", { user: `@${a.handle}` })).body.request;
  expect((await api(a.token).get<any>("/v1/friends/requests?direction=outgoing")).body.items.map((x: any) => x.id)).toEqual([out.id]);
  expect((await api(a.token).get<any>("/v1/friends/requests?direction=incoming")).body.items.map((x: any) => x.id)).toEqual([inc.id]);
  await seedPendingRequestsSql(a, 99);   // local fixture: 99 users rows + pending friend_requests from a (one INSERT … SELECT FROM json_each)
  const d = await signup();
  expect((await api(a.token).post<any>("/v1/friends/requests", { user: `@${d.handle}` })).body.error).toMatchObject({ code: "limit_exceeded", details: { what: "pending_friend_requests", limit: 100 } });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/friends.test.ts` · Expected: FAIL 404.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/friends.test.ts` · Expected: `Tests  5 passed (5)`
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/helpers.ts server/test/api/friends.test.ts
git commit -m "feat(server): friends and friend requests"
```

---

### Task 18: 차단·비공개 해제 일괄 승인

**Files:**
- Modify: `server/src/services/relations.ts` (`applyBlock`, `removeBlock`, `approveAllPendingFollows`)
- Create: `server/src/routes/blocks.ts`
- Modify: `server/src/routes/me.ts` (`PATCH /me/settings` 의 `is_private: true → false` 연결), `server/src/app.ts`
- Test: `server/test/api/blocks.test.ts`, `server/test/api/me.test.ts`

**Interfaces:**
- Consumes: Task 17 `unfriend`; Task 15 `notifyMany`
- Produces: `applyBlock(rt, blockerId, blockedId)` — 한 batch(api.md 3.2): `blocks` INSERT OR IGNORE, 친구 삭제, 양방향 대기 친구 요청 `canceled`, 양방향 follows(대기 포함) 삭제 + active 였던 것의 `follower_count/following_count` 감소, 양방향 대기 직접 초대 `canceled`. 이벤트: 차단한 사람에게만 `block.updated{user_id, blocked:true}`, 친구였으면 `friend.removed`(차단한 사람에게만). `removeBlock` → `block.updated{blocked:false}`. `approveAllPendingFollows(rt: Runtime, userId: string): Promise<void>` — **이 작업이 완성본**(plan 02 Task 3 은 다시 만들지 않고 고정만): pending 요청자 목록 읽기 → 한 batch(`UPDATE follows SET status='active', accepted_at=now WHERE followee_id=? AND status='pending'`, 요청자들의 `following_count + 1` 은 `json_each` 한 문, 내 `follower_count += n`) → 커밋 뒤 각 요청자에게 `notifyMany` `follow_accepted`(행위자 = userId; subject·alert 는 Task 15 표 — kind `social`, `"{이름} 님이 팔로우 요청을 수락했어요"`). `blockRoutes` (api.md 5.4): `GET /blocks`(최신순), `PUT /blocks/:user`(`RL_SOCIAL`, 204, 자기 자신 409 `conflict` `self`), `DELETE /blocks/:user`(204).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// blocks.test.ts
it("block cleans relations in one go and only the blocker hears about it", async () => {
  const [a, b] = await Promise.all([signup(), signup()]); await makeFriends(a, b);
  await followSql(a, b, "active"); await followSql(b, a, "pending");
  const [ca, cb] = await Promise.all([connectWs(a.token), connectWs(b.token)]);
  expect((await api(a.token).put(`/v1/blocks/@${b.handle}`)).status).toBe(204);
  expect((await ca.next("block.updated")).data).toEqual({ user_id: b.user.id, blocked: true });
  await ca.next("friend.removed"); await settle();
  expect(cb.frames.some((f) => ["friend.removed", "block.updated"].includes(f.type))).toBe(false);
  const q = (sql: string) => env.DB.prepare(sql).bind(a.user.id, b.user.id).first<any>();
  expect((await q("SELECT COUNT(*) n FROM friendships WHERE user_a IN (?1,?2) AND user_b IN (?1,?2)")).n).toBe(0);
  expect((await q("SELECT COUNT(*) n FROM follows WHERE follower_id IN (?1,?2) AND followee_id IN (?1,?2)")).n).toBe(0);
  expect((await env.DB.prepare("SELECT follower_count FROM users WHERE id=?").bind(b.user.id).first<any>()).follower_count).toBe(0);
  expect((await api(a.token).get<any>("/v1/blocks")).body.items[0].user.id).toBe(b.user.id);
  expect((await api(a.token).del(`/v1/blocks/@${b.handle}`)).status).toBe(204);
  expect((await api(a.token).get<any>(`/v1/users/@${b.handle}`)).body.user.relationship.is_friend).toBe(false); // not restored
});
it("pending friend requests both ways become canceled", async () => {
  const [a, b] = await Promise.all([signup(), signup()]); const r = await api(b.token).post<any>("/v1/friends/requests", { user: `@${a.handle}` });
  await api(a.token).put(`/v1/blocks/@${b.handle}`);
  expect((await env.DB.prepare("SELECT status FROM friend_requests WHERE id=?").bind(r.body.request.id).first<any>()).status).toBe("canceled");
});
```
```ts
// me.test.ts
it("going public approves pending follow requests and notifies requesters", async () => {
  const [p, r1] = await Promise.all([signup(), signup()]); await api(p.token).patch("/v1/me/settings", { is_private: true });
  await followSql(r1, p, "pending"); const c = await connectWs(r1.token);
  await api(p.token).patch("/v1/me/settings", { is_private: false });
  expect((await c.next("notification.created")).data.notification.type).toBe("follow_accepted");
  expect((await env.DB.prepare("SELECT status FROM follows WHERE follower_id=?").bind(r1.user.id).first<any>()).status).toBe("active");
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/blocks.test.ts test/api/me.test.ts` · Expected: FAIL.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: 같은 명령 · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/blocks.test.ts server/test/api/me.test.ts
git commit -m "feat(server): blocking with relation cleanup and public-switch follow approval"
```

---

### Task 19: 채널 직렬화·DM·그룹 DM

**Files:**
- Modify: `server/src/lib/serialize.ts` (`serializeChannels`, `loadUnreadStates`)
- Create: `server/src/services/membership.ts` (`addChannelMembers`, `removeChannelMember` — group_dm 경로), `server/src/routes/dms.ts`, `server/src/routes/channels.ts` (이 작업: `GET /channels/:channel_id`, `POST|DELETE /channels/:channel_id/members…` 의 group_dm 분기)
- Modify: `server/src/app.ts`
- Test: `server/test/api/dms.test.ts`, `server/test/api/permissions.test.ts` (`describe("dm api")`)

**Interfaces:**
- Consumes: Task 7 `canDm`, `canDmMany`, `getChannelAccess`; Task 11 `publishEvent`
- Produces:
  - `serializeChannels(db, viewerId | null, rows): Promise<Channel[]>` (계약 4.9) — dm/group_dm `members`(전원), text `null`; 공개 text `member_count` = 방 `member_count`; `read_only/read_only_reason` 은 `viewerId` 기준(`null` 이면 기본값 events.md 4.4); `my` = prefs(기본 dm/group_dm `all`, text `mentions`) + `loadUnreadStates`.
  - 덧붙임: `loadUnreadStates(db: D1Database, userId: string, channelIds: string[]): Promise<Map<string, UnreadState>>` — data-model 3.4 비용 규칙(① `last_message_id > 기준점` 채널만 ② 채널별 `LIMIT 1000` 하위 질의, 999 상한; unread = 기준점 이후 최상위·비시스템·남이 쓴 것; mention = 기준점 이후 최상위 `message_mentions(in_thread=0)` 에 나; 기준점 = `channel_reads` 또는 `idFloor(joined_at)`).
  - `membership.ts` (계약 4.14 일부): `addChannelMembers(rt, channelId, actorId, userIds): Promise<Channel>`, `removeChannelMember(rt, channelId, actorId, userId): Promise<void>` — 이 작업은 group_dm 규칙(api.md 3.1·5.6; 총원 10 초과 422 `group_dm_members`; 추가자 `can_dm` + 기존 멤버와 차단 → 403 `dm_not_allowed` `details.user_ids`; 이미 멤버 409 `already_member`; 제거는 owner_id 또는 본인, 그 외 403 `forbidden`). 비공개 text 분기는 Task 23. group_dm 시스템 메시지(`group_member_added/removed`) 호출은 `postSystemMessage` 가 생기는 Task 20 에서 연결한다.
  - `dmRoutes` (api.md 5.6): `GET /dms`(최신 메시지순, 차단한 상대의 1:1 제외), `POST /dms`(`RL_SOCIAL`; 1명: 기존 있으면 200, 없으면 201, `dm_key`, `can_dm` 실패 403 `dm_not_allowed`; 2..9명: 새 group_dm 201, 실패 ID `details.user_ids`; `channel.created` 모든 참여자).
  - `channelRoutes`: `GET /channels/:channel_id`(읽기 권한), 멤버 추가/제거 경로(1:1 dm·공개 text → 409 `conflict` `reason:"not_supported"`). 이벤트: `channel.member_added{channel_id, user}`(기존 멤버), `channel.created`(새 멤버), `channel.member_removed`(남은 멤버), `channel.deleted{reason:"removed"}`(빠진 사람).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// dms.test.ts
it("1:1 dm create is idempotent and requires can_dm", async () => {
  const [a, b, c] = await Promise.all([signup(), signup(), signup()]); await makeFriends(a, b);
  const r1 = await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] }); expect(r1.status).toBe(201);
  expect(r1.body.channel).toMatchObject({ kind: "dm", space_id: null, name: null, read_only: false, my: { notify: "all", unread_count: 0 } });
  expect(r1.body.channel.members.map((m: any) => m.id).sort()).toEqual([a.user.id, b.user.id].sort());
  const r2 = await api(b.token).post<any>("/v1/dms", { users: [`@${a.handle}`] }); expect(r2.status).toBe(200); expect(r2.body.channel.id).toBe(r1.body.channel.id);
  expect((await api(a.token).post<any>("/v1/dms", { users: [`@${c.handle}`] })).body.error.code).toBe("dm_not_allowed");
});
it("group dm: 3..10, failing ids reported, channel.created to all", async () => {
  const me = await signup(); const fs = await Promise.all(Array.from({ length: 9 }, () => signup())); for (const f of fs) await makeFriends(me, f);
  const c1 = await connectWs(fs[0].token); const stranger = await signup();
  const bad = await api(me.token).post<any>("/v1/dms", { users: [`@${fs[0].handle}`, `@${stranger.handle}`] });
  expect(bad.body.error).toMatchObject({ code: "dm_not_allowed", details: { user_ids: [stranger.user.id] } });
  const g = await api(me.token).post<any>("/v1/dms", { users: fs.map((f) => `@${f.handle}`), name: "점심팟" });
  expect(g.status).toBe(201); expect(g.body.channel).toMatchObject({ kind: "group_dm", name: "점심팟", owner_id: me.user.id, member_count: 10 });
  await c1.next("channel.created");
  const extra = await signup(); await makeFriends(me, extra);
  expect((await api(me.token).post<any>(`/v1/channels/${g.body.channel.id}/members`, { users: [`@${extra.handle}`] })).body.error.details).toEqual({ what: "group_dm_members", limit: 10 });
});
it("group dm member removal: owner or self only; dm list hides blocked 1:1", async () => {
  const [o, x, y] = await Promise.all([signup(), signup(), signup()]); await makeFriends(o, x); await makeFriends(o, y);
  const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel;
  expect((await api(x.token).del(`/v1/channels/${g.id}/members/@${y.handle}`)).status).toBe(403);
  const cy = await connectWs(y.token);
  expect((await api(y.token).del(`/v1/channels/${g.id}/members/@me`)).status).toBe(204);
  expect((await cy.next("channel.deleted")).data).toEqual({ channel_id: g.id, space_id: null, reason: "removed" });
  const d = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`] })).body.channel;
  await api(o.token).put(`/v1/blocks/@${x.handle}`);
  expect((await api(o.token).get<any>("/v1/dms")).body.items.map((c: any) => c.id)).not.toContain(d.id);
  expect((await api(x.token).get<any>(`/v1/channels/${d.id}`)).body.channel).toMatchObject({ read_only: true, read_only_reason: "not_eligible" });
});
```
```ts
// permissions.test.ts
describe("dm api", () => {
  it("unfriend turns 1:1 dm read-only (send -> 403 read_only)", async () => {
    const [a, b] = await Promise.all([signup(), signup()]); await makeFriends(a, b);
    const d = (await api(a.token).post<any>("/v1/dms", { users: [`@${b.handle}`] })).body.channel;
    await api(a.token).del(`/v1/friends/@${b.handle}`);
    expect((await api(a.token).get<any>(`/v1/channels/${d.id}`)).body.channel).toMatchObject({ read_only: true, read_only_reason: "not_eligible" });
  });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/dms.test.ts test/api/permissions.test.ts` · Expected: FAIL.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: 같은 명령 · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/dms.test.ts server/test/api/permissions.test.ts
git commit -m "feat(server): channel serialization with unread state, dms and group dms"
```

---

### Task 20: 메시지 전송·목록·조회 (+주사위·투표 첨부·시스템 메시지)

**Files:**
- Create: `server/src/services/messages.ts` (`createMessage`, `postSystemMessage`), `server/src/services/polls.ts` (`pollInsertStmts` 만), `server/src/routes/messages.ts` (전송·목록·단건)
- Modify: `server/src/lib/serialize.ts` (`serializeMessages`, `serializePolls`), `server/src/services/membership.ts` (group_dm 시스템 메시지 연결), `server/src/app.ts`
- Test: `server/test/api/messages.test.ts`

**Interfaces:**
- Consumes: Task 7 `requireChannelWrite/Read`; Task 3 `parseDice`, `rollDice`, `extractMentions`, `hasHereMention`; Task 11 `publishEvent`
- Produces:
  - `services/messages.ts` (계약 4.14): `CreateMessageInput`, `createMessage(rt, input): Promise<{ message: Message; created: boolean }>` — 한 batch: (poll 이면 `pollInsertStmts`) + `messages` INSERT + `channels.last_message_id/at` 조건부 + 작성자 `channel_reads` 전진(더 클 때만) + 멘션 행(Task 24 가 해석 규칙 추가; 이 작업은 빈 목록). nonce 충돌(`uq_messages_nonce`) → 기존 메시지 `created:false`. 이벤트 `message.created{message}`(청중 `channel`, 기록). `postSystemMessage(rt, channelId, actorId, system)` — `kind:"system"`, `meta` = SystemEvent JSON, nonce 없음.
  - `services/polls.ts` (계약 4.14 일부): `pollInsertStmts(db, pollId, creatorId, ctx, input, nowMs): D1PreparedStatement[]` (`closes_at = now + duration_sec*1000` 또는 null, 선택지 2..10, 각 1..80, 질문 1..200 — 검증은 호출 전 zod).
  - `serializeMessages(db, viewerId | null, rows): Promise<Message[]>` — 작성자·리액션(이모지별 `MIN(created_at)` 순, 같으면 그 이모지 첫 행의 rowid 순, `me`)·고정·투표·명시 멘션(`is_here=0`)·`thread`(reply_count ≥1 일 때; `participant_ids` 최근순 ≤5) 일괄 로드; `roll`·`system` 은 `meta` 에서; 삭제 묘비는 `body:""`, `deleted:true`. `serializePolls(db, viewerId | null, pollIds): Promise<Map<string, Poll>>` (`closed` = `closed_at` 있음 또는 `closes_at <= now`, `my_votes` 는 viewer null 이면 `[]`).
  - `messageRoutes` (api.md 5.10): `POST /channels/:channel_id/messages`(`RL_MESSAGE`; `kind` text|me|roll|poll; text/me body 1..4000; roll body 는 `parseDice` 실패 → 400 `fields.body="invalid"`, meta = `rollDice` 결과; poll 은 `poll` 필수·body 무시), `GET /channels/:channel_id/messages?before|after|around&limit`(기본 50 최대 100, 최상위만, 항상 ID 오름차순, `has_more_before/after`, 둘 이상 지정 → 400; 삭제된 것은 답글 있을 때만 묘비), `GET /messages/:message_id`(답글 포함, 읽기 권한).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// fixture: dmPair() -> {a, b, ch} (friends + POST /v1/dms)
it("send 201, duplicate nonce 200, event to both", async () => {
  const { a, b, ch } = await dmPair(); const cb = await connectWs(b.token);
  const body = { client_nonce: "n-1", body: "  안녕 **세상**  " };
  const r1 = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, body);
  expect(r1.status).toBe(201); expect(r1.body.message).toMatchObject({ kind: "text", body: "안녕 **세상**", client_nonce: "n-1", thread: null, reactions: [], pinned: false, deleted: false, edited_at: null });
  const r2 = await api(a.token).post<any>(`/v1/channels/${ch}/messages`, body);
  expect(r2.status).toBe(200); expect(r2.body.message.id).toBe(r1.body.message.id);
  expect((await cb.next("message.created")).data.message.id).toBe(r1.body.message.id);
  expect((await api(a.token).get<any>(`/v1/channels/${ch}`)).body.channel.last_message_id).toBe(r1.body.message.id);
});
it("validation: empty body, 4001 chars, emoji counted per code point, bad dice", async () => {
  const { a, ch } = await dmPair(); const p = (b: any) => api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: crypto.randomUUID().slice(0, 8), ...b });
  expect((await p({ body: "   " })).body.error.details.fields.body).toBe("too_short");
  expect((await p({ body: "가".repeat(4001) })).body.error.details.fields.body).toBe("too_long");
  expect((await p({ body: "😀".repeat(4000) })).status).toBe(201);
  expect((await p({ kind: "roll", body: "2d1" })).body.error.details.fields.body).toBe("invalid");
  const roll = (await p({ kind: "roll", body: "2d6+1" })).body.message; expect(roll.roll).toMatchObject({ expr: "2d6+1", modifier: 1 }); expect(roll.roll.rolls).toHaveLength(2);
  const poll = (await p({ kind: "poll", poll: { question: "점심?", options: ["국밥", "라멘"] } })).body.message;
  expect(poll.poll).toMatchObject({ question: "점심?", multi: false, closed: false, my_votes: [], options: [{ idx: 0, text: "국밥", vote_count: 0 }, { idx: 1, text: "라멘", vote_count: 0 }] });
});
it("pagination: latest, before, after, around; ascending; has_more flags", async () => {
  const { a, ch } = await dmPair(); const ids: string[] = [];
  for (let i = 0; i < 12; i++) ids.push((await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: `p${i}`, body: `m${i}` })).body.message.id);
  const latest = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?limit=5`)).body;
  expect(latest.items.map((m: any) => m.id)).toEqual(ids.slice(7)); expect(latest).toMatchObject({ has_more_before: true, has_more_after: false });
  expect((await api(a.token).get<any>(`/v1/channels/${ch}/messages?before=${ids[7]}&limit=5`)).body.items.map((m: any) => m.id)).toEqual(ids.slice(2, 7));
  expect((await api(a.token).get<any>(`/v1/channels/${ch}/messages?after=${ids[9]}`)).body.items.map((m: any) => m.id)).toEqual(ids.slice(10));
  const ar = (await api(a.token).get<any>(`/v1/channels/${ch}/messages?around=${ids[5]}&limit=4`)).body.items.map((m: any) => m.id);
  expect(ar).toContain(ids[5]); expect(ar).toEqual([...ar].sort());
  expect((await api(a.token).get(`/v1/channels/${ch}/messages?before=${ids[1]}&after=${ids[0]}`)).status).toBe(400);
});
it("non-member gets 404; read-only dm send -> 403 read_only", async () => {
  const { a, b, ch } = await dmPair(); const x = await signup();
  expect((await api(x.token).get(`/v1/channels/${ch}/messages`)).status).toBe(404);
  await api(a.token).del(`/v1/friends/@${b.handle}`);
  expect((await api(b.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "z", body: "hi" })).body.error.code).toBe("read_only");
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/messages.test.ts` · Expected: FAIL 404.
- [ ] **Step 3: 구현** — Produces 대로. `around`: 앵커 이하 `ceil(limit/2)` 개 + 이후 `floor(limit/2)` 개. Task 19 의 group_dm 멤버 변경에 `postSystemMessage` 연결.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/messages.test.ts test/api/dms.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/messages.test.ts
git commit -m "feat(server): send/list/get messages with nonce idempotency, rolls, polls and system messages"
```

---

### Task 21: 방(Space)·멤버·역할·강퇴·밴·이전·삭제

**Files:**
- Create: `server/src/routes/spaces.ts`
- Modify: `server/src/services/membership.ts` (`joinSpace`, `removeFromSpace`), `server/src/lib/serialize.ts` (`serializeSpace`, `serializeSpaceDetail`, `serializeSpaceMembers`), `server/src/app.ts`
- Test: `server/test/api/spaces.test.ts`

**Interfaces:**
- Consumes: Task 20 `postSystemMessage`; Task 7 `requireSpaceRole`; Task 19 `serializeChannels`
- Produces:
  - `joinSpace(rt, spaceId, userId, via): Promise<SpaceDetail>` — 밴 → 403 `banned_from_space`, 이미 멤버 → 409 `already_member`, 5000명 → 422 `space_members`; batch: `space_members` + `member_count+1` (+초대 `use_count` 조건부 UPDATE 는 Task 22 가 `via` 로 전달). 이벤트 `space.joined{space}`(나), `space.member_joined{space_id, member}`(방), 시스템 메시지 `member_joined`(기본 채널).
  - `removeFromSpace(rt, spaceId, userId, actorId, reason, banReason?)` — 한 batch: `space_members` DELETE + 그 방 모든 비공개 채널의 내 `channel_members` DELETE(+`member_count` 감소) + `spaces.member_count` 감소 + (banned) `space_bans`. 이벤트: `space.member_left{space_id, user_id, reason}`(방 + 나간 사람), `space.removed{space_id, reason}`(나간 사람), 빠진 비공개 채널마다 `channel.member_removed`(남은 채널 멤버), 시스템 메시지 `member_left`/`member_kicked`(밴도 kicked).
  - `serializeSpace(db, viewerId | null, row): Promise<Space>`, `serializeSpaceDetail(db, viewerId, row): Promise<SpaceDetail>`(볼 수 있는 text 채널, position 순). 덧붙임: `serializeSpaceMembers(db: D1Database, rows: SpaceMemberRow[]): Promise<SpaceMember[]>`.
  - `spaceRoutes` (api.md 5.7 + 5.9 앞 두 경로): `GET /spaces`(가입순), `POST /spaces`(`RL_SOCIAL`; batch 순서 data-model 3.8: spaces(미리 만든 default_channel_id) → channels(`일반`, 공개) → space_members(owner); 소유 100 → 422 `owned_spaces`; 201 + `space.joined`), `GET|PATCH|DELETE /spaces/:space_id`(PATCH owner·admin → `space.updated{space}`; DELETE owner → **batch 전에** 방 멤버 ID 전부를 읽어 두고, 한 batch 로 방·채널 소프트 삭제, 커밋 뒤 `{users: 그 ID들}` 로 `space.deleted{space_id}` — `{space}` 청중은 삭제된 방을 거르므로 쓰지 않는다), `POST /spaces/:space_id/transfer`(owner → 대상 owner, 이전 owner admin; `space.member_updated`×2 + `space.updated`), `GET /spaces/:space_id/members?q&role&cursor&limit`(역할 순 → display_name), `PATCH /spaces/:space_id/members/:user`(owner; `role` admin|member), `DELETE /spaces/:space_id/members/:user?ban&reason`(@me 나가기, owner 나가기 409 `owner_cannot_leave`; 강퇴 권한 api.md 3.5), `GET /spaces/:space_id/bans`, `DELETE /spaces/:space_id/bans/:user`, `GET /spaces/:space_id/channels`.
  - 이 작업의 테스트는 지역 픽스처 `spaceViaApi(owner)`(POST /v1/spaces) 와 `joinSql(spaceId, user)`(space_members INSERT + member_count+1) 로 멤버를 넣는다. `helpers.makeSpace` 는 초대 링크가 생기는 Task 22 에서 추가한다.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
it("create space: owner, default channel 일반, space.joined", async () => {
  const a = await signup(); const c = await connectWs(a.token);
  const r = await api(a.token).post<any>("/v1/spaces", { name: " 노바 개발방 ", icon_emoji: "🚀" });
  expect(r.status).toBe(201);
  expect(r.body.space).toMatchObject({ name: "노바 개발방", my_role: "owner", member_count: 1, channels: [{ kind: "text", name: "일반", is_private: false }] });
  expect(r.body.space.default_channel_id).toBe(r.body.space.channels[0].id);
  await c.next("space.joined");
});
it("roles: admin can kick members but not admins; owner cannot leave; transfer", async () => {
  const [o, ad, m1, m2] = await Promise.all([signup(), signup(), signup(), signup()]); const s = await spaceViaApi(o); for (const u of [ad, m1, m2]) await joinSql(s.id, u);
  await api(o.token).patch(`/v1/spaces/${s.id}/members/@${ad.handle}`, { role: "admin" });
  expect((await api(m1.token).del(`/v1/spaces/${s.id}/members/@${m2.handle}`)).status).toBe(403);
  const cm2 = await connectWs(m2.token);
  expect((await api(ad.token).del(`/v1/spaces/${s.id}/members/@${m2.handle}?ban=true&reason=spam`)).status).toBe(204);
  expect((await cm2.next("space.removed")).data).toEqual({ space_id: s.id, reason: "banned" });
  expect((await api(ad.token).get<any>(`/v1/spaces/${s.id}/bans`)).body.items[0]).toMatchObject({ user: { id: m2.user.id }, reason: "spam" });
  expect((await api(o.token).del<any>(`/v1/spaces/${s.id}/members/@me`)).body.error.details.reason).toBe("owner_cannot_leave");
  await api(o.token).post(`/v1/spaces/${s.id}/transfer`, { user: `@${m1.handle}` });
  expect((await api(o.token).get<any>(`/v1/spaces/${s.id}`)).body.space.my_role).toBe("admin");
});
it("leaving removes private channel memberships and their events stop", async () => {
  const [o, m] = await Promise.all([signup(), signup()]); const s = await spaceViaApi(o); await joinSql(s.id, m);
  const priv = await privateTextSql(s.id, [o, m]); await api(m.token).del(`/v1/spaces/${s.id}/members/@me`);
  expect((await env.DB.prepare("SELECT COUNT(*) n FROM channel_members WHERE channel_id=? AND user_id=?").bind(priv, m.user.id).first<any>()).n).toBe(0);
  expect((await api(m.token).get(`/v1/channels/${priv}`)).status).toBe(404);
  const cm = await connectWs(m.token); await api(o.token).post(`/v1/channels/${priv}/messages`, { client_nonce: "x", body: "secret" });
  await settle(); expect(cm.frames.some((f) => f.type === "message.created")).toBe(false);
  const general = (await api(o.token).get<any>(`/v1/channels/${s.default_channel_id}/messages`)).body.items.at(-1);
  expect(general.system).toMatchObject({ event: "member_left", actor_id: m.user.id });
});
it("delete space: members get space.deleted; channels 404 afterwards", async () => {
  const [o, m] = await Promise.all([signup(), signup()]); const s = await spaceViaApi(o); await joinSql(s.id, m); const cm = await connectWs(m.token);
  expect((await api(m.token).del(`/v1/spaces/${s.id}`)).status).toBe(403);
  expect((await api(o.token).del(`/v1/spaces/${s.id}`)).status).toBe(204);
  expect((await cm.next("space.deleted")).data).toEqual({ space_id: s.id }); expect((await api(m.token).get(`/v1/channels/${s.default_channel_id}`)).status).toBe(404);
});
it("list, patch + space.updated, member list order and q, unban", async () => {
  const [o, ad, m, x] = await Promise.all([signup({ displayName: "오너" }), signup({ displayName: "가관리" }), signup({ displayName: "나멤버" }), signup()]);
  const s = await spaceViaApi(o); for (const u of [ad, m, x]) await joinSql(s.id, u);
  await api(o.token).patch(`/v1/spaces/${s.id}/members/@${ad.handle}`, { role: "admin" });
  expect((await api(m.token).get<any>("/v1/spaces")).body.items.map((sp: any) => sp.id)).toContain(s.id);
  const cm = await connectWs(m.token);
  expect((await api(m.token).patch(`/v1/spaces/${s.id}`, { name: "x" })).status).toBe(403);
  expect((await api(ad.token).patch<any>(`/v1/spaces/${s.id}`, { name: "새 이름", icon_emoji: null })).body.space).toMatchObject({ name: "새 이름" });
  expect((await cm.next("space.updated")).data.space.name).toBe("새 이름");
  const members = (await api(m.token).get<any>(`/v1/spaces/${s.id}/members`)).body.items;
  expect(members.slice(0, 2).map((mm: any) => mm.role)).toEqual(["owner", "admin"]); expect(members).toHaveLength(4);
  expect((await api(m.token).get<any>(`/v1/spaces/${s.id}/members?q=${encodeURIComponent("나멤")}`)).body.items.map((mm: any) => mm.user.id)).toEqual([m.user.id]);
  await api(o.token).del(`/v1/spaces/${s.id}/members/@${x.handle}?ban=true`);
  expect((await api(m.token).del(`/v1/spaces/${s.id}/bans/@${x.handle}`)).status).toBe(403);
  expect((await api(ad.token).del(`/v1/spaces/${s.id}/bans/@${x.handle}`)).status).toBe(204);
  expect((await api(o.token).get<any>(`/v1/spaces/${s.id}/bans`)).body.items).toEqual([]);
});
```

(`signup({ displayName })` 은 계약 7.2 `signup(opts?)` 의 선택 필드.)

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/spaces.test.ts` · Expected: FAIL 404.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/spaces.test.ts` · Expected: `Tests  5 passed (5)`
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/helpers.ts server/test/api/spaces.test.ts
git commit -m "feat(server): spaces, roles, membership removal, bans and transfer"
```

---

### Task 22: 초대 링크·직접 초대

**Files:**
- Create: `server/src/routes/invites.ts`
- Modify: `server/src/lib/serialize.ts` (`serializeInvites`, `serializeInvitePreview`, `serializeDirectInvites`), `server/src/services/membership.ts` (`joinSpace` 의 `via`), `server/src/app.ts`
- Test: `server/test/api/invites.test.ts`

**Interfaces:**
- Consumes: Task 21 `joinSpace`; Task 15 `notify`; Task 7 `areFriends`, `isBlockedEither`
- Produces:
  - 덧붙임 serialize: `serializeInvites(db: D1Database, rows: SpaceInviteRow[]): Promise<Invite[]>`, `serializeInvitePreview(db: D1Database, viewerId: string, row: SpaceInviteRow): Promise<InvitePreview>`, `serializeDirectInvites(db: D1Database, rows: SpaceDirectInviteRow[]): Promise<DirectInvite[]>`.
  - 수락 검사 순서(테스트가 이 순서를 가정): 초대 유효성 410 → 밴 403 `banned_from_space` → 이미 멤버 409 `already_member` → 정원 422.
  - **`joinSpace` 의 경합 안전 batch (정확히 이 3문, 이 순서).** D1 batch 는 한 트랜잭션 안에서 순서대로 실행되고 `changes()` 는 **직전 문**의 영향 행 수다. batch 가 끝난 뒤에는 되돌릴 수 없으므로, "실패"는 뒤 문들이 아무것도 쓰지 않는 것으로 표현한다.
    - `{ inviteCode }`:
      1. `UPDATE space_invites SET use_count = use_count + 1 WHERE code = ?1 AND space_id = ?2 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > ?3) AND (max_uses IS NULL OR use_count < max_uses)`
      2. `INSERT INTO space_members (space_id, user_id, role, joined_at) SELECT ?2, ?4, 'member', ?3 WHERE changes() > 0`
      3. `UPDATE spaces SET member_count = member_count + 1, updated_at = ?3 WHERE id = ?2 AND changes() > 0` (2번이 넣었을 때만)
    - `{ directInviteId }`:
      1. `UPDATE space_direct_invites SET status = 'accepted', responded_at = ?3 WHERE id = ?1 AND space_id = ?2 AND invitee_id = ?4 AND status = 'pending'`
      2·3. 위와 같음.
    - 커밋 뒤 `results[0].meta.changes === 0` 이면 아무것도 쓰이지 않은 것 → 초대 링크는 초대 행을 다시 읽어 `410 invite_invalid` `reason` = `revoked`(revoked_at) > `expired`(expires_at ≤ now) > `exhausted`(그 밖), 직접 초대는 `404 not_found`(이미 처리·취소됨). 이벤트·시스템 메시지는 `results[1].meta.changes === 1` 일 때만 보낸다. 사전 검사(아래 순서)를 통과한 뒤 같은 사용자가 동시에 가입해 2번 INSERT 가 PK 충돌로 batch 전체를 실패시키면(전부 롤백) `409 already_member` 로 바꾼다. `joinSpace(rt, spaceId, userId, {})`(via 없음, Task 21 경로)는 1번 없이 2·3번만(`WHERE changes() > 0` 대신 조건 없음).
    - 정원 422 는 사전 검사(`member_count >= 5000`)만으로 판정한다(동시 가입 경합으로 상한을 한두 명 넘을 수 있다 — 초대 사용 횟수와 가입을 한 묶음으로 유지하려는 이 계획의 결정).
  - `inviteRoutes` (api.md 5.8): `GET|POST /spaces/:space_id/invites`(owner·admin; `max_uses` 1..1000|null, `expires_in_sec` 300..2592000, 생략 604800, null 무기한), `DELETE /invites/:code`, `GET /invites/:code`(무효 410 `invite_invalid` `reason` expired|exhausted|revoked, 없음 404), `POST /invites/:code/accept`, `POST /spaces/:space_id/direct-invites`(`RL_SOCIAL`; 친구 아님/차단 403 `action_not_allowed`, 멤버 409 `already_member`, 대기 중 409 `already_exists`, 밴 403 `banned_from_space`; `space.invite_received{invite}` + 알림 `space_invite` + alert `invite` — 입력값은 Task 15 표), `GET /me/direct-invites`, `POST /direct-invites/:invite_id/accept`(위 `{ directInviteId }` batch), `POST /direct-invites/:invite_id/decline`(초대받은 사람만, 아니면 404; `UPDATE … SET status='declined' WHERE id=? AND status='pending'`, 0행이면 404), `DELETE /direct-invites/:invite_id`(초대한 사람 또는 그 방 owner·admin; 그 방 멤버가 아니면 404, 방의 일반 멤버면 403 `forbidden`; `status='canceled'` 같은 조건부 UPDATE, 0행이면 404). `space.invite_removed{invite_id, reason}` 는 초대받은·초대한 사람 둘 다.
  - `helpers.makeSpace` 완성(링크 생성 + 각 멤버 accept).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
it("link: create (default 7d), preview, accept, system message, use_count", async () => {
  const [o, m] = await Promise.all([signup(), signup()]); const s = await spaceViaApi(o);
  const inv = (await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, { max_uses: 1 })).body.invite;
  expect(inv.code).toMatch(/^[A-HJ-NP-Za-km-np-z2-9]{8}$/);
  expect(Date.parse(inv.expires_at) - Date.now()).toBeGreaterThan(604_790_000);
  expect((await api(m.token).get<any>(`/v1/invites/${inv.code}`)).body.invite).toMatchObject({ already_member: false, space: { id: s.id, member_count: 1 } });
  const co = await connectWs(o.token);
  const j = await api(m.token).post<any>(`/v1/invites/${inv.code}/accept`); expect(j.body.space).toMatchObject({ id: s.id, my_role: "member" });
  expect((await co.next("space.member_joined")).data.member.user.id).toBe(m.user.id);
  const third = await signup();
  expect((await api(third.token).post<any>(`/v1/invites/${inv.code}/accept`)).body.error).toMatchObject({ code: "invite_invalid", details: { reason: "exhausted" } });
  expect((await api(m.token).post<any>(`/v1/invites/${inv.code}/accept`)).status).toBe(410);
});
it("revoked / expired / banned / already member", async () => {
  const [o, m, b] = await Promise.all([signup(), signup(), signup()]); const s = await spaceViaApi(o);
  const inv = (await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, {})).body.invite;
  await api(m.token).post(`/v1/invites/${inv.code}/accept`);
  expect((await api(m.token).post<any>(`/v1/invites/${inv.code}/accept`)).body.error.code).toBe("already_member");
  await api(b.token).post(`/v1/invites/${inv.code}/accept`); await api(o.token).del(`/v1/spaces/${s.id}/members/@${b.handle}?ban=true`);
  expect((await api(b.token).post<any>(`/v1/invites/${inv.code}/accept`)).body.error.code).toBe("banned_from_space");
  await api(o.token).del(`/v1/invites/${inv.code}`);
  expect((await api((await signup()).token).get<any>(`/v1/invites/${inv.code}`)).body.error.details.reason).toBe("revoked");
  await env.DB.prepare("UPDATE space_invites SET revoked_at=NULL, expires_at=1 WHERE code=?").bind(inv.code).run();
  expect((await api((await signup()).token).get<any>(`/v1/invites/${inv.code}`)).body.error.details.reason).toBe("expired");
});
it("direct invite: friend only, any role; accept emits invite_removed to both", async () => {
  const [o, m, f, s2] = await Promise.all([signup(), signup(), signup(), signup()]); const { space } = await makeSpace(o, [m]);
  expect((await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.error.code).toBe("action_not_allowed");
  await makeFriends(m, f); const cf = await connectWs(f.token); const cm = await connectWs(m.token);
  const di = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` }));
  expect(di.status).toBe(201); await cf.next("space.invite_received"); expect((await cf.next("alert")).data.alert.kind).toBe("invite");
  expect((await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.error.code).toBe("already_exists");
  expect((await api(f.token).get<any>("/v1/me/direct-invites")).body.items).toHaveLength(1);
  await api(f.token).post(`/v1/direct-invites/${di.body.invite.id}/accept`);
  expect((await cm.next("space.invite_removed")).data).toEqual({ invite_id: di.body.invite.id, reason: "accepted" });
  expect((await api(f.token).post(`/v1/direct-invites/${di.body.invite.id}/accept`)).status).toBe(404); // no longer pending
});
it("direct invite decline and cancel; invite list for admins", async () => {
  const [o, m, f, g] = await Promise.all([signup(), signup(), signup(), signup()]); const { space } = await makeSpace(o, [m]);
  await makeFriends(m, f); await makeFriends(m, g); const cm = await connectWs(m.token);
  const d1 = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${f.handle}` })).body.invite;
  expect((await api(f.token).post(`/v1/direct-invites/${d1.id}/decline`)).status).toBe(204);
  expect((await cm.next("space.invite_removed", (d) => d.invite_id === d1.id)).data.reason).toBe("declined");
  const d2 = (await api(m.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${g.handle}` })).body.invite; const cg = await connectWs(g.token);
  expect((await api(f.token).del(`/v1/direct-invites/${d2.id}`)).status).toBe(404);   // stranger to this invite
  expect((await api(o.token).del(`/v1/direct-invites/${d2.id}`)).status).toBe(204);   // owner may cancel
  expect((await cg.next("space.invite_removed")).data).toEqual({ invite_id: d2.id, reason: "canceled" });
  expect((await api(g.token).get<any>("/v1/me/direct-invites")).body.items).toEqual([]);
  const l = (await api(o.token).post<any>(`/v1/spaces/${space.id}/invites`, {})).body.invite;
  expect((await api(m.token).get(`/v1/spaces/${space.id}/invites`)).status).toBe(403);
  expect((await api(o.token).get<any>(`/v1/spaces/${space.id}/invites`)).body.items.map((i: any) => i.code)).toContain(l.code);
});
it("two concurrent accepts of a max_uses=1 link: exactly one joins", async () => {
  const [o, u1, u2] = await Promise.all([signup(), signup(), signup()]); const s = await spaceViaApi(o);
  const inv = (await api(o.token).post<any>(`/v1/spaces/${s.id}/invites`, { max_uses: 1 })).body.invite;
  const rs = await Promise.all([u1, u2].map((u) => api(u.token).post<any>(`/v1/invites/${inv.code}/accept`)));
  expect(rs.map((r) => r.status).sort()).toEqual([200, 410]);
  expect(rs.find((r) => r.status === 410)!.body.error.details.reason).toBe("exhausted");
  const row = await env.DB.prepare("SELECT s.member_count, i.use_count, (SELECT COUNT(*) FROM space_members WHERE space_id=s.id) n FROM spaces s JOIN space_invites i ON i.space_id=s.id WHERE i.code=?").bind(inv.code).first<any>();
  expect(row).toEqual({ member_count: 2, use_count: 1, n: 2 });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/invites.test.ts` · Expected: FAIL 404.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/invites.test.ts test/api/spaces.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/helpers.ts server/test/api/invites.test.ts
git commit -m "feat(server): invite links and direct space invites"
```

---

### Task 23: text 채널 관리·비공개 채널 멤버·채널 환경설정

**Files:**
- Modify: `server/src/routes/channels.ts`, `server/src/routes/spaces.ts` (`POST /spaces/:space_id/channels`), `server/src/services/membership.ts` (비공개 text 분기)
- Test: `server/test/api/channels.test.ts`

**Interfaces:**
- Consumes: Task 21·22; Task 20 `postSystemMessage`
- Produces (api.md 5.9): `POST /spaces/:space_id/channels`(owner·admin; 이름 `^[a-z0-9가-힣_-]{1,32}$`(NFC 후), 같은 방 같은 이름 409 `already_exists`, 100개 422 `channels_per_space`; 비공개면 `members`(방 멤버만) + 만든 사람; 이벤트 공개 → `channel.created`(space), 비공개 → 채널 멤버; 시스템 메시지 `channel_created`), `PATCH /channels/:channel_id`(text owner·admin `name/topic/position`; group_dm 멤버 `name/topic`; dm 409; `channel.updated` + 시스템 메시지 `channel_renamed|channel_topic_changed|group_renamed`), `DELETE /channels/:channel_id`(text 만, owner·admin, 기본 채널 409 `default_channel`; **batch 전에** 청중 사용자 ID 전부(공개 = `space_members`, 비공개 = `channel_members` ∩ `space_members`)를 읽고, 소프트 삭제 커밋 뒤 `{users: 그 ID들}` 로 `channel.deleted{channel_id, space_id, reason:"deleted"}` — `{channel}` 청중은 삭제된 채널을 거르므로 쓰지 않는다), `GET /channels/:channel_id/members?cursor&limit`(읽기 권한; 공개 text 는 방 멤버, 그 밖은 `channel_members`; `{items:[{user, joined_at}], next_cursor}`), 비공개 text 멤버 추가(owner·admin 또는 그 채널 멤버 — 추가만; 대상이 방 멤버 아니면 404)·제거(owner·admin 또는 @me), `PUT /channels/:channel_id/prefs`(`notify`, `muted`; `channel.prefs_updated{channel_id, my}`). `GET /channels/:channel_id/pins` 는 Task 26.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
it("admin creates public channel; member cannot; duplicate name incl. NFD -> 409", async () => {
  const [o, m] = await Promise.all([signup(), signup()]); const { space } = await makeSpace(o, [m]); const cm = await connectWs(m.token);
  expect((await api(m.token).post(`/v1/spaces/${space.id}/channels`, { name: "잡담" })).status).toBe(403);
  const r = await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "개발", topic: "dev" });
  expect(r.status).toBe(201); expect((await cm.next("channel.created")).data.channel.name).toBe("개발");
  expect((await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "개발".normalize("NFD") })).body.error.code).toBe("already_exists");
  expect((await api(o.token).post(`/v1/spaces/${space.id}/channels`, { name: "Bad Name" })).status).toBe(400);
});
it("private channel visible only to its members (even owner must be member)", async () => {
  const [o, ad, m] = await Promise.all([signup(), signup(), signup()]); const { space } = await makeSpace(o, [ad, m]);
  await api(o.token).patch(`/v1/spaces/${space.id}/members/@${ad.handle}`, { role: "admin" });
  const p = (await api(ad.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "비밀", is_private: true, members: [`@${m.handle}`] })).body.channel;
  const names = async (u: TestUser) => (await api(u.token).get<any>(`/v1/spaces/${space.id}/channels`)).body.items.map((c: any) => c.name);
  expect(await names(m)).toContain("비밀"); expect(await names(o)).not.toContain("비밀");
  expect((await api(o.token).get(`/v1/channels/${p.id}`)).status).toBe(404);
  expect((await api(m.token).post<any>(`/v1/channels/${p.id}/members`, { users: [`@${o.handle}`] })).status).toBe(200); // member may add
  expect((await api(m.token).del(`/v1/channels/${p.id}/members/@${o.handle}`)).status).toBe(403);         // but not remove
});
it("rename -> channel.updated + system message; default channel undeletable; prefs", async () => {
  const [o, m] = await Promise.all([signup(), signup()]); const { space, generalChannelId } = await makeSpace(o, [m]); const cm = await connectWs(m.token);
  const ch = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "a1" })).body.channel;
  await api(o.token).patch(`/v1/channels/${ch.id}`, { name: "a2" });
  expect((await cm.next("channel.updated")).data.channel.name).toBe("a2");
  expect((await api(o.token).get<any>(`/v1/channels/${ch.id}/messages`)).body.items.at(-1).system).toMatchObject({ event: "channel_renamed", data: { name: "a2" } });
  expect((await api(o.token).del<any>(`/v1/channels/${generalChannelId}`)).body.error.details.reason).toBe("default_channel");
  const pr = await api(m.token).put<any>(`/v1/channels/${ch.id}/prefs`, { notify: "all", muted: true });
  expect(pr.body.my).toMatchObject({ notify: "all", muted: true }); await cm.next("channel.prefs_updated");
});
it("delete channel: members get channel.deleted{reason:'deleted'}; member list", async () => {
  const [o, m] = await Promise.all([signup(), signup()]); const { space } = await makeSpace(o, [m]); const cm = await connectWs(m.token);
  const ch = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "tmp" })).body.channel;
  expect((await api(o.token).get<any>(`/v1/channels/${ch.id}/members`)).body.items.map((x: any) => x.user.id).sort()).toEqual([o.user.id, m.user.id].sort());
  expect((await api(m.token).del(`/v1/channels/${ch.id}`)).status).toBe(403);
  expect((await api(o.token).del(`/v1/channels/${ch.id}`)).status).toBe(204);
  expect((await cm.next("channel.deleted")).data).toEqual({ channel_id: ch.id, space_id: space.id, reason: "deleted" });
  expect((await api(m.token).get(`/v1/channels/${ch.id}`)).status).toBe(404);
});
it("group dm rename by any member -> channel.updated + group_renamed; 1:1 dm patch 409", async () => {
  const [o, x, y] = await Promise.all([signup(), signup(), signup()]); await makeFriends(o, x); await makeFriends(o, y);
  const g = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`, `@${y.handle}`] })).body.channel; const cy = await connectWs(y.token);
  expect((await api(x.token).patch<any>(`/v1/channels/${g.id}`, { name: "점심팟" })).body.channel.name).toBe("점심팟");
  expect((await cy.next("channel.updated")).data.channel.name).toBe("점심팟");
  expect((await api(y.token).get<any>(`/v1/channels/${g.id}/messages`)).body.items.at(-1).system).toMatchObject({ event: "group_renamed", actor_id: x.user.id, data: { name: "점심팟" } });
  expect((await api(x.token).get<any>(`/v1/channels/${g.id}/members`)).body.items).toHaveLength(3);
  const d = (await api(o.token).post<any>("/v1/dms", { users: [`@${x.handle}`] })).body.channel;
  expect((await api(o.token).patch(`/v1/channels/${d.id}`, { name: "x" })).status).toBe(409);
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/channels.test.ts` · Expected: FAIL.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/channels.test.ts test/api/dms.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/channels.test.ts
git commit -m "feat(server): text channel management, private channel members and prefs"
```

---

### Task 24: 멘션·@here 해석·메시지 알림 팬아웃·수정·삭제

**Files:**
- Modify: `server/src/services/messages.ts` (멘션 해석, `editMessage`, `deleteMessage`), `server/src/lib/notify.ts` (`fanoutMessageNotifications`), `server/src/routes/messages.ts` (`PATCH|DELETE /messages/:message_id`)
- Test: `server/test/api/messages.test.ts`

**Interfaces:**
- Consumes: Task 15 `notifyMany`, `sendAlerts`; Task 7 `getChannelAccess`, `isBlockedEither`
- Produces:
  - 멘션 해석(api.md 2.1): `extractMentions` 결과 중 `here` 제외, 사용자 존재 + 그 채널 `getChannelAccess(...).canRead` + 작성자와 차단 없음만 `message_mentions(is_here=0)` + `Message.mentions[]`. `@here`(1:1 dm 제외): 채널 독자 중 작성자·차단 관계 제외, `user_presence.status IN ('online','away')` → 명시 멘션과 겹치지 않는 사람만 `is_here=1`. `in_thread` = 스레드 답글이면 1. `messages.mention_here` 설정.
  - `fanoutMessageNotifications(rt, message, channel)` (계약 4.12): 수신자별 alert 1개, 우선순위 `dm > mention > thread_reply > channel_message`; 알림함: 멘션 대상(1:1 dm 제외)에 `message_mention`, 스레드 참여자(Task 25)에 `thread_reply` — **같은 수신자가 둘 다면 `message_mention` 하나만**(이 계획의 결정). 작성자 본인 제외. alert `title`: text = `"<display_name> · <space name> #<channel>"`, dm = `"<display_name>"`, group_dm = `"<display_name> · <그룹 이름 또는 멤버 이름 3명>"`, 80자 자름; `body` = `plainSnippet(body, 200)`; `target{type:"message", channel_id, message_id, thread_root_id, space_id}`. `createMessage` 가 커밋·발행 뒤 호출.
  - `editMessage(rt, messageId, actorId, body): Promise<Message>` — 작성자·`text|me` 만(아니면 403 `forbidden`), 1:1 dm 은 쓰기 권한 재확인, 멘션 재해석(새로 추가된 명시 멘션에만 알림, `@here` 재확장 안 함, 사라진 멘션 행 삭제), `edited_at`, `message.updated`.
  - `deleteMessage(rt, messageId, actorId, opts?)` — 작성자 또는 `canModerateChannel`(또는 `asAdmin`); 소프트 삭제 + `body=''` + 리액션·고정·멘션 행 DELETE; 답글이면 루트 `reply_count` 감소 + `thread.updated`; `message.deleted{channel_id, message_id, thread_root_id, tombstone}`(tombstone = 루트이고 reply_count>0).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
describe("mentions", () => {
  it("@handle resolves only for readers without blocks; author in mentions[] but not notified", async () => {
    const [o, m, out, blk] = await Promise.all([signup(), signup(), signup(), signup()]); const { space } = await makeSpace(o, [m, blk]);
    const priv = (await api(o.token).post<any>(`/v1/spaces/${space.id}/channels`, { name: "p", is_private: true, members: [`@${blk.handle}`] })).body.channel;
    await api(blk.token).put(`/v1/blocks/@${o.handle}`);
    const cm = await connectWs(m.token);
    const msg = (await api(o.token).post<any>(`/v1/channels/${priv.id}/messages`, { client_nonce: "m1", body: `@${m.handle} @${out.handle} @${blk.handle} @${o.handle} hi` })).body.message;
    expect(msg.mentions.map((x: any) => x.handle)).toEqual([o.handle]);   // m: not a member of private channel; out: not in space; blk: blocked
    await settle(); expect(cm.frames.some((f) => f.type === "notification.created")).toBe(false);
  });
  it("mention in channel -> message_mention inbox + mention alert; @here expands to online non-dnd only", async () => {
    const [o, on, dnd, off] = await Promise.all([signup(), signup(), signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [on, dnd, off]);
    const [con, cdnd] = await Promise.all([connectWs(on.token), connectWs(dnd.token)]); await api(dnd.token).put("/v1/me/presence", { mode: "dnd" });
    const msg = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "h1", body: "@here 회의" })).body.message;
    expect(msg).toMatchObject({ mention_here: true, mentions: [] });
    expect((await con.next("notification.created")).data.notification).toMatchObject({ type: "message_mention", context: { message_id: msg.id, thread_root_id: null } });
    expect((await con.next("alert")).data.alert.kind).toBe("mention");
    await settle(); expect(cdnd.frames.some((f) => f.type === "notification.created")).toBe(false);
    expect((await env.DB.prepare("SELECT COUNT(*) n FROM message_mentions WHERE message_id=?").bind(msg.id).first<any>()).n).toBe(1);
  });
  it("@here in 1:1 dm does not expand; dm alert to partner", async () => {
    const { a, b, ch } = await dmPair(); const cb = await connectWs(b.token);
    await api(a.token).post(`/v1/channels/${ch}/messages`, { client_nonce: "d1", body: "@here yo" });
    expect((await cb.next("alert")).data.alert).toMatchObject({ kind: "dm", title: a.user.display_name, body: "@here yo" });
    expect((await env.DB.prepare("SELECT COUNT(*) n FROM message_mentions m JOIN messages x ON x.id=m.message_id WHERE x.channel_id=?").bind(ch).first<any>()).n).toBe(0);
  });
  it("channel notify:all gives channel_message alert (top-level only); default mentions gives none", async () => {
    const [o, m, q] = await Promise.all([signup(), signup(), signup()]); const { space, generalChannelId: g } = await makeSpace(o, [m, q]);
    await api(m.token).put(`/v1/channels/${g}/prefs`, { notify: "all" });
    const [cm, cq] = await Promise.all([connectWs(m.token), connectWs(q.token)]);
    const msg = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "c1", body: "**공지** 입니다" })).body.message;
    expect((await cm.next("alert")).data.alert).toMatchObject({ kind: "channel_message", body: "공지 입니다", target: { type: "message", channel_id: g, message_id: msg.id, space_id: space.id } });
    await settle(); expect(cq.frames.some((f) => f.type === "alert")).toBe(false);
    await api(o.token).post(`/v1/messages/${msg.id}/thread`, { client_nonce: "c2", body: "답글" }); await settle();
    expect(cm.frames.filter((f) => f.type === "alert")).toHaveLength(1);   // thread replies never give channel_message
  });
  it("Review Focus 2: @here with 120 online members writes 119 is_here rows and 119 inbox rows", async () => {
    const o = await signup(); const { space, generalChannelId: g } = await makeSpace(o, []);
    await seedOnlineMembersSql(space.id, 119);  // local fixture: users + user_settings + user_presence(status='online') + space_members + member_count, via INSERT … SELECT FROM json_each
    const r = await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "h120", body: "@here 공지" });
    expect(r.status).toBe(201);
    const count = async (sql: string) => (await env.DB.prepare(sql).bind(r.body.message.id).first<any>()).n;
    expect(await count("SELECT COUNT(*) n FROM message_mentions WHERE message_id=? AND is_here=1")).toBe(119);
    expect(await count("SELECT COUNT(*) n FROM notifications WHERE type='message_mention' AND subject_id=?")).toBe(119);
  });
});
describe("edit/delete", () => {
  it("only author edits text; new mention notifies once", async () => {
    const [o, m] = await Promise.all([signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [m]); const cm = await connectWs(m.token);
    const msg = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "e1", body: "hello" })).body.message;
    expect((await api(m.token).patch(`/v1/messages/${msg.id}`, { body: "hack" })).status).toBe(403);
    const ed = (await api(o.token).patch<any>(`/v1/messages/${msg.id}`, { body: `hello @${m.handle}` })).body.message;
    expect(ed.edited_at).not.toBeNull(); await cm.next("message.updated"); await cm.next("notification.created");
    await api(o.token).patch(`/v1/messages/${msg.id}`, { body: `hello again @${m.handle}` }); await settle();
    expect(cm.frames.filter((f) => f.type === "notification.created")).toHaveLength(1);
  });
  it("delete: author / space admin ok, other member 403; event tombstone flag", async () => {
    const [o, m, x] = await Promise.all([signup(), signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [m, x]); const cx = await connectWs(x.token);
    const msg = (await api(m.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "d1", body: "bye" })).body.message;
    expect((await api(x.token).del(`/v1/messages/${msg.id}`)).status).toBe(403);
    expect((await api(o.token).del(`/v1/messages/${msg.id}`)).status).toBe(204);
    expect((await cx.next("message.deleted")).data).toEqual({ channel_id: g, message_id: msg.id, thread_root_id: null, tombstone: false });
    expect((await api(o.token).get<any>(`/v1/channels/${g}/messages`)).body.items.map((i: any) => i.id)).not.toContain(msg.id);
  });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/messages.test.ts` · Expected: 새 테스트 FAIL.
- [ ] **Step 3: 구현** — Produces 대로. 후보 사용자 조회는 `json_each` 1회, 채널 접근 확인은 후보(≤50)마다 `getChannelAccess`. `@here` 대상은 SQL 한 문(채널 독자 ∩ `user_presence.status IN ('online','away')` − 작성자 − 차단 관계 − 명시 멘션)으로 고르고, `message_mentions` 행은 `INSERT INTO message_mentions … SELECT … FROM json_each(?)` 한 문으로 넣는다. 알림함 행(`notifyMany`)도 행마다 문 하나(batch) 또는 `json_each` 로 — 한 문에 `VALUES (?,…)` 를 여러 행 이어 붙여 바인딩 100개(D1 상한)를 넘기지 않는다. 알림 입력값은 Task 15 표(`message_mention` dedupeKey `message_mention:<message_id>:<user_id>` — 편집 재해석에도 같은 사람에게 한 번만).
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/messages.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/messages.test.ts
git commit -m "feat(server): mention and @here resolution, message notifications, edit and delete"
```

---

### Task 25: 스레드

**Files:**
- Modify: `server/src/services/messages.ts` (`threadRootId` 경로), `server/src/routes/messages.ts` (`GET|POST /messages/:message_id/thread`), `server/src/lib/notify.ts` (스레드 수신자)
- Test: `server/test/api/threads.test.ts`

**Interfaces:**
- Consumes: Task 20 `createMessage`, Task 24 알림 팬아웃
- Produces (api.md 5.11): `POST /messages/:message_id/thread`(`RL_MESSAGE`; 답글 ID 를 주면 그 루트로; kind text|me|roll — poll 400; 같은 batch: 답글 INSERT + 루트 `reply_count+1`, `last_reply_at` + `thread_participants` upsert(첫 답글이면 루트 작성자도, 이때 루트 작성자 행의 `last_reply_at` = 루트 `created_at` 이라 답글 작성자가 더 최근으로 정렬됨) — 채널 `last_message_*`·읽음 포인터는 건드리지 않음; 이벤트 `message.created`(thread_root_id 설정) + `thread.updated{channel_id, root_id, thread}`; 알림 `thread_reply` + alert `thread_reply` → 루트 작성자·이전 참여자(작성자 제외), 멘션 우선 규칙 Task 24; 입력값(subject = 답글, context.thread_root_id = 루트 등)은 Task 15 표), `GET /messages/:message_id/thread?before|after&limit`(`{root, items, has_more_before, has_more_after}`, 오름차순). 삭제된 루트는 답글이 있으면 묘비로 목록에 남는다.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
it("reply updates root summary and notifies root author + participants", async () => {
  const [o, a, b] = await Promise.all([signup(), signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [a, b]);
  const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "배포 몇 시?" })).body.message;
  const [co, ca] = await Promise.all([connectWs(o.token), connectWs(a.token)]);
  const r1 = (await api(a.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: "t1", body: "3시" })).body.message;
  expect(r1.thread_root_id).toBe(root.id);
  expect((await co.next("thread.updated")).data.thread).toMatchObject({ reply_count: 1, participant_ids: [a.user.id, o.user.id] });
  expect((await co.next("alert", (d) => d.alert.kind === "thread_reply")).data.alert.target.thread_root_id).toBe(root.id);
  await api(b.token).post(`/v1/messages/${r1.id}/thread`, { client_nonce: "t2", body: "ok" }); // reply-to-reply -> same root
  expect((await ca.next("notification.created", (d) => d.notification.type === "thread_reply")).data.notification).toMatchObject({ subject: { type: "message" }, context: { channel_id: g, thread_root_id: root.id } });
  const t = (await api(o.token).get<any>(`/v1/messages/${root.id}/thread`)).body;
  expect(t.root.thread.reply_count).toBe(2); expect(t.items).toHaveLength(2);
  expect((await api(o.token).get<any>(`/v1/channels/${g}/messages`)).body.items.every((m: any) => m.thread_root_id === null)).toBe(true);
});
it("thread mention counts only in inbox, not channel mention_count; poll reply 400", async () => {
  const [o, a] = await Promise.all([signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [a]);
  const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "q" })).body.message;
  await api(o.token).post(`/v1/messages/${root.id}/thread`, { client_nonce: "t", body: `@${a.handle} 봐줘` });
  expect((await api(a.token).get<any>(`/v1/channels/${g}`)).body.channel.my.mention_count).toBe(0);
  expect((await api(o.token).post(`/v1/messages/${root.id}/thread`, { client_nonce: "p", kind: "poll", body: "", poll: { question: "q", options: ["a", "b"] } })).status).toBe(400);
});
it("deleted root with replies stays as tombstone; deleting a reply decrements count", async () => {
  const [o, a] = await Promise.all([signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [a]);
  const root = (await api(o.token).post<any>(`/v1/channels/${g}/messages`, { client_nonce: "r", body: "q" })).body.message;
  const rep = (await api(a.token).post<any>(`/v1/messages/${root.id}/thread`, { client_nonce: "t", body: "x" })).body.message;
  await api(o.token).del(`/v1/messages/${root.id}`);
  expect((await api(a.token).get<any>(`/v1/channels/${g}/messages`)).body.items.find((m: any) => m.id === root.id)).toMatchObject({ deleted: true, body: "" });
  const ca = await connectWs(a.token); await api(a.token).del(`/v1/messages/${rep.id}`);
  expect((await ca.next("thread.updated")).data.thread.reply_count).toBe(0);
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/threads.test.ts` · Expected: FAIL 404.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/threads.test.ts test/api/messages.test.ts` · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/threads.test.ts
git commit -m "feat(server): message threads with participants and reply notifications"
```

---

### Task 26: 리액션·고정

**Files:**
- Modify: `server/src/routes/messages.ts` (리액션·고정), `server/src/routes/channels.ts` (`GET /channels/:channel_id/pins`)
- Test: `server/test/api/reactions-pins.test.ts`

**Interfaces:**
- Consumes: Task 3 `isSingleEmoji`; Task 7 `canModerateChannel`, `requireChannelRead`, `canDm`; Task 20 `postSystemMessage`
- Produces (api.md 5.12): `PUT /messages/:message_id/reactions/:emoji`(`RL_REACT`; 경로 디코딩 후 NFC, `isSingleEmoji(_,32)` 아니면 400 `fields.emoji="invalid"`; 읽기 권한 + 1:1 dm 은 `canDm`; 서로 다른 이모지 20 초과 422 `reactions_per_message`; 이미 있으면 이벤트 없이 204; `reaction.added{channel_id, message_id, thread_root_id, emoji, user_id, count}`), `DELETE …/reactions/:emoji`(`reaction.removed`, count 는 제거 후), `GET …/reactions/:emoji?cursor&limit`(UserSummary 페이지), `PUT|DELETE /messages/:message_id/pin`(`RL_REACT`; 최상위만 아니면 400; text 는 owner·admin, dm/group_dm 은 멤버; 채널당 50 → 422 `pins_per_channel`; `message.pinned{channel_id, message_id, pinned_by}` / `message.unpinned{…, unpinned_by}`; 고정 시 시스템 메시지 `message_pinned{data:{message_id}}`), `GET /channels/:channel_id/pins`(고정 시각 최신순).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
it("Review Focus 4: multi-codepoint single graphemes ok, two graphemes / text 400", async () => {
  const { a, b, ch } = await dmPair(); const msg = (await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "x", body: "hi" })).body.message;
  const cb = await connectWs(b.token);
  for (const e of ["👍🏽", "👨‍👩‍👧", "🇰🇷"]) expect((await api(a.token).put(`/v1/messages/${msg.id}/reactions/${encodeURIComponent(e)}`)).status).toBe(204);
  for (const e of ["👍👍", "ab", "가"]) expect((await api(a.token).put(`/v1/messages/${msg.id}/reactions/${encodeURIComponent(e)}`)).status).toBe(400);
  expect((await cb.next("reaction.added")).data).toMatchObject({ emoji: "👍🏽", user_id: a.user.id, count: 1, thread_root_id: null });
  await api(b.token).put(`/v1/messages/${msg.id}/reactions/${encodeURIComponent("👍🏽")}`);
  const m = (await api(b.token).get<any>(`/v1/messages/${msg.id}`)).body.message;
  expect(m.reactions[0]).toEqual({ emoji: "👍🏽", count: 2, me: true });
  expect(m.reactions.map((r: any) => r.emoji)).toEqual(["👍🏽", "👨‍👩‍👧", "🇰🇷"]);
  await api(a.token).del(`/v1/messages/${msg.id}/reactions/${encodeURIComponent("👍🏽")}`);
  expect((await cb.next("reaction.removed")).data.count).toBe(1);
  expect((await api(a.token).get<any>(`/v1/messages/${msg.id}/reactions/${encodeURIComponent("👍🏽")}`)).body.items.map((u: any) => u.id)).toEqual([b.user.id]);
});
it("21st distinct emoji -> 422", async () => {
  const { a, ch } = await dmPair(); const msg = (await api(a.token).post<any>(`/v1/channels/${ch}/messages`, { client_nonce: "x", body: "hi" })).body.message;
  const emojis = [..."😀😁😂🤣😃😄😅😆😉😊😋😎😍😘🥰😗😙😚🙂🤗🤩"];
  for (const e of emojis.slice(0, 20)) await api(a.token).put(`/v1/messages/${msg.id}/reactions/${encodeURIComponent(e)}`);
  expect((await api(a.token).put<any>(`/v1/messages/${msg.id}/reactions/${encodeURIComponent(emojis[20])}`)).body.error.details).toEqual({ what: "reactions_per_message", limit: 20 });
});
it("pins: text needs admin, dm any member; system message; list newest first", async () => {
  const [o, m] = await Promise.all([signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [m]);
  const [m1, m2] = [await post(o, g, "a"), await post(o, g, "b")];
  expect((await api(m.token).put(`/v1/messages/${m1.id}/pin`)).status).toBe(403);
  const cm = await connectWs(m.token);
  expect((await api(o.token).put(`/v1/messages/${m1.id}/pin`)).status).toBe(204); await api(o.token).put(`/v1/messages/${m2.id}/pin`);
  expect((await cm.next("message.pinned")).data).toEqual({ channel_id: g, message_id: m1.id, pinned_by: o.user.id });
  expect((await api(m.token).get<any>(`/v1/channels/${g}/pins`)).body.items.map((x: any) => x.id)).toEqual([m2.id, m1.id]);
  expect((await api(m.token).del(`/v1/messages/${m1.id}/pin`)).status).toBe(403);
  expect((await api(o.token).del(`/v1/messages/${m1.id}/pin`)).status).toBe(204);
  expect((await cm.next("message.unpinned")).data).toEqual({ channel_id: g, message_id: m1.id, unpinned_by: o.user.id });
  expect((await api(m.token).get<any>(`/v1/channels/${g}/pins`)).body.items.map((x: any) => x.id)).toEqual([m2.id]);
  const { a, ch } = await dmPair(); const d = await post(a, ch, "dm");
  expect((await api(a.token).put(`/v1/messages/${d.id}/pin`)).status).toBe(204);
});
it("51st pin in a channel -> 422 pins_per_channel", async () => {
  const { a, ch } = await dmPair();
  await seedPinnedMessagesSql(ch, a, 50);   // local fixture: 50 messages + message_pins rows (INSERT … SELECT FROM json_each)
  const extra = await post(a, ch, "one more");
  expect((await api(a.token).put<any>(`/v1/messages/${extra.id}/pin`)).body.error.details).toEqual({ what: "pins_per_channel", limit: 50 });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/reactions-pins.test.ts` · Expected: FAIL 404.
- [ ] **Step 3: 구현** — Produces 대로. 리액션 수 집계는 INSERT 와 같은 batch 뒤 `SELECT COUNT(*)` 1회.
- [ ] **Step 4: 통과 확인** — Run: `npx vitest run test/api/reactions-pins.test.ts` · Expected: `Tests  4 passed (4)`
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/reactions-pins.test.ts
git commit -m "feat(server): emoji reactions and message pins"
```

---

### Task 27: 읽음 표시·안 읽음 수·입력 중

**Files:**
- Modify: `server/src/routes/channels.ts` (`PUT /channels/:channel_id/read`), `server/src/routes/me.ts` (`GET /me/unreads`), `server/src/do/user-hub.ts` (`typing` 프레임)
- Test: `server/test/api/channels.test.ts` (`describe("read state")`), `server/test/realtime/fanout.test.ts` (`describe("typing")`)

**Interfaces:**
- Consumes: Task 19 `loadUnreadStates`; Task 7 `getChannelAccess`; Task 11 `publishEvent`
- Produces: `PUT /channels/:channel_id/read {message_id}` — 그 채널의 최상위 메시지 ID 가 아니면 400 `fields.message_id="invalid"`; `INSERT … ON CONFLICT DO UPDATE SET last_read_message_id = excluded.last_read_message_id WHERE excluded.last_read_message_id > channel_reads.last_read_message_id`; `200 {state: UnreadState}` + `read_state.updated{state}`(나). `GET /me/unreads` — `accessibleChannelIds` → `loadUnreadStates` → 0 아닌 것만. UserHub `typing`: full 만(notify 는 Task 11), `getChannelAccess(...).canWrite` 아니면 `reply{not_found}`(attachment 에 5분 캐시 허용), 같은 연결·채널(+스레드) 2초 안 중복 버림, `publishEvent(rt, {channel}, "typing", {channel_id, thread_root_id, user_id, expires_in_ms: 6000}, {ephemeral: true, exclude: [userId]})`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// channels.test.ts
describe("read state", () => {
  it("unread/mention counts top-level by others after pointer; baseline = join time; own send advances pointer", async () => {
    const [o, m] = await Promise.all([signup(), signup()]);
    const { space, generalChannelId: g } = await makeSpace(o, []); await post(o, g, "before join");
    await joinViaInvite(space.id, o, m);
    const x1 = await post(o, g, "one"); await post(o, g, `@${m.handle} two`);
    await api(o.token).post(`/v1/messages/${x1.id}/thread`, { client_nonce: "tr", body: "reply not counted" });
    const st = (await api(m.token).get<any>("/v1/me/unreads")).body.items.find((s: any) => s.channel_id === g);
    expect(st).toMatchObject({ unread_count: 2, mention_count: 1, last_read_message_id: null });
    const cm2 = await connectWs(m.token);
    const r = await api(m.token).put<any>(`/v1/channels/${g}/read`, { message_id: x1.id });
    expect(r.body.state).toMatchObject({ unread_count: 1, mention_count: 1, last_read_message_id: x1.id }); await cm2.next("read_state.updated");
    // api.md 5.10: sending moves the author's own channel_reads to the new message -> nothing unread, channel drops out of /me/unreads
    const mine = await post(m, g, "mine");
    expect((await api(m.token).get<any>("/v1/me/unreads")).body.items.find((s: any) => s.channel_id === g)).toBeUndefined();
    expect((await api(m.token).get<any>(`/v1/channels/${g}`)).body.channel.my).toMatchObject({ unread_count: 0, mention_count: 0, last_read_message_id: mine.id });
  });
  it("Review Focus 5: pointer never moves back; foreign/thread ids rejected", async () => {
    const [o, m] = await Promise.all([signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [m]);
    const [a1, a2] = [await post(o, g, "1"), await post(o, g, "2")];
    await api(m.token).put(`/v1/channels/${g}/read`, { message_id: a2.id });
    expect((await api(m.token).put<any>(`/v1/channels/${g}/read`, { message_id: a1.id })).body.state.last_read_message_id).toBe(a2.id);
    const reply = (await api(o.token).post<any>(`/v1/messages/${a1.id}/thread`, { client_nonce: "t", body: "r" })).body.message;
    expect((await api(m.token).put(`/v1/channels/${g}/read`, { message_id: reply.id })).status).toBe(400);
    const { a, ch } = await dmPair(); const other = await post(a, ch, "elsewhere");
    expect((await api(m.token).put(`/v1/channels/${g}/read`, { message_id: other.id })).status).toBe(400);
  });
});
```
```ts
// fanout.test.ts
describe("typing", () => {
  it("typing reaches other members only, ephemeral, deduped within 2s; no write access -> reply not_found", async () => {
    const [o, m] = await Promise.all([signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [m]);
    const [co, cm] = await Promise.all([connectWs(o.token), connectWs(m.token)]);
    co.send({ type: "typing", data: { channel_id: g, thread_root_id: null } }); co.send({ type: "typing", data: { channel_id: g, thread_root_id: null } });
    const t = await cm.next("typing"); expect(t).toMatchObject({ seq: null, data: { channel_id: g, user_id: o.user.id, expires_in_ms: 6000 } });
    await settle(); expect(cm.frames.filter((f) => f.type === "typing")).toHaveLength(1); expect(co.frames.some((f) => f.type === "typing")).toBe(false);
    const x = await signup(); const cx = await connectWs(x.token);
    cx.send({ type: "typing", ref: "t1", data: { channel_id: g, thread_root_id: null } });
    expect((await cx.next("reply", (d) => d.ref === "t1")).data.error.code).toBe("not_found");
  });
});
```

(`post(user, channelId, body)` 와 `joinViaInvite` 는 테스트 파일 지역 헬퍼 — API 호출만 감쌈.)

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/channels.test.ts test/realtime/fanout.test.ts` · Expected: 새 테스트 FAIL.
- [ ] **Step 3: 구현** — Produces 대로.
- [ ] **Step 4: 통과 확인** — Run: 같은 명령 · Expected: 모두 PASS.
- [ ] **Step 5: Commit**

```bash
git add server/src server/test/api/channels.test.ts server/test/realtime/fanout.test.ts
git commit -m "feat(server): read markers, unread counts and typing indicators"
```

---

### Task 28: 메시지 검색 (FTS trigram + LIKE 폴백)

**Files:**
- Create: `server/src/lib/search.ts`, `server/src/routes/search.ts` (이 작업은 `/search/messages`; `/search/posts`·`/hashtags/search` 는 Task 29)
- Modify: `server/src/lib/serialize.ts` (`loadChannelRefs`), `server/src/app.ts`
- Test: `server/test/unit/search-query.test.ts`, `server/test/api/search.test.ts`

**Interfaces:**
- Consumes: Task 7 `accessibleChannelIds`; Task 20 `serializeMessages`; Task 6 `limit("RL_SEARCH")`
- Produces: `buildMessageSearch(q): { mode: "fts"; match: string; terms: string[] } | { mode: "like"; terms: string[] }`, `highlights(body, terms): SearchHighlight[]` (계약 4.13; data-model 4장 규칙 1·2·3·6 — 낱말 `"…"` 감싸기(내부 `"` → `""`) + `AND`, 3글자 미만 낱말이 하나라도 있으면 like, 하이라이트는 대소문자 무시·코드 포인트 위치·겹침 없이 시작 순). 덧붙임: `loadChannelRefs(db: D1Database, channelIds: string[]): Promise<Map<string, ChannelRef>>`. `searchRoutes`: `GET /search/messages?q&channel_id&space_id&author&cursor&limit` — `RL_SEARCH`, q 1..100(NFC), limit 기본 20 최대 50, 범위 `channel_id`(읽기 권한, 아니면 404) > `space_id`(멤버) > 전체; 채널 500 초과 → 422 `{what:"search_scope", limit:500}`; 차단한 사용자의 메시지 제외; 삭제 제외; like 모드는 `m.id >= idFloor(now-90일)` + 최근 5000행 한도; 최신순 커서(`{k: id, i: id}`); 응답 `{items:[{message, channel, highlights}], next_cursor}`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// unit/search-query.test.ts
it("quotes terms and blocks FTS syntax injection", () => {
  expect(buildMessageSearch(' 배포는  "몇시"  ')).toEqual({ mode: "fts", match: '"배포는" AND """몇시"""', terms: ["배포는", '"몇시"'] });
  expect(buildMessageSearch("배포 OR*")).toEqual({ mode: "like", terms: ["배포", "OR*"] });
  expect(buildMessageSearch("배포".normalize("NFD") + "는")).toMatchObject({ mode: "fts", terms: ["배포는"] });
});
it("highlights in code points, case-insensitive", () => {
  expect(highlights("😀 Deploy 배포는 deploy", ["deploy", "배포"])).toEqual([{ start: 2, length: 6 }, { start: 9, length: 2 }, { start: 13, length: 6 }]);
});
```
```ts
// api/search.test.ts (append)
describe("message search", () => {
  it("3+ chars via FTS, 2 chars via LIKE, scoped to accessible channels, excludes blocked authors", async () => {
    const [o, m, out, blk] = await Promise.all([signup(), signup(), signup(), signup()]); const { generalChannelId: g } = await makeSpace(o, [m, blk]);
    const tag = uniqueHandle("k").slice(-6);
    const hit = await post(o, g, `오늘 배포는 ${tag} 세 시`); await post(blk, g, `배포는 ${tag} 차단`);
    const { a, ch } = await dmPair(); await post(a, ch, `배포는 ${tag} 남의 DM`);
    await api(m.token).put(`/v1/blocks/@${blk.handle}`);
    const r = (await api(m.token).get<any>(`/v1/search/messages?q=${encodeURIComponent("배포는 " + tag)}`)).body;
    expect(r.items.map((i: any) => i.message.id)).toEqual([hit.id]);
    expect(r.items[0]).toMatchObject({ channel: { id: g, kind: "text", name: "일반" }, highlights: [{ start: 3, length: 3 }, expect.any(Object)] });
    expect((await api(m.token).get<any>(`/v1/search/messages?q=${encodeURIComponent("배포 " + tag)}`)).body.items.map((i: any) => i.message.id)).toEqual([hit.id]);
    expect((await api(out.token).get<any>(`/v1/search/messages?q=${tag}`)).body.items).toEqual([]);
    expect((await api(out.token).get(`/v1/search/messages?q=${tag}&channel_id=${g}`)).status).toBe(404);
  });
  it("Review Focus 1: NFD query finds NFC text; deleted messages disappear; empty q 400", async () => {
    const [o] = await Promise.all([signup()]); const { generalChannelId: g } = await makeSpace(o, []); const tag = uniqueHandle("n").slice(-6);
    const msg = await post(o, g, `한글검색 ${tag}`);
    expect((await api(o.token).get<any>(`/v1/search/messages?q=${encodeURIComponent("한글검색".normalize("NFD") + " " + tag)}`)).body.items).toHaveLength(1);
    await api(o.token).del(`/v1/messages/${msg.id}`);
    expect((await api(o.token).get<any>(`/v1/search/messages?q=${tag}`)).body.items).toEqual([]);
    expect((await api(o.token).get(`/v1/search/messages?q=`)).status).toBe(400);
  });
});
```

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/unit/search-query.test.ts test/api/search.test.ts` · Expected: FAIL.
- [ ] **Step 3: 구현** — Produces 대로. FTS 질의는 data-model 4장 2번 SQL 에 `m.channel_id IN (SELECT value FROM json_each(?))`, `m.author_id NOT IN (SELECT blocked_id FROM blocks WHERE blocker_id = ?)`, 커서 조건을 더한 형태.
- [ ] **Step 4: 통과·타입 확인**

Run: `npx vitest run test/unit/search-query.test.ts test/api/search.test.ts && npm run typecheck`
Expected: 두 파일 PASS, typecheck 코드 0.

- [ ] **Step 5: Commit**

```bash
git add server/src server/test/unit/search-query.test.ts server/test/api/search.test.ts
git commit -m "feat(server): message search with trigram FTS and short-term LIKE fallback"
```

---

### Task 29: 글 직렬화·글 검색·해시태그 검색 (`routes/search.ts` 완성)

plan 02 는 `/search/posts`·`/hashtags/search` 를 이 계획의 `routes/search.ts` 몫으로 두고 손대지 않는다(plan 02 Global Constraints "1부 경계 규칙"). 글 작성 API 는 plan 02 가 만들므로, 이 작업의 테스트는 글·해시태그 행을 SQL 로 심는다(`posts_fts` 는 트리거가 채운다).

**Files:**
- Modify: `server/src/lib/search.ts`(새 export 없음 — 글 검색도 `buildMessageSearch`·`highlights` 를 그대로 쓴다), `server/src/routes/search.ts` (`GET /search/posts`, `GET /hashtags/search`), `server/src/lib/serialize.ts` (`serializeMedia`, `serializePosts`)
- Test: `server/test/api/search.test.ts` (`describe("post search")`, `describe("hashtag search")`)

**Interfaces:**
- Consumes: Task 7 `postVisibilitySql`, `canViewPost`, `resolveUserRef`(Task 6); Task 20 `serializePolls`; Task 6 `loadUserSummaries`, `limit("RL_SEARCH")`; Task 28 `buildMessageSearch`, `highlights`; Task 2 `idFloor`.
- Produces:
  - `serializeMedia(row: UploadRow): Media` (계약 4.9) — `url = "/v1/media/<id>"`.
  - `serializePosts(db: D1Database, viewerId: string | null, rows: PostRow[]): Promise<Post[]>` (계약 4.9 — **이 작업이 완성본**, plan 02 는 덧붙이지 않고 고정만): 작성자·`reply_to_author`(`reply_to_id` 의 작성자)·미디어(`post_media` JOIN `uploads`, `status <> 'removed'`, `position` 순, `serializeMedia`)·투표(`serializePolls`)·해시태그(`post_hashtags`, tag 오름차순)·멘션(`post_mentions` JOIN `users` → `MentionRef`)·`viewer`(`post_likes`/`reposts` 에 viewer 행 존재; viewer null 이면 둘 다 false)·`quote`(1단계: 인용 원글이 삭제면 `{available:false, id, reason:"deleted"}`, `canViewPost(viewer, 원글)` 실패면 `"not_visible"`, 아니면 `{available:true, post}` 이고 그 `post.quote` 는 항상 `null`)를 전부 `json_each` 일괄 로드(N+1 금지 — 인용 원글은 두 번째 일괄 로드 1회). 삭제된 글은 `body:""`, `media:[]`, `poll:null`, `hashtags:[]`, `mentions:[]`, `deleted:true`. 응답 순서 = 입력 `rows` 순서.
  - `GET /search/posts?q&author&cursor&limit` (api.md 5.13): 라우트별 `requireAuth` + `limit("RL_SEARCH")`; `q` NFC·trim 1..100(빈 값 400 `fields.q`), `limit` 기본 20 최대 50; `buildMessageSearch(q)` 의 `mode` 로 분기 — fts: `SELECT p.* FROM posts_fts f JOIN posts p ON p.rowid = f.rowid JOIN users u ON u.id = p.author_id WHERE posts_fts MATCH ?` / like: `p.body LIKE '%'||?||'%'`(낱말마다 AND) + `p.id >= idFloor(now - 90일)` + 최근 5000행 한도. 두 모드 공통 조건: `p.deleted_at IS NULL` + `postVisibilitySql(viewerId, "p")`(api.md 3.4 — 정지 작성자·차단 양방향·비공개 비팔로워 제외) + `author` 가 있으면 `p.author_id = resolveUserRef(...)`(없는 사용자 404) + 커서 `p.id < ?`; `ORDER BY p.id DESC LIMIT limit+1`. 응답 `{items:[{post, highlights}], next_cursor}`(`post` = `serializePosts`, `highlights(body, terms)`).
  - `GET /hashtags/search?q&limit` (api.md 5.13): 라우트별 `requireAuth` + `limit("RL_SEARCH")`(`/hashtags` 는 plan 02 `timelines.ts` 와 공유 접두사); `q` 는 NFC + 소문자 + 앞 `#` 하나 제거 + trim, 1..50(아니면 400 `fields.q`); `limit` 기본 10 최대 20. 후보 = `post_hashtags h JOIN posts p ON p.id = h.post_id JOIN users u ON u.id = h.author_id WHERE h.tag >= ?q AND h.tag < ?q || char(1114111)`(접두 일치를 범위 비교로 — `LIKE` 의 `%`·`_` 이스케이프 불필요) `AND h.created_at >= now - 7일 AND p.deleted_at IS NULL AND u.suspended_at IS NULL(또는 만료된 정지) AND u.is_private = 0`(트렌드 집계 data-model 6장과 같은 제외 규칙), `GROUP BY h.tag`; `post_count_24h = SUM(h.created_at >= now - 24시간)`; 정렬 `post_count_24h DESC, tag ASC`. 응답 `{items:[{tag, post_count_24h}]}`(최근 7일엔 있지만 24시간엔 없는 태그는 `0` 으로 포함).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// api/search.test.ts (append)
// local fixtures: insertPost(author, body, opts?: {createdAt?, tags?: string[], deleted?: boolean}) -> PostRow
//   (raw INSERT INTO posts + post_hashtags with author_id/created_at; posts_fts is filled by the trigger), followSql(a, b, "active")
describe("post search", () => {
  it("FTS + LIKE, visibility rules, author filter, highlights", async () => {
    const [v, pub, priv, blk, sus] = await Promise.all([signup(), signup(), signup(), signup(), signup()]);
    const tag = uniqueHandle("p").slice(-6);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(priv.user.id).run();
    const hit = await insertPost(pub, `오늘 배포는 ${tag} 완료`);
    const hidden = await insertPost(priv, `배포는 ${tag} 비공개`);
    await insertPost(blk, `배포는 ${tag} 차단`); await insertPost(sus, `배포는 ${tag} 정지`); await insertPost(pub, `배포는 ${tag} 삭제`, { deleted: true });
    await api(v.token).put(`/v1/blocks/@${blk.handle}`);
    await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), sus.user.id).run();
    const q = (s: string, u = v) => api(u.token).get<any>(`/v1/search/posts?q=${encodeURIComponent(s)}`);
    const r = (await q(`배포는 ${tag}`)).body;
    expect(r.items.map((i: any) => i.post.id)).toEqual([hit.id]);
    expect(r.items[0]).toMatchObject({ post: { author: { id: pub.user.id }, body: `오늘 배포는 ${tag} 완료`, deleted: false, quote: null, viewer: { liked: false, reposted: false } }, highlights: [{ start: 3, length: 3 }, expect.any(Object)] });
    expect((await q(`배포 ${tag}`)).body.items.map((i: any) => i.post.id)).toEqual([hit.id]);            // 2-char term -> LIKE mode
    await followSql(v, priv, "active");
    expect((await q(`배포는 ${tag}`)).body.items.map((i: any) => i.post.id)).toEqual([hidden.id, hit.id]); // active follower now sees private author
    expect((await api(v.token).get<any>(`/v1/search/posts?q=${tag}&author=@${pub.handle}`)).body.items.map((i: any) => i.post.id)).toEqual([hit.id]);
    expect((await api(v.token).get(`/v1/search/posts?q=`)).status).toBe(400);
  });
  it("NFD query; cursor pages newest first; quote of a deleted post is a placeholder", async () => {
    const a = await signup(); const tag = uniqueHandle("c").slice(-6);
    const old = await insertPost(a, `한글검색 ${tag} 1`, { deleted: false }); const gone = await insertPost(a, "원글", { deleted: true });
    const q2 = await insertPost(a, `한글검색 ${tag} 2`, { quoteOf: gone.id });
    const p1 = (await api(a.token).get<any>(`/v1/search/posts?q=${encodeURIComponent("한글검색".normalize("NFD") + " " + tag)}&limit=1`)).body;
    expect(p1.items.map((i: any) => i.post.id)).toEqual([q2.id]); expect(p1.items[0].post.quote).toEqual({ available: false, id: gone.id, reason: "deleted" });
    const p2 = (await api(a.token).get<any>(`/v1/search/posts?q=${encodeURIComponent("한글검색 " + tag)}&limit=1&cursor=${p1.next_cursor}`)).body;
    expect(p2.items.map((i: any) => i.post.id)).toEqual([old.id]); expect(p2.next_cursor).toBeNull();
  });
});
describe("hashtag search", () => {
  it("prefix over last 7 days, 24h counts, excludes private/suspended/deleted, # and NFD ignored", async () => {
    const [a, b, p] = await Promise.all([signup(), signup(), signup()]); const base = "첫눈" + uniqueHandle("h").slice(-5);
    await env.DB.prepare("UPDATE users SET is_private=1 WHERE id=?").bind(p.user.id).run();
    await insertPost(a, "x", { tags: [base] }); await insertPost(b, "y", { tags: [base] });
    await insertPost(a, "z", { tags: [base + "_old"], createdAt: Date.now() - 3 * 86_400_000 });   // inside 7d, outside 24h
    await insertPost(a, "w", { tags: [base + "_gone"], createdAt: Date.now() - 8 * 86_400_000 });  // outside 7d
    await insertPost(p, "s", { tags: [base + "_priv"] }); await insertPost(a, "d", { tags: [base + "_del"], deleted: true });
    const r = await api(a.token).get<any>(`/v1/hashtags/search?q=${encodeURIComponent("#" + base.normalize("NFD"))}`);
    expect(r.body.items).toEqual([{ tag: base, post_count_24h: 2 }, { tag: base + "_old", post_count_24h: 0 }]);
    expect((await api(a.token).get(`/v1/hashtags/search?q=%23`)).status).toBe(400);
    expect((await api(a.token).get<any>(`/v1/hashtags/search?q=${encodeURIComponent(base)}&limit=1`)).body.items).toHaveLength(1);
  });
});
```

(`insertPost` 는 테스트 파일 지역 픽스처: `opts.quoteOf` → `posts.quote_of_id`, `opts.deleted` → `deleted_at=now`(FTS 검색 제외 확인을 위해 본문은 그대로 둔다), `opts.tags` → `post_hashtags` 행(삭제 글이어도 넣는다 — `_del` 케이스는 태그 행이 남아도 `p.deleted_at` 조건으로 빠지는지 본다), `opts.createdAt` → `posts.created_at`·`post_hashtags.created_at`. ID 는 `newId()`(미래 시각 금지 — Task 2).)

- [ ] **Step 2: 실패 확인** — Run: `npx vitest run test/api/search.test.ts` · Expected: 새 describe FAIL(`/v1/search/posts` 404).
- [ ] **Step 3: 구현** — Produces 대로. `serializePosts` 는 plan 02 의 글·타임라인·해시태그 피드가 그대로 쓰므로 계약 4.9 시그니처를 바꾸지 않는다.
- [ ] **Step 4: 전체 통과·타입 확인**

Run: `npx vitest run && npm run typecheck`
Expected: 모든 테스트 파일 PASS(실패 0 — Task 6 `app wiring` 포함), typecheck 코드 0.

- [ ] **Step 5: 로컬 dev 스모크(선택, 수동)**

Run: `npx wrangler d1 migrations apply DB --local && npx wrangler dev` 후 다른 터미널에서 `curl -s localhost:8787/v1/health`
Expected: `{"ok":true}`. (원격 명령은 실행하지 않는다.)

- [ ] **Step 6: Commit**

```bash
git add server/src server/test/api/search.test.ts
git commit -m "feat(server): post serialization, post search and hashtag search"
```
