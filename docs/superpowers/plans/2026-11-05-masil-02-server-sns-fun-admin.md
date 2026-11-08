# masil 서버 2부 — SNS·재미·운영자 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 서버 1부(계정·실시간·채팅) 위에 R2 업로드·미디어, 피드(글·답글·좋아요·리포스트·인용), 팔로우(비공개 승인), 타임라인, 해시태그·트렌드, 글 검색·해시태그 검색, 스토리(+Cron 만료), 알림함, 투표(채팅+피드), 방명록, 상태 메시지·집중 모드, 신고, 전역 운영자 기능을 얹고, `wrangler dev` 대상 로컬 E2E 스모크 스크립트로 전체를 확인한다.

**Architecture:** 1부가 만든 `lib/*`(오류·검증·D1·권한·직렬화·실시간·알림 헬퍼), `services/messages.ts`, DO 두 개(`UserHub`, `TopicHub`)를 그대로 쓰고, 이 계획은 `server-architecture.md` 2장이 SNS·재미·운영에 배정한 라우트 모듈·서비스·cron 파일(`cron/index.ts`·`trends.ts`·`cleanup.ts` 전부 — 1부는 `cron/` 을 만들지 않는다)과 `src/index.ts` 의 `scheduled: handleScheduled` 한 줄, 1부 `lib/serialize.ts` 에 덧붙이는 SNS 직렬화 함수(계약 4.9 의 Upload·TimelineItem·Story·Guestbook·Report)를 추가하고, 1부가 만든 SNS 조각(`serializeMedia`·`serializePosts`, `/search/posts`·`/hashtags/search`, `approveAllPendingFollows`, `/me/status`·`/me/focus`)은 이 계획의 테스트로 고정한다. 모든 쓰기는 `db.batch` 로 커밋한 **뒤** `publishEvent`/`notify` 를 부르고, 글 내용을 싣는 이벤트는 `VisibilitySpec` 가시성 필터를 거친다. 라우트 모듈끼리는 import 하지 않고 공유 로직은 `services/` 로 간다.

**Tech Stack:** Cloudflare Workers(TypeScript 7.0.2) · Hono 4.13.16 · zod 4.6.7 + @hono/zod-validator 0.9.1 · D1 · R2 · Durable Objects · Cron Triggers · wrangler 4.157.0 · vitest 4.1.12 + @cloudflare/vitest-pool-workers 0.23.0 · Node 22+ 내장 `fetch`/`WebSocket`(스모크 스크립트).

**Spec:** `docs/superpowers/specs/2026-11-05-masil-design.md` (최우선). 계약(확정, 전부 읽을 것): `docs/contract/api.md`, `docs/contract/events.md`, `docs/contract/data-model.md`, `docs/contract/server-architecture.md`, `docs/contract/client-architecture.md`(서버 범위 밖, 읽기만). 공통 브리프: `docs/superpowers/plans/00-brief.md`.

## Global Constraints

- **배포 금지**: `wrangler deploy`, `wrangler d1 create`, `wrangler r2 bucket create`, `--remote` 붙은 모든 명령은 **오너 승인 후 only**. 이 계획의 모든 실행은 `vitest` 와 `wrangler dev`(로컬) 뿐이다. 비밀값을 코드·문서에 쓰지 않는다(현재 필요한 비밀 없음).
- 커밋 단계는 실행 세션이 커밋 권한을 받았을 때만 수행하고, `git push` 는 하지 않는다. 커밋 메시지는 영어. (각 태스크의 `git add` 목록은 그 태스크가 손댈 수 있는 파일 전부다 — 실제로 바뀌지 않은 파일은 `git add` 가 그냥 넘어간다.)
- 버전 고정(`server-architecture.md` 1장): hono 4.13.16, zod 4.6.7, @hono/zod-validator 0.9.1, wrangler 4.157.0, vitest **4.1.12**, @cloudflare/vitest-pool-workers 0.23.0, typescript 7.0.2, `compatibility_date: "2026-11-01"`. 새 npm 의존성 추가 금지.
- **1부 경계 규칙**: 1부 소유 파일(`index.ts`, `app.ts`, `lib/*`, `types/*`, `services/{membership,messages,relations,users,polls}.ts`, `routes/{me,presence,users,search,...}.ts`, `test/helpers.ts`, `package.json`)은 **다시 만들지 않는다**. 이 계획이 그 파일에 하는 일은 (a) 지정된 export/라우트 핸들러/등록 한 줄을 **덧붙이기**, (b) 1부가 그 이름을 이미 구현해 두었으면 손대지 않고 이 계획의 테스트로 **고정만** 하기, (c) 1부가 스텁(예외를 던지거나 빈 값을 반환)만 두었으면 그 이름을 처음 쓰는 태스크에서 본문을 채우기 — 셋 중 하나다. 시그니처는 `server-architecture.md` 4장 그대로. 1부 계획(`2026-11-05-masil-01-server-core.md`)의 "소유 경계" 문단과 맞춘 분담:
  - **이 계획이 만든다**(1부는 만들지 않음): `cron/` 디렉터리 전체(`index.ts`·`trends.ts`·`cleanup.ts` — Task 8·10), `src/index.ts` 의 `scheduled: handleScheduled`(Task 8), 계약 4.9 의 `serializeUpload`(Task 1)·`serializeTimelineItems`(Task 7)·`serializeStories`(Task 9)·`serializeGuestbook`(Task 12)·`serializeReports`(Task 16), `lib/notify.ts` 의 `unreadNotificationCount`(Task 2), `lib/text.ts` 의 `normalizeTag`(Task 8 — 1부에 같은 규칙의 export 가 이미 있으면 그것을 씀).
  - **1부가 만들고 이 계획은 고정만 한다**(경계 규칙 (b); 1부에 없거나 스텁이면 (c) 로 그 태스크에서 채운다): `serializeMedia`·`serializePosts`(1부 Task 29 — 이 계획 Task 1·4 가 규칙을 테스트로 고정), `GET /search/posts`·`GET /hashtags/search`(1부 Task 29, `routes/search.ts`·`lib/search.ts` — 이 계획 Task 17 이 고정), `approveAllPendingFollows`(1부 Task 18 — Task 3), `PUT|DELETE /me/status`·`POST|GET|DELETE /me/focus`(1부 Task 16·12 — Task 13). 같은 경로를 두 번 등록하거나 같은 export 를 두 번 만들지 않는다. 이 계획에 적힌 그 이름들의 동작 규칙은 "고정 기준"이다 — 1부 구현이 이 계획의 테스트에 어긋나면 이 계획의 규칙대로 고친다.
  - `PATCH /v1/me/profile` 의 아바타 부착(1부 `routes/me.ts`)은 이 계획이 건드리지 않는다.
- 라우트 모듈은 `<name>Routes` 하나만 export 하고, `app.ts` 에 `app.route("/v1", <name>Routes);` 한 줄을 `server-architecture.md` 2장 순서대로 넣는다(이미 있으면 그대로).
- **공유 경로 접두사(`/users/*`, `/me/*`, `/search/*`)에는 `.use(prefix, requireAuth)` 를 쓰지 않는다** — Hono 가 하위 앱 미들웨어를 부모 라우터에 합쳐 1부 라우트 요청에서도 두 번 돌고 `RL_API` 를 두 번 센다. 이런 경로는 라우트마다 `requireAuth` 를 인자로 단다. 이 계획 고유 접두사(`/posts`, `/timeline`, `/stories`, `/notifications`, `/polls`, `/guestbook`, `/reports`, `/admin`, `/uploads`, `/media`, `/trends`, `/hashtags/:tag/posts`)는 `.use` 를 써도 된다.
- 길이·개수 한도(api.md 0.3·4장): post body 0..500(미디어·투표·인용 없으면 1..500), 미디어 0..4장·장당 1..5,242,880 바이트, pending 업로드 사용자당 20, story body 0..200·TTL 86,400초·활성 30개, guestbook 1..300, status.text 0..64·emoji 1개(≤16바이트)·`expires_in_sec` 60..2,592,000, focus `duration_sec` 60..43,200(기본 `focus_default_min*60`), poll question 1..200·options 2..10 각 1..80·`duration_sec` 60..604,800, report note 0..1000, client_nonce 1..64 `[A-Za-z0-9_-]`, 해시태그 글당 10개·1..50자.
- 리미터(api.md 0.7): `RL_POST`(글·답글·인용·스토리·방명록), `RL_REACT`(좋아요·리포스트·투표), `RL_UPLOAD`(`POST /v1/uploads`), `RL_SOCIAL`(팔로우 PUT·신고). 인증 요청 전부 `RL_API` 추가(1부 `requireAuth` 가 처리).
- 오류는 `err.*`(lib/errors.ts)로만. 볼 권한 없음 = `404 not_found`(존재를 숨김). 상호작용 금지 = `403 action_not_allowed`. 비공개 계정 열람 = `403 private_account`.
- D1: 가변 ID 목록은 `jsonList()` 파라미터 1개, `undefined` 는 `null` 로. 여러 테이블 쓰기는 한 `batch`. 카운터는 **가드 패턴**으로 같은 batch 안에서: 증가 `UPDATE ... SET n = n + 1 WHERE id = ?1 AND NOT EXISTS (<관계 행>)` 를 `INSERT OR IGNORE` **앞에**, 감소 `SET n = MAX(n - 1, 0) WHERE id = ?1 AND EXISTS (<관계 행>)` 를 `DELETE` **앞에** 둔다(동시 중복 토글에도 카운터가 한 번만 움직인다). **팔로우 카운터(`follower_count`/`following_count`)의 `<관계 행>` 은 반드시 `status='active'` 행**이다(data-model 7장: active 만 센다) — 증가 `… AND NOT EXISTS (SELECT 1 FROM follows WHERE follower_id=? AND followee_id=? AND status='active')`, 감소 `… AND EXISTS (SELECT 1 FROM follows WHERE follower_id=? AND followee_id=? AND status='active')`. 그래서 pending 요청의 거절·취소·팔로워 제거는 카운터를 움직이지 않는다. pending → active 승인은 `UPDATE … SET status='active'` **앞에** `… +1 WHERE id=? AND EXISTS (SELECT 1 FROM follows WHERE follower_id=? AND followee_id=? AND status='pending')`(한 쌍에 행이 하나뿐이므로 "행이 있고 아직 active 가 아님" = `NOT EXISTS (… status='active')` 와 같고, 동시 승인 두 번에도 한 번만 오른다).
- N+1 금지: 목록 응답은 `serialize.ts` 함수(관련 행을 `json_each` IN 으로 한 번에 읽음)로만 만든다. 이 계획이 덧붙이는 직렬화 함수도 같다 — 입력 행 수와 무관하게 관련 테이블마다 질의 1회(`… WHERE x IN (SELECT value FROM json_each(?))`), 행마다 질의 금지. 입력이 빈 배열이면 질의 없이 `[]`.
- 커서: `encodeCursor({k, i})`/`decodeCursor`, `parseLimit(raw, 30, 100)`. 정렬 기본 ID 내림차순.
- 알림 alert 문구(서버가 만드는 `title`, ≤80자; `body` = `plainSnippet(본문, 200)` 또는 `""`): `follow` "{이름} 님이 팔로우했어요" · `follow_request` "{이름} 님의 팔로우 요청" · `follow_accepted` "{이름} 님이 팔로우 요청을 수락했어요" · `post_like` "{이름} 님이 글을 좋아해요" · `post_repost` "{이름} 님이 글을 리포스트했어요" · `post_reply` "{이름} 님의 답글" · `post_quote` "{이름} 님이 글을 인용했어요" · `post_mention` "{이름} 님이 글에서 멘션했어요" · `guestbook_entry` "{이름} 님의 방명록". `{이름}` = 행위자 `display_name`. alert kind: `post_mention` → `"mention"`, 나머지 위 목록 → `"social"`.
- 테스트 규칙(`server-architecture.md` 7.2): 매 테스트가 `uniqueHandle()` 새 사용자로 시작하고 저장소 격리에 의존하지 않는다. 한 사용자로 60초에 `RL_POST` 행위(글+스토리+방명록 합산) 10회, 10초에 `RL_REACT` 30회, 60초에 `RL_SOCIAL` 30회를 넘기지 않는다.
- 이 계획에는 **TUI 화면이 없다**(서버 전용). 골든 스냅숏(200×60·120×40·80×24·50×20)은 클라이언트 `sns-fun` 계획 소관이며, 여기서는 해당 없음.
- 문서는 한국어, 코드 식별자·주석·커밋 메시지는 영어.

## Review Focus

1. **동시 중복 토글** — 같은 사용자가 좋아요·리포스트·투표를 거의 동시에 두 번 보내면(네트워크 재시도, 키 연타) 카운터는 한 번만 움직여야 한다. → Task 6 `concurrent double like counts once`, Task 11 `concurrent double vote keeps voter_count 1`.
2. **만료됐지만 cron 이 아직 안 돈 스토리** — `expires_at` 이 지났으면 cleanup 전이라도 피드·사용자 스토리·미디어에서 즉시 사라져야 한다. → Task 9 `expired story hidden before cleanup`.
3. **리포스트·인용을 통한 차단·비공개 누수** — A 가 B 를 차단했는데 A 가 팔로우하는 C 가 B 의 글을 리포스트하면 A 의 팔로잉 타임라인·실시간 이벤트에 나오면 안 된다. → Task 7 `repost of blocked author hidden from timeline and event`.
4. **정지된 작성자** — 정지 즉시 그 사람의 글·미디어가 발견 타임라인·해시태그 피드에서 빠지고, 해제하면 돌아와야 한다. → Task 16 `suspension hides posts and media, unsuspend restores`.
5. **해시태그 정규화** — NFD(자모 분해)로 입력된 `#첫눈`, 대문자 `#Go` 가 NFC·소문자 태그와 같은 피드로 모여야 하고, URL 경로의 `%23`/`#` 접두는 무시돼야 한다. → Task 8 `NFD and uppercase tags normalize to one feed`.

---

## 시작 전 확인 (1부 완료 여부)

- [ ] `cd /Users/sam/Develop/test/terminal-sns/server && npm test && npx tsc --noEmit` — Expected: 1부 테스트 전부 PASS, 타입 오류 0.
- [ ] 1부가 넘겨줘야 할 이름을 **이름마다** 확인한다(개수 세기로는 빠진 이름을 못 찾는다):

```bash
cd /Users/sam/Develop/test/terminal-sns/server
for n in requireAuth requireAdmin getAuth resolveUserRef limit err first all run batch stmt jsonList \
         encodeCursor decodeCursor parseLimit newId idFloor now iso isoOrNull \
         canViewPost canViewUserContent postVisibilitySql isBlockedEither areFriends getSpaceRole requireChannelRead \
         toUserSummary loadUserSummaries serializeNotifications serializePolls serializeMedia serializePosts approveAllPendingFollows \
         publishEvent notify notifyMany fanoutMessageNotifications \
         normalizeText charCount plainSnippet extractHashtags extractMentions \
         r2Key removeUploads buildMessageSearch highlights pollInsertStmts deleteMessage revokeSessions \
         connectWs makeFriends makeSpace signup api uniqueHandle; do
  grep -rqE "export (async )?(function|const|class|let) $n\b" src test/helpers.ts || echo "MISSING(1부) $n"
done
for r in '"/search/posts"' '"/hashtags/search"' '"/me/status"' '"/me/focus"'; do
  grep -rqF "$r" src/routes || echo "MISSING(1부 경로) $r"
done
for n in serializeUpload serializeTimelineItems serializeStories serializeGuestbook serializeReports \
         sniffImage normalizeTag unreadNotificationCount handleScheduled; do
  grep -rqE "export (async )?(function|const) $n\b" src && echo "PRESENT(이 계획 몫) $n — 스텁이면 경계 규칙 (c), 구현돼 있으면 (b)"
done
```

Expected: `MISSING(1부…)` 줄 0개. 있으면 1부 경계 규칙 (c) 에 따라 그 이름·경로를 처음 쓰는 태스크에서 채운다(예: `serializePosts` → Task 4, `/search/posts` → Task 17, `approveAllPendingFollows` → Task 3). 마지막 목록은 이 계획이 만드는 이름이라 보통 아무것도 찍히지 않는다.

---

### Task 1: R2 업로드·미디어 서빙

**Files:**
- Modify(덧붙이기/고정): `server/src/lib/uploads.ts`
- Modify(덧붙이기): `server/src/lib/serialize.ts` — `serializeUpload` (계약 4.9; 1부는 만들지 않음). `serializeMedia` 는 1부 Task 29 것을 고정만(없거나 스텁이면 여기서 채움)
- Create: `server/src/routes/uploads.ts`
- Modify: `server/src/app.ts` (`app.route("/v1", uploadRoutes);` — `searchRoutes` 다음)
- Modify(덧붙이기): `server/test/helpers.ts` (아래 SNS 픽스처)
- Test: `server/test/unit/uploads-sniff.test.ts`, `server/test/api/uploads.test.ts`

**Interfaces:**
- Consumes: `requireAuth`, `getAuth`, `limit("RL_UPLOAD")`, `err.*`, `first/all/run/batch/stmt`, `newId()`, `now()`, `iso()`, `canViewPost(db, viewerId, post: PostRow)`, `canViewUserContent(db, viewerId, ownerId)`, `UploadRow`/`PostRow`/`StoryRow`(types/rows.ts).
- Produces:
  - (고정 기준 — 1부 Task 29 소유) `serializeMedia(row: UploadRow): Media` — 순수 함수(DB 없음): `{ id: row.id, url: "/v1/media/" + row.id, content_type, size, width, height }`(`width/height` 는 NULL 이면 `null`).
  - `serializeUpload(row: UploadRow): Upload` — `{ ...serializeMedia(row), purpose, status, created_at: iso(row.created_at) }`. 필드 순서·이름은 api.md 2장 `Media`/`Upload` 그대로, 다른 필드(`r2_key`, `owner_id`, `attached_to`) 는 내보내지 않는다.
  - `sniffImage(head: Uint8Array): { contentType: Media["content_type"]; ext: "png" | "jpg" | "gif" | "webp"; width: number | null; height: number | null } | null`
  - `r2Key(purpose: "avatar" | "post" | "story", ownerId: string, uploadId: string, ext: string): string` → `"<avatars|posts|stories>/<ownerId>/<uploadId>.<ext>"`
  - `removeUploads(env: Env, uploadIds: string[]): Promise<void>` — `MEDIA.delete(keys)` 후 `status='removed', removed_at=now`
  - `uploadRoutes`: `POST /uploads?purpose=`, `DELETE /uploads/:upload_id`, `GET /media/:upload_id`
  - test/helpers.ts 덧붙임(이후 모든 SNS 테스트가 씀):
    ```ts
    export const PNG_1X1: Uint8Array; // base64 "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="
    export function uploadPng(t: TestUser, purpose: "avatar" | "post" | "story"): Promise<Upload>;   // expects 201
    export function createPost(t: TestUser, body: string, extra?: { media_upload_ids?: string[]; reply_to_id?: string; quote_of_id?: string; poll?: PollInput }): Promise<Post>; // fresh client_nonce, expects 201
    export function follow(a: TestUser, b: TestUser): Promise<"active" | "pending">;                 // PUT /v1/users/@b/follow
    export function makeAdmin(t: TestUser): Promise<void>;                                          // env.DB UPDATE users SET role='admin'
    export function watch(c: WsClient, topics: string[]): Promise<void>;                            // sends {type:"watch",ref,data:{topics}}, awaits reply ok
    ```

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/unit/uploads-sniff.test.ts
import { sniffImage, r2Key } from "../../src/lib/uploads";
import { PNG_1X1 } from "../helpers";
test("png magic + IHDR size", () => {
  expect(sniffImage(PNG_1X1)).toEqual({ contentType: "image/png", ext: "png", width: 1, height: 1 });
});
test("jpeg SOF0 size", () => {
  const jpg = new Uint8Array([0xff,0xd8,0xff,0xe0,0,16,0x4a,0x46,0x49,0x46,0,1,1,0,0,1,0,1,0,0, 0xff,0xc0,0,17,8,0,20,0,30,3,1,0x22,0,2,0x11,1,3,0x11,1]);
  expect(sniffImage(jpg)).toMatchObject({ contentType: "image/jpeg", ext: "jpg", width: 30, height: 20 });
});
test("gif89a and webp VP8X sizes", () => {
  const gif = new Uint8Array([...new TextEncoder().encode("GIF89a"), 3,0, 2,0, 0,0,0]);
  expect(sniffImage(gif)).toMatchObject({ contentType: "image/gif", width: 3, height: 2 });
  const webp = new Uint8Array([...new TextEncoder().encode("RIFF"),0,0,0,0,...new TextEncoder().encode("WEBPVP8X"),10,0,0,0,0,0,0,0, 99,0,0, 49,0,0]);
  expect(sniffImage(webp)).toMatchObject({ contentType: "image/webp", width: 100, height: 50 });
});
test("unknown bytes / truncated header", () => {
  expect(sniffImage(new TextEncoder().encode("hello"))).toBeNull();
  expect(sniffImage(PNG_1X1.slice(0, 12))).toMatchObject({ contentType: "image/png", width: null, height: null });
});
test("r2Key layout", () => { expect(r2Key("story", "U1", "X1", "webp")).toBe("stories/U1/X1.webp"); });

// test/api/uploads.test.ts
import { env } from "cloudflare:test";
import { api, signup, uploadPng, PNG_1X1 } from "../helpers";
const post = (tok: string, purpose: string, body: Uint8Array, ct = "image/png") =>
  api(tok).raw(`/v1/uploads?purpose=${purpose}`, { method: "POST", headers: { "Content-Type": ct }, body });

