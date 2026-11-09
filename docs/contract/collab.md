# masil 방 문서 실시간 편집 계약 (CollabHub) v1

> **v0.1.3 새 문서 (2026-11-09, P0 Task 2)**: 방 문서(`api.md` 5.24)의 내용 모델·문서 소켓 `GET /v1/docs/{doc_id}/ws` 의 프레임·한도·종료 코드·문서 종류 등록부·공유 벡터 형식. 서버 구현은 P0 Task 27·28(`server/src/do/collab-hub.ts`, `server/src/lib/collab-reducer.ts`, `server/src/lib/collab-doctypes.ts`), Go 구현은 Task 30(`internal/collab`). 6장 `arch` 는 P3, 7장 `apicol` 은 P4 가 채운다(더하기만, 다른 장은 고치지 않는다).

- 상태: 확정 계약 v1 (2026-11-09). 상위 출처: `docs/superpowers/specs/2026-11-09-masil-v0.1.3-design.md` C1·C2.11.
- 짝 문서: `api.md` 5.24(문서 REST·권한·버전), `events.md` 4.10(목록·댓글 배지용 `doc.*` 이벤트 — 내용은 이 소켓으로만), `data-model.md` 3.13(D1 메타·DO SQLite 표), `server-architecture.md` 3.1(`CollabHub`).
- 기존 실시간 소켓 `/v1/ws`(events.md)의 프레임 8KB·10초 30프레임 계약은 그대로다. 이 문서의 한도는 문서 소켓에만 적용된다.

---

## 1. 내용 모델

문서 하나 = **객체 묶음**. 객체는 `id`(문서 안에서 유일, 이름 규칙 1.2), `type`(객체 종류 이름, 이름 규칙 1.2, 예 `node`·`edge`·`request` — 종류별 목록은 6·7장), `fields`(필드 이름(이름 규칙 1.2) → JSON 값), `deleted`(삭제 표시) 를 가진다. 병합 단위는 **필드**다: 서로 다른 필드를 동시에 바꾸면 둘 다 남고, 같은 필드를 동시에 바꾸면 서버가 매긴 순번(`seq`)이 늦은 쪽이 이긴다(필드 LWW). 삭제는 표시만 하고 필드는 지우지 않는다(`undel` 로 되살리면 그대로 돌아온다).

```ts
type Json = null | boolean | number | string | Json[] | { [k: string]: Json };

interface Snapshot {
  seq: number;                       // 이 상태가 반영한 마지막 순번. 빈 문서 = 0
  objects: {
    id: string;
    type: string;
    fields: Record<string, Json>;
    deleted?: boolean;               // true 일 때만 실음
  }[];
}

// op — 클라→서버 op 프레임의 ops 원소, 서버→클라 op 프레임의 ops 원소
type Op =
  | { op: "put"; id: string; type: string; fields: Record<string, Json> }   // 만들기. 이미 있으면 준 필드만 덮어쓰고 type 은 그대로. 삭제 상태면 무시
  | { op: "set"; id: string; field: string; value: Json }                  // 필드 하나. 삭제된 객체·없는 객체면 버림
  | { op: "del"; id: string }                                              // 삭제가 이긴다(뒤의 set 은 버려짐). 없는 객체면 버림
  | { op: "undel"; id: string }                                            // 되살리기. 없는 객체면 버림
  | { op: "reset"; snapshot: Snapshot };                                   // 서버만 보냄(버전 복원). 상태 전체를 교체
```

### 1.1 적용 규칙 (결정적)

- 서버는 op 프레임을 받는 순서대로 순번을 하나씩 매기고(`seq` = 직전 + 1, 프레임 하나 = 순번 하나), 그 순서로 `ops` 를 앞에서부터 적용한다. 한 프레임 안에서 같은 필드를 두 번 바꾸면 뒤의 것이 남는다.
- `base_seq`(클라이언트가 보던 순번)는 **병합에 쓰지 않는다** — 필드 LWW = 순번 순서 적용. 클라이언트가 "내 편집이 덮어써졌다" 를 판단할 때만 쓴다.
- 적용 결과는 Go(`collab.Doc.Apply`)와 서버(`applyOps`)가 바이트 단위로 같아야 하고, 8장의 공유 벡터로 양쪽이 같은 파일을 시험한다.
- 스냅숏의 정규형: `objects` 는 `id` 의 바이트 순 정렬, `deleted` 는 `true` 일 때만 싣는다, 삭제된 객체도 필드를 그대로 싣는다.
- `reset` 은 상태 전체를 `snapshot.objects` 로 바꾼다. 결과 순번은 그 `reset` 을 실은 프레임의 `seq` 이고 `snapshot.seq` 는 무시한다(복원한 버전의 옛 순번일 뿐이다 — 벡터 `reset_replaces_everything`). `reset` 도 순번 하나를 쓴다.

