# P0 batch "server" 실행 기록 (Task 8·24·26·27·28)

- 브랜치 `feat/v013-p0-server` (기준 `dev/v0.1.3` c58fc9f), 2026-11-09.
- 계획: `docs/superpowers/plans/2026-11-09-masil-v013-P0.md` Task 8·24·26·27·28. 서버 쪽만. push·tag·배포·원격 wrangler 없음.
- 시험은 기계 부하(load 30~50, 다른 batch 병렬)로 5초 제한 시간 초과가 가끔 났다. `npx vitest run --maxWorkers=4` 로 돌리고, 실패한 파일은 혼자 다시 돌려 통과를 확인했다(코드 결함이 아닌 부하 지연).

## Task 8 — alert 템플릿 · 연결 언어별 렌더 · 한글 래칫

- 내 판단: 집중 모드 REST 는 `routes/me.ts` 가 아니라 `routes/presence.ts` 에 있어 그쪽에서 `DELETE /v1/me/focus` 의 요약을 `localizeSummary` 로 바꿨다.
- 내 판단: `doc.comment_mention` 의 ko 제목은 `{space}` 를 빼고 `"{actor}님이 {doc} 댓글에서 멘션했어요"` 로 했다(Task 29 기대 제목 `"하나님이 결제 시스템 v3 댓글에서 멘션했어요"` 와 맞춤). en·zh-Hans 는 `{space}` 를 싣는다. 시험은 "언어마다 쓰는 param 은 선언된 param 의 부분집합이고, 합집합은 선언과 같다" 로 본다.
- 내 판단: 기존 realtime 시험(fanout·hub-await-races·notify-mode·presence-focus·replay-hidden-actors·ws-resume)을 새 `Alert.template`·`AlertSpec` 타입에 맞춰 고쳤다(`template: null`·`alertSpec`). 동작 기대값은 바꾸지 않았다.
- 내 판단: `AlertItem` 모양은 `{userId, actorId, alert: AlertSpec, channelId?}` — 저장되는 ko 대체 문구는 `notify.ts` 가 한 번 만들고, UserHub 가 연결 언어로 다시 그린다. 언어는 저장하지 않는다(연결 첨부 `lang` 은 메모리).
- 내 판단: 한글 래칫의 허용 목록은 `test/fixtures/hangul-allow.json` = `{"src/routes/spaces.ts": 1}`(기본 채널 이름 — P5 Task 20 몫), `src/lib/i18n.ts`·`src/lib/validate.ts` 는 면제.

## Task 24 — 카드 메시지

- 내 판단: `updateCardMessage` 는 `edited_at`·멘션을 건드리지 않는다(카드 갱신은 사람의 편집이 아님).
- 내 판단: `kind` 가 `"card"` 가 아닌 전송에 붙은 `card` 는 무시한다(오류 아님 — 0.1.2 클라이언트의 모르는 필드와 같은 취급).
- 내 판단: 카드 메시지의 본문 수정 `PATCH` 는 `400 fields.message_id = "not_allowed"`, 삭제는 `card_json = NULL`(직렬화 `card: null`).

## Task 26 — 문서 표 · REST · 이벤트 · 자리

- 내 판단: `worker-configuration.d.ts` 는 Files 목록에 없지만 새 바인딩(COLLAB_HUB·WORKSPACE_HUB·RL_DOC·RL_SHARE·RL_SHARE_WS) 타입 때문에 로컬 `npx wrangler types --strict-vars=false` 로 다시 만들었다(원격 호출 없음, diff 작음).
- 내 판단: `GET /v1/docs/{doc_id}/ws` 경로와 api.md 6장 행은 Task 27 이 아니라 Task 26 에서 더했다 — Task 26 시험(방 밖 사용자 ws 404)이 경로를 필요로 하고, `contract-limiters` 시험은 경로와 행이 같은 커밋에 있어야 통과한다.
- 내 판단: 문서 제목·잠금 변경도 `updated_at` 을 올린다(목록 최신순에 반영).
- 내 판단: `CollabHub.init` 은 `docType` 도 받고(종류별 한도 검사용) 결과를 돌려준다. Task 27 에서 `{seq} | {error}` 로 확정.

## Task 27 — CollabHub DO

