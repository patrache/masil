# P0 batch "base" 실행 기록 (Task 1·2)

- 브랜치 `feat/v013-p0-base`, 2026-11-09.
- 계획: `docs/superpowers/plans/2026-11-09-masil-v013-P0.md` Task 1·2.

## Task 1 — Go 1.26.6 · 고정 의존성 · 제3자 고지

- 내 판단: `x/vt` v0.1.0 이 끌어오는 `golang.org/x/exp/ordered` v0.1.0 은 피할 수 없는 새 간접 의존성이라 그대로 받았고, goeditor v0.19.0 이 요구해 `regexp2/v2` 가 v2.2.1 → v2.7.1 로 올랐다(MVS). 기존 골든 파일은 바뀌지 않았다.
- 내 판단: ultraviolet 은 MVS 결과 v0.0.0-20261008173134-6b8d4baf91b4 그대로 남았다(올리지 않음).
- 내 판단: `scripts/notices.sh` 는 `go list -m all` 전부가 아니라 go.mod 의 require(직접·간접) + Go 표준 라이브러리만 싣는다. `go list -m all` 에는 의존성의 시험 전용 모듈(testify 등, 바이너리에 안 들어가고 내려받지도 않음)이 섞이기 때문이고, `TestThirdPartyNoticesCoversModules` 도 같은 범위를 검사한다.

## Task 2 — 계약 문서

- 내 판단: 이름 버전 101번째의 오류는 계획 본문의 "400" 대신 기존 계약 api.md 0.5 표(`limit_exceeded` = 422)를 따라 `422 limit_exceeded {what:"named_versions", limit:100}` 로 적었다. 0.5 의 `what` 목록에 `named_versions` 를 더했다.
- 내 판단: `alert-templates.json` 에는 계획 Task 8 Step 1 표 전체(16개)를 지금 넣었다 — `doc.comment_mention` 은 Task 29 에서 쓰이지만 표에 있어 함께 넣었다. body 값(none/verbatim)은 지금 서버 코드의 알림 body 와 대조해 표와 같음을 확인했다.
- 내 판단: api.md 6장 표 행은 Task 26·28·29 몫이라 건드리지 않았다(limiter 계약 시험이 6장만 대조). 0.7 표의 namespace 7111·7112·7113 은 계획 색인 그대로.
- 내 판단: `collab.md` 의 `put` 이 이미 있는(삭제 안 된) 객체에 오면 "준 필드만 덮어쓰고 나머지 필드와 `type` 은 그대로" 로 정했다(계획의 "이미 있으면 필드 덮어씀" 을 필드 LWW 에 맞춰 가장 좁게 읽음).
- 내 판단: 없는 객체에 대한 `set`·`del`·`undel` 은 오류 없이 버린다(no-op, 순번은 씀). 프레임 하나 = 순번 하나, 한 프레임 안에서는 뒤 op 가 이긴다.
- 내 판단: 스냅숏 정규형 = `objects` 를 `id` 바이트 순 정렬, `deleted` 는 true 일 때만 싣고, 삭제된 객체도 필드를 그대로 싣는다. 벡터의 `want.seq` 는 적용된 마지막 순번이고 거절 배치는 순번을 쓰지 않는다.
- 내 판단: 벡터 형식에 거절 배치 `{ops, reject:"invalid"}`(seq 없음)를 더해 "모르는 op 거절" 을 표현했다. 벡터는 공통 모양 검사만 보고 종류별 한도·`validateOp` 는 쓰지 않는다. 벡터 13개는 scratchpad 의 참조 reducer 로 교차 확인했다.
- 내 판단: `collab.md` 의 `reject` 순서에서 잠금(`locked`)·속도(`rate_limited`) 검사를 공통 단계(1단계)에 넣었다 — 계획은 공통 → `validateOp` → 한도만 적어, 그 밖 코드의 자리를 가장 앞으로 정함.
- 내 판단: i18n 설계서 6.4.3 은 계획대로 영역 목록에서 `claude.toml` 을 빼고 `tools.toml` 을 더했다(JX8 주석 한 줄).

## P0 리뷰 반영 (Task 1·2 리뷰 지적 10건)

