# masil WebSocket 실시간 계약 v1

- 상태: 확정 계약 v1 (2026-11-05). 상위 출처: 설계서 `docs/superpowers/specs/2026-11-05-masil-design.md`.
- 짝 문서: `api.md`(REST·객체 스키마), `data-model.md`, `server-architecture.md`(DO 구현).
- 객체 타입(`Message`, `Channel`, `Post`, `Alert` …)은 **`api.md` 2장의 정의를 그대로** 쓴다. 이벤트 안의 객체는 REST 응답과 같은 직렬화 함수로 만든다.

---

## 0. 개요

- **클라이언트(프로세스)당 소켓 1개**로 내가 속한 모든 방·채널·DM·알림 이벤트를 받는다(설계서 5.2). 채널별 구독은 없다.
- 서버 쪽 소켓 종착점은 **사용자별 Durable Object `UserHub`**(이름 = user_id). 한 사용자의 여러 연결(TUI, 데몬, 다른 기기)이 같은 UserHub 에 붙는다. Hibernation API 를 써서 유휴 중 과금되지 않는다.
- 이벤트 종류:
  - **기록 이벤트(logged)**: UserHub 가 사용자별 일련번호 `seq` 를 매기고 로그에 저장한다. 재접속 시 놓친 것을 다시 받을 수 있다.
  - **휘발 이벤트(ephemeral)**: `seq: null`. 입력 중·접속 상태·피드 카운터처럼 놓쳐도 REST 로 다시 맞추면 되는 것. 다시 보내 주지 않는다.
- 놓친 메시지 채우기의 최종 수단은 REST 재동기화(열린 채널·스레드 창을 통째로 다시 받기, 5.3)다. WebSocket 재전송은 짧은 끊김(최대 2분 + 로그 보존 범위)을 메우는 용도다.

## 1. 연결

### 1.1 요청

```
GET /v1/ws?mode=full&device_id=01JC8Z3K4M5N6P7Q8R9S0TVWXY HTTP/1.1
Host: masil.<account>.workers.dev
Upgrade: websocket
Connection: Upgrade
Authorization: Bearer msl_...
User-Agent: masil/0.1.0 (darwin; arm64)
```

- `mode`: `full`(TUI, 기본) | `notify`(데몬 — 알림 전용, 7장).
- `device_id`: 기기별 ULID. 클라이언트가 처음 실행 시 만들어 `~/.config/masil/device_id` 에 저장하고 **TUI 와 데몬이 같은 값을 쓴다**. 필수, 형식 오류 → `400 invalid_request`.
- 토큰은 반드시 `Authorization` 헤더로(쿼리 금지). Go 클라이언트는 `websocket.DialOptions.HTTPHeader` 로 넣는다.
- 업그레이드 전 실패는 일반 HTTP 오류 봉투: `401 unauthorized`, `403 account_suspended`, `400 invalid_request`, `426 upgrade_required`, `429 rate_limited`(`RL_WS`).
- 사용자당 동시 연결 최대 **10**개. 넘으면 가장 오래된 연결을 `4009` 로 닫고 새 연결을 받는다.

### 1.2 종료 코드

| 코드 | 이름 | 의미 | 클라이언트 동작 |
|---|---|---|---|
| 1000 | normal | 정상 종료 | 재접속 안 함(사용자가 끈 경우) |
| 1001 | going_away | 서버 재배포·DO 재시작 | 즉시 재접속(백오프) |
| 1009 | too_big | 프레임 8KB 초과 | 버그. 백오프 재접속 |
| 1011 | internal_error | 서버 오류 | 백오프 재접속 |
| 4000 | protocol_error | 잘못된 프레임, `hello` 후 10초 안에 `resume` 없음 | 백오프 재접속 |
| 4001 | unauthorized | 세션 폐기·만료(로그아웃, 비밀번호 변경) | **재접속 금지**, 토큰 삭제 후 로그인 화면 |
| 4002 | heartbeat_timeout | 서버가 ping 을 120초 넘게 못 받음 | 즉시 재접속 |
| 4003 | account_suspended | 계정 정지 | **재접속 금지**, 정지 안내 |
| 4008 | rate_limited | 클라이언트 프레임 과다 | 10초 후 재접속 |
| 4009 | replaced | 연결 수 초과로 밀려남 | 재접속하되 1분 안에 두 번 받으면 멈추고 상태줄에 표시 |
| 4010 | upgrade_required | 클라이언트 버전 낮음 | 재접속 금지, 업데이트 안내 |

### 1.3 재접속 백오프

- 지연 = `random(0, min(30s, 0.5s × 2^n))` (full jitter), `n` = 연속 실패 횟수. `ready` 후 60초 유지되면 `n=0`.
- 재접속 시에는 항상 마지막 `epoch`·`seq` 로 `resume` 한다(3장).

## 2. 프레임 형식

- **텍스트 프레임만**, UTF-8 JSON 하나. 바이너리 프레임 → `4000`. 클라이언트 프레임 최대 8KB.

서버 → 클라이언트:
```json
{ "type": "message.created", "seq": 1042, "ts": "2026-11-05T02:03:04.567Z", "data": { "message": { "...": "..." } } }
```
- `type`: 이벤트 이름(4장). `seq`: 기록 이벤트면 정수(사용자별 단조 증가, 1부터), 휘발이면 `null`. `ts`: 서버가 이벤트를 만든 시각. `data`: 이벤트별 객체.
- 예외: `pong` 은 정확히 `{"type":"pong"}` (자동 응답, 3.3).

클라이언트 → 서버:
```json
{ "type": "typing", "ref": "c-17", "data": { "channel_id": "01JB...", "thread_root_id": null } }
```
- `ref?`: 클라이언트가 붙이는 상관 ID(1..32자). 붙이면 서버가 `reply` 로 결과를 알려 준다. 안 붙이면 성공 시 응답 없음(실패 시에도 무시).
- 예외: `ping` 은 정확히 `{"type":"ping"}` (3.3).

`reply` 이벤트(휘발):
```json
{ "type": "reply", "seq": null, "ts": "...", "data": { "ref": "c-17", "ok": true, "error": null } }
{ "type": "reply", "seq": null, "ts": "...", "data": { "ref": "c-18", "ok": false, "error": { "code": "not_found", "message": "channel not found" } } }
```
`error.code`: `invalid_frame`, `unknown_type`, `not_found`, `not_allowed_in_mode`, `rate_limited`, `limit_exceeded`.