```ts
// server/src/lib/collab-reducer.ts
export function applyOps(state: DocState, seq: number, ops: Op[]): Change[];   // 결정적, Go 와 같은 벡터
// Change { id, field, removed } — field "" = 객체 전체(put/del/undel/reset)
```

### 1.2 이름 규칙 (`id`·`type`·필드 이름)

- 셋 다 `^[A-Za-z0-9_.:-]{1,64}$` — ASCII 만이라 길이는 바이트·글자·UTF-16 어느 단위로 세도 같다(1..64).
- `__proto__`·`constructor`·`prototype` 은 셋 어디에도 쓸 수 없다.
- 어기면 그 프레임 전체가 3.3 1단계에서 `reject invalid`(벡터 `proto_field_name_is_rejected`·`reserved_names_are_rejected`·`non_ascii_names_are_rejected`·`name_length_is_1_to_64_bytes`).
- 필드 **값**(JSON) 안의 객체 키는 이 규칙을 받지 않는다 — 값은 통째로 저장·비교할 뿐 병합하지 않는다(벡터 `name_punctuation_is_allowed`).
- 서버 구현: 객체 표와 필드 표는 `Map`(또는 `Object.create(null)`)에 두고, 받은 JSON 의 필드 키는 `JSON.parse` 결과의 자기 키(`Object.keys`)로만 읽는다. `Object.assign`·`target[name] = …` 로 필드를 옮기지 않는다(`__proto__` 가 프로토타입을 바꾼다). Go 는 `map[string]json.RawMessage`.

---

## 2. 연결

- `GET /v1/docs/{doc_id}/ws`, `Upgrade: websocket`, `Authorization: Bearer <token>`(쿼리 금지, events.md 0.1 과 같음), `Accept-Language`(api.md 0.1). 리미터 `RL_WS`.
- Worker 가 `requireAuth` → `RL_WS` → 방 멤버 확인을 한 뒤 `X-Masil-Doc-Auth`(사용자 id·역할; 클라이언트가 보낸 같은 헤더는 지운다)를 붙여 `COLLAB_HUB.getByName("doc:" + doc_id)` 로 넘긴다. DO 는 그 헤더만 믿는다.
- 방 멤버가 아니거나, 방이 삭제됐거나(`spaces.deleted_at`, 소프트 삭제 — CASCADE 가 일어나지 않는다), 문서가 없거나 지워졌으면 업그레이드 전에 `404`(존재를 숨김, api.md 5.24).
- 서버는 WebSocket Hibernation 을 쓴다. 연결 첨부는 `{ userId, sessionId, role }`(`role` = `"creator"|"admin"|"member"`, 접속 때 Worker 가 정한 값; 바뀌면 4.1 의 `setRole` 이 고친다). presence 는 메모리에만 있고 저장하지 않는다(다시 붙으면 없다).

---

## 3. 프레임

모든 프레임은 JSON 텍스트 한 개, `t` 필드로 종류를 가른다.

### 3.1 클라→서버

```ts
{ t: "hello"; since: number | null }                    // 붙자마자 한 번. null = 처음(스냅숏 받기), n = 그 순번까지 갖고 있음
{ t: "op"; client_op_id: string /*^[A-Za-z0-9_-]{1,64}$*/; base_seq: number; ops: Op[] /*1..200, reset 금지*/ }
{ t: "presence"; cursor?: Json; selection?: string[]; dragging?: Json }   // 고친 것만
```

### 3.2 서버→클라

