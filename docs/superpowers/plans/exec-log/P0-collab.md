# P0 batch "collab" 실행 기록 (Task 29·30)

- 브랜치 `feat/v013-p0-collab`(기준 `dev/v0.1.3` 58376da), 2026-11-09.
- 계획: `docs/superpowers/plans/2026-11-09-masil-v013-P0.md` Task 29·30.

## Task 29 — 문서 댓글 · `doc.comment_mention` 알림

- 내 판단: 잠긴 문서에도 댓글은 쓸 수 있다. 댓글은 문서 편집이 아니고, 잠금은 op·제목(5b4d9d2: 만든 사람·owner·admin만)에만 걸린다.
- 내 판단: `root_id` 는 같은 문서의 루트 댓글(`root_id IS NULL`)만 받는다. 답글에 다시 답글을 다는 것은 400 `invalid_request {root_id:"invalid"}` 로 막아 답글을 한 단계로 둔다.
- 내 판단: 삭제된 댓글에 대한 PATCH·DELETE 는 404 다. 목록에는 `deleted:true`, `body:""` 로 남아 스레드 모양을 지킨다.
- 내 판단: 이벤트는 계약에 있는 `doc.comment_created` 만 보낸다. PATCH·DELETE 는 이벤트가 없고, 본문을 고쳐 멘션이 새로 생겨도 알림을 다시 보내지 않는다(계약에 이벤트가 없어 가장 좁게 읽음).
- 내 판단: 댓글 DELETE 권한은 글쓴이 또는 방의 owner·admin(`getSpaceRole`)이다. 문서를 만든 사람은 따로 넣지 않았다.
- 내 판단: 멘션 대상은 그 방 멤버의 handle 이고 글쓴이 자신은 뺀다. 차단 관계와 방 밖 사람은 기존 `sendAlerts` 규칙대로 알림이 가지 않는다. alert 는 `mention` 종류라 받은편지함(inbox) 줄을 만들지 않는다.
- 내 판단: Task 29 커밋에서는 Go 쪽 doc 메서드가 아직 없어 `internal/api/contract_limiters_test.go` 의 `pendingLimiterRows` 에 댓글 3행(POST/PATCH/DELETE)을 임시로 넣었다. Task 30 에서 doc 메서드 사례(`docEndpointCases`)를 더해, 그 3행과 이미 있던 문서 REST 5행(Task 26·28 몫)을 모두 지우고 waiver 를 비웠다(빈 map).

## Task 30 — Go `internal/collab` · doc REST · `collabtest`

- 내 판단: `DocSocketURL` 은 `escPath`·`u.Path` 대신 `url.PathEscape` 로 문자열을 잇는다. `""`·`"."`·`".."` 이면 `""` 를 돌려준다. 요청 경로를 만드는 함수가 아니라 소켓 URL 이라, 경로 검사기(`path_build_test.go`)의 `pathBuildAllowed` 에 이유를 적어 넣었다.
- 내 판단: Files 목록 밖의 시험 기반 파일을 고쳤다: `internal/api/path_test.go`(pathCalls 행), `path_build_test.go`, `endpoint_harness_test.go`(`dedicatedLimiters` 에 `RL_DOC`), `types_test.go`와 `testdata/types/Doc*.json` 4개, `limiter.go`(`rlDoc`). 새 메서드를 기존 하네스가 요구하는 대로 등록한 것뿐이다.
- 내 판단: 소켓 읽기 한도(2,162,688)를 걸기 위해 `internal/realtime/conn.go` 에 `DialConn(ctx, url, h, limit)` 를 더하고, 기존 `coderDial` 도 이것을 쓰게 했다(동작 같음). 재연결 지터는 `internal/realtime/backoff.go` 의 `NewBackoff`/`Backoff` 로 노출했다. 둘 다 Files 목록 밖이다.
- 내 판단: `doc.created`·`doc.updated`·`doc.comment_created` 는 `events.go` 에 디코더를 두고 `loggedTypes` 에도 넣었다. `protocol.go` 는 고칠 것이 없었다.
- 내 판단: `EventOverwritten` 은 "확인된 내 값이 남의 더 늦은 op 로 바뀜" 이다. 원격 op 에는 base_seq 가 없어 진짜 동시성은 알 수 없다. 그래서 두 경로를 잡는다. 하나는 ack 뒤에 원격 op 가 오는 경우다. 다른 하나는 재연결 catch-up(hello ops) 뒤에 중복 ack 가 오는 경우로, 이때 내 값과 지금 값이 다르면 `Mine`·`By` 를 싣는다. 세션은 내 사용자 ID 를 모르므로 내 쓰기는 내부 표식(`"\x00me"`)으로 기록한다.
- 내 판단: `reject{code:"rate_limited"}` 는 op 를 버리지 않고 1초 뒤 다시 보낸다. 그 밖의 reject(`invalid`·`locked`·`forbidden`·`too_large` …)는 op 를 빼고 `EventRejected` 와 되돌림 `EventChanged` 를 보낸다.
- 내 판단: `Submit` 은 공통 모양 검사(`CheckOps`)에 걸리거나 프레임이 65,536 바이트를 넘으면 보내지 않고 그 자리에서 `EventRejected{invalid|too_large}` 를 낸다(서버가 1009 로 끊는 것을 막음).
- 내 판단: 시험 경로 `collab.SetBackoffForTest(rnd) (restore)` 를 공개했다. collabtest·uitest 시험이 재연결을 바로 일으키는 데 쓴다.
- 내 판단: `Doc.Field(id, field)` 는 계획에 없는 작은 읽기 도우미로 더했다.
- 내 판단: `collabtest.Hub` 에 계획에 없는 `MapToken(token, user)`·`Snapshot(docID)`·`Conns(docID, user)` 를 더했다. 매핑이 없으면 bearer 토큰 자체를 사용자 ID 로 읽는다. 오프라인(`SetOnline(user,false)`)은 소켓을 1006 으로 끊고, api.Error 가 아닌 `ErrOffline` 으로 dial 을 거절한다(세션은 재시도). 문서 DELETE 는 열린 소켓에 `kicked{doc_deleted}` 뒤 4100 을 보낸다.
- 내 판단: `TestHubVectors` 는 reset 이 아닌 배치를 허브 소켓의 op 프레임으로 보내 ack seq 를 확인한다. reset 배치는 `Inject` 로 넣고, 거절 배치는 `reject invalid` 를 확인한다. 끝 스냅숏은 벡터의 `want` 와 같아야 한다.
- 내 판단: `uitest.WithCollab` 를 연결하려고 Files 목록 밖인 `internal/ui/uitest/harness.go` 에 몇 줄을 더했다. `app.New` 직후 `ctx.CollabDial = hub.Dial` 을 넣고, `hub.Register(srv)` 와 `hub.MapToken(creds.Token, creds.UserID)` 를 부른다. 시험은 `internal/ui/uitest/collab_test.go`(새 파일)에 있다. WithCollab 가 없으면 `CollabDial` 은 nil(진짜 연결)로 남아 기존 골든은 그대로다.
- 내 판단: TDD 순서를 일부 어겼다. api doc 메서드, collab op/doc, session, collabtest hub 는 구현을 먼저 썼다. 대신 각 시험을 쓴 뒤 구현 파일을 scratchpad 로 잠시 옮겨 빌드 실패를 확인했고, 되돌린 뒤 통과를 확인했다. realtime(`DialConn`·`NewBackoff`)과 uitest `WithCollab` 는 실패를 먼저 본 뒤 구현했다.
- 내 판단: 서버 전체 시험(vitest)은 기계 부하(load ≈ 500) 때문에 `--maxWorkers=4` 로 돌렸다. 시간 초과로 실패한 파일은 따로 다시 돌려 통과를 확인했다(Task 29 때 `relation-races` 1회).