클라이언트 프레임 속도 제한: 연결당 10초에 30프레임(`ping` 제외). 넘으면 `reply{rate_limited}`, 계속 넘으면(10초에 60) `4008` 종료.

## 3. 핸드셰이크·재개·하트비트

### 3.1 순서

```
client                                   server (UserHub)
  | --- GET /v1/ws (Upgrade, Bearer) ---> |
  | <-------------- 101 ----------------- |
  | <-- hello {epoch, seq, ...} --------- |   (즉시)
  | --- resume {epoch, last_seq} -------> |   (10초 안에, 정확히 1회)
  | <-- (재전송 이벤트들, seq 오름차순) -- |
  | <-- ready {resumed, replayed, ...} -- |
  | <-- presence.snapshot -------------- |   (full 모드만)
  | <== 실시간 이벤트 ===================> |
```

`ready` 전에 생긴 이벤트도 로그에 있으므로 재전송에 포함된다. 클라이언트는 `ready` 를 받기 전까지 UI 에 "동기화 중" 을 표시해도 된다.

### 3.2 `hello` (서버, 휘발)

```json
{
  "type": "hello", "seq": null, "ts": "2026-11-05T02:03:04.567Z",
  "data": {
    "connection_id": "01JC9...",
    "session_id": "01JA7...",
    "user_id": "01JA1...",
    "mode": "full",
    "device_id": "01JC8Z...",
    "epoch": "01JC2...",
    "seq": 1041,
    "oldest_seq": 12,
    "heartbeat_interval_ms": 25000,
    "heartbeat_timeout_ms": 120000,
    "local_tui_connected": false,
    "server_time": "2026-11-05T02:03:04.567Z",
    "limits": { "max_frame_bytes": 8192, "watch_topics_max": 50, "typing_min_interval_ms": 3000 }
  }
}
```
- `epoch`: UserHub 의 로그 세대 ID(ULID). **모드별로 따로 관리**한다(full 연결은 full epoch, notify 연결은 notify epoch 를 받는다. 둘 다 같은 `seq`·로그를 가리킨다). 그 모드의 로그 연속성이 끊길 수 있는 상황(6.2)마다 새로 바뀐다.
- `seq`: 현재 마지막 seq(아직 없으면 0). `oldest_seq`: 로그에 남은 가장 오래된 seq(비었으면 `seq+1`).
- `local_tui_connected`: 같은 `device_id` 의 full 모드 연결이 (이 연결 제외) 하나라도 있으면 true. 데몬용(7장).

### 3.3 `resume` (클라이언트)

```json
{ "type": "resume", "data": { "epoch": "01JC2...", "last_seq": 1030 } }
{ "type": "resume", "data": { "epoch": null, "last_seq": null } }
{ "type": "resume", "data": { "use_device_ack": true } }
```

서버 처리:
1. `epoch=null` (처음 연결): 재전송 기준 = `hello.seq`. 결과 `resumed:false, resync_required:false`. 클라이언트는 어차피 REST 로 처음부터 불러온다(`ready` 를 받은 **뒤에** 시작한다 — 6.2).
2. `epoch` 가 이 연결 모드의 현재 epoch 와 같고 `last_seq >= oldest_seq - 1`: `last_seq` 이후 전부 재전송. `resumed:true`.
3. `use_device_ack:true`: 이 (`device_id`, `mode`) 가 마지막으로 `ack` 한 seq 를 `last_seq` 로 써서 2번 규칙 적용(기록 없거나 그 모드의 epoch 바뀜 → 4번). 같은 기기의 TUI 와 데몬은 `device_id` 를 공유하지만 ack 기록은 모드별로 따로라 서로 덮어쓰지 않는다. 데몬이 디스크 상태 없이 재시작할 때 쓴다.
4. 그 밖(epoch 불일치, 로그에서 밀려남): 재전송 기준 = `hello.seq`, `resumed:false, resync_required:true`. 클라이언트는 **5.3 재동기화**를 한다.

재전송 이벤트는 원래 `seq`·`ts` 그대로 보낸다. notify 모드 연결에는 7장의 허용 타입만 재전송한다.

**재전송 때 거르는 알림 [대기 목록 4-a]**: 로그는 소급해서 지우지 않지만, 재전송은 **`resume` 을 처리하는 그 시점**에 알림함이 숨기는 행위자(`api.md` 5.18 — 내가 차단한 사람, 지금 정지 중인 사람(3.9, `suspended_until <= 지금` 이면 정지 아님))가 든 알림 프레임을 다시 보내지 않는다.
- `notification.created`(`notification.actor`)·`alert`(`alert.actor`)의 행위자가 그 조건에 걸리면 그 프레임을 건너뛴다(`replayed` 에 세지 않는다). 행위자가 없는(`null`) 프레임은 그대로.
- `focus.ended` 는 `summary.items` 에서 그런 행위자의 alert 만 빼고 보낸다(`counts` 는 기록 당시 값 그대로 — 누구인지 드러내지 않는다).
- 그 밖의 프레임(`message.created` 의 작성자 요약, `friend.request_received` 등)은 그대로 재전송되고, 정지·차단은 뒤따르는 `user.updated`·`block.updated`·`*_removed` 이벤트로 반영된다(`api.md` 3.9).
- 서버는 재전송 범위의 행위자를 D1 로 한 번 확인하는 동안 그 UserHub 의 다른 일(발행·알람·다른 프레임)을 막고 확인과 재전송을 한 덩어리로 처리한다 — 이벤트는 재전송 범위 또는 `ready` 뒤 실시간 중 정확히 한쪽으로 간다. 이 확인이 실패하면 거르지 않은 재전송 대신 `1011` 로 닫는다(클라이언트는 1.3 백오프로 재접속).

`ready` (서버, 휘발):
```json
{ "type": "ready", "seq": null, "ts": "...", "data": { "resumed": true, "resync_required": false, "replayed": 11, "seq": 1041, "epoch": "01JC2..." } }
```

### 3.4 하트비트