test("upload stores R2 object with metadata and returns pending Upload", async () => {
  const a = await signup();
  const up = await uploadPng(a, "post");
  expect(up).toMatchObject({ purpose: "post", status: "pending", content_type: "image/png", size: PNG_1X1.length, width: 1, height: 1, url: `/v1/media/${up.id}` });
  expect(Object.keys(up).sort()).toEqual(["content_type", "created_at", "height", "id", "purpose", "size", "status", "url", "width"]);
  expect(typeof up.created_at).toBe("string");
  const row = await env.DB.prepare("SELECT r2_key FROM uploads WHERE id=?").bind(up.id).first<{ r2_key: string }>();
  expect(row!.r2_key).toBe(`posts/${a.user.id}/${up.id}.png`);
  const obj = await env.MEDIA.head(row!.r2_key);
  expect(obj!.httpMetadata!.contentType).toBe("image/png");
  expect(obj!.customMetadata).toEqual({ upload_id: up.id, owner_id: a.user.id, purpose: "post" });
});
test("content-type mismatch -> 415, unknown bytes -> 415, >5MB -> 413, bad purpose -> 400", async () => {
  const a = await signup();
  expect((await post(a.token, "post", PNG_1X1, "image/jpeg")).status).toBe(415);
  expect((await post(a.token, "post", new TextEncoder().encode("not an image"))).status).toBe(415);
  const big = new Uint8Array(5_242_881); big.set(PNG_1X1);
  const r = await post(a.token, "post", big);
  expect(r.status).toBe(413);
  expect((await r.json() as any).error.code).toBe("payload_too_large");
  expect((await post(a.token, "banner", PNG_1X1)).status).toBe(400);
});
test("21st pending upload -> 422 pending_uploads", async () => {
  const a = await signup();
  await env.DB.prepare(`INSERT INTO uploads (id, owner_id, purpose, r2_key, content_type, size, status, created_at)
    SELECT 'P' || ?1 || value, ?1, 'post', 'k/' || ?1 || '/' || value, 'image/png', 1, 'pending', ?2 FROM json_each(?3)`)
    .bind(a.user.id, Date.now(), JSON.stringify([...Array(20).keys()].map(String))).run();
  const r = await post(a.token, "post", PNG_1X1);
  expect(r.status).toBe(422);
  expect((await r.json() as any).error.details).toEqual({ what: "pending_uploads", limit: 20 });
});
test("media: pending visible to owner only; ETag/304; cache headers", async () => {
  const a = await signup(); const b = await signup();
  const up = await uploadPng(a, "post");
  const r = await api(a.token).raw(`/v1/media/${up.id}`, { method: "GET" });
  expect(r.status).toBe(200);
  expect(new Uint8Array(await r.arrayBuffer())).toEqual(PNG_1X1);
  expect(r.headers.get("Cache-Control")).toBe("private, max-age=31536000, immutable");
  const etag = r.headers.get("ETag")!;
  expect((await api(a.token).raw(`/v1/media/${up.id}`, { method: "GET", headers: { "If-None-Match": etag } })).status).toBe(304);
  expect((await api(b.token).raw(`/v1/media/${up.id}`, { method: "GET" })).status).toBe(404);
});
test("delete pending upload: owner only, removes R2 object", async () => {
  const a = await signup(); const b = await signup();
  const up = await uploadPng(a, "avatar");
  expect((await api(b.token).del(`/v1/uploads/${up.id}`)).status).toBe(404);
  expect((await api(a.token).del(`/v1/uploads/${up.id}`)).status).toBe(204);
  expect(await env.MEDIA.head(`avatars/${a.user.id}/${up.id}.png`)).toBeNull();
  expect((await api(a.token).raw(`/v1/media/${up.id}`, { method: "GET" })).status).toBe(404);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/unit/uploads-sniff.test.ts test/api/uploads.test.ts`
Expected: FAIL — `uploadPng`/`PNG_1X1` 미정의 또는 `/v1/uploads` 404.

- [ ] **Step 3: 구현**

- `test/helpers.ts` 에 위 6개 픽스처를 덧붙인다(`createPost` 는 `client_nonce: crypto.randomUUID()`; `watch` 는 `ref: "w-" + 카운터`, `reply.data.ok === true` 를 기다림).
- `lib/serialize.ts` 덧붙임: 위 Produces 의 `serializeUpload`(1부의 `serializeMedia` 를 감싼다). `serializeMedia` 가 1부에 없거나 스텁이면 위 규칙대로 채운다.
- `lib/uploads.ts`: `sniffImage` — PNG `89 50 4E 47 0D 0A 1A 0A` + IHDR(바이트 16..23 BE), JPEG `FF D8 FF` + 마커 순회로 SOF0..SOF15(C4·C8·CC 제외) 의 높이/너비 BE, GIF `GIF87a|GIF89a` + 6..9 LE, WebP `RIFF....WEBP` + `VP8 `(26..29 LE &0x3fff)/`VP8L`(비트 패킹 14비트+1)/`VP8X`(24비트 LE +1). 크기를 못 읽으면 형식만 맞고 `width/height=null`.
- `routes/uploads.ts`:
  - `POST /uploads` (`requireAuth`, `limit("RL_UPLOAD")`): `purpose` zod enum → `Content-Length` 없음 400(`fields["content-length"]="required"`), `> 5_242_880` 이면 본문을 읽기 전에 413. `c.req.arrayBuffer()` 후 실제 길이 0 또는 초과 → 400/413. `sniffImage` null 또는 `contentType` ≠ 요청 `Content-Type` → 415. pending 수 ≥ 20 → `err.limit("pending_uploads", 20)`. `MEDIA.put(key, bytes, { httpMetadata: { contentType }, customMetadata: { upload_id, owner_id, purpose } })` 성공 뒤 `uploads` INSERT. `201 { upload: serializeUpload(row) }`.
  - `DELETE /uploads/:upload_id`: 내 것 + `pending` 아니면 404 → `removeUploads(env,[id])` → 204.
  - `GET /media/:upload_id`: 권한(api.md 5.14) — `removed` → 404, `pending` → 소유자만, `attached_to` 가 `avatar:*` → 누구나, `post:<id>` → 글 행 `deleted_at IS NULL` + `canViewPost`, `story:<id>` → 스토리 `deleted_at IS NULL AND expires_at > now` + `canViewUserContent(viewer, author) === "ok"`. 통과 시 `MEDIA.get(key, { onlyIf: c.req.raw.headers })` — 본문 없는 객체면 `304`(ETag 포함), 있으면 `obj.writeHttpMetadata(headers)`, `ETag: obj.httpEtag`, `Content-Length`, `Cache-Control: private, max-age=31536000, immutable`, 본문 스트림.
- `app.ts` 에 등록 한 줄.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/unit/uploads-sniff.test.ts test/api/uploads.test.ts && npx tsc --noEmit`
Expected: PASS (2 files), 타입 오류 0.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/lib/uploads.ts server/src/lib/serialize.ts server/src/routes/uploads.ts server/src/app.ts server/test/helpers.ts server/test/unit/uploads-sniff.test.ts server/test/api/uploads.test.ts
git commit -m "feat(server): R2 uploads, media serving with ETag and access rules"
```

---

### Task 2: 알림함 라우트 + 알림 헬퍼 고정 + 1부 기능의 알림 훅

**Files:**
- Create: `server/src/routes/notifications.ts`
- Modify(고정/필요 시 수정 + 덧붙이기): `server/src/lib/notify.ts` — export `unreadNotificationCount` 를 덧붙이고 `notify()` 의 `notification.created.unread_count` 도 이 함수로 계산하게 한다; 훅이 빠져 있을 때만 `server/src/routes/friends.ts`, `server/src/routes/invites.ts`, `server/src/services/messages.ts` 의 커밋 직후 지점에 `notify(...)` 한 호출
- Modify: `server/src/app.ts` (`notificationRoutes` — 2장 순서 자리. 아직 없는 모듈의 줄은 건너뛰고 상대 순서만 지킨다)
- Test: `server/test/api/notifications.test.ts`

**Interfaces:**
- Consumes: `notify(rt, input: NotifyInput)`, `notifyMany`, `fanoutMessageNotifications(rt, message, channel)`, `serializeNotifications(db, rows: NotificationRow[])`, `publishEvent(rt, {users:[me]}, "notifications.read", data)`, `connectWs`, `makeFriends`, `makeSpace`.
- Produces:
  - `notificationRoutes` — `GET /notifications?cursor&limit&unread_only`, `GET /notifications/unread-count`, `POST /notifications/read`.
  - `lib/notify.ts` 덧붙임: `export function unreadNotificationCount(db: D1Database, userId: string): Promise<number>` — 안 읽은 알림 수의 **단일 출처**. `SELECT COUNT(*) FROM notifications n WHERE n.user_id = ?1 AND n.read_at IS NULL AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = ?1 AND b.blocked_id = n.actor_id)`(`idx_notifications_unread` 를 탐). `notify()`/`notifyMany()` 가 보내는 `notification.created.unread_count`, 이 라우트의 목록·`unread-count`·`read` 응답, `notifications.read.unread_count` 가 모두 이 함수 값이다(배지가 이벤트와 REST 사이에서 튀지 않게).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/notifications.test.ts
import { env } from "cloudflare:test";
import { api, signup, connectWs, makeFriends, makeSpace } from "../helpers";
const inbox = async (t: any, q = "") => (await api(t.token).get<any>(`/v1/notifications${q}`)).body;

test("friend_request creates inbox row, notification.created and alert on daemon socket", async () => {
  const a = await signup(); const b = await signup();
  const d = await connectWs(b.token, { mode: "notify" });
  const r = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  const n = (await inbox(b)).items[0];
  expect(n).toMatchObject({ type: "friend_request", actor: { id: a.user.id }, subject: { type: "friend_request", id: r.body.request.id }, read: false });
  expect((await d.next("notification.created")).data.unread_count).toBe(1);
  expect((await d.next("alert")).data.alert).toMatchObject({ kind: "friend_request", suppressed: false });
});
test("friend_accepted, space_invite, message_mention, thread_reply hooks", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  const req = await api(a.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  await api(b.token).post(`/v1/friends/requests/${req.body.request.id}/accept`);
  expect((await inbox(a)).items[0]).toMatchObject({ type: "friend_accepted", subject: { type: "user", id: b.user.id } });
  const { space, generalChannelId } = await makeSpace(a, [c]);
  const inv = await api(a.token).post<any>(`/v1/spaces/${space.id}/direct-invites`, { user: `@${b.handle}` });
  expect((await inbox(b)).items[0]).toMatchObject({ type: "space_invite", subject: { type: "space_direct_invite", id: inv.body.invite.id }, context: { space_id: space.id, space_name: space.name } });
  const m = await api(a.token).post<any>(`/v1/channels/${generalChannelId}/messages`, { client_nonce: "n1", body: `@${c.handle} 점심?` });
  expect((await inbox(c)).items[0]).toMatchObject({ type: "message_mention", context: { channel_id: generalChannelId, message_id: m.body.message.id, thread_root_id: null, space_id: space.id } });
  const rep = await api(c.token).post<any>(`/v1/messages/${m.body.message.id}/thread`, { client_nonce: "n2", body: "ㅇㅋ" });
  expect((await inbox(a)).items[0]).toMatchObject({ type: "thread_reply", subject: { type: "message", id: rep.body.message.id }, context: { thread_root_id: m.body.message.id } });
});
test("notify helper: self skipped, blocked actor skipped, dedupe_key collapses", async () => {
  const a = await signup(); const b = await signup();
  const { notify } = await import("../../src/lib/notify");
  const rt = { env, waitUntil: () => {} };
  const base = { userId: b.user.id, type: "follow" as const, actorId: a.user.id, subject: { type: "user" as const, id: a.user.id }, dedupeKey: `follow:${b.user.id}:${a.user.id}`, alert: null };
  await notify(rt, base); await notify(rt, base);
  await notify(rt, { ...base, userId: a.user.id, dedupeKey: undefined });
  expect((await inbox(b)).items.length).toBe(1);
  expect((await inbox(a)).items.length).toBe(0);
  await api(b.token).put(`/v1/blocks/@${a.handle}`);
  await notify(rt, { ...base, type: "follow_request", dedupeKey: undefined });
  const n = await env.DB.prepare("SELECT COUNT(*) AS n FROM notifications WHERE user_id = ?").bind(b.user.id).first<{ n: number }>();
  expect(n!.n).toBe(1);
});
test("list hides notifications whose actor I blocked; unread_only; read by ids/before/all", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  for (const x of [a, c]) await api(x.token).post("/v1/friends/requests", { user: `@${b.handle}` });
  let list = await inbox(b);
  expect(list.unread_count).toBe(2);
  await api(b.token).put(`/v1/blocks/@${c.handle}`);
  list = await inbox(b);
  expect(list.items.map((n: any) => n.actor.id)).toEqual([a.user.id]);
  expect((await api(b.token).get<any>("/v1/notifications/unread-count")).body.unread_count).toBe(1);
  const w = await connectWs(b.token);
  const d = await signup();
  await api(d.token).post("/v1/friends/requests", { user: `@${b.handle}` });
  // event badge uses the same block-filtered count as the REST route: a + d, c's unread row excluded
  expect((await w.next("notification.created")).data.unread_count).toBe(2);
  expect((await api(b.token).get<any>("/v1/notifications/unread-count")).body.unread_count).toBe(2);
  list = await inbox(b);
  expect(list.items[0].actor.id).toBe(d.user.id);
  const r = await api(b.token).post<any>("/v1/notifications/read", { ids: [list.items[0].id] });
  expect(r.body.unread_count).toBe(1);
  expect((await w.next("notifications.read")).data).toEqual({ ids: [list.items[0].id], all: false, before: null, unread_count: 1 });
  expect((await inbox(b, "?unread_only=true")).items.map((n: any) => n.actor.id)).toEqual([a.user.id]);
  expect((await api(b.token).post("/v1/notifications/read", {})).status).toBe(400);
  expect((await api(b.token).post("/v1/notifications/read", { ids: [] })).status).toBe(400);
  expect((await api(b.token).post<any>("/v1/notifications/read", { all: true })).body.unread_count).toBe(0);
});
test("cursor pagination newest first", async () => {
  const b = await signup();
  for (let i = 0; i < 3; i++) { const x = await signup(); await api(x.token).post("/v1/friends/requests", { user: `@${b.handle}` }); }
  const p1 = await inbox(b, "?limit=2");
  expect(p1.items.length).toBe(2); expect(p1.next_cursor).not.toBeNull();
  const p2 = await inbox(b, `?limit=2&cursor=${p1.next_cursor}`);
  expect(p2.items.length).toBe(1); expect(p2.next_cursor).toBeNull();
  expect(p1.items[0].id > p1.items[1].id && p1.items[1].id > p2.items[0].id).toBe(true);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/notifications.test.ts`
Expected: FAIL — `/v1/notifications` 404.

- [ ] **Step 3: 구현**

- `routes/notifications.ts`: 목록 `WHERE n.user_id = ?me AND NOT EXISTS (SELECT 1 FROM blocks b WHERE b.blocker_id = ?me AND b.blocked_id = n.actor_id)`(+ `read_at IS NULL` if `unread_only`), `idx_notifications_user` 로 ID 내림차순 커서(`k=i=id`). 응답 `{ items, next_cursor, unread_count }`. `POST /notifications/read` zod: 정확히 하나 — `{ids: zId[] 1..100}` | `{all: true}` | `{before: zId}` → `UPDATE ... SET read_at=now WHERE user_id=?me AND read_at IS NULL AND (조건)` → 새 `unreadNotificationCount(db, me)` → `publishEvent(rt, {users:[me]}, "notifications.read", { ids|null, all, before|null, unread_count })`. 목록 응답의 `unread_count` 와 `GET /notifications/unread-count` 도 같은 함수.
- `lib/notify.ts`: `unreadNotificationCount` 를 덧붙이고, 1부 `notify()` 안의 `unread_count` 계산을 이 함수 호출로 바꾼다(1부가 차단 필터 없이 따로 세고 있으면 그 식을 지운다). 나머지는 고정: 테스트가 실패하면 api.md 5.18 규칙(행위자=수신자 생략, 양방향 차단 생략, `INSERT OR IGNORE` dedupe, `notification.created{notification, unread_count}` 기록 발행, alert 는 수신자 `user_settings.notify_*` 스위치가 켜졌을 때만) 대로 고친다.
- 1부 훅: 위 테스트의 해당 단언이 실패하는 경우에만, 1부 파일의 커밋 직후 지점에 api.md 5.18 표대로 `notify(rt, …)` 를 한 번 부른다(`friend_request` alert kind `friend_request`, `space_invite` alert kind `invite`, `friend_accepted` alert 없음; `message_mention`/`thread_reply` 는 `fanoutMessageNotifications` 가 담당).

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/notifications.test.ts && npm test`
Expected: PASS, 1부 테스트 회귀 없음.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/routes/notifications.ts server/src/app.ts server/src/lib/notify.ts server/src/routes/friends.ts server/src/routes/invites.ts server/src/services/messages.ts server/test/api/notifications.test.ts
git commit -m "feat(server): notifications inbox and pinned notification hooks"
```

---

### Task 3: 팔로우·비공개 계정 승인·팔로워 목록

**Files:**
- Modify(덧붙이기): `server/src/services/relations.ts` (`follow`; `approveAllPendingFollows` 는 1부 Task 18 것을 고정만 — 없거나 스텁이면, 또는 아래 카운터 규칙에 어긋나면 여기서 채우거나 고친다)
- Create: `server/src/routes/follows.ts`
- Modify(덧붙이기): `server/src/routes/users.ts` (`GET /users/:user/followers`, `GET /users/:user/following`)
- Modify(훅이 빠져 있을 때만): `server/src/routes/me.ts` — `PATCH /me/settings` 의 `is_private: true → false` 커밋 뒤 `approveAllPendingFollows(rt, me)` 한 호출(1부 Task 18 이 이미 연결했으면 손대지 않음)
- Modify: `server/src/app.ts` (`followRoutes`, `blockRoutes` 다음)
- Test: `server/test/api/follows.test.ts`

**Interfaces:**
- Consumes: `resolveUserRef(db, ref, me)`, `isBlockedEither`, `canViewUserContent`, `loadUserSummaries`, `notify`, `notifyMany`, `limit("RL_SOCIAL")`.
- Produces:
  - `follow(rt: Runtime, followerId: string, followeeId: string): Promise<"active" | "pending">`
  - (고정 기준 — 1부 Task 18 소유) `approveAllPendingFollows(rt: Runtime, userId: string): Promise<void>` — 1부 `PATCH /me/settings` 의 `is_private: true → false` 가 부른다
  - `followRoutes`: `PUT|DELETE /users/:user/follow`, `DELETE /users/:user/follower`, `GET /me/follow-requests`, `POST /me/follow-requests/:user/accept|decline` (모두 라우트별 `requireAuth`)

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/follows.test.ts
import { api, signup, follow } from "../helpers";
const me = async (t: any) => (await api(t.token).get<any>("/v1/me")).body.user;
const inbox = async (t: any) => (await api(t.token).get<any>("/v1/notifications")).body.items;

test("public follow is active, counters, follow notification deduped across refollow", async () => {
  const a = await signup(); const b = await signup();
  expect(await follow(a, b)).toBe("active");
  expect(await follow(a, b)).toBe("active");
  expect((await me(b)).follower_count).toBe(1); expect((await me(a)).following_count).toBe(1);
  expect((await api(a.token).del(`/v1/users/@${b.handle}/follow`)).status).toBe(204);
  expect((await me(b)).follower_count).toBe(0);
  await follow(a, b);
  expect((await inbox(b)).filter((n: any) => n.type === "follow").length).toBe(1);
});
test("self -> 409 conflict self; blocked either way -> 403 action_not_allowed", async () => {
  const a = await signup(); const b = await signup();
  const s = await api(a.token).put<any>(`/v1/users/@me/follow`);
  expect(s.status).toBe(409); expect(s.body.error.details.reason).toBe("self");
  await api(b.token).put(`/v1/blocks/@${a.handle}`);
  expect((await api(a.token).put<any>(`/v1/users/@${b.handle}/follow`)).body.error.code).toBe("action_not_allowed");
});
test("private account: pending -> follow_request; accept -> active + follow_accepted; decline silent", async () => {
  const p = await signup(); const a = await signup(); const c = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  expect(await follow(a, p)).toBe("pending");
  expect((await me(p)).follower_count).toBe(0);
  expect((await inbox(p))[0]).toMatchObject({ type: "follow_request", actor: { id: a.user.id } });
  const reqs = await api(p.token).get<any>("/v1/me/follow-requests");
  expect(reqs.body.items.map((x: any) => x.user.id)).toEqual([a.user.id]);
  expect((await api(p.token).post(`/v1/me/follow-requests/@${a.handle}/accept`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(1);
  expect((await inbox(a))[0]).toMatchObject({ type: "follow_accepted", actor: { id: p.user.id } });
  await follow(c, p);
  expect((await api(p.token).post(`/v1/me/follow-requests/@${c.handle}/decline`)).status).toBe(204);
  expect((await inbox(c)).length).toBe(0);
  // pending rows were never counted: decline must not touch the counters
  expect((await me(p)).follower_count).toBe(1); expect((await me(c)).following_count).toBe(0);
  expect((await api(p.token).post(`/v1/me/follow-requests/@${c.handle}/accept`)).status).toBe(404);
});
test("cancelling a pending request or removing a pending follower leaves counters unchanged", async () => {
  const p = await signup(); const a = await signup(); const c = await signup();
  await follow(a, p);
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  expect(await follow(c, p)).toBe("pending");
  expect((await api(c.token).del(`/v1/users/@${p.handle}/follow`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(1); expect((await me(c)).following_count).toBe(0);
  expect(await follow(c, p)).toBe("pending");
  expect((await api(p.token).del(`/v1/users/@${c.handle}/follower`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(1); expect((await me(c)).following_count).toBe(0);
  expect((await api(p.token).get<any>("/v1/me/follow-requests")).body.items).toEqual([]);
});
test("going public approves all pending and notifies each requester", async () => {
  const p = await signup(); const a = await signup(); const b = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(a, p); await follow(b, p);
  await api(p.token).patch("/v1/me/settings", { is_private: false });
  expect((await me(p)).follower_count).toBe(2);
  for (const x of [a, b]) expect((await inbox(x))[0].type).toBe("follow_accepted");
});
test("followers/following lists: active only, private -> 403 private_account, blocked -> 404, remove follower", async () => {
  const p = await signup(); const a = await signup(); const s = await signup(); const blk = await signup();
  await follow(a, p);
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(s, p);
  const own = await api(p.token).get<any>(`/v1/users/@${p.handle}/followers`);
  expect(own.body.items.map((u: any) => u.id)).toEqual([a.user.id]);
  expect((await api(s.token).get<any>(`/v1/users/@${p.handle}/followers`)).body.error.code).toBe("private_account");
  expect((await api(a.token).get<any>(`/v1/users/@${a.handle}/following`)).body.items[0].id).toBe(p.user.id);
  await api(blk.token).put(`/v1/blocks/@${a.handle}`);
  expect((await api(a.token).get(`/v1/users/@${blk.handle}/following`)).status).toBe(404);
  expect((await api(p.token).del(`/v1/users/@${a.handle}/follower`)).status).toBe(204);
  expect((await me(p)).follower_count).toBe(0);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/follows.test.ts`
Expected: FAIL — `/v1/users/@x/follow` 404.

- [ ] **Step 3: 구현**

- `follow`: 자기 자신 → `err.conflict("conflict","self")`; 상대 정지 또는 `isBlockedEither` → `err.forbidden("action_not_allowed")`; 기존 행이 있으면 그 상태 반환. 새 행은 `is_private ? 'pending' : 'active'`(`active` 면 `accepted_at=now`), `active` 면 같은 batch 에서 가드 패턴으로 `follower_count`/`following_count` +1(가드는 `NOT EXISTS (… status='active')`; `pending` 이면 카운터 문 자체를 넣지 않는다). 커밋 뒤 `notify`: `follow`(dedupe `follow:<followee>:<follower>`) 또는 `follow_request`(dedupe `follow_request:<followee>:<follower>`), alert kind `social`.
- `approveAllPendingFollows`(1부 Task 18 구현을 `going public approves all pending…` 테스트로 고정; 어긋나거나 없으면 이 규칙대로): pending 목록 읽기 → batch, 카운터 문을 상태 변경 **앞에**: ① `UPDATE users SET following_count = following_count + 1 WHERE id IN (SELECT follower_id FROM follows WHERE followee_id=?1 AND status='pending')`, ② `UPDATE users SET follower_count = follower_count + (SELECT COUNT(*) FROM follows WHERE followee_id=?1 AND status='pending') WHERE id=?1`, ③ `UPDATE follows SET status='active', accepted_at=now WHERE followee_id=?1 AND status='pending'` (같은 batch 라 사이에 끼어든 행도 세는 집합과 바뀌는 집합이 같다) → `notifyMany` `follow_accepted`(행위자 = userId, 미리 읽은 목록 기준).
- `follows.ts`: 언팔로우·요청 취소(`DELETE /users/:user/follow`)·팔로워 제거(`DELETE /users/:user/follower`, pending 거절 포함)·거절은 라우트 안의 batch(가드 감소 → DELETE). **감소 가드는 `EXISTS (… status='active')`** 이므로 pending 행을 지울 때는 카운터가 움직이지 않는다(Global Constraints 의 팔로우 카운터 규칙). 수락은 pending 만(없으면 404): batch(`follower_count`/`following_count` +1 `WHERE … AND EXISTS (… status='pending')` → `UPDATE follows SET status='active', accepted_at=now WHERE … AND status='pending'`) + `follow_accepted` 알림. 목록은 `created_at` 키 커서.
- `users.ts` 덧붙임: `canViewUserContent(me, owner)` 가 `"hidden"` → 404, `"private"` → `403 private_account`; `active` 만, `accepted_at` 내림차순 커서(`k=accepted_at, i=user_id`), 보는 사람과 차단 관계인 사용자는 목록에서 뺀다.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/follows.test.ts test/api/me.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/services/relations.ts server/src/routes/follows.ts server/src/routes/users.ts server/src/routes/me.ts server/src/app.ts server/test/api/follows.test.ts
git commit -m "feat(server): follows with private-account approval and follower lists"
```

---

### Task 4: 글 작성·조회·삭제 (미디어·해시태그·멘션·멱등)

**Files:**
- Create: `server/src/services/posts.ts`
- Create: `server/src/routes/posts.ts` (`POST /posts`, `GET /posts/:post_id`, `DELETE /posts/:post_id`)
- Modify(고정/필요 시 수정): `server/src/lib/serialize.ts` — `serializePosts` (계약 4.9). 1부 Task 29 가 만든다; 이 태스크와 Task 5·15 의 테스트가 아래 규칙 전부를 고정하고, 어긋나는 부분(또는 없거나 스텁이면 전부)을 이 규칙대로 채운다
- Modify(없을 때만 덧붙이기): `server/src/services/polls.ts` — `pollInsertStmts`(1부가 만들었으면 손대지 않음)
- Modify: `server/src/app.ts` (`postRoutes`, `uploadRoutes` 다음)
- Test: `server/test/api/posts.test.ts`

**Interfaces:**
- Consumes: `pollInsertStmts(db, pollId, creatorId, {type:"post", id}, input, nowMs)`(services/polls.ts — 없으면 이 태스크에서 만든다), `extractHashtags`, `extractMentions`, `normalizeText`, `charCount`, `plainSnippet`, `canViewPost`, `loadUserSummaries`, `serializePolls(db, viewerId, pollIds)`, `serializeMedia`(1부 Task 29), `publishEvent`, `notifyMany`, `removeUploads`, `VisibilitySpec`.
- Produces:
  - (고정 기준 — 1부 Task 29 소유) `serializePosts(db: D1Database, viewerId: string | null, rows: PostRow[]): Promise<Post[]>` (계약 4.9) — 입력 순서 그대로 같은 길이의 배열. 규칙:
    - **일괄 로드**(입력 행 수와 무관하게 각 1회, 모두 `json_each`): (1) 인용 대상 행 `SELECT * FROM posts WHERE id IN (rows 의 quote_of_id)` — **1단계만**(인용된 글의 인용은 읽지 않는다), (2) 답글 부모 `SELECT id, author_id, deleted_at FROM posts WHERE id IN (reply_to_id 들)`, (3) 이제 "직렬화 대상" = rows ∪ 인용 대상 행 — 이 집합 기준으로 `loadUserSummaries`(작성자 + 부모 작성자), 미디어 `SELECT pm.post_id, pm.position, u.* FROM post_media pm JOIN uploads u ON u.id = pm.upload_id WHERE pm.post_id IN (…) AND u.status <> 'removed' ORDER BY pm.post_id, pm.position`(`deletePost` 뒤에도 `post_media` 행은 남으므로 `removed` 업로드는 반드시 거른다) → `serializeMedia`, 투표 `serializePolls(db, viewerId, poll_id 들)`, 해시태그 `SELECT post_id, tag FROM post_hashtags WHERE post_id IN (…) ORDER BY rowid`(삽입 순서 = `extractHashtags` 순서), 멘션 `SELECT pm.post_id, u.id AS user_id, u.handle FROM post_mentions pm JOIN users u ON u.id = pm.user_id WHERE pm.post_id IN (…) ORDER BY pm.rowid` → `MentionRef{user_id, handle}`, `viewerId` 가 있으면 `SELECT post_id FROM post_likes WHERE user_id=?1 AND post_id IN (…)` 와 같은 꼴의 `reposts` 질의, 인용 대상·답글 부모 작성자의 가시성 판정용(아래) 차단·팔로우 질의 각 1회(`blocks` 를 보는 사람 기준 양방향으로 그 작성자 집합에 대해, `follows` 를 `follower_id = viewer AND status='active' AND followee_id IN (…)` 로).
    - 필드: `body`, `reply_to_id`, `root_id`, 카운터 4개는 행 그대로; `reply_to_author` = 부모 작성자 `UserSummary` — 단 보는 사람이 그 부모 글을 볼 수 있을 때만(부모 미삭제 + 아래 인용과 같은 3.4 의 2~4 판정; `viewerId` `null` 이면 작성자 비정지만), 아니면 `null`(최상위 글도 `null`). 부모가 자리표시로 나가는데 그 작성자 이름이 자식 글에 실리면 안 되기 때문; `viewer` = `{liked, reposted}`(viewerId `null` 이면 `{liked:false, reposted:false}` — events.md 4.4); `poll` 은 `poll_id` 가 있으면 그 `Poll`, 없으면 `null`; `deleted = deleted_at !== null`; `created_at = iso(...)`.
    - **인용(`quote`)** — `quote_of_id` 가 `null` 이면 `null`. 아니면 인용 대상 행 Q 로: Q 없음 또는 `Q.deleted_at` 있음 → `{available:false, id, reason:"deleted"}`; 보는 사람이 Q 를 볼 수 없음 → `{available:false, id, reason:"not_visible"}` — `viewerId` 가 있으면 api.md 3.4 의 2~4(작성자 비정지, 보는 사람↔Q 작성자 양방향 차단 없음, Q 작성자 공개 또는 본인 또는 `active` 팔로워)를 위의 일괄 질의 결과로 판정, `viewerId` 가 `null`(이벤트)이면 작성자 정지만 본다(나머지는 발행 쪽 `visibleTo` 필터가 이미 걸렀다 — events.md 4.8); 그 외 → `{available:true, post: <Q 를 같은 규칙으로 직렬화한 Post, 단 post.quote = null>}`.
    - **자리표시(placeholder)** — `deleted_at` 이 있는 행(진짜 삭제든, `/context`·`/replies` 가 "볼 수 없는 글"로 표시한 행이든)은 내용을 **전부** 비운다: `author` = 고정 값 `HIDDEN_AUTHOR = { id: "00000000000000000000000000", handle: "", display_name: "", avatar_url: null, status: null, is_private: false, suspended: false }`(Post.author 는 non-null 이라 null 대신 이 값; 실제 작성자는 보이든 안 보이든 내보내지 않는다 — 차단·비공개 작성자 신원 누수 방지, 클라이언트는 `deleted:true` 면 작성자를 그리지 않는다), `body:""`, `media:[]`, `poll:null`, `quote:null`, `hashtags:[]`, `mentions:[]`, `reply_to_author:null`, `like_count/repost_count/quote_count: 0`, `viewer:{liked:false, reposted:false}`, `deleted:true`. 남기는 것은 구조 정보뿐: `id`, `reply_to_id`, `root_id`, `created_at`, 그리고 하위 트리를 펼치는 데 필요한 `reply_count`(api.md 5.15 `/replies`). 자리표시 행은 미디어·투표·해시태그·멘션·인용 질의 대상에서 뺀다.
  - `interface CreatePostInput { authorId: string; body: string; clientNonce: string; mediaUploadIds?: string[]; replyToId?: string | null; quoteOfId?: string | null; poll?: PollInput | null }`
  - `createPost(rt: Runtime, input: CreatePostInput): Promise<{ post: Post; created: boolean }>`
  - `deletePost(rt: Runtime, postId: string, actorId: string, opts?: { asAdmin?: boolean }): Promise<void>`
  - `postRoutes` (이 태스크 3개 경로, Task 5·6 이 같은 파일에 덧붙임)

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/posts.test.ts
import { env } from "cloudflare:test";
import { api, signup, uploadPng, createPost, follow, connectWs } from "../helpers";

test("create with media, hashtags, mentions; counters; uploads attached", async () => {
  const a = await signup(); const b = await signup();
  const m1 = await uploadPng(a, "post"); const m2 = await uploadPng(a, "post");
  const p = await createPost(a, `오늘 첫 눈 ❄️ #첫눈 #Go #첫눈 @${b.handle}`, { media_upload_ids: [m2.id, m1.id] });
  expect(p.media.map((m) => m.id)).toEqual([m2.id, m1.id]);
  expect(p.hashtags).toEqual(["첫눈", "go"]);
  expect(p.mentions).toEqual([{ user_id: b.user.id, handle: b.handle }]);
  expect(p).toMatchObject({ reply_to_id: null, root_id: null, quote: null, like_count: 0, deleted: false, viewer: { liked: false, reposted: false } });
  expect((await api(a.token).get<any>("/v1/me")).body.user.post_count).toBe(1);
  const st = await env.DB.prepare("SELECT status, attached_to FROM uploads WHERE id=?").bind(m1.id).first<any>();
  expect(st).toEqual({ status: "attached", attached_to: `post:${p.id}` });
  expect((await api(b.token).get<any>("/v1/notifications")).body.items[0]).toMatchObject({ type: "post_mention", context: { post_id: p.id } });
});
test("same client_nonce -> 200 same post", async () => {
  const a = await signup();
  const body = { client_nonce: "dup-1", body: "hi" };
  const r1 = await api(a.token).post<any>("/v1/posts", body);
  const r2 = await api(a.token).post<any>("/v1/posts", body);
  expect([r1.status, r2.status]).toEqual([201, 200]);
  expect(r2.body.post.id).toBe(r1.body.post.id);
});
test("validation: empty body w/o media, 501 chars, 5 media, someone else's upload, reply+quote, poll on reply", async () => {
  const a = await signup(); const b = await signup();
  const P = (j: any) => api(a.token).post<any>("/v1/posts", { client_nonce: crypto.randomUUID(), ...j });
  expect((await P({ body: "  " })).body.error.details.fields.body).toBe("too_short");
  expect((await P({ body: "가".repeat(501) })).body.error.details.fields.body).toBe("too_long");
  const ups = await Promise.all([0, 1, 2, 3, 4].map(() => uploadPng(a, "post")));
  const five = await P({ body: "", media_upload_ids: ups.map((u) => u.id) });
  expect(five.status).toBe(422); expect(five.body.error.details).toEqual({ what: "post_media", limit: 4 });
  const theirs = await uploadPng(b, "post");
  expect((await P({ body: "x", media_upload_ids: [theirs.id] })).body.error.details.fields.media_upload_ids).toBe("invalid");
  const story = await uploadPng(a, "story");
  expect((await P({ body: "x", media_upload_ids: [story.id] })).status).toBe(400);
  const t = await createPost(a, "t");
  expect((await P({ body: "x", reply_to_id: t.id, quote_of_id: t.id })).status).toBe(400);
  expect((await P({ body: "x", reply_to_id: t.id, poll: { question: "q", options: ["a", "b"] } })).status).toBe(400);
  expect((await P({ body: "", poll: { question: "점심?", options: ["국밥", "라멘"] } })).status).toBe(201);
});
test("mention of a user who cannot view (blocked / private author non-follower) is not resolved", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  await api(b.token).put(`/v1/blocks/@${a.handle}`);
  await api(a.token).patch("/v1/me/settings", { is_private: true });
  const p = await createPost(a, `@${b.handle} @${c.handle}`);
  expect(p.mentions).toEqual([]);
  expect((await api(c.token).get<any>("/v1/notifications")).body.items).toEqual([]);
});
test("GET visibility and timeline.item_created to follower + author", async () => {
  const a = await signup(); const f = await signup(); const x = await signup();
  await follow(f, a);
  const wf = await connectWs(f.token); const wa = await connectWs(a.token);
  const p = await createPost(a, "hello");
  expect((await wf.next("timeline.item_created")).data.item).toMatchObject({ id: p.id, type: "post", reposted_by: null });
  expect((await wa.next("timeline.item_created")).seq).toBeNull();
  expect((await api(x.token).get<any>(`/v1/posts/${p.id}`)).body.post.id).toBe(p.id);
  await api(a.token).put(`/v1/blocks/@${x.handle}`);
  expect((await api(x.token).get(`/v1/posts/${p.id}`)).status).toBe(404);
});
test("delete: author only; soft delete, R2 removed, post.deleted to followers, post_count decremented", async () => {
  const a = await signup(); const f = await signup();
  await follow(f, a);
  const m = await uploadPng(a, "post");
  const p = await createPost(a, "bye", { media_upload_ids: [m.id] });
  const wf = await connectWs(f.token);
  expect((await api(f.token).del(`/v1/posts/${p.id}`)).status).toBe(403);
  expect((await api(a.token).del(`/v1/posts/${p.id}`)).status).toBe(204);
  expect((await wf.next("post.deleted")).data).toEqual({ post_id: p.id });
  expect((await api(a.token).get(`/v1/posts/${p.id}`)).status).toBe(404);
  expect(await env.MEDIA.head(`posts/${a.user.id}/${m.id}.png`)).toBeNull();
  const row = await env.DB.prepare("SELECT body, deleted_by FROM posts WHERE id=?").bind(p.id).first<any>();
  expect(row).toEqual({ body: "", deleted_by: a.user.id });
  expect((await api(a.token).get<any>("/v1/me")).body.user.post_count).toBe(0);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/posts.test.ts`
Expected: FAIL — `/v1/posts` 404.

- [ ] **Step 3: 구현**

- `createPost` 순서: (1) nonce 로 기존 글 조회 → 있으면 `{created:false}`. (2) 검증: 본문 길이(미디어·투표·인용 없으면 1..500), `replyToId`+`quoteOfId` 동시 → `fields.reply_to_id="not_allowed"`, 답글+poll → `fields.poll="not_allowed"`, 미디어 5개 이상 → `err.limit("post_media",4)`, 중복·남의 것·`purpose≠post`·`status≠pending` → `fields.media_upload_ids="invalid"`. (3) 대상 글(Task 5 가 채움). (4) 멘션 해석: `extractMentions` → 존재·비정지 사용자 중 새 글을 볼 수 있는 사람만(작성자와 양방향 차단 없음, 작성자가 비공개면 작성자 본인 또는 `active` 팔로워). (5) 한 batch: `pollInsertStmts`(있으면) → `posts` INSERT(`root_id = parent.root_id ?? parent.id`) → `post_media`(요청 순서 = position) → `uploads SET status='attached', attached_to='post:<id>'` → `post_hashtags`(`extractHashtags`, `author_id`, `created_at`) → `post_mentions` → `users.post_count+1` (+ Task 5 의 부모/인용 카운터). UNIQUE(nonce) 충돌이면 (1) 로 돌아가 200. (6) 커밋 뒤: 최상위·인용이면 `publishEvent(rt, {followers: authorId}, "timeline.item_created", {item}, {ephemeral:true, includeSelf: authorId, visibleTo})` — `item = { id: post.id, type: "post", post: (await serializePosts(db, null, [row]))[0], reposted_by: null, at: iso(row.created_at) }`(이벤트용 viewer 기본값; 응답용 `post` 는 따로 `serializePosts(db, authorId, [row])`), `visibleTo = { authors: [author, 인용 원글 작성자?], privateAuthors: 그중 is_private }`. 멘션 알림 `post_mention`(subject post, context.post_id, alert kind `mention`) — Task 5 의 `post_reply`/`post_quote` 대상과 같은 사람이면 멘션 알림은 생략.
- `deletePost`: 작성자 아니면(그리고 `asAdmin` 아님) 볼 수 있으면 `err.forbidden()` 아니면 404. 한 batch: `posts SET body='', deleted_at, deleted_by`, `post_hashtags`·`post_mentions` DELETE, `reposts` DELETE, `post_count` 가드 감소(이미 삭제된 글이면 아무 것도 안 함) (+ 부모 `reply_count`/인용 `quote_count` 가드 감소). 커밋 뒤 `removeUploads(env, 미디어 ids)`, `post.deleted` 를 `{topic:"post:<id>"}` 와 `{followers: authorId}`(includeSelf) 에 휘발로 발행(부모가 있으면 부모 토픽 `post.stats` 도).
- `GET /posts/:post_id`: 행 없음·`deleted_at` 있음·`!canViewPost` → 404.
- `lib/serialize.ts`: 1부 Task 29 의 `serializePosts` 를 위 테스트(+ Task 5 의 자리표시·인용·`reply_to_author` 단언, Task 15 의 removed 미디어 단언)로 고정한다. 어긋나면 Interfaces 의 규칙대로 고치고, 없거나 스텁이면 규칙 전부를 구현한다. `HIDDEN_AUTHOR` 는 모듈 비공개 상수. 행마다 질의하는 코드가 없어야 한다(관련 테이블마다 1회).

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/posts.test.ts && npx tsc --noEmit`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/services/posts.ts server/src/routes/posts.ts server/src/lib/serialize.ts server/src/services/polls.ts server/src/app.ts server/test/api/posts.test.ts
git commit -m "feat(server): create/get/delete posts with media, hashtags, mentions, idempotency"
```

---

### Task 5: 답글·인용·문맥·답글 목록

**Files:**
- Modify: `server/src/services/posts.ts` (대상 글 검증·카운터·알림·이벤트)
- Modify: `server/src/routes/posts.ts` (`GET /posts/:post_id/context`, `/replies`, `/quotes`)
- Modify(고정/필요 시 수정): `server/src/lib/serialize.ts` — `serializePosts` 의 자리표시·인용 `reason`·`reply_to_author` 규칙(Task 4 Interfaces)이 아래 테스트에 어긋날 때만
- Test: `server/test/api/posts-replies.test.ts`

**Interfaces:**
- Consumes: Task 4 `createPost`, `canViewPost`, `postVisibilitySql(viewerId, "p")`, Task 4 `serializePosts`(자리표시·인용·`reply_to_author` 규칙 포함), `watch(c, topics)`, `uploadPng`, `TopicHub.publish` 경유 `publishEvent(rt, {topic}, …, {visibleTo})`.
- Produces: 같은 `createPost` 의 답글·인용 경로, 라우트 3개.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/posts-replies.test.ts
import { api, signup, createPost, follow, connectWs, watch, uploadPng } from "../helpers";

test("reply: root_id, parent reply_count, post_reply notification, post.reply_created + post.stats on parent topic", async () => {
  const a = await signup(); const b = await signup();
  const root = await createPost(a, "root");
  const w = await connectWs(a.token); await watch(w, [`post:${root.id}`]);
  const r1 = await createPost(b, "r1", { reply_to_id: root.id });
  const r2 = await createPost(a, "r2", { reply_to_id: r1.id });
  expect(r1).toMatchObject({ reply_to_id: root.id, root_id: root.id, reply_to_author: { id: a.user.id } });
  expect(r2.root_id).toBe(root.id);
  expect((await w.next("post.reply_created")).data).toMatchObject({ parent_id: root.id, post: { id: r1.id } });
  expect((await w.next("post.stats")).data).toMatchObject({ post_id: root.id, reply_count: 1 });
  expect((await api(a.token).get<any>("/v1/notifications")).body.items[0]).toMatchObject({ type: "post_reply", subject: { type: "post", id: r1.id }, context: { post_id: r1.id } });
});
test("post.reply_created not delivered to watcher blocked by reply author or who can't see a private replier", async () => {
  const a = await signup(); const b = await signup(); const v = await signup(); const p = await signup();
  const root = await createPost(a, "root");
  const wv = await connectWs(v.token); await watch(wv, [`post:${root.id}`]);
  await api(b.token).put(`/v1/blocks/@${v.handle}`);
  await createPost(b, "hidden reply", { reply_to_id: root.id });
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await createPost(p, "private reply", { reply_to_id: root.id });
  await expect(wv.next("post.reply_created", undefined, 500)).rejects.toThrow();
});
test("replies list: ascending, hides blocked author's reply, keeps deleted reply only if it has children", async () => {
  const a = await signup(); const b = await signup(); const c = await signup(); const v = await signup();
  const root = await createPost(a, "root");
  const x = await createPost(b, "x", { reply_to_id: root.id });
  const y = await createPost(c, "y", { reply_to_id: root.id });
  const z = await createPost(a, "z", { reply_to_id: root.id });
  await createPost(a, "child", { reply_to_id: y.id });
  await api(c.token).del(`/v1/posts/${y.id}`); await api(a.token).del(`/v1/posts/${z.id}`);
  await api(v.token).put(`/v1/blocks/@${b.handle}`);
  const r = await api(v.token).get<any>(`/v1/posts/${root.id}/replies`);
  expect(r.body.items.map((p: any) => [p.id, p.deleted])).toEqual([[y.id, true]]);
  expect((await api(a.token).get<any>(`/v1/posts/${root.id}/replies`)).body.items.map((p: any) => p.id)).toEqual([x.id, y.id]);
});
test("context returns ancestors root-first; invisible ancestor is a deleted placeholder", async () => {
  const a = await signup(); const b = await signup(); const v = await signup();
  const root = await createPost(a, "root");
  const m = await uploadPng(b, "post");
  const mid = await createPost(b, `mid #비밀태그 @${a.handle}`, { reply_to_id: root.id, media_upload_ids: [m.id] });
  await api(a.token).put(`/v1/posts/${mid.id}/like`);
  const leaf = await createPost(a, "leaf", { reply_to_id: mid.id });
  await api(v.token).put(`/v1/blocks/@${b.handle}`);
  const r = await api(v.token).get<any>(`/v1/posts/${leaf.id}/context`);
  expect(r.body.ancestors.map((p: any) => [p.id, p.deleted, p.body])).toEqual([[root.id, false, "root"], [mid.id, true, ""]]);
  // placeholder carries structure only: no author identity, media, tags, mentions or counts of the hidden post
  expect(r.body.ancestors[1]).toMatchObject({
    author: { id: "00000000000000000000000000", handle: "", display_name: "", avatar_url: null },
    reply_to_id: root.id, root_id: root.id, reply_to_author: null,
    media: [], poll: null, quote: null, hashtags: [], mentions: [],
    like_count: 0, repost_count: 0, quote_count: 0, reply_count: 1, viewer: { liked: false, reposted: false } });
  expect(r.body.post).toMatchObject({ id: leaf.id, reply_to_author: null }); // parent hidden from v -> its author is not named either
  expect((await api(a.token).get<any>(`/v1/posts/${leaf.id}`)).body.post.reply_to_author).toMatchObject({ id: b.user.id });
});
test("quote: quote_count, post_quote notification, quotes list; quoting private or invisible post rejected", async () => {
  const a = await signup(); const b = await signup(); const p = await signup();
  const orig = await createPost(a, "orig");
  const q = await createPost(b, "lol", { quote_of_id: orig.id });
  expect(q.quote).toMatchObject({ available: true, post: { id: orig.id, quote: null } });
  expect((await api(a.token).get<any>(`/v1/posts/${orig.id}`)).body.post.quote_count).toBe(1);
  expect((await api(a.token).get<any>("/v1/notifications")).body.items[0]).toMatchObject({ type: "post_quote", subject: { id: q.id } });
  expect((await api(a.token).get<any>(`/v1/posts/${orig.id}/quotes`)).body.items.map((x: any) => x.id)).toEqual([q.id]);
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  const pp = await createPost(p, "secret");
  await follow(b, p); await api(p.token).post(`/v1/me/follow-requests/@${b.handle}/accept`);
  const r = await api(b.token).post<any>("/v1/posts", { client_nonce: "q2", body: "x", quote_of_id: pp.id });
  expect(r.body.error.code).toBe("action_not_allowed");
  expect((await api(a.token).post("/v1/posts", { client_nonce: "q3", body: "x", reply_to_id: pp.id })).status).toBe(404);
  await api(a.token).del(`/v1/posts/${orig.id}`);
  expect((await api(b.token).get<any>(`/v1/posts/${q.id}`)).body.post.quote).toEqual({ available: false, id: orig.id, reason: "deleted" });
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/posts-replies.test.ts`
Expected: FAIL — `reply_to_id` 무시됨 / `/context` 404.

- [ ] **Step 3: 구현**

- `createPost` 대상 글: 행 없음·삭제·`!canViewPost` → `err.notFound()`. 인용 대상 작성자가 비공개면(보는 사람이 누구든) `err.forbidden("action_not_allowed")`. batch 에 부모 `reply_count+1` 또는 인용 `quote_count+1`. 커밋 뒤:
  - 답글 → `publishEvent(rt, {topic:"post:<parent>"}, "post.reply_created", {parent_id, post: (await serializePosts(db, null, [row]))[0]}, {ephemeral:true, visibleTo:{authors:[replier, parentAuthor], privateAuthors}})` + 부모 토픽 `post.stats`, 알림 `post_reply`(부모 작성자, subject/context = 답글).
  - 인용 → 원글 토픽 `post.stats`, 알림 `post_quote`(원글 작성자). `timeline.item_created` 는 Task 4 경로.
- `GET /context`: 부모 체인을 최대 20개 올라가며 수집 후 뒤집기. 볼 수 없거나 삭제된 조상은 행 복사본에 `deleted_at = row.deleted_at ?? now` 를 넣어 `serializePosts` 에 넘긴다 — 그러면 Task 4 의 **자리표시 규칙**이 작성자(`HIDDEN_AUTHOR`)·본문·미디어·투표·인용·해시태그·멘션·좋아요/리포스트/인용 수·viewer 를 전부 비우고 `id`·`reply_to_id`·`root_id`·`created_at`·`reply_count` 만 남긴다(본문만 비우는 것은 누수다 — 위 context 테스트가 고정). 대상 글 자체를 못 보면 404.
- `GET /replies`: `reply_to_id = ?`, ID 오름차순 커서. 조건 = `(p.deleted_at IS NULL AND <postVisibilitySql>) OR (p.deleted_at IS NOT NULL AND p.reply_count > 0 AND <보는 사람↔작성자 차단 없음>)`. 삭제된 답글은 같은 자리표시 규칙으로 나간다(`reply_count` 로 하위 트리를 펼칠 수 있게 남김). 부모를 못 보면 404.
- `GET /quotes`: `quote_of_id = ?` + `postVisibilitySql`, ID 내림차순.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/posts.test.ts test/api/posts-replies.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/services/posts.ts server/src/routes/posts.ts server/src/lib/serialize.ts server/test/api/posts-replies.test.ts
git commit -m "feat(server): replies, quotes, context and reply tree listing with visibility filter"
```

---

### Task 6: 좋아요·리포스트

**Files:**
- Modify: `server/src/routes/posts.ts` (`PUT|DELETE /posts/:post_id/like`, `GET /likes`, `PUT|DELETE /repost`, `GET /reposts`)
- Test: `server/test/api/posts-likes.test.ts`

**Interfaces:**
- Consumes: `canViewPost`, `limit("RL_REACT")`, `loadUserSummaries`, Task 4 `serializePosts`(이벤트 item 용, viewer `null`), `TimelineRow`(types/rows.ts; 1부에 없으면 `export interface TimelineRow { item_id: string; type: "post" | "repost"; post_id: string; reposter_id: string | null; at: number }` 를 덧붙인다), `notify`, `publishEvent`.
- Produces: 응답 `{ post_id, liked, like_count }`, `{ post_id, reposted, repost_count }`; 리포스트 `timeline.item_created`(`type:"repost"`, `id` = `reposts.id`).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/posts-likes.test.ts
import { api, signup, createPost, follow, connectWs, watch } from "../helpers";

test("like toggles, post.stats on topic, post_like notification survives unlike and is deduped", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "p");
  const w = await connectWs(a.token); await watch(w, [`post:${p.id}`]);
  expect((await api(b.token).put<any>(`/v1/posts/${p.id}/like`)).body).toEqual({ post_id: p.id, liked: true, like_count: 1 });
  expect((await w.next("post.stats")).data.like_count).toBe(1);
  expect((await api(b.token).del<any>(`/v1/posts/${p.id}/like`)).body).toEqual({ post_id: p.id, liked: false, like_count: 0 });
  await api(b.token).put(`/v1/posts/${p.id}/like`);
  const n = (await api(a.token).get<any>("/v1/notifications")).body.items.filter((x: any) => x.type === "post_like");
  expect(n.length).toBe(1);
  expect((await api(b.token).get<any>(`/v1/posts/${p.id}`)).body.post.viewer).toEqual({ liked: true, reposted: false });
  expect((await api(a.token).get<any>(`/v1/posts/${p.id}/likes`)).body.items.map((u: any) => u.id)).toEqual([b.user.id]);
});
test("concurrent double like counts once", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "p");
  await Promise.all([api(b.token).put(`/v1/posts/${p.id}/like`), api(b.token).put(`/v1/posts/${p.id}/like`)]);
  expect((await api(a.token).get<any>(`/v1/posts/${p.id}`)).body.post.like_count).toBe(1);
  await Promise.all([api(b.token).del(`/v1/posts/${p.id}/like`), api(b.token).del(`/v1/posts/${p.id}/like`)]);
  expect((await api(a.token).get<any>(`/v1/posts/${p.id}`)).body.post.like_count).toBe(0);
});
test("repost: own post allowed, timeline.item_created(repost) to reposter's followers, notification, list", async () => {
  const a = await signup(); const b = await signup(); const f = await signup();
  await follow(f, b);
  const p = await createPost(a, "p");
  const wf = await connectWs(f.token);
  const r = await api(b.token).put<any>(`/v1/posts/${p.id}/repost`);
  expect(r.body).toEqual({ post_id: p.id, reposted: true, repost_count: 1 });
  const ev = (await wf.next("timeline.item_created", (d) => d.item.type === "repost")).data.item;
  expect(ev).toMatchObject({ type: "repost", post: { id: p.id }, reposted_by: { id: b.user.id } });
  expect(ev.id).not.toBe(p.id);
  expect((await api(a.token).get<any>("/v1/notifications")).body.items[0].type).toBe("post_repost");
  expect((await api(a.token).get<any>(`/v1/posts/${p.id}/reposts`)).body.items.map((u: any) => u.id)).toEqual([b.user.id]);
  expect((await api(a.token).put(`/v1/posts/${p.id}/repost`)).status).toBe(200);
});
test("private author's post: repost forbidden even for approved follower and author; blocked liker gets 404", async () => {
  const p = await signup(); const f = await signup(); const x = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(f, p); await api(p.token).post(`/v1/me/follow-requests/@${f.handle}/accept`);
  const post = await createPost(p, "secret");
  expect((await api(f.token).put<any>(`/v1/posts/${post.id}/repost`)).body.error.code).toBe("action_not_allowed");
  expect((await api(p.token).put<any>(`/v1/posts/${post.id}/repost`)).body.error.code).toBe("action_not_allowed");
  expect((await api(f.token).put(`/v1/posts/${post.id}/like`)).status).toBe(200);
  await api(p.token).put(`/v1/blocks/@${x.handle}`);
  expect((await api(x.token).put(`/v1/posts/${post.id}/like`)).status).toBe(404);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/posts-likes.test.ts`
Expected: FAIL — `/like` 404.

- [ ] **Step 3: 구현**

- 공통: 글 없음·삭제·`!canViewPost` → 404. 쓰기는 Global Constraints 의 가드 패턴 batch(`UPDATE posts SET like_count = like_count + 1 WHERE id=?1 AND NOT EXISTS (SELECT 1 FROM post_likes WHERE post_id=?1 AND user_id=?2)` → `INSERT OR IGNORE post_likes`). 응답 카운트는 batch 뒤 다시 읽은 값. 상태가 실제로 바뀐 경우에만 `post.stats`(토픽, 휘발) 와 알림.
- 좋아요 알림 `post_like` dedupe `post_like:<post>:<actor>`, 리포스트 `post_repost` dedupe `post_repost:<post>:<actor>`; 둘 다 alert kind `social`. 취소해도 알림은 지우지 않는다.
- 리포스트: 원글 작성자 비공개 → `action_not_allowed`. 새 행 `reposts.id = newId()`. 커밋 뒤 `publishEvent(rt, {followers: reposter}, "timeline.item_created", {item}, {ephemeral:true, includeSelf: reposter, visibleTo:{authors:[reposter, author, 인용 원글 작성자?], privateAuthors}})` — `item = { id: reposts.id, type: "repost", post: (await serializePosts(db, null, [원글 row]))[0], reposted_by: <reposter UserSummary>, at: iso(reposts.created_at) }`(이벤트용 viewer 기본값; Task 7 의 `serializeTimelineItems` 와 같은 모양). 취소는 가드 감소 → DELETE.
- 목록: likes `k=created_at,i=user_id`, reposts `k=i=reposts.id`; 보는 사람과 차단 관계인 사용자 제외.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/posts-likes.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/routes/posts.ts server/src/types/rows.ts server/test/api/posts-likes.test.ts
git commit -m "feat(server): likes and reposts with guarded counters and timeline events"
```

---

### Task 7: 타임라인(팔로잉·발견) + 사용자 글 탭

**Files:**
- Create: `server/src/routes/timelines.ts` (`GET /timeline/following`, `GET /timeline/discover`; Task 8 이 `/hashtags/:tag/posts`, `/trends` 를 덧붙임)
- Modify(덧붙이기): `server/src/routes/users.ts` (`GET /users/:user/posts?filter=posts|replies|media`)
- Modify(덧붙이기): `server/src/lib/serialize.ts` — `serializeTimelineItems` (계약 4.9)
- Modify: `server/src/app.ts` (`timelineRoutes`, `postRoutes` 다음)
- Test: `server/test/api/timelines.test.ts`

**Interfaces:**
- Consumes: `postVisibilitySql`, Task 4 `serializePosts`, Task 6 `TimelineRow`, `loadUserSummaries`, `canViewUserContent`, `encodeCursor/decodeCursor/parseLimit`.
- Produces:
  - `serializeTimelineItems(db: D1Database, viewerId: string, rows: TimelineRow[]): Promise<TimelineItem[]>` (계약 4.9) — 일괄: `SELECT * FROM posts WHERE id IN (고유 post_id 들)` 1회 → `serializePosts(db, viewerId, 그 행들)` 1회(같은 글이 여러 항목에 나와도 한 번만 직렬화) → 리포스트한 사람 `loadUserSummaries(reposter_id 들)` 1회. 항목 = `{ id: item_id, type, post, reposted_by: type === "repost" ? summary : null, at: iso(at) }`, 입력 순서 유지, 글 행이 없는 항목은 뺀다. 가시성은 호출 쪽 SQL 이 이미 걸렀다고 가정한다(직렬화가 거르지 않음). 이벤트 payload 는 이 함수를 쓰지 않고 Task 4·6 처럼 `serializePosts(db, null, …)` 로 같은 모양을 만든다.
  - 응답 `{ items: TimelineItem[], next_cursor: string | null, gap: boolean }`; 모듈 비공개 `pageSince(rows, limit)` 규칙: `since` 가 있으면 `id > since` 를 최신순 `limit+1` 개 읽어 `gap = rows.length > limit`, `next_cursor=null`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/timelines.test.ts
import { api, signup, createPost, follow, connectWs } from "../helpers";
const tl = async (t: any, q = "") => (await api(t.token).get<any>(`/v1/timeline/following${q}`)).body;

test("following: me + active followees, top-level and quotes, no replies, repost deduped to latest", async () => {
  const me = await signup(); const a = await signup(); const b = await signup(); const z = await signup();
  await follow(me, a); await follow(me, b);
  const mine = await createPost(me, "mine");
  const pa = await createPost(a, "a");
  await createPost(a, "reply", { reply_to_id: pa.id });
  const pz = await createPost(z, "z");
  await api(a.token).put(`/v1/posts/${pz.id}/repost`);
  await api(b.token).put(`/v1/posts/${pz.id}/repost`);
  const items = (await tl(me)).items;
  expect(items.map((i: any) => [i.type, i.post.id, i.reposted_by?.id ?? null])).toEqual([
    ["repost", pz.id, b.user.id], ["post", pa.id, null], ["post", mine.id, null]]);
});
test("cursor and since/gap", async () => {
  const me = await signup();
  const p = [];
  for (let i = 0; i < 4; i++) p.push(await createPost(me, `p${i}`));
  const page1 = await tl(me, "?limit=3");
  expect(page1.items.map((i: any) => i.id)).toEqual([p[3].id, p[2].id, p[1].id]);
  expect((await tl(me, `?limit=3&cursor=${page1.next_cursor}`)).items.map((i: any) => i.id)).toEqual([p[0].id]);
  expect(await tl(me, `?since=${p[1].id}&limit=5`)).toMatchObject({ gap: false, next_cursor: null });
  expect((await tl(me, `?since=${p[0].id}&limit=2`)).gap).toBe(true);
  expect((await tl(me)).gap).toBe(false);
});
test("repost of blocked author hidden from timeline and event", async () => {
  const me = await signup(); const c = await signup(); const blocked = await signup();
  await follow(me, c);
  await api(me.token).put(`/v1/blocks/@${blocked.handle}`);
  const bp = await createPost(blocked, "blocked post");
  const w = await connectWs(me.token);
  await api(c.token).put(`/v1/posts/${bp.id}/repost`);
  await expect(w.next("timeline.item_created", undefined, 500)).rejects.toThrow();
  expect((await tl(me)).items.find((i: any) => i.post.id === bp.id)).toBeUndefined();
});
test("discover: public non-suspended top-level only, excludes private and blocked, no reposts", async () => {
  const v = await signup(); const pub = await signup(); const priv = await signup(); const blk = await signup();
  const a = await createPost(pub, "pub");
  await api(priv.token).patch("/v1/me/settings", { is_private: true });
  const b = await createPost(priv, "priv");
  const c = await createPost(blk, "blk");
  await api(blk.token).put(`/v1/blocks/@${v.handle}`);
  await api(pub.token).put(`/v1/posts/${a.id}/repost`);
  const items = (await api(v.token).get<any>(`/v1/timeline/discover?since=${a.id.slice(0, 10)}${"0".repeat(16)}&limit=100`)).body.items;
  const ids = items.map((i: any) => i.id);
  expect(ids).toContain(a.id); expect(ids).not.toContain(b.id); expect(ids).not.toContain(c.id);
  expect(items.every((i: any) => i.type === "post")).toBe(true);
});
test("users/{user}/posts filters; private -> 403 private_account", async () => {
  const a = await signup(); const v = await signup(); const o = await signup();
  const top = await createPost(a, "top");
  const rep = await createPost(a, "rep", { reply_to_id: top.id });
  const op = await createPost(o, "o"); await api(a.token).put(`/v1/posts/${op.id}/repost`);
  const tab = async (f: string) => (await api(v.token).get<any>(`/v1/users/@${a.handle}/posts?filter=${f}`)).body.items.map((i: any) => i.post.id);
  expect(await tab("posts")).toEqual([op.id, top.id]);
  expect(await tab("replies")).toEqual([rep.id]);
  expect(await tab("media")).toEqual([]);
  await api(a.token).patch("/v1/me/settings", { is_private: true });
  expect((await api(v.token).get<any>(`/v1/users/@${a.handle}/posts`)).body.error.code).toBe("private_account");
});
test("profile posts tab hides reposts the viewer can't see (blocked author, quoted blocked author, later-private author)", async () => {
  const a = await signup(); const b = await signup(); const c = await signup(); const p = await signup(); const v = await signup();
  await follow(a, p);
  const bp = await createPost(b, "b post");
  const pp = await createPost(p, "p post");
  const cq = await createPost(c, "quoting b", { quote_of_id: bp.id });
  for (const id of [bp.id, pp.id, cq.id]) await api(a.token).put(`/v1/posts/${id}/repost`);
  await api(b.token).put(`/v1/blocks/@${v.handle}`);
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  const tab = async (t: any) => (await api(t.token).get<any>(`/v1/users/@${a.handle}/posts?filter=posts`)).body.items.map((i: any) => i.post.id);
  expect(await tab(v)).toEqual([]);
  expect(await tab(a)).toEqual([cq.id, pp.id, bp.id]);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/timelines.test.ts`
Expected: FAIL — `/v1/timeline/following` 404.

- [ ] **Step 3: 구현**

- following 원천(SQL 한 문, `UNION ALL` 후 `ORDER BY item_id DESC LIMIT ?`): ① 글 — `p.author_id IN (?me ∪ SELECT followee_id FROM follows WHERE follower_id=?me AND status='active') AND p.reply_to_id IS NULL AND <postVisibilitySql>`, `item_id = p.id`; ② 리포스트 — `r.user_id IN (같은 집합)`, 원글이 `postVisibilitySql` 통과 + 리포스트한 사람과 보는 사람 사이 차단 없음 + 인용 원글이 있으면 그 작성자와도 차단 없음, 같은 `post_id` 는 `MAX(r.id)` 1개만, `item_id = r.id`. 커서 `k=i=item_id` 의 `item_id < ?`. 결과 행 → `TimelineRow` → `serializeTimelineItems`.
- discover: `idx_posts_top` 범위, 작성자 `is_private=0` 비정지, `postVisibilitySql`, 리포스트 없음.
- `users.ts` 덧붙임: `canViewUserContent` 로 404/403(프로필 주인 기준 — 주인의 **자기 글**만 이것으로 충분하다). `posts` = 그 사용자의 최상위 글(인용 포함) ∪ 그 사용자의 리포스트, `replies` = 답글만, `media` = `EXISTS (SELECT 1 FROM post_media pm JOIN uploads u ON u.id = pm.upload_id WHERE pm.post_id=p.id AND u.status <> 'removed')` 인 그 사용자의 글. **리포스트 갈래는 following ② 와 같은 필터를 건다**: 리포스트된 원글이 `postVisibilitySql(viewer, "p")` 를 통과(원글 작성자 정지·차단·비공개 비팔로워 제외) + 원글이 인용이면 그 인용된 글의 작성자와 보는 사람 사이에 양방향 차단 없음 — 아니면 그 리포스트 항목은 빠진다(위 `profile posts tab hides reposts…` 테스트). 두 갈래를 `UNION ALL` 후 `item_id` 내림차순, 결과는 `serializeTimelineItems`.
- `lib/serialize.ts` 덧붙임: Interfaces 의 `serializeTimelineItems`(1부가 스텁만 두었으면 본문을 채운다).

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/timelines.test.ts test/api/users.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/routes/timelines.ts server/src/routes/users.ts server/src/lib/serialize.ts server/src/app.ts server/test/api/timelines.test.ts
git commit -m "feat(server): following/discover timelines with since/gap and profile post tabs"
```

---

### Task 8: 해시태그 피드 + 트렌드 Top 10 (Cron)

**Files:**
- Create: `server/src/cron/index.ts` — `handleScheduled` (1부는 `cron/` 을 만들지 않는다; 1부 소유 경계 문단)
- Create: `server/src/cron/trends.ts`
- Modify: `server/src/index.ts` (1부 파일, 계약 5장 형태로 덧붙이기) — `import { handleScheduled } from "./cron";` 한 줄과 default export 객체의 `scheduled: handleScheduled,` 한 줄. 1부가 남긴 `fetch`·DO re-export 는 그대로.
- Modify(덧붙이기): `server/src/lib/text.ts` — `normalizeTag` (해시태그 피드 경로 값 정규화; 1부 `lib/text.ts` 에 같은 규칙의 export 가 이미 있으면 새로 만들지 않고 그것을 쓴다)
- Modify: `server/src/routes/timelines.ts` (`GET /hashtags/:tag/posts`, `GET /trends`)
- Modify(빠져 있을 때만): `server/wrangler.jsonc` — `"triggers": { "crons": ["*/5 * * * *", "17 * * * *"] }`(계약 6장; 1부가 6장 블록을 그대로 넣었으면 손대지 않음)
- Test: `server/test/cron/trends.test.ts`, `server/test/api/hashtags.test.ts`

**Interfaces:**
- Consumes: `createScheduledController`, `createExecutionContext`(cloudflare:test), `extractHashtags`(정규화 규칙의 단일 출처), `normalizeText`, `postVisibilitySql`, `serializePosts`.
- Produces:
  - `src/cron/index.ts`: `export async function handleScheduled(controller: ScheduledController, env: Env, ctx: ExecutionContext): Promise<void>` (계약 5장) — `switch (controller.cron)`: `"*/5 * * * *"` → `await recomputeTrends(env)`; `"17 * * * *"` → `await runCleanup(env)` — **이 태스크에서는 아직 `cleanup.ts` 가 없으므로 이 갈래는 빈 스텁**(`// runCleanup: Task 10` 주석 + 아무것도 안 함), Task 10 이 import 와 호출로 바꾼다; 그 밖의 문자열 → `console.warn("unknown cron", controller.cron)`. 오류는 삼키지 않는다(cron 실패가 대시보드에 남도록).
  - `src/index.ts`: `export default { fetch: app.fetch, scheduled: handleScheduled } satisfies ExportedHandler<Env>` (계약 5장 그대로). 이것이 없으면 `wrangler dev --test-scheduled` 의 `/__scheduled`(Task 18 스모크 12단계)와 배포 후 Cron 2개가 아무것도 실행하지 않는다.
  - `normalizeTag(raw: string): string | null` (lib/text.ts) — 앞의 `#` 하나 제거 → `normalizeText`(NFC+trim) → `toLowerCase()` → `^[\p{L}\p{N}_]{1,50}$`(u) 이면 그 값, 아니면 `null`. `extractHashtags` 가 저장하는 태그와 같은 규칙.
  - `recomputeTrends(env: Env): Promise<void>`; `GET /trends` → `{ items: [{rank, tag, post_count, author_count}], computed_at: Timestamp | null }`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/hashtags.test.ts
import { api, signup, createPost } from "../helpers";
test("NFD and uppercase tags normalize to one feed", async () => {
  const a = await signup(); const v = await signup();
  const sfx = Math.random().toString(36).slice(2, 7);
  const p1 = await createPost(a, `#첫눈${sfx}`);
  const p2 = await createPost(a, `#${`첫눈${sfx}`.normalize("NFD")}`);
  const p3 = await createPost(a, `#GoLang${sfx} 그리고 #첫눈${sfx}이`);
  const feed = async (t: string) => (await api(v.token).get<any>(`/v1/hashtags/${encodeURIComponent(t)}/posts`)).body.items.map((p: any) => p.id);
  expect(await feed(`첫눈${sfx}`)).toEqual([p2.id, p1.id]);
  expect(await feed(`#첫눈${sfx}`.normalize("NFD"))).toEqual([p2.id, p1.id]);
  expect(await feed(`golang${sfx.toUpperCase()}`)).toEqual([p3.id]);
  expect((await api(v.token).get(`/v1/hashtags/${encodeURIComponent("a b")}/posts`)).status).toBe(400);
});
test("hashtag feed excludes private accounts' posts even for followers", async () => {
  const p = await signup(); const tag = `t${Math.random().toString(36).slice(2, 8)}`;
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await createPost(p, `#${tag}`);
  expect((await api(p.token).get<any>(`/v1/hashtags/${tag}/posts`)).body.items).toEqual([]);
});

// test/cron/trends.test.ts
import { env, createScheduledController, createExecutionContext } from "cloudflare:test";
import { handleScheduled } from "../../src/cron";
import { api, signup, createPost } from "../helpers";
test("trends ranks by distinct authors, excludes private/deleted, Top 10 replaced every run", async () => {
  const tag = `트렌드${Math.random().toString(36).slice(2, 7)}`;
  const authors = await Promise.all([0, 1, 2, 3, 4, 5, 6, 7].map(() => signup()));
  for (const a of authors) await createPost(a, `#${tag}`);
  await createPost(authors[0], `#${tag} again`);
  await api(authors[7].token).patch("/v1/me/settings", { is_private: true });
  const del = await createPost(authors[6], `#${tag} del`);
  await api(authors[6].token).del(`/v1/posts/${del.id}`);
  await handleScheduled(createScheduledController({ cron: "*/5 * * * *", scheduledTime: Date.now() }), env, createExecutionContext());
  const r = (await api(authors[0].token).get<any>("/v1/trends")).body;
  expect(r.items.length).toBeLessThanOrEqual(10);
  expect(r.items[0]).toEqual({ rank: 1, tag, post_count: 8, author_count: 7 });
  expect(typeof r.computed_at).toBe("string");
  const n = await env.DB.prepare("SELECT COUNT(*) AS n FROM trends").first<{ n: number }>();
  expect(n!.n).toBe(r.items.length);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/hashtags.test.ts test/cron/trends.test.ts`
Expected: FAIL — `/v1/hashtags/x/posts` 404, `../../src/cron` 모듈 없음(`handleScheduled` import 실패).

- [ ] **Step 3: 구현**

- `cron/index.ts` 생성(Interfaces 그대로, cleanup 갈래는 스텁), `cron/trends.ts` 생성, `src/index.ts` 에 `scheduled: handleScheduled` 연결. `wrangler.jsonc` 의 `triggers.crons`(`*/5 * * * *`, `17 * * * *`)는 1부가 계약 6장대로 이미 넣었다 — 없으면 그 한 줄을 덧붙인다.
- 태그 정규화: `lib/text.ts` 에 `normalizeTag` 를 덧붙이고, 경로 값이 `null` 로 정규화되면 `err.invalid({tag:"invalid"})`.
- 해시태그 피드: `post_hashtags h JOIN posts p` on `h.tag = ?`, `u.is_private = 0`, `postVisibilitySql`, `p.id` 내림차순 커서 → `serializePosts`.
- `recomputeTrends` — 한 batch(`DELETE FROM trends` + 최대 10개 INSERT):

```sql
SELECT h.tag, COUNT(*) AS post_count, COUNT(DISTINCT h.author_id) AS author_count
FROM post_hashtags h JOIN posts p ON p.id = h.post_id JOIN users u ON u.id = h.author_id
WHERE h.created_at >= ?1 AND p.deleted_at IS NULL AND u.is_private = 0
  AND (u.suspended_at IS NULL OR (u.suspended_until IS NOT NULL AND u.suspended_until < ?2))
GROUP BY h.tag ORDER BY author_count DESC, post_count DESC, h.tag ASC LIMIT 10
-- ?1 = now - 86_400_000, ?2 = now
```
- `GET /trends`: `SELECT * FROM trends ORDER BY rank`, `computed_at = isoOrNull(MAX(computed_at))`.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/hashtags.test.ts test/cron/trends.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/cron/index.ts server/src/cron/trends.ts server/src/index.ts server/src/lib/text.ts server/wrangler.jsonc server/src/routes/timelines.ts server/test/api/hashtags.test.ts server/test/cron/trends.test.ts
git commit -m "feat(server): hashtag feed and 5-minute trends top10 cron"
```

---

### Task 9: 스토리

**Files:**
- Create: `server/src/routes/stories.ts` (`POST /stories`, `GET /stories/feed`, `GET /users/:user/stories`, `POST /stories/:story_id/view`, `GET /stories/:story_id/viewers`, `DELETE /stories/:story_id`)
- Modify(덧붙이기): `server/src/lib/serialize.ts` — `serializeStories` (계약 4.9)
- Modify: `server/src/app.ts` (`storyRoutes`, `timelineRoutes` 다음)
- Test: `server/test/api/stories.test.ts`

**Interfaces:**
- Consumes: `canViewUserContent`, `loadUserSummaries`, `serializeMedia`, `removeUploads`, `publishEvent`, `limit("RL_POST")`, `uploadPng`.
- Produces:
  - `serializeStories(db: D1Database, viewerId: string, rows: StoryRow[]): Promise<Story[]>` (계약 4.9) — 입력 순서 유지. 일괄(각 1회): 작성자 `loadUserSummaries`, 미디어 `SELECT * FROM uploads WHERE id IN (media_upload_id 들) AND status <> 'removed'` → `serializeMedia`(없거나 removed 면 `media:null`), 본 기록 `SELECT story_id FROM story_views WHERE viewer_id=?1 AND story_id IN (작성자가 viewer 가 아닌 스토리들)`, 조회 수 `SELECT story_id, COUNT(*) AS n FROM story_views WHERE story_id IN (작성자가 viewer 인 스토리들) GROUP BY story_id`. 필드: `seen` = 작성자 본인 스토리면 **항상 `true`**, 아니면 `story_views` 행 존재; `view_count` = 작성자 본인에게만 숫자(행 없으면 `0`), 그 외 `null`; `bg`, `body` 그대로; `created_at`/`expires_at` = `iso(...)`. 가시성(만료·삭제·차단·비공개·정지)은 호출 쪽 `visibleStorySql` 이 거른다.
  - `storyRoutes`; 이벤트 `story.created {user_id, story_id, expires_at}`, `story.deleted {user_id, story_id}` (청중 `{followers: author}` + `includeSelf`, 휘발). 모듈 비공개 `visibleStorySql` = `s.deleted_at IS NULL AND s.expires_at > ?now` + 작성자 비정지 + 차단 없음 + (공개 또는 본인 또는 active 팔로워).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/stories.test.ts
import { env } from "cloudflare:test";
import { api, signup, follow, uploadPng, connectWs } from "../helpers";

test("create text+media story, 24h expiry, story.created to followers and self", async () => {
  const a = await signup(); const f = await signup();
  await follow(f, a);
  const wf = await connectWs(f.token);
  const m = await uploadPng(a, "story");
  const r = await api(a.token).post<any>("/v1/stories", { body: "점심 🍜", media_upload_id: m.id, bg: "accent3" });
  expect(r.status).toBe(201);
  const s = r.body.story;
  expect(Date.parse(s.expires_at) - Date.parse(s.created_at)).toBe(86_400_000);
  expect(s).toMatchObject({ bg: "accent3", seen: true, view_count: 0, media: { id: m.id }, author: { id: a.user.id } }); // author's own story is always seen
  expect((await wf.next("story.created")).data).toEqual({ user_id: a.user.id, story_id: s.id, expires_at: s.expires_at });
  expect((await api(f.token).raw(`/v1/media/${m.id}`, { method: "GET" })).status).toBe(200);
});
test("validation: empty story 400, post-purpose media 400, body 201 chars too_long", async () => {
  const a = await signup();
  expect((await api(a.token).post("/v1/stories", {})).status).toBe(400);
  const pm = await uploadPng(a, "post");
  expect((await api(a.token).post("/v1/stories", { media_upload_id: pm.id })).status).toBe(400);
  expect((await api(a.token).post<any>("/v1/stories", { body: "가".repeat(201) })).body.error.details.fields.body).toBe("too_long");
});
test("31st active story -> 422 active_stories", async () => {
  const a = await signup();
  await env.DB.prepare(`INSERT INTO stories (id, author_id, body, created_at, expires_at) SELECT 'S' || value || ?1, ?1, 'x', ?2, ?3 FROM json_each(?4)`)
    .bind(a.user.id, Date.now(), Date.now() + 86_400_000, JSON.stringify([...Array(30).keys()].map(String))).run();
  const r = await api(a.token).post<any>("/v1/stories", { body: "one more" });
  expect(r.status).toBe(422); expect(r.body.error.details).toEqual({ what: "active_stories", limit: 30 });
});
test("feed ordering: me first, then unseen groups by latest_at desc, then seen; stories oldest first", async () => {
  const me = await signup(); const a = await signup(); const b = await signup(); const c = await signup();
  for (const x of [a, b, c]) await follow(me, x);
  const sa = (await api(a.token).post<any>("/v1/stories", { body: "a1" })).body.story;
  await api(a.token).post("/v1/stories", { body: "a2" });
  await api(b.token).post("/v1/stories", { body: "b1" });
  const sc = (await api(c.token).post<any>("/v1/stories", { body: "c1" })).body.story;
  await api(me.token).post("/v1/stories", { body: "me" });
  await api(me.token).post(`/v1/stories/${sc.id}/view`); await api(me.token).post(`/v1/stories/${sc.id}/view`);
  const g = (await api(me.token).get<any>("/v1/stories/feed")).body.items;
  expect(g.map((x: any) => [x.user.id, x.has_unseen])).toEqual([[me.user.id, false], [b.user.id, true], [a.user.id, true], [c.user.id, false]]);
  expect(g[2].stories.map((s: any) => s.body)).toEqual(["a1", "a2"]);
  expect((await api(c.token).get<any>(`/v1/stories/${sc.id}/viewers`)).body.items.map((v: any) => v.user.id)).toEqual([me.user.id]);
  expect((await api(me.token).get(`/v1/stories/${sc.id}/viewers`)).status).toBe(403);
  expect(sa.view_count).toBe(0);
  expect(g.find((x: any) => x.user.id === c.user.id).stories[0]).toMatchObject({ id: sc.id, seen: true, view_count: null });
  expect(g[2].stories[0]).toMatchObject({ seen: false, view_count: null });
});
test("expired story hidden before cleanup (feed, user stories, media, view)", async () => {
  const a = await signup(); const v = await signup();
  await follow(v, a);
  const m = await uploadPng(a, "story");
  const s = (await api(a.token).post<any>("/v1/stories", { media_upload_id: m.id })).body.story;
  await env.DB.prepare("UPDATE stories SET expires_at = ? WHERE id = ?").bind(Date.now() - 1000, s.id).run();
  expect((await api(v.token).get<any>("/v1/stories/feed")).body.items).toEqual([]);
  expect((await api(v.token).get<any>(`/v1/users/@${a.handle}/stories`)).body.items).toEqual([]);
  expect((await api(v.token).raw(`/v1/media/${m.id}`, { method: "GET" })).status).toBe(404);
  expect((await api(v.token).post(`/v1/stories/${s.id}/view`)).status).toBe(404);
});
test("private author's stories need active follow; delete by author emits story.deleted and removes media", async () => {
  const p = await signup(); const x = await signup(); const f = await signup();
  await api(p.token).patch("/v1/me/settings", { is_private: true });
  await follow(f, p); await api(p.token).post(`/v1/me/follow-requests/@${f.handle}/accept`);
  const m = await uploadPng(p, "story");
  const s = (await api(p.token).post<any>("/v1/stories", { media_upload_id: m.id })).body.story;
  expect((await api(x.token).get<any>(`/v1/users/@${p.handle}/stories`)).body.error.code).toBe("private_account");
  const wf = await connectWs(f.token);
  expect((await api(x.token).del(`/v1/stories/${s.id}`)).status).toBe(404);
  expect((await api(p.token).del(`/v1/stories/${s.id}`)).status).toBe(204);
  expect((await wf.next("story.deleted")).data).toEqual({ user_id: p.user.id, story_id: s.id });
  expect(await env.MEDIA.head(`stories/${p.user.id}/${m.id}.png`)).toBeNull();
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/stories.test.ts`
Expected: FAIL — `/v1/stories` 404.

- [ ] **Step 3: 구현**

- `POST /stories` (`RL_POST`): zod `{ body?: zText(0,200), media_upload_id?: zId, bg?: enum }`; 둘 다 비면 `fields.body="required"`; 미디어는 내 `purpose:"story"`·`pending` 아니면 `fields.media_upload_id="invalid"`; 활성(`deleted_at IS NULL AND expires_at > now`) 30개 이상 → `err.limit("active_stories",30)`. batch: `stories` INSERT(`expires_at = now + 86_400_000`) + `uploads attached 'story:<id>'`. 커밋 뒤 `story.created`.
- `GET /stories/feed`: 나 + active 팔로우 대상 중 `visibleStorySql` 통과 스토리 한 번에 읽어(+ `story_views` 로 seen) 그룹핑. 정렬: 나 → `has_unseen` 그룹(`latest_at` desc) → 나머지(`latest_at` desc), 최대 100그룹, 그룹 안은 `created_at` 오름차순.
- `GET /users/:user/stories`: `canViewUserContent` → 404/403, 그다음 `visibleStorySql`.
- `seen` 규칙(`serializeStories` 가 구현): 작성자 본인 스토리는 항상 `seen=true`(그래서 `POST /stories` 응답도 `seen:true`, 내 그룹은 `has_unseen=false`), 그 외는 `story_views` 행 존재. `view_count` 는 작성자에게만 숫자. `has_unseen` = 그룹 안에 `seen:false` 가 하나라도 있음, `latest_at` = 그룹 안 최신 `created_at`.
- `lib/serialize.ts` 덧붙임: Interfaces 의 `serializeStories`(1부가 스텁만 두었으면 본문을 채운다).
- `view`: 보이지 않으면 404; 작성자 본인은 기록하지 않음; `INSERT OR IGNORE story_views`; 204.
- `viewers`: 못 보면 404, 작성자가 아니면 `err.forbidden()`. `viewed_at` desc 커서.
- `DELETE`: 못 보면 404, 작성자가 아니면 403. `deleted_at=now` → `removeUploads` → `story.deleted`.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/stories.test.ts test/api/uploads.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/routes/stories.ts server/src/lib/serialize.ts server/src/app.ts server/test/api/stories.test.ts
git commit -m "feat(server): 24h stories with feed rings, views and author-only viewers"
```

---

### Task 10: 정리 Cron (스토리 만료 외 9개 작업)

**Files:**
- Create: `server/src/cron/cleanup.ts`
- Modify: `server/src/cron/index.ts` (Task 8 이 만든 파일) — `"17 * * * *"` 갈래의 스텁을 `import { runCleanup } from "./cleanup"` + `await runCleanup(env)` 로 바꾼다
- Test: `server/test/cron/cleanup.test.ts`

**Interfaces:**
- Consumes: Task 8 `handleScheduled`(cron/index.ts), `removeUploads(env, ids)`, `jsonList`, `batch`.
- Produces: `runCleanup(env: Env): Promise<void>` — `data-model.md` 6장 1~9 순서, 작업마다 최대 500행.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/cron/cleanup.test.ts
import { env, createScheduledController, createExecutionContext } from "cloudflare:test";
import { handleScheduled } from "../../src/cron";
import { api, signup, uploadPng } from "../helpers";
const run = () => handleScheduled(createScheduledController({ cron: "17 * * * *", scheduledTime: Date.now() }), env, createExecutionContext());
const H = 3_600_000, D = 86_400_000;

test("expired story (>1h past) deleted with views and R2 media; recently expired kept", async () => {
  const a = await signup(); const v = await signup();
  const m = await uploadPng(a, "story");
  const old = (await api(a.token).post<any>("/v1/stories", { media_upload_id: m.id })).body.story;
  const recent = (await api(a.token).post<any>("/v1/stories", { body: "r" })).body.story;
  await api(v.token).post(`/v1/stories/${old.id}/view`);
  await env.DB.batch([
    env.DB.prepare("UPDATE stories SET expires_at=? WHERE id=?").bind(Date.now() - H - 1000, old.id),
    env.DB.prepare("UPDATE stories SET expires_at=? WHERE id=?").bind(Date.now() - 1000, recent.id)]);
  await run();
  expect(await env.DB.prepare("SELECT 1 FROM stories WHERE id=?").bind(old.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM story_views WHERE story_id=?").bind(old.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM stories WHERE id=?").bind(recent.id).first()).not.toBeNull();
  expect((await env.DB.prepare("SELECT status FROM uploads WHERE id=?").bind(m.id).first<any>()).status).toBe("removed");
  expect(await env.MEDIA.head(`stories/${a.user.id}/${m.id}.png`)).toBeNull();
});
test("orphan pending upload >24h removed; status message and suspension expiry cleared; stale presence offline", async () => {
  const a = await signup();
  const up = await uploadPng(a, "post");
  const now = Date.now();
  await env.DB.batch([
    env.DB.prepare("UPDATE uploads SET created_at=? WHERE id=?").bind(now - D - 1000, up.id),
    env.DB.prepare("UPDATE users SET status_text='x', status_expires_at=?, suspended_at=?, suspended_until=?, suspend_reason='r' WHERE id=?").bind(now - 1, now - D, now - 1, a.user.id),
    env.DB.prepare("UPDATE user_presence SET status='online', online_until=? WHERE user_id=?").bind(now - 1, a.user.id)]);
  await run();
  expect((await env.DB.prepare("SELECT status FROM uploads WHERE id=?").bind(up.id).first<any>()).status).toBe("removed");
  expect(await env.DB.prepare("SELECT status_text, status_expires_at, suspended_at, suspended_until, suspend_reason FROM users WHERE id=?").bind(a.user.id).first())
    .toEqual({ status_text: null, status_expires_at: null, suspended_at: null, suspended_until: null, suspend_reason: null });
  expect((await env.DB.prepare("SELECT status FROM user_presence WHERE user_id=?").bind(a.user.id).first<any>()).status).toBe("offline");
});
test("old sessions, notifications, responded requests, dead invites deleted; pending requests kept", async () => {
  const a = await signup(); const b = await signup(); const c = await signup();
  await api(a.token).post("/v1/friends/requests", { user: `@${b.handle}` });
  const kept = await api(c.token).post<any>("/v1/friends/requests", { user: `@${b.handle}` });
  const now = Date.now(), M = 30 * D;
  await env.DB.batch([
    env.DB.prepare("UPDATE friend_requests SET status='declined', responded_at=? WHERE from_user_id=?").bind(now - M - 1000, a.user.id),
    env.DB.prepare("UPDATE friend_requests SET created_at=? WHERE id=?").bind(now - 2 * M, kept.body.request.id),
    env.DB.prepare("UPDATE notifications SET created_at=? WHERE user_id=?").bind(now - 90 * D - 1000, b.user.id),
    env.DB.prepare("UPDATE sessions SET revoked_at=?, revoke_reason='logout' WHERE user_id=?").bind(now - M - 1000, c.user.id)]);
  await run();
  expect(await env.DB.prepare("SELECT 1 FROM friend_requests WHERE from_user_id=?").bind(a.user.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM friend_requests WHERE id=?").bind(kept.body.request.id).first()).not.toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM notifications WHERE user_id=?").bind(b.user.id).first()).toBeNull();
  expect(await env.DB.prepare("SELECT 1 FROM sessions WHERE user_id=?").bind(c.user.id).first()).toBeNull();
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/cron/cleanup.test.ts`
Expected: FAIL — 행이 남아 있음(Task 8 의 cleanup 갈래가 아직 스텁).

- [ ] **Step 3: 구현**

`runCleanup` — 각 작업은 `SELECT ... LIMIT 500` 로 대상 ID 를 고른 뒤 `jsonList` 한 파라미터로 처리:
1. `stories WHERE expires_at < now - 3_600_000` → 미디어 ID 모아 `removeUploads` → batch(`story_views` DELETE, `stories` DELETE).
2. `uploads WHERE status='pending' AND created_at < now - 86_400_000` → `removeUploads`.
3. `sessions WHERE expires_at < now - 30일 OR revoked_at < now - 30일` DELETE.
4. `notifications WHERE created_at < now - 90일` DELETE.
5. `friend_requests`, `space_direct_invites` `WHERE status <> 'pending' AND responded_at < now - 30일` DELETE.
6. `space_invites WHERE revoked_at < now - 30일 OR expires_at < now - 30일` DELETE.
7. `users SET status_emoji=NULL, status_text=NULL, status_expires_at=NULL WHERE status_expires_at < now`.
8. `users SET suspended_at=NULL, suspended_until=NULL, suspend_reason=NULL WHERE suspended_until < now`.
9. `user_presence SET status='offline', updated_at=now WHERE status <> 'offline' AND online_until < now`.
작업 하나가 실패해도 `console.error` 후 다음 작업을 계속한다.
`cron/index.ts` 의 `"17 * * * *"` 스텁을 `await runCleanup(env)` 로 교체한다.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/cron/cleanup.test.ts test/cron/trends.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/cron/cleanup.ts server/src/cron/index.ts server/test/cron/cleanup.test.ts
git commit -m "feat(server): hourly cleanup cron incl. story expiry and orphan uploads"
```

---

### Task 11: 투표 (채팅 + 피드)

**Files:**
- Modify(덧붙이기): `server/src/services/polls.ts` (`vote`, `closePoll`; `pollInsertStmts` 는 1부 것 또는 Task 4 것)
- Create: `server/src/routes/polls.ts` (`GET /polls/:poll_id`, `PUT|DELETE /polls/:poll_id/vote`, `POST /polls/:poll_id/close`)
- Modify: `server/src/app.ts` (`pollRoutes`, `notificationRoutes` 다음)
- Test: `server/test/api/polls.test.ts`

**Interfaces:**
- Consumes: `serializePolls(db, viewerId, pollIds): Promise<Map<string, Poll>>`, `requireChannelRead`, `canViewPost`, `publishEvent`, `limit("RL_REACT")`.
- Produces:
  - `pollInsertStmts(db: D1Database, pollId: string, creatorId: string, ctx: { type: "message" | "post"; id: string }, input: PollInput, nowMs: number): D1PreparedStatement[]`
  - `vote(rt: Runtime, pollId: string, userId: string, optionIdxs: number[] | null): Promise<Poll>` (null = 취소)
  - `closePoll(rt: Runtime, pollId: string, userId: string): Promise<Poll>`
  - 이벤트 `poll.updated {poll}`(`my_votes: []`): 메시지 투표 → `{channel}` 기록, 글 투표 → `{topic:"post:<id>"}` 휘발

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/polls.test.ts
import { env } from "cloudflare:test";
import { api, signup, createPost, connectWs, makeSpace, watch } from "../helpers";

test("chat poll: vote replaces, poll.updated to channel with empty my_votes, non-member 404", async () => {
  const a = await signup(); const b = await signup(); const x = await signup();
  const { generalChannelId } = await makeSpace(a, [b]);
  const m = (await api(a.token).post<any>(`/v1/channels/${generalChannelId}/messages`,
    { client_nonce: "p1", kind: "poll", body: "", poll: { question: "점심?", options: ["국밥", "라멘", "샐러드"] } })).body.message;
  const pid = m.poll.id;
  const wa = await connectWs(a.token);
  const r = await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [1] });
  expect(r.body.poll).toMatchObject({ voter_count: 1, my_votes: [1], closed: false });
  expect(r.body.poll.options.map((o: any) => o.vote_count)).toEqual([0, 1, 0]);
  const ev = await wa.next("poll.updated");
  expect(ev.seq).not.toBeNull(); expect(ev.data.poll.my_votes).toEqual([]);
  const r2 = await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [2] });
  expect(r2.body.poll.options.map((o: any) => o.vote_count)).toEqual([0, 0, 1]);
  expect((await api(x.token).get(`/v1/polls/${pid}`)).status).toBe(404);
});
test("single needs exactly 1, multi 1..n distinct in range; retract; close by creator only; closed -> 409", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "", { poll: { question: "고르기", options: ["a", "b", "c"], multi: true } });
  const pid = p.poll!.id;
  for (const bad of [[], [0, 0], [3], [-1]]) expect((await api(b.token).put(`/v1/polls/${pid}/vote`, { option_idxs: bad })).status).toBe(400);
  expect((await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [0, 2] })).body.poll.my_votes).toEqual([0, 2]);
  expect((await api(b.token).del<any>(`/v1/polls/${pid}/vote`)).body.poll).toMatchObject({ voter_count: 0, my_votes: [] });
  expect((await api(b.token).post(`/v1/polls/${pid}/close`)).status).toBe(403);
  expect((await api(a.token).post<any>(`/v1/polls/${pid}/close`)).body.poll.closed).toBe(true);
  expect((await api(b.token).put<any>(`/v1/polls/${pid}/vote`, { option_idxs: [0] })).body.error.code).toBe("poll_closed");
  const s = await createPost(a, "", { poll: { question: "단일", options: ["x", "y"] } });
  expect((await api(b.token).put(`/v1/polls/${s.poll!.id}/vote`, { option_idxs: [0, 1] })).status).toBe(400);
});
test("closes_at in the past counts as closed; post poll.updated on topic", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "", { poll: { question: "q", options: ["x", "y"], duration_sec: 60 } });
  const w = await connectWs(a.token); await watch(w, [`post:${p.id}`]);
  await api(b.token).put(`/v1/polls/${p.poll!.id}/vote`, { option_idxs: [0] });
  expect((await w.next("poll.updated")).seq).toBeNull();
  await env.DB.prepare("UPDATE polls SET closes_at=? WHERE id=?").bind(Date.now() - 1, p.poll!.id).run();
  expect((await api(b.token).get<any>(`/v1/polls/${p.poll!.id}`)).body.poll.closed).toBe(true);
  expect((await api(b.token).put(`/v1/polls/${p.poll!.id}/vote`, { option_idxs: [1] })).status).toBe(409);
});
test("concurrent double vote keeps voter_count 1", async () => {
  const a = await signup(); const b = await signup();
  const p = await createPost(a, "", { poll: { question: "q", options: ["x", "y"] } });
  await Promise.all([api(b.token).put(`/v1/polls/${p.poll!.id}/vote`, { option_idxs: [0] }), api(b.token).put(`/v1/polls/${p.poll!.id}/vote`, { option_idxs: [1] })]);
  const poll = (await api(a.token).get<any>(`/v1/polls/${p.poll!.id}`)).body.poll;
  expect(poll.voter_count).toBe(1);
  expect(poll.options.reduce((s: number, o: any) => s + o.vote_count, 0)).toBe(1);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/polls.test.ts`
Expected: FAIL — `/v1/polls/...` 404.

- [ ] **Step 3: 구현**

- 권한: `context_type='message'` → 메시지 행의 채널로 `requireChannelRead`(실패 404), `'post'` → 글이 `canViewPost`(실패 404). 닫힘 = `closed_at IS NOT NULL OR (closes_at IS NOT NULL AND closes_at <= now)` → `err.conflict("poll_closed")`.
- 입력 검증: 단일이면 길이 정확히 1, 복수면 1..옵션 수, 중복 없음, `0 <= idx < 옵션 수` — 위반 `fields.option_idxs="invalid"`.
- `vote` 한 batch(동시 요청에도 일관):

```sql
UPDATE poll_options SET vote_count = MAX(vote_count - 1, 0) WHERE poll_id = ?1 AND idx IN (SELECT option_idx FROM poll_votes WHERE poll_id = ?1 AND user_id = ?2);
UPDATE polls SET voter_count = MAX(voter_count - 1, 0) WHERE id = ?1 AND EXISTS (SELECT 1 FROM poll_votes WHERE poll_id = ?1 AND user_id = ?2);
DELETE FROM poll_votes WHERE poll_id = ?1 AND user_id = ?2;
-- when optionIdxs is not null:
INSERT INTO poll_votes (poll_id, user_id, option_idx, created_at) SELECT ?1, ?2, value, ?3 FROM json_each(?4);
UPDATE poll_options SET vote_count = vote_count + 1 WHERE poll_id = ?1 AND idx IN (SELECT value FROM json_each(?4));
UPDATE polls SET voter_count = voter_count + 1 WHERE id = ?1;
```
- `closePoll`: 만든 사람 아니면 `err.forbidden()`; 이미 닫혔으면 그대로 반환; `closed_at=now`. 셋 다 커밋 뒤 `serializePolls(db, null, [id])` 로 이벤트, 응답은 `serializePolls(db, userId, [id])`.
- `pollInsertStmts` 가 없다면: `duration_sec` 60..604800 → `closes_at = now + sec*1000`, `polls` → `poll_options`(idx 0..) 순서.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/polls.test.ts test/api/messages.test.ts test/api/posts.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/services/polls.ts server/src/routes/polls.ts server/src/app.ts server/test/api/polls.test.ts
git commit -m "feat(server): poll voting and closing for chat and feed polls"
```

---

### Task 12: 방명록

**Files:**
- Create: `server/src/routes/guestbook.ts` (`GET|POST /users/:user/guestbook`, `DELETE /guestbook/:entry_id`)
- Modify(덧붙이기): `server/src/lib/serialize.ts` — `serializeGuestbook` (계약 4.9)
- Modify: `server/src/app.ts` (`guestbookRoutes`, `pollRoutes` 다음)
- Test: `server/test/api/guestbook.test.ts`

**Interfaces:**
- Consumes: `resolveUserRef`, `canViewUserContent`, `areFriends`, `isBlockedEither`, `loadUserSummaries`, `notify`, `limit("RL_POST")`.
- Produces:
  - `serializeGuestbook(db: D1Database, viewerId: string, rows: GuestbookRow[]): Promise<GuestbookEntry[]>` (계약 4.9) — 입력 순서 유지, 작성자는 `loadUserSummaries` 1회(가려지는 항목의 작성자는 질의 대상에서 빼도 됨). 필드: `hidden = is_secret === 1 && viewerId !== owner_id && viewerId !== author_id`; `deleted = deleted_at !== null`; `author` = `hidden` 이면 `null`, 아니면 작성자 `UserSummary`; `body` = `hidden || deleted` 이면 `""`, 아니면 본문; `is_secret` 불리언(가려져도 `true` 로 보인다 — "비밀글" 자리 표시용); `owner_id`, `id`, `created_at = iso(...)`. 목록 필터(삭제·차단·정지)는 호출 쪽이 한다.
  - `guestbookRoutes`; `201 { entry: GuestbookEntry }`.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/guestbook.test.ts
import { api, signup, follow, makeFriends } from "../helpers";
const gb = async (viewer: any, owner: any) => (await api(viewer.token).get<any>(`/v1/users/@${owner.handle}/guestbook`)).body;

test("write, notify owner, secret entry hidden from third party", async () => {
  const o = await signup(); const a = await signup(); const x = await signup();
  const e1 = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "놀러왔다 감 👋" })).body.entry;
  const e2 = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "비밀", is_secret: true })).body.entry;
  expect((await api(o.token).get<any>("/v1/notifications")).body.items[0]).toMatchObject({ type: "guestbook_entry", subject: { type: "guestbook_entry", id: e2.id } });
  const third = (await gb(x, o)).items;
  expect(third.map((e: any) => [e.id, e.hidden, e.body, e.author])).toEqual([[e2.id, true, "", null], [e1.id, false, "놀러왔다 감 👋", expect.objectContaining({ id: a.user.id })]]);
  expect((await gb(o, o)).items[0]).toMatchObject({ id: e2.id, hidden: false, body: "비밀" });
  expect((await gb(a, o)).items[0].body).toBe("비밀");
  await api(o.token).post(`/v1/users/@me/guestbook`, { body: "내 방명록" });
  expect((await api(o.token).get<any>("/v1/notifications")).body.items.filter((n: any) => n.type === "guestbook_entry").length).toBe(2);
});
test("blocked -> 403 action_not_allowed; private owner: friend or active follower only", async () => {
  const o = await signup(); const a = await signup(); const f = await signup(); const fr = await signup();
  await api(o.token).put(`/v1/blocks/@${a.handle}`);
  expect((await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).body.error.code).toBe("action_not_allowed");
  await api(o.token).patch("/v1/me/settings", { is_private: true });
  const s = await signup();
  expect((await api(s.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).body.error.code).toBe("private_account");
  await follow(f, o); await api(o.token).post(`/v1/me/follow-requests/@${f.handle}/accept`);
  expect((await api(f.token).post(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).status).toBe(201);
  await makeFriends(fr, o);
  expect((await api(fr.token).post(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).status).toBe(201);
  expect((await api(s.token).get<any>(`/v1/users/@${o.handle}/guestbook`)).body.error.code).toBe("private_account");
});
test("delete by author or owner only; body length 1..300", async () => {
  const o = await signup(); const a = await signup(); const x = await signup();
  const e = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "hi" })).body.entry;
  expect((await api(x.token).del(`/v1/guestbook/${e.id}`)).status).toBe(403);
  expect((await api(o.token).del(`/v1/guestbook/${e.id}`)).status).toBe(204);
  expect((await gb(a, o)).items).toEqual([]);
  expect((await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "가".repeat(301) })).body.error.details.fields.body).toBe("too_long");
  expect((await api(a.token).post(`/v1/users/@${o.handle}/guestbook`, { body: "   " })).status).toBe(400);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/guestbook.test.ts`
Expected: FAIL — 404.

- [ ] **Step 3: 구현**

- `GET`: `canViewUserContent(me, owner)` → 404/403. 목록 = 삭제 안 됨 + 작성자 비정지 + 보는 사람↔작성자 차단 없음, ID 내림차순 커서 → `serializeGuestbook`(비밀글 가리기는 직렬화 규칙).
- `POST` (`RL_POST`, 라우트별 `requireAuth`): 주인 정지 또는 `isBlockedEither` → `action_not_allowed`; 주인 비공개면 작성자 ∈ {주인, active 팔로워, 친구} 아니면 `err.forbidden("private_account")`. INSERT → `notify` `guestbook_entry`(subject guestbook_entry, 행위자=작성자; 자기 글은 헬퍼가 생략, alert kind `social`, target `{type:"user", id: ownerId}`).
- `DELETE`: 작성자·주인 → `body='', deleted_at, deleted_by`; 볼 수 있는 제3자 → 403; 못 보면 404.
- `lib/serialize.ts` 덧붙임: Interfaces 의 `serializeGuestbook`(1부가 스텁만 두었으면 본문을 채운다).

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/guestbook.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/routes/guestbook.ts server/src/lib/serialize.ts server/src/app.ts server/test/api/guestbook.test.ts
git commit -m "feat(server): cyworld-style guestbook with secret entries"
```

---

### Task 13: 상태 메시지·집중 모드

**Files:**
- Modify(덧붙이기): `server/src/routes/me.ts` (`PUT /me/status`, `DELETE /me/status`)
- Modify(덧붙이기): `server/src/routes/presence.ts` (`POST|GET|DELETE /me/focus`)
- Modify(고정 기준에 어긋날 때만): `server/src/do/user-hub.ts` (`endFocus` 의 조기 종료 경로가 `alert{kind:"focus_summary"}` 를 내지 않으면 고친다 — 아래 Step 3)
- Test: `server/test/api/status-focus.test.ts`

**Interfaces:**
- Consumes: `UserHub.startFocus(userId, durationSec): Promise<Focus>`, `UserHub.endFocus(userId): Promise<FocusSummary | null>`, `UserHub.getPresenceState(userId): Promise<PresenceState>`(stub = `env.USER_HUB.getByName(userId)`), `toUserSummary`, `publishEvent(rt, {presenceWatchers: me}, "user.updated", {user}, {includeSelf: me})`.
- Produces: `200 { status: UserStatus }`, `204`; `200 { focus: Focus }`, `200 { focus: Focus | null }`, `200 { summary: FocusSummary }` / `409 conflict reason:"not_focusing"`.
- 고정 기준(집중 종료 이벤트): 조기 종료(`DELETE /me/focus`)와 자연 종료(알람) **모두** `focus.ended{summary}`(기록)와 `alert{kind:"focus_summary", title:"집중 모드 끝 — 멘션 N, DM M", suppressed:false}`(기록)를 1번씩 보낸다(events.md 4.7 alert kind 표 "집중 모드 종료 시 1회 · 항상", api.md 5.1 `DELETE /me/focus`). 클라이언트 요약 오버레이의 **유일한 트리거**가 이 alert 이므로(client-architecture 9.1 — `EndFocus` 응답·`focus.ended` 는 상태만 갱신) 조기 종료에서 alert 가 빠지면 요약이 뜨지 않는다. 1부 Task 12 Produces 의 "알람 종료일 때만 alert" 문구는 이 기준으로 대체된다.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/status-focus.test.ts
import { env } from "cloudflare:test";
import { api, signup, connectWs, makeFriends } from "../helpers";

test("status put/delete, user.updated to friend, expiry hides it", async () => {
  const a = await signup(); const f = await signup();
  await makeFriends(a, f);
  const wf = await connectWs(f.token);
  const r = await api(a.token).put<any>("/v1/me/status", { emoji: "🎧", text: "듣는 중…", expires_in_sec: 3600 });
  expect(r.body.status).toMatchObject({ emoji: "🎧", text: "듣는 중…" });
  expect(Date.parse(r.body.status.expires_at) - Date.now()).toBeGreaterThan(3_590_000);
  expect((await wf.next("user.updated")).data.user.status.text).toBe("듣는 중…");
  await env.DB.prepare("UPDATE users SET status_expires_at=? WHERE id=?").bind(Date.now() - 1, a.user.id).run();
  expect((await api(f.token).get<any>(`/v1/users/@${a.handle}`)).body.user.status).toBeNull();
  await api(a.token).put("/v1/me/status", { text: "🔨 배포 중" });
  expect((await api(a.token).del("/v1/me/status")).status).toBe(204);
  expect((await api(a.token).get<any>("/v1/me")).body.user.status).toBeNull();
});
test("status validation", async () => {
  const a = await signup();
  const bad = async (j: any) => (await api(a.token).put<any>("/v1/me/status", j)).body.error.details.fields;
  expect(await bad({ text: "가".repeat(65) })).toEqual({ text: "too_long" });
  expect(await bad({ emoji: "🎧🎧", text: "x" })).toEqual({ emoji: "invalid" });
  expect(await bad({ emoji: "a", text: "x" })).toEqual({ emoji: "invalid" });
  expect(await bad({ text: "x", expires_in_sec: 59 })).toEqual({ expires_in_sec: "invalid" });
  expect((await api(a.token).put("/v1/me/status", { emoji: "🇰🇷", text: "" })).status).toBe(200);
});
test("focus: default duration, suppressed alerts, early end summary, 409 when not focusing", async () => {
  const a = await signup(); const b = await signup();
  const r = await api(a.token).post<any>("/v1/me/focus", {});
  expect(Date.parse(r.body.focus.ends_at) - Date.parse(r.body.focus.started_at)).toBe(25 * 60_000);
  expect((await api(a.token).get<any>("/v1/me/focus")).body.focus).toEqual(r.body.focus);
  const w = await connectWs(a.token);
  await api(b.token).post("/v1/friends/requests", { user: `@${a.handle}` });
  expect((await w.next("alert")).data.alert).toMatchObject({ kind: "friend_request", suppressed: true, suppressed_reason: "focus" });
  const end = await api(a.token).del<any>("/v1/me/focus");
  expect(end.body.summary.counts.friend_request).toBe(1);
  expect(end.body.summary.items.length).toBe(1);
  expect((await w.next("focus.ended")).data.summary.counts.friend_request).toBe(1);
  // early end must also emit the focus_summary alert: it is the client's only trigger for the summary overlay (CA 9.1)
  const sa = await w.next("alert", (d: any) => d.alert.kind === "focus_summary");
  expect(sa.data.alert).toMatchObject({ kind: "focus_summary", suppressed: false });
  expect(sa.data.alert.title).toMatch(/^집중 모드 끝/);
  const again = await api(a.token).del<any>("/v1/me/focus");
  expect(again.status).toBe(409); expect(again.body.error.details.reason).toBe("not_focusing");
  expect((await api(a.token).get<any>("/v1/me/focus")).body.focus).toBeNull();
});
test("focus duration bounds and restart replaces ends_at", async () => {
  const a = await signup();
  expect((await api(a.token).post("/v1/me/focus", { duration_sec: 59 })).status).toBe(400);
  expect((await api(a.token).post("/v1/me/focus", { duration_sec: 43_201 })).status).toBe(400);
  const f1 = (await api(a.token).post<any>("/v1/me/focus", { duration_sec: 60 })).body.focus;
  const f2 = (await api(a.token).post<any>("/v1/me/focus", { duration_sec: 120 })).body.focus;
  expect(Date.parse(f2.ends_at)).toBeGreaterThan(Date.parse(f1.ends_at));
  expect((await api(a.token).get<any>("/v1/me")).body.user.presence.status).toBe("dnd");
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/status-focus.test.ts`
Expected: FAIL — `/v1/me/status` 또는 `/v1/me/focus` 404(1부가 이미 구현했다면 해당 테스트가 PASS 하고 Step 3 의 그 부분은 건너뜀). 1부 UserHub 가 "알람 종료일 때만 alert" 로 구현돼 있으면 `focus: ... early end summary` 테스트가 `w.next("alert", … focus_summary)` 시간 초과로 FAIL — Step 3 마지막 항목으로 고친다.

- [ ] **Step 3: 구현**

- `PUT /me/status`(라우트별 `requireAuth`): zod `{ emoji?: string|null, text: zText(0,64), expires_in_sec?: int 60..2592000|null }`. emoji 검사: `new Intl.Segmenter(undefined, { granularity: "grapheme" })` 로 정확히 1개 + `/\p{Extended_Pictographic}|\p{Regional_Indicator}/u` 일치 + UTF-8 ≤16바이트, 아니면 `fields.emoji="invalid"`. `UPDATE users SET status_emoji, status_text, status_expires_at, updated_at`. 커밋 뒤 `user.updated`. `DELETE` 는 세 컬럼 NULL + 같은 이벤트.
- `POST /me/focus`: `duration_sec` 60..43200, 없으면 `user_settings.focus_default_min*60` → `stub.startFocus(me, sec)` → `{focus}`. `GET` → `(await stub.getPresenceState(me)).focus`. `DELETE` → `stub.endFocus(me)` 가 null 이면 `err.conflict("conflict","not_focusing")`, 아니면 `{summary}`. (이벤트 `focus.started/ended`, `presence.updated` 발행은 UserHub 몫.)
- 고정 기준 맞추기(경계 규칙 — 1부 구현이 이 계획의 테스트에 어긋나면 이 계획대로 고친다): `server/src/do/user-hub.ts` 의 집중 종료 공통 경로(`endFocus` 의 DELETE 조기 종료와 알람 자연 종료가 함께 쓰는 처리)가 종료 사유와 상관없이 `focus.ended{summary}` 다음에 `alert{kind:"focus_summary", title:"집중 모드 끝 — 멘션 N, DM M", suppressed:false}`(기록)를 내도록 한다. 이미 그렇게 돼 있으면 손대지 않는다. 이 alert 는 집중이 끝난 뒤 발행되므로 suppression 대상이 아니다. 1부 `test/realtime/presence-focus.test.ts` 의 두 focus 테스트는 그대로 PASS 해야 한다(조기 종료 테스트는 alert 부재를 단언하지 않는다).

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/status-focus.test.ts test/realtime/presence-focus.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/routes/me.ts server/src/routes/presence.ts server/src/do/user-hub.ts server/test/api/status-focus.test.ts
git commit -m "feat(server): custom status message and focus mode endpoints"
```

---

### Task 14: 신고

**Files:**
- Create: `server/src/routes/reports.ts` (`POST /reports`)
- Modify: `server/src/app.ts` (`reportRoutes`, `guestbookRoutes` 다음)
- Test: `server/test/api/reports.test.ts`

**Interfaces:**
- Consumes: `requireChannelRead`, `canViewPost`, `canViewUserContent`, `getSpaceRole`, `resolveUserRef`, `limit("RL_SOCIAL")`.
- Produces: `201 { report_id: ID }`; `reports.snapshot` JSON — user `{handle, display_name, bio, avatar_url}`, message `{body, author_id, channel_id, kind}`, post `{body, author_id, media_urls}`, story `{body, author_id, media_urls}`, guestbook_entry `{body, author_id, owner_id}`, space `{name, description, owner_id}`; `target_user_id` = 작성자/소유자/본인.

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/reports.test.ts
import { env } from "cloudflare:test";
import { api, signup, createPost, uploadPng, makeSpace } from "../helpers";
const report = (t: any, j: any) => api(t.token).post<any>("/v1/reports", { reason: "spam", ...j });

test("report post copies snapshot; duplicate open report -> 409 already_exists", async () => {
  const a = await signup(); const r = await signup();
  const m = await uploadPng(a, "post");
  const p = await createPost(a, "광고 #스팸", { media_upload_ids: [m.id] });
  const res = await report(r, { target_type: "post", target_id: p.id, note: "도배" });
  expect(res.status).toBe(201);
  const row = await env.DB.prepare("SELECT target_user_id, snapshot, status FROM reports WHERE id=?").bind(res.body.report_id).first<any>();
  expect(row.target_user_id).toBe(a.user.id); expect(row.status).toBe("open");
  expect(JSON.parse(row.snapshot)).toMatchObject({ body: "광고 #스팸", author_id: a.user.id, media_urls: [`/v1/media/${m.id}`] });
  expect((await report(r, { target_type: "post", target_id: p.id })).body.error.code).toBe("already_exists");
});
test("reporter must be able to see the target", async () => {
  const a = await signup(); const r = await signup();
  await api(a.token).patch("/v1/me/settings", { is_private: true });
  const p = await createPost(a, "secret");
  expect((await report(r, { target_type: "post", target_id: p.id })).status).toBe(404);
  const { space, generalChannelId } = await makeSpace(a);
  const msg = (await api(a.token).post<any>(`/v1/channels/${generalChannelId}/messages`, { client_nonce: "m", body: "x" })).body.message;
  expect((await report(r, { target_type: "message", target_id: msg.id })).status).toBe(404);
  expect((await report(r, { target_type: "space", target_id: space.id })).status).toBe(404);
  expect((await report(r, { target_type: "user", target_id: a.user.id, reason: "harassment" })).status).toBe(201);
});
test("validation: bad reason, note too long", async () => {
  const a = await signup(); const r = await signup();
  expect((await report(r, { target_type: "user", target_id: a.user.id, reason: "meh" })).status).toBe(400);
  expect((await report(r, { target_type: "user", target_id: a.user.id, note: "x".repeat(1001) })).body.error.details.fields.note).toBe("too_long");
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/reports.test.ts`
Expected: FAIL — 404.

- [ ] **Step 3: 구현**

`POST /reports`(`RL_SOCIAL`): zod enum 두 개 + `note: zText(0,1000)` 기본 `""`. 대상별 열람 검사(user: 존재 + 차단 무관 프로필 공개 / message: `requireChannelRead` / post: 삭제 안 됨 + `canViewPost` / story: 미삭제·미만료 + `canViewUserContent==="ok"` / guestbook_entry: 미삭제 + 주인 `canViewUserContent==="ok"` + 비밀글이면 주인·작성자만 / space: `getSpaceRole` non-null + 미삭제), 실패 404. 같은 신고자·대상의 open 신고가 있으면 `err.conflict("already_exists")`(INSERT 의 UNIQUE 충돌도 같은 오류로). 신고자에게 이벤트·알림 없음.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/reports.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/routes/reports.ts server/src/app.ts server/test/api/reports.test.ts
git commit -m "feat(server): content and user reports with snapshots"
```

---

### Task 15: 운영자 — 콘텐츠 강제 삭제

**Files:**
- Create: `server/src/services/content.ts`
- Create: `server/src/routes/admin.ts` (`adminRoutes.use("/admin/*", requireAuth, requireAdmin)` + `POST /admin/content/remove`; Task 16 이 덧붙임)
- Modify(덧붙이기): `server/src/lib/serialize.ts` — 계획 추가분 `serializeAdminActions(db: D1Database, rows: AdminActionRow[]): Promise<AdminAction[]>` (계약 4.9 에 없는 이름; `AdminActionRow` 가 types/rows.ts 에 없으면 data-model 3.10 컬럼 그대로 덧붙임)
- Modify: `server/src/app.ts` (`adminRoutes`, `reportRoutes` 다음)
- Test: `server/test/api/reports-admin.test.ts` (Task 16 과 공유)

**Interfaces:**
- Consumes: `deleteMessage(rt, id, actorId, {asAdmin:true})`, `deletePost(rt, id, actorId, {asAdmin:true})`, `removeUploads`, `publishEvent`, `requireAdmin`, `makeAdmin`.
- Produces: `removeContent(rt: Runtime, adminId: string, target: { type: "message" | "post" | "story" | "guestbook_entry" | "upload" | "space"; id: string }, reason: string, reportId?: string): Promise<AdminAction>`; 모듈 비공개 `resolveReports(db, reportId, targetType, targetId, adminId, resolution, note): D1PreparedStatement` (Task 16 도 같은 규칙을 씀 — 같은 파일에서 export).

- [ ] **Step 1: 실패하는 테스트 작성**

```ts
// test/api/reports-admin.test.ts
import { env } from "cloudflare:test";
import { api, signup, createPost, uploadPng, makeSpace, makeAdmin, connectWs } from "../helpers";
const remove = (adm: any, j: any) => api(adm.token).post<any>("/v1/admin/content/remove", { reason: "illegal", ...j });

test("non-admin -> 403 not_admin", async () => {
  const u = await signup();
  expect((await api(u.token).get<any>("/v1/admin/reports")).body.error.code).toBe("not_admin");
});
test("remove post with report: R2 deleted, post gone, all open reports on target resolved", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const a = await signup(); const r1 = await signup(); const r2 = await signup();
  const m = await uploadPng(a, "post");
  const p = await createPost(a, "bad", { media_upload_ids: [m.id] });
  const rep1 = (await api(r1.token).post<any>("/v1/reports", { target_type: "post", target_id: p.id, reason: "illegal" })).body.report_id;
  const rep2 = (await api(r2.token).post<any>("/v1/reports", { target_type: "post", target_id: p.id, reason: "spam" })).body.report_id;
  const res = await remove(adm, { target_type: "post", target_id: p.id, report_id: rep1 });
  expect(res.body.action).toMatchObject({ action: "remove_content", target_type: "post", target_id: p.id, report_id: rep1, admin: { id: adm.user.id } });
  expect((await api(r1.token).get(`/v1/posts/${p.id}`)).status).toBe(404);
  expect(await env.MEDIA.head(`posts/${a.user.id}/${m.id}.png`)).toBeNull();
  const rows = (await env.DB.prepare("SELECT status, resolution FROM reports WHERE id IN (?, ?)").bind(rep1, rep2).all<any>()).results;
  expect(rows).toEqual([{ status: "resolved", resolution: "content_removed" }, { status: "resolved", resolution: "content_removed" }]);
});
test("remove message emits message.deleted; remove upload detaches from post; remove space emits space.deleted", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const a = await signup(); const b = await signup();
  const { space, generalChannelId } = await makeSpace(a, [b]);
  const msg = (await api(a.token).post<any>(`/v1/channels/${generalChannelId}/messages`, { client_nonce: "x", body: "bad" })).body.message;
  const wb = await connectWs(b.token);
  await remove(adm, { target_type: "message", target_id: msg.id });
  expect((await wb.next("message.deleted")).data.message_id).toBe(msg.id);
  const m1 = await uploadPng(a, "post"); const m2 = await uploadPng(a, "post");
  const p = await createPost(a, "two pics", { media_upload_ids: [m1.id, m2.id] });
  await remove(adm, { target_type: "upload", target_id: m1.id });
  expect((await api(b.token).get<any>(`/v1/posts/${p.id}`)).body.post.media.map((x: any) => x.id)).toEqual([m2.id]);
  expect((await api(b.token).raw(`/v1/media/${m1.id}`, { method: "GET" })).status).toBe(404);
  await remove(adm, { target_type: "space", target_id: space.id });
  expect((await wb.next("space.deleted")).data).toEqual({ space_id: space.id });
  expect((await api(b.token).get(`/v1/spaces/${space.id}`)).status).toBe(404);
});
test("remove story and guestbook entry; unknown target -> 404", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const a = await signup(); const o = await signup();
  const s = (await api(a.token).post<any>("/v1/stories", { body: "x" })).body.story;
  await remove(adm, { target_type: "story", target_id: s.id });
  expect((await api(a.token).get<any>(`/v1/users/@${a.handle}/stories`)).body.items).toEqual([]);
  const e = (await api(a.token).post<any>(`/v1/users/@${o.handle}/guestbook`, { body: "bad" })).body.entry;
  await remove(adm, { target_type: "guestbook_entry", target_id: e.id });
  expect((await api(o.token).get<any>(`/v1/users/@${o.handle}/guestbook`)).body.items).toEqual([]);
  expect((await remove(adm, { target_type: "post", target_id: "0".repeat(26) })).status).toBe(404);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/reports-admin.test.ts`
Expected: FAIL — `/v1/admin/*` 404.

- [ ] **Step 3: 구현**

`removeContent` 대상별(없으면 `err.notFound()`):
- `message` → `deleteMessage(..., {asAdmin:true})`. `post` → `deletePost(..., {asAdmin:true})`.
- `story` → `deleted_at=now` + `removeUploads` + `story.deleted`(작성자 followers + includeSelf).
- `guestbook_entry` → `body='', deleted_at, deleted_by=adminId`.
- `upload` → `removeUploads(env,[id])` + 붙은 곳 정리: `post:<id>` → `post_media` 행 DELETE, `avatar:<uid>` → `users.avatar_upload_id=NULL`, `story:<id>` → 스토리 본문이 `''` 이면 스토리 소프트 삭제(CHECK 제약 때문에 `media_upload_id` 는 그대로), 아니면 `media_upload_id=NULL`.
- `space` → 삭제 **전에** 멤버 ID 를 읽어 두고, batch(`spaces.deleted_at`, 그 방 `channels.deleted_at`) 후 `publishEvent(rt, {users: memberIds}, "space.deleted", {space_id})`.
- 공통 마무리 batch: `admin_actions` INSERT(`remove_content`, `report_id`) + `reportId` 가 있으면 `UPDATE reports SET status='resolved', resolution='content_removed', resolved_by, resolved_at, resolution_note=? WHERE status='open' AND (id=?1 OR (target_type=?2 AND target_id=?3))` (target_type 은 `upload` 일 때 신고 대상 종류와 다를 수 있으므로 `id=?1` 이 기준). 반환은 `serializeAdminActions`.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/reports-admin.test.ts test/api/messages.test.ts test/api/spaces.test.ts`
Expected: PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/services/content.ts server/src/routes/admin.ts server/src/lib/serialize.ts server/src/types/rows.ts server/src/app.ts server/test/api/reports-admin.test.ts
git commit -m "feat(server): admin forced content removal with R2 deletion and report resolution"
```

---

### Task 16: 운영자 — 신고 처리·계정 정지·조회

**Files:**
- Modify(덧붙이기): `server/src/services/users.ts` (`suspendUser`, `unsuspendUser`)
- Modify: `server/src/routes/admin.ts` (`GET /admin/reports`, `GET /admin/reports/:report_id`, `POST /admin/reports/:report_id/resolve`, `POST /admin/users/:user/suspend|unsuspend`, `GET /admin/users`, `GET /admin/actions`)
- Modify(덧붙이기): `server/src/lib/serialize.ts` — 계약 4.9 `serializeReports` + 계획 추가분 `serializeAdminUsers(db: D1Database, rows: UserRow[]): Promise<AdminUserView[]>`
- Test: `server/test/api/reports-admin.test.ts` (덧붙임)

**Interfaces:**
- Consumes: `revokeSessions(rt, userId, "all", "suspended")`(1부 services/users.ts), `loadUserSummaries`, Task 15 `resolveReports`, `serializeAdminActions`.
- Produces:
  - `serializeReports(db: D1Database, rows: ReportRow[]): Promise<Report[]>` (계약 4.9) — 입력 순서 유지. 일괄(각 1회): 사람 `loadUserSummaries(reporter_id ∪ target_user_id ∪ resolved_by)`; 대상 존재 여부는 행에 나온 `target_type` 마다 1회(최대 6회) — `user`: `users` 행 있음, `message`/`post`/`guestbook_entry`/`space`: 행 있음 + `deleted_at IS NULL`, `story`: 행 있음 + `deleted_at IS NULL AND expires_at > now`(cron 이 지운 스토리는 행 없음 → `false`); 열린 신고 수 `SELECT target_type, target_id, COUNT(*) AS n FROM reports WHERE status='open' AND target_id IN (…) GROUP BY target_type, target_id`(종류까지 맞춰 읽음). 필드: `reporter` = 신고자 요약, `target_user` = `target_user_id` 요약 또는 `null`, `snapshot = JSON.parse(row.snapshot)`, `target_exists`, `open_reports_on_target`(없으면 `0`), `resolved_by` = 요약 또는 `null`, `resolved_at`/`created_at` = `isoOrNull`/`iso`, `note`·`reason`·`status`·`resolution`·`resolution_note` 그대로.
  - `suspendUser(rt: Runtime, adminId: string, userId: string, reason: string, durationSec: number | null, reportId?: string): Promise<AdminUserView>`
  - `unsuspendUser(rt: Runtime, adminId: string, userId: string): Promise<AdminUserView>`

- [ ] **Step 1: 실패하는 테스트 작성 (같은 파일에 덧붙임)**

```ts
test("suspend: socket 4003, login 403, admin target 409, report resolved user_suspended; unsuspend", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const adm2 = await signup(); await makeAdmin(adm2);
  const u = await signup(); const r = await signup();
  const rep = (await api(r.token).post<any>("/v1/reports", { target_type: "user", target_id: u.user.id, reason: "harassment" })).body.report_id;
  const w = await connectWs(u.token);
  const closed = new Promise<number>((res) => w.ws.addEventListener("close", (e) => res(e.code)));
  const s = await api(adm.token).post<any>(`/v1/admin/users/@${u.handle}/suspend`, { reason: "harassment", duration_sec: 3600, report_id: rep });
  expect(s.body.user).toMatchObject({ id: u.user.id, suspended: true, suspend_reason: "harassment", open_report_count: 0 });
  expect(await closed).toBe(4003);
  const login = await api().post<any>("/v1/auth/login", { handle: u.handle, password: u.password });
  expect(login.body.error.code).toBe("account_suspended");
  expect((await api(adm.token).get<any>(`/v1/admin/reports/${rep}`)).body.report).toMatchObject({ status: "resolved", resolution: "user_suspended", resolved_by: { id: adm.user.id } });
  expect((await api(adm.token).post<any>(`/v1/admin/users/@${adm2.handle}/suspend`, { reason: "x" })).status).toBe(409);
  expect((await api(adm.token).post<any>(`/v1/admin/users/@${u.handle}/unsuspend`)).body.user).toMatchObject({ suspended: false, suspended_at: null });
  expect((await api().post("/v1/auth/login", { handle: u.handle, password: u.password })).status).toBe(200);
});
test("suspension hides posts and media, unsuspend restores", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const u = await signup(); const v = await signup();
  const tag = `s${Math.random().toString(36).slice(2, 8)}`;
  const m = await uploadPng(u, "post");
  const p = await createPost(u, `#${tag}`, { media_upload_ids: [m.id] });
  await api(adm.token).post(`/v1/admin/users/@${u.handle}/suspend`, { reason: "x" });
  expect((await api(v.token).get<any>(`/v1/hashtags/${tag}/posts`)).body.items).toEqual([]);
  expect((await api(v.token).get<any>(`/v1/timeline/discover?limit=100`)).body.items.find((i: any) => i.post.id === p.id)).toBeUndefined();
  expect((await api(v.token).raw(`/v1/media/${m.id}`, { method: "GET" })).status).toBe(404);
  await api(adm.token).post(`/v1/admin/users/@${u.handle}/unsuspend`);
  expect((await api(v.token).get<any>(`/v1/hashtags/${tag}/posts`)).body.items.map((x: any) => x.id)).toEqual([p.id]);
});
test("reports list oldest-first by status; resolve no_action -> dismissed + dismiss_report; users & actions listing", async () => {
  const adm = await signup(); await makeAdmin(adm);
  const a = await signup(); const r = await signup();
  const p1 = await createPost(a, "1"); const p2 = await createPost(a, "2");
  const id1 = (await api(r.token).post<any>("/v1/reports", { target_type: "post", target_id: p1.id, reason: "spam" })).body.report_id;
  const id2 = (await api(r.token).post<any>("/v1/reports", { target_type: "post", target_id: p2.id, reason: "spam" })).body.report_id;
  const open = (await api(adm.token).get<any>("/v1/admin/reports?target_type=post&limit=100")).body.items.map((x: any) => x.id);
  expect(open.indexOf(id1)).toBeLessThan(open.indexOf(id2));
  const d = await api(adm.token).post<any>(`/v1/admin/reports/${id1}/resolve`, { resolution: "no_action", note: "ok" });
  expect(d.body.report).toMatchObject({ status: "dismissed", resolution: "no_action", resolution_note: "ok", target_exists: true,
    reporter: { id: r.user.id }, target_user: { id: a.user.id }, resolved_by: { id: adm.user.id }, open_reports_on_target: 0, snapshot: { body: "1" } });
  const open2 = (await api(adm.token).get<any>(`/v1/admin/reports/${id2}`)).body.report;
  expect(open2).toMatchObject({ status: "open", open_reports_on_target: 1, resolved_by: null, resolved_at: null });
  await api(a.token).del(`/v1/posts/${p2.id}`);
  expect((await api(adm.token).get<any>(`/v1/admin/reports/${id2}`)).body.report.target_exists).toBe(false);
  expect((await api(adm.token).get<any>("/v1/admin/reports?status=dismissed&limit=100")).body.items.map((x: any) => x.id)).toContain(id1);
  const acts = (await api(adm.token).get<any>(`/v1/admin/actions?target_type=post&target_id=${p1.id}`)).body.items;
  expect(acts.map((x: any) => x.action)).toEqual(["dismiss_report"]);
  await api(adm.token).post(`/v1/admin/users/@${a.handle}/suspend`, { reason: "x" });
  const us = (await api(adm.token).get<any>(`/v1/admin/users?q=${a.handle}&suspended=true`)).body.items;
  expect(us.map((x: any) => [x.id, x.open_report_count])).toEqual([[a.user.id, 1]]);
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/reports-admin.test.ts`
Expected: FAIL — 새 3개 테스트 404.

- [ ] **Step 3: 구현**

- `suspendUser`: 대상 `role='admin'` → `err.conflict("conflict","admin")`. batch: `users SET suspended_at=now, suspended_until = durationSec ? now+sec*1000 : NULL, suspend_reason` + `admin_actions`(`suspend_user`) + `reportId` 있으면 `resolveReports(..., "user_suspended")`. 커밋 뒤 `revokeSessions(rt, userId, "all", "suspended")`(D1 폐기 → UserHub 가 `session.revoked` 후 4003). `unsuspendUser`: 세 컬럼 NULL + `admin_actions`(`unsuspend_user`).
- `GET /admin/reports`: `status` 기본 `open`, `idx_reports_status` ID **오름차순** 커서, `target_type` 선택 필터 → `serializeReports`. `GET /admin/reports/:id`·`resolve` 응답도 `serializeReports(db, [row])[0]`.
- `lib/serialize.ts` 덧붙임: Interfaces 의 `serializeReports`(1부가 스텁만 두었으면 본문을 채운다)와 `serializeAdminUsers`.
- `POST /admin/reports/:id/resolve`: `no_action` → `dismissed` + `dismiss_report`, 그 외 → `resolved` + `resolve_report`; 같은 대상의 다른 open 신고도 같은 값으로(`resolveReports`).
- `GET /admin/users`: `q` = handle 접두(`LIKE ?||'%'`) 또는 display_name 부분 일치, `suspended=true|false` 필터(`suspended_at IS NOT NULL AND (suspended_until IS NULL OR suspended_until > now)`), ID 내림차순 커서 → `serializeAdminUsers`(`open_report_count` = `target_user_id` 기준 open 신고 수, `json_each` 한 번).
- `GET /admin/actions`: `target_type`·`target_id` 필터, ID 내림차순 커서.

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/reports-admin.test.ts test/api/auth.test.ts && npm test`
Expected: 전체 PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/services/users.ts server/src/routes/admin.ts server/src/lib/serialize.ts server/src/types/rows.ts server/test/api/reports-admin.test.ts
git commit -m "feat(server): admin report triage, suspension and audit listings"
```

---

### Task 17: 글 검색·해시태그 검색 고정 (`/search/posts`, `/hashtags/search`)

1부 Task 29 가 이 두 경로(`routes/search.ts`)와 `serializeMedia`·`serializePosts` 를 만든다(1부 소유 경계 — `lib/search.ts` 에 새 export 없음, `buildMessageSearch`·`highlights` 를 그대로 씀). 1부 테스트는 글·해시태그 행을 SQL 로 심어 검증하므로, 이 태스크는 **이 계획의 실제 글 작성 경로(`createPost` → `post_hashtags`·`posts_fts` 트리거·`deletePost`)를 거친 끝에서 끝 동작**을 아래 테스트로 고정한다(경계 규칙 (b)). 1부에 경로가 없거나 스텁이면 1부 Task 29 의 Produces 규칙대로 그 파일에 **덧붙인다**(경계 규칙 (c)). 어느 쪽이든 끝나면 api.md 5.13·설계서 3.1(전역 검색)·3.2(해시태그) 범위가 이 계획의 테스트로도 덮인다.

**Files:**
- Modify(고정; 없거나 테스트와 어긋날 때만 수정): `server/src/routes/search.ts` — `GET /search/posts`, `GET /hashtags/search` (`searchRoutes` 는 1부가 이미 `app.ts` 에 마운트함 — `app.ts` 는 건드리지 않는다. 같은 경로를 다시 등록하지 않는다)
- Modify(어긋날 때만): `server/src/lib/serialize.ts` — 검색 결과의 `post` 가 Task 4 의 `serializePosts` 고정 기준과 다를 때
- Test: `server/test/api/search-posts.test.ts`

**Interfaces:**
- Consumes: 1부 Task 29 `GET /search/posts`·`GET /hashtags/search`, `serializePosts`; 1부 Task 28 `buildMessageSearch`, `highlights`; `idFloor`, `postVisibilitySql`, `resolveUserRef`, `limit("RL_SEARCH")`, 라우트별 `requireAuth`(`/search/*`·`/hashtags/*` 는 공유 접두사); Task 4 `createPost`·`deletePost`, `uniqueHandle`.
- Produces: 없음(고정 테스트). 고정하는 동작(1부 Task 29 Produces 와 같음):
  - `GET /search/posts` — `RL_SEARCH`; `q` NFC·trim 1..100(빈 값 400); `limit` 기본 20 최대 50; 모든 낱말이 3글자 이상이면 `posts_fts` trigram MATCH, 하나라도 짧으면 `LIKE` 폴백 + **최근 90일**(`p.id >= idFloor(now - 90일)`)로 범위 제한; 두 모드 공통 `p.deleted_at IS NULL` + `postVisibilitySql(viewer, "p")`(정지·양방향 차단·비공개 비팔로워 제외; 답글도 대상); `author` 필터; `p.id` 내림차순 커서; 응답 `{ items: [{ post, highlights }], next_cursor }`.
  - `GET /hashtags/search` — `RL_SEARCH`; `q` 는 앞 `#` 제거·NFC·소문자·trim 후 1..50(빈 값 400); `limit` 기본 10 최대 20; 후보 = `post_hashtags.created_at >= now - 7일` 인 태그 중 접두 일치(범위 비교 — `LIKE` 와일드카드 문제 없음), 삭제 글·비공개·정지 계정 제외(트렌드와 같은 규칙), `GROUP BY tag`; `post_count_24h` = 그중 24시간 안 개수(없으면 `0`); 정렬 `post_count_24h DESC, tag ASC`; 응답 `{ items: [{ tag, post_count_24h }] }`.

- [ ] **Step 1: 실패하는(또는 1부 구현을 고정하는) 테스트 작성**

```ts
// test/api/search-posts.test.ts
import { env } from "cloudflare:test";
import { api, signup, createPost, follow, uniqueHandle } from "../helpers";
const sp = async (t: any, q: string) => (await api(t.token).get<any>(`/v1/search/posts?q=${encodeURIComponent(q)}`)).body;

test("3+ char Korean term via FTS with highlights; 2-char term via LIKE fallback", async () => {
  const a = await signup(); const v = await signup();
  const tag = uniqueHandle("s").slice(-6);
  const p = await createPost(a, `오늘 배포는 ${tag} 완료`);
  await createPost(a, `다른 글 ${tag}`);
  const r = await sp(v, `배포는 ${tag}`);
  expect(r.items.map((i: any) => i.post.id)).toEqual([p.id]);
  expect(r.items[0].highlights).toEqual([{ start: 3, length: 3 }, { start: 7, length: tag.length }]);
  expect(r.next_cursor).toBeNull();
  expect((await sp(v, `배포 ${tag}`)).items.map((i: any) => i.post.id)).toEqual([p.id]);   // "배포" < 3 chars -> LIKE
  expect((await api(v.token).get("/v1/search/posts?q=")).status).toBe(400);
});
test("LIKE fallback only looks at the last 90 days", async () => {
  const a = await signup(); const v = await signup();
  const tag = uniqueHandle("o").slice(-6);
  const old = await createPost(a, `옛 ${tag}`);
  const oldId = "0".repeat(10) + old.id.slice(10);   // same random part, timestamp far in the past
  await env.DB.prepare("UPDATE posts SET id=?, created_at=? WHERE id=?").bind(oldId, Date.now() - 100 * 86_400_000, old.id).run();
  expect((await sp(v, `옛 ${tag}`)).items).toEqual([]);
  expect((await sp(v, `${tag}`)).items.map((i: any) => i.post.id)).toEqual([oldId]);   // FTS path has no age bound
});
test("visibility: blocked, private non-follower, suspended and deleted authors excluded; private visible to active follower", async () => {
  const v = await signup(); const blk = await signup(); const priv = await signup(); const sus = await signup(); const pub = await signup();
  const tag = uniqueHandle("q").slice(-6);
  await api(priv.token).patch("/v1/me/settings", { is_private: true });
  await createPost(blk, `검색어 ${tag}`);
  const pp = await createPost(priv, `검색어 ${tag}`);
  await createPost(sus, `검색어 ${tag}`);
  const pd = await createPost(pub, `검색어 ${tag} 삭제`);
  const ok = await createPost(pub, `검색어 ${tag}`);
  await api(blk.token).put(`/v1/blocks/@${v.handle}`);
  await env.DB.prepare("UPDATE users SET suspended_at=? WHERE id=?").bind(Date.now(), sus.user.id).run();
  await api(pub.token).del(`/v1/posts/${pd.id}`);
  expect((await sp(v, `검색어 ${tag}`)).items.map((i: any) => i.post.id)).toEqual([ok.id]);
  await follow(v, priv); await api(priv.token).post(`/v1/me/follow-requests/@${v.handle}/accept`);
  expect((await sp(v, `검색어 ${tag}`)).items.map((i: any) => i.post.id)).toEqual([ok.id, pp.id]);
});
test("hashtag search: prefix over the last 7 days with 24h counts; private and >7d excluded", async () => {
  const a = await signup(); const b = await signup(); const priv = await signup(); const v = await signup();
  const pre = `해${uniqueHandle("h").slice(-5)}`;
  await createPost(a, `#${pre}가 #${pre}나`); await createPost(b, `#${pre}가`);
  const twoDays = await createPost(a, `#${pre}다`);
  await env.DB.prepare("UPDATE post_hashtags SET created_at=? WHERE post_id=?").bind(Date.now() - 2 * 86_400_000, twoDays.id).run();
  const old = await createPost(a, `#${pre}라`);
  await env.DB.prepare("UPDATE post_hashtags SET created_at=? WHERE post_id=?").bind(Date.now() - 8 * 86_400_000, old.id).run();
  await api(priv.token).patch("/v1/me/settings", { is_private: true });
  await createPost(priv, `#${pre}마`);
  const r = (await api(v.token).get<any>(`/v1/hashtags/search?q=${encodeURIComponent("#" + pre.toUpperCase())}`)).body;
  expect(r.items).toEqual([{ tag: `${pre}가`, post_count_24h: 2 }, { tag: `${pre}나`, post_count_24h: 1 }, { tag: `${pre}다`, post_count_24h: 0 }]);
  expect((await api(v.token).get(`/v1/hashtags/search?q=${encodeURIComponent("#")}`)).status).toBe(400);   // empty after stripping "#"
});
```

- [ ] **Step 2: 실패 확인**

Run: `cd server && npx vitest run test/api/search-posts.test.ts`
Expected: 1부 Task 29 가 끝나 있으면 PASS(이때 Step 3 은 건너뛰고 Step 4 로). 아니면 FAIL — `/v1/search/posts`·`/v1/hashtags/search` 404 또는 단언 불일치.

- [ ] **Step 3: 구현 (1부에 없거나 테스트와 어긋날 때만)**

- 1부에 경로가 없으면 `routes/search.ts` 에 1부 Task 29 Produces 그대로 두 핸들러를 덧붙인다(라우트마다 `requireAuth, limit("RL_SEARCH"), vQuery(...)`; `buildMessageSearch`·`highlights` 를 그대로 쓰고 `lib/search.ts` 에 새 export 를 만들지 않는다).
- 경로가 있는데 단언이 어긋나면 다시 등록하지 말고 그 핸들러 본문을 위 Interfaces 의 고정 동작대로 고친다(실패 원인이 `serializePosts` 면 Task 4 의 규칙으로 고친다).

- [ ] **Step 4: 통과 확인**

Run: `cd server && npx vitest run test/api/search-posts.test.ts test/api/search.test.ts && npx tsc --noEmit`
Expected: PASS, 1부 메시지 검색 테스트 회귀 없음.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/src/routes/search.ts server/src/lib/serialize.ts server/test/api/search-posts.test.ts
git commit -m "test(server): pin post search and hashtag search behaviour"
```

---

### Task 18: 로컬 E2E 스모크 스크립트 (`wrangler dev` 대상)

**Files:**
- Create: `server/scripts/smoke.mjs` (계약 파일 트리에 없는 유일한 경로 — 오케스트레이터가 지정한 범위 "local end-to-end smoke script" 를 위해 이 계획이 정함)
- Modify(덧붙이기): `server/package.json` `"scripts": { "smoke": "node scripts/smoke.mjs" }`

**Interfaces:**
- Consumes: 실행 중인 `npx wrangler dev --test-scheduled`(기본 `http://localhost:8787`, `MASIL_SMOKE_BASE` 로 변경), Node 22+ 전역 `fetch`·`WebSocket`(Node 26 에서 `new WebSocket(url, { headers })` 로 `Authorization` 헤더 전송 확인함), `child_process.execFileSync("npx", ["wrangler","d1","execute","DB","--local","--command", sql])`(관리자 지정 — **`--local` 고정, `--remote` 금지**).
- Produces: 단계마다 `✓ <단계>` 한 줄, 끝에 `SMOKE OK`, 실패 시 단계 이름과 응답 본문을 찍고 exit 1.

- [ ] **Step 1: 스크립트가 검증할 시나리오(=테스트)를 작성**

`server/scripts/smoke.mjs` 단계(순서 고정, 각 단계는 `check(name, cond, detail)` 로 단언):
1. `GET /v1/health` → `{ok:true}`.
2. 가입 3명(`smk_<6자 랜덤>` alice·bob·admin; User-Agent `masil/0.1.0 (smoke; node)`).
3. alice WS(`mode=full`, `device_id` = 스크립트 안 `ulid()`(48비트 ms 시각 + 80비트 난수, Crockford base32 26자)) → `hello` → `resume {epoch:null,last_seq:null}` → `ready`. 같은 기기 데몬 WS(`mode=notify`) 도 `ready` 까지.
4. bob → alice 팔로우 `active`.
5. alice 업로드(1×1 PNG, `purpose=post`) 201 → `POST /v1/posts`(`#스모크<랜덤>`, 미디어 1장) 201 → bob `GET /v1/timeline/following` 첫 항목 = 그 글 → bob `GET /v1/media/<id>` 바이트 일치·`ETag` 재요청 304.
6. bob 좋아요 → alice notify 소켓이 `notification.created`(`post_like`) 수신.
7. bob 답글 → `GET /v1/posts/<id>/replies` 1개. bob 인용 → 원글 `quote_count:1`.
8. alice 스토리 → bob `GET /v1/stories/feed` 첫 그룹 alice·`has_unseen:true` → view → 다시 조회 `has_unseen:false`.
9. bob → alice 방명록 201, alice 알림함에 `guestbook_entry`.
10. alice 투표 글 → bob 투표 → `voter_count:1`.
11. alice `PUT /v1/me/status` → bob `GET /v1/users/@alice` 의 `status.text` 일치. alice `POST /v1/me/focus {duration_sec:60}` → `DELETE` → `summary` 존재 → alice full WS 가 `focus.ended` 수신 뒤 `next("alert", d => d.alert.kind === "focus_summary")` 로 `alert{kind:"focus_summary", suppressed:false}` 수신(조기 종료에도 요약 alert — 클라이언트 요약 오버레이의 유일한 트리거). 같은 기기 데몬 WS(`mode=notify`)도 같은 `alert.id` 의 `focus_summary` 를 받는다(notify 허용 타입).
12. `GET {BASE}/__scheduled?cron=*/5+*+*+*+*` → `GET /v1/trends` 에 그 태그 포함. `GET {BASE}/__scheduled?cron=17+*+*+*+*` → 200.
13. bob 신고(alice 글) 201 → admin 계정 `role='admin'` 로컬 D1 갱신 → `GET /v1/admin/reports` 에 그 신고 → `POST /v1/admin/content/remove`(report_id 포함) → bob 이 그 글 404, 미디어 404.
14. admin 이 alice 정지 → alice WS close 코드 4003 → alice 로그인 403 `account_suspended` → 해제 → 로그인 200.
15. `SMOKE OK`.

- [ ] **Step 2: 서버 없이 실행해 실패 확인**

Run: `cd server && npm run smoke`
Expected: `✗ health` (연결 거부) 와 exit code 1.

- [ ] **Step 3: 스크립트 구현**

ESM 한 파일, 외부 의존성 없음. 헬퍼 `call(method, path, {token, json, body, headers})`, `wsConnect(token, mode, deviceId)`(메시지 버퍼 + `next(type, pred, ms=5000)`), `check(name, cond, detail)`. 각 실행이 새 handle 을 쓰므로 반복 실행 가능; 단 `RL_SIGNUP`(IP 당 60초 5회) 때문에 **연속 실행은 60초 간격**(스크립트 시작 시 안내 출력).

- [ ] **Step 4: 로컬 서버에 대해 통과 확인**

Run:
```bash
cd server
npx wrangler d1 migrations apply DB --local
npx wrangler dev --test-scheduled &        # http://localhost:8787 (local D1/R2/DO/ratelimit simulation)
sleep 8 && npm run smoke; kill %1
```
Expected: `✓` 14줄 + `SMOKE OK`, exit code 0. 이어서 `npm test && npx tsc --noEmit` 전체 PASS.

- [ ] **Step 5: Commit** (실행 세션이 커밋 권한을 받았을 때만; `git push` 금지 — Global Constraints)

```bash
git add server/scripts/smoke.mjs server/package.json
git commit -m "test(server): local end-to-end smoke script against wrangler dev"
```

---

## 부록: 배포 — **오너 승인 후 only** (이 계획의 실행 범위 밖)

아래는 되돌리기 어렵거나 과금될 수 있는 작업이다. 에이전트는 실행하지 않는다. 오너가 **건별로** 승인한 항목만, 승인받은 그 순서대로 실행한다.

- [ ] (오너 승인 후 only) `npx wrangler d1 create masil` → 출력된 `database_id` 를 `wrangler.jsonc` 자리표시자 대신 넣고 `npx wrangler types`.
- [ ] (오너 승인 후 only) `npx wrangler r2 bucket create masil-media`.
- [ ] (오너 승인 후 only) Rate Limiting `namespace_id`(7101~7110)가 계정 안에서 다른 Worker 와 겹치지 않는지 오너와 확인.
- [ ] (오너 승인 후 only) `npx wrangler d1 migrations apply DB --remote`.
- [ ] (오너 승인 후 only) `npx wrangler deploy` — Cron 2개(`*/5 * * * *`, `17 * * * *`)가 함께 등록된다.
- [ ] (오너 승인 후 only) 운영자 지정: `npx wrangler d1 execute DB --remote --command "UPDATE users SET role='admin' WHERE handle='<오너 handle>'"`.
- [ ] (오너 승인 후 only) 원격 스모크 `MASIL_SMOKE_BASE=https://masil.<account>.workers.dev npm run smoke` — 스크립트 13단계의 관리자 지정은 `--local` D1 전용이라 원격에서는 동작하지 않는다. 원격 실행 방식(13·14단계 생략 또는 위 운영자 지정 후 수동 확인)은 그때 오너에게 여쭙는다.