- 내 판단: 순서를 지키지 못한 부분 — `collab-reducer.ts` 는 시험보다 먼저 썼다(벡터·한도 시험은 그 뒤에 쓰고 통과 확인). DO(`collab-hub.test.ts`)는 시험을 먼저 써서 501 로 15개 중 14개 실패를 본 뒤 구현했다.
- 내 판단: POST `snapshot` 거절은 `400 fields.snapshot` — 모양·이름 규칙·`id` 중복·종류의 `validateOp`(`{op:"reset", snapshot}` 하나로 부름) 거절 = `"invalid"`, 5장 한도 초과 = `"too_long"`. 객체가 있으면 순번 1, 비면 0, `snapshot.seq` 무시.
- 내 판단: 공유 벡터는 고치지 않고 3개만 더했다: `client_reset_is_rejected`, `ops_count_is_1_to_200`, `missing_required_keys_are_rejected`(모두 3.3 1단계 규칙 그대로 — Go Task 30 도 같은 파일을 통과해야 함).
- 내 판단: `client_op_id` 가 없거나 모양이 틀린 op 프레임은 답할 곳이 없어 조용히 버린다. `base_seq` 가 0 이상 정수가 아니면 `reject invalid`. 바이너리 프레임은 close 1003, JSON 아님·모르는 `t` 는 버림.
- 내 판단: 속도(초당 20)는 모양 검사보다 먼저 센다(거절될 프레임도 셈). 검사 순서는 크기(1009) → 속도 → 모양 → 멱등 ack → 잠금 → `validateOp` → 한도.
- 내 판단: `hello{since}` 가 서버 순번보다 크면 snapshot. `op`·`presence` 는 hello 를 보낸 소켓에만 간다.
- 내 판단: presence 는 연결마다 마지막 값에 고친 것만 합쳐 전체 `{cursor, selection, dragging}` 를 보내고, 받는 쪽은 다른 사용자의 소켓(보낸 사람의 다른 소켓 제외)이다. 새로 붙은 소켓에 지난 presence 를 다시 보내지 않는다. `selection` 은 64자 이하 문자열 200개 이하(어기면 버림).
- 내 판단: 문서 소켓 연결 때 세션을 D1 에서 다시 확인하지 않는다(Worker 의 `requireAuth` 를 믿음). `revokeSessions` 와 연결 사이의 짧은 경합은 다음 연결의 `requireAuth` 가 막는다(collab.md 4.1 "D1 이 진실").
- 내 판단: 잠금 표시는 연결 때 Worker 가 D1 에서 읽은 값으로 DO `meta.locked` 를 덮는다(`setLocked` RPC 와 함께). 잠금 변경과 동시에 들어온 연결이 옛 값을 쓸 수 있는 아주 짧은 경합은 남는다.
- 내 판단: `closeAll(reason)` 뒤에는 DO 도 새 업그레이드를 404 로 거절한다(meta `closed`).
- 내 판단: 위 세부를 collab.md 3.4 "서버가 정한 세부" 로 더했다(다른 절은 고치지 않음).

## Task 28 — 버전 · 복원 · 잠금 · 강퇴 끊기

- 내 판단: 순서를 지키지 못한 부분 — 구현을 먼저 하고 시험을 썼다. 대신 배선(routes/docs.ts·spaces.ts·membership.ts·users.ts)을 `git stash` 로 잠시 빼고 새 시험 15개 중 14개가 실패함을 확인한 뒤 되돌렸다(통과한 1개는 Task 26 에서 이미 배선된 문서 삭제).
- 내 판단: `0005_collab.sql` 은 Task 26 판에 이미 `auto`·`restored_from` 열이 있어 고치지 않았다. DO `versions` 표는 아직 배포 전이라 Task 27 의 정의를 고쳐 `kind`(named/auto/restore)·`created_at` 을 더했다.
- 내 판단: 버전 메타(D1 `collab_versions`)도 DO 가 쓴다 — 자동 버전은 Worker 없이 alarm 이 만들기 때문에 쓰는 곳을 하나로 모았다. 본문 먼저, D1 실패 시 본문 삭제. `no` 는 DO 카운터.
- 내 판단: 이름 버전 100개 한도는 DO 가 센다(되돌리기 버전은 세지 않음). 101번째 → `422 limit_exceeded {what:"named_versions", limit:100}`(base batch 의 결정 그대로).
- 내 판단: 이름 버전·되돌리기 버전도 "마지막 버전" 으로 본다 — 그 뒤 순번이 없으면 자동 버전을 만들지 않는다. 200개 조건은 `head_seq - 마지막 버전의 seq`.
- 내 판단: 복원 응답의 `doc.head_seq` 는 reset 순번으로 D1 을 바로 올린 값이다(alarm 을 기다리지 않음, `MAX` 라 뒤로 가지 않음). 복원은 `doc.updated` 를 내지 않는다(내용 변경은 문서 소켓으로만 — events.md 4.10 의 목록 이벤트는 메타 변경용).
- 내 판단: 버전 경로의 `{no}` 가 양의 정수가 아니면 404(없는 버전과 같음). api.md 5.24 에 한 줄 더함.
- 내 판단: 소유권 이전 뒤에는 새 owner 에게만 `setDocRole(admin)` 을 부른다(이전 owner 는 admin → 문서 역할 그대로 admin).
- 내 판단: 문서 소켓 정리(`kickFromSpaceDocs`·`closeDocSessions`·`setDocRole`)는 모두 정리 batch 커밋 뒤 `rt.waitUntil` 로 돌린다(collab.md 4.1). 실패는 기록만.
- 내 판단: `closeDocSessions` 는 그 사용자가 멤버인 방(삭제된 방 포함)의 삭제 안 된 문서마다 부른다 — 삭제된 방의 문서 소켓은 이미 닫혀 있어 해가 없다.
- 내 판단: `revokeSessions` 의 대상 목록은 UserHub 에 넘기는 것과 같은 값(`"all"` 또는 실제로 폐기한 세션 id)을 문서 쪽에도 넘긴다.