- 클라이언트는 `heartbeat_interval_ms`(25초)마다 **정확히 이 15바이트 문자열** `{"type":"ping"}` 을 텍스트 프레임으로 보낸다(공백·필드 추가 금지). 서버는 DO 의 `setWebSocketAutoResponse` 로 정확히 `{"type":"pong"}` 을 돌려준다 — DO 를 깨우지 않는다(요청·응답 문자열 각 2,048자 한도 안).
- 클라이언트: ping 후 10초 안에 `pong`(또는 어떤 프레임이든) 이 없으면 연결이 죽은 것으로 보고 닫은 뒤 재접속.
- 서버: UserHub 알람이 `getWebSocketAutoResponseTimestamp(ws)` 를 보고 마지막 ping 이 `heartbeat_timeout_ms`(120초) 보다 오래된 연결을 `4002` 로 닫는다. 알람 간격은 full 소켓이 하나라도 있으면 **60초**, notify 소켓만 있으면 15분이다.
  - 따라서 죽은 full 연결(잠든 노트북, 끊긴 네트워크처럼 Close 가 오지 않는 경우)은 **최대 약 180초**(120초 + 알람 60초) 동안 연결된 것으로 보일 수 있다. 그동안 접속 상태·`local_tui_connected` 가 그 값을 유지한다. 프로세스가 죽은 경우는 OS 가 TCP 를 닫으므로 보통 즉시 `webSocketClose` 가 온다. 데몬 쪽 보완은 7장.
  - 죽은 notify 연결은 최대 약 17분 늦게 정리된다. notify 연결은 접속 상태에 영향이 없고, 그동안 쌓인 alert 는 로그에 남아 재개 때 받으므로 문제없다.
- WebSocket 프로토콜 수준 ping 제어 프레임은 런타임이 처리하므로 써도 무방하지만, 서버 생존 판정에는 위 텍스트 ping 만 쓰인다.

### 3.5 `ack` (클라이언트, 선택)

```json
{ "type": "ack", "data": { "seq": 1041 } }
```
- 처리 완료한 마지막 seq. UserHub 가 (`device_id`, `mode`) 별로 그 모드의 epoch 와 함께 저장(3.3 의 `use_device_ack` 용). 5초에 1번 이하로 보내면 충분하다. 응답 없음.

## 4. 서버 → 클라이언트 이벤트

표기: **청중**(누가 받는가), **기록/휘발**, **모드**(full=F, notify=N). 청중 이름은 6.1 참조. 자기 행동의 결과도 내 다른 연결과 **보낸 연결 자신**에 똑같이 온다 — 클라이언트는 ID(메시지는 `client_nonce` 도)로 중복을 제거한다.

### 4.1 연결 제어

| type | data | 비고 |
|---|---|---|
| `hello` | 3.2 | 휘발, F/N |
| `ready` | 3.3 | 휘발, F/N |
| `reply` | 2장 | 휘발, F/N |
| `pong` | (없음, 문자열 고정) | 자동 응답 |
| `session.revoked` | `{ session_id: ID, reason: "logout"\|"revoked"\|"password_changed"\|"suspended" }` | 휘발, F/N. 직후 `4001`/`4003` 종료 |

### 4.2 메시지 (청중: `channel:<channel_id>`, 기록, F)

| type | data |
|---|---|
| `message.created` | `{ message: Message }` — 최상위·스레드 답글 모두(`message.thread_root_id` 로 구분) |
| `message.updated` | `{ message: Message }` — 수정, 투표 첨부 변경 등 전체 교체 |
| `message.deleted` | `{ channel_id: ID, message_id: ID, thread_root_id: ID \| null, tombstone: boolean }` — `tombstone=true` 면 "삭제된 메시지" 자리표시로 바꾸고, false 면 목록에서 제거 |
| `thread.updated` | `{ channel_id: ID, root_id: ID, thread: ThreadSummary }` — 답글 추가·삭제 시 루트의 "답글 N개" 갱신 |
| `reaction.added` | `{ channel_id: ID, message_id: ID, thread_root_id: ID \| null, emoji: string, user_id: ID, count: number }` |
| `reaction.removed` | 같은 형태(`count` 는 제거 후 값, 0 이면 그 이모지 제거) |
| `message.pinned` | `{ channel_id: ID, message_id: ID, pinned_by: ID }` |
| `message.unpinned` | `{ channel_id: ID, message_id: ID, unpinned_by: ID }` |
| `poll.updated` | `{ poll: Poll }` — 메시지 투표. `poll.my_votes` 는 **받는 사람과 무관하게 빈 배열**로 오며, 클라이언트는 자기 투표 상태를 유지한다(내 투표 변경은 REST 응답으로 반영) |

예:
```json
{ "type": "message.created", "seq": 1042, "ts": "2026-11-05T02:05:00.120Z",
  "data": { "message": { "id": "01JC9Z...", "channel_id": "01JB...", "author": { "id": "01JA...", "handle": "sam", "display_name": "sam", "avatar_url": null, "status": null, "is_private": false, "suspended": false },
    "kind": "text", "body": "@minji 점심 먹고 ㄱㄱ", "mentions": [ { "user_id": "01JA2...", "handle": "minji" } ], "mention_here": false,
    "thread_root_id": null, "thread": null, "reactions": [], "pinned": false, "poll": null, "roll": null, "system": null,
    "client_nonce": "01JC9X...", "created_at": "2026-11-05T02:05:00.100Z", "edited_at": null, "deleted": false } } }
```

클라이언트 규칙:
- 메시지는 **ID 순으로 끼워 넣는다**(도착 순서 아님).
- 불러오지 않은 구간(보고 있는 범위보다 오래된 것)의 `updated/deleted/reaction` 은 무시해도 된다.
- 모르는 메시지에 대한 `message.updated`·`reaction.*` 이 보고 있는 범위 안이면 `GET /v1/messages/{id}` 로 받아 넣는다.
- 안 읽음 수: 내가 보낸 것이 아니고, `thread_root_id == null`, `kind != "system"` 이고, 그 채널을 지금 보고 있지 않으면 `my.unread_count+1`.
- 멘션 수(`my.mention_count`)는 서버와 똑같이 **최상위 메시지만** 센다(`api.md` 2장 `ChannelMyState`). 스레드 답글 속 멘션은 알림함(`message_mention`)·`alert` 로만 알리고 채널 배지에는 넣지 않는다. 증가 규칙(그 채널을 지금 보고 있지 않을 때, 한 메시지는 ID 기준 한 번만):
  - `message.created` 이고 `thread_root_id == null` 이고 `mentions` 에 내가 있으면 `+1`.
  - `@here` 확장(`mention_here:true` 인데 `mentions` 에 내가 없음)은 클라이언트가 추측하지 않는다. 같은 메시지에 대한 `notification.created`(`type:"message_mention"`, `context.message_id` 일치, `context.thread_root_id == null`)를 받았을 때만 `+1`. 이 알림은 서버가 나를 `@here` 대상으로 확장했을 때만 온다(`api.md` 2.1).