```ts
{ t: "hello"; seq: number; snapshot?: Snapshot; ops?: { seq: number; author: ID; ops: Op[] }[] }
  // since 가 null 이거나 압축으로 지워진 범위면 snapshot, 아니면 since+1 .. seq 의 ops.
  // 그 ops 묶음의 JSON 직렬화가 문서 종류의 maxDocBytes 를 넘으면 ops 대신 snapshot 을 보낸다(4장 프레임 상한)
{ t: "op"; seq: number; author: ID; client_op_id?: string; ops: Op[] }
  // 다른 사람의 편집(그리고 서버의 reset). 보낸 본인에게는 op 대신 ack.
  // 버전 복원의 op{ops:[{op:"reset", snapshot}]} 은 author = 복원을 요청한 사용자 ID, client_op_id 없음, 접속자 모두에게
{ t: "ack"; client_op_id: string; seq: number }
{ t: "reject"; client_op_id: string; code: "invalid" | "too_large" | "limit_exceeded" | "locked" | "forbidden" | "rate_limited"; detail?: string }
  // 프레임 전체가 거절됨(부분 적용 없음, 순번을 쓰지 않음)
{ t: "presence"; user: ID; cursor: Json | null; selection: string[]; dragging: Json | null }   // 다른 사람에게만
{ t: "kicked"; reason: "left" | "kicked" | "banned" | "space_deleted" | "doc_deleted" }      // 직후 4100 종료
```

- `reject.code` 는 위 여섯 개로 **닫혀 있다**. `forbidden` 은 예약이다 — v1 서버는 보내지 않는다(앞으로의 읽기 전용 역할용). 클라이언트는 받으면 `locked` 와 같이 다룬다(그 편집을 되돌리고 보내지 못한 편집으로 보존). 문서 종류별 이유는 선택 `detail`(`^[a-z0-9_]{1,40}$`, 예 `"arch_cycle"`·`"secret_value"`·`"limit_node"`)로만 더한다. 클라이언트는 모르는 `detail` 을 `code` 로만 다룬다.
- **멱등**: 이미 순번을 받은 `client_op_id` 를 다시 보내면(재접속 뒤 재전송) 상태를 바꾸지 않고 처음 `seq` 로 `ack` 만 다시 보낸다. `client_op_id` 는 문서마다 유일해야 하고(클라이언트가 만든 무작위 값), 압축으로 op 기록이 지워진 뒤의 재전송은 보장하지 않는다(그 전에 `hello` 의 snapshot 으로 따라잡는다).

### 3.3 검사 순서 (op 프레임마다, 원자적)

1. 공통: 프레임 모양, `client_op_id` 모양(3.1), `ops` 1..200, 각 op 의 모양(알려진 `op` 다섯 중 `reset` 이외, 필수 필드, 이름 규칙 1.2), 잠금(문서가 잠겨 있고(`locked`) 보낸 사람의 첨부 `role` 이 `creator`·`admin` 이 아니면 `reject locked` — 잠기지 않은 문서는 방 멤버 누구나 편집), 속도(4장).
2. 문서 종류의 `validateOp(op, state)` 를 op 마다(첫 거절에서 멈추고 그 `{code, detail}` 로 `reject`).
3. 적용한 뒤 한도: `maxDocBytes`(→ `too_large`), `maxObjects`(삭제 안 된 것, → `limit_exceeded`, `detail` 은 `limit_<type>`), `maxFieldBytes`(→ `too_large`). 넘으면 되돌리고 거절.

모르는 `op` 값·`reset` 은 1단계에서 `reject invalid`. 거절된 프레임은 순번·상태·`client_op_id` 기록을 남기지 않는다.

---

## 4. 한도·종료 코드

| 항목 | 값 | 넘으면 |
|---|---|---|
| 프레임 크기 클라→서버 | 65,536 바이트 | 서버가 close `1009` |
| 프레임 크기 서버→클라 | 2,162,688 바이트(가장 큰 `maxDocBytes` 2,097,152 + 65,536) | 클라이언트가 close `1009`(서버 버그) |
| op 프레임 | 연결당 초당 20 | 그 프레임 `reject rate_limited`; 3초 연속 넘기면 close `4008` |
| presence 프레임 | 연결당 초당 10 | 조용히 버림 |
| `ops` 개수 | 프레임당 1..200 | `reject invalid` |
| 문서 크기·객체 수·필드 크기 | 종류별(5장) | `reject too_large` / `limit_exceeded` |