- 내 판단: 방 삭제는 소프트 삭제라 CASCADE 가 일어나지 않는다. data-model 3.13 문장을 고치고, 삭제된 방의 문서는 모든 문서 경로(REST·`/ws`)에서 `spaces.deleted_at` 을 보고 404 로 숨긴다고 collab.md 2장·api.md 5.24·계획 Task 26 에 적었다. 열린 소켓은 Task 28 의 `kickFromSpaceDocs(space_deleted)` 가 닫는다.
- 내 판단: 이름 버전 101번째 오류는 계획 Task 28(Interfaces·Step 1)도 `422 limit_exceeded` 로 맞췄다. 계약(api.md 0.5 표)과 계획이 이제 같다. 오너 확인 항목으로 남긴다.
- 내 판단: go.mod 의 `charmbracelet/x/exp/ordered` v0.1.0 추가와 `dlclark/regexp2/v2` v2.2.1 → v2.7.1 상향은 고치지 않았다. 둘 다 x/vt·goeditor 가 요구해서 생긴 것이라 빼면 고정한 세 모듈을 쓸 수 없다. 오너 승인을 받기 전까지는 계획 Global Constraints 도 고치지 않는다(오너 확인 항목).
- 내 판단: `Alert.target.doc_type` 은 `null` 로 늘 싣지 않고, "응답 필드는 항상 있다" 규칙의 예외라고 그 줄에 적었다. `type == "doc"` 일 때만 싣는다. 기존 alert 대상 모양을 바이트까지 그대로 두고, Go 의 `omitempty` 와 맞추기 위해서다.
- 내 판단: `reset` op 의 `author` 는 복원을 요청한 사용자 ID 다. 결과 순번은 그 프레임의 seq 이고 `snapshot.seq` 는 무시한다. 이 규칙을 collab.md 1.1·3.2, api.md 5.24 복원 행, 계획 Task 27·28·30 에 적었다.
- 내 판단: 문서 소켓의 프레임 상한을 방향별로 나눴다. 클라→서버는 65,536(설계서 C1.4 의 64KB), 서버→클라는 2,162,688(가장 큰 문서 2,097,152 + 65,536)이다. 나눠 보내는(조각) 형식은 만들지 않았다. Hibernation 중인 DO 는 조각을 모으던 메모리를 잃을 수 있고 첨부에는 넣을 수 없어서다. Cloudflare 문서상 받는 메시지 한도는 32 MiB, 보내는 쪽은 한도가 없다. `hello` 의 ops 묶음이 `maxDocBytes` 를 넘으면 snapshot 으로 바꾼다.
- 내 판단: `apicol` 의 `maxFieldBytes` 를 65,536 → 61,440 으로 낮췄다(collab.md 5장, 계획 P0 Task 27 자리표시, P4 Task 계획 720행). 필드 하나를 `set` 프레임 하나에 실을 수 있어야 하기 때문이다. 설계서 C3 의 "공유 컬렉션 요청 본문 64KB" 와 약 4KB 차이가 난다 — 오너 확인 항목.
- 내 판단: 문서 소켓 첨부를 `{userId, sessionId, role}` 로 바꾸고 RPC `revokeSession(userId, sessionIds|"all", code)`·`setRole(userId, role)` 을 더했다(collab.md 4.1, server-architecture 3.1). 부르는 곳은 `services/users.ts` 의 `revokeSessions` 뒤와 방 역할을 바꾸는 경로다. 종료 코드 표에 4003 을 더했다. 세션 만료(폐기 없이 시간이 지남)는 열린 소켓을 닫지 않는다 — 세션은 쓰는 동안 늘어나고, 다음 연결은 `requireAuth` 가 막는다.
- 내 판단: 자동 버전의 "op 200개" 는 순번(op 프레임) 기준이다. 자동 버전끼리는 5분 이상 떨어지게 했다. 자동 버전과 되돌리기 버전은 "기록 버전" 으로 함께 다룬다: 30일 보관, 문서당 최근 200개. 이름 버전은 영구 100개 그대로다. 버전 본문은 버전마다 스냅숏 전체를 둔다. 목록은 최대 300개라 페이지를 두지 않았다(Go `ListDocVersions` 모양을 바꾸지 않으려고). 계획은 되돌리기 버전을 "영구" 로 적었지만 그러면 크기에 상한이 없어 바꿨다 — 오너 확인 항목.
- 내 판단: `id`·`type`·필드 이름 규칙을 `^[A-Za-z0-9_.:-]{1,64}$` 로 정했다. `__proto__`·`constructor`·`prototype` 은 쓸 수 없다. 규칙을 어긴 프레임은 `reject invalid` 다. `client_op_id` 는 `^[A-Za-z0-9_-]{1,64}$` 다. 필드 값 안의 키는 이 규칙을 받지 않는다. 거절 벡터 4개와 허용 벡터 1개를 collab-vectors.json 끝에 더했다(기존 13개는 그대로). 18개 모두 scratchpad 의 참조 검사기로 확인했다. 옛 규칙(1..64자만)으로 돌리면 새 거절 벡터 3개가 실패한다.
- 내 판단: 잠금 검사는 "문서가 잠겨 있고, 보낸 사람이 만든 사람·owner·admin 이 아닐 때" 로 고쳤다. `forbidden` 은 예약 코드로 남겼다. v1 서버는 보내지 않고, 클라이언트는 `locked` 와 같이 다룬다(코드 목록이 닫혀 있고 계획 여러 곳에 있어서 빼지 않았다).
- 참고(고치지 않음): DO SQLite 의 행 한도는 8MB 라 2MB 문서 본문은 들어간다(Cloudflare limits 문서 확인).