- 서버 값과의 최종 일치는 `read_state.updated` 와 `GET /v1/me/unreads`.
- 같은 작성자의 `message.created` 를 받으면 그 채널(스레드)의 입력 중 표시를 지운다.

### 4.3 입력 중·읽음

| type | 청중 | 기록 | data |
|---|---|---|---|
| `typing` | `channel:<id>` (보낸 사람 제외) | 휘발, F | `{ channel_id: ID, thread_root_id: ID \| null, user_id: ID, expires_in_ms: 6000 }` — 6초 동안 "입력 중…" 표시, 다시 오면 연장 |
| `read_state.updated` | `users:[나]` | 기록, F | `{ state: UnreadState }` — 다른 기기/연결에서 읽음 처리한 결과 |

### 4.4 채널·DM·방

| type | 청중 | data |
|---|---|---|
| `channel.created` | 새 채널을 볼 수 있는 사람(공개: `space:<id>`, 비공개·DM: 채널 멤버) | `{ channel: Channel }` |
| `channel.updated` | `channel:<id>` | `{ channel: Channel }` |
| `channel.deleted` | 이전 청중 / 빠진 사람 | `{ channel_id: ID, space_id: ID \| null, reason: "deleted" \| "removed" }` |
| `channel.member_added` | `channel:<id>` | `{ channel_id: ID, user: UserSummary }` |
| `channel.member_removed` | `channel:<id>` | `{ channel_id: ID, user_id: ID }` |
| `channel.prefs_updated` | `users:[나]` | `{ channel_id: ID, my: ChannelMyState }` |
| `space.joined` | `users:[나]` | `{ space: SpaceDetail }` |
| `space.updated` | `space:<id>` | `{ space: Space }` |
| `space.deleted` | `space:<id>` | `{ space_id: ID }` |
| `space.removed` | `users:[빠진 사람]` | `{ space_id: ID, reason: "left" \| "kicked" \| "banned" }` |
| `space.member_joined` | `space:<id>` | `{ space_id: ID, member: SpaceMember }` |
| `space.member_left` | `space:<id>` | `{ space_id: ID, user_id: ID, reason: "left" \| "kicked" \| "banned" }` |
| `space.member_updated` | `space:<id>` | `{ space_id: ID, member: SpaceMember }` |
| `space.invite_received` | `users:[초대받은 사람]` | `{ invite: DirectInvite }` |
| `space.invite_removed` | `users:[초대받은 사람, 초대한 사람]` | `{ invite_id: ID, reason: "accepted" \| "declined" \| "canceled" }` |

모두 기록, F.

**수신자별로 다른 필드 처리 규칙**: 팬아웃은 한 번 만든 payload 를 여러 사람에게 그대로 보낸다. 따라서 이벤트 안의 `Channel.my`, `Channel.read_only`, `Channel.read_only_reason`, `Space.my_role`, `Poll.my_votes`, `Message.reactions[].me`, `Post.viewer` 는 **기본값**(`my` = `{notify: 기본, muted:false, last_read_message_id:null, unread_count:0, mention_count:0}`, `read_only:false`, `my_role:"member"`, `my_votes:[]`, `reactions[].me:false`, `viewer:{liked:false,reposted:false}`)으로 채워져 온다. 클라이언트는:
- `*.created`/`space.joined` 에서는 그대로 쓰되 `my_role` 은 `space.joined`(나에게만 가므로 정확함)과 `space.member_updated` 로 맞춘다.
- `*.updated` 에서는 이 필드들을 **덮어쓰지 않고** 로컬 값을 유지한다. 특히 `message.updated` 는 메시지 전체 교체지만 `reactions` 는 이모지별로 `count` 만 이벤트 값을 쓰고 `me` 는 로컬 값을 유지한다(로컬에 없던 이모지는 `me:false`). 내 리액션 상태는 내 REST 응답과 `reaction.added/removed`(`user_id` 가 나) 로만 바꾼다.
- `Post.quote`(인용 글의 `available`/`not_visible`)는 보는 사람에 따라 다를 수 있지만, SNS 이벤트는 4.8 의 가시성 필터를 통과한 수신자에게만 가므로 이벤트 안의 값이 그 수신자에게도 맞다.
- 예외: `space.joined`, `channel.prefs_updated`, `read_state.updated` 처럼 `users:[나]` 청중 이벤트는 나 기준 정확한 값이다.

### 4.5 친구·차단·사용자

| type | 청중 | data |
|---|---|---|
| `friend.request_received` | `users:[받는 사람]` | `{ request: FriendRequest }` |
| `friend.request_accepted` | `users:[보낸 사람]` | `{ request: FriendRequest }` |
| `friend.request_removed` | `users:[보낸 사람, 받는 사람]` | `{ request_id: ID, reason: "declined" \| "canceled" }` |
| `friend.added` | 각자에게 | `{ friend: Friend }` (상대 정보) |
| `friend.removed` | 각자에게(차단으로 인한 해제는 차단한 사람에게만) | `{ user_id: ID }` |
| `block.updated` | `users:[나]` | `{ user_id: ID, blocked: boolean }` |
| `user.updated` | `presenceWatchers:<user_id>` + 나 | `{ user: UserSummary }` — 이름·아바타·상태 메시지·비공개 여부 변경, 운영자의 정지·정지 해제(`api.md` 3.9, 이때는 청중이 `summaryWatchers:<user_id>` — 같은 방 멤버 포함, "나" 없음) |
| `settings.updated` | `users:[나]` | `{ settings: Settings }` |

