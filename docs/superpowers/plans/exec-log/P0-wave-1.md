# P0 wave 1 통합 기록

- 통합 브랜치: `dev/v0.1.3` (워크트리 `.claude/worktrees/dev-v013`), 2026-11-09.

## 병합한 것

| 브랜치 | 머지 커밋 | 브랜치 끝 | 들어온 커밋 |
|---|---|---|---|
| `feat/v013-p0-base` | `6150b16` (`--no-ff`, 앞 부모 `c4f903e`) | `800f580` | `7f0d457` P0 Task 1, `efd50c3` P0 Task 2, `800f580` P0 리뷰 반영 |

## 충돌과 해결

- 충돌 없음. `dev/v0.1.3` 은 브랜치를 딴 뒤 바뀐 것이 없어서 ort 병합이 그대로 끝났다.
- i18n 생성 파일과 골든 파일은 이번 병합에서 바뀌지 않았다. 병합 뒤 `git status` 도 깨끗하다(골든 변경 0).

## 통합 수정

- 없음. 병합 직후 검증 목록이 모두 통과했다.
- `server/node_modules` 가 없어 `npm ci` 를 먼저 돌렸다. server/ 코드는 바뀌지 않았지만, 계약 문서(api.md)를 읽는 server 시험이 있어 typecheck 와 전체 시험도 돌렸다.

## 검증 (이 세션, `6150b16` 기준)

- `go build ./...` rc=0
- `go test -count=1 ./...` rc=0, ok 패키지 39개, FAIL/panic 0 (마지막 `ok github.com/patrache/masil/internal/version`)
- `go vet ./...` · `GOOS=windows go vet ./...` · `GOOS=linux go vet ./...` 모두 rc=0
- 바뀐 .go 파일(`cmd/masil/notices_test.go`, `internal/deps/deps.go`) `gofmt -l` 빈 출력
- `cd server && npm run typecheck` rc=0, `npm test` → Test Files 64 passed (64), Tests 626 passed (626)

## 다음 wave 가 쓸 수 있는 인터페이스

Task 1 (빌드·의존성)
- go.mod `go 1.26.6`, toolchain 줄 없음. 새 require `github.com/charmbracelet/x/vt` v0.1.0, `github.com/charmbracelet/x/xpty` v0.1.4, `github.com/ionut-t/goeditor` v0.19.0 (간접: `creack/pty` v1.1.24, `charmbracelet/x/conpty` v0.2.0, `charmbracelet/x/exp/ordered` v0.1.0, `dlclark/regexp2/v2` v2.7.1).
- `internal/deps/deps.go` blank import: `x/vt`, `x/xpty`, `goeditor`, `golang.org/x/text/language`.
- `scripts/notices.sh` → `THIRD_PARTY_NOTICES` (`== <module> <version> ==` + `-- <licensefile> --`). `.goreleaser.yaml` archives 에 포함, ci.yml `go-version: '1.26.6'`.
- 시험: `TestThirdPartyNoticesCoversModules`, `TestGoDirective` (`cmd/masil/notices_test.go`).

Task 2 (계약)
- `docs/contract/client-architecture.md` 1.1–1.3, 2.1, 2.2, 3.2, 3.3, 4.3.4, 4.6, 4.9, 5.2, 5.3.2, 5.3.3, 5.3.6, 5.4–5.6, 9.1, 10.2, 10.3.
- `docs/contract/api.md`: 0.1 Accept-Language, 0.5 `what:"named_versions"`, 0.7 `RL_DOC`·`RL_SHARE`·`RL_SHARE_WS` (namespace 7111·7112·7113), 2장 `Message.kind "card"`·`Card`·`CardType`·`Alert.template`·`AlertTemplate`·`target.type "doc"`+`doc_type`(doc 일 때만), 5.10/5.11 카드 규칙, 5.24 `Doc`·`DocVersion`·`DocComment`·REST 표·권한·버전 규칙.
- `docs/contract/events.md`: 4.7 `alert.template`, 4.10 `doc.created`·`doc.updated`·`doc.comment_created`.
- `docs/contract/data-model.md`: 마이그레이션 0004–0007, 3.12 `messages.card_json`, 3.13 `collab_docs`·`collab_versions`·`collab_comments` + DO SQLite.
- `docs/contract/server-architecture.md` 3.1: DO 4개(UserHub·TopicHub·CollabHub·WorkspaceHub), 태그 v2, CollabHub RPC `revokeSession(userId, sessionIds|"all", code)`·`setRole(userId, role)`.
- `docs/contract/collab.md`: `Op`(put/set/del/undel/reset), `Snapshot`, 프레임 hello/op/ack/reject/presence/kicked, `reject.code` 6개(닫힘, `forbidden` 예약), 이름 규칙 `^[A-Za-z0-9_.:-]{1,64}$`(+`__proto__`·`constructor`·`prototype` 금지), `client_op_id` `^[A-Za-z0-9_-]{1,64}$`, 프레임 상한 클라→서버 65,536 / 서버→클라 2,162,688, 종료 코드 1009/4001/4003/4008/4100, 연결 첨부 `{userId, sessionId, role}`, `DocStateView`, `RejectReason`, `DocTypeSpec`, `DOC_TYPES`(arch 1,048,576/300/1000/16,384, apicol 2,097,152/1000/61,440), 6장 arch·7장 apicol 은 빈 절.
- `docs/contract/collab-vectors.json` 벡터 18개(기존 13 + 이름 규칙 5).
- `docs/contract/i18n/alert-templates.json` `{"version":1,"templates":{...16개}}`.

## 미룬 것 (오너 확인 필요)