## 리뷰 반영 (P0 review)

- 내 판단: `rate_limited` 로 붙잡힌 op 가 있으면 그 뒤 op 는 보내지 않는다(`flush` 에서 `continue` 대신 `break`). 이미 전선에 올라 먼저 순번을 받은 뒤 op(B)는 되돌릴 수 없으므로, B 의 ack 가 "앞선 내 op 가 아직 대기 중"일 때나 "앞선 내 op 가 받은 seq 보다 작을" 때 B 의 ops 를 새 `client_op_id` 로 그 자리에 다시 넣어 앞선 op 뒤에 한 번 더 보낸다. 옛 id 는 서버가 중복으로 보아 적용하지 않으므로 새 id 가 필요하다. 그래서 그런 B 는 Submit 이 돌려준 id 로 ack·reject 가 오지 않을 수 있다(EventRejected 에는 Ops 가 실린다).
- 내 판단: 재연결 업그레이드가 404 이고 그 세션이 한 번이라도 hello 를 받았으면 `EventKicked{Reason: ReasonNotFound("not_found"), Pending}` 를 낸 뒤 마지막 `EventState{Err}` 를 낸다. 처음 여는 404 는 강퇴가 아니라서 내지 않는다. 401·403 은 강퇴가 아니라 계정 문제라 EventKicked 를 내지 않고, 문서 주석에 "마지막 EventState 뒤에도 Pending() 이 답한다"고 적었다.
- 내 판단: 덮어쓰기 판단의 값 비교는 바이트가 아니라 뜻으로 한다(`sameJSON`: 둘 다 `any` 로 풀어 `reflect.DeepEqual`, 숫자는 JS 처럼 float64). collab.md "JSON 값은 의미가 같으면 같다(키 순서 무관)" 를 따랐다.
- 내 판단: `Options.UserID` 를 더했다. 같은 사용자의 다른 터미널 op(author == UserID)는 "남이 덮어씀"으로 보지 않고 내 쓰기로 기록한다. 비어 있으면 예전처럼 모든 원격 author 를 남으로 본다.
- 내 판단: 이벤트 전달 고루틴은 큐가 생길 때 띄우고 큐가 비면 끝낸다. 그래서 ctx 만 취소되고 Close 가 불리지 않아도 고루틴이 남지 않는다. Close 는 실행 중인 OnEvent 를 기다리되, OnEvent 안에서 부른 Close 는 기다리지 않는다(고루틴 ID 로 구별). `sync.Once` 를 빼서, OnEvent 안의 Close 와 바깥 Close 가 겹쳐도 교착이 없다.
- 내 판단: 연결이 끊길 때 다른 사람 presence 가 있었으면 `EventPresence{Peers: nil}` 을 한 번 낸다.
- 내 판단: `rejectRetry` 를 상수에서 시험용 변수로 바꿨다(패키지 시험은 150ms). `flush` 에서 인코딩이 실패한 op(Submit 이 이미 검사해 실제로는 없음)는 건너뛰지 않고 `EventRejected{invalid}` 로 뺀다. 남겨 두면 위 순서 규칙 때문에 뒤 op 가 영영 막히기 때문이다.
- 내 판단: `collabtest` 시험 `TestHubSessions` 를 계획 이름 `TestHubKickAndOffline` 으로 바꿨다. `Hub.SeedDoc(meta, s)` 를 더해 SpaceID·CreatedBy 를 줄 수 있게 했고, `Seed` 는 그 얇은 포장이다(space "").