모두 기록, F.

### 4.6 접속 상태·집중 모드

| type | 청중 | 기록 | 모드 | data |
|---|---|---|---|---|
| `presence.snapshot` | 이 연결만 | 휘발 | F | `{ items: [ { user_id: ID, status: PresenceStatus, last_seen_at: Timestamp \| null } ] }` — `ready` 직후, 친구 + DM/그룹 DM 상대 전원. 정지 중인 계정은 `status: "offline"`, `last_seen_at: null`(`api.md` 3.9) |
| `presence.updated` | `presenceWatchers:<user_id>` | 휘발 | F | `{ user_id: ID, status: PresenceStatus, last_seen_at: Timestamp \| null }` — 정지 중인 계정은 `status: "offline"`, `last_seen_at: null`(`api.md` 3.9; 정지 순간의 연결 종료 포함). 운영자의 정지·정지 해제 때는 상태가 바뀌지 않아도(수동 `dnd`·집중 모드는 소켓 0개에서도 유지, 6.3) 한 번 나간다: 정지는 그 가린 항목, 해제는 실제 항목(`GET /v1/presence` 와 같은 값). 온라인이던 계정은 정지 때 같은 offline 항목을 두 번 받을 수 있다 |
| `presence.self` | `users:[나]` | 휘발 | F | `{ presence: PresenceState }` — 내 상태가 바뀔 때(다른 기기 반영) |
| `focus.started` | `users:[나]` | 기록 | F/N | `{ focus: Focus }` |
| `focus.ended` | `users:[나]` | 기록 | F/N | `{ summary: FocusSummary }` — 종료 시점에 알림함이 숨기는 행위자(`api.md` 5.18: 내가 차단, 지금 정지 중)의 alert 는 `items`·`counts` 에서 빠진다 [대기 목록 4-a]. 그 확인(D1)이 실패하면 `counts` 는 전부 세고 `items` 는 행위자 없는 alert 만 |

- 방 멤버 목록처럼 친구·DM 상대가 아닌 사람의 상태는 푸시하지 않는다 → `GET /v1/presence` 를 60초마다(화면에 보일 때만).

### 4.7 알림함·데스크톱 알림

| type | 청중 | 기록 | 모드 | data |
|---|---|---|---|---|
| `notification.created` | `users:[받는 사람]` | 기록 | F/N | `{ notification: Notification, unread_count: number }` |
| `notifications.read` | `users:[나]` | 기록 | F/N | `{ ids: ID[] \| null, all: boolean, before: ID \| null, unread_count: number }` |
| `alert` | `users:[받는 사람]` | 기록 | F/N | `{ alert: Alert }` |

`alert` 는 "OS 데스크톱 알림을 띄울 후보"다. 서버가 설정·채널 환경설정·차단·집중 모드를 이미 반영해서 만든다.

| alert.kind | 생성 조건 | 설정 스위치 |
|---|---|---|
| `dm` | 1:1/그룹 DM 에 남이 보낸 메시지(스레드 답글 포함), 채널 `notify != "none"` 이고 `muted=false` | `notify.dm` |
| `mention` | 채널 메시지·스레드 답글에서 나를 멘션(`@here` 확장 포함 — 해석 규칙은 `api.md` 2.1: 그 채널을 읽을 수 있고 작성자와 차단 관계가 아닌 사람만), 글에서 나를 멘션 | `notify.mention` |
| `thread_reply` | 내가 루트 작성자/참여자인 스레드에 새 답글(멘션이 같이 있으면 `mention` 하나만) | `notify.thread_reply` |
| `invite` | 방 직접 초대 받음 | `notify.invite` |
| `friend_request` | 친구 요청 받음 | `notify.friend_request` |
| `channel_message` | `notify:"all"` 로 설정한 text 채널의 새 최상위 메시지 | 채널 prefs |
| `social` | 좋아요·리포스트·팔로우·팔로우 요청·글 답글·인용·방명록 | `notify.social` |
| `focus_summary` | 집중 모드 종료 시 1회. `title` 예: "집중 모드 끝 — 멘션 3, DM 2" | 항상 |

- 한 메시지가 한 사람에게 여러 조건을 만족하면 우선순위 `dm > mention > thread_reply > channel_message` 중 하나만 만든다.
- `muted=true` 채널, `notify:"none"` 채널, 차단한 사용자가 행위자인 경우 alert 를 만들지 않는다.
- 집중 모드 중: `suppressed:true, suppressed_reason:"focus"`(요약에 누적). 수동 `dnd` 중: `suppressed:true, suppressed_reason:"dnd"`(누적 안 함). suppressed alert 는 OS 알림을 띄우지 않지만 TUI 는 알림 패널에 조용히 보일 수 있다.
- `alert.id` 는 모든 연결·기기에 같은 값. 같은 `id` 는 한 번만 띄운다(재전송 대비).

예:
```json
{ "type": "alert", "seq": 1043, "ts": "2026-11-05T02:05:00.140Z",
  "data": { "alert": { "id": "01JC9ZA...", "kind": "mention", "title": "sam · 노바 개발방 #개발", "body": "@minji 점심 먹고 ㄱㄱ",
    "actor": { "id": "01JA...", "handle": "sam", "display_name": "sam", "avatar_url": null, "status": null, "is_private": false, "suspended": false },
    "target": { "type": "message", "id": "01JC9Z...", "channel_id": "01JB...", "message_id": "01JC9Z...", "thread_root_id": null, "post_id": null, "space_id": "01JB0..." },
    "created_at": "2026-11-05T02:05:00.100Z", "suppressed": false, "suppressed_reason": null } } }
```

### 4.8 SNS (피드)