| 종료 코드 | 이름 | 뜻 | 클라이언트 |
|---|---|---|---|
| 1009 | too_big | 프레임 상한 초과(위 표, 방향별) | 버그. 백오프 재접속 |
| 4001 | unauthorized | 세션 폐기(로그아웃·세션 끊기·비밀번호 변경, 4.1 `revokeSession`) | 재접속 금지(events.md 와 같음) |
| 4003 | account_suspended | 계정 정지(4.1 `revokeSession`) | 재접속 금지, 정지 안내(events.md 와 같음) |
| 4008 | rate_limited | op 프레임 과다가 3초 이어짐 | 10초 뒤 재접속 |
| 4100 | kicked | 직전 `kicked{reason}` 프레임 | 재접속 금지, 문서를 닫고 이유를 보여 줌 |

그 밖의 끊김(네트워크·배포)은 백오프 재접속 뒤 `hello{since: 마지막 seq}` 로 따라잡고, 보내지 못한 op 를 같은 `client_op_id` 로 다시 보낸다.

**`kicked` 의 원인**(api.md 5.24): 스스로 나감 `left`, owner·admin 의 강퇴 `kicked`, 차단 `banned`, 방 삭제 `space_deleted`, 문서 삭제 `doc_deleted`. 정리 batch 가 커밋된 뒤 보낸다.

- **프레임 상한이 방향별인 까닭**: 클라→서버는 편집(op·presence)뿐이라 64KB 로 묶는다(설계서 C1.4). 서버→클라의 `hello{snapshot}`·복원 `reset` 은 문서 전체를 프레임 하나에 싣는다 — 정규형 스냅숏은 `maxDocBytes` 이하이고(5장), `hello` 의 ops 묶음도 그보다 커지면 snapshot 으로 바꾸므로(3.2) 서버→클라 프레임은 위 상한을 넘지 않는다. 나눠 보내는 형식은 없다. 클라이언트의 읽기 한도는 2,162,688 바이트다.
- 클라→서버 프레임 하나에 다 들어가지 않는 `put`(필드가 많은 객체)은 클라이언트가 `put`(일부 필드) + `set` 여러 프레임으로 나눠 보낸다.

### 4.1 연결을 닫거나 고치는 서버 경로 (CollabHub RPC)

| RPC | 부르는 곳(정리 batch 커밋 뒤, `rt.waitUntil`) | 하는 일 |
|---|---|---|
| `kick(userId, reason)` | 탈퇴·강퇴·차단(그 방의 삭제 안 된 문서마다) | 그 사용자 소켓에 `kicked{reason}` 뒤 4100 |
| `closeAll(reason)` | 방 삭제(`space_deleted`, 그 방 문서마다)·문서 삭제(`doc_deleted`) | 모든 소켓에 `kicked{reason}` 뒤 4100 |
| `revokeSession(userId, sessionIds \| "all", code)` | `services/users.ts` `revokeSessions`(로그아웃·세션 끊기·비밀번호 변경 = 4001, 계정 정지 = 4003) — 그 사용자가 멤버인 방들의 삭제 안 된 문서마다 | 첨부 `sessionId` 가 맞는 소켓(`"all"` = 그 사용자 전부)을 `code` 로 닫는다. `kicked` 프레임은 보내지 않는다 |
| `setRole(userId, role)` | 방 역할 바뀜(멤버 역할 변경·소유권 이전 — `space.member_updated` 를 내는 경로, 그 방 문서마다) | 그 사용자 소켓들의 첨부 `role` 을 고친다(`creator` 는 그대로 둔다). 다음 op 부터 3.3 잠금 검사가 새 값을 본다 |
| `setLocked(locked)`·`init(snapshot)`·`head()` | 문서 REST(api.md 5.24) | 잠금 표시·처음 상태·지금 순번 |

- 세션 만료(폐기 없이 시간이 지남)는 열린 소켓을 닫지 않는다 — 세션은 쓰는 동안 늘어나고(`last_used_at`), 다음 연결의 `requireAuth` 가 막는다.
- RPC 가 실패해도 D1 이 진실이다: 다음 연결은 Worker 의 세션·멤버 확인에서 막힌다. 실패는 기록만 한다(`revokeSessions` 의 UserHub 호출과 같음).