- 이름 버전 101번째를 `422 limit_exceeded` 로 한 것(계획 원문은 400).
- go.mod 의 `charmbracelet/x/exp/ordered` v0.1.0 추가, `regexp2/v2` v2.2.1 → v2.7.1 상향 승인. 승인 전에는 계획 Global Constraints 를 고치지 않음.
- apicol `maxFieldBytes` 61,440 이 설계서 "요청 본문 64KB" 와 약 4KB 다른 점.
- 되돌리기 버전을 영구 보관에서 30일·200개 보관으로 바꾼 점.
- server/ 계약 시험은 아직 api.md 리미터 대조뿐. collab-vectors.json 을 읽는 실제 시험은 CollabHub 구현 Task 몫.

## 배치별 "내 판단" 모음

batch base (`docs/superpowers/plans/exec-log/P0-base.md`)
- 내 판단: `x/vt` v0.1.0 이 끌어오는 `charmbracelet/x/exp/ordered` v0.1.0 은 피할 수 없는 새 간접 의존성이라 받았고, goeditor v0.19.0 때문에 `regexp2/v2` 가 v2.2.1 → v2.7.1 로 올랐다. 골든 변화 없음.
- 내 판단: ultraviolet 은 MVS 결과 v0.0.0-20261008173134-6b8d4baf91b4 그대로.
- 내 판단: `scripts/notices.sh` 는 go.mod require(직접·간접) + 표준 라이브러리만 싣는다(시험 전용 모듈 제외). 시험도 같은 범위.
- 내 판단: 이름 버전 101번째 오류를 api.md 0.5 표에 맞춰 `422 limit_exceeded {what:"named_versions",limit:100}` 로 하고, 계획 Task 28 도 맞췄다.
- 내 판단: `alert-templates.json` 에 Task 8 Step 1 표 16개를 모두 넣었다(`doc.comment_mention` 포함). none/verbatim 은 서버 코드와 대조.
- 내 판단: api.md 6장 표는 Task 26·28·29 몫이라 건드리지 않음. 0.7 namespace 7111·7112·7113 은 계획 색인 그대로.
- 내 판단: 이미 있는 객체에 `put` 이 오면 준 필드만 덮어쓰고 나머지 필드와 `type` 은 그대로.
- 내 판단: 없는 객체에 대한 `set`·`del`·`undel` 은 오류 없이 버린다(순번은 씀). 프레임 하나 = 순번 하나, 프레임 안에서는 뒤 op 가 이김.
- 내 판단: 스냅숏 정규형 = `objects` 는 `id` 바이트 순, `deleted` 는 true 일 때만, 삭제된 객체도 필드 유지. `want.seq` 는 적용된 마지막 순번, 거절 배치는 순번을 쓰지 않음.
- 내 판단: 벡터 형식에 거절 배치 `{ops, reject:"invalid"}` 를 더했다. 벡터는 공통 모양 검사만 본다.
- 내 판단: 잠금(`locked`)·속도(`rate_limited`) 검사를 reject 순서의 공통 1단계에 넣었다.
- 내 판단: i18n 설계서 6.4.3 영역 목록에서 `claude.toml` 을 빼고 `tools.toml` 을 더했다(JX8).
- 내 판단: 방 삭제는 소프트 삭제라 CASCADE 가 없다. 삭제된 방의 문서는 REST·`/ws` 모두 `spaces.deleted_at` 을 보고 404. 열린 소켓은 Task 28 `kickFromSpaceDocs(space_deleted)` 가 닫는다.
- 내 판단: `Alert.target.doc_type` 은 `type == "doc"` 일 때만 싣는다("필드는 항상 있다" 규칙의 예외로 명시).
- 내 판단: `reset` 의 `author` 는 복원을 요청한 사용자, 결과 순번은 그 프레임의 seq, `snapshot.seq` 는 무시.
- 내 판단: 프레임 상한을 방향별로 나눔(클라→서버 65,536, 서버→클라 2,162,688). 조각 전송은 만들지 않음. `hello` 의 ops 가 `maxDocBytes` 를 넘으면 snapshot.
- 내 판단: apicol `maxFieldBytes` 를 61,440 으로 낮춤(필드 하나가 `set` 프레임 하나에 들어가야 함).
- 내 판단: 연결 첨부 `{userId, sessionId, role}`, RPC `revokeSession`·`setRole` 추가, 종료 코드 4003 추가. 세션 만료만으로는 열린 소켓을 닫지 않음.
- 내 판단: 자동 버전 "op 200개" 는 순번 기준, 자동 버전 간격 5분 이상. 자동·되돌리기 버전은 "기록 버전" 으로 30일·문서당 200개, 이름 버전은 영구 100개. 버전마다 스냅숏 전체, 목록은 최대 300개라 페이지 없음.
- 내 판단: 이름 규칙 `^[A-Za-z0-9_.:-]{1,64}$`, `__proto__`·`constructor`·`prototype` 금지, `client_op_id` `^[A-Za-z0-9_-]{1,64}$`. 벡터 5개 추가(총 18개).
- 내 판단: 잠금은 "잠겨 있고 보낸 사람이 만든 사람·owner·admin 이 아니면 `locked`". `forbidden` 은 예약 코드로 남김.

wave 1 통합
- 내 판단: 배치 보고서는 새 간접 의존성을 `golang.org/x/exp/ordered` 라 적었지만 go.mod 실제 경로는 `github.com/charmbracelet/x/exp/ordered` 다. 이 기록에는 실제 경로로 적었다(P0-base.md 원문은 고치지 않음).
- 내 판단: server/ 코드는 바뀌지 않았지만 server 시험이 계약 문서를 읽어서 `npm run typecheck`·`npm test` 도 돌렸다.