| type | 청중 | 기록 | data |
|---|---|---|---|
| `timeline.item_created` | `followers:<작성자>` + 나 | 휘발 | `{ item: TimelineItem }` — 팔로잉 타임라인 맨 위에 끼움(최상위 글·인용·리포스트) |
| `post.deleted` | `topic:post:<id>` + `followers:<작성자>` | 휘발 | `{ post_id: ID }` |
| `post.stats` | `topic:post:<id>` | 휘발 | `{ post_id: ID, like_count: number, repost_count: number, reply_count: number, quote_count: number }` |
| `post.reply_created` | `topic:post:<parent_id>` | 휘발 | `{ parent_id: ID, post: Post }` |
| `poll.updated` | `topic:post:<post_id>` | 휘발 | `{ poll: Poll }` (글 투표, `my_votes` 는 빈 배열) |
| `story.created` | `followers:<작성자>` + 나 | 휘발 | `{ user_id: ID, story_id: ID, expires_at: Timestamp }` — 스토리 링 갱신 |
| `story.deleted` | `followers:<작성자>` + 나 | 휘발 | `{ user_id: ID, story_id: ID }` |

모두 F 모드만.

**가시성 필터**(서버 필수): 글 내용을 싣는 이벤트(`timeline.item_created`, `post.reply_created`)는 받는 사람 각각이 그 글을 REST 로 볼 수 있을 때만(`api.md` 3.4) 보낸다. 서버는 발행 때 "관련 작성자" 집합을 넘기고(`server-architecture.md` 4.11 `VisibilitySpec`), 수신자 R 은 다음을 모두 만족해야 받는다.
- 관련 작성자 전원과 R 사이에 어느 방향으로도 차단이 없다.
- 관련 작성자 중 비공개 계정인 사람마다 R 이 그 본인이거나 `active` 팔로워다.

관련 작성자:
- `timeline.item_created`: 글 작성자 + (인용이면) 인용된 글의 작성자 + (리포스트면) 리포스트한 사람.
- `post.reply_created`: 답글 작성자 + 부모 글 작성자.

`followers:` 청중은 원래 `active` 팔로워(리포스트한 사람/작성자와의 차단 제외)만 포함하고, 그 위에 이 필터를 한 번 더 건다. `topic:` 청중은 TopicHub 가 시청자 목록에 같은 필터를 D1 질의 1회로 건다. 이 필터를 통과한 수신자에게는 `Post.quote` 가 기본값 그대로 정확하다(인용된 글을 못 보는 사람은 이미 빠졌으므로). `post.deleted`·`post.stats`·`story.*` 는 ID·숫자만 실으므로 필터 없이 청중대로 보낸다.

### 4.9 데몬 조율

| type | 청중 | 기록 | 모드 | data |
|---|---|---|---|---|
| `device.tui_state` | 같은 `device_id` 의 notify 연결 | 휘발 | N | `{ device_id: string, tui_connected: boolean }` — 그 기기의 full 연결 수가 0↔1 이상으로 바뀔 때 |

## 5. 클라이언트 → 서버 프레임

| type | data | 모드 | 설명 |
|---|---|---|---|
| `ping` | — (문자열 고정 `{"type":"ping"}`) | F/N | 3.4 |
| `resume` | `{ epoch: string \| null, last_seq: number \| null }` 또는 `{ use_device_ack: true }` | F/N | `hello` 후 정확히 1회 |
| `ack` | `{ seq: number }` | F/N | 3.5 |
| `typing` | `{ channel_id: ID, thread_root_id: ID \| null }` | F | 입력창에 글자가 있는 동안 **3초에 1번 이하**. 서버는 연결·채널별 2초 안의 중복을 버린다. 쓰기 권한 없는 채널 → `reply{not_found}` |
| `idle` | `{ idle: boolean }` | F | TUI 가 10분간 입력이 없으면 `true`, 입력이 다시 생기면 `false`. `mode:"auto"` 일 때 상태 계산에 쓰인다(6.3) |
| `watch` | `{ topics: string[] }` | F | 지금 화면에 보이는 글의 실시간 카운터 구독. **전체 교체**. 형식 `post:<post_id>`, 최대 50개(`reply{limit_exceeded}`). 보이는 글 목록이 바뀔 때 + **120초마다 같은 목록으로 다시 보낸다**(서버 임대 300초). 빈 배열 = 전부 해제 |

notify 모드에서 `typing/idle/watch` → `reply{not_allowed_in_mode}`.

### 5.1 `watch` 예

```json
{ "type": "watch", "ref": "w-3", "data": { "topics": ["post:01JD1...", "post:01JD2..."] } }
```

### 5.2 클라이언트 상태 기억

클라이언트는 연결마다 `epoch`, 마지막 처리 `seq` 를 메모리에 둔다. 데몬은 추가로 `~/.config/masil/daemon-state.json` 에 저장해도 된다(또는 `use_device_ack`). 형식은 `client-architecture.md`.

### 5.3 재동기화 절차 (`resync_required:true` 또는 첫 실행)

`ready` 를 받은 **뒤에** 시작한다(그래야 그 뒤에 커밋된 이벤트는 소켓으로, 그 전 것은 REST 로 빠짐없이 잡힌다 — 6.2).

1. `GET /v1/me`, `GET /v1/spaces`, 각 방 `GET /v1/spaces/{id}`, `GET /v1/dms`, `GET /v1/me/unreads`, `GET /v1/notifications/unread-count`, `GET /v1/friends`, `GET /v1/friends/requests?direction=incoming`, `GET /v1/me/direct-invites`, `GET /v1/blocks`.
2. **메시지 캐시는 이어 붙이지 않고 버린다.** 놓친 구간에는 새 메시지뿐 아니라 이미 가진 메시지의 수정·삭제(묘비)·리액션·고정·스레드 요약(`thread.updated`) 변경도 있을 수 있고, 이를 골라 받을 증분 API 는 없다. 그래서:
   - 열려 있는 채널마다 지금 보이는 창을 통째로 다시 받아 **교체**한다: 맨 아래를 보고 있었으면 `GET /v1/channels/{id}/messages`(최신 페이지), 위로 스크롤해 있었으면 `?around=<화면 기준 메시지 ID>`. 그 밖의 위쪽 구간은 사용자가 스크롤할 때 `before=` 로 다시 받는다.
   - 열려 있지 않은 채널의 메모리 캐시는 지운다(다음에 열 때 새로 받음).
3. 열려 있는 스레드도 `GET /v1/messages/{root}/thread` 로 다시 받아 루트와 답글 목록을 교체한다.
4. 피드가 열려 있으면 `GET /v1/timeline/following?since=<겹침 기준 ID>`(`gap=true` 면 처음부터). 열려 있는 글 상세·답글 트리도 다시 받는다.
5. 이 동안 들어오는 실시간 이벤트는 그대로 적용한다(ID 기준 병합이라 순서 무관).