---

## 5. 문서 종류 등록부

`server/src/lib/collab-doctypes.ts` 가 P3·P4 가 붙는 유일한 자리다. 종류마다 파일 하나(`server/src/lib/collab/arch.ts`, `apicol.ts`)가 `export const docType: DocTypeSpec` 를 내고 등록부가 모은다.

```ts
export interface DocStateView {
  get(id: string): { type: string; fields: Record<string, Json>; deleted: boolean } | undefined;
  count(type: string): number;       // 삭제 안 된 것
  bytes(): number;                   // 정규형 스냅숏의 JSON 직렬화 바이트
}
export interface RejectReason { code: "invalid" | "too_large" | "limit_exceeded"; detail?: string }
export interface DocTypeSpec {
  type: "arch" | "apicol";
  maxDocBytes: number;
  maxObjects: Record<string, number>; // 객체 type 이름 → 상한
  maxFieldBytes: number;              // 필드 값 하나의 JSON 직렬화 바이트
  validateOp(op: Op, state: DocStateView): RejectReason | null;
}
export const DOC_TYPES: Record<"arch" | "apicol", DocTypeSpec>;
```

| 종류 | `maxDocBytes` | `maxObjects` | `maxFieldBytes` | 내용 규칙 |
|---|---|---|---|---|
| `arch` | 1,048,576 | `node` 300, `edge` 1000 | 16,384 | 6장(P3) |
| `apicol` | 2,097,152 | `request` 1000 | 61,440 | 7장(P4) |

- `maxFieldBytes` 는 클라→서버 프레임 상한(65,536)보다 작아야 한다 — 필드 하나를 `set` 프레임 하나(봉투 약 4KB 여유)에 실을 수 있어야 하므로. `apicol` 의 61,440 은 그래서 정한 값이다(설계서 C3 의 "요청 본문 64KB" 와의 차이는 P4·오너 확인).
- P0 은 두 파일을 자리표시(`validateOp: () => null`)로 만든다. `maxObjects` 에 없는 객체 type 은 개수 한도가 없고, 허용 여부는 `validateOp` 가 정한다.
- `POST /v1/spaces/{space_id}/docs` 의 `snapshot`·버전 복원도 같은 한도를 지난다(api.md 5.24).
- 서버는 필드 **내용**을 탐지하지 않는다 — 모양·크기만 본다(종류별 절이 정한 `detail` 이외).

---

## 6. `arch` (P3)

(P3 가 객체 종류·필드·한도·거절 `detail` 을 더한다.)

## 7. `apicol` (P4)

(P4 가 객체 종류·필드·한도·거절 `detail` 을 더한다.)

---

## 8. 공유 벡터 `docs/contract/collab-vectors.json`

서버(`server/test/unit/collab-reducer.test.ts`)와 Go(`internal/collab` `TestDocVectors`, `collabtest` `TestHubVectors`)가 같은 파일을 읽는다.

```ts
type Vector = {
  name: string;                      // 유일, 시험 이름
  start: Snapshot;                   // 정규형
  batches: (
    | { seq: number; ops: Op[] }                 // 이 순번으로 적용
    | { ops: Op[]; reject: "invalid" }           // 공통 검사(3.3 의 1단계)에서 거절 — 순번·상태 변화 없음
  )[];
  want: Snapshot;                    // 정규형, want.seq = 적용된 마지막 순번(거절은 세지 않음)
}[];
```

- 벡터는 종류별 한도·`validateOp` 를 쓰지 않는다(그것은 종류별 시험). `reject` 배치는 공통 모양 검사(이름 규칙 1.2 포함)만 본다. 순번이 있는 배치는 서버가 적용하는 것이라 `reset` 을 담을 수 있다.
- 벡터 파일은 JSON 텍스트 그대로 읽는다(`"__proto__"` 키가 들어 있다). 시험 코드도 1.2 의 구현 규칙을 따른다.
- 비교는 정규형(1.1)끼리, JSON 값은 의미가 같으면 같다(키 순서 무관).
- 새 규칙을 더하면 벡터도 더한다. 기존 벡터는 고치지 않는다.