**겹쳐 받기(필수)**: 서로 다른 isolate 에서 만든 ID 는 커밋 순서와 다를 수 있다(`data-model.md` 1.1). 그래서 `after=`/`since=` 로 "이 ID 이후"를 받을 때는 가진 마지막 ID 를 그대로 쓰지 않는다. 가진 항목 가운데 시각(`created_at`/`at`)이 **(가진 마지막 항목의 시각 − 5초) 이하인 가장 새 항목의 ID** 를 기준으로 쓰고, 결과는 ID 로 중복 제거해 병합한다(ID 를 파싱하지 않고 정렬 비교만 쓴다). 스크롤 내려 받기(`after=`)·스레드 `after=`·타임라인 `since=` 모두 같다.

## 6. 팬아웃 의미론 (서버 구현 계약)

구현 세부는 `server-architecture.md` 3장. 여기서는 클라이언트가 기대해도 되는 동작을 정한다.

### 6.1 청중(Audience)

| 이름 | 받는 사람 |
|---|---|
| `users:[...]` | 지정한 사용자들 |
| `channel:<id>` | 공개 text: 그 방(삭제 안 됨) 멤버 전원 / 비공개 text: `channel_members` 중 **그 방(삭제 안 됨)의 현재 멤버**인 사람 / dm·group_dm: `channel_members` |
| `space:<id>` | 그 방 멤버 전원 |
| `followers:<user_id>` | 그 사용자의 `active` 팔로워(차단 관계 제외). SNS 이벤트는 4.8 가시성 필터를 추가로 건다 |
| `presenceWatchers:<user_id>` | 그 사용자의 친구 + 같은 dm/group_dm 멤버 |
| `summaryWatchers:<user_id>` | `presenceWatchers` + 그 사용자가 속한 방(삭제 안 됨)의 멤버 — 그 사용자의 `UserSummary` 를 들고 있을 수 있는 사람(멤버 목록·메시지 작성자). 정지·정지 해제의 `user.updated` 만 쓴다(`api.md` 3.9) |
| `topic:<topic>` | 그 토픽을 `watch` 중인 연결(TopicHub DO 가 관리). SNS 이벤트는 4.8 가시성 필터를 추가로 건다 |

모든 청중에서 **온라인 판정(6.2)을 통과한 사용자에게만** 전달한다. 기록 이벤트라도 오프라인 사용자에게는 보내지 않는다 — 다시 켜면 REST 로 채운다(설계서 3.1 "놓친 메시지가 채워짐"). 데몬(notify 연결)만 떠 있는 사용자는 7장 허용 타입(alert·알림함·집중 모드)만 받는다.

### 6.2 온라인 판정·로그 연속성

- D1 `user_presence` 에 생존 기한을 **모드별로 둘** 둔다.
  - `online_until` — **full 연결** 기준. full 연결이 생기면 `now + 10분`. full 연결이 있는 동안 알람(60초 간격)이 남은 시간이 5분 아래면 `now + 10분` 으로 연장(D1 쓰기는 약 5분에 1번). 마지막 full 연결이 끊기면 `now + 120초`(유예).
  - `notify_online_until` — **notify 연결(데몬)** 기준. notify 연결이 생기면 `now + 40분`, notify 연결이 있는 동안 알람(15분 간격)이 `now + 40분` 으로 연장. 마지막 notify 연결이 끊기면 `now + 120초`.
- 발행자의 대상 선정:
  - 7장 notify 허용 타입(`alert`, `notification.created`, `notifications.read`, `focus.started`, `focus.ended`)만으로 된 발행: `online_until > now` **또는** `notify_online_until > now`.
  - 그 밖의 모든 발행(메시지·리액션·입력 중·채널·방·친구·접속 상태·SNS 등): `online_until > now` 만. 데몬만 떠 있는 사용자의 UserHub 는 이런 이벤트로 깨우지 않는다(어차피 notify 소켓이 버린다).
- **워터마크**: UserHub 는 D1 에 쓴 두 기한을 자기 저장소(`meta.full_until`, `meta.notify_until`)에도 둔다. 연장은 D1 쓰기가 성공한 **뒤에** 반영하고, 단축은 D1 쓰기 성패와 무관하게 즉시 반영한다 — 워터마크가 D1 실제 값보다 길게 남는 일이 없게.
- **epoch 교체 규칙**(새 연결이 올 때마다, 모드 무관하게 먼저 검사):
  - `now >= full_until` 이면 full epoch 를 새로 만든다(그 사이 full 대상 이벤트가 이 사용자에게 발행되지 않았을 수 있으므로).
  - `now >= max(full_until, notify_until)` 이면 notify epoch 도 새로 만든다.
  - 이 규칙은 `webSocketClose`/`webSocketError` 가 불렸는지와 **무관하다**. 코드 배포로 소켓이 끊기거나, 응답 없는 상대에게 `close(4002)` 를 보냈거나, DO 가 퇴거되는 등 close 처리기가 돌지 않는 경로가 있어도 워터마크만 보고 판단한다.
  - 이전 epoch 로의 `resume` 은 `resync_required:true`.
- 알람은 full(또는 notify) 소켓이 0개인데 그 모드의 워터마크가 `now + 120초` 보다 길게 남아 있으면 close 처리를 놓친 것으로 보고 그 자리에서 끊김 처리(기한·워터마크를 `now + 120초` 로 단축, 접속 상태 재계산)를 한다. 소켓이 있는 동안 알람은 항상 예약돼 있으므로 강제 끊김 뒤에도 한 번은 돈다.
- 연결 중인데 D1 연장이 계속 실패해 `full_until` 이 지나 버리면 UserHub 는 full 소켓을 `1011` 로 닫는다(발행이 끊긴 채 조용히 놓치지 않게. 재접속 시 새 epoch → 재동기화).
- 새 연결의 D1 기한 쓰기는 **`hello` 를 보내기 전에 끝까지 기다린다**. 따라서 `hello` 뒤에 커밋된 이벤트는 반드시 이 사용자에게 발행되고, 그 전에 커밋된 것은 클라이언트가 `ready` 뒤에 하는 REST 조회(5.3)에 잡힌다.
- 결과: 끊긴 뒤 **120초 안에** 재접속하면 그 사이 기록 이벤트가 로그에 있고 `resume` 이 성공한다. 그보다 길면 재동기화.
- 로그 보존: 최근 **2,000건 또는 24시간** 중 작은 쪽. 넘어간 seq 로 재개하면 `resync_required:true`.

### 6.3 접속 상태 계산 (UserHub)

입력: full 모드 연결 수 `n`, 각 연결의 `idle`, 수동 `mode`, 집중 모드.
1. 집중 모드 활성 → `dnd`
2. `mode == "dnd"` → `dnd`
3. `n == 0` → `offline` (notify 모드 연결은 세지 않는다 — 데몬만 떠 있으면 오프라인)
4. `mode == "away"` → `away`
5. full 연결 모두 `idle:true` → `away`
6. 그 외 → `online`

값이 바뀔 때만 D1 `user_presence` 를 고치고 `presence.updated`(휘발)·`presence.self` 를 발행한다. `offline` 이 될 때 `last_seen_at = now`.

### 6.4 순서·중복 보장

- 한 연결 안에서 기록 이벤트는 `seq` 오름차순으로 도착한다(재전송 포함). 휘발 이벤트는 그 사이 아무 데나 낄 수 있다.
- 서로 다른 요청이 만든 이벤트 사이의 순서는 보장하지 않는다(예: 아주 빠른 전송 직후 수정). 클라이언트는 ID 기준 병합과 "모르는 메시지면 REST 조회" 규칙(4.2)으로 맞춘다.
- 같은 이벤트가 두 번 올 수 있다(재전송 경계). 기록 이벤트는 `seq`, 객체는 ID 로 멱등 처리한다.

## 7. 데몬(notify 모드)

설계서 3.3: 데몬은 알림만 맡고, TUI 가 떠 있으면 쉬어서 중복 알림이 없어야 한다.

- 데몬은 `mode=notify` 로 접속한다. 받는 이벤트는 **`hello`, `ready`, `reply`, `pong`, `session.revoked`, `alert`, `notification.created`, `notifications.read`, `focus.started`, `focus.ended`, `device.tui_state`** 뿐이다(재전송도 이 타입만).
- notify 연결은 접속 상태에 영향을 주지 않는다(6.3). 데몬만 떠 있으면 서버는 그 사용자를 메시지·채널·SNS 팬아웃 대상으로 보지 않는다(6.2 — `notify_online_until` 은 위 허용 타입에만 쓰인다).
- 같은 기기 TUI 감지(서버 제공 신호): `hello.local_tui_connected` 와 `device.tui_state`. 데몬은 `tui_connected=true` 인 동안 OS 알림을 띄우지 않는다. 로컬 락 파일 등 추가 조율은 `client-architecture.md` 재량이며, 둘 다 쓸 경우 **둘 중 하나라도 TUI 가 있다고 하면** 띄우지 않는다. 단 하나의 예외: 로컬 신호가 "그 TUI 프로세스가 끝났다"를 확실히 알려 주면(예: TUI 가 잡고 있던 파일 락이 풀림) 서버 신호보다 우선한다 — 서버가 죽은 full 연결을 최대 약 180초 늦게 알아채기 때문이다(3.4).
- 데몬은 `tui_connected:false` 를 받은 뒤 **3초** 동안 들어온 alert 는 바로 띄우지 않고 3초가 지난 시점에도 TUI 가 없을 때만 띄운다(TUI 가 곧바로 재접속하는 경우의 중복 방지).
- OS 알림을 띄우는 조건: `alert.suppressed == false` 이고 그 기기에 TUI 가 없음. `focus_summary` 는 suppressed 가 아니므로 집중 종료 시 한 번 뜬다.
- **기기 안 중복 방지(TUI·데몬 공통, 필수)**: OS 알림을 띄운 `alert.id` 를 `~/.config/masil/` 아래 **공유 기록**(최근 500개, 파일 형식·잠금은 `client-architecture.md`)에 남기고, 띄우기 전에 그 기록을 확인해 이미 있으면 띄우지 않는다. TUI 가 재접속 중일 때 데몬이 띄운 alert 를 TUI 가 `resume` 재전송으로 다시 받는 경우, 데몬이 재시작해 TUI 가 이미 띄운 alert 를 재전송받는 경우 모두 이 기록으로 막는다. 메모리의 중복 집합만으로는 부족하다(프로세스마다 따로이고 재시작 때 사라진다).
- 재전송된 alert(같은 연결에서 `ready` 전에 받은 것)가 10분보다 오래됐으면 TUI·데몬 모두 OS 알림을 띄우지 않는다(TUI 는 알림 패널에만 조용히 넣는다).
- 데몬의 하트비트·재접속·`resume` 규칙은 TUI 와 같다. 재시작 시 `use_device_ack`(notify 모드 ack 기록만 읽음) 또는 저장된 `epoch/last_seq` 로 재개해 놓친 alert 를 받는다.
- CPU·메모리 최소: 데몬은 이벤트를 파싱해 alert 만 처리하고 상태를 쌓지 않는다.

## 8. 이벤트 이름 전체 목록

`hello, ready, reply, pong, session.revoked,`
`message.created, message.updated, message.deleted, thread.updated, reaction.added, reaction.removed, message.pinned, message.unpinned, poll.updated,`
`typing, read_state.updated,`
`channel.created, channel.updated, channel.deleted, channel.member_added, channel.member_removed, channel.prefs_updated,`
`space.joined, space.updated, space.deleted, space.removed, space.member_joined, space.member_left, space.member_updated, space.invite_received, space.invite_removed,`
`friend.request_received, friend.request_accepted, friend.request_removed, friend.added, friend.removed, block.updated, user.updated, settings.updated,`
`presence.snapshot, presence.updated, presence.self, focus.started, focus.ended,`
`notification.created, notifications.read, alert,`
`timeline.item_created, post.deleted, post.stats, post.reply_created, story.created, story.deleted,`
`device.tui_state`

클라이언트는 모르는 `type` 을 무시한다(서버가 나중에 추가해도 깨지지 않게).

클라이언트 프레임: `ping, resume, ack, typing, idle, watch`.
