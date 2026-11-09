# P1~P4 줄기 통합 기록

- 통합 브랜치: `dev/v0.1.3` (워크트리 `.claude/worktrees/dev-v013`), 2026-11-10. 병합 전 기준 `9f915c2d`(P5 0단계 통합 기록). 네 줄기 모두 이 기준에서 갈라졌다.
- push·tag·배포·원격 wrangler·유료 리소스 없음. 작업 브랜치·워크트리는 지우지 않았다(`v013-p3` 워크트리의 추적 안 된 `masil` 바이너리도 그대로).

## 병합 (끝난 순서: P4 → P1 → P2 → P3, 모두 `--no-ff`)

| 순서 | 줄기 | 브랜치 끝 | 들어온 커밋 | 머지 커밋 | 충돌 |
|---|---|---|---|---|---|
| 1 | P4 API 시험 | `62cdfb3a` (05:15) | 55 | `4e69244c` | 없음 |
| 2 | P1 LeetCode·lazygit | `2391c975` (05:31) | 58 | `b0c2f44d` | CA 1곳 |
| 3 | P2 작업공간 | `952dfe19` (07:13) | 70 | `c349b782` | 문서 3·Go 시험 3 |
| 4 | P3 아키텍처 설계 | `2b66d747` (08:10) | 68 | `17820684` | 문서 2 |

순서는 각 줄기 마지막 커밋(리뷰 반영까지 끝난 시각) 순이다. 네 줄기 모두 마지막 묶음 리뷰 반영이 커밋돼 있고 워크트리가 깨끗했다.

## 충돌과 해결

원칙: 색인 2절 — 덧붙이기 표·목록은 양쪽을 다 남기고, 먼저 머지한 쪽 행을 앞에 둔다. 뒤에 머지한 줄기가 번호를 양보한다.

- **P1 머지** — `docs/contract/client-architecture.md` 3.2 v0.1.3 파일 표: P4 `api/*` 8행 + P1 `tools-state.json` P1 섹션·캐시 2행, 둘 다 남김(P4 먼저).
- **P2 머지**
  - `docs/contract/api.md` 5장: P1 이 이미 **5.25 LeetCode** 를 썼고 P2 도 5.25 를 썼다 → P2 절을 **5.26 작업공간 공유** 로 번호 변경. 6장 경로 표는 leet 행 다음 workspace 행.
  - `docs/contract/data-model.md`: P1 **3.14 LeetCode**, P2 → **3.15 작업공간 공유**.
  - 번호 변경에 맞춰 P2 의 교차 참조를 고쳤다: `api.md` 0.7·2장 두 줄, `events.md` 4.11, `internal/api/{types.go(60행),ep_workspace.go,ep_workspace_test.go,limiter.go,types_workspace.go}`, `server/src/types/{api.ts,rows.ts}`, `server/src/{lib/i18n.ts,services/workspace-shares.ts,do/workspace-hub.ts,routes/workspace-shares.ts}`, `server/migrations/0007_workspace.sql`(주석만), `server/test/api/workspace-shares.test.ts`. P1 의 5.25/3.14 참조(예: `types.go` 126행, `contract_limiters_test.go`)는 그대로. P2 의 exec-log·계획 문서 안 옛 번호는 기록이라 고치지 않았다.
  - CA 3.2: P4·P1 행 뒤에 P2 `workspace-audit/`·`workspace-journal/` 행. CA 5.6: 양쪽의 자리표시(`P1 … leet.*, git.*` / `P2 … ws.*`)를 버리고 P1·P2 상세 행을 남김.
  - `internal/api/contract_limiters_test.go`(leet·workspace 케이스 둘 다), `path_test.go`(leet·workspace 메서드 둘 다), `types_test.go`(P1 Leet* 표본 + P2 `WorkspaceShare*` 두 줄을 `DocComment` 뒤에) — 모두 합친 뒤 `gofmt`.
- **P3 머지**
  - `api.md` 2장 카드 표: P1·P4 상세 행은 그대로, `arch.design`·`arch.sim` 의 "P3" 자리표시를 P3 상세 행으로 바꿈.
  - CA 2.1 패키지 트리(P4 줄 다음에 P3 줄, 그 뒤 P1 블록), 2.2 규칙(P2·P1 다음에 P3), 3.2 `arch/` 행, 3.3 `[tools.arch]` 주석 줄은 `[tools.api]` 절 **앞에** 두어 TOML 상 api 절에 붙지 않게, 5.6 은 P1~P4 상세 행 순서대로.
- 자동 병합된 공유 파일(손으로 확인): `server/test/api/cards.test.ts`(P1 leet 데이터 + P3 `archCardsLoose()` + P4 api 데이터가 함께), `server/test/realtime/collab-hub.test.ts`·`server/test/unit/collab-reducer.test.ts`(P3 `archRulesOff()` + P4 `req:` id·apicol 한도), `docs/contract/collab.md`(P3 6장 + P4 7장), `internal/api/types.go`·`ep_me.go`, `server/src/{lib/serialize.ts,routes/me.ts}`, `internal/api/testdata/types/{AuthResult,Me,Settings}.json`(P1 `leet_share_with_friends` + P2 `notify.workspace`) — 시험이 모두 통과해 그대로 둠.
- **마이그레이션**: `server/migrations/` 에 `0006_leet.sql`(P1)·`0007_workspace.sql`(P2) 둘 다 있음(0001~0007 연속).
- **i18n**: 병합 뒤 `go generate ./internal/i18n` → `git status` 깨끗(생성물 차이 0). 영역 파일은 줄기마다 따로(`leetcode`·`lazygit`·`workspace`·`alerts`(P2 행)·`archcanvas`·`apitest`)라 충돌 없음.
- **골든**: `git diff --name-status 9f915c2d HEAD -- '*.golden'` 에서 A(새 파일) 아닌 것 0개 — 기존 0.1.2/P0 골든 변경 없음. 골든은 손으로 합치지도 다시 만들지도 않았다(합칠 것이 없었음). 새 골든은 모두 각 도구 패키지 `testdata/` 아래.
- **go.mod**: P1 만 바꿈(`third_party/goeditor` 사본으로 `atotto/clipboard`·`rivo/uniseg` 직접 의존). `go mod tidy -diff` 깨끗.

## 통합 수정

- 없음. 네 줄기를 합친 트리에서 빌드·vet 세 OS·Go 전체 시험·서버 typecheck·vitest 가 처음부터 통과해 시험 먼저 고칠 깨짐이 없었다.
- 교차 확인(시험 밖): 도구 허브 순서 leet 1·workspace 2·arch 3·git 4·api 5 겹침 없음, `cmd/masil/register_{leetgit,workspace,arch,api}.go` 네 도구 모두 실제 바이너리에 등록, alert 대상 `workspace_share`(P2)·`doc:arch`(P3) 겹침 없음.

## 검증 (머지 커밋 `17820684` 트리, 이 세션)

- `go build ./...` → BUILD_OK
- `go vet ./...` / `GOOS=windows go vet ./...` / `GOOS=linux go vet ./...` → VET_OK / VET_WIN_OK / VET_LINUX_OK
- `gofmt -l` (9f915c2d..HEAD 에서 바뀐·새 .go 전부) → 출력 없음; `go mod tidy -diff` → 깨끗
- `go test -count=1 ./...` → EXIT=0, ok 패키지 84개, FAIL 0 (`cmd/masil 40.1s`, `internal/i18n 4.6s`(한글 래칫 포함), `ui/leet 19.5s`, `ui/gittool 4.9s`, `ui/workspace 8.6s`, `ui/arch 11.4s`, `ui/apitool 14.2s`, `ui/e2e 173.9s`)
- `cd server && npm run typecheck` → `tsc --noEmit && tsc --noEmit -p test` 오류 없음(TC_EXIT=0)
- `npm test` (단독 실행) → `Test Files 92 passed (92)`, `Tests 848 passed (848)`, NPM_EXIT=0

## 줄기별 미룬 것

**P1**
- Task 17 Step 6 README lazygit 줄 — P0 Task 31 뒤(색인 2절 README 순서).
- homebrew caveats 영어 줄을 P5 Task 21 의 영어 caveats 블록 안으로 옮기기 — P5 Task 21 rebase 때(`TestFormulaCaveatsMentionLazygit` 이 두 줄을 지킴).
- chat `msgPane.cardKey` 의 `p.repaint()` 한 줄(P0 Task 25 소유 파일) — P0 fix 커밋으로 옮길지 승인 대기. `TestCardKeyRepaints` 가 지킴.
- Windows: goeditor 의 CONIN$ 문제는 `third_party/goeditor` 사본으로 피했지만 실기기 확인 필요. Windows 자격 증명 관리자 저장 → 재시작 뒤 다시 묻지 않음 실기기 확인.
- `Fetcher.Today` 의 "LeetCode 날짜가 늦으면 10분 뒤 한 번 더"(하루 최대 2회) — 계획 JA27 "UTC 하루 1회" 와 다름.

**P2**
- 참가자 화면의 DM 대화 칸(B6.5·JB16 `🔓 암호화 안 됨`) — 대화 열이 P0 셸이라 P2 안에서 못 만듦(키 `dm_unencrypted`·`dm_hint` 만 있음).
- 셸 오버레이 위 `ctrl+\ x` 를 "그리기 횟수 > 커서 질문 횟수" 추론으로 처리 — P0 에 실제 오버레이 신호를 둘지.
- `ReasonUserSettings` 카탈로그 문구(Task 20/21 몫) 확인, Windows 8.3 짧은 이름·끝 점/빈칸 경로는 실기기 미시험.
- claude 를 작업공간에서 띄우면 공유가 아니어도 늘 `--permission-mode manual` — 명령줄이 settings 의 `defaultMode` 를 이기는지 실제 claude 로 미확인.
- 참가자가 문서만 닫은 경우 그 커서가 다음 위치 알림·나갈 때까지 남음. 허브가 내보내기와 끊김을 같은 `left` 로 알려 멤버 기록을 지우지 못함(프로토콜 그대로).
- Task 15~18 묶음 중 한 번 `go test ./...` 가 어느 패키지인지 모르게 FAIL 했던 간헐 실패 — 이후 모든 실행(이번 통합 포함)은 통과. 출처 미확인.

**P3**
- `archsim` 5ms 예산(Task 7, `MASIL_PERF=1 TestAnalyzeBudget`) — 조용한 M1 에서 통과 확인 전까지 출시 관문으로 남음(부하 중 6회 중 2회 통과).
- 가격표 값 — 네트워크 없이 기억으로 옮긴 정가·비율 리전, `as_of 2026-11` 미대조. 색인 4절 오너 확인 표에 넣을 후보.
- masil 을 끄는 길(ctrl+c·종료)에서 편집 뒤 1초 안의 개인 설계 편집 유실 — P0 에 "끄기 전 정리" 등록부가 필요.
- 위젯 `tab` 하위 보기 순환은 하지 않음(전역 `tab` 우선).
- 댓글 수 `✎n`(설계서 `💬n`), 댓글 패널 = 설정 패널의 한 모드(설계서는 탭) — 골든 갱신 승인 필요.
- 일일 도전은 팔레트 `arch.challenge` 로만 — P0 허브·열기 메뉴에 확장점이 없음(C2.12 "허브에서 따로 연다").
- 계획 Task 26 Produces·Task 28 Consumes 의 카드 함수 서명 문구 갱신(실제: `designCard(t, d, r, ref)`, `simCard(t, d, r, ref, ch *cardChallenge)`).
- `d2`·`mmdc` 실제 렌더링, `terraform validate` 미확인(fmt 만 확인).

**P4**
- 서버 `apicol` 의 `folder` 개수 한도 — 설계서·collab.md 에 없는 새 한도라 미추가.
- 공유 본문 실제 한도 = 필드 한도 61,440 바이트(설계서 "64KB" 와 다름, collab.md 7.2 에 기록).
- curl 왕복 수용 기준 2 해석(직접 친 자격 증명은 `•••` 로 내보냄).
- 기록·스필 파일 정리는 도구를 열 때 1시간 넘은 것만(끄기 고리 없음).

## 오너(또는 오케스트레이터) 결정이 필요한 것

1. **P0 소유 서버 시험 파일 수정 인정 여부** (P1·P2·P3·P4 공통; P2 는 `server/test/unit/{slots,contract-limiters}.test.ts`·`api/auth.test.ts`·`cron/cleanup.test.ts` 도 고침 — 빈 몸 POST 400·허브 모르는 경로 404·리미터 이름 정규식 `RL_[A-Z_]+`): `server/test/api/cards.test.ts`(P1 leet 데이터 교체, P3 `archCardsLoose()` 2줄, P4 api 데이터 교체), `server/test/realtime/collab-hub.test.ts`·`collab-kick.test.ts`·`server/test/api/doc-versions.test.ts`·`server/test/unit/collab-reducer.test.ts`(P3 `archRulesOff()` 2줄, P4 `req:` id·apicol 2MB 케이스의 `validateOp` 끄기). 색인에 "한 줄 호출" 예외로 받을지, P0 이 시험 전용 카드·문서 종류를 만들지.
2. P1: chat `p.repaint()` 줄을 P0 커밋으로 옮기기 / 문제를 열 때 패널 nvim 자동 실행 여부 / `internal/third_party/goeditor` MIT 사본 수용 / goeditor 안 vim `r`·`s` 포기 / 하루 2회 fetch 예외.
3. P1: `contract_limiters_test.go`(P0 파일)의 "행에 리미터 둘이면 클라이언트 것이 그중 하나면 통과" 변경 수용.
4. P2: 대기열·참가 요청 키 `ctrl+\ enter`(설계서 B6.7 은 상태줄 `enter`) / 오버레이 추론 대 P0 신호 / DM 칸 미구현 / 판단 4번 묶음(초대를 알림함 행으로 안 만듦, `notify.workspace` 설정 줄 대신 팔레트, 친구 프로필 "공유 중" 대신 초대 목록, claude 에 늘 `--settings`) / 색인 4절 11행에 "자동 모드에 대한 같은 설정 유무" 추가(P0 Task 31).
5. P3: 가격표 대조, 5ms 예산, 끄기 전 정리 등록부(P0), `✎n`·댓글 탭, 일일 도전 허브 행(P0 확장점), 계획 서명 문구.
6. P4: curl 왕복 해석, 본문 61,440 바이트 한도, `folder` 개수 한도.
7. 통합: P2 절 번호를 api.md 5.26·data-model.md 3.15 로 옮긴 것(아래 내 판단).

## 통합 내 판단

- 내 판단: 병합 순서는 "깨끗이 끝난 순서" 를 각 줄기 브랜치의 마지막 커밋 시각(리뷰 반영까지)으로 읽어 P4 → P1 → P2 → P3 로 했다. P1 Task 24 리뷰와 P3 1묶음 리뷰 보고는 "시험 대기 중" 으로 끝났지만, 브랜치에 리뷰 반영 커밋(`2391c975`, P3 묶음별 커밋)이 있고 워크트리가 깨끗했으며 통합 트리 전체 시험이 통과해 막는 문제로 보지 않았다.
- 내 판단: api.md 5.25·data-model.md 3.14 는 먼저 머지한 P1 이 갖고, P2 를 5.26·3.15 로 옮겼다(색인 2절 "뒤에 머지하는 쪽이 rebase"). P2 코드·시험·주석의 교차 참조는 같은 머지 커밋에서 고쳤고, 마이그레이션 `0007_workspace.sql` 은 첫 줄 주석만 바뀌었다(SQL 문 그대로 — 아직 어디에도 적용되지 않은 새 파일).
- 내 판단: CA 5.6 의 서로 다른 자리표시 행(`leet.*, git.*` / `ws.*` / `arch.*` / `api.*`)은 "덧붙이기 행" 이 아니라 각 줄기가 채울 자리이므로, 상세 행이 있는 쪽만 남겼다.
- 내 판단: CA 3.3 의 `[tools.arch]` 주석 줄을 `[tools.api]` 키들 뒤에 붙이면 읽는 사람이 api 절의 키로 오해하므로 그 절 앞에 두었다.
- 내 판단: 통합 트리에서 깨진 시험이 없어 통합 수정 커밋은 없다. 서버 vitest 는 Go 시험과 동시에 돌리면 5초 시간 초과가 난 적이 있어(P3 보고) 단독으로 돌렸다.

## 네 줄기의 "내 판단" 전부

각 줄기 `exec-log/P{1..4}.md` 에서 "내 판단" 이 들어간 줄과 "내 판단" 제목 아래의 항목을 그대로 옮겼다(긴 줄도 자르지 않음). 원문의 위치는 굵은 글씨의 절 이름.

### P1 (`exec-log/P1.md`)


**1묶음 (Task 1~6 — 서버 leet 뼈대 + Go `internal/api`) / 내 판단**

- 내 판단: 계획의 api.md 새 절 번호 "5.24 LeetCode" 는 P0 방 문서가 이미 5.24 라서 **5.25** 로 했다. data-model.md 의 "3.12 LeetCode" 도 3.12(카드)·3.13(방 문서)가 이미 있어 **3.14** 로 했다.
- 내 판단: P1 이 자리표시 스키마를 실제 스키마로 바꾸면서 P0 의 `server/test/api/cards.test.ts` 가 `leet.daily`·`leet.solve` 에 넣던 임시 data(`{slug}`, `{ms:3}`, `{}`)가 스키마에 걸려, 그 시험의 data 만 P1 스키마에 맞는 값으로 바꿨다(검사 내용은 그대로, Files 목록 밖).
- 내 판단: 서버 `contract-limiters` 시험은 라우트 handler 안의 `checkLimit(c.env, "RL_MESSAGE", …)` 도 6장 행에 있어야 통과한다. 그래서 6장 행은 계획의 `RL_SOCIAL` 하나가 아니라 `PUT /v1/leet/daily/{date}` = `RL_SOCIAL, RL_MESSAGE`, `PUT/DELETE /v1/leet/solves/{date}` = `RL_SOCIAL, RL_MESSAGE(PUT)` 로 적었다. Go `contract_limiters_test.go` 는 "행에 리미터가 여럿이면 실패" 였는데, "harness 의 리미터가 그 가운데 하나면 통과" 로 고치고 leet case 를 대조 목록에 더했다(Files 목록 밖). Go 메서드의 `request.Limiter` 는 늘 세는 `RL_SOCIAL`.
- 내 판단: `checkLimit` 은 lint 상 `src/routes` 의 등록 호출 안에서만 부를 수 있어, `ensureDailyRoots(rt, reporterId, date, facts, opts)` 에 `opts.beforeCreate`(첫 루트를 실제로 만들기 직전 한 번)와 `opts.spaceIds` 를 더하고 라우트가 그 훅 안에서 `checkLimit` 을 부른다. (리뷰 반영 뒤: `upsertSolveCards` 의 `beforeWrite` 훅은 없앴다 — 풀이 PUT 은 공유할 방이 있으면 라우트가 쓰기 전에 `checkLimit` 을 한 번 부른다.)
- 내 판단: `createTextChannel(rt, spaceId, actorId, input)` 의 `input` 에 계획의 `{name, topic?}` 외에 선택 `isPrivate`·`memberIds` 를 더했다 — 기존 `POST /v1/spaces/{id}/channels` 의 비공개 채널 동작(멤버 확인 404, channel_members, 이벤트 대상)을 그대로 옮기려고. 역할 검사는 호출자(두 라우트 모두 admin). 이름 충돌 409·한도 422·이벤트·system 메시지는 그대로.
- 내 판단: #daily 켜기의 채널 고르기 순서 = `channel_id`(그 방의 살아 있는 공개 채널, 아니면 404) → 전에 기억한 채널(살아 있고 공개) → 그 방의 공개 `daily` → `createTextChannel` 로 새 `daily`. 동시에 두 번 켜서 409 가 나면 생긴 `daily` 를 다시 찾아 쓴다. 비공개 `daily` 만 있으면 409 그대로. 같은 채널로 다시 켜면 `enabled_by`·`enabled_at` 은 처음 값 유지, 끄면 `channel_id` 를 기억해 다시 켤 때 같은 채널을 쓴다. 한 번도 안 켠 방을 끄면 빈 행(`enabled:false`, 나머지 null).
- 내 판단: 루트 경합에서 다른 요청이 막 차지한(NULL) 행은 2초까지 25ms 간격으로 기다리고, 그래도 메시지가 없으면 그 방을 `roots` 에서 뺀다. 루트 메시지가 지워졌으면(운영자·방장 삭제) 30초 넘은 NULL 행처럼 `(message_id, claimed_at)` 비교로 넘겨받아 새 루트를 만든다(하루에 살아 있는 루트 하나).
- 내 판단: 사실 보고·풀이 기록의 `date` 오류(그제·내일·형식·없는 날)는 `400 invalid_request fields.date = "invalid"`. 사실 보고 본문은 엄격(zod strict) — `content`·`html` 같은 키는 `fields.request = "invalid"`. `title`·`tags` 는 NFC+trim 뒤 code point 로 센다.
- 내 판단: 풀이 기록 `date` 는 오늘·어제(UTC)만 받는다(자정 직후 제출용). `DELETE` 는 실제 달력 날짜면 아무 날이나, 기록이 없어도 `204`.
- 내 판단: 공유할 방이 있는데 그날 `leet_daily` 가 없으면 `404`(daily) — 클라이언트가 보고를 먼저 한다. 그 방의 루트를 다른 요청이 아직 만드는 중이라 2초 뒤에도 없으면 `503 service_unavailable`(다시 시도). 검사(방·사실·카드 스키마·16KB·공유 시 `RL_MESSAGE`)는 모두 쓰기 전에 해서 실패하면 아무것도 쓰지 않는다(카드 data 는 `validateCard` 로 미리 검사). 503 만은 기록·앞선 카드가 남는 부분 쓰기이고 다시 보내면 멱등(api.md 5.25 에 적음).
- 내 판단: 재PUT 은 계획대로 `lang`·`runtime_percentile`·`submission_id` 만 바꾸고 `slug`·`passed_at` 은 첫 기록 값. 카드의 `frontend_id` 는 요청 값(`leet_solves` 에 열이 없음), `code.lang` 은 기록의 `lang`. `share_space_ids` 를 안 준 재PUT 은 이미 있는 카드를 건드리지 않는다.
- 내 판단: 방 순위 `solved` = 그 방 #daily 채널의 살아 있는 내 `leet.solve` 카드의 서로 다른 `card.data.date` 수(기간 필터도 `card.data.date`), 기간 안 카드가 없으면 목록에서 뺀다. `streak` = 그 사람의 전체 기록 기준 현재 연속. `period` 생략은 `week`(A10.4 오버레이의 첫 칸). (리뷰 반영 뒤) #daily 를 끄면 순위는 `items: []` — api.md 5.25 의 "#daily = enabled = 1 + 채널 살아 있음" 정의를 따른다. 정지 판정은 `suspended_until` 이 지나면 활성(기존 `suspendedNowSql` 과 같은 경계).
- 내 판단: 친구 순위는 연속 0 인 친구도 싣고(공유를 켠 친구 전부 + 나), 지금 정지된 친구는 뺀다. 정렬 `current` → `best` → `handle`. 내가 공유를 꺼도 내 화면에는 나를 싣는다.
- 내 판단: `Settings.leet_share_with_friends` 의 api.md 2장 행은 Task 1 이 아니라 서버가 실제로 그 값을 내보내는 Task 5 커밋에 넣었다.
- 내 판단: 계획 Task 3 의 "리액션 PUT → 200" 은 기존 계약(5.12) 대로 `204` 로 시험했다.
- 내 판단: `leet-no-content` 시험은 D1 의 LIKE 패턴 길이 제한(`LIKE or GLOB pattern too complex`) 때문에 `instr()` 로 찾고, FTS5 shadow 표(`*_fts_*`)는 BLOB 이라 뺐다. 검색이 실제로 찾는지는 저장된 제목이 `leet_daily.title` 에서 잡히는 것으로 확인한다.
- 내 판단: Go `UpdateSettingsRequest.LeetShareWithFriends` 는 계획의 `*bool` 대신 같은 구조체의 다른 칸과 같은 `Opt[bool]`(`omitzero`)로 했다. `LeetSolveRequest` 의 선택 칸은 nil·빈 값이면 보내지 않는다. `ReportLeetDaily` 는 `Tags` 가 nil 이면 `[]` 로 보낸다(서버는 배열 필수).
- 내 판단: `client-architecture.md` 는 계획의 4.3.2·4.3.3 대신 v0.1.3 추가 절 4.3.4 에 LeetCode 블록으로 더했다(P0 의 Doc 블록과 같은 자리).

**1묶음 (Task 1~6 — 서버 leet 뼈대 + Go `internal/api`) / 리뷰 반영 (P1 review)**

- 내 판단: 리뷰 지적 3(서명이 계획과 다름)은 코드를 계획에 맞추지 않고 위 실제 서명을 기록하는 쪽으로 했다 — `LeetDailyRow` 를 받아야 직렬화(`first_reporter` 조회) 없이 저장된 사실로 카드를 만들 수 있고, 카드 data 를 라우트가 미리 만들어야 쓰기 전에 `validateCard` 로 16KB 를 검사할 수 있다.
- 내 판단: 풀이 DELETE 에서 내보내진 방의 카드도 지운다 — api.md 5.25 가 "그날 내 카드를 모두" 지운다고 하고, 작성자 본인의 카드이며, 남겨 두면 이 엔드포인트로는 영영 못 지운다.

**2묶음 (Task 7~12 — 운영자 `leet-daily` + UI 없는 `internal/leetcode`) / 내 판단**

- 내 판단: Task 12 를 Task 9 앞에 커밋했다. Task 9 의 `Options.Creds *Credentials` 가 Task 12 의 타입이라, 9 를 먼저 하면 자리표시 타입을 만들어야 한다(계획: 스텁 금지). 커밋은 Task 마다 하나 그대로.
- 내 판단: 운영자 수정은 아무도 보고하지 않은 날에도 받는다(upsert — `first_reporter: null`, `first_reported_at` = 지금). 보고된 날이면 `first_reporter` 는 그대로 두고 사실·`mismatch_count = 0` 만 바꾼다. `date` 는 오늘·어제 제한 없이 실제 달력 날짜면 된다(과거 날 수정용). 루트만 새로 쓰고 **풀이 카드(`leet.solve`)의 `slug`·`frontend_id` 는 그대로** 둔다(계획 범위 = 루트). 지워진 루트는 건너뛰고, 조회와 쓰기 사이에 지워진 것(404)도 넘긴다.
- 내 판단: 서버 로직은 Files 목록대로 `routes/admin.ts` 안에 SQL 로 두었다(`services/leet.ts` 의 `loadDaily`·`dailyCardData`·`serializeLeetDaily` 와 `services/messages.ts` 의 `updateCardMessage` 를 import 만). api.md 6장 행 `PUT /v1/admin/leet/daily/{date} | admin | ` (추가 리미터 없음 — 다른 운영자 경로와 같음).
- 내 판단: (리뷰 반영) client-architecture.md 에 `AdminSetLeetDaily` 를 더했다 — 5.22 admin 블록(P0 소유)이 아니라 Task 6 이 만든 P1 의 LeetCode 블록(4.3.4) 끝에, 줄 주석으로 api.md 5.22 를 가리킨다(색인의 소유 규칙: P1 은 자기 행만).
- 내 판단: `masil admin` 의 맨 위 `Usage` 상수(한 줄 사용법)는 바꾸지 않았다 — `TestAdminHelp` 가 `Usage` 와 같기를 보고, 한글 리터럴을 고치면 래칫 셈과 무관하지만 카탈로그 밖 문구를 늘리는 셈이라. `leet-daily` 의 사용법·도움말(`-h`)·사용법 오류는 모두 `i18n.For(i18n.Ko).Leetcode.AdminUsage()` 한 줄로 내고, 이유 문장은 따로 찍지 않는다(오류 값에 영어 detail). admincli 래칫 77 그대로.
- 내 판단: `leet-daily` 는 `subs` 에 npos 0 으로 넣고 `set <date>` 낱말을 leet.go 가 직접 읽는다(공용 `usage` 가 맨 위 Usage 를 찍기 때문). 검사(날짜 실제 달력·`--id` `^[0-9]{1,6}$`·`--slug` `^[a-z0-9-]{1,100}$`·`--title` 공백 아님·`--difficulty` Easy|Medium|Hard)는 모두 요청 전. `--tags` 는 쉼표로 나눠 앞뒤 공백·빈 칸을 버리고, 없으면 `[]`. 출력의 날짜·slug 는 서버 응답 값(Sanitize).
- 내 판단: admincli 는 `internal/leetcode` 를 import 하지 않고 같은 정규식을 따로 둔다(Task 7 이 Task 8 보다 먼저라 — 래칫·import 규칙엔 영향 없음).
- 내 판단: `internal/api/path_test.go` 의 `pathCalls` 에 `AdminSetLeetDaily` 행을 더했다(`TestPathCallsCoverEveryPathMethod` 가 요구 — Files 목록 밖, Task 6 과 같은 이유).
- 내 판단: `SolutionPath` 는 slug 외에 날짜(`YYYY-MM-DD` 실제 날짜)·번호(`FrontendIDRe`)·확장자(`^\.[a-z0-9]{1,8}$`)도 검사해 하나라도 틀리면 `ErrBadSlug`.
- 내 판단: `TestNoForbiddenImports` 는 `./internal/lazygit/...` 폴더가 있을 때만 그것도 훑는다(Task 14 전에는 폴더가 없어 `go list` 가 실패하므로). 테스트 전용 import(`secrettest` → `apitest` → `api`)는 `go list -deps`(시험 제외) 에 안 잡히므로 `creds_test.go` 가 `secrettest` 를 써도 된다. `leetcodetest` 는 표준 라이브러리만 쓴다.
- 내 판단: 비밀값 키는 계획대로 `CredNS.Key(name)` 가 만든다(`secrets.Key{Tool:"leet", Server, UserID, Name}`). P0 는 "키는 언제나 `ctx.SecretKey`" 라 했으므로 UI(Task 21)는 `ctx.SecretScope()` 의 `(server, userID)` 로 `CredNS` 를 채운다 — 같은 키가 된다(`ToolID = "leet"`).
- 내 판단: `NormalizeCookie` 는 공백·줄바꿈·제어 문자를 **값 안에서도** 모두 지운다(쿠키 값에는 공백이 없다). 순서: 공백 제거 → `name=` 접두(대소문자 무시) → 첫 `;` 뒤 버림 → 감싼 `"`/`'` 제거. 환경변수 값도 같은 정규화를 거친다. `Mask` 는 P0 `Secret.Masked()` 를 그대로 쓴다.
- 내 판단: `SaveCredentials` 는 세션·CSRF 가 비면 오류(저장 안 함), `cf_clearance` 가 비면 저장돼 있던 것을 지운다(옛 값이 남아 새 세션과 섞이지 않게). `DeleteCredentials` 는 셋 다 지우고 오류를 모은다. `LoadCredentials` 는 `getenv` 가 nil 이면 환경을 보지 않고, 저장소에서 `cf_clearance` 만 없으면 괜찮다.
- 내 판단: `Question(slug)` 와 오늘의 문제는 **두 번 묻는다** — 먼저 메타 정보만(`content`·`codeSnippets`·`exampleTestcaseList` 를 이름조차 넣지 않은 질의), 프리미엄이 아니면 본문 질의 한 번. 그래서 "GraphQL 요청 1건" 은 `activeDailyCodingChallengeQuestion` 질의 1건(+ 본문 1건)으로 시험한다. 프리미엄 오늘의 문제는 오류 없이 메타 정보만(`ContentHTML == ""`, UI 가 🔒), `Question(slug)` 은 메타 정보 + `ErrPremiumOnly`. 두 질의 사이에 프리미엄으로 바뀌면 받은 본문을 버린다.
- 내 판단: (리뷰 반영) `UserStatus` 는 쿠키를 보냈는데 `isSignedIn:false` 면 상태(`SignedIn:false`)와 함께 `ErrAuthExpired` 를 돌려준다(계획 "`isSignedIn:false` 로 실행 시도" = 만료). 쿠키 없이 물으면 오류 없이 로그아웃 상태. 쿠키가 아예 없을 때의 실행·제출은 그대로 `ErrNotSignedIn`(보내지 않음). UI(Task 21)는 로그인 확인·실행 전 확인 모두 `ErrAuthExpired` → 로그인 오버레이 다시.
- 내 판단: 클라이언트는 **리다이렉트를 따라가지 않는다**(`CheckRedirect = ErrUseLastResponse`) — Go 는 다른 호스트로 갈 때 `Cookie` 는 빼지만 `x-csrftoken` 같은 사용자 헤더는 넘기므로. 3xx 는 `/accounts/login` 으로 가면 `ErrAuthExpired`, 아니면 `ErrUnexpected`. `Options.HTTP` 는 복사해서 고친다(호출자 것은 그대로).
- 내 판단: 오류 분류 순서 = 3xx(`/accounts/login` 이면 `ErrAuthExpired`, 아니면 `ErrUnexpected` — Django 로그인 리다이렉트는 `text/html` 빈 본문이라 HTML 검사보다 먼저, 리뷰 반영) → HTML(`text/html`·`application/xhtml+xml` 또는 본문 첫 글자 `<`, 3xx 아닌 모든 상태) → 429 → 401/403 → 그 밖 비 2xx(`ErrUnexpected`) → 2xx 빈 본문(`ErrAuthExpired`, 설계서 "빈 응답 = 쿠키 만료"). GraphQL `data:null`·JSON 아님·4 MiB 초과·없는 문제·응답 slug/번호가 정규식에 안 맞음(`ErrBadSlug`)·물은 slug 와 다른 문제는 오류. 자동 재시도 없음(시험이 요청 1건 확인).
- 내 판단: 디버그 로그는 메서드·경로·상태·길이와 `cookie`/`x-csrftoken` 이 있으면 `(redacted)` 라는 표시만 남긴다(값 0번). `Referer` 는 `<BaseURL>/problems/<slug>/`(실제로는 `Origin`), 문제와 상관없는 질의는 `<BaseURL>/`.
- 내 판단: 가짜 서버는 기본으로 오늘의 문제 = `daily_1606.json`, `two-sum` 열림, 로그아웃 상태, 실행 = `interpret_ok.json`, 제출 = `submit_ok.json`(계획 목록 밖 픽스처 하나 추가 — `SetSubmit(fixture)` 의 기본값). 오늘의 문제 픽스처의 문제는 slug 로도 열린다. 질의가 이름을 대지 않은 본문 필드는 응답에서 뺀다(진짜처럼). UI 시험용으로 `SetUserStatus(signedIn, username, premium)` 와 `Fixture(tb, name)` 를 더했다. `QueueCheck` 는 순서대로 답하고 마지막 것을 계속 답하며, 아무것도 없는 id 는 404. 픽스처의 문제 본문은 LeetCode 원문이 아니라 짧게 새로 쓴 시험용 문장이다.
- 내 판단: `Interpret`·`Submit` 은 쿠키(`ErrNotSignedIn`) → 문제(slug·`QuestionID`, `ErrBadSlug`) → 프리미엄(`ErrPremiumOnly`) → 언어(표에 있는 slug) 순으로 검사하고 하나라도 걸리면 보내지 않는다. `Check` 의 id 는 `^[A-Za-z0-9._-]{1,80}$`(아니면 `ErrUnexpected`, 보내지 않음). `code_output` 은 제출이면 문자열, 실행이면 줄 배열이라 둘 다 받아 `\n` 으로 잇는다. `CompileError`·`RuntimeError` 는 `full_*` 이 있으면 그것. `submission_id` 는 숫자·문자열 둘 다 받는다.
- 내 판단: `Pacer` 는 시계가 거꾸로 가면(Mark 보다 이른 now) 간격 전체를 기다리게 한다. 상수 `FreeInterval`(10s)·`PremiumInterval`(1s)를 내보냈다.
- 내 판단: 캐시 = `<cacheRoot>/leetcode/daily/<date>.json`(`{date, question}`)·`questions/<slug>.json`, 폴더 0700(이미 있으면 chmod), 파일 0600 `config.WriteFileAtomic`. 깨진 파일·4 MiB 초과·일반 파일 아님·키와 내용(날짜/slug)이 다름·slug/번호 정규식 실패는 모두 "없음". `Prune(now, keep)` 은 mtime 기준, `keep <= 0` 이면 30일(`DefaultKeep`).
- 내 판단: `Fetcher.Today` 는 LeetCode 가 알려 준 날짜로 캐시하고 그 날짜를 돌려준다(시계와 다르면 화면이 그 날짜를 보인다). (리뷰 반영) 날짜가 시계의 UTC 날과 다르면 `fetched/<시계 날>.json`(`{day, date, at, tries}`) 표시를 남겨, 그날 뒤 호출은 요청 없이 캐시(LeetCode 날짜)로 답한다 — 시계가 하루 틀려도 하루 1회. 예외 하나: LeetCode 날짜가 시계보다 **이르면**(아직 날을 안 넘김) `LagRetry`(10분) 뒤 첫 호출에서 **한 번만** 더 묻는다(그날 최대 2회). 카운트다운 0 초에 받은 요청이 LeetCode 의 몇 초 늦은 전환에 걸려 하루 종일 어제 문제를 보이는 일을 막으려는 것 — JA27 의 "1회" 를 이 경우에만 2회로 넘는 판단이다(자동 재시도가 아니라 사용자가 다시 열 때만). LeetCode 가 시계보다 앞이면 다시 묻지 않는다. `Prune` 이 `fetched/` 도 치운다. Task 19 는 카운트다운 0 에서 몇 초 늦춰 묻는 편이 낫다. 받은 문제는 slug 캐시에도 넣어 `Open` 이 네트워크 없이 연다. 캐시 쓰기 실패는 화면을 막지 않는다(무시). 프리미엄은 메타 정보만 캐시하고 다시 열어도 `ErrPremiumOnly`.
- 내 판단: `cmd/masil` `TestImportGraph` 에 "`internal/api` 는 `leetcode` 를 import 하지 않음" 을 더하는 일과 client-architecture.md 2.1·2.2 패키지 행은 이 묶음 Files 에 없어 하지 않았다(Task 24 의 import 규칙·통합 몫으로 남김).

**2묶음 (Task 7~12 — 운영자 `leet-daily` + UI 없는 `internal/leetcode`) / 실제 내보낸 겉모양 (Task 13 이후는 이것을 쓴다)**

- 내 판단: Task 7 커밋의 `internal/admincli/leet_test.go` 한 줄이 gofmt 에 걸려(한 줄 함수 리터럴) 이 마지막 커밋에서 gofmt 로 고쳤다(내용 변화 없음).

**3묶음 (Task 13~17 — htmldoc 변환기 + lazygit) / 내 판단**

- 내 판단: `htmldoc.Block.Inline` 을 계획의 `[]markup.Inline` 대신 htmldoc 자체 타입 `Inline{Kind InlineKind; Text string}` 으로 했다. `internal/markup` 이 `internal/api` 를 import 해서(`render.go` 의 `api.MentionRef`) `markup` 을 쓰면 `TestNoForbiddenImports`(leetcode → api 금지)가 깨진다. 종류 값은 `markup` 과 같게(`Text`·`Bold`·`Italic`·`InlineCode` = 0~3) 두어 UI(Task 19)는 `markup.InlineKind(k)` 로 바꾸면 된다 — `TestInlineKindsMatchMarkup` 이 값이 같은지 본다(시험 전용 import 는 규칙에 안 걸림).
- 내 판단: htmldoc — `script`·`style` 안은 글자도 버린다(설계서 "글자만" 은 표시용 태그 이야기로 봄), 주석·`<!doctype>` 도 버린다. `<br>` 은 블록을 끊고, 한 `li` 에서 첫 블록 뒤(br 뒤·안쪽 `p`)는 같은 `Depth` 의 `Para`. `<ol start>` 를 따른다. `&nbsp;` 만 있는 블록은 내지 않는다. `Pre` 는 앞뒤 빈 줄을 빼고 줄 끝 공백을 지우며 탭은 공백 4칸. `pre` 안 `img` 는 무시. `<p>` 안 `<p>`, 열린 `li` 뒤 `li` 는 앞을 닫는다. 끝나지 않은 태그는 버리고, 끝나지 않은 `sup` 글자는 그대로 낸다. `img` 의 URL 은 assets 접두 + 인쇄 가능한 ASCII(따옴표·공백·`<>`·`\` 없음) + 2048자 이하일 때만. `sup` 안 공백은 다듬고, 바꿀 수 없으면 괄호 없이 `^n+1`. `MaxInput`(1 MiB)·`MaxDepth`(3) 를 내보냈다.
- 내 판단: `lazygit.Version` 은 `lazygit --version` 의 첫 `version=`(줄 처음 또는 쉼표 뒤)만 읽는다 — 실제 출력 끝의 `git version=2.x` 를 잡지 않으려고. 값은 숫자로 시작, 32자 이하.
- 내 판단: `Locate` 는 `[tools.lazygit] path` 의 앞 `~` 를 홈으로 펴고 `LookPath` 로 실행 파일인지 본 뒤 절대 경로로 돌려준다. `RepoRoot` 도 `~` 를 펴고, git 이 준 최상위를 `EvalSymlinks` 한다(실패하면 그대로).
- 내 판단: `ResolveRepo` 에서 설정 `repo` 가 저장소가 아니면 오류로 멈추지 않고 2단계(시작 폴더)로 넘어간다(설계서 "첫 일치"). `err` 는 ctx 오류뿐. 시작 폴더는 `os.Getwd()`.
- 내 판단: `config` 에 `DefaultLeetToolConfig()`·`DefaultLazygitToolConfig()`(`Open:"auto"`)를 더했다 — `ToolSection` 은 없는 키를 건드리지 않으므로 기본값에서 시작해 읽는다. 계약 3.3 에 "P1 도구 키" 블록(키·기본값·Go 타입)을 P0 문단 아래 덧붙였다.
- 내 판단: `UserConfigFiles` — 빈(공백뿐) `LG_CONFIG_FILE` 은 없는 것으로 본다. `-cd` 출력이 비었거나 여러 줄이면 `ok=false`(덧씌우지 않음). `config.yml` 은 일반 파일일 때만.
- 내 판단: 덧씌움 YAML 의 네 키는 `activeBorderColor`·`inactiveBorderColor`·`selectedLineBgColor`·`optionsTextColor`(각각 색 하나짜리 목록, `bold` 같은 속성도 넣지 않음 — 색만). 파일 이름은 내용 sha256 앞 12자, 내용이 같으면 다시 쓰지 않는다. 색은 `#rrggbb` 만 받는다(YAML 주입 막기). `cacheDir` 가 비었거나 덧씌움 경로에 쉼표(목록 구분자)가 있거나 쓰기에 실패하면 덧씌우지 않는다.
- 내 판단: 허브 상태(`Status`)는 그릴 때 불리므로 lazygit 찾기를 `ctx.Now()` 기준 30초 기억하고, `--version` 은 실행 파일 경로가 바뀔 때만 다시 돈다(첫 허브 그리기에서 한 번 동기 실행, 2초 제한). 버전을 못 읽으면 `설치됨`(새 키 `hub_installed_unknown`). `ToolSpec.Available` = lazygit 을 찾음(지금 앱은 안 씀) — `Open` 은 두 경우를 모두 처리한다.
- 내 판단: `Open`(허브·팔레트 `git.open`)의 찾기·`git rev-parse` 는 돌려주는 Cmd 안에서 돈다. 허브 `m` 메뉴로 고른 방식은 `OpenTool` query `mode` 로 인스턴스에 넘긴다.
- 내 판단: 고르기 화면은 인스턴스 `git:pick` 하나다. 저장소를 고르면 `tea.Batch(ui.CloseTool("git:pick"), ui.OpenTool("git", "git:"+repo, query))`(`tea.Sequence` 는 UI 정적 규칙이 금지). 인스턴스가 실행에 실패하면(패널 상한·lazygit 없어짐) 같은 ID 그대로 고르기 화면을 보이고, 같은 저장소를 고르면 그 자리에서 다시 실행, 다른 저장소면 그 저장소 인스턴스를 연다 — 계획의 "인스턴스는 picking 유지".
- 내 판단: 경로 입력 칸의 `tab` 은 앱의 하드 전역 키라 `ui.Completer`(`Completing()` = 입력 중)를 구현해 먼저 받는다. 자동 완성은 디렉터리(디렉터리를 가리키는 링크 포함)만, 숨은 폴더는 입력이 `.` 으로 시작할 때만, 하나면 이름 + 구분자, 여럿이면 공통 앞부분.
- 내 판단: 설치 안내 오버레이 — `y` 는 `ui.Copy`(OSC 52 + 시스템 클립보드, 토스트 `install_copied`), Windows 의 두 명령은 `↑↓` 로 고른다, `enter`·`esc`·`q` 닫기. JA29 "시험한 버전 표시" 를 따라 `masil 은 lazygit v0.66.3 으로 시험했어요` 줄을 넣고, macOS·Windows 밖에서는 배포판 안내 문장(`install_linux`)과 공식 주소를 보인다. 크기 64×12.
- 내 판단: 인스턴스 제목은 계획의 "폴더 이름" 대신 `lazygit · <폴더 이름>` — 설계서 A8.2 그림(`🌿 lazygit · masil`)과 A8.3 맥락 줄 모양을 따랐다(폴더 이름만 보이는 원칙은 그대로).
- 내 판단: 덧씌움 여부를 정하려면 실행 전에 방식을 알아야 해서, 인스턴스가 `termhost.Choose(req, termhost.CurrentEnv(cfg, "git", tiny))` 로 미리 정한다(시험 이음매 `SetTermEnvForTest` — termpanetest 의 `SetEnv` 와 같은 환경을 준다). `Request.Mode` 는 원한 방식(허브 메뉴, 아니면 `cfg.ToolOpen("git")`) 그대로 넘겨 Launch 의 "줄어든 방식" 토스트가 맞게 나온다. 그래서 `TestPaneThemeOverlay` 의 exec 확인은 `Started()[0].Mode` 가 아니라 인자에 `--use-config-file` 이 없고 화면이 `화면 전환으로 실행 중이에요` 인지로 본다.
- 내 판단: 최근 목록은 실행에 성공했을 때(모든 방식)와 OSC 7 이 다른 저장소를 알릴 때 `PushRecent` 한다. 고르기 화면 자체는 쓰지 않는다.
- 내 판단: 화면 전환·팝업으로 실행 중인 인스턴스는 `{방식}으로 실행 중이에요` 한 줄, 끝나면 `종료됨 · enter 다시 열기` + 흐린 `x 닫기`. 패널은 `SingleCell: true`(`ctrl+\ z` = 넓게 보기). 허브에서 닫으면(`Closer`) 살아 있는 패널의 Handle 을 `Close`. 패널 상한은 Launch 의 토스트만, 그 밖 실행 오류는 `launch_failed` 토스트를 더한다. 실행 직전 lazygit 이 없어졌으면 설치 안내를 띄우고 고르기 화면.
- 내 판단: homebrew caveats — P5 Task 21(영어+한국어 caveats)이 아직 이 브랜치에 없어 지금 형식(한국어 줄)에 맞춰 `lazygit 도구를 쓰려면: brew install lazygit` 한 줄만 더했다. P5 Task 21 뒤 rebase 때 계획의 두 줄 형식(`To use the lazygit tool: brew install lazygit / lazygit 도구를 쓰려면: brew install lazygit`)으로 맞춘다.
- 내 판단: `cmd/masil/importgraph_test.go` 의 `toolPackagePrefixes` 에 `internal/lazygit` 을 더하는 일은 2묶음과 같은 이유(이 Task 들의 Files 밖)로 Task 24 몫으로 남겼다. `internal/leetcode` 의 `TestNoForbiddenImports` 는 폴더가 생겨 이제 `./internal/lazygit/...` 도 훑는다(통과).

**3묶음 (Task 13~17 — htmldoc 변환기 + lazygit) / Task 13~17 리뷰 반영**

- 내 판단: 고르기 화면(`git:pick`)과 고르기로 돌아간 인스턴스가 저장소를 고르면 이제 `ui.CloseTool` 만 내고, 여는 `ui.OpenTool` 은 그 컴포넌트의 `Closer.CloseTool` 이 돌려준다. 앱은 닫기를 처리해 자리를 비운 뒤에 그 Cmd 를 돌리므로, 도구 표면이 가득(인스턴스 5개)이어도 열기가 거절되지 않는다(`tea.Batch` 는 순서가 없고 `tea.Sequence` 는 UI 규칙이 금지). 위 "`tea.Batch(ui.CloseTool…, ui.OpenTool…)`" 판단을 대신한다. 시험 `TestPickerOpensOnFullSurface`·`TestInstancePickOtherOnFullSurface` 는 메시지를 거꾸로 배달해 본다.
- 내 판단: 허브 상태의 lazygit 찾기는 더 이상 View 안에서 돌지 않는다. `Status`·`Available` 은 마지막 답만 읽고, 답이 없거나 30초가 지났으면 고루틴에서 찾은 뒤 `ctx.Post.Coalesce("gittool.probe", probedMsg{})` 로 다시 그리게 한다(받는 컴포넌트는 없다 — 메시지가 다시 그리기만 일으킨다). 첫 답이 오기 전 상태 칸은 비어 있다(새 문구를 더하지 않음). `--version` 은 실행 파일마다 한 번만(못 읽은 답도 기억). 위 "첫 허브 그리기에서 한 번 동기 실행" 판단을 대신한다. 시험은 `gittool.WaitProbeForTest` 로 찾기가 끝나길 기다린다. `TestGitGolden/install` 은 허브를 한 번 그려 찾기를 끝낸 뒤 `4` 를 누르도록 바꿨다 — 골든 파일은 그대로.
- 내 판단: `lazygit.SystemEnv().Run` 에 `cmd.WaitDelay = 500ms` — 제한 시간이 지나면 남은 자식이 출력을 쥐고 있어도 `Output` 이 돌아온다.
- 내 판단: `LaunchArgs` 는 `-cd` 로 찾은 사용자 설정 경로에 쉼표가 있어도 덧씌우지 않는다(목록이 쉼표로 나뉘어 lazygit 이 바로 끝나므로).
- 내 판단: `htmldoc.Convert` 는 같은 모양 글자를 `strings.Builder` 로 이어 붙인다 — 바꾸는 것 없는 태그로 잘게 나뉜 1 MiB 입력에서 몇 초·수 GB 이던 것이 선형이 됐다(`TestConvertLinearInShortRuns`, 할당 상한으로 본다).
- 내 판단: 인스턴스 자체 화면(준비 중·다른 곳에서 실행 중·종료됨)도 끝을 `textwidth.Fit` 으로 맞춘다(출력은 같다).
- 내 판단: homebrew caveats 에 계획의 두 줄 형식 중 영어 줄(`To use the lazygit tool: brew install lazygit`)을 지금 더했고, `TestFormulaCaveatsMentionLazygit` 가 두 줄을 모두 본다. P5 Task 21 뒤 rebase 에서 충돌을 어느 쪽으로 풀어도 두 줄이 남아야 시험이 통과한다. **후속(열려 있음):** rebase 때 영어 줄이 P5 의 영어 caveats 묶음 안에 놓이도록 순서를 맞춘다.
- 내 판단: 계획 Task 13 Interfaces 와 Task 19 Step 3 에 `Block.Inline` 이 `[]htmldoc.Inline` 이고 `markup.InlineKind(in.Kind)` 로 바꾼다는 줄을 더했다.

**4묶음 (Task 18~23 — `ui/leet` 도구) / 내 판단**

- 내 판단: 허브 줄은 네트워크를 부르지 않는다 — 이 실행의 기억(마지막 LeetMe)과 로컬 캐시만 읽고, 오늘 캐시가 없으면 30초마다만 다시 본다. 문구는 `hub_status_short`(연속 모름)·`hub_not_loaded`.
- 내 판단: 첫 실행 안내는 인스턴스 `Init` 에서 연다. 골든은 홈 위에 오버레이를 연 모습.
- 내 판단: `leet.board` 팔레트 명령은 Task 18 에서 등록하고 오버레이는 Task 23 에서 만들었다.
- 내 판단: 설정 창의 #daily 켜짐 표시는 `LeetMe.DailySpaces` 에서 온다(켜고 끈 답도 기억에 반영).
- 내 판단: `instance.go` 는 Task 18 에 뼈대로 먼저 만들었다. 시험은 카드 화면 때문에 `ui/chat` 을 import 한다.
- 내 판단: 남은 시간 머리줄은 테두리 제목이 아니라 본문 첫 줄이다.
- 내 판단: 120×40 에서 인스턴스 안쪽 폭이 92 라 나눔이 40|53 이 아니라 38|53 이다(`splitFor` 자체는 표를 따른다).
- 내 판단: `o` 는 셸 없이 `goeditor.OpenWithSystem` 으로 열고, 실패하면 링크를 복사한다(core 에 링크 열기 경로가 없음).
- 내 판단: 가짜 LeetCode 에 `SetDailyDate` 를 더했다(Files 목록 밖, `leetcodetest`).
- 내 판단: 문제 본문의 그림은 그리지 않고 `[그림] o 열기` 링크 줄과 alt 글만 보인다.
- 내 판단: 자정(UTC)을 넘기면 다음 분 경계에 깨어나 하루에 한 번만 다시 받는다.
- 내 판단: 불러오는 중·실패 상태 줄은 전체 폭을 쓴다(38칸 왼쪽에서 접히지 않게).
- 내 판단: goeditor 의 일반 모드에서 `esc` 는 편집기를 떠난다(그 뒤 `r`·`s` 는 도구 키) — vim 의 `r`·`s` 를 덮어쓰지 않으려고.
- 내 판단: `:w` 는 풀이 파일에만 쓰고 경로 인자는 무시한다.
- 내 판단: 편집기는 `e` 를 누를 때 시작한다(문제 골든이 안정되게).
- 내 판단: 화면 전환 편집기는 InstanceID `id+"#exec"` 로 띄우고, 끝나면 편집기 내용을 다시 읽는다.
- 내 판단: goeditor 출력은 SGR 만 남기고 다른 제어열은 버린다.
- 내 판단: `goeditor.New` 동안 Windows 밖에서는 `os.Stdin` 을 nil 로 바꿔 둔다. Windows 는 여전히 CONIN$ 을 묻는다(오너 확인 목록).
- 내 판단: `l` 은 config `lang` 을 쓰고 열린 편집기를 닫는다(새 언어 파일로 다시 연다).
- 내 판단: 위젯 모드에서 `l`·`h` 는 하위 탭을 바꾸지 않는다(←/→ 와 1~3 만).
- 내 판단: `TestChooseEditor` 는 `layout_test.go` 에 있다.
- 내 판단: "상위 N%" = ceil(100 − runtime_percentile). 픽스처 85.31 은 설계서 예의 12% 가 아니라 15% 다.
- 내 판단: 쿠키와 간격기(pacer)는 계정별 실행 기억에 둔다. 프리미엄 여부는 `leetState.Premium` 에 둔다.
- 내 판단: 설정의 로그인 줄(describe)은 저장소를 읽지 않고 `Username != ""` 로 판단한다.
- 내 판단: 로그인을 기다리던 실행·제출은 로그인이 끝나면 이어서 보낸다.
- 내 판단: 간격·실패 안내는 전체 폭 맨 아래 줄에 보인다.
- 내 판단: 오류 내용은 컴팩트 모드에서만 기본으로 접는다.
- 내 판단: 실행·제출 요청 메시지는 인스턴스 id 를 싣는다(여러 인스턴스).
- 내 판단: 통과 결과는 공유 창이 읽도록 패키지 맵(인스턴스 id → 통과)에 둔다.
- 내 판단: 공유 창에서 `enter` 는 어느 줄에서든 보내고, `space`·`x` 가 고르기다.
- 내 판단: 위젯 모드에서 실행·제출을 보내면 결과 탭으로 넘어간다.
- 내 판단: 상태 메시지를 되돌릴 때 남은 만료 시간을 넘긴다(이미 지났으면 지운다).
- 내 판단: chat 의 `cardLines` 는 그리기 함수가 `nil` 을 돌려도 "카드로 그렸다" 로 쳐서 빈 칸이 된다(계획의 "nil → chat 이 body" 와 다름). 그래서 data 디코드 실패·날짜/slug 검사 실패·플래그 꺼짐이면 그리기 함수가 직접 `body` 를 chat 과 같은 markup 으로 그린다(위젯·초소형은 한 줄).
- 내 판단: `CardRenderer.Open` 은 등록하지 않고 `enter` 를 `Key` 에서 처리한다 — `Open` 은 런타임에 nil 이 될 수 없어서, 플래그 꺼짐·나쁜 slug 일 때 `enter` 가 chat 의 스레드 열기로 돌아가려면 `Key` 가 handled=false 를 돌려야 한다.
- 내 판단: 스포일러 확인은 `widgets.Confirm` 오버레이 대신 카드 안 두 줄(`아직 안 풀었어요. 그래도 볼까요?` + `enter 보기 · esc 그만두기`)로 했다. chat 목록은 줄을 (id, 폭, 선택) 으로 캐시해서, 오버레이 yes 처럼 비동기로 바뀐 카드 상태는 다시 그려지지 않는다. 확인을 `Key` 안에서 동기로 끝내고, chat `msgPane.cardKey` 에 "그리기 함수의 `Key` 가 처리했으면 `repaint()`" 한 줄을 더했다(Files 목록 밖 — P0 `CardRenderer` 주석의 "spoiler toggles" 를 실제로 가능하게 하는 최소 수정, chat 골든 무변화).
- 내 판단: 펼친 코드·확인 상태는 메시지 id 별 패키지 기억(이 실행에서만, 저장 안 함). 위젯·초소형 한 줄 밀도에서는 펼치지 않는다.
- 내 판단: "내가 그날 풀었나" 는 이 실행의 마지막 LeetMe 의 `Recent` 로 본다 — 아직 모르면(도구를 안 열었으면) 안 푼 것으로 보고 확인을 묻는다. 시험은 도구를 한 번 열어 LeetMe 를 받는다.
- 내 판단: 불일치 경고는 이 실행의 기억(그날 받은 문제), 없으면 로컬 캐시 `Daily(date)` 의 slug 와 카드 slug 를 비교한다. 둘 다 없으면 경고하지 않는다.
- 내 판단: `b`(순위)는 루트 카드와 풀이 카드 모두에서, 그 메시지 채널의 방으로 연다. 방을 모르면(DM) chat 에 넘긴다.
- 내 판단: 친구 순위에는 `solved` 가 없어(`LeetFriendStreak{Current, Best}`) 줄을 `{rank}. {name} · 🔥{current} · 최고 {best}일` 로 했다(새 키 `board_friend_row`).
- 내 판단: 순위 탭은 처음 보일 때 한 번만 묻고, 다시 오면 받은 것을 보인다. 방이 없이 열면 친구 탭만. 내 줄은 굵게.
- 내 판단: 시험 픽스처의 #daily 메시지는 `fixtures` 를 바꾸지 않고 시험 안에서 `GET /v1/channels/{id}/messages`·`/thread` 처리기를 바꿔 내놓는다(#카드 채널 재사용). 풀이 카드 골든은 스레드 화면.
- 내 판단: `TestOldClientFallback` 은 등록부를 비운 뒤 카드만 빼고 나머지(패널·경로·오버레이·알림 대상·도구·킬스위치)를 다시 등록한다.

**4묶음 (Task 18~23 — `ui/leet` 도구) / Task 18~23 리뷰 반영 (P1 review)**

- 내 판단: `l` 은 tools-state `lang` 에 쓴다. config.toml `[tools.leet] lang` 이 있으면 그 값이 `l` 로 고른 것을 가리므로, 그때만 그 한 키를 config.toml 에서 지운다(다른 키는 그대로). 설정 창의 언어 줄은 지금처럼 config `lang` 을 쓴다.
- 내 판단: 설정 로그인 줄의 ok 는 `ctx.Secrets.Where(LEETCODE_SESSION 키) != ""` 다(OS 저장소를 부르지 않고 색인·이번 실행 메모리만 본다). "이번 실행만" 으로 로그인했다면 다시 켠 뒤에는 로그인 안 됨으로 보인다. 같은 기기의 다른 masil 계정은 자기 쿠키가 없으면 로그인 안 됨이다.
- 내 판단: LeetCode 사용자 이름·프리미엄은 tools-state `leet.accounts["<서버 호스트>/<masil 사용자 id>"] = {username, premium}` 로 계정별로 둔다. 위의 `{"lang","first_run_done","username","premium"}` 섹션 모양은 `{"lang","first_run_done","accounts"}` 로 바뀌었다(아직 내보낸 적 없는 형식이라 옮기기 없음). [지우기]는 그 계정 항목만 지운다.
- 내 판단: 429 는 계정 실행 기억에 "10초 뒤까지 보내지 않음"(`hold`)을 두고, 간격기(1초·10초)와 둘 중 긴 쪽을 기다린다. 간격기를 미래 시각으로 `Mark` 하면 `Remaining` 이 "시계가 거꾸로 감" 으로 보고 1초만 기다리기 때문이다.
- 내 판단: 오늘 문제가 바뀌면(날짜 또는 slug) 편집기를 닫고, 끝난 결과·통과 카드(`c`)·로그인 대기·간격 표시를 지운다. 아직 진행 중인 실행·제출은 자기 문제(날짜·문제·코드)를 들고 있으므로 그대로 끝까지 받는다.
- 내 판단: 상태 메시지 자동 표시는 계정 기억에 하나(`statusHold`): 처음 연 인스턴스만 이전 상태를 저장하고 "🧩 … 푸는 중" 을 보내며, 마지막 인스턴스가 닫히거나 통과할 때만 되돌린다. 되돌릴 때 Store 의 내 상태도 바로 바꾼다(응답이 올 때 받을 인스턴스가 이미 닫혀 있을 수 있어서). 두 번째 인스턴스는 상태 글을 자기 문제 번호로 바꾸지 않는다.
- 내 판단: "친구에게 연속 풀이 보이기" 는 Store 를 낙관적으로 바꾸지 않는다. 창 안에서만 보낸 값을 보이고, 성공은 창이 열려 있으면 답으로, 닫혀 있으면 서버의 `settings.updated` 실시간 이벤트로 Store 에 들어간다. 실패 토스트는 Cmd 가 직접 낸다(창이 닫혀 있어도 뜬다). ui 에 패키지 수준 결과 처리기가 없어서 이렇게 했다.
- 내 판단: goeditor 일반 모드에서 아무 명령도 반쯤 쳐 있지 않을 때(개수·연산자·`f`/`t` 인자 없음)만 `r`·`s` 를 실행·제출로 가로챈다. vim 의 replace·substitute 는 이 편집기에서 쓸 수 없다(`cl`·`R` 등은 그대로). `esc` 를 한 번 더 누르면 편집기를 떠나는 것은 그대로다.
- 내 판단: `max` 는 ko 카탈로그에만 적는 메타다(생성기가 모든 언어에 같은 폭을 검사한다; en 파일에는 `max` 필드가 없다). 허브 40, 문제 보기 힌트 64, 위젯 힌트 56, 위젯 머리 24, 초소형 줄 40·30, 카드 한 줄 40.
- 내 판단: 인스턴스 제목은 문제를 받은 뒤 `instance_title` = `오늘의 문제 · {id}`(받기 전에는 `오늘의 문제`). `ToolStatus` 는 실행·제출 중이면 `실행 중…`/`제출 중…`, 아니면 편집기 종류(`패널 nvim`·`masil 편집기`), 편집기가 없으면 빈 칸. 이 때문에 leet 골든(문제·편집기·결과·공유)의 탭 제목이 `오늘의 문제 · 1606` 으로 바뀌었다(`-args -update`, 바뀐 줄은 제목뿐 — 다른 패키지 골든 무변화).
- 내 판단: 풀이 파일은 문제를 받은 때(`loaded`, 본문이 있는 문제만) Cmd 로 `EnsureSolutionFile` 한다. 있으면 건드리지 않고, 실패는 조용히 넘긴다(`e`·`E`·`r`·`s` 가 다시 만들며 그때 토스트). 패널 nvim 을 자동으로 띄우지는 않는다(오너 확인 목록).
- 내 판단: goeditor 는 계획의 마지막 길(고정 사본)로 갔다. 감싸기로는 막을 수 없다 — Windows 에서 lipgloss 가 `os.Stdin` 이 무엇이든 `CONIN$` 을 직접 연다. 루트 패키지 세 파일만 복사했고 `core`·`highlighter` 는 모듈에서 그대로 쓴다. 바꾼 곳은 `WithDarkBackground` 옵션과 `hasDarkBackground` 이음매뿐(`masil` 표시, README). masil 테마의 `Dark` 를 넘긴다. `go mod tidy` 로 `atotto/clipboard`·`rivo/uniseg` 가 직접 의존이 되었다(버전 그대로).
- 내 판단: 문제 그림은 `https://assets.leetcode.com/` 주소(htmldoc 이 거른 뒤 여기서 한 번 더 확인)만 쿠키 없이 받는다: 20초, 4MB, 리디렉트도 같은 호스트만, `imgrender.Decode`. 그리기는 `imgrender.HalfBlock`(가로 최대 64칸·세로 최대 20줄, 비율 유지, 키우지 않음) — kitty 전송(`Setup`) 경로는 쓰지 않는다. 그래픽 꺼짐·다른 호스트·받는 중·실패는 지금처럼 `[그림] o 열기` 링크 줄. 받은 그림은 이번 실행 메모리에만(최대 32장), 실패한 주소는 이번 실행에 다시 묻지 않는다. 시험은 `SetImageTransportForTest` 로 네트워크 없이(기본 `setup` 은 모든 그림 요청을 실패시킨다).

**5묶음 (Task 24 — 통합 확인) / 내 판단**

- 내 판단: Step 2 에서 새 시험(`TestCookieNeverLeavesPC`·`TestCookieFromEnvNeverStored`·`TestModeSwitchKeepsInstances`×2·`TestTinyOpensExec`×2)이 처음부터 통과했다 — 누출·회귀가 없어 고칠 코드가 없고, Step 3 은 계약 문서·오너 확인 표만 했다. 훑기가 헛돌지 않는지는 (1) LeetCode 가짜 서버의 제출 요청 `Cookie` 헤더에 쿠키가 실제로 실렸는지, (2) 동의한 파일 저장소에 값이 실제로 들어갔는지를 같은 시험에서 확인하고, 파일에 값을 심으면 `noValueUnder` 가 잡는 것을 따로 돌려 확인했다(커밋하지 않음).
- 내 판단: 누출 시험의 비밀값 저장소는 OS 저장소가 "쓸 수 없음"인 `secrets.Open(OS: downOS, Paths: 하네스 경로)` 로 바꾸고 동의 대화상자에서 ① 파일을 골랐다 — (b) "`secrets/` 는 동의한 파일 저장소이므로 별도로 0600 확인" 을 실제 `MASIL_CONFIG_DIR/secrets/` 에서 하려고. 폴더 0700·파일 0600(Windows 는 권한 비트를 보지 않음).
- 내 판단: (c) `slog 버퍼` 는 시험 동안 `slog.Default()` 를 Debug 수준 버퍼로 바꿔 훑는다. `ui/leet` 는 LeetCode 클라이언트에 로거를 넘기지 않아(`leetcode.Options.Log` = nil → 버림) 디버그 줄 자체가 없다 — 로거를 연결하는 것은 이번 Task 범위가 아니라 그대로 둔다(연결하더라도 `Cookie`·`x-csrftoken` 값은 `(redacted)` — Task 9 시험).
- 내 판단: (d) 골든 출력은 `internal/ui/leet/testdata` 의 모든 파일을 훑는다. 화면은 단계마다(편집기·로그인 입력·동의·실행·제출·공유·순위) 찍어 모두 훑는다.
- 내 판단: 환경변수 쿠키 경로도 같은 훑기를 한다(`TestCookieFromEnvNeverStored`): 저장소·`secrets/`·설정·캐시·풀이 폴더·요청·로그·화면·`Request.Env/Args` 어디에도 없다. 다만 `termhost`(P0)가 자식 프로세스에 `os.Environ()` 을 그대로 넘기므로 사용자가 `LEETCODE_SESSION=… masil` 로 띄우면 nvim·lazygit 도 그 변수를 상속한다. masil 이 넣는 것은 아니고 고칠 자리가 P0 `termhost` 라 이 줄기에서 고치지 않고 오너 확인 표에 올렸다.
- 내 판단: `TestModeSwitchKeepsInstances` 는 계획 문구("lazygit·leet 를 연 채")와 달리 패키지마다 자기 도구만 연다 — gittool 은 저장소 두 개(인스턴스 둘), leet 는 오늘의 문제 + 패널 nvim. 한 시험 패키지가 두 도구를 함께 import 하면 "ui/leet·ui/gittool 은 서로 import 금지" 의 경계가 시험 쪽에서 흐려지기 때문. 각 크기에서 `h.Check()`(80×24 탭 줄의 `도구` 칸 포함), 프로세스 수 그대로, 같은 인스턴스, 도구가 다시 보임을 확인한다.
- 내 판단: `TestTinyOpensExec` 는 lazygit 은 계획대로(40×14 에서 열기 → 화면 전환, 색 덧씌움 인자 없음, 돌아온 뒤 `종료됨` 줄·무결성). leet 는 초소형에서 `e` 가 패널을 띄우지 않고 goeditor 로 그 자리에서 편집하며(A10.4 편집기 고르기), `E` 가 nvim 을 `ModeExec` 로 띄우는 것을 확인한다. termpanetest 가 인스턴스별 결정 모드를 내보내지 않아(P0 파일) lazygit 쪽은 화면의 `화면 전환으로 실행 중` 줄과 패널 글자 없음으로 `ModeExec` 를 확인한다.
- 내 판단: `ui/leet`·`ui/gittool` 사이·`ui/chat|feed|fun` import 금지는 지금 `go list -deps` 로 0 임을 확인했지만 시험으로 강제하지는 않았다 — 공유 파일 `importgraph_test.go` 에는 색인 표대로 접두사 한 줄(`internal/lazygit`)만 더했다(`internal/leetcode` 는 P0 가 이미 넣어 둠). CA 2.2 에 규칙을 글로 적었다.
- 내 판단: CA 3.2 는 `tools-state.json` 의 실제 `leet`·`git` 섹션 모양(계획의 `username` 한 칸이 아니라 계정별 `accounts`)과, 계획에 없던 캐시 `leetcode/`·`lazygit/` 줄을 함께 적었다. CA 5.6 은 실제 등록 id 다섯 개(`leet.open`·`leet.board`·`leet.settings`·`leet.daily.toggle`·`git.open`).
- 내 판단: 계획 Step 4 의 `make lint && make test && make cross` 대신 이 실행 지시의 검증 목록(build·test·vet 세 OS·gofmt)을 돌렸다. 서버는 바꾸지 않아 `npm` 은 돌리지 않았다.

**Task 24 리뷰 반영**

- 내 판단: `LEETCODE_*` 환경변수가 자식(nvim·lazygit)에게 상속되던 것은 P0 `termhost` 를 건드리지 않고 P1 안에서 막았다. `ui/leet` 의 `init()` 이 masil 시작 때 세 변수(`LEETCODE_SESSION`·`LEETCODE_CSRFTOKEN`·`LEETCODE_CFCLEARANCE`)를 메모리로 옮기고 `os.Unsetenv` 한다 — 어떤 프로그램도 띄우기 전이므로 termhost 의 `os.Environ()` 에 더는 없다. 실행·제출은 그 메모리(`envCookie`)를 읽는다. 시험은 `TakeEnvCookiesForTest` 로 같은 일을 하고, 누출 훑기 (e) 가 `os.Environ()` + `Request.Env`(termhost 가 실제로 만드는 자식 환경)까지 본다. 오너 확인 표의 "승인: LEETCODE_* 상속" 줄은 해결되어 지웠다. termhost 에 도구 비밀 이름 목록을 두는 일반 해법은 P0 몫으로 남긴다(지금은 필요 없음).
- 내 판단: 두 도구를 함께 연 모드 전환 시험(`TestModeSwitchKeepsBothTools`)은 `ui/e2e`(P0 Task 31 몫) 대신 `ui/leet` 의 외부 시험 패키지에 두고 `ui/gittool` 을 blank import 했다 — leet 의 가짜 LeetCode 주소 등 이음매가 leet 시험 파일에만 있어서. gittool 쪽 이음매는 쓸 수 없으므로 `/tmp/lg…/lazygit` 짧은 가짜 실행 파일을 PATH 앞에 두고 실제 찾기·`termhost.CurrentEnv`(TERM·TMUX 고정)를 쓴다. Windows 는 패널이 없어 건너뛴다. 운영 패키지 사이의 import 금지는 그대로다.
- 내 판단: `TestTinyOpensExec`(gittool)는 P0 `termpanetest` 를 고치지 않고 gittool 의 `export_test.go` 에 `LaunchedModeForTest`(인스턴스가 받은 `termpane.Launched.Mode`)를 두어 `ModeExec` 를 직접 확인한다. leet 쪽은 초소형에서 `e` 가 프로그램을 띄우지 않고(goeditor) `E` 가 `ModeExec` 를 명시하므로 Choose 의 초소형 규칙이 결정할 띄우기가 없다 — 그대로 둔다.
- 내 판단: `ui/leet` 가 LeetCode 클라이언트에 `slog.Default()` 를 로거로 넘긴다(디버그 줄: 방식·경로·상태·길이, `Cookie`·`x-csrftoken` 은 `(redacted)`). 누출 훑기 (c) 는 버퍼에 `leetcode request` 줄과 `cookie=(redacted)` 가 있어야 통과한다(빈 버퍼로 통과하지 않음).
- 내 판단: 오너 확인 표에 Windows 자격 증명 관리자 저장 → 재시작 뒤 다시 묻지 않음(긴 값 분할 포함) 줄을 더했다. `credman_test.go` 의 분할 시험은 가짜 API 위라 실제 재시작 확인을 대신하지 못한다.

(P1: 143줄)

### P2 (`exec-log/P2.md`)


**1묶음 (Task 1~6 — `wire`·`wscrypto`·`ot`, P0 의존 없음) / 내 판단**

- 내 판단: 안쪽 메시지의 JSON 필드 이름은 snake_case 로 정했다(`client_op_id`·`base_seq`·`keyboard_until` …). 설계서 B7.2 가 편집 봉투를 `collab.md` 와 같은 이름으로 쓰라고 해서 나머지 메시지도 같은 꼴로 맞췄다. `[]byte` 는 JSON 표준 base64.
- 내 판단: 표에서 MinLevel 이 `—`(참가자가 보내지 않음)인 종류를 위해 `wire.LevelNoPeer = 0xFF` 를 두었다. 선로에 실리지 않는 값이고 `Level.Allows(kind)` 가 언제나 거짓을 낸다. 계획의 `Level` 다섯 값(None~Keyboard)은 그대로다.
- 내 판단: 계획에 모양이 없던 `SeatInfo` 는 `{seat, user_id}` 로 정했다(Task 13 서버가 채움). 터미널 바이트 평문 도우미 `wire.TermPayload`·`SplitTerm`, 종류 목록 `wire.Kinds()`, `Kind.Valid()`, 헤더만 읽는 `wire.ParseHeader` 를 더했다.
- 내 판단: 조각 다시 모으기는 (보낸 자리, Kind) 마다 따로 이어 붙인다 — 오너가 `term_out` 과 `tree_list` 조각을 섞어 보내도 엉키지 않게. 64 MiB 한도는 계획대로 보낸 자리당 합계로 센다(넘으면 그 자리의 모으던 조각을 모두 버림). `Chunk` 는 첫 조각에만 `FlagGroupStart` 를 남긴다.
- 내 판단: 서명된 방송 프레임은 헤더 `Len` 에 서명 64바이트가 들어 있어서, `wscrypto.Verify` 가 서명을 떼어 낸 프레임을 돌려주면 `wire.Parse` 로는 길이가 안 맞는다. 계획의 두 문장("Verify → Open(frame, 64)", "Open 은 끝 64바이트를 무시")을 둘 다 살려 `Opener.Open(frame, extra)` 가 서명이 붙은 프레임과 떼어 낸 프레임을 모두 받게 했다(`Len` 으로 어느 쪽인지 정해짐). 서명 검사는 언제나 `Verify` 가 먼저 한다.
- 내 판단: `wscrypto.Derive` 는 자기 공개값과 상대 공개값이 transcript 의 두 공개값(역할 자리 그대로)과 맞는지 먼저 본다(`ErrTranscript`). 잘못 짠 transcript 로 조용히 다른 키가 나오는 일을 막는다. X25519 저차 점(모두 0 공유 비밀)은 표준 `crypto/ecdh` 가 이미 거절하며 이를 `ErrWeakKey` 로 바꾼다. 공개값이 32바이트가 아니면 `ErrBadKey`.
- 내 판단: 시험 벡터(`testdata/vector.txt`, 계약 5절)는 Go 구현과 별도로 Python `cryptography` 46 의 X25519·HKDF 로 계산해 같은 값(`801 609`, O2P `0f19574e…`)임을 확인한 뒤 굳혔다.
- 내 판단: 봉인 카운터가 `2^64-1` 에 이르면 `ErrExhausted`(실제로는 `RekeyAfter = 2^40` 에서 키를 바꾸므로 닿지 않음). 256 KiB 를 넘는 봉인은 카운터를 쓰지 않고 `wire.ErrTooLarge`. 한 프레임 평문 한도는 `wscrypto.MaxPlaintext(extra)`.
- 내 판단: **재접속 때 받음(ack)을 잃은 편집이 두 번 들어가는 문제**. 계획의 `Client.Reset(seq, text)` 만으로는 다시 받은 상태에 내 대기 편집이 이미 들어갔는지 알 수 없다. 그래서 `collab.md` 의 멱등 규칙을 따라 (1) `Sequencer.SubmitID(clientOpID, base, op)`(같은 id 는 `ErrDuplicate` + 처음 순번)·`AppliedID(id)` 를 더하고, (2) `Client.Resync() string`(재접속 시작, 대기 id 반환; 그동안 보내지 않고 ack·남의 편집을 버림)과 `Client.ResetAcked(seq, text)`(대기 편집이 이미 들어간 상태 위에 버퍼만 다시 얹음)를 더했으며, (3) `wire.DocOpen.Pending`·`wire.DocState.PendingSeq` 와 계약 7절 재접속 문단을 더했다. `Reset(seq, text)` 은 계획 그대로(대기 + 버퍼를 다시 얹음)이고, 다시 얹을 때는 마지막으로 확정했던 문서와 받은 문서의 `Diff` 에 맞춰 변환한다. Task 15·18 의 Guest·Host 가 이 순서를 쓴다.
- 내 판단: 무작위 수렴 시험에서 참가자 2가 끊기면 받는 줄과 보내는 줄(아직 안 닿은 프레임)이 모두 사라진다(실제 소켓과 같게). 끊긴 동안 친 편집의 프레임도 사라진다. 그래서 "편집은 순번을 받았는데 ack 만 잃은" 경우가 실제로 생기고(300 시드에서 391번), 시험이 이를 세어 0 이면 실패한다. 끝난 뒤 표지 글자(사용 안 하는 영역 U+E000~)마다 "아무도 안 지웠으면 정확히 한 번 남아 있음"을 본다(두 번 들어가면 실패).
- 내 판단: `Undo` 는 자기가 본 문서를 따라가며(Record·Remote·Undo·Redo 때 갱신) 되돌리기의 역연산(다시 하기)을 직접 만든다 — 계획의 `Undo() (Op, bool)`·`Redo() (Op, bool)` 모양을 지키기 위해. 남의 편집으로 내 글자가 모두 지워져 아무것도 안 하는 항목은 건너뛴다. 두 쌓임은 각 1,000개로 자른다. 연속 입력을 한 항목으로 묶는 일은 편집기 어댑터(Task 19) 몫이다.
- 내 판단: `Diff` 의 줄 단위 Myers 는 단계마다 대각선 범위만큼만 기록해(합 약 D², int32) D = 2,000 에서도 약 16 MB 안이다. 넘으면 가운데를 통째로 바꾼다. 바뀐 덩어리 안의 앞뒤 공통 떼기(`trimDiff`)는 Task 5 의 재접속 다시 얹기에서 먼저 썼고, Task 6 에서 `Client` 가 `Diff` 를 쓰도록 바꿨다.
- 내 판단: `DiskTracker.Edited` 는 순번을 받은 편집마다 부르되, `DiskChanged` 가 돌려준 디스크 편집에는 부르지 않는다(이미 `unsaved` 에 반영됨) — 주석에 적었다. 같은 자리 넣기 동률은 저장 안 된 편집이 앞(`Transform(x, unsaved)` 의 b 우선).

**리뷰 보정 (Task 1~6 리뷰 지적 6건)**

- 내 판단: **연산 수 넘침**. `ot.MaxLen = 1<<30`(rune) 을 두고 `validate` 가 각 retain·delete 와 retain+delete 합·결과 길이 합이 이를 넘으면 `ErrInvalid` 로 거절한다. JSON 읽기도 `|n| > MaxLen` 이면 `ErrInvalid`(`-2^63` 이 부호를 뒤집지 못하게). `Apply` 는 조각마다 `pos + n ≤ len(doc)` 를 다시 보고 넘으면 `ErrBaseLen` — 공황 대신 오류. 열 수 있는 문서가 2 MiB 이므로 정상 연산에는 닿지 않는다.
- 내 판단: **client_op_id 충돌**. 중복 판정 열쇠를 (보낸 자리, id) 로 바꿨다: `Sequencer.SubmitID(author int, clientOpID string, baseSeq int64, op Op)`, `AppliedID(author int, clientOpID string)`. `author` 는 오너가 아는 소켓의 자리(헤더 `From`, 서버가 확인)이지 메시지 안의 값이 아니다. 그리고 `NewClient` 가 id 를 `접두어-무작위16진수16자리-카운터` 로 만든다(Client 마다 `crypto/rand` 8바이트) — 계획의 `idPrefix + "-" + 카운터` 에서 가운데 조각만 더했다. 같은 자리가 문서를 다시 열거나 masil 을 다시 켜도 오너가 기억하는 예전 id 와 겹치지 않는다. 내용 비교(같은 id, 다른 op)는 하지 않았다 — 자리 범위 + 무작위로 우연한 충돌은 사라지고, 같은 자리 안의 흉내는 자기 편집만 망친다.
- 내 판단: **Task 18 인터페이스**. 계획 Task 18 Produces 를 고쳤다(“P2 리뷰 보정” 표시): `DocManager.Submit(rel, author, clientOpID, baseSeq, op)`(`ErrDuplicate` 면 처음 순번, Host 는 `doc_ack` 만), `DocManager.PendingSeq(rel, author, clientOpID) int64`(`doc_state.pending_seq` 채움), Guest 재접속 순서(`Resync` → `doc_open{pending}` → `Reset`/`ResetAcked`), 시험 `TestLostAckNotDuplicated`. `DocView` 에 필드를 더하는 대신 따로 묻는 함수로 해서 `Open` 의 모양은 계획 그대로다.
- 내 판단: 계약 9절에 외부 변경 확인 0.5초·자동 저장 2초(기본 끔)·기록 30일·끝난 공유 D1 행 30일과 바이너리 규칙(앞 8 KiB NUL 또는 UTF-8 아님)을 더했고, 1절의 "4장 조각 규칙" 을 "2장" 으로 고쳤다.

**2묶음 (Task 7~11 — `fsroot`·`launch`·`hooks`·`audit`·`journal`, P0 의존 없음) / 내 판단**

- 내 판단: **오너 쪽 파일 함수와 차단 목록**. `ReadText`·`WriteText`·`Stat`·`Lstat`·`Watcher` 는 문법·`..`·절대 경로·폴더 밖 링크만 거절하고 차단 목록은 보지 않는다(오너는 자기 `.env` 를 볼 수 있어야 함). 참가자 경로는 Host(Task 18)가 먼저 `Check(rel, true)` 를 부른다. `Create`·`Rename`·`Remove` 는 계획대로 `Check(…, false)` 라 오너도 차단 목록 이름은 만들지·옮기지·지우지 못한다.
- 내 판단: `Reason` 값은 1부터(0 = 이유 없음). `C:` 꼴(둘째 바이트가 `:` 이고 첫 글자가 ASCII 영문)은 Unix 에서도 `ReasonAbsolute` — `a:b` 같은 이름도 거절된다. 링크 40번 넘게 따라가면(고리) `ReasonInvalid`. 링크의 절대 경로 대상이 폴더(또는 `EvalSymlinks` 한 폴더) 안이면 받아들이고 밖이면 `Escape`. 없는 조각을 만나면 링크 따라가기를 멈추고 나머지를 그대로 붙인다(아직 없는 파일은 링크일 수 없음).
- 내 판단: 차단 목록은 대소문자를 가리지 않는다(이름 전체 `EqualFold`/소문자 `path.Match`). `.ssh`·`.aws`·`.gnupg`·`.git` 은 그 폴더 자체도 차단(트리에서 숨김). `.gitignore`·`.github` 는 차단 아님.
- 내 판단: **무시 규칙**. 경로나 그 위 폴더 중 하나라도 무시되면 무시(git 은 무시된 폴더 안 파일을 다시 넣지 못함). git 에는 끝 `/` 없이 경로를 넘긴다 — `x/` 로 넘기면 `x/**` 가 폴더 `x` 자체에 걸려 git 과 대체 파서가 달라졌고, 있는 폴더는 git 이 파일 시스템을 보고 안다. git 이 128·시간 초과·없음이면 그 호출만 대체 파서로. `.masilignore` 는 두 방식 모두 OR 로 덧붙인다(그래서 `.masilignore` 의 `!` 는 `.gitignore` 가 숨긴 것을 되살리지 못함). `Open` 은 `.git` 이 파일이어도(작업 트리·하위 모듈) Git 방식을 고른다.
- 내 판단: 규칙 파일(`.gitignore`·`.masilignore`)은 참가자 `Check` 에서 `ReasonIgnored`, 오너 `List` 에서는 `Ignored=true` 로 표시(공유 안 됨 표시용). 무시 검사 오류는 참가자 쪽에서 닫힌 쪽으로(거절). 링크는 논리 경로와 실제 대상 둘 다 무시 검사.
- 내 판단: `List` 에서 폴더 밖을 가리키는 링크는 오너에게 `Denied=true`(🔒), 참가자에게는 뺀다. 링크 항목의 `Dir`·`Size` 는 대상 기준. git 시험(`TestGitIgnorerMatchesFallback`)은 `HOME`·`XDG_CONFIG_HOME`·`GIT_CONFIG_GLOBAL`·`GIT_CONFIG_NOSYSTEM` 을 시험 폴더로 돌려 사용자 전역 무시 파일 영향을 막는다.
- 내 판단: 계획 밖으로 더한 공개 이름: `fsroot.GitRunner`(= `NewGitIgnorer` 의 `run` 함수 꼴), `GitIgnoreFile`·`MasilIgnoreFile`, `Fingerprint.IsZero()`(0 값 = 파일 없음).
- 내 판단: **`WriteText` 비교**. 지문 비교는 있음/없음 + 크기 + SHA-256(수정 시각은 안 봄). 0 값 `prev.FP` 는 "파일 없음" 이라 그새 누가 만들었으면 `ErrChangedOnDisk`, 거꾸로 디스크에서 지워졌어도 `ErrChangedOnDisk`(Task 18 의 `디스크에서 지워짐` 처리가 0 값 지문으로 다시 부름). `prev.Kind != KindText`(읽기 전용 종류)는 `ErrNotText`. 링크는 실제 대상에 써서 링크를 지키고, 없는 부모 폴더는 만들지 않는다(`ErrNotExist`). 분류 순서는 2 MiB 초과 > 바이너리 > 줄 끝 섞임 > 512 KiB 초과. 줄 끝 섞임은 BOM 만 떼고 CR 은 그대로 돌려준다.
- 내 판단: `Watcher.Poll` 은 보고한 뒤 기준을 새 지문으로 바꾼다(같은 변경 두 번 보고 안 함). 지운 파일은 한 번 `Deleted`, 다시 생기면 다시 변경으로 보고. 잠깐의 읽기 오류는 다음 확인으로 넘긴다.
- 내 판단: `launch.Check` 에 빈 argv 용 `ReasonEmpty` 를 더했다. 프로그램 이름은 소문자 + `.exe`·`.cmd`·`.bat`·`.ps1` 떼기(`launch.ProgramName`·`IsClaude` 공개). `-` 로 시작하지 않는 인자(프롬프트 글)는 보지 않고 `--` 뒤도 보지 않는다. `--permission-mode` 가 여러 번이면 모두 검사, 값이 없으면 막는다. `--settings` 는 `--` 앞에 넣는다. hook 주소가 비면 `disableBypassPermissionsMode` 만 쓴다. 자동 모드를 끄는 설정 이름은 확인하지 못해 넣지 않았다 — 확인할 곳은 P2 Task 25 Step 4 항목 2("자동 모드에 대한 같은 설정이 있는지"). 색인 §4 오너 확인 목록 11번 줄에는 이 항목이 없으므로, 색인을 마무리하는 P0 Task 31 이 11번 줄에 "자동 모드에 대한 같은 설정 유무" 를 더해야 한다(색인은 P2 소유가 아니라 여기서 고치지 않음).
- 내 판단: `Programs` 의 마지막 "직접 입력" 은 `Program{}`(Argv nil, `Custom()` 참) — 화면 이름은 Task 20 이 카탈로그에서. claude·nvim 의 Argv 는 찾은 전체 경로가 아니라 이름 그대로.
- 내 판단: `hooks` 는 POST 만 받고(그 밖 404), 터미널 이름은 `[A-Za-z0-9._-]{1,64}` 가 아니면 404. 64 KiB 넘는 본문은 잘려서 JSON 이 깨지므로 사건 없이 200. 모르는 hook 이름도 200·사건 없음. `Close` 는 두 번 불러도 되고 `Events()` 를 닫는다.
- 내 판단: `audit.Open` 은 공유 ID 가 `[A-Za-z0-9_-]{1,64}` 가 아니면 거절(다른 경로를 가리키지 못하게), 이미 있던 폴더·파일도 0700·0600 으로 맞춘다. `Purge` 는 일반 `*.jsonl` 만, 폴더가 없으면 `0, nil`.
- 내 판단: `journal.Entry` JSON 이름은 `rel`·`base`·`text`·`at`(`At` = 저장 때 UTC). `Pending` 은 깨진 파일과 파일 이름이 `hash(rel)` 과 맞지 않는 것을 건너뛰고 경로 순으로 돌려준다. 없는 항목 `Drop` 은 오류 아님.
- 내 판단: 의존 규칙 시험은 시험 파일만 있는 `internal/workspace` 패키지(`package workspace_test`)에 두었다 — `go build ./...`·`go vet ./...` 가 문제없이 지나감을 확인했다. CA 2.2 문장 추가는 계획대로 Task 24 몫.

**리뷰 보정 (Task 7~11 리뷰 지적 8건)**

- 내 판단: **링크 대상의 `..`**. `follow` 는 링크 대상을 `path.Clean` 하지 않고 날 조각 그대로 남은 조각 앞에 붙이며, `..` 는 지금까지 푼 경로에서 한 조각을 뺀다(뿌리 위로 가면 `Escape`) — 커널·`os.Root` 와 같은 순서. 절대 경로 대상도 정리하지 않고 폴더 경로(또는 실제 경로) 접두어만 떼어 같은 방식으로 걷는다(접두어가 정확히 맞지 않으면 `Escape`, 닫힌 쪽). 없는 조각 뒤에 링크에서 온 `..` 가 남으면 OS 도 걷지 못하므로 `ReasonInvalid`.
- 내 판단: **디스크에 저장된 이름으로 검사**. `follow` 가 있는 조각마다 부모 폴더 이름 목록을 읽어 바이트가 같은 항목이 있으면 그대로, 없으면 `os.SameFile` 로 같은 파일인 항목 하나의 이름으로 바꾼다(대소문자·U+017F `ſ`·8.3 짧은 이름·끝 점/공백·NFC/NFD 가 모두 저장 이름으로 모임). 같은 파일이 둘 이상(하드 링크)이거나 폴더를 못 읽으면 `ReasonInvalid`(닫힌 쪽). 거절 대신 바꿔 넣기를 고른 이유: 맥에서 한글 파일 이름을 NFC 로 친 경로가 NFD 로 저장된 파일을 여는 정상 사용을 막지 않기 위해. 차단 목록·무시 규칙·규칙 파일 검사는 이렇게 얻은 실제 경로에도 돌고, `List` 도 자식마다 실제 경로(`real/이름`)를 함께 검사한다. 대가: `Check` 마다 경로 조각 수만큼 폴더 이름 읽기. Windows 의 8.3·끝 점 경우는 이 Mac 에서 시험하지 못했다(같은 코드 경로, `SameFile` 실패 시 닫힌 쪽).
- 내 판단: **특수 파일**. FIFO·장치·소켓은 열지 않는다 — `ReadText` 는 `KindBinary` + `ErrNotText`, `WriteText` 는 `ErrNotText`, `Watcher` 는 지워진 것처럼 보고, `List` 는 `Denied=true`(오너 🔒, 참가자에게 안 보임). Stat 과 연 파일의 Stat 둘 다에서 일반 파일인지 본다.
- 내 판단: **읽기 크기 한도**. `ReadText`·`WriteText` 의 디스크 확인·`Watcher` 모두 `MaxView+1` 바이트까지만 읽는다. `MaxView` 를 넘는 파일의 지문은 내용 해시 대신 크기·수정 시각으로 만든 표지 해시라 크기나 시각이 바뀌면 변경으로 보고된다(`Fingerprint` 모양은 그대로 — 복구 일지 JSON 이 바뀌지 않게).
- 내 판단: **hook 본문**. 본문 전체를 `json.Unmarshal` 하지 않고 맨 위 객체를 토큰으로 걸어 `hook_event_name` 까지 읽는다(앞 값들은 건너뜀). 64 KiB 에서 잘려도 이름이 앞에 있으면 사건이 난다. 안쪽 객체의 같은 이름 키는 세지 않는다.
- 내 판단: **사용자 `--settings`**. `launch.Check` 가 사용자가 준 `--settings`(파일·JSON 글)를 새 `ReasonUserSettings` 로 막는다 — 그 안의 `permissions.defaultMode` 를 masil 이 볼 수 없고, `--settings` 두 개를 claude 가 어떻게 합치는지 확인되지 않았기 때문. `Prepare` 가 만든 임시 파일(`masil-claude-*.json`)은 알아보고 통과시킨다(Task 21 이 Prepare 결과 argv 로 Check 해도 됨). Task 20/21 은 이 Reason 의 화면 문구를 카탈로그에 더해야 한다. 그리고 `Prepare` 는 `--permission-mode` 가 없으면 `--permission-mode manual` 을 붙인다 — 명령줄 모드가 사용자·프로젝트·로컬 설정 파일의 `defaultMode` 보다 앞서므로 Check 가 말한 모드로 시작한다(복제한 저장소의 `.claude/settings.json` 이 `dontAsk` 를 넣어도). 대가: 공유하지 않을 때도 작업공간에서 띄운 claude 는 설정 파일의 기본 모드 대신 `manual` 로 시작한다. "명령줄 모드가 설정 `defaultMode` 보다 앞선다" 는 실제 claude 로 확인하지 않았다 — P2 Task 25 Step 4 항목 2 에서 함께 확인할 것.

**3묶음 (Task 12~14 — 서버 메타·REST·`WorkspaceHub`·클라이언트 REST·이벤트·`wsstore`·`share/conn`·`sharetest`) / 내 판단**

- 내 판단: **P0 슬롯 시험**. 슬롯을 채우면 P0 `test/unit/slots.test.ts` 의 빈 슬롯 기대가 깨질 수밖에 없어 고쳤다(빈 몸 `POST /v1/workspace-shares` 는 이제 400, 허브의 모르는 경로는 501 대신 404).
- 내 판단: **리미터 이름 정규식**. `RL_SHARE_WS` 를 읽도록 TS(`test/unit/contract-limiters.test.ts`)·Go(`internal/api/contract_limiters_test.go`) 계약 시험의 `RL_[A-Z]+` 를 `RL_[A-Z_]+` 로 넓혔다(두 파일 모두 Task 의 Files 목록 밖). Go `realtimeLimits` 에 `RL_SHARE_WS` 를 더했다(요청 리미터가 아니라 소켓 업그레이드). Task 12 에서 둔 `pendingLimiterRows` 의 두 POST 행 면제는 Task 14 에서 엔드포인트 사례가 생겨 지웠다.
- 내 판단: api.md 6장은 Task 12 에 6행, `/ws` 행은 그 경로가 생긴 Task 13 커밋에 넣었다.
- 내 판단: **만들기**. 오너의 다른 기기도 새 공유를 알도록 만들기에서도 오너에게 휘발 `workspace_share.updated` 를 보낸다. 초대에는 알림함 행을 만들지 않는다(계획대로 alert 만).
- 내 판단: **다시 초대**. 내보낸 멤버를 다시 초대하면 같은 자리로 되돌리고(행 값 초기화), 빈 자리가 없으면 가장 오래 전에 내보낸 행을 지우고 그 자리를 쓴다(api.md 5.25 에 적음). 오너가 자기를 내보내기는 `403 forbidden`. 내보내기·나가기의 `workspace_share.updated` 는 내보내진 사람에게도 간다(자기 목록에서 빼도록).
- 내 판단: `endShare` 에 `fromHub` 선택을 두어 허브가 부를 때는 허브 `/end` 를 다시 부르지 않는다(DO 가 자기를 fetch 하지 않게). `ctl_kick` 은 REST 내보내기처럼 D1 에서도 내보낸다(다시 붙지 못함).
- 내 판단: **`ctl_levels`** 는 표 전체 교체. D1 `max_level` 은 지금까지 준 단계 중 가장 높은 것(내려도 유지), `verified` 는 한 번 참이면 참, D1 행이 실제로 바뀐 때만 `updated` 발행. 키프레임 캐시는 `To=255` 의 `term_key`(묶음 시작)·`term_out` 만 담고, 재생은 단계가 없다가 생긴 자리에만 한 번(workspace.md 4.1 에 적음).
- 내 판단: `srv_hello.seats` = 그 순간 붙어 있는 소켓의 자리. 같은 사용자의 새 소켓이 옛 소켓을 `4102` 로 밀어낼 때는 `srv_peer left` 를 보내지 않는다. 끝남 표시는 DO 저장소에 남겨 그 뒤 `/connect` 를 404 로 거절한다.
- 내 판단: `workspace_attention` alert 의 actor 는 null(오너 자신의 프로그램이 부르는 것). 30초 간격 기록(`lastAttention`)은 메모리에만 — DO 가 잠들었다 깨면 한 번 더 갈 수 있다.
- 내 판단: DO 의 `connect` 메서드 이름이 DurableObject 기반 클래스의 `connect(socket)` 과 겹쳐 `onConnect` 로 바꿨다. 시계는 시험용으로 `protected now()`.
- 내 판단: 클라이언트 `alerts.toml` 에는 ko·en 만 더했다(zh-Hans 는 P5 소유, 생성기가 "zh-Hans missing" 경고만 낸다). 서버 `TEMPLATES` 의 zh-Hans 는 en 을 그대로 썼다.
- 내 판단: 정리 작업은 data-model.md 6 의 10번째 항목으로 적었지만 코드에는 이미 10개가 있어 실제로는 11번째다(끝난 공유는 끝난 지 30일 뒤 삭제).
- 내 판단: **TDD 어긋남 1건**. `workspace-frame.ts` 를 시험보다 먼저 써서 `test/unit/workspace-frame.test.ts` 는 처음부터 통과했다. 허브 시험(`test/realtime/workspace-hub.test.ts`)과 Task 14 의 Go 시험들은 먼저 실패(없는 이름·컴파일 실패)를 확인했다.
- 내 판단: `NotifySettingsPatch` 는 `types.go` 가 아니라 `ep_me.go` 에 있어 거기에 `Workspace` 를 더했다(전송 몸 `notifyPatchBody` 와 함께). 타입 표본 4개(`NotifySettings`·`Settings`·`Me`·`AuthResult`)에 `"workspace": true` 를 더했다.
- 내 판단: `RemoveWorkspaceParticipant` 의 `@me` 는 다른 `{user}` 경로처럼 `url.PathEscape` 로 한 조각이 되며 `@` 는 그대로 남는다(`/participants/@me`). `dedicatedLimiters` 에 `RL_SHARE` 를 더했고, 경로 시험(`path_test.go` 의 `pathCalls`·`path_build_test.go` 의 `WorkspaceShareWSURL` 허용)도 함께 고쳤다.
- 내 판단: `protocol.go` 는 바꿀 것이 없었다(디코더 표는 `events.go` 에 있음). `workspace_share.invited`·`ended` 는 기록, `updated` 는 휘발.
- 내 판단: **`wsstore`**. 내 공유/초대받은 공유는 Store 의 `Me` 로 오너를 비교해 가른다. `updated` 가 나를 멤버로도 오너로도 담지 않으면 목록에서 뺀다. `ended` 는 지우지 않고 `Status="ended"`·`EndReason` 으로 남겨 `Share(id)` 가 끝난 이유를 돌려준다(`Invited()`·`Mine()` 은 live 만, 최근 만든 순). `Resync` 결과는 `ResyncMsg` 로 돌아오고 `ApplyResync` 가 목록을 바꾸되, Resync 이후 사건이 바꾼 공유는 사건 쪽을 남긴다. `Reset` 전에 나간 결과는 버린다. `updated` 가 휘발이라 `Backfill` 도 같은 두 목록을 다시 받는다. 작업공간 도구 깃발(`features.On("workspace")`)이 꺼져 있으면 아무것도 받지 않는다. 앱에 `ResyncMsg` 를 넘기는 연결(`ApplyResync` 호출)은 화면을 붙이는 뒤 Task 몫이다.
- 내 판단: **`share.Conn`**. 거절된 업그레이드를 `*api.Error` 로 풀려고 `realtime` 의 내부 함수를 `realtime.UpgradeError` 로 내보냈다(Files 목록 밖, 한 줄 감싸기). `Recv()` 는 `Done` 뒤에 닫힌다. 로컬 `Close(code)` 뒤의 `Err()` 는 그 code 를 담은 `websocket.CloseError`, 연결이 그냥 끊기면 code 없음(`CloseCodeOf` = 0). 받은 글자 프레임은 버린다.
- 내 판단: **`sharetest.Relay`**. 서버 규칙 1~6 과 4.1 세부 규칙을 옮겼다(규칙 6 은 리뷰 보정에서 `SetClock` 과 함께 더함). 규칙 7(오너 2분 유예, `End()` 로 대신)·attention 30초 간격은 옮기지 않았다(attention 은 횟수만 `Attention()`). `Send` 는 그 순간의 단계로 바로 라우팅해 큐에 넣고 `Pump()` 가 배달한다. 닫힘 코드는 즉시 적용. 받은편지함은 4096 프레임, 넘치면 panic(시험이 Pump 사이에 Recv 를 비워야 함). 계획 밖으로 관찰용 `Levels()`·`Verified()`·`Attention()`·`Ended()`·`Connected(seat)`·`ErrDropped` 를 더했다. `Tamper` 는 중계·서버 프레임 모두에 걸리고 `Inject` 에는 걸리지 않는다.

**리뷰 보정 (Task 12~14 리뷰 지적 5건, 그중 2건은 같은 지적)**

- 내 판단: **허브가 스스로 닫는 소켓**. `4000`·`4008` 로 닫는 소켓도 떠남으로 친다 — `drop()` 이 닫고 곧바로 `departed()` 를 돌린다(참가자면 `srv_peer left`·`left_at`, 오너의 마지막 소켓이면 2분 alarm). 닫힘 사건은 `gone` 이라 두 번 돌지 않는다. `kick`(`4101`, `kickUser` 가 `left` 를 보냄)·`end`(`4100`)·`replace`(`4102`)는 그대로 떠남 처리 없음. 같은 사용자의 옛 소켓은 새 소켓의 D1 확인이 통과한 뒤에야 `4102` 로 닫는다(확인 실패·D1 장애 때 옛 소켓이 그대로 남게). D1 장애(`1011`/`503`)로 오너 소켓을 거절할 때 다른 오너 소켓이 없으면 alarm 을 건다(확인 중에 옛 소켓이 스스로 닫힌 경우). 참가자의 `1011` 은 `joined` 를 보낸 적이 없으므로 `left` 도 보내지 않는다. D1 장애 경우는 시험으로 재현하지 못했다(코드 읽기로만 확인).
- 내 판단: **DO 저장소 정리**. 공유가 끝나면(`endAll`) `meta`(제목·오너)·`levels`·`verified` 를 지우고 `ended` 표시만 남긴다. 끝남과 엇갈린 `/connect` 가 `meta` 를 다시 쓴 경우 404 로 거절하면서 다시 지운다. data-model.md 3.14(P2 절)·workspace.md 4.1 에 적었다. 공유가 살아 있는 동안 `meta` 를 두는 것은 alarm·attention 이 쓰기 때문.
- 내 판단: **api.md 0.5**. `live_workspace_shares`·`workspace_share_members` 를 0.5 목록 줄에서 빼고(P0 소유 줄, P2 가 고칠 수 있는 범위 밖) 5.25 앞머리에 "한도" 한 줄로 옮겼다.
- 내 판단: **`sharetest.Relay` 규칙 6**. `SetClock(func() time.Time)`(기본 `time.Now`)을 더하고 연결별 한도를 허브와 같은 창(1초·`prop_submit` 1분, 바닥 나눗셈)과 같은 순서(헤더·From 검사 뒤, 종류 확인 앞)로 옮겼다. 넘는 프레임은 버리고 3초 연속이면 `4008`, 참가자면 오너에게 `srv_peer left`. 시험 `TestRelayRateLimitsMirrorServer` 는 서버의 "limits and alerts" 두 사례와 `prop_submit` 분당·오너 바이트 사례를 다룬다.

**4묶음 (Task 15~18 — 공유 엔진: 합류·키 교환·재개, 단계 사다리·키보드 만료·Kill/End·내보내기, 터미널 중계·제안 대기열, DocManager) / 내 판단**

- 내 판단: 작업 지시가 `v013-p2` 작업 트리·`feat/v013-p2` 가지를 지정해 거기서만 했다(세션의 기본 디렉터리는 `v013-p3` 였으나 건드리지 않음).
- 내 판단: **ManualTick**. 고루틴을 하나도 띄우지 않는다. `Tick(now)` 가 엔진 시각을 `now` 로 두고 `Recv` 를 막지 않고 비운 뒤(닫힘 코드 처리·재접속 포함) 타이머를 돌린다. 엔진 안의 "지금" 은 마지막 `Tick` 의 시각이다. 실제 운용(ManualTick=false)은 10 ms 고리가 같은 `Tick` 을 부른다.
- 내 판단: **보내기 순서와 잠금**. `outMu` 가 연결·보낼 큐·방송 봉인기/서명기를 지키고 봉인을 그 안에서 해 큐 순서 = 카운터 순서가 된다. `sendMu` 는 `flush` 하나만 돌게 하고, `flush` 는 상태 잠금을 쥔 채 부르지 않는다. 잠금 순서는 Host.mu/Guest.mu → DocManager.mu → outMu/evMu. 이를 위해 Task 16 에서 `handshake.go` 를 고쳐 `bseal`/`bsign` 을 `outMu` 아래로 옮겼다.
- 내 판단: **계획 밖 사건**. `EventJoinExpired`·`EventSASReady`·`EventKeyboardPaused`/`Resumed`/`Ended`·`EventDoc`·`EventDocApplied`·`EventDocRejected`·`EventDocReset`·`EventDocRemote`·`EventNotice`·`EventFsResult` 를 더했다(화면이 알아야 할 상태 변화). `EventDocApplied` 는 참가자(author ≥ 1)의 편집에만 난다(오너 자신의 편집은 화면이 이미 앎).
- 내 판단: **HostConfig 추가**. `RemoveParticipant`·`EndShare`(REST 를 엔진에 주입, nil 이면 건너뜀)와 `Root`·`Docs` 를 더했다.
- 내 판단: **TDD 실패 확인**. 각 Task 의 시험을 먼저 쓰고 그 Task 의 구현 파일을 스크래치 폴더로 옮기고 고친 파일은 `git checkout HEAD --` 로 되돌린 상태에서 돌려 실패(없는 이름으로 컴파일 실패)를 본 뒤 되돌렸다(bare `git stash` 금지 규칙 때문).
- 내 판단: `sharetest.Relay` 에 `Stall()` 이 없어 relay.go 를 고치지 않고 시험 쪽 `stallConn`(보내기가 막히는 연결 감싸개)으로 Kill 200 ms 제한을 시험했다.
- 내 판단: `Terminal` 인터페이스·`ShareTerminal`·`Guest.Input` 은 Task 16 시험(키보드 만료가 입력을 막는지)에 필요해 Task 16 에서 `term.go` 와 함께 먼저 만들고 Task 17 에서 묶음·키프레임·상태를 더했다.
- 내 판단: **오너 재접속**. 오너 소켓이 다시 붙으면 `ctl_levels`·명단을 다시 보내고 `notice{resumed}` 를 방송하며 모든 터미널에 키프레임을 보낸다. 참가자는 이 notice 로 `EventResumed` 를 내고 열린 문서를 모두 다시 맞춘다(떨어진 동안 프레임이 사라졌을 수 있음).
- 내 판단: **재개 실패**. 오너가 여는 키가 없는 `hs_resume`(멤버가 아님 — 오너 쪽 Host 를 새로 만든 경우 등)에는 `hs_decline{"resume"}` 을 평문으로 보내고, 참가자는 이를 받으면 끝내지 않고 새 악수를 시작한다(리뷰 보정에서 실제로 보내게 고침 — 처음 구현은 기록만 했다). 이미 멤버인 사람이 새 악수를 하면(프로그램 재시작) 키보드는 편집자로 내리고(`restart`) 새 `JoinRequest` 로 오너 승인을 다시 받는다.
- 내 판단: **Kill 의 키 지우기**. 엔진이 들고 있는 K_s 바이트만 0 으로 덮고 봉인기·여는 기를 버린다. `wscrypto` 내부(AEAD 상태)는 지울 수 없다.
- 내 판단: **제안 대상 터미널**. 제안 글은 공유 중인 터미널 중 id 가 가장 작은 곳에 친다(제안 프레임에 터미널 id 가 없음).
- 내 판단: **오너 출력 한도**. 1초에 2 MiB 를 넘기면 그 1초의 나머지는 아무것도 보내지 않고(`outFull`), 다음 1초는 키프레임부터 보낸다 — 넘을 때마다 키프레임을 보내면 1초에 키프레임이 수십 개 나갔다.
- 내 판단: **오너 쪽 속도 제한**. 허브와 별도로 오너도 `term_input` 초당 50·`doc_op` 초당 30 을 넘는 프레임을 버리고 기록한다(허브를 거치지 않은 프레임 대비).
- 내 판단: **too_large 사전 검사**. `Submit` 은 적용 전에 "현재 바이트 + 넣는 글자 바이트" 로 `MaxCollab` 초과를 본다(지우는 양은 빼지 않는 상한 — 경계에서 보수적).
- 내 판단: **복구**. 계획의 Produces 에 없던 `Recoverable()`·`Recover(rel)` 을 더했다(재시작 뒤 일지를 다시 여는 길). 오너는 차단 목록 파일(`.env` 등)도 열 수 있다 — 막는 것은 참가자 쪽뿐.
- 내 판단: **문서 다시 맞추기**. 참가자는 (1) `doc_bcast` 의 seq 가 건너뛰면, (2) 보낸 편집·문서 요청이 5초 동안 답이 없으면(허브가 버렸을 수 있음) `doc_open{pending}` 으로 다시 맞춘다. 이미 답을 받은 요청의 두 번째 `doc_state`(재개·시간 초과로 두 번 물은 경우)는 버린다 — 처음엔 이것을 새 문서로 받아 OT 클라이언트를 새로 만들어 버려서 3자 동시 편집 시험에서 편집이 사라지고 `base_len` 거절이 났다. 같은 문서를 다시 `OpenDoc` 하면 reload 로 다룬다(내 미확인 편집은 버림).
- 내 판단: `doc_bcast` 와 `doc_ack` 는 DocManager 의 seq 훅 안(`outMu` 아래)에서 함께 봉인해 오너의 동시 편집이 둘 사이에 끼지 못하게 했다.
- 내 판단: 저장소 `.gitignore` 가 `node_modules` 를 무시해 `testdata/proj/node_modules/x.js` 를 커밋할 수 없다. 시험이 임시 폴더에 복사할 때 `node_modules/x.js`·`.git/config`·`link_env → .env` 링크를 만든다.
- 내 판단: 이 묶음의 기록은 Task 18 커밋에 함께 넣었다.

**리뷰 보정 (Task 15~18 리뷰 지적 10건, 그중 2건은 같은 지적)**

- 내 판단: **상태 잠금과 다시 걸기**. 계획 Task 15 Step 3 의 "한 고루틴이 상태를 소유하는 액터(명령은 채널로)" 대신 4묶음은 상태 잠금(`Host.mu`/`Guest.mu`)으로 만들었고, 그 차이를 적지 않았다. 액터로 다시 짜지 않고 잠금을 유지하되 막히는 일은 잠금 밖에서 하게 고쳤다 — `redial` 이 잠금을 풀고 10초 시한(`dialTimeout`, 엔진 ctx 아래)으로 dial 한 뒤 다시 잡고, 그새 공유가 끝났으면 새 소켓을 닫는다. 소켓 쓰기는 원래부터 잠금 밖(`flush`)이다. 그래서 UI 호출(`OwnerActivity`·`Members`·`SetLevel` …)과 Kill 은 dial 중에도 바로 돈다. dial 동안 그 Tick 고리는 타이머를 돌리지 않지만(최대 10초) 소켓이 없어 참가자 프레임도 들어오지 않는다.
- 내 판단: **재개 다시 보내기**. 참가자의 `hs_resume` 이 오너가 없는 동안 허브에서 버려질 수 있어, 재개 중인 참가자는 `notice{resumed}` 를 받으면(그때는 `EventResumed` 를 내지 않고) 그리고 5초(`resumeRetry`)마다 `hs_resume` 을 다시 보낸다. `EventResumed` 는 `hs_grant` 가 낸다. 이미 받은 뒤 두 번째 `hs_grant` 는 조용히 버린다.
- 내 판단: **`hs_commit` 다시 보내기**. 오너가 없는 동안 보낸 약속값도 버려지므로 `hs_owner_pub` 을 받을 때까지 3초(`commitRetry`)마다 같은 약속값을 다시 보낸다. 오너는 같은 자리·같은 사용자·같은 약속값이면 새 키 쌍을 만들지 않고 같은 공개키로 답한다(이미 파생했으면 무시). `JoinTimeout`(5분) 동안 답이 없으면 참가자는 `EventDeclined{expired}` 로 끝낸다.
- 내 판단: **오너만 연 문서는 중계하지 않음**. DocManager 를 혼자 쓰기와 함께 쓰므로 seq 훅이 오너가 연 `.env` 의 편집까지 `K_s` 방송으로 내보냈다(B7.3·B7.7 위반). 참가자가 하나라도 연 문서(`peerDocs`, `root.Check(rel, true)` 를 통과한 것뿐)만 `doc_bcast`·문서 notice·오너 커서를 보낸다. 연 사람마다 O2P 로 따로 봉인하는 대신 방송을 유지했다 — 참가자에게 보이는 문서는 모든 참가자가 열 수 있으므로 기밀 차이가 없고, `doc_bcast` 의 클래스(Broadcast·서명)를 바꾸지 않아도 된다. `peerMu` 는 맨 끝 잠금(Host.mu 나 DocManager.mu 아래에서만 잡음).
- 내 판단: **공유가 끝날 때 문서 돌려주기**. `finishLocked` 가 모든 자리의 Open 을 닫고 seq 훅을 뗀다(같은 DocManager 에 뒤에 붙은 Host 의 훅은 남김 — `hookBy`). 오너가 멤버를 `LevelNone` 으로 내릴 때도 제안·문서를 정리한다(`changeLevel` 으로 옮겨 참가자가 스스로 나갈 때와 한곳).
- 내 판단: **자리 재사용**. 허브는 REST 로 나간 사람에게도 `srv_peer left` 만 보내 오너가 내보내기와 끊김을 가를 수 없다(프로토콜을 바꾸지 않음). 대신 `srv_hello`·`srv_peer joined`(그리고 `grant`)에서 그 자리의 사용자가 멤버 기록과 다르면 옛 기록을 지운다(제안·문서·속도 상태 포함). 시험용으로 `sharetest.Relay` 에 `Remove(user)`(REST 나가기·내보내기와 같은 `4101`·`left`)·`Invite(seat, user)` 를 더했다.
- 내 판단: **단계 미달 `doc_op`**. 단계 검사에 걸린 `doc_op` 은 버리기 전에 묶음을 다 모아 `doc_reject{code:"level"}` 로 답한다(다른 종류는 그대로 조용히 버리고 기록). 오너가 보내던 `not_open` 은 workspace.md 7 의 code 목록에 더했다.
- 내 판단: **키보드 회수 때 키 교체**. 설계서 B9·workspace.md 6 대로 키보드를 거둘 때(만료·60초 끊김·재시작·오너가 내림·스스로 내림)와 방송 카운터가 `RekeyAfter` 에 이를 때 새 `K_s` 를 보낸다. B8.4 의 "세션 키 교체 → 키보드 보유자 편집으로" 는 내보내기의 교체에만 적용했다 — 회수 교체에도 적용하면 한 사람의 만료가 다른 키보드 보유자를 모두 내리는 연쇄가 된다. 한 Tick 안의 여러 회수는 한 번의 교체로 묶고(`rekeyDue`), 오너 소켓이 없으면 다시 붙은 뒤 보낸다. 카운터 검사는 매 Tick(시험은 `SetNeedsRekey` 로 대신).
- 내 판단: **`SetTermSize` 반환값**. 계획 Produces 에는 반환값이 없었으나 `error`(`ErrNoTerminal`·`ErrBadSize`)를 돌려주게 만들었고 기록하지 않았다. UI 가 목록 밖 크기 거절을 알 수 있도록 유지하고 계획 Produces 줄을 고쳤다.

**5묶음 (Task 19~22 — goeditor 감싸기, 혼자 쓰기 화면, 오너 공유 화면·대기열·공유 끝) / 내 판단**

- 내 판단: **goeditor 는 `core` 를 직접 몬다**. `Model` 의 키 다리는 키 글자의 첫 바이트만 넘겨 한글이 깨지고, `View` 는 남의 커서를 겹쳐 그릴 수 없다. 그래서 `core.Editor` 에 rune 단위로 키를 넣고 화면은 `peers.go` 가 직접 그린다. 끝 줄바꿈(goeditor 가 하나 버림)은 감싸개가 기억하고, `SetMaxHistory(0)` 뒤 u/U·ctrl+r·`:u`·`:red` 를 가로채 OT 쪽 되돌리기로 보낸다. ZWJ 이모지는 goeditor 가 묶지 않아 커서를 그래핌 경계로 맞춘다(`snapCursor`). 포크는 필요 없었다.
- 내 판단: `tea.Sequence` 는 UI 에서 금지라 `:wq`·`:x` 는 `SaveMsg{Close: true}` 하나로 낸다(계획은 `SaveMsg{}` 뒤 `CloseMsg{}`).
- 내 판단: `TestAdapterEmojiColumns` 의 삽입 위치는 계획의 6 이 아니라 rune 수로 센 5(`👩‍💻` 은 rune 3개 + `한글` 2개).
- 내 판단: **인스턴스 id 는 `"workspace:" + sha256(abs)[:8]`**. 계획의 `"ws:"` 는 P0 `ui.OpenTool` 규칙("`<toolID>:` 로 시작")과 어긋나 바꿨다. 폴더 고르기 화면은 `workspace:pick`. 고르기에서 이미 열린 폴더를 고르면 그 인스턴스를 다시 보인다.
- 내 판단: **대화상자는 표면 안 양식**(`form.go`, 계획에 없던 파일). 앱 오버레이 대신 도구 표면 전체를 덮고, 모든 문장은 줄바꿈되며 본문이 스크롤된다(자르지 않음). 대화상자는 맨 위부터 보이고(경고문을 첫 줄부터 읽게) 선택이 움직일 때만 선택 줄을 따라 스크롤한다.
- 내 판단: 계획에 없던 카탈로그 키를 더했다 — `prefix_help_*`·`dlg_hint`·`recent_none`·`open_failed`·`code_empty_hint`·`code_cannot_open`·`saved`·`save_failed`·`recovery_body`, Task 21 의 `share_dialog_hint`·`share_title_label`·`check_blocked_mode`·`check_blocked_settings`·`legal_notice_short`·`keyboard_*` 보조 문구·`ago_*`·`people_*`·`tamper_stop/continue`·`cancel`, Task 22 의 `proposals_hint*`·`proposal_from`·`end_*` 보조 문구·`tiny_*` 조각·팔레트 이름. `subtab_people` 는 `사람(00)` 이 9칸이라 max 9(계획 8). `share_invite_space`(방 하나를 골라 그 멤버) 는 처음엔 만들지 않았다가 리뷰 보정에서 더했다(아래).
- 내 판단: `owner.go`·`tiny.go` 는 Task 20 에서 빈 고리로 먼저 만들어 빌드를 지키고 Task 21·22 에서 채웠다.
- 내 판단: **패널 가능 여부**는 `runtime.GOOS != "windows"` 로 본다(`paneSupported`, 시험 훅). embedded=false·TERM=dumb·초소형은 `termpane.Launch` 가 exec 로 내리고 P0 토스트를 낸다.
- 내 판단: **터미널마다 termpane 인스턴스 id `<표면 id>#<n>`**. 앱의 `FocusChangedMsg` 는 표면 경로로 오므로 표면이 각 터미널의 경로 키로 바꿔 넘기고, `SetCellFocus` 로 칸 포커스를 알린다. claude 의 `--settings` 임시 파일은 `Paths.Cache` 에, hook URL 의 마지막 칸은 터미널 번호 n.
- 내 판단: **오너 편집 동기화**. 편집기의 키마다 나온 op 를 `DocManager.Submit(rel, 0, "", seq, op)` 로 보내고 돌아온 seq 가 seq+1 이 아니면 DocManager 글로 다시 맞춘다. 남의 편집(`EventDocApplied`·디스크 합침)은 seq 가 이어질 때만 적용하고 건너뛰면 다시 맞춘다. 되돌리기는 내 op 만(`ot.Undo`), 키 하나 = 되돌리기 한 단계(vim 처럼 삽입 한 번을 묶지 않음).
- 내 판단: 편집기가 내는 요청(되돌리기·저장·닫기)은 브로드캐스트하지 않고 표면의 `HandleKey` 안에서 바로 처리한다. 코드 칸은 esc 를 삼킨다(실수로 도구를 떠나지 않게; 칸 이동은 `ctrl+\` 1~4).
- 내 판단: **디스크 합침 알림의 u/d**. u 는 그 합침 뒤 아무 편집도 없을 때 합침을 되돌리는 편집(역연산)을 넣고, d 는 `Discard` 로 디스크 판으로 돌린다. 그 밖의 키를 치면 알림이 사라진다.
- 내 판단: 작업공간을 닫을 때 저장 안 된 글은 `DocManager.Tick(now+1분)` 으로 복구 일지에 바로 쓴다(1초 묶음을 기다리지 않음).
- 내 판단: **공유 시작은 대화상자의 enter 하나로**. space 가 켬/끔, enter 는 어느 줄에서든 공유 시작(별도 단추 없음). API 응답과 `Host.Start` 는 Cmd 안에서(네트워크) 하고, 결과 msg 를 받아 터미널 공유·크기·자동 저장을 건다. 파일을 끄면 `Root`·`Docs` 를 넘기지 않는다. 띠의 이름은 핸들(`minji[편집]`), 토스트·대화상자는 표시 이름.
- 내 판단: **키보드 확인**. 친 글자를 그대로(정화하지 않고) 받아 `typed == SanitizeInput(typed) && EqualFold(typed, handle)` 일 때만 허용한다 — 정화한 뒤 비교하면 표시 이름 `Jun​` 이 `jun` 과 같아져 계획의 시험 뜻과 어긋난다.
- 내 판단: (리뷰 보정에서 바뀜 — 아래) ~~참가 요청이 오면 토스트(벨 종류)와 함께 다른 대화상자가 없을 때 확인 숫자 대화상자를 바로 연다(밀리면 대기 줄에 두고 대화상자가 닫힐 때 다음 것을 연다).~~ `다름` 은 `Decline(seat, true)` 뒤 엔진의 `EventMITMWarning` 으로 경고 대화상자를 연다. 미확인 참가자의 편집 이상 올리기는 `ErrNeedsVerify` 토스트로 막는다(숫자 다시 비교 흐름은 만들지 않음).
- 내 판단: (리뷰 보정에서 바뀜 — 아래) ~~**오너용 입력 띠**는 만들지 않았다.~~
- 내 판단: **제안 대기열은 `ctrl+\` enter**(도구 PrefixKeys 에 enter 를 더함)와 팔레트 `ws.proposals` 로 연다. 계획의 "상태줄 enter" 는 트리·사람 칸의 enter 와 겹친다. 고쳐서 넣기는 한 줄 입력 칸이라 여러 줄 제안의 줄바꿈은 빈칸이 된다. 초소형에서는 요약 화면에서 바로 s/i/r/x.
- 내 판단: **공유 끝**. 끝내기 대화상자는 늘 연다 — 참가자 미저장 파일이 있으면 `저장 / 버리기 / 열어 두기`, 없으면 `공유 끝내기`, 그리고 공유 터미널이 있을 때 `프로그램에 중단 신호 보내기(ctrl+c)` 켬/끔. `Host.End` 는 Cmd 안에서, 실패하면 `Kill`. `ctrl+\ x` 킬 스위치 등록은 Task 24 몫이라 하지 않았다. 작업공간을 닫으면(Closer) 공유를 `End` 한다.
- 내 판단: `owner_only_answer` 는 참가자 화면(Task 23) 문구라 키만 넣고 오너 화면에는 쓰지 않았다.
- 내 판단: 골든의 확인 숫자는 무작위라 `SetSASForTest("482 913")` 로 고정했다. 패널은 초소형에서 열리지 않으므로 Task 22 골든은 120x40 에서 장면을 만든 뒤 시험 크기로 줄여 그린다. Task 22 의 초소형 요약이 Task 21 의 50x20 골든 두 개(`people`·`sharing_band`)와 Task 20 의 `TestGoldenSolo` 50x20·40x14 골든을 바꿨다 — 모두 이 묶음에서 새로 생긴 것이고 `-args -update` 로만 다시 만들었다.
- 내 판단: `cmd/masil` 이 `internal/ui/workspace` 를 아직 import 하지 않는다(도구 등록·팔레트 명령이 실제 바이너리에 들어가려면 필요). 이 묶음의 파일 목록에 없어 손대지 않았다.
- 내 판단: 이 묶음의 기록은 Task 22 커밋에 함께 넣었다.

**리뷰 보정 (Task 19~22 리뷰 지적 16건, 그중 2건은 같은 지적)**

- 내 판단: **초소형 요약의 파일 요청**. 이름 바꾸기·지우기 요청은 줄에 `{name} · 지우기: 경로` 로 종류를 보이고, 대기열 대화상자와 같은 규칙으로 `a` 로만 한다(`s`·`i` 는 글 제안에만). 그 줄을 골랐을 때 바닥 줄은 `tiny_owner_keys_fs`(`a 허용 · r 거절 · x 공유 끝`, 새 키).
- 내 판단: **대기열의 enter**. 대기열 대화상자에서 `enter` 는 아무 제안도 실행하지 않는다 — 넣기는 `i`, 넣고 보내기는 `s` 만(계획의 키 목록대로).
- 내 판단: **참가 요청은 저절로 대화상자를 열지 않는다**(B6.3 #2: 토스트 + 벨 + 대기 줄). 띠와 초소형 요약에 `참가 요청 {n}건(ctrl+\ enter 확인)`(`join_pending`, 새 키)을 보이고, 확인 숫자 대화상자는 `ctrl+\ enter`(기다리는 참가 요청이 있으면 그것부터, 없으면 제안 대기열)나 사람 칸의 `enter` 로만 연다. 대화상자를 닫아도 다음 요청을 이어서 열지 않는다. 그래서 스펙 B6.7 의 "`enter`로 대기 목록 오버레이" 는 `ctrl+\ enter` 다(트리·사람 칸의 enter 와 겹침) — **오너 확인 필요**.
- 내 판단: 띠의 제안 힌트는 실제 키를 쓴다: `제안 {n}건 대기(ctrl+\ enter 보기)`(키는 `keymap.TermhostEscape` 의 도움말 키 + ` enter`).
- 내 판단: **벨**. P0 토스트에는 소리가 없어 참가 요청 토스트와 함께 `tea.Raw("\a")` 로 BEL 을 터미널에 바로 보낸다(시험 훅 `ringBell`).
- 내 판단: **기록 위치**는 `ctx.Paths.Dir/workspace-audit`(B8.5 `~/.config/masil/workspace-audit/<공유ID>.jsonl`). 처음 구현은 `Paths.Cache` 아래였다. `Dir` 이 비면 기록을 열지 않는다.
- 내 판단: **파일을 끈 공유의 문서**. 엔진은 `Docs` 를 받았을 때만 DocManager 를 Tick 하므로, 파일을 끈 공유 동안에는 UI 의 펌프가 계속 `DocManager.Tick` 을 부른다(디스크 합침·복구 일지·자동 저장).
- 내 판단: **공유가 끝나면 터미널을 돌려준다**. 끝나는 모든 길(끝내기 대화상자·즉시 끊기·원격 끝·작업공간 닫기)에서 공유했던 터미널마다 `UnshareTerminal`(tap 해제)과 `SetFixedSize(0,0)`(고정 크기 풀기)을 한다. 엔진(`finishLocked`)은 고치지 않았다 — 잠금 아래에서 tap 을 떼지 않으려고.
- 내 판단: **시작 중에 닫기**. 시작 Cmd 와 인스턴스 사이에 인스턴스마다 `shareStarts`(잠금 + 닫힘 + 넘겨받을 엔진)를 둔다. 닫힌 뒤 끝난 시작은 Cmd 가 스스로 `End` 하고, 닫을 때 아직 설치되지 않은 시작은 닫기가 `End` 한다. 같은 폴더를 다시 열어 같은 id 의 새 인스턴스가 생겨도 옛 시작은 설치되지 않는다. 끝내기는 모두 Cmd 안에서 15초 시한으로, 실패하면 `Kill`, 기록 파일을 닫는다.
- 내 판단: **띠에 공유 이름**. `🔴 공유 중 · {n}명 · {title} · minji[편집] …` — B6.2 의 `🔴 공유 중 · 0명` 과 B6.7 의 `🔴 공유 중 · {title} · …` 를 함께 만족하게 사람 수 뒤에 이름을 넣었다(카탈로그 `band_sharing` 은 그대로). 이 가지에서 새로 생긴 `TestGoldenOwner` 의 `people`·`sharing_band`·`share_dialog` 골든만 `-args -update` 로 다시 만들었다.
- 내 판단: 오너 대화상자 골든(`TestGoldenOwner`, `TestGoldenOwner2` 의 `proposals_overlay`·`end_unsaved`)을 40x14 에서도 만든다.
- 내 판단: **팔레트**. `ws.share.start` 는 혼자일 때만, `ws.share.end`·`ws.proposals` 는 앞의 작업공간이 공유 중일 때만 보인다. **Task 24 에 넘김**: 이 세 id 는 이미 `register.go` 에서 등록되어 있다 — Task 24 가 `palette.go` 를 만들 때 이 셋을 그리로 옮기고 다시 등록하지 않는다(`palette.Register` 는 같은 id 에 panic).
- 내 판단: **커서**. `EventPresence` 를 문서별·자리별로 두고(뒤이은 편집마다 `ot.TransformIndex` 로 옮김) 편집기 `SetPeers` 로 그린다(이름은 핸들, 색은 `Theme.UserColor`). 오너의 커서는 코드 칸 키마다 바뀌었을 때만 `Host.Presence` 로 보낸다. 참가자가 나가거나 공유가 끝나면 지운다. 참가자가 문서를 닫은 것은 UI 에 알려지지 않아 그 커서는 다음 presence·나감까지 남는다.
- 내 판단: **오너용 입력 띠**. 엔진이 키보드 입력을 터미널에 넣은 직후 `EventInput{Seat, Term, Data}`(메모리만, 로그 금지)를 낸다. 키보드를 가진 사람이 있으면 상태줄(`StatusText`)이 어느 칸에서든 `마지막 입력: {handle} · 3초 전 · ⌨ {최근 40칸}` 을 보인다(터미널이 키를 가진 동안은 그 안내를 뒤에 붙임). 띠는 ANSI 를 벗기고 출력 가능한 글자만, Enter 는 `⏎`. 1분 안의 지난 시간은 초 단위(`ago_seconds`, 새 키).
- 내 판단: **방 멤버 초대**(JB18). 공유 대화상자에서 친구 목록 아래 `방 하나를 골라 그 멤버 초대`(`share_invite_space`)로 방을 고르면 `ListSpaceMembers`(첫 쪽, 최대 100명 — 서버 쪽 최대)를 불러 같은 초대 목록(최대 8)에 넣을 수 있다. 나·정지된 사람·이미 친구로 보인 사람은 빼고, 방을 다시 고르면 풀린다. 새 키 `share_space_loading`·`share_space_failed`.
- 내 판단: 이 보정의 기록은 마지막 보정 커밋에 함께 넣었다.

**6묶음 (Task 23~25 — 참가자 화면·즉시 끊기 가드·팔레트·alert·CLI·계약 마감·B16 수락 시험) / 내 판단**

- 내 판단: **참가 인스턴스 id 는 `workspace:join:<공유 ID>`**. 계획의 `ws:join:` 은 P0 `ui.OpenTool` 규칙("`<toolID>:` 로 시작")과 어긋나 바꿨다(Task 20 의 `workspace:` 와 같은 이유).
- 내 판단: 참가자 화면에는 사람 칸이 없다(트리·코드·터미널만). 참가자 목록은 띠의 이름들로 본다.
- 내 판단: **키프레임마다 `term_status` 를 다시 보낸다**(`internal/workspace/share/term.go`, 이 묶음 Files 밖의 엔진 수정). 상태는 공유를 걸 때·바뀔 때만 보내서 늦게 들어온 참가자는 제목·크기를 받지 못했다.
- 내 판단: `pan_more {n}` 의 n 은 창 오른쪽에 가려진 칸 수(계획 시험의 수치를 이 정의에 맞췄다). 이동 키는 한 칸씩(←→↑↓·hjkl), `home`/`g` 처음, `f`·`esc`·`q` 로 따라가기 복귀.
- 내 판단: **키 인코딩 `keyBytes` 를 직접 구현**(xterm 규칙). P0 `termhost.Replay` 에 키 인코더가 없다.
- 내 판단: 첫 키프레임 전에 온 터미널 출력은 버린다(재생 화면의 기준이 없음).
- 내 판단: 확인 숫자 대화상자의 `esc` 는 `다름` 이 아니다 — 대화상자만 닫히고 띠가 기다림을 보이며, `ctrl+\ enter` 로 다시 연다. `다름`·확인 전송 실패는 기존 `sas_mismatch_warning` 문구를 다시 쓴다.
- 내 판단: `dm_unencrypted`·`dm_hint` 키는 더했으나 대화 칸(B6.5)이 P2 파일 밖이라 그리지 않는다.
- 내 판단: 계획 밖 키를 더했다 — Task 23 `join_connecting`·`ended_hint`·`guest_tiny_keys`·`guest_term_empty`·`prop_sent/done/rejected/failed`, Task 24 `autosave_on/off`·`notify_failed`·`cli_usage`·`cli_not_dir`·`cli_not_logged_in`·`cli_stop_failed`.
- 내 판단: 저장 안 된 편집 수(`Unsaved`)는 참가자 띠에 보인다(제안 이상).
- 내 판단: `pump.go`·`tree.go`·`code.go`·`dlg_open.go` 를 최소로 고쳤다(Files 밖): 참가자 펌프, 트리 이동 함수 분리, 참가자에게 문서 관리자가 없을 때의 dirty, 폴더 고르기 맨 위의 "초대받은 공유" 목록.
- 내 판단: 참가자 시험은 오너를 UI 없이 `share.Host` 로 직접 돌린다(같은 relay).
- 내 판단: **즉시 끊기 가드와 셸 접두키의 충돌**. P0 셸은 킬 스위치가 켜져 있으면 키를 잡지 않은 화면에서 `ctrl+\` 를 먼저 가져가 `x` 말고는 버린다. 그대로면 공유 중인 작업공간 표면의 `ctrl+\ enter`·`4`·`S` 가 모두 죽는다. 그래서 가드의 `Active` = "살아 있는 공유가 있고, 앞에 있는 작업공간 표면이 `ctrl+\` 를 스스로 읽지 않을 때"(대화상자·고르기·키를 잡은 터미널이 없으면 표면이 읽음). 표면이 읽는 `ctrl+\ x` 는 표면이 가드를 바로 끊는다(`killNow`, 확인 없음). 표면 안에서 키를 잡은 터미널(termpane)은 셸 스위치를 묻으므로 그때는 다시 켜진다. 셸 오버레이(팔레트 등)가 작업공간 위에 떠 있는 동안의 `ctrl+\ x` 는 오버레이가 받는다 — `ui.KillSwitch.Active` 가 오버레이를 알 수 없어서다(P0 셸을 고치지 않음) — **오너 확인 필요**. 시험 `TestKillFromChatScreen`(다른 화면)·`TestKillOnWorkspaceItself`(표면 위, `ctrl+\ 4` 가 계속 동작).
- 내 판단: 즉시 끊기 뒤에는 토스트 `killed_toast` 하나와, 표면마다 `sharesKilledMsg` 로 띠를 바로 혼자 쓰기로 돌린다(엔진의 `EventEnded` 가 늦게 와도 `shareEnded` 는 한 번).
- 내 판단: **팔레트**. Task 22 가 `register.go` 에 등록한 세 id 를 `palette.go` 로 옮겼다. 새 이름은 기존 꼴 `작업공간: …`(`ws.open` 과 `ws.notify.toggle` 은 계획 문구 그대로 `작업공간 열기`·`작업공간 알림 켜기/끄기`). `ws.term.new`·`ws.autosave.toggle` 은 앞의 작업공간에 폴더가 열렸을 때, `ws.people` 은 공유 중일 때, `ws.join` 은 초대가 있을 때(하나면 바로 참가, 여럿이면 고르기). 자동 저장 상태는 표면이 들고(`autosave`), 공유가 끝나면 꺼진다(기존 동작).
- 내 판단: `ws.notify.toggle` 은 PATCH 만 보내고 Store 를 미리 바꾸지 않는다 — 서버의 `settings.updated` 실시간 이벤트가 Store 를 맞춘다. 토스트는 요청한 값.
- 내 판단: **alert 열기**. 내 공유(살아 있는 오너 표면의 공유 ID)면 그 표면 경로로, 아니면 참가 화면. 플래그가 꺼져 있으면 nil(알림함에 남음).
- 내 판단: **CLI**. `masil workspace` 는 `Hidden: !features.On("workspace")`(init 시점)로 출시 전 도움말에 안 보이고, 플래그가 꺼져 있으면 실행해도 사용법 오류(종료 2). `open` 은 경로 없음·폴더 아님·파일이면 종료 2, 아니면 `workspace.OpenRoute(abs)`(새로 내보낸 함수)로 TUI 시작. `stop` 은 로그인 정보가 없으면 종료 1(요청 0), id 하나면 그것만, 없으면 `ListWorkspaceShares("owner","live")` 를 모두 끝내고 `cli_stopped`/`cli_no_shares`. 일부가 실패해도 나머지는 끝내고 종료 1. 서버는 로그인 정보의 서버(`masil logout` 과 같음). 문구는 `i18n.For(i18n.Ko)`(한글 리터럴 0).
- 내 판단: `cmd/masil/register_workspace.go` 로 작업공간 패키지가 처음 바이너리에 들어간다(5묶음의 미결 사항 해소). 플래그 기본 꺼짐이라 기존 골든은 바뀌지 않았다(`git status --porcelain -- '*.golden'` 비어 있음).
- 내 판단: 시험이 한 프로세스에서 여러 하네스를 돌리므로 `setup` 이 앞 시험이 남긴 표면·공유를 잊는다(`forgetSurfaces`) — 남은 표면이 "앞에 있음" 으로 보여 가드를 끄던 것을 막는다.
- 내 판단: 계약 10.3 에 P2 필수 시험 줄을 더했다(OT 무작위·키 교환/SAS·경로·B16·즉시 끊기). 3.2 의 감사 기록 행은 실제 위치 `~/.config/masil/workspace-audit/<공유 ID>.jsonl`, 복구 일지는 `<캐시>/workspace-journal/<폴더 해시>/`.
- 내 판단: `TestAuditCoversLadder` 는 참가 승인 때의 단계를 `join_allow` 로 적기 때문에 `level` 줄을 보려고 단계를 한 번 내렸다 올린다. 오너 자신의 키는 엔진을 거치지 않으므로(오너는 패널에 바로 침) seat 0 의 `keyboard_input` 은 구조적으로 없다.
- 내 판단: `TestEndFromOtherDevice` 의 "다른 기기에서 끝냄" 은 서버가 REST DELETE 를 받고 하는 일(relay `End()`, 4100)로 흉내 낸다.
- 내 판단: `TestNoFolderDataInAnyRequest` 는 REST 기록과 relay 가 보는 모든 프레임(전달된 프레임·서버 프레임·오너의 제어 프레임)을 통째로 검사한다 — 평문 머리·서버 페이로드뿐 아니라 암호문 자리에도 폴더 경로·파일 이름·내용 조각이 0번.

**6묶음 (Task 23~25 — 참가자 화면·즉시 끊기 가드·팔레트·alert·CLI·계약 마감·B16 수락 시험) / 리뷰 보정 (Task 23~25)**

- 내 판단: **오버레이 위의 즉시 끊기**. P0 는 도구에 셸 오버레이(팔레트·도움말·확인 창)를 알리지 않는다. 대신 P0 계약(CA 5.3.7)대로 셸은 매 프레임 그린 뒤 오버레이가 없을 때만 포커스 컴포넌트의 `Cursor()` 를 묻고, 오버레이가 없을 때만 키를 준다. 그래서 표면이 그려진 횟수(`drawn`)와 마지막 커서 질문·키 때의 횟수(`cursorAt`)를 세어 `drawn > cursorAt` 이면 위에 오버레이가 있다고 보고, 가드가 셸 스위치를 켠다(`ctrl+\ x` 가 오버레이로 가지 않고 끊는다). P0 파일은 고치지 않았다(소유 표). 실제 프로그램은 Update 마다 `View` 로 그리므로 정확하다. 시험 하네스의 `Model.Frame()`(uitest `Golden` 이 View 뒤에 부름)은 커서를 묻지 않고 다시 그려 오버레이처럼 보이므로, 표면이 읽어야 할 `ctrl+\` 키 앞에서는 `h.Screen()` 으로 한 번 그린다 — `TestGoldenOwner` 에 그 한 줄을 더했다(골든 내용 무변화). 시험 `TestKillWithOverlayOverSharingWorkspace`(팔레트·도움말), `TestOverlayClosedSurfacePrefixBack`.
- 내 판단: 참가자의 "공유가 끝났어요" 화면은 `ctrl+\` 를 읽지 않는 화면으로 본다(아무 키나 닫으므로). 내 공유가 살아 있으면 셸 스위치가 `ctrl+\ x` 로 끊는다. 시험 `TestKillFromGuestEndedNotice`.
- 내 판단: 끝난 화면의 아무 키 → `ui.Navigate(허브)` 와 `ui.CloseTool` 을 함께 보낸다(둘이 어느 순서로 와도 허브가 앞에 남는다). 시험 `TestGuestEndedKeyGoesToHub`.
- 내 판단: **참가자가 받는 터미널 크기 상한** `share.MaxTermCols=500`·`MaxTermRows=300`. 엔진(`share.Guest`)이 `term_status` 의 크기를 이 범위로 자르고(음수는 0), 재생 화면(`replayTerm.size`)도 한 번 더 자른다. 변조된 오너 클라이언트가 큰 크기로 참가자의 메모리를 다 쓰게 하지 못한다(Review Focus 5). 오너 창이 이보다 크면 참가자 화면은 잘린 크기로 그린다.
- 내 판단: 팔레트 제목은 계획 문구 그대로로 바꿨다(`작업공간 공유 시작`·`공유 끝`·`참가자`·`제안 대기 목록`·`초대받은 공유 참가`·`새 터미널`·`자동 저장 켜기/끄기`). 위의 "기존 꼴 `작업공간: …`" 판단을 대신한다. en·zh-Hans 문구는 그대로 두었다(P5 영역의 번역).
- 내 판단: 참가자 화면에 사람 칸을 더했다(B6.7 "같은 배치"): 위젯 하위 탭 `사람(n)`·`ctrl+\ 4`, 지휘소 오른쪽 사람 칸. 읽기 전용(단계는 오너가 정함), 줄은 `{핸들} {단계}`(오프라인 표시). 위의 "참가자 화면에는 사람 칸이 없다" 판단을 대신한다. 이 가지에서 Task 23 이 만든 `TestGoldenGuest` 의 `200x60`·`80x24` `guest_editing` 과 `guest_terminal_pan` 골든만 `-args -update` 로 다시 만들었다.
- 내 판단: CLI `masil workspace stop` 의 오류 문장은 `errtext.Describe(i18n.For(i18n.Ko), err)`. 지금은 `api.Describe` 와 출력이 같아(errtext 가 아직 0.1.2 한국어 줄을 씀) 실패하는 시험을 먼저 만들 수 없었다 — 실패 경로 시험 `TestCLIWorkspaceStopFailure` 를 더했다.
- 내 판단: `TestViewerIsReadOnly` 는 참가자 연결이 보낸 프레임을 모두 기록해 `doc_op` 0개를 보고, 편집기가 읽기 전용인지·내 사본이 그대로인지도 본다(엔진이 보기 단계의 `Edit` 를 거절하므로 프레임만으로는 UI 회귀를 못 잡는다).

(P2: 161줄)

### P3 (`exec-log/P3.md`)


**1묶음 (Task 1~6 — UI 없는 핵심 모델) / 내 판단**

- 내 판단: `Decode` 는 빠진 필드를 그 객체의 기본값(노드 = `Defaults(kind)`, 연결 = `DefaultEdge`, `traffic` = `DefaultTraffic`, 상자 `rect` = `[0,0,32,18]`, `duration_s` = 300)으로 채운다. "앞으로의 필드 허용"과 손으로 쓴 JSON 을 함께 받으려는 것이고, `Encode` 는 늘 모든 필드를 쓰므로 왕복은 바뀌지 않는다. `null` 은 빠진 것과 같다.
- 내 판단: `Decode` 는 목록을 ID 순으로 정렬하고 빈 목록은 nil 로 돌려준다 — `FuzzDecode` 의 "Decode(Encode(d)) == d" 가 입력 순서와 무관하게 성립하게. `FromObjects` 도 같은 규칙.
- 내 판단: 이슈 코드에 `syntax`(JSON 객체 하나가 아님·뒤에 다른 값)를 더했다. 계획 목록에 없지만 깨진 파일을 `ErrInvalid` 로 돌려야 해서(Task 4 `TestStoreBrokenFileListed`).
- 내 판단: 열거값 위반은 모두 `unknown_kind`(`kind`·`disk.type`·`params.lb_algo`·`traffic.curve`·`protocol`), 타입 틀림·정수 아님·`id` 모양·문자열 64바이트 초과는 `range`, 상자 부모 순환은 `depth`.
- 내 판단: 새 설계의 시드는 ULID 의 FNV-64a 를 2⁵³ 미만으로 자른다(서버 JavaScript 수 왕복에서 깨지지 않게). `Decode` 는 계획대로 uint64 전체를 받는다.
- 내 판단: `id` 는 `^[A-Za-z0-9_-]{1,62}$` — 협업 키 `n:<id>` 가 collab.md 1.2 의 64자 안에 들게.
- 내 판단: 계획이 범위를 적지 않은 필드는 내가 정해 `arch.md` 1.4 표에 적었다(`autoscale.min/max` 1..1000, `warmup_s`·`cooldown_s` 0..3600, `disk.size_gb` 0..65536, `disk.iops` 0..256000, `custom_hourly_micro_usd` 0..10¹², `params.*`, `traffic.spike_mult` 1..100, 장애 `count` 0..1000 등). 상자 `rect` 의 w·h 는 1 이상(계획은 "각 −100,000..100,000").
- 내 판단: `provider`·`instance_type`·`region` 은 값을 검사하지 않고 64바이트만 본다 — 뒤 버전의 사업자·리전을 옛 클라이언트가 거부하지 않게.
- 내 판단: `params.failover_to` 가 자기 자신이면 `dangling_ref`. `autoscale.enabled` 인데 `min > max` 면 `range`(`autoscale.max`).
- 내 판단: 상자 부모 규칙은 `region` = 최상위만, `vpc` = 최상위 또는 region 안, `az` = 최상위 또는 vpc 안. 최상위 vpc·az 는 검증을 통과하고 설계 점검(`vpc_outside_region`·`az_outside_vpc`)이 경고한다.
- 내 판단: 계획에 없는 겉면을 더 내보냈다 — `DefaultEdge()`, `DefaultTraffic()`, `BoxKinds()`, `DiskTypes()`, `MetaKey`, `MaxName`, `UndoCap`, `(*UndoLog).CanUndo/CanRedo`, `Issue.String`.
- 내 판단: 부품별 기본값(빌더 재량): 요청당 CPU API 8,000µs·worker 20,000·cache 200·DB 2,000·search 30,000·LB 100·CDN 50·object storage 100·queue 100, 인스턴스는 primary·replica·queue 1 대 나머지 2 대, CDN 적중 800‰·cache 900‰, DB 읽기 0.5·쓰기 1.25 IOPS/요청, 풀 100, 장애 조치 30초, 복제 지연 20ms, 외부 서비스 고정 지연 100ms·한도 1,000rps. AWS 기본 종류의 CPU·메모리·NIC 은 가격표 값과 같다(pricing 시험이 확인). client·external 은 사업자가 `""`(비용 없음).
- 내 판단: 관대한 적재에서 `kind` 를 모르는 노드·상자·장애는 뺀다. ~~상자 종류 규칙(`parent_kind`)은 고치지 않는다~~(리뷰로 바뀜 — 아래 "리뷰 반영") — 계획의 "순환은 가장 작은 ID 에서 끊음·깊이 초과는 조부모로" 규칙이 종류가 틀린 부모(예: vpc 안 vpc)를 전제로 하기 때문. 깊이 초과는 ID 순으로 한 상자씩 3단 안에 들 때까지 올린다. 이슈는 (경로, 코드) 순으로 정렬해 돌려준다.
- 내 판단: `ChangePut`·`ChangeRestore` 의 `Value` 는 필드 JSON 객체, `ChangeSet` 의 `Value` nil 은 그 필드를 지움(로컬 미리보기 전용 — `DiffObjects` 가 사라진 필드에 쓴다).
- 내 판단: 설계 점검 세부 — `spof_single_instance` 는 api·worker·cache·db_primary·db_replica·queue·search 중 인스턴스 1대(자동 확장 최소 2 이상 제외)이고 앞에 LB 가 없는 노드, 장애 조치 대상이 있는 primary 는 제외(object storage 는 관리형이라 제외). `spof_single_lb` = 인스턴스 1대인 LB. `primary_replica_same_az` = 같은 AZ 의 primary·replica 쌍마다 하나. `single_az_service` = 종류별(client·CDN·external·DB 제외)로 그 종류 노드가 모두 한 AZ 안에 있을 때. `unconnected`·`spof_*`·상자 경고는 대상 하나당 Finding 하나, `cycle` 은 강결합 요소 하나당 하나(자기 고리 포함).
- 내 판단: 자동 정리는 원점 (0,0) 에서 시작, 상자 안쪽 여백 좌우 2·위 2(제목 줄)·아래 1칸. 상자의 열 = 안에 든 노드의 가장 작은 열, 빈 상자는 0열. 순환의 역방향 간선은 client 노드부터(그다음 나머지) ID 순 DFS 로 정한다. 정리한 설계를 다시 정리하면 변경이 없다.
- 내 판단: 되돌리기 기록은 아무것도 바꾸지 않은 단계를 버리고, 모든 변경을 건너뛰어도 `Undo` 는 한 단계 물러난다. 내가 만든 객체를 남이 고쳤어도 그 만들기를 되돌리면 지운다(되살리기로 돌아옴).
- 내 판단: 설계 이름은 계획의 금지 목록에 더해 `.` 으로 시작하는 이름(`config.WriteFileAtomic` 의 임시 파일 `.이름.tmp-…` 과 겹침), 보이지 않는 문자(`textwidth.Invisible` — 양방향 제어 등), ~~250바이트~~ 223바이트 초과(리뷰로 바뀜 — 아래)를 거부한다. 쓰기는 계획대로 `config.WriteFileAtomic`(경로 기반, 이름에 구분자가 없음을 `SafeName` 이 보장)이고, 읽기·이름 바꾸기·지우기·존재 확인은 `os.Root` 안에서 한다(밖을 가리키는 심볼릭 링크는 따라가지 않음 — 시험). `Save` 는 검증을 통과하지 못한 설계를 거부하고, `Rename` 도 대상이 있으면 `ErrExists`. `List` 는 이름이 `SafeName` 을 통과하고 NFC 그대로인 정규 파일만, 1MB 넘는 파일은 열지 않고 제목을 비운다.
- 내 판단: **가격표 값은 네트워크로 확인하지 못했다.** 각 사업자 공개 가격 페이지의 정가를 내가 아는 대로 손으로 옮겼고, 종류마다 따로 옮기지 않은 리전은 기준 리전(us-east-1·us-central1·eastus) 값 × 그 계열의 리전 비율이다(표 `note` 에 그대로 적음). `AsOf` = `2026-11`. 출시 전 오너가 `Sources` 페이지와 대조해야 한다 — 색인 4절 오너 확인 표에 넣을 후보(색인은 P3 소유가 아니라 고치지 않았다).
- 내 판단: LB·객체 저장은 사용량 과금이라 가상 종류를 Instances 에 둔다 — `alb`·`cloud-lb`·`standard-lb`(시간당 = LB 시간 가격), `s3`·`gcs`·`blob-hot`(명목 1µUSD, 저장은 `ObjectGBMonthMicroUSD`). Task 7 비용은 LB 시간을 인스턴스 시간 가격으로 한 번만 세고 `LBHourlyMicroUSD` 와 겹쳐 세지 말 것. queue 의 AWS 기본은 Amazon MQ `mq.m5.large`, cache·DB·search 는 ElastiCache·RDS·OpenSearch 대표 종류. GCP·Azure 는 관리형 대신 범용 VM 으로 대표한다(`Managed` 는 lb·object_storage 만).
- 내 판단: 표 파일에 사업자별 `defaults`(종류 → 기본 인스턴스 종류)를 두고 `DefaultType` 이 그것을 쓴다. 시험은 계획(AWS 만)보다 넓게 세 사업자 모두에서 기본 종류가 모든 리전에 있는지 본다.
- 내 판단: `RegionLatencyMicros` 는 왕복 기본 지연이다(같은 AZ 0.2ms·AZ 간 1ms 와 같은 뜻). 리전을 도시로 묶어(서울 = ap-northeast-2·asia-northeast3·koreacentral 등) 같은 도시의 다른 사업자는 2ms, 도시 사이는 표값, 모르면 150ms.
- 내 판단: FMA 검사(`TestNoFusedMultiplyAdd`)를 계획보다 엄격하게 했다. Go 명세는 문장 사이 합침(`t = x*y; r = t + z`)도 허용하고 arm64 는 SSA 값 단위로 합치므로, 부동소수 곱은 `float64(…)` 변환·다른 곱/나눗셈·비교의 직접 피연산자일 때만 통과한다(변수에 담기·반환·함수 인자·`x *= y` 도 잡음). Task 7·8 은 `t := float64(x*y)` 꼴로 써야 한다. 검사기 자체를 고정하는 시험(`TestFMADetectorFindsFusable`)을 더했다.
- 내 판단: `split(stream)` 은 부모를 진행시키지 않는다. ~~상태 ^ stream·황금비 상수(계획 그대로), stream 0 은 부모와 같은 흐름~~ — 리뷰로 바뀜, 아래 "리뷰 반영"의 Task 6 인터페이스 수정. `sample` 은 부하와 상관없이 난수를 세 번 써서 흐름이 어긋나지 않게 했고, 큰 c 에서 Erlang C 를 매번 다시 세지 않도록 `waitProb`·`sampleP`·`capacity` 를 더했다. 퇴화 입력(창구 0·속도 0·유입 0·음수)은 NaN·음수 없이 0 또는 +Inf.
- 내 판단: `TestDetLogClose` 의 최소 subnormal 은 `math.Log` 와 비교하지 않고 정확값 −744.4400719213812 로 고정했다 — amd64 의 어셈블리 `math.Log` 가 −709.08… 을 낸다(`GOARCH=amd64` Rosetta 로 확인). `detLog`·`exp`·`erlangC`·`sample` 10만 회 체크섬이 darwin/arm64 와 darwin/amd64(Rosetta)에서 비트까지 같음을 확인했다(일회성 확인, 시험으로 남기지 않음 — 고정 묶음 골든은 Task 7).
- 내 판단: `detLog` 는 Go `math/log.go` 를 옮긴 것이라 출처 주석에 Go(BSD 계열)와 FreeBSD 원저작 고지를 함께 적었다.

**1묶음 (Task 1~6 — UI 없는 핵심 모델) / 리뷰 반영 (1묶음 리뷰 9건)**

- 내 판단: `Decode` 는 목록 길이가 한도(300·100·1,000·50)를 넘으면 항목을 하나도 읽지 않고 `too_many_*` 하나만 낸다(1MB 에 빈 객체 35만 개 → 2GB 할당·이슈 419만 개였음). 이슈 목록 자체에 상한(예: 100개 + 잘림 표시)은 두지 않았다 — 목록 한도만으로 이슈 수가 수만 개로 묶이고, 새 이슈 코드는 계약을 넓히기 때문.
- 내 판단: 1MB 한도는 입력과 **정규 인코딩** 둘 다에 건다(`Decode` 가 끝에서 `Encode` 크기를 봄, 넘으면 `too_large`). 라벨에 바이트 한도를 더하는 길은 택하지 않았다 — 40 그래핌 이모지 ZWJ 라벨(한 그래핌 25바이트 이상)을 깨지 않으려고. `Validate` 는 크기를 보지 않는다(`Store.Save` 가 이미 `Encode` 크기를 봄). 반례를 `TestDecodeLimits/canonical_too_large` 와 `FuzzDecode` 씨앗으로 남겼다.
- 내 판단(**P3 Task 6 인터페이스 수정 — Task 7·8 은 이것을 쓴다**): `split(stream)` = `&rng{s: mix64(r.s ^ mix64(stream·golden + golden))}`(`mix64` = SplitMix64 출력 함수). 계획의 `seed ^ stream*0x9e37…` 은 상태와 stream·golden 의 켜진 비트가 겹치지 않으면(시드 0·작은 시드) XOR = 덧셈이라 자식이 부모 흐름을 stream 칸 민 복사본이 된다(시드 0: `split(1..4)` 첫 값 = 부모 1..4번째 값). 이제 stream 0 도 부모와 다르다. `TestSplitStreamsDoNotOverlap`(시드 0·1·2·42·2⁵²·2⁶⁴−1, stream 0..64, 각 256개 값이 서로·부모와 안 겹침). 계획 문서 330행은 P3 소유 계획이지만 실행 기록을 단일 출처로 남기고 고치지 않았다 — 오케스트레이터가 계획에도 반영할 것.
- 내 판단: `FromObjects` 는 결과가 늘 `Validate` 를 통과한다(`TestFromObjectsAlwaysValid` — 무작위 객체 묶음 3,000개). (1) 같은 `id` 를 다른 종류가 쓰면(`n:x`·`b:x`) 노드 → 상자 → 연결 → 장애 순으로 앞 것만 남기고 뒤 것은 `duplicate_id`(경로 = 객체 키)로 뺀다 — 노드가 가장 많이 참조되므로 노드를 남김, 키 정렬 순(`b:` < `n:`)이 아님. (2) 순환 끊기·깊이 올리기 뒤에 종류 규칙을 어긴 상자 부모를 `""` 로(`parent_kind`). 순환은 종류 위반 없이 생길 수 없으므로 "가장 작은 ID 에서 끊음" 규칙은 그대로 먼저 돌고(이슈 `depth`), 나머지 위반 상자가 최상위로 간다 — `TestFromObjectsDanglingAndCycles` 의 기대(둘 중 하나만 최상위)를 이슈 기준으로 고쳤다. 정규 인코딩 1MB 는 보장하지 않으므로 내보내기·카드 사본(Task 26)은 `Encode` 크기를 따로 본다(`arch.md` 2.3 8).
- 내 판단: `label`·`title` 은 제어 문자(C0·C1·DEL), 양방향 표시·내장·재정의·격리(U+061C·U+200E·U+200F·U+202A–E·U+2066–9), 줄·문단 구분(U+2028·2029)을 `range` 로 거부한다. `textwidth.Invisible` 은 ZWJ·이체 선택자까지 잡아 이모지 라벨을 깨므로 쓰지 않았다. 서버 검증기(Task 21)는 같은 집합을 쓴다(`arch.md` 1.3).
- 내 판단: **P3 계획 1040행(Task 21)의 객체 키 정규식 `^(meta|[nbef]:[0-9a-z_]{1,40})$` 는 대체됨** — 정확한 정규식은 `^(meta|[nbef]:[A-Za-z0-9_-]{1,62})$`(`arch.md` 2.1). `NewID` 가 `n_01JA…` 처럼 대문자 ULID 를 내므로 계획 정규식이면 실제 클라이언트의 모든 `put` 이 `arch_bad_object` 로 거부된다. 색인·계획은 고치지 않았다(오케스트레이터 몫).
- 내 판단: 설계 이름 바이트 한도 250 → 223(= 255 − `.` − `.json` − `.tmp-` − pid 10자리 − `-` − 난수 10자리). `config.WriteFileAtomic` 의 임시 파일 이름이 ext4·xfs·btrfs 의 255바이트 한도에 들게. `Rename` 은 대상이 있어도 `os.SameFile` 로 같은 파일(대소문자만 바꾸기, APFS·NTFS)이면 허락한다. Windows 의 `os.Root.Lstat` + `os.SameFile` 은 이 Mac 에서 확인하지 못했다(`GOOS=windows go vet` 만).
- 내 판단(**미룸 — 오너 확인 필요**): 가격표(Task 5)는 고치지 않았다. 값은 네트워크 없이 기억으로 옮긴 정가이고 일부 리전은 비율로 만든 값, GCP·Azure 의 cache·DB·queue·search 는 관리형 대신 범용 VM 이다. `as_of: "2026-11"` 은 대조를 거친 날짜가 아니다. 어느 리전이 비율 값인지 표에 표시가 없어 지금 `derived` 표시를 붙일 수 없다. **색인 4절 오너 확인 표에 다음 줄을 더해 달라(P3 는 색인 소유 아님)**: "P3 Task 5 가격표를 Sources 페이지와 대조, 비율로 만든 리전 값 교체, GCP·Azure 관리형 대표 가격 결정". 그 확인 전에는 Task 5 가 C2.10 을 다 채웠다고 보지 않는다.

**2묶음 (Task 7~11 — 엔진·고정 묶음·내보내기) / 내 판단 — Task 7 (즉시 분석·비용)**

- 내 판단: 위상 정렬은 client 부터(그다음 나머지) ID 순 DFS, 스택 위 노드로 가는 연결(자기 고리 포함)은 흐름 0 — `EdgeResult` 에는 RPS 0 으로 남긴다. 장애 조치 링크(primary → `failover_to`)도 정렬의 간선으로 넣어 승격될 노드가 늘 primary 뒤에 오게 했고, 그 링크가 순환을 닫으면 장애 조치를 없앤다.
- 내 판단: 경로 비율 — 한 노드에서 나가는 연결의 읽기·쓰기 몫 합이 1000 을 넘으면 합으로 나누고, 모자란 몫은 그 노드에서 끝(성공). 쓰기 몫이 replica 로 가는 연결은: 출발이 DB 노드(primary·replica)면 그냥 끝(복제로 봄), 아니면 그 노드의 primary 연결들에 쓰기 몫 비율대로 옮기고(몫이 모두 0 이면 똑같이), primary 연결이 없으면 에러로 센다.
- 내 판단: 부하(attempt)와 원래 요청을 따로 흘린다. 노드는 처리한 것(과부하면 처리 능력만큼)을 아래로 넘긴다 — 타임아웃된 요청도 아래 처리 능력을 쓴다(스펙). 연결 u→v 의 attempt = 넘긴 양 × Σ_{k=0..R} P^k(R = u 의 Retries, 0..10 으로 자름, P = 1 − (1 − 연결 손실)(1 − v 의 attempt 실패 확률)), 원래 요청은 그 연결에서 P^(R+1) 만큼 실패. 전체 에러율 = 실패한 원래 요청 / client RPS, 노드 `ErrPermille` = 그 노드의 attempt 실패 확률. async 연결 아래로는 부하만 가고 원래 요청은 넣는 순간 끝난다(성공/실패만 셈).
- 내 판단: attempt 실패 확률 = 1 − (1 − 과부하 버림)(1 − 타임아웃)(1 − `params.error_permille`). 과부하 버림 = 1 − 처리 능력/유입, 타임아웃(ρ<1) = C·e^{−(cμ−λ)T}(T = 그 노드 `timeout_ms`). 과부하 노드의 지연은 p50·p99 모두 `timeout_ms` 이고 타임아웃 몫을 따로 더하지 않는다(시험의 333‰). ρ 가 정확히 1 이어도 과부하로 본다(M/M/c 가 정의되지 않음).
- 내 판단: 한도 후보와 단위 — CPU = 인스턴스 × 코어 × 1e6 / CPU µs, IOPS = 디스크 IOPS × 인스턴스 ÷ 요청당 IOPS(읽기·쓰기 milli-IOPS 를 섞은 비율로; DB 는 작업 데이터 > 메모리 × 인스턴스면 읽기 IOPS × 작업/메모리), NIC = NIC Mbps × 인스턴스 ÷ 요청당 비트((요청 + 응답 KB) × 8192 — 1KB = 1024B, 1Mbps = 10⁶ bit/s), 풀 = `pool_max` × 1e6 / CPU µs(Little 의 법칙, 서비스 시간 = CPU 시간), external·object_storage 의 `max_rps` → `ext_limit`, queue 의 `max_rps` → `queue`. `max_rps` 0 = 한도 없음. 값이 같으면 cpu → iops → nic → pool → ext_limit → queue 순.
- 내 판단: `link` 한도는 연결에 자기 대역폭(`mbps` > 0)이 있을 때만 사용률·손실(1 − 1/사용률)을 낸다. `mbps` 0 이면 끝점 NIC(정적 인스턴스 수)로 전송 시간만 계산하고 사용률은 0(노드 NIC 한도와 겹쳐 세지 않음).
- 내 판단: 줄서기 대기는 CPU 창구(c = 인스턴스 × 코어, μ = 1e6 / CPU µs)에 "가장 먼저 찬 한도의 사용률"을 입힌 M/M/c 다 — 서비스 시간은 CPU 그대로, 대기는 병목이 무엇이든 그 사용률을 따른다(IOPS 98% 면 대기도 98% 의 줄). CPU µs 0(검증 안 거친 사본)이면 대기·서비스 0, 창구 수는 10⁶ 에서 자른다.
- 내 판단: 노드별 p50/p99 는 표본이 아니라 M/M/c 응답 시간 W+S 의 정확한 생존 함수((1−C)e^{−μt} + C·(θe^{−μt} − μe^{−θt})/(θ−μ))를 이분법으로 0.1µs 까지 풀어 타임아웃에서 자르고 고정 지연을 더한다. 노드마다 표본을 뽑으면 노드 300 에서 5ms 를 못 지킨다. 전 구간 p50/p99 만 표본(시드의 `split(1)`).
- 내 판단: 표본 경로 — client 를 RPS 비율로, 읽기/쓰기를 읽기 비율로 고르고 연결 지연 + 노드 대기·서비스 + 고정 지연을 더한다. 꺼진 노드에서는 지연 없이 끝, 과부하 노드나 대기+서비스가 타임아웃을 넘으면 타임아웃을 더하고 끝, cache·CDN 적중·async 연결·남은 몫에서 끝. 재시도는 표본에 넣지 않았다(부하·에러에는 들어감).
- 내 판단: 연결 기본 지연(`latency_micros` −1) — 끝점 하나라도 상자 밖 client·CDN·external 이면 20ms, 두 노드의 리전이 모두 정해지고 다르면 `pricing.RegionLatencyMicros`, 같은 AZ(둘 다 AZ 밖인 경우 포함) 200µs, 그 밖 1ms. 노드 리전 = `region` 이 비면 둘러싼 리전 상자의 코드.
- 내 판단: 즉시 분석의 자동 확장 = CPU 사용률이 목표 이하가 되는 가장 적은 대수 ceil(λ·CPU µs / (코어·1e6·목표))를 Min..Max 로 자름. Min > Max(검증 안 된 사본)면 Max 를 Min 으로 올려 Min 고정. 인스턴스 ≤ 0 = `Down`(attempt 실패 1000‰), 코어 ≤ 0 = CPU 처리 능력 0 → 과부하, NIC 0 → `nic` 한도 0 → 과부하.
- 내 판단: 정수는 모두 `toInt` 로 한 번 반올림(NaN·음수 → 0, 10¹⁵ 위는 10¹⁵). 사용률 permille 은 상한 없음(1500 가능), 에러 permille 만 1000 상한. `CapacityRPS` 는 아무 한도도 없으면 0, client 노드는 `InRPS` = 보내는 RPS 외 0.
- 내 판단: `Bottleneck` = 사용률이 가장 높은 노드(꺼짐 제외)·연결, 같으면 노드 먼저·ID 순, 부하가 없으면 nil. `PerUnitMilli` — cpu·pool = 요청당 CPU µs, iops = 많은 쪽 요청 종류의 milli-IOPS(`Demand` 도 그 종류의 RPS), nic·link = 요청당 KB × 1000, ext_limit·queue = 1000.
- 내 판단: 고정점은 20회 또는 모든 실패 확률·연결 손실의 변화 < 1e-9 에서 멈춘다. `detExp`(Go `math/exp.go` 순수 Go 판, 곱마다 `float64()`)를 Task 6 의 `detmath.go` 에 더했다(같은 줄기 파일).
- 내 판단: 비용 — 가격 리전 = 노드 리전, 표에 없으면 기준 리전(aws us-east-1·gcp us-central1·azure eastus). 인스턴스 종류가 그 리전 표에 없으면 시간 가격 0(다른 종류로 바꿔 끼우지 않음). 디스크는 lb·cdn·object_storage 밖의 노드에 크기 × GB 가격 × 인스턴스, IOPS 추가 요금은 ssd = 3,000 넘는 몫, ssd_fast = 전부, hdd = 0. object_storage 는 `disk.size_gb` 를 저장 GB 로. LB 처리량 = 들어오는 attempt × KB × 2,628,000초 ÷ 1,048,576(KB → GB) × `LBGBMicroUSD`(시간 가격은 인스턴스 가격으로 한 번만). 인터넷 전송 = client RPS × 응답 KB × 2,628,000초를 client 의 첫 홉 노드에 경로 비율대로 나눠 그 노드 리전의 전송 가격으로 매김. custom = `custom_hourly_micro_usd` × 730 × 인스턴스. client·external 은 0.
- 내 판단: `MonthlyCost(d, m)` 는 맵에 없는 노드를 자기 `instances`(자동 확장이면 Min..Max 로 자른 값)로 세고, 사용량(전송·LB)은 정상 상태 흐름으로 계산한다. `Analyze` 의 비용은 자동 확장된 대수.
- 내 판단: 벤치 `BenchmarkAnalyze300` = 5.7ms/op(M1 Pro, 다른 줄기 작업으로 load average ~18 일 때). 첫 구현은 16ms 였고 경로 구성의 O(N·E) 와 이분법 60회 고정을 없앴다. `TestAnalyzeBudget`(MASIL_PERF=1, 5ms)은 이 부하에서 통과를 확인하지 못했다 — 조용한 기계에서 다시 볼 것.

**2묶음 (Task 7~11 — 엔진·고정 묶음·내보내기) / 내 판단 — Task 8 (시간 실행)**

- 내 판단: tick i = [0.1i, 0.1(i+1))초, 사건의 `T10` = 일어난 tick. 프레임은 `FrameEveryTicks` 마다, `T10` = 묶음의 끝(10, 20, …), `RPS`·`ErrPermille` = 묶음 평균, 나머지는 마지막 tick 값. 길이 = `opt.DurationS` → 설계 `duration_s` → 300, 600 상한(60 하한은 두지 않음 — 시험·짧은 미리보기).
- 내 판단: 과부하·타임아웃의 실패 확률은 앞 tick 값을 쓴다(재시도 폭주가 시간에 따라 자람). 꺼진 노드는 그 tick 에 바로 1.
- 내 판단: 줄 — 도착 λ·0.1 을 줄에 더하고 처리 능력 × 0.1 만큼 처리, `queue_cap` 을 넘는 몫을 버림. 줄 대기 = 줄 / 처리 능력이고, 그것이 타임아웃을 넘으면 그 tick 처리분 전부 타임아웃 실패, 줄이 비면 정상 상태 타임아웃 확률. 프레임 사용률 = max(유입, 처리) / 처리 능력. `queue_drop` 은 버리기 시작한 tick 에만(Value = 그 tick 에 버린 수). 표본은 줄이 있으면 무작위 대기 대신 줄 대기를 쓴다.
- 내 판단: 대상이 없는 장애는 무시한다(사건도 없음). `fault_start` Value = Count 또는 AddMs, `latency` 는 노드나 연결, `az_off`·`region_off` 는 그 상자와 하위 상자 안 모든 노드. client 를 `node_off` 하면 그 client 의 트래픽이 0.
- 내 판단: 장애 조치 — primary 가 꺼지면 `failover_start`(Value = FailoverS), 대상이 없으면 `writes_failing`. FailoverS 뒤 대상이 살아 있으면 primary 를 대상으로 넘기는 통로로 영구히 바꾸고(primary 가 돌아와도 되돌리지 않음) `failover_done`(Target = 대상). 그때 대상이 꺼져 있으면 `writes_failing` 하고 회복 없음. 기한 전에 primary 가 살아나면 취소. 대상은 db_replica·db_primary 만.
- 내 판단: LB 는 꺼진 대상을 꺼진 지 `health_interval_s` 뒤에 빼고(그 몫을 남은 대상에 비례해 나눔) `lb_reroute`(Target = LB, Value = 남은 대상 수). 대상이 살아나면 바로 다시 넣는다(사건 없음 — 스펙은 빼는 쪽만 말함). LB 가 아닌 노드는 꺼진 대상으로 계속 보낸다.
- 내 판단: 자동 확장 — 1초마다(앞 tick 의 수요 기준 CPU 사용률) 목표 초과이고 활성 + 주문 < Max 면 1대 주문, `scale_out` 은 **주문 때**(Value = 주문 뒤 목표 대수), 투입은 WarmupS 뒤. 축소는 주문이 없고 마지막 투입·제거 뒤 CooldownS 가 지났고 한 대 줄여도 목표 아래일 때 1대씩 `scale_in`(Value = 남은 대수). 시작 대수 = `instances` 를 Min..Max 로.
- 내 판단: `Summary` — RPS·노드 InRPS·인스턴스·비용은 실행 평균, UtilPermille·Limit·Bottleneck 은 최고점, Overloaded·Down 은 한 번이라도, 노드 p50/p99 는 요약 표본이 그 노드에서 쓴 시간, 연결은 평균 RPS·최고 사용률. 요약 표본 20,000 개는 프레임마다 나눠 그 프레임 상태로 뽑는다(시드 `split(3).split(프레임)`), 프레임 표본은 `split(2).split(프레임)`.
- 내 판단: 노드 300·600초 실행이 0.7초, heap ~18MB, `json.Marshal(Timeline)` 11MB — 계획의 "수 MB 안"보다 크다. 대부분 인터페이스의 `Frame.Nodes map[string]FrameNode`(프레임마다 300 항목 맵)라 인터페이스를 바꾸지 않고 그대로 뒀다. UI(Task 19)가 필요하면 프레임 간격을 늘려 줄일 수 있다.
- 내 판단: `erlangC` 에 B 가 0 으로 내려가면 멈추는 조기 종료와 노드별 `waitProb` 기억을 더했다(결과는 비트까지 같음). 유효 범위 안의 코어 448 × 인스턴스 1000 설계에서 60초 실행이 1.5초 → 0.01초.

**2묶음 (Task 7~11 — 엔진·고정 묶음·내보내기) / 내 판단 — Task 9 (고정 묶음)**

- 내 판단: 20개 설계는 `Decode` 의 부품 기본값에 기대는 짧은 JSON 이고 모두 `Validate` 를 통과한다(시험이 확인). 20번(노드 300·연결 1,000·장애 1)은 결정적 LCG 로 만든 파일이며 생성 스크립트는 저장소에 넣지 않았다(파일 자체가 원본).
- 내 판단: "건강한 구조" 주제(2~10, 14~16, 19)는 기본 처리 능력에서 과부하가 나지 않게 인스턴스 수를 맞췄고, 11(과부하)·12(재시도 폭주)·13(오름 곡선의 시간 실행)·18(연결 대역폭)·20 은 일부러 과부하다. 생성된 숫자를 훑어 p99 ≥ p50, 사용률·비용이 상식 범위임을 확인했다(17·18 의 월 $50만·$11만은 1MB·512KB 응답 × 수천 RPS 의 인터넷 전송비라 맞음).
- 내 판단: 골든 경로는 `testdata/TestCorpusGolden/<파일 이름, 확장자 없이>.golden`. 시험에서 골든 패키지를 `xgolden` 으로 들였다 — archsim 의 상수 `golden`(SplitMix 증분)과 이름이 겹친다.
- 내 판단: `GOARCH=amd64`(Rosetta)로 `TestCorpusGolden` 이 같은 골든에 통과함을 확인했다(arm64 와 amd64 가 바이트까지 같음). linux/amd64 실제 일치는 CI 가 본다.

**2묶음 (Task 7~11 — 엔진·고정 묶음·내보내기) / 내 판단 — Task 10 (Mermaid·D2)**

- 내 판단: 라벨 정리 = 줄바꿈·탭·U+2028/9 → 공백, `textwidth.Sanitize`, 앞뒤 공백 제거, 비면 부품 종류(모르는 종류면 `node`, 상자는 종류 또는 `box`). Mermaid 따옴표 안 이스케이프는 계획의 `"`·`<`·`>`·`#` 에 `%`(`#37;`)·`&`(`#38;`)·`` ` ``(`#96;`)를 더했다 — Mermaid 의 `%%{…}%%` 지시문 검출은 따옴표 안도 보고, 백틱 라벨은 마크다운 문자열이 되기 때문.
- 내 판단: 연결에는 사용자 라벨 필드가 없어 "프로토콜 · 지연"(지연 −1 이면 프로토콜만, 0.1ms 반올림 — `HTTP/2 · 2ms`, `TCP · 0.2ms`)을 라벨로 쓴다. 알 수 없는 프로토콜(검증 안 된 사본)은 정리·이스케이프해 보인다 — 적대적 라벨 시험은 노드·상자 라벨과 이 프로토콜 자리에 넣는다. async 는 Mermaid `-.->`, D2 `{style.stroke-dash: 3}`.
- 내 판단: 내보내기의 관대한 트리 — 노드·상자·연결을 ID 순 정렬 뒤 첫 것만, 노드와 ID 가 같은 상자는 버림(노드가 ID 를 가짐, `FromObjects` 와 같은 우선순위), 상자 부모 순환은 가장 작은 ID 의 상자를 최상위로, 없는 부모는 최상위, 끝이 없는 연결은 생략. `safeIDs` 는 노드 `n1…`·상자 `b1…`.
- 내 판단: D2 머리 주석의 `$` 도 `\$` 로 바꿔 출력 어디에도 이스케이프 안 된 `$` 가 없다. D2 모양 — db = cylinder, queue = queue, client = person, cdn·external = cloud, object_storage = stored_data, 그 밖 기본 사각형. Mermaid 머리 주석은 `{` 앞에 공백을 넣는다(계획대로).
- 내 판단: 파일 목록에 없던 시험 자료 `internal/archexport/testdata/designs/{three_tier,autoscale,mixed_providers}.json` 을 더했다(한글 라벨 3계층 다중 AZ, 자동 확장 2..8, GCP·Azure·custom 섞임). 이 기계에 d2·mmdc 가 없어 생성물을 실제 도구로 그려 보지는 못했다.

**2묶음 (Task 7~11 — 엔진·고정 묶음·내보내기) / 내 판단 — Task 11 (Terraform)**

- 내 판단(**계획 인터페이스와 다름**): `tfIdent(label, fallback string, used map[string]int)` — 계획은 `tfIdent(label, used)` 이지만 한글만인 라벨처럼 비게 되는 경우 "부품 종류 이름"으로 대체하려면 종류가 필요하다. 이름은 48바이트에서 자르고, `used` 는 모든 리소스 공통이다. 한 노드의 짝 리소스(launch template + ASG, LB + 대상 그룹 + listener, VPC + 보안 그룹)는 같은 이름을 쓴다(타입이 달라 주소가 겹치지 않음).
- 내 판단: `hclString` 은 따옴표까지 포함한 리터럴을 돌려준다. `$`·`%` 가 여러 개 이어진 뒤 `{` 가 오는 경우를 `terraform console` 로 확인했다(HCL 은 왼쪽부터 읽어 `"a$$${"` 에서 문자열이 끝나고 `"%%%{ if }"` 는 지시문이 아님) — `${`→`$${`·`%{`→`%%{` 치환이 사용자 글을 정확히 되살리며 보간을 만들지 않는다. 시험의 `interpolates` 를 그 규칙대로 썼다.
- 내 판단: `variable "region"` 의 기본값 = ID 순으로 첫 리전 상자 중 `pricing.RegionOf(pricing.AWS, code)` 가 아는 AWS 리전. 없으면 기본값 없음. 변수는 `ami_id`(API·worker 가 있을 때), `db_username`(primary 가 있을 때 — 비밀번호는 `manage_master_user_password = true` 로 AWS 가 관리해 비밀번호 변수가 없음), CDN 앞에 AWS LB 가 없으면 `<이름>_origin_domain`. 모두 기본값 없음. `account_id` 는 쓰는 곳이 없어 만들지 않았다.
- 내 판단: 서브넷 CIDR = VPC 상자 i(ID 순)에 `10.i.0.0/16`, 그 안 AZ j 에 `10.i.j.0/24`, AZ 이름은 `data.aws_availability_zones.available.names[j]`. VPC 밖 AZ 는 TODO 주석. 노드 배치 = 그 AZ 의 서브넷, 아니면 그 VPC 의 모든 서브넷, VPC 밖이면 빈 목록(+ ASG·LB 에 TODO).
- 내 판단: 리소스 세부 — ALB 에 listener(80 → 대상 그룹)를 더함, ASG 는 앞에 있는 AWS LB 의 대상 그룹 ARN, 자동 확장이면 min/max = Min/Max·desired = `instances` 를 그 범위로, 아니면 셋 다 `instances`. ElastiCache = redis 노드 1, RDS = postgres·`allocated_storage` = max(디스크 GB, 20)·`multi_az` = 인스턴스 > 1, replica 의 원본 = 그 replica 로 장애 조치하는 primary → 없으면 첫 AWS primary → 없으면 TODO. S3 = `bucket_prefix`(전역 이름 충돌 피함), OpenSearch `domain_name` 28자, CloudFront = 최소 필수 블록(`forwarded_values`, 기본 인증서), 원본 = CDN 이 보내는 첫 AWS LB 의 `dns_name`. AWS 식별자(`identifier`·`cluster_id` 등)는 이름의 `_` 를 `-` 로.
- 내 판단: 사업자 gcp·azure 노드 → `# TODO: non-AWS provider (<kind> "<라벨>")`(라벨은 `hclString` 꼴이라 따옴표·`${` 가 이스케이프됨). custom·빈 값·그 밖은 AWS 뼈대로 만든다(custom 은 "가격을 직접 입력"이라는 뜻일 뿐). client·external 은 리소스를 만들지 않는다.
- 내 판단: 이 기계에 terraform 이 있어 `TestTerraformFmt` 가 건너뛰지 않고 돈다 — 골든 3개와 적대적 라벨 26개의 출력 전부가 `terraform fmt -check` 를 통과(문법 해석 포함). `terraform validate` 는 provider 내려받기(네트워크)가 필요해 하지 않았다. `hclWriter` 는 연속된 한 줄 속성의 `=` 를 맞추고 블록·객체 속성·주석 앞에 빈 줄을 둬 fmt 결과와 같게 쓴다.

**2묶음 (Task 7~11 — 엔진·고정 묶음·내보내기) / 리뷰 반영 (2묶음 리뷰 5건)**

- 내 판단: `erlangC` 는 창구 1,024 개(`erlangCExactMax`) 이하에서 지금 점화식 그대로(비트까지 같음 — 고정 묶음의 창구는 수십 개라 골든 불변), 그보다 많으면 같은 Halfin–Whitt β = (c − a)/√a 를 갖는 창구 1,024 개 줄로 옮겨 계산한다(√a' = (√(β² + 4·1024) − β)/2). 정확값과의 차는 c 1,025..448,000·ρ 0.1..0.9999 에서 0.004 이하이고 조금 높게(비관적으로) 나온다(`TestErlangCLargeServersClose` 는 0.01 로 고정). 스펙 C2.9 가 "Erlang C 계산 방식"을 빌더 재량으로 둔다. `math.Sqrt` 는 IEEE 정확 반올림이라 아키텍처마다 같다. 리뷰의 재현 설계(인스턴스 1000 × 코어 448 API 299개, 검증 통과)에서 Analyze 722ms → 약 7ms(load average 40), MonthlyCost 1.07s → 0.6ms, 60초 시간 실행 약 0.3초.
- 내 판단: `RunTimed` 는 ctx 를 100 tick 마다가 아니라 매 tick 본다(`cancelEvery` 없앰). 노드 루프 안까지는 넣지 않았다 — Erlang C 비용이 묶였으니 한 tick 은 노드 300 에서도 1ms 안팎. 회귀 시험 `TestAnalyzeHugeServerCountFast`(Analyze·MonthlyCost 50ms 이하, 3회 중 최소)·`TestTimedCancelPromptHugeDesign`(취소 뒤 100ms 안에 `context.Canceled`).
- 내 판단: 즉시 분석 5ms 예산 — 출력을 바꾸지 않는 세 가지를 했다. (1) 전 구간·프레임·노드별 p50/p99 를 전체 정렬 대신 quickselect(세 갈래 분할, 중앙값 셋 피벗, 64회 넘으면 정렬로 물러남, NaN 이 있으면 정렬)로 고른다 — 같은 순서 통계량이라 값이 같다(`TestP50P99MatchesSort`). (2) 대기 표본의 난수는 늘 뽑되 로그(`detLog`)는 실제로 기다릴 때만 계산(`expOf`) — 흐름·값 동일. (3) `archdoc.newGraph` 맵 크기를 미리 잡음. 그래프를 `Check` 와 나눠 쓰는 길은 택하지 않았다 — archdoc 공개 겉면이 늘고 이득이 작음. `Samples` 는 그대로(골든).
- 내 판단(**예산 미확인 — 출시 관문으로 남김**): 이 세션 내내 load average 31~44 라 조용한 기계에서 잴 수 없었다. 같은 부하에서 번갈아 잰 `BenchmarkAnalyze300` 15회 중 최소 3개: 이전 5.83·6.23·6.62ms → 지금 4.51·4.58·4.67ms. `MASIL_PERF=1 go test -run TestAnalyzeBudget` 은 6회 중 2회 통과(실패 때 중앙값 5.5~6.5ms). 시험은 그대로 두며, 조용한 M1 에서 통과를 보기 전에는 Task 7 의 5ms 수용 조건을 다 채웠다고 보지 않는다.
- 내 판단: 시간 실행 메모리(Task 8) — 인터페이스(`Frame.Nodes map[string]FrameNode`)는 바꾸지 않고 계획 Task 8 Step 3 의 "수 MB 안"을 실측값으로 고쳤다(600초 × 노드 300·10 tick 프레임: Timeline 약 17MB, JSON 약 12MB; `FrameEveryTicks` 50 이면 JSON 약 2.6MB). Task 19 가 긴·큰 실행에 프레임 간격을 키워 넘긴다고 계획 줄에 적었다.
- 내 판단: `tfIdent` 3인자 서명을 유지하고 계획 Task 11 Produces 줄을 코드에 맞게 고쳤다(날짜 표시). 2인자로는 "비면 부품 종류 이름" 규칙을 지킬 수 없다.
- 내 판단: Terraform 의 AWS 식별자(RDS `identifier` 63·ElastiCache `cluster_id` 40·OpenSearch `domain_name` 28·SQS `name` 80)는 서비스별 이름 공간(rds·elasticache·opensearch·sqs — primary 와 replica 는 같은 rds)마다 고유하게 만든다: 자른 값이 이미 쓰였으면 끝을 줄이고 `-2`, `-3`… 을 한도 안에 붙인다. CloudFront `origin_id` 는 배포 하나 안에서만 쓰여 그대로, S3 는 `bucket_prefix` 라 AWS 가 뒤를 붙인다. 골든 3개 불변, 시험 `TestTerraformAWSIdentifiersUnique`(같은 라벨 search 3·cache 3·DB 2).

**3묶음 (Task 12~17 — 캔버스 그리기·편집) / 내 판단 — Task 12 (archdraw 격자 그리기)**

- 내 판단: 확대 단계는 좌표와 크기를 함께 줄인다(그래야 "작아져 겹치면 확대를 높이세요"가 생긴다). 상세 = 세계 1:1·노드 12×4(이름×n / 막대 / p99 / 경고 줄), 보통 = 가로·세로 3/4·노드 10×3, 점 = 가로 2/3·세로 1/3·노드 10×1 `[이름 81%]`. 자동 정리 간격(16×6)은 세 단계 모두에서 겹치지 않는다. 계획 시험 주석의 "14칸 떨어진 두 노드"는 점 단계에서 9칸 간격이 되어 겹친다고 본다(`TestOverlapsAtDot` — 16칸은 안 겹침).
- 내 판단: 점 단계 이름은 8칸 안에 사용률과 함께 들어가야 해 `[결…  81%]` 처럼 많이 잘린다(스펙 예 `[api×4 81%]` 11칸보다 좁음 — 그 폭이면 정리한 설계가 점 단계에서 겹침).
- 내 판단: 상자 테두리는 lipgloss RoundedBorder 글자(╭╮╰╯), 노드는 보통 테두리(┌┐└┘)로 둘을 구별한다. 줄인 단계에서 안쪽 상자가 부모 테두리와 같은 줄·칸에 겹치지 않게 부모 안쪽으로 한 칸 당겨 그린다(적중 검사도 같은 사각형).
- 내 판단: `＋－` 확대 단추는 전각이라 두 단추가 뷰포트 오른쪽 위 4칸을 쓴다. 단추는 `Overlay.Buttons` 가 켜졌을 때만 그린다(스냅숏·카드 그림에는 없음) — 계획 `Overlay` 에 없던 필드를 더했다. `HitTest` 는 늘 그 4칸을 단추로 본다.
- 내 판단: 유령 상자는 점선(╌╎) 테두리, 끌던 원래 노드는 `StyleGhost`(흐리게)로 그린다. `Ghost.ID` 가 상자면 상자 윤곽을 점선으로 옮겨 그린다.
- 내 판단: `HitComment` 는 상수만 둔다 — `HitTest` 는 댓글 수를 모르므로 댓글 표시(`✎n`, 노드 아래 테두리 오른쪽) 적중은 댓글 패널(Task 25)이 정한다.
- 내 판단: 계획 밖 겉면을 더했다 — `Zooms()`, `(Zoom).In/Out`, `(*Grid).At`, `(*Scene).NodeIDs/Bounds/NodeAt/BoxAt/BoxAtRect`, `FormatMs`, `EdgeLabel`.
- 내 판단: TDD 순서를 일부 어겼다 — `scene.go`·`render.go`·`hit.go` 를 시험보다 먼저 썼다. 시험을 쓴 뒤 구현 파일을 scratchpad 로 옮겨 `undefined: Build` 실패를 확인하고 되돌려 통과를 확인했다(P0 기록과 같은 방식). Task 13 부터는 시험을 먼저 돌려 실패를 보았다.

**3묶음 (Task 12~17 — 캔버스 그리기·편집) / 내 판단 — Task 13 (직각 선 경로)**

- 내 판단: `Route` 가 돌려주는 점은 "세계 좌표"가 아니라 그 확대 단계의 칸 좌표(세계 × 축척, 상세에서는 세계와 같음)다. 확대마다 노드 크기가 달라 경로를 단계별 칸에서 찾아야 하기 때문이다(`Point` 주석에 적음).
- 내 판단: 경로 = 두 끝 둘레(여백 6, 막히면 24) 안의 A*(한 칸 1, 꺾임 4, 남은 꺾임을 더한 휴리스틱, 동점은 f·깊이·y·x·방향 순). 나가는 쪽 = 대상이 완전히 오른쪽이면 오른쪽(◆ 쪽), 완전히 왼쪽이면 왼쪽, 아니면 아래·위. 화살표는 늘 들어가는 쪽을 가리킨다. 한 번에 1,500칸 넘게 펼쳐도 못 찾으면 단순한 ㄱ자 선으로 둔다(노드 밑으로 지나가 노드에 가려질 수 있음 — 빽빽한 설계에서만).
- 내 판단: 선은 칸마다 이어진 방향을 모아 글자를 정한다(━┃┏┓┗┛, 세 갈래 ┳┻┣┫, 네 갈래 ┼) — 그래서 그리는 순서와 무관하고 교차는 `┼`. 선택한 연결은 `StyleEdgeSelected`. 라벨은 상세에서 모두, 그 밖에서는 선택한 연결만, 가장 긴 가로 구간 가운데에(라벨 폭 + 4 이상일 때).
- 내 판단: 성능 — 노드 300·연결 1,000 빽빽한 격자(벤치 설계), 200×60: 캐시된 한 화면 중앙값은 8ms 안(`MASIL_PERF=1 TestRenderBudget` 통과), `BenchmarkRender300x1000 -benchtime 20x` = 8.7ms/op(첫 장의 경로 계산 포함 평균). 장면을 새로 만든 첫 장은 상세 약 90ms·보통 약 55ms·점 약 280ms(경로 찾기 — 편집 한 번마다 한 번). 실제 설계(three_tier)는 첫 장 1ms 안팎. 같은 입력 = 같은 경로를 지키려고 이전 장면의 경로를 다시 쓰지 않았다.

**3묶음 (Task 12~17 — 캔버스 그리기·편집) / 내 판단 — Task 14 (글자 스냅숏·카드 미니 그림)**

- 내 판단: 분석 결과 → 노드 표시 변환을 `archexport.Badges(d, r) map[string]archdraw.NodeBadge` 로 내보냈다(UI 캔버스도 같은 것을 씀): `×n` = 평균 대수(반올림)가 2 이상일 때, ✕ = 과부하·꺼짐, ⚠ = 사용률 80% 이상·병목·설계 점검 대상.
- 내 판단: 스냅숏 = 제목 / 빈 줄 / 그림(폭에 들고 겹침 없는 가장 자세한 확대, 없으면 점 단계를 잘라 마지막 줄 `…`, 그림 최대 60줄) / 빈 줄 / 표(ID 순, 숫자는 오른쪽 맞춤, 이름 칸만 줄여 폭에 맞춤) / 빈 줄 / `p99 <n> · ~$<합계><CostNote>`. 줄 끝 공백은 지운다. 폭은 40..200 으로 묶는다(계획은 80·120). `r == nil` 이면 숫자 칸은 `-` 이고 합계 줄이 없다.
- 내 판단: 표의 "대수" 칸은 설계의 인스턴스 수(자동 확장 평균이 아님), 비용 칸은 `NodeResult.CostUSDMonth`. 천 단위 쉼표는 Task 15 의 `humanize.Grouped` 보다 먼저라 `archexport` 안에 작은 `grouped` 를 따로 두었다(내보내기 패키지가 humanize 를 끌어오지 않게).
- 내 판단: 카드 미니 그림은 보통 → 점 순서로 60×8 에 맞는 단계를 고르고, 둘 다 안 되면 점 단계를 60×7 로 잘라 마지막 줄을 `…` 로 둔다. 노드가 없으면 빈 목록.

**3묶음 (Task 12~17 — 캔버스 그리기·편집) / 내 판단 — Task 15 (도구 등록·모드별 배치·읽기 화면)**

- 내 판단: 인스턴스 id = OpenTool 키. `arch:file:<이름>`(같은 파일은 한 인스턴스), `arch:new:<ULID>`, `arch:doc:`·`arch:msg:` 는 Task 22·26 용으로 접두사만 둔다(지금은 빈 읽기 전용 사본으로 열림). 허브 enter = 앞에 연 arch 인스턴스(없으면 첫 인스턴스)를 다시 보이고, 없으면 새 설계.
- 내 판단: `arch.new`·`arch.open` 은 플래그만 켜지면 보이고, `arch.share/export/versions/send_card/run` 은 앞에 연 arch 인스턴스가 있을 때만 보인다(실행은 `commandMsg` 로 그 인스턴스에 — 내용은 뒤 Task 들). `arch.open` 은 열기 대화상자(Task 20)가 오기 전까지 `dialog=open` 질의를 단 새 설계 인스턴스를 연다. 그룹은 모두 `GroupCommand`.
- 내 판단: 배치 — 머리 1줄, 지표 3줄(컴팩트·위젯·80칸 미만·24줄 미만이면 1줄, `!` 로 점검 줄을 펼치면 2줄). 110칸 이상이면 팔레트 16칸·패널 28칸을 붙박이로, 지휘소 모드 110칸 미만은 팔레트만 서랍·패널은 붙박이, 그 밖은 둘 다 오른쪽 서랍(열면 캔버스가 줄어듦). 80칸 미만은 기본이 목록 보기(`v` 로 바꿈), 43칸 이하·15줄 이하·초소형은 4줄 요약.
- 내 판단: 확대 단계를 사용자가 고르기 전에는 상세, 위젯 모드이거나 캔버스가 80칸 미만이면 보통으로 시작한다. 처음 열 때 설계의 왼쪽 위 모서리를 캔버스 왼쪽 위(2칸·1줄 여유)에 맞춘다.
- 내 판단: 파일이 없으면 그 이름으로 새 설계를 시작하고(첫 편집 1초 뒤 저장), 읽기·검증에 실패하면 빈 읽기 전용(roBroken)으로 연다 — 원래 파일은 건드리지 않는다.
- 내 판단: 비용 줄은 지표 둘째 줄 맨 앞에 두어 어느 크기에서도 잘리지 않게 했다(수용 기준 8). 오른쪽 패널에서는 줄바꿈한다. 천 단위 쉼표는 `humanize.Grouped`.
- 내 판단: 화면 시험은 `internal/ui/tools` 를 blank import 한다 — RouteTool 팩토리가 그 패키지 `init()` 에 있어서, 없으면 앱이 "준비 중" 자리표시를 그린다.

**3묶음 (Task 12~17 — 캔버스 그리기·편집) / 내 판단 — Task 16 (키보드 편집)**

- 내 판단: 모든 편집은 "설계 사본을 고친 뒤 `DiffObjects(전, 후)`" 로 바꿈 목록을 만들고 `commit`(sink.Apply → UndoLog.Record → 재분석) 한 길로 보낸다. 되돌리기·다시하기는 `UndoLog.Undo/Redo` 결과를 기록 없이 sink 에 적용한다(남이 바꾼 칸은 UndoLog 가 건너뜀). 읽기 전용(카드 사본·깨진 파일)은 편집 키에 `read_only_copy` 토스트.
- 내 판단: 계획의 다섯 모드에 셋을 더했다 — `modePick`(a 부품·g 상자 종류를 팔레트 커서로 고름, j/k·enter·esc), `modeHelp`(? 도움말), `modePanel`(e — 설정 패널의 실제 편집은 Task 18). 팔레트가 서랍인 배치에서는 고르는 동안만 서랍을 연다.
- 내 판단: 이동은 2×1 격자(H/L 2칸, J/K 1줄), `m` 은 유령(점선)을 움직이고 enter 에 한 번에 확정. 노드를 옮기면 놓인 자리를 통째로 품는 가장 안쪽 상자가 새 부모가 된다(마우스 놓기와 같은 규칙). 상자를 옮기면 안의 상자·노드가 함께 움직이고 부모는 그대로. 상자 유령은 테두리만 그리게 archdraw 를 고쳤다(안의 노드가 가려지지 않게).
- 내 판단: 새 부품은 선택한 노드 오른쪽(간격 4칸)부터 아래·왼쪽·위·바깥 고리 순서로 다른 노드와 4×2 간격이 나는 첫 빈자리에 둔다(선택이 없으면 캔버스 왼쪽 위). 라벨은 그때 언어의 종류 이름(`API 서버`), 새 상자는 기본 크기로 설계 오른쪽에. `g` 의 상자는 노드들을 여백 2·위 2·아래 1 로 감싸고, 노드들이 같은 상자에 있고 중첩 규칙(AZ⊂VPC⊂region)이 맞으면 그 상자 안에, 아니면 맨 위에 만든다. 감싼 뒤 새 상자를 선택한다.
- 내 판단: `v` — 목록 보기에서나 노드가 선택되지 않았을 때는 캔버스↔목록 보기 전환(Task 15 의 `v`·Task 17 시험의 "v canvas"), 노드가 선택돼 있으면 여러 개 선택(hjkl 로 닿는 노드를 더하고 space 로 넣고 빼기, esc·v 로 끝). 여러 개 선택에서 m·H J K L·d·g 는 선택 전체에 적용.
- 내 판단: `d` 는 노드와 그 연결·그 노드를 겨눈 장애를 한 단계로 지우고(`failover_to` 도 비움), 상자를 지우면 안의 것은 상자의 부모로 올린다(같이 지우지 않음). 키보드로 연결을 고르는 길은 Task 18 패널에 둔다(지금은 마우스·찾기로 노드·상자만).
- 내 판단: `/` 찾기는 라벨·종류 ID·종류 이름에 대소문자 무시 부분 일치, 노드(ID 순) 다음 상자, 찾으면 가운데로 화면을 옮긴다. 입력 중에는 `CapturesInput` 이 참이고 진짜 커서를 프롬프트 끝에 둔다. 못 찾으면 `find_none` 토스트.
- 내 판단: `space`·`<`·`>`·`x` 는 키만 잡아 둔다(시간 실행·배속·장애 메뉴는 Task 19). 배속 단계는 1·2·4·8·16.
- 내 판단: 캔버스는 어느 모드에서도 ctrl+k·ctrl+t·ctrl+c·tab·shift+tab·[·]·ctrl+w 를 받지 않고, 보통 모드의 esc·enter 도 넘긴다(전역 뒤로·열기). 도구 표면에서 tab 은 앱 규칙대로 포커스를 표면 밖으로 옮긴다.
- 내 판단: 상태 줄 힌트(Help)가 생기면서 Task 15 의 ui/arch 골든 6개의 맨 아래 줄이 바뀌어 `-args -update` 로 다시 만들었다(이 패키지 골든만, 상태 줄 한 줄씩).
- 내 판단: 키 안내 문구용 카탈로그 키를 더했다 — `help_title`, `key_*`(24개), `connect_exists`, `find_none`.

**3묶음 (Task 12~17 — 캔버스 그리기·편집) / 내 판단 — Task 17 (마우스 끌어다 놓기)**

- 내 판단: 캔버스가 `ui.DragHandler` 를 구현한다 — 왼쪽 누름은 `HandleMouse`, 그 뒤 움직임·놓기는 앱이 `HandleDrag` 로 보낸다. 끄는 동안 바꾸는 것은 화면뿐이고 놓을 때 바꿈 한 묶음(`commit` 한 번 = 되돌리기 한 단계)을 보낸다.
- 내 판단: 누른 곳이 노드 몸통이면 옮기기, `◆` 면 연결, 상자 테두리·라벨이면 상자째 옮기기, 상자 안 빈 곳·연결선·빈 캔버스면 화면 이동(상자 안·연결선은 선택도 함). 옮긴 거리는 2×1 격자에 맞추고, 노드를 놓으면 그 자리를 통째로 품는 가장 안쪽 상자가 부모가 된다(끄는 동안 그 상자 테두리를 선택 색으로 강조 — `Ghost.IntoBox`). 상자를 끌면 안의 상자·노드가 함께 움직인다.
- 내 판단: 팔레트 항목을 끌어 캔버스에 놓으면 놓은 칸(격자에 맞춤)이 새 노드·상자의 왼쪽 위가 된다. 끄는 동안 포인터 자리에 점선 윤곽과 이름을 그린다. 팔레트 항목을 끌지 않고 누르기만 하면 키보드 `a` 와 같이 선택 옆 빈자리에 더한다.
- 내 판단: 캔버스가 60칸보다 좁으면 편집 끌기(옮기기·연결·팔레트)는 첫 움직임에 `narrow_no_drag` 토스트만 내고 아무것도 바꾸지 않는다 — 화면 이동 끌기·휠·클릭 선택·＋－ 는 된다. 읽기 전용 설계에서도 편집 끌기는 `read_only_copy` 토스트로 막는다.
- 내 판단: 제스처 취소 = Resize, `ui.ModeChangedMsg`, 이 인스턴스가 아닌 곳으로의 `ui.FocusChangedMsg`, 앱의 `ClearDrag`, 끌던 노드·상자가 설계에서 사라짐(재구성 때 확인). 취소되면 유령을 지우고 놓아도 아무것도 보내지 않는다.
- 내 판단: 휠은 위아래 3줄씩 화면 이동(목록 보기에서는 선택 줄 이동). 확대는 ＋－ 단추와 키로만.
- 내 판단: 오른쪽 클릭 메뉴는 캔버스 안에 그리는 작은 상자(설정·장애·삭제·댓글, j/k·enter·esc, 항목 클릭, 바깥 클릭은 닫기만)다. "설정"은 `e` 와 같은 패널 모드, "삭제"는 `d` 와 같은 한 단계 삭제, "장애"·"댓글"은 Task 19·25 가 채울 때까지 메뉴만 닫는다. 메뉴 문구용 카탈로그 키 `menu_settings/fault/delete/comment` 를 더했다.
- 내 판단: 마우스·키보드 동등 시험(수용 기준 2)은 같은 120×40 에서 마우스로 만든 설계와, 그 결과 위치로 `m`+방향키로 옮기는 키 스크립트로 만든 설계를 ID·시드만 이름으로 바꿔 JSON 으로 통째 비교한다. 120×40 표준 배치의 캔버스는 74칸이라 자동 확대가 "보통"이므로, 마우스 시험은 `+` 로 상세 단계에 놓고 화면 칸 = 세계 칸으로 잰다.

**3묶음 (Task 12~17 — 캔버스 그리기·편집) / 내 판단 — 리뷰 반영 (Task 12~17, 2026-11-10)**

- 내 판단: C2.6 의 폭(80×24 위젯, 44~79칸 목록, 43칸 이하 요약, 60칸 미만 끌기 막음)은 터미널 칸이다. `computeRegions` 가 받는 것은 테두리 안쪽 칸이라 2칸 좁으므로 `listBelow`·`tinyMaxW`·`dragMinW` 를 `paneBorder`(2) 만큼 줄였다(78·41·58). 80×24 는 이제 캔버스(보통 확대)를 보이고, `TestArchScreens/80x24.golden` 하나만 `-args -update` 로 다시 만들었다(목록 → 캔버스). 높이 기준(15줄)은 그대로.
- 내 판단: 위젯 모드의 `tab` 하위 보기 전환(캔버스 ↔ 목록 ↔ 지표)은 넣지 않는다. 계획 Global Constraints·`TestCanvasLeavesGlobalsAlone` 가 "캔버스는 `tab` 을 가로채지 않는다"고 정하고 있어 그쪽을 따랐다. 캔버스 ↔ 목록은 `v`, 지표는 어느 크기에서도 아래 줄에 보이고 `!` 로 점검 줄을 펼친다. 계획 Task 15 규칙 줄을 이에 맞춰 고쳤다.
- 내 판단: `p` 는 설정 패널 서랍을 열고 닫는다(`e` 는 열기만). 도움말 줄은 `e p`.
- 내 판단: 편집 뒤 첫 화면 시간(C2.7 8ms)을 지키려고 (1) `archdraw.BuildFrom(prev, d)` — 두 끝 사각형이 같고 탐색이 살핀 칸(비트맵)에 걸친 장애물이 바뀌지 않은 경로는 그대로 이어 쓴다(같은 입력 = 같은 경로라 결과는 `Build` 와 같음), (2) 도착 직전 칸이 막혀 있으면 탐색 없이 곧바로 실패(결과는 전과 같음 — 점 확대의 촘촘한 설계에서 대부분이 여기 걸림), (3) `Overlay.RouteLimit`(캔버스는 `FrameRouteLimit` = 16): 한 프레임에 새로 찾는 경로를 16개로 묶고, 끝이 움직이지 않았는데 근처가 바뀐 경로는 그동안 앞 장면의 선을 그린 뒤 30ms 마다 `Settle` 이 16개씩 마저 찾는다(편집 직후 몇 프레임만 옛 선이 보일 수 있음, 끝이 움직인 연결은 늘 바로 다시 찾음). 측정(Apple M1 Pro, `MASIL_PERF=1`, 노드 300·연결 1,000·200×60): 노드 하나 옮긴 뒤 첫 화면 중앙값 상세 6.3ms·보통 4.4ms·점 2.3ms, `Settle` 한 단계 최대 7.7ms(전에는 각각 약 120·110·290ms).
- 내 판단: 화면 밖으로 길게 뻗은 연결·큰 상자는 화면(±1칸) 안의 칸만 그리고 맞춰 본다(전에는 경로 전체 칸을 펼쳐 한 프레임에 수 GB 할당).
- 내 판단: 개인 설계 캔버스를 닫으면(`ui.Closer`) 아직 안 쓴 편집을 그 자리에서 동기 저장한다(실패하면 `save_failed` 토스트). **masil 을 끄는 길(ctrl+c 두 번·종료)은 고치지 못했다** — 끌 때 도구 인스턴스에 알리는 고리가 `internal/ui/app/*`(P0 소유)에 없어서다. 편집 뒤 1초 안에 끄면 그 편집은 여전히 잃는다. P0 에 "끄기 전 정리" 등록부(예: `ui.RegisterQuitFlush`)가 생기면 같은 `flush` 를 걸면 된다 — 오너 결정 대기.
- 내 판단: 노드 300·연결 1,000·상자 100 한도에 닿은 설계에 더하는 편집(추가·놓기·연결·감싸기·그 밖의 모든 `commit`)은 거절하고 `limit_reached` 토스트(ko·en 카탈로그 새 키)를 낸다. 읽어 올 때 ID 순으로 잘라 기존 객체가 사라지던 문제를 막는다. 개수만 세어 보므로 객체 복사는 없다.
- 내 판단: 계획 Task 13 Produces 의 `Route` 점열을 "그 확대의 칸 좌표(상세에서만 세계 좌표와 같음)"로 고치고 `BuildFrom`·`RouteLimit`·`Settle` 을 적었다.
- 내 판단: 마우스·키보드 동등 시험과 키보드 편집 흐름 시험은 선택을 `c.sel` 대입 대신 키(hjkl, 안 되면 `/` + 라벨 + enter)로 한다 — 키로 고를 수 없으면 시험이 실패한다.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 18 (설정 패널·결과·지표 줄)**

- 내 판단: 설정 칸은 archdoc 객체 필드 이름(`arch.md` 2.1)으로 읽고 쓴다 — 값 확정은 `ChangeSet` 묶음 하나를 `commit` 한 번으로 보내고, 보내기 전에 `FromObjects` 가 이슈를 내면(라벨 금지 문자·자동 확장 최소 > 최대 등) 보내지 않고 `invalid_value` 를 칸 아래에 보인다. 숫자 범위는 `arch.md` 1.4 와 같다(입력 단위: 메모리 GiB, 백분율 %, 직접 입력 가격 ¢).
- 내 판단: 입력 칸은 bubbles `textinput` 대신 찾기(`/`)와 같은 손수 만든 한 줄 입력이다. 캔버스는 칸 버퍼에 직접 그리므로 같은 방식이 맞고, 진짜 커서(`Cursorer`)는 입력 끝에 둔다.
- 내 판단: 가격표에 있는 종류에서 CPU·메모리·NIC 를 손으로 바꾸면 `instance_type` 만이 아니라 `provider` 도 `custom` 으로 바꾸고 그 종류의 시간당 가격을 `custom_hourly_micro_usd` 에 옮긴다. 엔진(Task 7)은 사업자가 `custom` 일 때만 그 값을 쓰므로, 사업자를 aws 로 두면 계산 비용이 0 이 되어 "추정"이 더 틀려진다. 대신 사업자 `custom` 노드는 디스크 가격이 빠진다(가격표 리전 없음).
- 내 판단: 노드 리전이 가격표에 없으면(상자 따름이고 리전 상자가 없음) 종류 목록·시간당 가격은 그 사업자 표의 첫 리전으로 보인다. 엔진의 기준 리전(us-east-1 등)은 내보내지 않은 값이라 고치지 않았다 — 두 값이 다를 수 있음.
- 내 판단: 패널은 `e` 로 열린 동안(modePanel)만 칸 목록 폼을 그리고, 그 밖에는 Task 15 의 읽기 줄을 그대로 둔다. 목록 보기에서는 `enter` 가 그 자리에 폼(stacked form)을 그리고(서랍을 열지 않음) 마지막 줄 "연결 대상 고르기" 로 연결한다. 폼에서 `d` 는 그 노드를 지우고 폼을 닫는다.
- 내 판단: 원격 삭제로 패널이 닫히는 안내는 `rebuild` 가 표시만 하고 `refresh(ctx) tea.Cmd`(원격 변경 경로, Task 22 가 씀)가 `node_deleted` 토스트로 바꾼다. 내 편집·되돌리기로 사라지면 조용히 닫는다. 포커스가 다른 칸으로 가면 입력 중인 값을 확정한다(범위 밖이면 버림).
- 내 판단: 비용 상위 3개는 지표 첫 줄에 `비용 상위 <라벨> <몫>%` 로 달러 없이 보인다 — "$" 마다 "추정"과 가격표 날짜가 붙어야 하는 규칙(수용 기준 8, `checkEstimate`)을 지키려고. 병목 줄은 `병목 ⚠ <라벨> · <한도> <n>% (<수요>/s × <단위>)` 로 바꿨고(`metric_bottleneck` 인자 2개 더함), 100% 이상이면 위험색이다. 사용률 줄은 10칸 막대 + 85% 경고색 + 100% 위험색·`⚠`.
- 내 판단: Task 18 이 지표 줄 모양을 바꾸므로 이 패키지 골든 `TestArchScreens/120x40`·`200x60` 두 개만 `-args -update` 로 다시 만들었다(지표 첫 두 줄만 바뀜). 다른 패키지 골든은 그대로다.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 19 (장애 메뉴·시간 실행 재생)**

- 내 판단: 장애 메뉴(`faultMenu`)는 앱 오버레이가 아니라 오른쪽 클릭 메뉴처럼 캔버스 안에 그리는 상자다(키는 열린 동안 메뉴가 모두 받음, 클릭은 닫기). 세 단계: 종류·대상 → (인스턴스 끄기는 대수 1..1,000, 지연은 +ms 1..600,000) → "지금" / "시간 실행의 mm:ss 에"(0..10:00, `30` 처럼 초만 적어도 됨). 고른 것이 노드면 노드 끄기·n대 끄기·지연·감싼 AZ 끄기·감싼 리전 끄기, 연결이면 지연(+ 양 끝이 아닌 연결 자체), 상자면 그 AZ·리전. 아무것도 안 골랐거나 넣을 것이 없으면 `fault_none` 토스트. 장애 50개 한도면 `limit_reached`.
- 내 판단: 재생은 실제 시간 0.1초마다 `playTickMsg` 하나, 배속 n 이면 틱마다 n/10 프레임(한 프레임 = 시뮬 1초)을 나아간다. `timedDoneMsg`·`playTickMsg` 에 인스턴스 ID 를 더했다(같은 앱에 캔버스가 여럿일 수 있음). 편집(내 commit·되돌리기·원격 변경)은 `runGen` 을 올리고 계산 중인 `RunTimed` 를 ctx 로 취소한다 — 늦게 온 옛 결과는 버린다.
- 내 판단: space 상태: 없음 → 계산 → 재생 ↔ 멈춤(끝에서 space 는 처음부터). 계산 중 space 는 계산을 멈춘다. 멈춘 동안 화살표 ←/→ 만 시간 막대를 1초씩 움직이고 h/l 은 여전히 선택이다(계획의 "시간 막대 포커스"를 따로 두지 않음). 시간 실행은 편집할 때까지 화면에 남는다.
- 내 판단: 재생 중에는 노드 막대·위험 표시(꺼짐 또는 100% 이상)·경고(85% 이상)와 지표 줄의 처리량·p50·p99·에러가 그 프레임 값이다. 비용·병목·점검은 즉시 분석 값 그대로이고, 지표 마지막 줄(1줄 배치는 꼬리)에 점검 대신 가장 최근 이벤트 `mm:ss <문구>`(같은 순간의 이벤트는 ` → ` 로 이음)를 보인다. 머리 줄 엔진 상태는 `시간 실행 01:23/05:00 ×4` · `멈춤` · `계산 중`.
- 내 판단: 시험 고정 설계의 시드를 42 로 박았다 — `archdoc.New` 가 ULID 로 시드를 뽑아 골든의 p99 가 실행마다 달랐다(엔진은 결정적).

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 20 (개인 설계 열기·저장과 내보내기 대화상자)**

- 내 판단: 명령 `arch.open` 은 새 빈 캔버스(`dialog=open`)를 띄우지 않고 파일 대화상자 오버레이만 연다. 설계를 고르면 `arch:file:<이름>` 인스턴스가 열리므로, 먼저 띄운 빈 "새 설계" 인스턴스가 남는 일을 없앤다. `dialog=open` 질의는 그대로 받아(다른 진입점 대비) Init 에서 같은 대화상자를 연다.
- 내 판단: "새로"는 이름을 먼저 받아 `arch:file:<이름>` 로 연다(첫 편집 때 저장). 이름 없는 새 설계는 기존 `arch.new` 가 맡는다.
- 내 판단: 열려 있는 설계(`arch:file:<이름>` 인스턴스가 있음)는 이름 바꾸기·지우기를 막고 "닫은 뒤 다시"를 안내한다. 열린 캔버스의 1초 뒤 저장이 옛 이름 파일을 되살리는 것을 막는 가장 작은 방법이다.
- 내 판단: 지우기 확인은 `widgets.Confirm` 을 대화상자 위에 하나 더 올리고, "지우기"의 Cmd 가 `filesDeletedMsg` 를 내면 목록을 다시 읽는다(앱이 오버레이에도 메시지를 돌린다). 덮어쓰기 확인도 같은 방식이다: "취소"면 확인 창만 닫히고 내보내기 대화상자는 남아 클립보드 등을 다시 고를 수 있다. `tea.Sequence` 는 금지라 쓰지 않는다.
- 내 판단: JSON 가져오기는 크기를 `Stat` 으로 먼저 보고(1MB 초과면 읽지 않음) `LimitReader(1MB+1)` 로 읽어 `archdoc.Decode` 한다. 실패 사유는 첫 이슈로 고른다(too_large·syntax·schema_version 은 전용 문구, 나머지는 "경로: 코드"). 저장 이름은 제목 → 파일 이름 → "arch" 순으로 파일에 쓸 수 있게 다듬고(금지 문자는 `-`), 이미 있으면 " (2)"… 를 붙여 **절대 덮어쓰지 않는다**. 성공하면 바로 연다.
- 내 판단: 내보내기 파일 이름의 `SafeName(제목)` 은 제목에 `/` 같은 금지 문자가 있으면 실패하므로 그 문자를 `-` 로 바꾼 뒤 `SafeName` 을 거친다(그래도 안 되면 "arch"). 글자 스냅숏 80·120 은 계획대로 둘 다 `.txt` 라 두 번째는 덮어쓰기 확인을 받는다.
- 내 판단: 내보낸 파일은 0644 로 쓴다(사용자가 다른 도구·저장소로 가져갈 산출물). 개인 설계 파일(0600)과 달리 밖으로 내보내려고 만든 파일이기 때문이다. 폴더는 0755 로 만든다.
- 내 판단: Mermaid·D2 에는 머리 주석을 넣지 않는다(계획이 머리를 Terraform 에만 정함). Terraform 머리 = `tf_header` + `estimate_disclaimer`. 글자 스냅숏의 표 머리·비용 꼬리는 새 키 `snap_col_*`·`snap_cost_note`(legal) 로 카탈로그에서 가져온다.
- 내 판단: `export_rights`(legal) 는 내보내기 대화상자 아래와 캔버스 도움말(?) 끝에 함께 보인다.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 21 (서버 `arch` 문서 종류 검증기)**

- 내 판단: 객체 키 정규식은 계획의 `^(meta|[nbef]:[0-9a-z_]{1,40})$` 가 아니라 `arch.md` 2.1 의 `^(meta|[nbef]:[A-Za-z0-9_-]{1,62})$` 를 쓴다(이미 1묶음에서 대체로 기록됨).
- 내 판단: P0 의 CollabHub 테스트들(`collab-hub`·`collab-kick`·`doc-versions`·`unit/collab-reducer`)은 `arch` 종류로 `n1`·`cpu`·16KB 문자열 같은 일반 객체를 써서 순서·한도·소켓·버전을 시험한다. 실제 `arch` 규칙으로는 표현할 수 없는 경우(16KB 필드, 1MB 문서)까지 있어 고정 데이터를 바꾸는 대신 `test/helpers.ts` 에 `archRulesOff()`(그 파일에서만 `DOC_TYPES.arch.validateOp` 를 끄고 끝나면 되돌림)를 두고 그 네 파일 머리에 한 줄씩 불렀다. 기존 단언은 하나도 고치지 않았다. `arch` 규칙 자체는 새 `unit/arch-doctype`·`realtime/collab-arch` 가 맡는다.
- 내 판단: 값 `null` 은 모든 필드에서 받는다(`arch.md` 1.2 "null = 빠진 것", 2.3 이 기본값으로 읽음) — 클라이언트가 필드를 비우는 `set` 을 보낼 수 있게.
- 내 판단: 가리키는 대상이 없는 참조(`parent`·`from`·`to`·`target`·`failover_to`)와 상자 종류 규칙(`parent_kind`)은 거절하지 않는다. 동시 삭제로 늘 생기고 `FromObjects`(2.3)가 모든 클라이언트에서 같게 고치기 때문이다. `arch_depth` 는 바꾸는 상자의 위쪽 사슬만 본다(`DocStateView` 에 자식 목록이 없음) — 자식까지 깊어지는 경우는 2.3 5단계가 끌어올린다.
- 내 판단: `arch_limit` 은 새 객체 `put` 과, 한도에 찬 상태에서 지운 객체를 되살리는 `undel` 에도 낸다. `maxObjects` 에 `box 100·fault 50·meta 1` 을 더했다(P0 의 적용 뒤 검사도 같은 표를 씀).
- 내 판단: `reset`(새 문서 첫 스냅숏·버전 복원)도 스냅숏 전체를 같은 규칙으로 검사한다(복원은 P0 가 `decideOp` 로 `validateOp` 를 거침).
- 내 판단: 서버 검증 표는 `arch.md` 2.4 로 넣었다. `collab.md` 6장(자리표시 "P3 가 더한다")은 이번 Task 의 파일 목록 밖이라 고치지 않았다 — 2.4 를 가리키는 한 줄이 필요하면 오케스트레이터 몫.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 22 (방에 올리기·공동 편집·접속자)**

- 내 판단: 계획의 `collabEventMsg{E}` 에 `instance` 필드를 더했다. 같은 문서를 연 캔버스가 둘 이상일 수 있고 앱은 사용자 메시지를 모든 인스턴스에 돌리므로, 자기 세션의 이벤트만 받게 한다.
- 내 판단: 파일 목록(shared.go·presence.go·sharedialog.go) 밖이지만 연결에 꼭 필요한 곳만 같은 패키지에서 고쳤다 — model.go(Update·initSink·CloseTool·runCommand·ToolStatus), edit.go(읽기 전용 안내 문구 = 이유별), keys.go·mouse.go(입력 뒤 선택 presence 동기화, 끄는 동안 presence), view_canvas.go(원격 표시·이름표), view_metrics.go(위 줄 방 상태), tool.go(`arch.save_copy` 팔레트). 계획의 커밋도 `git add internal/ui/arch/` 이다.
- 내 판단: 올리기 확인 창은 `widgets.Confirm`(48×8 고정)을 쓰지 않고 대화창 안의 둘째 단계로 만들었다. 고정 크기에서는 no_truncate·legal 경고문이 잘리기 때문이다. 경고문은 폭에 맞춰 줄바꿈하고(textwidth.Wrap — 한글은 음절 사이에서도 끊는 프로젝트 규칙), 행이 모자라면 아래의 키 안내부터 버린다.
- 내 판단: 위 줄 접속자 이름은 표시 이름(없으면 handle, 그것도 없으면 ID 끝 6자)으로 쓴다. 나를 맨 앞에 둔다(`sam·하나`).
- 내 판단: `EventOverwritten` 토스트는 `pos`·`rect`(옮김)일 때만 낸다(`moved_by`). 다른 필드 덮어쓰기는 `lastOverwrite` 에 기록만 한다 — 설정값 덮어쓰기마다 토스트를 띄우면 소란스럽고, 계획도 pos 만 명시했다. 상자 옮김(rect)도 "옮김" 이라 포함했다.
- 내 판단: 끄는 동안의 presence 간격(100ms, 초당 10 이하)은 `ctx.Now`(테스트는 FakeClock)로 잰다. collab 세션 자체도 presence 를 100ms 로 묶지만, 캔버스 쪽에서 먼저 줄여 보낸 횟수를 셀 수 있게 했다. 놓을 때 Dragging=null 을 바로 보낸다.
- 내 판단: 잠금 판정의 "owner·admin" 은 `ctx.Store.Spaces[doc.SpaceID].MyRole` 로 본다. 스토어에 그 방이 없으면 권한 없음으로 친다. `EventRejected` 의 `forbidden` 도 `locked` 와 같이 처리한다(collab 패키지 주석과 같음).
- 내 판단: `doc.updated` 의 `Deleted` 는 `EventKicked{Reason: doc_deleted}` 와 같게 처리한다(읽기 전용 + 안내 + 세션 닫음).
- 내 판단: 쫓겨났을 때 `Pending` 이 있으면 `kicked_pending(n)` 토스트로 `arch.save_copy` 를 권한다. 사본 저장은 팔레트 명령 `arch.save_copy`("아키텍처 설계 사본으로 저장")로 더했고, 개인 설계에서는 "이미 개인 설계" 안내만 한다.
- 내 판단: 원격 상태가 처음 도착하면(빈 → 객체) 뷰포트를 그 설계에 다시 맞춘다(`placed=false`). 세션이 열린 직후엔 상태가 비어 있어 빈 설계에 맞춰지기 때문이다.
- 내 판단: 테스트에서 "오프라인 30초" 는 FakeClock 을 30초 진행하는 것으로 표현했다(세션의 재접속 지연은 실시간이라 `SetBackoffForTest(0.02)` 로 짧게). 0 으로 두면 끊긴 동안 재접속 시도가 쉬지 않고 돌아 이벤트가 쏟아진다.
- 내 판단: 경고문 테스트의 "모든 크기" 는 `uitest.AllSizes()` + 40×14 중 40×14 이상(도구 화면이 그려지는 크기)으로 했다.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 23 (버전 목록·미리보기·차이·되돌리기)**

- 내 판단: `d` "두 버전 차이"는 고른 버전과 **바로 앞(더 오래된) 버전**을 비교한다(`v1 → v2 차이`). 두 버전을 따로 표시해 고르는 단계는 넣지 않았다 — 키 하나로 끝나고, 임의의 두 버전 비교는 차례로 넘기며 볼 수 있다. 가장 오래된 버전에서는 `versions_diff_oldest` 안내만 한다.
- 내 판단: 미리보기는 공유 문서 sink 안의 `preview` 로 둔다(`Objects()` 가 그 버전을 돌려주고 `ReadOnly()` = `roPreview`). 그동안 들어오는 원격 편집은 실제 상태(`objs`)에 계속 반영되고 `esc` 로 돌아오면 바로 보인다. 미리보기 중 편집 키는 `preview_readonly` 토스트.
- 내 판단: 목록 줄은 이름 버전 `v3 — 리뷰용`(오른쪽에 만든 사람·시각), 자동 버전 `v2 — 자동 저장 11-04 19:30`(시각이 줄 안에 있으므로 오른쪽은 비움), 되돌리기로 생긴 버전 `v4 — v2 버전으로 되돌림`. 시각은 `ctx.Loc` 의 `01-02 15:04`.
- 내 판단: 차이 목록의 이름은 객체 라벨(없으면 지금 설계의 라벨, 그것도 없으면 키), 필드는 설정 패널의 카탈로그 이름(`fields.go`)을 그대로 쓰고, 패널에 없는 `pos`·`rect`·`parent` 는 새 키 `ver_field_*` 로 더했다. 값은 저장값 그대로(`2 → 8`, 문자열은 따옴표 없이, 없는 값은 `—`).
- 내 판단: `arch.versions` 팔레트 항목은 앞에 있는 캔버스가 방 문서(`doc` 쿼리)일 때만 `Available`. 그래도 명령이 개인 설계에 닿으면 `versions_personal` 안내만 한다.
- 내 판단: 이름 저장의 서버 오류 `limit_exceeded` 는 대화상자 안의 안내(`versions_limit`)로, 그 밖 오류는 `versions_failed` 로 보인다. 되돌리기 성공 뒤에는 목록을 다시 불러와 새 버전에 커서를 둔다(이전 버전은 그대로 남음).
- 내 판단: 키 안내 줄이 72칸 대화상자를 넘으면 잘라 "…" 하지 않고 아래 줄로 접는다.
- 내 판단: 계획의 시험 3개에 더해 `TestVersionsNameSave`(이름 저장·`limit_exceeded`)와 `TestVersionsNotOnPersonal` 을 넣었다. 새 골든은 `TestVersionsListAndPreview/120x40.golden` 하나뿐이고 `-args -update` 로 만들었다. 기존 골든은 바뀌지 않았다.
- 내 판단: Task 22·23 은 TDD 순서(실패 시험 먼저)를 어기고 구현을 먼저 썼다(문맥 압축 전 작업 흐름). 대신 시험을 HEAD 소스 복사본(스크래치)에 넣어 돌려 `[build failed]` 실패를 확인한 뒤, 구현 위에서 통과를 확인했다.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 18~23 리뷰 반영**

- 내 판단: `collab.md` 6장을 채웠다(객체 종류·키 정규식·`maxObjects` 다섯 개·`arch_*` detail 표, 정본은 `arch.md` 2.1·1.4·2.4 를 가리킴). 5장의 `arch` 행(P0 소유)은 고치지 않고 6장에 "box 100·fault 50·meta 1 도 있다" 를 적었다.
- 내 판단: `archRulesOff()` 를 공유 `server/test/helpers.ts` 에서 P3 소유 새 파일 `server/test/arch-test-helpers.ts` 로 옮기고 `helpers.ts` 는 P0 Task 27 의 글 그대로 되돌렸다. P0 의 CollabHub 시험 네 파일(`api/doc-versions`·`realtime/collab-hub`·`realtime/collab-kick`·`unit/collab-reducer`)에는 import 한 줄 + 호출 한 줄이 남는다 — 그 시험들은 실제 `arch` 규칙으로 표현할 수 없는 일반 객체(16KB 필드, 1MB 문서)를 쓰므로 끄는 것 말고 방법이 없다. **오케스트레이터 확인 필요:** 이 P0 시험 변경을 인덱스 공유 파일 표에 "P3 = `archRulesOff` 호출 한 줄" 로 받아 줄지, 아니면 P0 이 시험 전용 문서 종류를 둘지. P4 의 `apicol` 검증기도 같은 문제를 만난다.
- 내 판단: 버전 차이의 필드 이름은 `edgeFields`·`trafficFields` 도 찾고, 설정 패널에 없는 필드(`kind`·`title`·`from`·`to`·장애의 `at_s`·`target`·`count`·`add_ms`·`dur_s`)는 새 키 `ver_field_*`(ko·en)로 더했다. 연결은 키 대신 `panel_edge`(`<출발> → <도착>`)로 부른다. 장애 객체 이름은 아직 키(`f:<id>`)다 — 라벨이 없는 객체라 이번에는 두었다.
- 내 판단: CA 5.6 P3 행에 `arch.save_copy` 를 더하고 `arch.versions` 는 앞의 인스턴스가 방 문서일 때만이라고 적었다.
- 내 판단: 설정 패널 입력을 손으로 만든 문자열에서 bubbles `textinput`(가상 커서 끔, `Cursorer` 로 실제 커서)으로 바꿨다 — 계획 Task 18 Produces 대로. 글자 입력은 전처럼 한 줄·`Sanitize`·60칸 상한, `ctrl+k`·붙여넣기 단축키·제안은 뺐다(chat `newTextInput` 과 같은 설정). 기존 골든은 그대로다.
- 내 판단: 재생 배속을 `canvasModel.speedX` 에서 계획대로 `playback.speed` 로 옮겼다. 편집·새 실행으로 `playback` 을 비울 때도 배속은 남긴다(전과 같은 동작). `playback` 의 나머지 필드 `computing`·`acc`·`cancel` 은 계획 Produces 에 없는 추가(백그라운드 계산 취소·0.1초 틱 누적)다.
- 내 판단: 끌기가 중간에 끊기면(오버레이·`ClearDrag`·포커스 이동·잠금·장애 메뉴·대상 삭제) `cancelGesture` 가 `clearDragPresence()`(시계 없이 `Dragging=null`)를 보낸다. 전에는 놓는 경로에서만 보내 상대 화면에 유령이 남았다.
- 내 판단: 방 문서 제목은 서버 `zText(1, 80)`(NFC 뒤 코드 포인트)에 맞춰 79 코드 포인트 + "…" 로 그래핌 경계에서 자른다(전에는 160칸). 80 그래핌 제목도 국기·태국어 모음 기호면 160 코드 포인트가 될 수 있다. `cleanText`(=`Sanitize`)가 ZWJ 이모지·데바나가리 결합 문자를 이미 첫 글자로 줄이는 것은 이번 범위 밖으로 두었다(표시용 정리를 서버로 보내는 문제 — 별도 판단 필요).
- 내 판단: Task 22·23 의 TDD 순서 위반은 위에 기록한 대로다. 리뷰 반영으로 `TestRemoteDragGhostVisible` 에 하나의 색(첫 상대 = `Acc2`) 검사를 더했고, 이름표 색을 일부러 틀리게 바꿔 그 단언이 실패하는 것을 본 뒤 되돌렸다.
- 내 판단: `-race` 에서 장애·재생 시험의 space(시간 실행 계산 = 하네스 Cmd 1초 한도)가 300초 설계로는 넘쳤다(3번 중 2번 실패). 시험 설계 `faultDesign` 을 60초(최소)로 줄였다 — `TestTinyStartStopOnly` 골든은 그대로다. 고친 뒤 `-race` 4번 모두 통과.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 25 (댓글 패널·앵커·doc:arch 알림)**

- 내 판단: 캔버스의 댓글 수 표시는 설계서의 `💬n` 이 아니라 `✎n` 으로 두었다 — archdraw 골든이 이미 `✎2` 로 고정돼 있고(바꾸면 기존 골든이 바뀜), 1칸이라 작은 노드에도 들어간다. `arch.md` 2.5 에 적었다.
- 내 판단: 계획의 `ctx.Store.Spaces[id].Members` 는 없다 — @ 멘션 후보는 `ListSpaceMembers(meta.SpaceID)` 로 받고, 못 받으면 Store 의 사용자들로 대신한다(나는 빼고).
- 내 판단: 아무것도 고르지 않고 `C` 를 누르면 캔버스 가운데 빈 자리(`at:x,y`)에 단다. 방 문서에서 빈 자리를 오른쪽 클릭하면 "댓글" 하나만 있는 메뉴가 뜬다(개인 설계에는 없음).
- 내 판단: 댓글은 설정 패널의 한 모드(`modeComments`)다 — 보통 패널에 탭 줄을 더하면 기존 골든이 바뀐다. 서랍 배치에서는 패널 서랍을 꺼내고, 자리가 없으면 캔버스 자리에 그린다.
- 내 판단: @ 자동 완성은 enter 로 고르고 ↑↓ 로 옮긴다 — tab 은 전역 키다.
- 내 판단: 노드 위 댓글 수는 노드 앵커만 센다(해결 안 된 묶음의 지우지 않은 댓글). 연결·상자·빈 자리 댓글은 패널에만 보인다.
- 내 판단: 잠긴 문서·버전 미리보기에서도 댓글은 달 수 있다(서버 권한이 정함). 삭제 UI 는 넣지 않았다. 댓글은 문서를 열 때 한 번 읽고 그 뒤로는 `doc.comment_created` 이벤트로 더한다.
- 내 판단: `openAlert` 는 이미 열린 같은 문서 인스턴스에도 `openCommentsMsg` 를 보내 댓글 패널을 연다(쿼리만 바꿔서는 Init 이 다시 돌지 않음).

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 26 (채팅 카드 `arch.design`·`arch.sim`)**

- 내 판단: Go 시그니처는 계획의 `designCard(d, r, docID, versionNo)` 대신 `designCard(t *i18n.T, d *archdoc.Design, r *archsim.Result, ref cardRef) (api.Card, string, error)`(`simCard` 도 같음) — 대체 문구 `body` 에 보낸 사람의 카탈로그가 필요하고, `doc_id` 는 `space_id` 와 함께만 실을 수 있다.
- 내 판단: 개인 설계 카드는 받는 곳이 DM 이든 방 채널이든 **늘** 사본을 싣는다 — `doc_id` 가 없으면 받은 사람이 열 길이 사본뿐이다(설계서는 "DM 처럼 방이 없는 곳에"). 16KB 를 넘으면 미리보기 전에 `card_too_big` 토스트로 끝내고 보내지 않는다.
- 내 판단: `version_no` 는 지금 늘 `null` 이다(캔버스가 최신 버전 번호를 따로 알지 못함). 방 문서인데 문서 정보(`GetDoc`)가 아직 안 왔으면 개인 설계처럼 사본을 싣는다.
- 내 판단: `arch.sim` 의 `max_rps` = 분석의 클라이언트 초당 요청(`Result.RPS`), `read_permille` = id 순 첫 클라이언트의 읽기 비율(없으면 기본 800), `bottleneck.target_label` = 그 노드의 라벨(없으면 id, 80 코드 포인트로 자름). `challenge` 는 Task 28 몫이라 지금은 늘 `null`.
- 내 판단: 카드 `Open` 은 열 것이 없을 때(도구 꺼짐, 데이터 이상, 방 멤버가 아닌데 사본도 없음) `nil` 대신 그 메시지의 스레드로 가는 Navigate 를 돌려준다 — chat 은 `Open` 이 있으면 스레드를 열지 않기 때문에, 계획의 "nil(스레드)" 뜻을 지키려면 이렇게 해야 한다.
- 내 판단: 클라이언트 재검사의 `space_id`·`doc_id` 는 ULID 가 아니라 `^[A-Za-z0-9_-]{1,64}$` 로 본다(인스턴스 키·쿼리에 들어가는 안전한 글자인지가 요점이고, 시험 허브의 문서 id 는 `D0001`). 그 밖 범위는 서버와 같다.
- 내 판단: 카드 끝에 키 안내 줄(`⏎ 열기` / `⏎ 사본으로 열기` / `r 다시 돌려 보기`)을 더했다(새 카탈로그 키). 위젯·초소형에서는 제목 한 줄만 — 비용 줄은 자르면 안 되므로 넣지 않는다.
- 내 판단: "다시 돌려 보기"(`r`, sim 카드)는 사본이 있을 때만 돈다 — 카드의 시드로 `Analyze` 해 p50·p99·에러·비용·`max_rps`(와 엔진 버전)가 모두 같으면 `✓`, 아니면 `✕` 와 지금 숫자를 토스트로 보인다. 방 문서만 가리키는 카드는 안내만 한다(그 버전을 받아 오는 일은 범위 밖).
- 내 판단: 사본은 메시지 id 별로 메모리에 16개까지 둔다. 재시작 등으로 없으면 `arch:msg` 인스턴스가 `GET /v1/messages/{id}` 로 다시 읽는다.
- 내 판단: 보내기 대화상자에서 `s` 가 설계/시뮬 카드를 바꾸고, 미리보기에서 `esc` 는 대화 고르기로 돌아간다. 방 채널은 목록에서만 `#일반 · 노바 개발방` 처럼 방 이름을 붙이고, 미리보기·토스트에는 `#일반` 만 쓴다.
- 내 판단: 진짜 chat 이 그리는 시험(`TestCardRendersInChat`·`TestOldClientSeesBody`·`TestCardRendererHostileData`)은 시험만 있는 새 패키지 `internal/ui/arch/archchat` 에 두었다 — arch 시험 바이너리는 stub chat 이라 chat 을 import 하면 기존 arch 골든이 바뀐다. arch 패키지에는 렌더러 단위 시험(`TestCardLinesHostile`·`TestCardOpen`·`TestCardCopyFetched`)을 더했다. "JSON 이 아닌 data" 는 메시지 목록 자체가 파싱되지 않아 화면 시험으로는 만들 수 없으므로 단위 시험에서만 보고, chat 시험은 모양이 틀린 JSON 으로 본다.
- 내 판단: 서버 미니 그림의 칸 계산은 동아시아 넓은 글자·이모지 범위만 2칸으로 보는 근사다. 스키마는 `strictObject` 라 모르는 키도 `invalid`.
- 내 판단: P0 의 `server/test/api/cards.test.ts` 는 `arch.design` 에 자리표시 데이터(`{n:14}` 등)를 보낸다 — `archRulesOff` 와 같은 방식으로 P3 소유 `arch-test-helpers.ts` 의 `archCardsLoose()`(import 한 줄 + 호출 한 줄)를 더했다. **오케스트레이터 확인 필요**(Task 18~23 리뷰의 `archRulesOff` 질문과 같은 종류).
- 내 판단: `sharedialog.go` 의 제목 자르기를 `cutCodePoints` 로 뽑아 카드 제목(80 코드 포인트)과 함께 쓴다(동작 그대로).

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 27 (수용 기준 일괄·의존 규칙·모드 전환)**

- 내 판단: 쓸어 보니 창 크기가 바뀌면 뷰포트가 왼쪽 위 모서리를 지켜 가운데가 움직였다. 노드를 고른 채면 가운데(세계 좌표)를 지키도록 `layout()` 을 고쳤다. 고른 것이 없으면 전처럼 왼쪽 위를 지킨다 — 그래야 크기를 차례로 바꾸는 기존 골든(`TestArchScreens` 등)이 그대로다.
- 내 판단: `TestCostNeverTruncated` 는 `$` 가 있는 줄의 `추…` 와 어디든 `추정…`·`가격…` 만 잘림으로 본다 — 44x24 의 키 안내 `a 추…`(추가)는 비용이 아니다.
- 내 판단: `TestNoSecretsInExports` 는 자격 증명 속성에 **값**(`= "`)이 있는지를 본다 — RDS 의 `manage_master_user_password = true` 는 암호를 공급자의 비밀 저장소에 두는 설정이라 괜찮다.
- 내 판단: `TestRatchetZero` 는 i18n 래칫과 같은 규칙(시험·`_gen.go` 제외, 한글 든 문자열 리터럴)을 arch 패키지 시험 안에서 다시 센다 — 래칫의 함수는 `i18n_test` 패키지 안이라 가져올 수 없다. P3 패키지가 `legacy_literals.txt` 에 없는 것도 본다.
- 내 판단: `TestPureImports` 는 `internal/archsim` 에 두고 `go list -deps` 로 본다. chat 쪽은 `internal/ui/arch` 뿐 아니라 `internal/arch*` 전부를 막았다.
- 내 판단: 계획 Step 4 의 `make lint`·`make cross` 에 더해 작업 지시의 검증 목록(`go build ./...`·`go test -count=1 ./...`·`go vet` 세 GOOS·바뀐 파일 `gofmt -l`·서버 `typecheck`·`npm test`)을 돌렸다. P3 밖 `testdata` 는 바뀌지 않았다(새 골든은 `TestSendDesignCardPreviewThenSend/120x40`, `TestAcceptanceSizes/*` 다섯 개).

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 25~27 리뷰 반영 (위 Task 25·26 판단 중 바뀐 것은 여기가 우선)**

- 내 판단: 시뮬 카드 "다시 돌려 보기" 키를 `r` 에서 `R`(shift+r)로 옮겼다 — `r` 은 채팅 메시지 줄의 리액션(`MsgReact`)이고 카드 `Key` 가 채팅 키보다 먼저 돈다. `R` 은 채팅이 메시지 줄에 묶지 않은 키다. 키맵(오버라이드 포함)이 묶은 키면 카드는 가져가지 않고(`ctx.Keys.Bound`), 다시 돌릴 것이 없는 카드는 `R` 도 채팅에 넘긴다(안내 줄에도 안 보임). 키맵에 새 동작을 더하면 P0 `keymap.go` 를 고쳐야 해서 그러지 않았다.
- 내 판단: 방 문서에서 만든 카드는 **절대** 사본을 싣지 않는다(`cardRef.room`). `GetDoc` 이 아직 안 왔거나 실패해 방을 모르면 `arch.send_card` 는 대화상자를 열지 않고 `card_doc_loading` 안내 + `GetDoc` 을 다시 부른다(앞의 "정보가 안 왔으면 개인 설계처럼 사본" 판단은 폐기 — 방 내용이 방 밖 DM 으로 나갈 수 있었다).
- 내 판단: 방 문서 카드의 `version_no` 를 채운다. 버전 미리보기 중이면 그 버전, 아니면 대화상자를 연 때의 `seq` — 가장 새 버전이 그 `seq` 면 그것, 아니면 미리보기로 넘어갈 때 이름 버전 `카드 · <제목 50자>` 를 만든다(`POST /v1/docs/{id}/versions`, 이름 버전 한도 100 에 센다). 저장 안 된 내 편집이 있으면 `card_version_unsaved`, 만든 버전의 `seq` 가 본 것과 다르거나 실패하면 `card_version_failed` 로 보내지 않는다. 사본 없는 방 문서 sim 카드는 그 방 멤버에게만 `GET /v1/docs/{id}/versions/{no}` 로 그 버전을 받아 다시 돌린다. 같은 상태에서 카드를 여러 번 보내면 버전은 하나만 생긴다.
- 내 판단: `simCard` 에 다섯째 인자 `ch *cardChallenge`(nil = 없음)를 더했다 — Task 28 이 시그니처를 다시 바꾸지 않게. 실제 시그니처는 `designCard(t *i18n.T, d *archdoc.Design, r *archsim.Result, ref cardRef) (api.Card, string, error)`, `simCard(t, d, r, ref, ch *cardChallenge)`. **오케스트레이터 확인 필요:** 계획 Task 26 Produces·Task 28 Consumes 를 이 시그니처로 고쳐 주시길.
- 내 판단: 위젯 모드도 카드 전부(미니 그림 — 칸 폭으로 자름, 요약·비용 줄바꿈, 엔진 안내, 키 안내)를 그린다. 초소형만 제목 한 줄. 앞의 "위젯·초소형은 제목 한 줄" 판단은 폐기.
- 내 판단: 서버 미니 그림 칸 계산을 클라이언트 `textwidth.Width` 의 하한으로 바꿨다 — `Intl.Segmenter` 그래핌마다 한글 음절 2칸, 결합·서식·한글 채움 문자만인 묶음 0칸, 나머지(이모지 포함) 1칸. 🖥·🌡 같은 글자 표현 이모지(Go 1칸)·피부색·ZWJ 묶음(Go 2칸 하나)을 서버가 더 넓게 세어 클라이언트가 만든 카드를 거절하던 문제. 이제 서버는 결코 클라이언트보다 엄하지 않다(대신 이모지·한자 줄은 60칸을 넘어도 통과 — 정확한 자르기는 클라이언트가 그릴 때 다시 한다, 데이터 전체는 16KB 상한).
- 내 판단: @ 멘션 후보는 방 멤버만(Store 사용자로 대신하던 것 폐기). 멤버를 받기 전에는 후보가 없고, 받기에 실패하면 다음에 댓글을 열 때 다시 묻는다. 100명이 넘는 방은 커서로 끝까지(최대 50쪽 = 방 최대 5,000명) 읽는다.
- 내 판단(보류, **오케스트레이터 확인 필요**): P0 `server/test/api/cards.test.ts` 의 `archCardsLoose()` 두 줄과 P0 CollabHub 시험 네 파일의 `archRulesOff()` 두 줄은 그대로 둔다 — 인덱스 공유 파일 표에 "P3 = 이 한 줄 호출" 예외로 받을지, P0 이 시험 전용 카드·문서 종류를 둘지는 오케스트레이터의 몫이다(Task 18~23 리뷰와 같은 질문).
- 내 판단(보류, **오케스트레이터 확인 필요**): 캔버스 댓글 수 `✎n`(설계서 `💬n`)과 댓글 패널 = 설정 패널의 한 모드(설계서 "표준은 오른쪽 패널의 탭")는 그대로 둔다. `✎` 골든은 P3 자신의 Task 12 골든이라 바꿀 수는 있지만, `💬` 는 2칸 이모지라 작은 노드 상자 폭 계산과 archdraw 골든이 함께 바뀌고 탭 줄은 패널 골든을 바꾼다 — 작업 지시의 "골든은 Task 가 말할 때만 -update" 에 걸려 오케스트레이터 승인 없이는 하지 않았다. 승인하면 설계서·계획 쪽을 고치거나 P3 골든을 갱신한다.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 28 (선택: 일일 도전)**

- 내 판단: 시나리오 파일 형식은 `scenarios/<id>.json` = `{ id, goal_p99_ms, goal_err_permille, budget_usd_month, start: <설계 JSON v1> }`. 시드·실행 길이·예약 장애는 `start` 안에 두고 `Scenario.Seed`·`Faults` 는 거기서 채운다 — 캔버스에 연 시작 설계가 장애를 그대로 보여 주도록. `Scenarios()`·`ByID`·`ForDate` 는 매번 새로 디코드한 사본을 준다(호출자가 고쳐도 공유되지 않음).
- 내 판단: 계획의 `Score` 외에 `Evaluate(tl, s) Outcome{KeptS, TotalS, Dropped, CostUSDMonth, Score}`, `Apply(d, s) (*Design, error)`(`ErrClientMissing`), `ByID`, `Date(t)`, `(Scenario).Design()` 을 더했다(`Score` = `Evaluate(...).Score`).
- 내 판단: "버린 요청" = Σ 프레임 초당 요청 × 프레임 에러율 × 프레임 길이(실패한 요청 전부). 엔진의 `queue_drop` 이벤트는 넘치기 시작한 틱에만 나와 합계를 낼 수 없고, 엔진을 고치면 `EngineVersion`·고정 묶음이 바뀌므로 프레임 숫자로만 셌다. "지킨 초" = 프레임 p99 ≤ 목표이고 에러율 ≤ 목표인 프레임 길이의 합(목표값 포함). 비용 = 시간 실행 요약의 평균 월 비용. 계산은 모두 int64, 점수는 0..MaxInt32.
- 내 판단: 도전 실행은 내 설계에 시나리오의 시드·실행 길이·장애·**클라이언트 트래픽(노드 id 로)** 을 덮어 돌린다 — 트래픽을 줄여 점수를 올리는 길을 막는다. 시나리오 클라이언트를 지우면 돌리지 않고 `challenge_client_missing` 토스트. 장애 대상 노드를 지우는 것은 막지 않았다(엔진이 없는 대상의 장애를 버림 — "친구끼리 놀이" 수준, 순위표 없음).
- 내 판단: 시나리오 15개 이름은 실제 회사·서비스·다른 작품 이름 없이 지었다(수강신청 대란·콘서트 예매 오픈·새해 인사 폭주 등). id 에 `sugang` 은 쓰지 않았다 — Task 26 리뷰의 `TestSimCardChallenge` 가 `sugang` 을 모르는 시나리오로 그리기 때문(id 그대로 표시). 시작 설계는 조용한 구간은 목표를 지키고 장애·급증에서 무너지도록, 예산은 대부분 시작 비용의 1.0~1.25배로 맞췄다(`photo_upload` 약 2배, `search_revamp` 0.9배, `budget_squeeze` 약 절반 — 비용을 줄이는 것이 과제). 시험 `TestScoreDeterministic` 이 "시작 설계가 이미 만점이 아님"을 본다.
- 내 판단: 입구는 팔레트 `arch.challenge`(플래그 뒤, 허브 행 없음)만. 인스턴스 key `arch:challenge:<UTC 날짜>`, 쿼리 `challenge=<id>`·`date=<날짜>` — 같은 날 두 번 열면 같은 인스턴스. 시작 설계는 저장 안 된 새 설계(`localSink`, 저장소 없음)로 열고 제목은 카탈로그 시나리오 이름. 날짜·id 가 이 빌드와 맞지 않으면 빈 새 설계.
- 내 판단: 점수가 나면 `arch.send_card` 는 시뮬 카드 쪽으로 열리고(`s` 로 설계 카드로 바꾸면 도전 칸 없음) 카드의 설계·숫자·시드는 **점수를 낸 실행 설계**(시나리오를 덮은 것)다 — 카드 `R` 다시 돌리기가 그대로 맞도록. 편집하면(`designChanged`) 점수를 지운다.
- 내 판단: 카드의 도전 줄은 시나리오 id 대신 카탈로그 이름(`challenge_<id>_title`, `i18n.T.Lookup`)을 그린다 — 모르는 id(새 버전 클라이언트가 보낸 카드)는 id 그대로.
- 내 판단: Task 28 파일 목록 밖이지만 같은 P3 패키지에서 연결에 필요한 곳만 고쳤다 — `model.go`(필드·initSink), `playback.go`(실행 설계·점수), `senddialog.go`(도전 칸), `cards.go`(도전 줄 이름), `tool.go`(팔레트), `internal/archsim/imports_test.go`·`acceptance_test.go`(순수성·래칫 목록에 `archsim/challenge`), 계약 `arch.md` 3.4·`client-architecture.md`(패키지 표·의존·CA 5.6 P3 행). 기존 골든은 바뀌지 않았다.
- 내 판단: UI 시험은 오늘 시나리오를 60초 실행으로 줄인 것으로 바꿔 끼운다(`scenarioOf` 변수) — space 의 시간 실행은 하네스 Cmd 1초 한도 안에서 돌아야 하고 `-race` 에서는 150~180초 시나리오가 넘칠 수 있다.

**4묶음 (Task 18~23 — 설정 패널·장애·파일·서버 검증·공유·버전) / 내 판단 — Task 28 리뷰 반영 (위 Task 28 판단 중 바뀐 것은 여기가 우선)**

- 내 판단: 도전 실행(`challenge.Apply`)은 시나리오 클라이언트 트래픽이 **서버까지 가야** 돌린다(`ErrUnrouted` → `challenge_unrouted` 토스트). 엔진은 나가는 연결이 넘기지 않은 몫을 그 노드에서 지연·비용 없이 끝난 것으로 세므로(`buildRoutes` "the rest ends at the node", 고리를 닫는 연결은 아무것도 나르지 않음), 클라이언트만 남기거나 클라이언트 → LB 하나만 두면 모든 시나리오가 만점이었다. 규칙: 시나리오 클라이언트에서 (읽기·쓰기 각각 비율 > 0 인 연결로) 닿는 클라이언트·LB·CDN·큐는 받는 종류마다 나가는 연결 비율 합 ≥ 1000‰, 그리고 트래픽을 나르는 연결의 고리 위에 있지 않을 것. API·워커·캐시·DB·검색·객체 저장소·외부 서비스는 스스로 요청을 처리하는 노드라(엔진이 CPU·IOPS·비용을 셈) 끝나도 된다. 엔진을 고치면 `EngineVersion`·고정 묶음이 바뀌므로 도전 쪽에서만 막았다.
- 내 판단: 예약 장애의 대상(노드·연결·AZ·리전)을 지운 설계도 돌리지 않는다(`ErrFaultTargetMissing` → `challenge_fault_target_missing`). 앞의 "장애 대상 삭제는 막지 않음" 판단은 폐기 — 장애를 피해 점수를 올리고, 실행 설계(= 카드 사본)가 `faults[0].target: dangling_ref` 로 받는 쪽 `archdoc.Decode` 에 거절돼 카드의 사본·`R` 다시 돌리기가 사라졌다. `Apply` 는 실행 설계의 `Validate` 에서 `faults[i].target` 문제만 보고 거절한다(나머지는 `FromObjects` 가 이미 정리).
- 내 판단: 시나리오는 모두 예약 장애를 하나 이상 가진다(C2.12 "시드 + 트래픽 곡선 + 예약된 장애 + 목표 + 시작 설계"; `TestScenariosHaveFaults`). 비어 있던 둘에 더함: `budget_squeeze` 60초에 `replica` 지연 +400ms 20초(값싼 설계가 복제본 느려짐도 견뎌야 함 — 시작 설계 지킨 초 120→100), `concert_presale` 50초(급증 한가운데)에 `worker` 인스턴스 1대 30초 꺼짐.
- 내 판단(**오케스트레이터 확인 필요**): 설계서 C2.12·JX12 는 일일 도전과 LeetCode 오늘의 문제를 "둘 다 허브에서 따로 연다". 지금 입구는 팔레트 `arch.challenge` 뿐이다 — P0 허브(`RouteTools`)·열기 메뉴에 도구가 행·항목을 더할 확장점이 없고 P3 는 그 파일을 고칠 수 없다. JX12 는 "한 화면으로 묶지 않는다"는 판단이지 허브 행이 없는 이유가 아니므로, 코드 주석·`arch.md` 3.4 에서 "허브 행 없음 — JX12" 표현을 고쳤다(CA 5.6 P3 행에는 원래 JX12 인용이 없었다). 허브에 도전 행이 필요하면 P0 에 도구 허브 확장점(예: `RegisterHubEntry`)을 둘지 정해 주시길.

(P3: 246줄)

### P4 (`exec-log/P4.md`)


**1묶음 (Task 1~6 — UI 없는 엔진 `internal/apitool`) / 내 판단**

- 내 판단: `cmd/masil/importgraph_test.go` 의 `toolPackagePrefixes` 에 `internal/apitool` 줄이 P0 Task 23 에서 이미 들어가 있어 Task 1 은 그 파일을 고치지 않았다.
- 내 판단: `TestApitoolImportRules` 는 `go list -deps ./internal/apitool/...` 에서 `internal/collab` 만 거쳐 오는 의존을 뺀 뒤 `internal/api`·`internal/ui`·`internal/realtime`·`internal/store` 를 찾는다. CA 2.2 의 `collab ─▶ api, realtime` 때문에 Task 18 이 collab 타입을 import 하면 글자 그대로의 검사는 반드시 깨지기 때문이다. CA 2.2 의 P4 줄에도 "collab 을 거친 전이만 예외" 로 적었다.
- 내 판단: `LocalStore.Changed` 는 수정 시각이 아니라 컬렉션 파일 이름·내용의 sha256 지문을 비교한다(같은 크기·같은 시각의 편집도 잡고, 내용이 같은 touch 는 무시). 내 `Load`·`Save` 가 지문을 갱신한다. 한 번도 읽지 않은 컬렉션은 "안 바뀜".
- 내 판단: 파일 JSON 필드 이름(C3.16)은 snake_case(`content_type`·`file_name`·`timeout_ms`·`allowed_hosts` …), 빈 값은 생략(`omitempty`·`omitzero`), HTML 글자는 이스케이프하지 않고 숫자는 원문 유지. `collection.json` 은 `version`(1)·`id`·`name`·`vars`·`root`(폴더 나무, 요청은 id)만, 요청은 `requests/<id>.json`(파일 이름이 id 의 출처). `globals.json` = `{"version":1,"vars":[…]}`. 나무에 없는 요청 파일(밖에서 더한 것)은 읽을 때 루트 끝에 붙이고, 파일이 없는 나무 항목은 버린다.
- 내 판단: `ErrBadName` 기준 — 빈 이름, `.`·`..`, `/`·`\`, Windows 금지 글자 `:*?"<>|`, 제어 문자, 앞뒤 공백, 끝 점, 200바이트 초과. 요청 id(파일 이름)에도 같은 검사.
- 내 판단: `Var.UnmarshalJSON` 도 `Secret` 이면 값을 버린다(손으로 파일에 넣은 비밀 값도 읽지 않음 — 값은 Vault 에서만).
- 내 판단: 비밀 판정은 OR — 환경·컬렉션·전역 중 한 층이라도 그 이름을 `Secret` 으로 선언하면 비밀이고, 그때는 임시 값·내 값·공유 값을 모두 무시하고 `Vault` 만 쓴다. 공유 문서의 비밀은 `Source = SourceMine`(C3.8 "늘 내 값"), 개인은 선언한 층. Vault 에 없거나 빈 값이면 `*UndefinedError`, 그 밖의 Vault 오류(`ErrUnavailable` 등)는 그대로 감싸 돌려준다.
- 내 판단: 컬렉션 변수가 비밀이면 칸 이름이 필요해 `Scope` 에 `CollectionID` 를 더하고 envID 자리에 `col:<CollectionID>` 를 쓴다(`local/col:<id>/<name>`, `doc/<doc>/col:<id>/<name>`). 전역은 docID 와 상관없이 `globals/<name>`. 환경 `ID` 가 비면(개인 환경 파일) 환경 이름을 envID 로 쓴다. `Mine` 키 `<envID>/<name>` 도 같은 규칙.
- 내 판단: 계획 밖 겉면을 조금 더했다 — `Scope.SecretSlot(name) (vaultKey, mineKey string, secret bool)`(추출·UI 가 Vault 칸을 알아야 함), `HasPlaceholder`, `Settings.Effective()`·`Timeout()`, `Folder.RequestIDs()`, `ToolID = "api"`(엔진이 `ui` 를 import 할 수 없어 상수를 따로 둠), `CollectionVersion`, `ExtractError`, 검사 실패 사유 코드 상수.
- 내 판단: `Prepare` — 방식 이름은 대문자로, 빈 값은 GET, HTTP 토큰이 아니면 오류. URL 에 `://` 가 없으면 `http://` 를 붙이고 http/https 만 받는다. 쿼리 행은 URL 의 기존 쿼리 뒤에 순서대로(`url.QueryEscape`). 사용자 `Content-Type` 헤더 행이 있으면 본문 종류의 기본값보다 이긴다. raw 기본 형식은 `text/plain; charset=utf-8`. multipart 는 `io.Pipe` 로 흘리고 파일은 보낼 때 열며(같은 boundary 로 리다이렉트 때 다시 열 수 있음) `Prepare` 에서 있는지만 확인한다. 파일 파트 형식은 확장자로. `Host` 헤더 행은 `req.Host` 로.
- 내 판단: 오류·로그에 URL 이 남지 않게 — 오류 문장에 URL 을 넣지 않고 `*url.Error` 껍질을 벗긴다(쿼리 속 비밀). 실패한 보내기도 `status=0` 로 로그 한 줄, 오류 문장은 로그에 쓰지 않는다.
- 내 판단: 타임아웃은 `http.Client.Timeout` 대신 `context.WithTimeout` — 그래야 `errors.Is(err, context.DeadlineExceeded)` 이고 본문 읽기까지 덮는다. 취소·시간 초과는 늘 ctx 의 오류로 돌려준다.
- 내 판단: `Accept-Encoding` 은 스스로 붙이지 않는다(curl 과 같음). 서버가 `Content-Encoding: gzip`/`x-gzip` 을 주면 직접 푼다(그 밖의 인코딩은 그대로). 넘침 파일은 `SpillDir`(빈 값 = `os.TempDir()`)의 `masil-api-*.body`(0600), 지우는 것은 호출자. 넘침 중 오류·취소면 바로 지운다. 바이너리 판정에서 8KB 경계에 잘린 UTF-8 글자는 바이너리 근거로 보지 않는다.
- 내 판단: `Jars.List(colID, u)` 는 표준 `cookiejar` 의 한계로 "그 URL 에 보낼 쿠키" 만 돌려준다(통 전체 목록 겉면이 표준에 없음).
- 내 판단: `CheckSecretHosts` 는 `Sender.Send` 첫머리에서 늘 부른다(화면·실행기 어느 길로 와도 막힘). 허용 목록이 빈 비밀은 어디로도 가지 않는다. 호스트 비교는 소문자·포트·`[]`·끝 점을 뺀 정확 일치.
- 내 판단: `PrivateOK` 는 그 요청의 **원래 호스트에만** 적용 — 허용 뒤 리다이렉트가 다른 사설 호스트로 가면 다시 `*PrivateTargetError`(`TestForeignRedirectToPrivateAsks` 추가).
- 내 판단: 프록시(`HTTPS_PROXY`)를 쓸 때는 연결 주소가 프록시라서 연결 시점 IP 검사를 프록시 주소에는 하지 않고, 대상 호스트의 글자 검사(`localhost`·`*.local`·사설 IP 글자)는 `Transport.Proxy` 함수 안에서 한다. (리뷰 뒤 고침) 검사 대상 이름이면 `Transport.Proxy` 안에서 내 PC 의 해석기(`Sender.Resolver`, 없으면 시스템)로 한 번 풀어 사설 주소가 하나라도 있으면 `*PrivateTargetError` — 프록시가 따로 푸는 것이라 최선 노력이고, 내 PC 에서 풀리지 않으면(프록시만 아는 이름) 지금처럼 프록시로 보낸다. 시험은 `net/http` 가 환경 변수를 프로세스에 한 번만 읽어 `proxyFromEnv` 꾸러미 변수로 바꿔 끼운다.
- 내 판단: `Sender.Resolver` 가 있으면 가드 여부와 상관없이 늘 그것으로 풀고 푼 IP 로 바로 연결한다(시험 격리, 풀고 다시 푸는 틈 없음). 사설 범위에 계획 목록 외 `fec0::/10`, NAT64 `64:ff9b::/96`(안의 IPv4 로 판정), 링크 로컬·인터페이스 로컬 멀티캐스트를 더했다. `PrivateTargetError.IP` 는 이름만으로 막은 경우(`printer.local`) 빈 값.
- 내 판단: 리다이렉트를 멈출 때 `RedirectBlock.Var` 는 다음 호스트를 허용하지 않는 첫 비밀 이름, `To` 는 정규화한 호스트 이름.
- 내 판단: JSON 경로 문법에 `["name"]` 도 받고, `[*]` 는 객체 값에도(키 정렬 순) 쓴다. `[*]` 가 든 경로의 값은 결과 배열 전체(`len`·`contains`·`eq` 가 그 배열을 본다). `.*`·`..`·필터·음수 색인은 `ErrBadPath`.
- 내 판단: 검사 표를 조금 넓혔다 — `time`·`size` 에 `gt`, `status in` 은 쉼표 목록(`2xx,404`), 단위 없는 시간 = ms, 단위 없는 크기 = B, 소수 허용. `eq` 는 숫자를 `big.Rat` 로(12000 = 12000.0), 배열·객체는 정규 JSON 비교, 문자열은 그대로 또는 따옴표 JSON 문자열. `AssertResult.Err` 는 영문 고정 코드 상수(`ErrBodyTruncated`="body truncated", `ErrNotJSON`, `ErrBadRegex`, `ErrRegexTooLong`, `ErrPathSyntax`, `ErrUnknownOp`, `ErrUnknownTarget`, `ErrBadValue`, `ErrMissing`) — 화면이 문구로 바꾼다. `Got` 은 256바이트에서 자른다.
- 내 판단: 잘린 본문의 `body contains/regex` 는 앞 10MB 에서 찾으면 통과, 못 찾으면 실패 + `body truncated`(뒤쪽에 있을 수 있음).
- 내 판단: `ApplyExtracts` — 공유 컬렉션(`DocID` 있음)의 비밀 아닌 추출 값은 공유 환경이 아니라 `Mine`(내 값)에 넣는다. 응답 내용이 문서로 올라가 동료·서버에 가면 안 되기 때문(C3.10 "요청은 내 PC 에서만"). 개인은 환경(없는 이름은 추가), 환경이 없으면 전역. 값이 같으면 `changed` 에 넣지 않는다. 비밀은 `Vault.Put` 하고 `ErrNeedConsent` 를 그대로 돌려준다.
- 내 판단: 탐지 규칙이 겹치면 더 구체적인 규칙 하나만 남긴다(개인 키 > JWT > AWS > GitHub > Slack > Bearer > Basic > 이름+무작위성; JWT 를 실은 Bearer 는 `jwt` 하나). Bearer/Basic 의 범위는 토큰 부분만(`{{token}}` 으로 바꿀 때 `Bearer ` 를 남김). 개인 키는 END 줄이 있으면 거기까지. GitHub 는 `ghp_/gho_/ghu_/ghs_/ghr_` + 36자와 `github_pat_` + 22자 이상, Slack 은 `xox[abprs]-` + 10자 이상, AWS 는 `AKIA`/`ASIA` + 16자.
- 내 판단: `SensitiveHeader` 의 이름 낱말(`token|secret|key|auth|session|password`)은 탐지기의 `named_high_entropy` 이름 규칙과 같은 표 하나. `RedactString` 은 비밀 값의 URL 쿼리·경로 인코딩 모양도 가린다(Review 3 — 특수 문자가 든 비밀이 치환 후 URL 에 인코딩되어 남는 경우).

**리뷰 반영 (Task 1~6 리뷰 8건)**

- 내 판단: `TestApitoolImportRules` 를 집합 소속이 아니라 import 그래프 간선으로 다시 썼다(`go list -deps -json`, apitool 패키지에서 BFS, `internal/collab` 안으로는 내려가지 않음). 그래서 apitool 이 `internal/api`·`realtime` 을 직접 import 하거나 collab 을 거치지 않는 경로로 닿으면 실패한다(시험용 탐침 import 로 확인 — 옛 시험은 통과, 새 시험은 실패). CA 2.2 의 P4 줄 문구도 그 뜻으로 고쳤다.
- 내 판단: `Host` 헤더 행이 있으면 `CheckSecretHosts` 가 URL 호스트와 함께 그 호스트(정규화)도 쓴 비밀마다 허용 목록에 있어야 보낸다(`*HostNotAllowedError.Host` = 그 호스트). 상대 리다이렉트가 `req.Host` 를 이어받는 경우도 `redirect` 가 같은 검사로 멈춘다. 비밀이 없는 요청의 `Host` 행은 그대로 둔다.
- 내 판단: 따라가지 않는 리다이렉트(`NoFollow`·`RedirectBlock`)에서 `net/http` 가 `GetBody` 로 다시 연 본문을 닫지 않아 multipart 쓰기 고루틴이 남던 것을 `CheckRedirect` 가 `http.ErrUseLastResponse` 를 돌려줄 때 `req.Body.Close()` 로 고쳤다(시험: 20번에 고루틴 3 → 23 이던 것이 그대로).
- 내 판단: 가리기에 `url.URL` 이 경로를 쓰는 모양(`(&url.URL{Path: v}).EscapedPath()`, `/`·`;`·`,` 를 남김)을 더했다. 소문자 16진 변형은 우리 코드가 만들지 않아 더하지 않았다.
- 내 판단: httptrace 고리를 `newPhaseTrace` 로 빼고 뮤텍스로 감쌌다. 연결 단계는 첫 시도 시작부터 첫 성공까지 한 구간(Happy Eyeballs 의 겹친 시도를 두 번 세지 않고, 늦게 끝난 패자는 무시).
- 내 판단: `ApplyExtracts` 의 공유 컬렉션 → `Mine` 동작은 그대로 두고 계획의 Task 5 Interfaces 줄과 Task 18 Step 4 에 그 뜻을 적었다.

**2묶음 (Task 7~12 — 기록·실행기·변환기·문구 영역과 위젯) / 내 판단**

- 내 판단: (T7) 기록 파일 이름 ULID 는 주입된 시계(`now`)로 `history.go` 가 직접 만든다. `ids.New()` 는 실제 시각을 써서 시계 주입·나이 판정이 안 되고, 엔진은 외부 모듈(oklog/ulid)을 직접 import 하지 않는다. 같은(또는 앞선) ms 면 앞 ID + 1(단조), 처음 쓸 때 폴더의 가장 큰 ID 에서 잇는다. 가지치기(500건·30일)는 파일을 읽지 않고 파일 이름의 시각으로 한다.
- 내 판단: (T7) `DefaultHistoryOptions()` 를 더했다. `OpenHistory` 는 `Max<=0` → 500, `MaxAge<=0` → 30일. `Add` 는 resp 가 nil 이어도 받는다(보내기 실패 기록). 읽지 못한 기록 파일은 `List` 가 건너뛴다.
- 내 판단: (T7) 기록·내보내기 공용 `RedactRequest(r, secretValues) Request` 를 내보냈다. 민감 이름(헤더·쿼리·form)의 값과 `Auth.Pass/Token/Value` 는 `{{자리표시자}}` 와 인증 방식 낱말(Bearer·Basic·Token·Digest)만 남으면 그대로, 직접 친 값이면 `•••`. 템플릿 URL 의 민감 쿼리 값도 같은 규칙. 치환 후 URL 은 비밀 값(쿼리·경로 인코딩 모양 포함)과 민감 이름 쿼리 값을 모두 가린다 — 그래서 비밀 변수가 아닌 `page_token=…` 도 기록에서 `•••` 다.
- 내 판단: (T7) 본문 앞 256KB 는 가린 뒤 자른다(경계에 걸친 비밀도 통째로 가려짐). 글자 본문은 룬 경계에서 자르고, 바이너리 본문도 저장한다(JSON 에서 base64). `HistoryEntry`·`RunSummary` JSON 은 snake_case 이고, Task 3 의 `Timing` 은 태그가 없어 필드 이름 그대로(나노초)다.
- 내 판단: (T8) `RunEvent` 에 `Failed`·`Reason`·`Response` 를 더했다. 실패한 줄만 응답을 싣고(C3.7 "실패한 줄 enter 로 그 응답을 연다"), 넘침 파일은 실행기가 지운다. 사유 코드 상수 `ReasonAsserts·Blocked·Private·Redirect·Undefined·Timeout·Extract·Error` (화면이 문구로 바꿈).
- 내 판단: (T8) 요청 하나의 통과 = 오류 없음 + 검사 모두 통과 + 리다이렉트 멈춤 아님. 검사가 없는 요청은 상태 코드와 상관없이 통과(Postman 과 같음).
- 내 판단: (T8) `RunOptions.Validate()` 를 더했다. 반복 수가 1..1000 밖이면 `Start` 는 요청 없이 `Done{Err: ErrIterations}` 하나만 낸다. 실행기는 `Send` 앞에서 `CheckSecretHosts` 를 한 번 더 부른다(주입된 `Send` 가 실제 Sender 가 아니어도 막힘). `PrivateOK` 키는 정규화한 호스트 이름.
- 내 판단: (T8) 보내는 중 취소된 요청은 통과도 실패도 아니다(Summary 에 안 셈), `Done.Err = ctx.Err()`. Done 은 늘 보내고, 취소 뒤 나머지 이벤트는 버린다 — 받는 쪽은 채널이 닫힐 때까지 비워야 한다. `Summary.Total` 은 요청 시간(`Timing.Total`)의 합(대기 제외), `Failures` 는 100개까지.
- 내 판단: (T8) 추출한 비밀 아닌 값은 스냅숏의 Scope(사본)에 들어간다. 라이브 환경에 반영할지는 실행기 화면(Task 17)이 정한다. 비밀은 Vault 를 함께 쓰므로 곧바로 저장된다. `RunHosts` 는 URL 만 펼쳐 호스트를 모으고, 펼칠 수 없는 요청은 빼고 오류를 묶어 돌려준다. 기본 대기는 `time.NewTimer`(엔진은 UI 타이머 규칙 밖).
- 내 판단: (T9) `Suggestion` 에 `RequestID` 를 더했다(컬렉션 가져오기에서 어느 요청인지). `Where` 는 `url`·`query:<이름>`·`header:<이름>`·`auth:pass|token|value`·`body`·`form:<이름>`.
- 내 판단: (T9) curl 은 기본으로 리다이렉트를 따르지 않으므로 `-L` 이 없으면 `NoFollow=true`. 데이터 분류: `-d` 조각 하나가 JSON 이면 json(이때 `Content-Type: application/json` 헤더 한 줄은 본문 종류가 대신하므로 지움), 모든 조각이 `k=v` 로 풀리면 urlencoded, 그 밖은 raw(헤더가 없으면 curl 이 실제로 보내는 `application/x-www-form-urlencoded` 를 ContentType 으로). URL 쿼리는 깨끗하게 풀릴 때 쿼리 행으로 옮긴다(`-G` 데이터도 같은 길). `#`·`=` 없는 조각·잘못된 `%` 가 있으면 URL 에 그대로 둔다.
- 내 판단: (T9) 계획 목록 밖으로 `--form-string`·`--max-time`(`-m`)·`--max-redirs` 와 `$'…'` 따옴표를 읽는다(왕복 때문). `-s` 같은 출력 플래그도 Ignored 에 올린다. `-d @file`·`--data-urlencode name@file`·`-F x=<file`·쿠키 파일 `-b`·`-H @file`·`-H 'Name:'`(curl 기본 헤더 지우기)·두 번째 URL 은 Ignored. 파일은 어떤 경우에도 읽지 않는다.
- 내 판단: (T9) 내보내기(curl·Postman)는 C3.10 4겹을 따라 `RedactRequest` 를 거친다 — 그래서 `-u user:pass`·`-b 'a=1'` 처럼 직접 친 자격 증명은 왕복 뒤 `•••` 가 된다. `TestCurlRoundTrip` 은 계획의 "같은 Request" 를 `RedactRequest(원본)` 과의 비교로 읽었다(스펙 4겹 우선, `{{token}}` 은 그대로).
- 내 판단: (T9) `ToCurl` 은 BodyJSON 에 `Content-Type: application/json`, 형식 없는 raw 에 `text/plain; charset=utf-8` 헤더를 붙여 우리가 보내는 것과 같게 한다. 쿼리·form 값은 `{{ }}`·`$` 등을 남기는 백분율 인코딩, 출력은 한 줄. `CurlNonSecret` 은 비밀 아닌 변수와 동적 변수를 펼치고, 정의 안 된 변수는 그대로 둔다.
- 내 판단: (T10) 문서 순서(경로·태그·예시 키 순서)를 지키려고 순서 있는 JSON 디코더 `decodeJSON`(깊이 64 검사 겸함)과 `encodeJSON` 을 `convert/limits.go` 에 두었다(Task 10 이 Task 9 파일을 고침, Postman 도 깊이 검사에 씀).
- 내 판단: (T10) 환경 이름은 `default`(`OpenAPIEnvName`), 변수 순서 `base_url` → 필요한 것(`token` 비밀, `username`, `password` 비밀), 컬렉션 `Envs` 에 담아 돌려준다(개인 컬렉션이면 화면이 envs 파일로 저장). 서버 변수 기본값을 채우고 끝 `/` 를 지운다. 서버가 없으면 `base_url` 은 빈 값.
- 내 판단: (T10) 필수가 아닌 쿼리·헤더 매개변수는 꺼진 행. `Accept`·`Content-Type`·`Authorization` 헤더 매개변수는 무시(OpenAPI 규정), cookie 매개변수는 Ignored. 예시 순서 example → examples(맵의 value, 3.1 배열) → schema 의 example·examples → default → enum[0]; 본문 예시가 없으면 계획대로 `{}`. `+json` media type 은 BodyJSON + ContentType. form·multipart 본문은 schema properties 를 행으로, binary 형식은 파일 칸(FileName = 필드 이름, 경로 없음).
- 내 판단: (T10) 보안은 첫 요구사항의 첫 방식만 본다. `{}`(선택 인증)이 먼저면 인증 없음, oauth2·openIdConnect → bearer, apiKey cookie·http digest 등은 Ignored `security: <이름> (METHOD path)`. 원격·깨진·순환 `$ref` 는 Ignored `$ref <ref>`(순환은 ` (cycle)`)로 한 번씩. 최상위 `[` 는 YAML 오류가 아니라 `ErrNotOpenAPI`, BOM 은 받는다.
- 내 판단: (T11) `ErrNotPostman` 을 더했다. 스키마 URL 은 `schema.getpostman.com` 과 `schema.postman.com` 둘 다 받는다(새 Postman 이 뒤쪽을 씀).
- 내 판단: (T11) 인증 상속(컬렉션 → 폴더 → 요청)은 가져올 때 풀어 요청마다 넣는다. `noauth` = 없음, 지원 밖 형식(oauth2 등)은 Dropped `auth: <형식> (<이름>)` 후 없음. 비밀 컬렉션 변수(type secret)는 이름·비밀 표시만 남기고, 값이 있었으면 Dropped `variable value: <이름>`(`ImportPostman` 은 Vault 값을 돌려줄 자리가 없음). 꺼진 변수는 Ignored.
- 내 판단: (T11) 경로 변수 `:id` → `{{id}}` + 컬렉션 변수(url.variable 값). graphql 본문 → BodyJSON `{query, variables}`, file 모드 본문은 Dropped. 빈 스크립트는 버린 목록에 넣지 않는다. protocolProfileBehavior(followRedirects·strictSSL·maxRedirects) ↔ Settings. 시험 픽스처의 요청 이름은 영어(계획 예시 "로그인"·"결제 만들기" 대신 "Login"·"Create payment").
- 내 판단: (T11) 환경의 비밀 판정 = type secret 또는 `apitool.SensitiveHeader(이름)`(가리기·탐지기와 같은 낱말 표), 빈 비밀 값은 secretValues 에 넣지 않는다. 내보내기는 검사·추출을 Dropped `asserts`·`extracts` 각 한 번, 타임아웃 `timeout: <이름>`, 공유 환경 `environments` 로 알린다. raw 의 ContentType 은 language(json·javascript·html·xml·text)로, 표에 없으면 text + `Content-Type` 헤더 행.
- 내 판단: (T12) 문구 키는 계획 목록 + 위젯용 `kv_empty`(max 30)·`tree_count`(count, max 12). `need_bigger` 는 en 이 22칸이라 max 24. `status_line` 의 max 는 짧은 형과 긴 형 모두에 걸려 긴 형 기준 60. `host_block` 의 `{{{var}}}` 는 생성기가 `{var}` 만 자리표시자로 보고 바깥 `{{ }}` 는 글자로 남긴다. zh-Hans 는 비움(P5, 경고만).
- 내 판단: (T12) `KVTable` 에 `HandleMsg(msg) bool`(5초 가림 타이머·붙여넣기)을 더했다. 비밀 값은 8자 이상이면 `••••`+끝 4자, 짧으면 `••••` 만. 편집 중인 칸은 친 글자를 보인다. `a` 로 더한 새 줄의 ID 는 비워 둔다 — CA 2.2 의 ui/apitool 허용 목록에 `internal/ids` 가 없어 저장할 때(Task 14·18) 붙인다. `esc` 로 방금 더한 빈 줄을 취소하면 줄도 없어진다. 계획 키 외에 `←→`(열)·`home/end/ctrl+a/ctrl+e/ctrl+u/delete`(편집)를 받고, 줄 머리는 `›` 커서·`✓`/`·` 켬/끔.
- 내 판단: (T12) JSON 나무는 루트 컨테이너를 줄로 보이지 않고 자식부터 깊이 0 으로 그린다. 펼침 `▾ "k": {`, 접힘 `▸ "k": {…} 4개`, 빈 컨테이너 `{}`(접기 없음), 배열 원소 키 `[0]`. 경로는 `apitool.EvalPath` 문법(식별자면 `.k`, 아니면 `['k']`). `Row` 에 `Count`·`Indent` 를 더했고, 개수 문구가 카탈로그라 줄 글자는 `RowText(t, r)` 가 만든다. 문자열 요약은 4KB 에서 자른다. `Window` 는 접힌 것이 없는 부분 나무를 통째로 건너뛰어 화면 위쪽 줄을 만들지 않는다.

**리뷰 반영 (Task 7~12 리뷰 12건)**

- 내 판단: (T7) 기록 본문 앞부분은 이제 **본문 전체(메모리 본문, 최대 `MaxViewBody` 10MB)를 가린 뒤** 256KB 에서 자른다. 앞에서 비밀이 `•••`(9바이트)로 여러 번 줄면 첫 절단점에 걸린 비밀의 앞부분이 남던 것을 고쳤다(`TestRedactHeadSecretOnCutAfterShrink`).
- 내 판단: (T7) apikey 인증이 `In=query` 이고 값이 직접 친 글자면, 그 값도 가릴 값 목록에 넣어 기록의 펼친 URL(`appid=…`)에서도 `•••` 다.
- 내 판단: (T9) `ToCurl` 은 Scope 가 닿는 비밀 변수(환경·컬렉션·전역에서 비밀로 선언된 것)의 Vault 값을 모두 읽어 `RedactRequest` 에 넘긴다 — 그 값이 본문·평범한 헤더에 글자로 들어 있어도 `•••`. 읽지 못하는 값은 건너뛴다. `-` 로 시작하는 URL 은 `--url <u>` 로 쓴다(curl 이 옵션으로 읽지 않게).
- 내 판단: (T9) 왕복 시험은 18개 경우는 원본과 **그대로 같아야** 하고, 12(`-u user:pass`)·18(`-b` 쿠키)만 가린 모양과 비교한다. 계획 Task 9 시험 줄에 적었다. **오너 확인 질문**: 수용 기준 2 "내보낸 curl 을 다시 가져오면 같은 요청" 을 "직접 친 자격 증명은 `•••` 로 내보낸다(C3.10 4겹)" 로 읽는 것이 맞는지.
- 내 판단: (T10) 경로 매개변수 변수는 계획대로 **빈 값**으로 둔다(예시 id 는 보낼 값이 아님, 같은 이름의 다른 예시끼리 첫 값이 이기는 문제도 없앰).
- 내 판단: (T10·T11) 가져온 비밀 아닌 컬렉션 변수(Postman)와 컬렉션·환경 변수(OpenAPI)의 값도 탐지기를 거쳐 `Suggestion{Where: "variable:<이름>"}`(RequestID 없음)으로 알린다. Postman 컬렉션 변수 중 이름이 민감한 것을 비밀로 바꾸지는 않았다 — `ImportPostman` 은 Vault 값을 돌려줄 자리가 없어 값을 버리게 되므로, 제안만 띄워 사용자가 고르게 한다.
- 내 판단: (T11) Postman formdata 의 `type: file` 은 `src` 경로를 **가져오지 않는다**(`File` 빈 값, `FileName` 만). 남이 준 컬렉션이 이 PC 의 아무 파일(예: `~/.ssh/id_rsa`)이나 가리킬 수 있기 때문. 원래 경로는 Dropped `file: <키> (<src>)` 로 알려 사용자가 다시 고르게 한다. curl 의 `-F x=@path` 는 그대로 둔다 — 사용자가 붙여 넣은 명령을 curl 이 실행해도 같은 파일을 읽으므로 뜻이 같다(다른 사람 손을 거친 파일이 아님).
- 내 판단: (T12) `KVTable` 편집 버퍼는 `SanitizeInput` 을 거친 글자로 채우고 그리기·커서 계산은 `Sanitize` 를 거친다 — 가져오기·공유 컬렉션에서 온 ESC·OSC 가 편집을 시작해도 화면에 실리지 않는다(그 칸을 확정하면 정화된 값이 저장된다). 비밀 줄의 값은 **값 칸 자체를 편집할 때만** 보인다(키 편집 중에는 가림 유지).
- 내 판단: (T12) JSON 나무는 중첩 512 단계(`maxTreeDepth`)를 넘으면 `BuildTree` 가 오류 → 화면은 글자 보기. 접기 경로는 모든 컨테이너의 경로 표를 만들지 않고, 접힌 경로마다 뿌리에서 한 단계씩 자식 가운데 찾아 내려가 결과를 기억한다(넓고 깊은 본문에서도 컨테이너마다 문자열을 만들지 않음).

**3묶음 (Task 13~17 — 도구 화면 `internal/ui/apitool`) / 내 판단**

- 내 판단: (T13) 배치 문턱 — 분할 보기는 도구 칸 폭 100·높이 16 이상, 그 아래 탭 보기(`[목록] [요청] [응답]`), 초소형·너무 작음은 앱 모드를 따른다. 나무 폭 26, 기록 최대 8줄, 76칸 미만이면 짧은 상태 줄과 JSON 들여쓰기 1. 분할선은 끌어 옮기고(위·아래 최소 5줄) tools-state 에 기억한다.
- 내 판단: (T13) 인스턴스는 하나(`api:main`), `WideDefault`. 팔레트 명령은 `api.open·api.new·api.import·api.env·api.runner·api.send·api.history.clear` 7개(CA 5.6 P4 줄에 적음), `api.send` 만 도구가 열려 있을 때. 명령은 route query `cmd`(+ 같은 명령을 다시 보내도 갱신되게 `n` nonce)로 인스턴스에 전한다.
- 내 판단: (T13) 첫 화면 골든(7 크기)을 Task 13 에서 최종 모양으로 그렸고 Task 14~17 은 골든을 바꾸지 않았다. 뒤 Task 의 진입점은 임시 `stubs_later.go` 에 두었다가 Task 17 에서 지웠다. **TDD 정직 기록**: Task 13 은 구현을 시험보다 먼저 쓴 부분이 많아, 빨강은 골든 파일 없음으로만 확인했다(Task 14~17 은 시험 먼저 → 빌드 실패 확인 → 구현).
- 내 판단: (T14) 편집기 키 — URL 줄에 포커스가 있을 때 `⏎` 보내기, `u` URL 편집, `m` 메서드 돌리기, `1`~`6` 탭, `h/l` 탭 이동, `j/↓` 탭 안으로. 표 첫 줄에서 `k/↑` 는 URL 줄로. 본문 탭 `⏎` 편집(`esc` 끝)·`b` 종류 돌리기, 설정 탭 `space` 켬/끔·`⏎` 숫자 편집. `n` 새 요청은 곧바로 URL 편집을 연다.
- 내 판단: (T14) curl 붙여넣기는 URL 편집 중이거나 편집 중인 칸이 없을 때만 요청 내용을 통째로 바꾼다(ID·이름 유지). 무시·버림·비밀 제안이 있으면 가져오기 보고 창을 띄운다.
- 내 판단: (T14) 비밀 탐지는 붙여넣은 뒤 그 칸 글자 전체(표 칸·URL·본문)에 `Detect` 를 돌려 **첫 발견 하나만** 묻는다. 기본 이름 jwt·bearer → `token`, basic → `basic_auth`, 이름 규칙 발견 → 필드 이름 소문자+`_`. "변수로 바꾸기" 면 그 부분을 `{{이름}}` 으로 바꾸고 고른 환경(없으면 내 전역)에 비밀 변수로 선언, 허용 호스트 = `DefaultHosts`(펼친 요청 URL·base_url). 값은 Cmd 안에서 `Vault.Put`; `ErrNeedConsent` 면 secretsui 동의 창 → `ConsentMsg` 에서 남은 값을 다시 넣고, 취소면 버린다.
- 내 판단: (T14) 보내기 오류 — 값 없는 변수는 토스트(대상 서버 요청 0), 호스트 막힘 `api.host_block`, 남이 바꾼 요청의 사설 주소 `api.private_target`(허용은 메모리 `privKey = docID/reqID/seq`), 취소 `send_canceled`, 타임아웃 `send_timeout`, 그 밖 `send_failed`(엔진 오류 문구, URL 없음). "이 호스트 허용" 은 그 비밀 변수를 선언한 층(환경 → 컬렉션 → 전역)의 허용 호스트에 더해 저장한 뒤 한 번 다시 보낸다.
- 내 판단: (T14) `ctrl+s` — 저장 안 된 새 요청은 "내 요청" 컬렉션으로(없으면 만듦). 요청·컬렉션 ID 는 `crypto/rand` 16 hex(CA 2.2 에 `ids` 가 없음). 저장은 UI 고루틴에서 바로 한다(작은 JSON 파일). 밖에서 바뀐 컬렉션이면 `widgets.Confirm` 덮어쓰기 확인.
- 내 판단: (T14) 바깥 변경은 `ToolSwitchedMsg`·`FocusChangedMsg` 때 Cmd 안에서 `LocalStore.Changed` 로 보고, 요청 칸 위에 띠 "파일이 바뀌었어요 · 다시 읽기 (R)" — `R` 로 다시 읽는다.
- 내 판단: (T14) 가져오기(`i`, 경로 입력)는 파일 내용으로 형식을 고른다: `curl` 로 시작 → "내 요청" 에 한 요청 추가, `"openapi"` 가 든 JSON → OpenAPI, 그 밖 JSON → Postman 컬렉션, 아니면 Postman 환경, JSON 이 아니면 OpenAPI 로 넘겨 YAML 안내. 같은 이름이 있으면 "이름 2"…(덮어쓰지 않음). OpenAPI 의 환경은 개인 환경 파일로, Postman 환경의 비밀 값은 Vault 로. 보고 창: 가져온 수, 스크립트 수(`prerequest:`·`test:` 접두), 그 밖 버린 것, 무시, 비밀 제안 위치("요청 이름 · header:X").
- 내 판단: (T14) 내보내기(`x`): curl 두 가지(변수 그대로 / 비밀 아닌 변수 채움 — `ToCurl` 이 Vault 를 읽으므로 Cmd 안에서) + 컬렉션을 Postman v2.1 파일로(기본 경로 `~/<컬렉션>.postman_collection.json`, 있으면 덮어쓰고 권한을 0600 으로 맞춤). 저장 안 한 요청의 Postman 내보내기는 `save_first` 토스트. 저장 결과는 경로가 잘리지 않게 창으로 보인다.
- 내 판단: (T14) 큰 응답 넘침 디렉터리 `<Cache>/api` 는 보내기·실행 job 안에서 만든다(`os.MkdirAll` 0700).
- 내 판단: (T14·T17) 창을 닫고 곧바로 다른 창을 여는 경우, 한 Batch 안의 Cmd 순서가 정해져 있지 않아 닫기가 새 창을 닫을 수 있다 — 그래서 같은 창을 제자리에서 바꾼다(`dialog.chain`: 내보내기 → 경로 입력, 실행기 설정 → 공유 호스트 목록 → 진행 표). `tea.Sequence` 는 UI 금지(5.3.2)라 쓰지 않았다.
- 내 판단: (T14) 시험 — 호스트 막힘·사설 문장은 화면 전체가 아니라 맨 위 오버레이 상자 안 글자로 본다(줄바꿈된 문장 사이에 뒤 화면 글자가 끼지 않게). 취소 시험은 서버가 요청을 받은 것을 확인한 뒤 `esc`(그 전에 취소하면 연결이 생기지 않아 "닫힘" 을 볼 수 없음). `TestExportCurlCopies` 의 저장 확인은 창 안 글자(`waitWhole`).
- 내 판단: (T15) 응답 칸 키 — 접을 수 있는 줄에서 `⏎`/`space` 접기·펴기, 그 밖에서 `⏎` 는 보내기. `h`/`l` 접기/펴기, `o` 글자↔나무, `y` 복사(나무 보기면 2칸 들여쓴 JSON, 글자 보기면 받은 그대로 — Cmd 안에서), `w` 파일로 저장, `/` 찾기 + `n`/`N`(대소문자 무시, 끝에서 처음으로 돌아감, 없으면 `find_none` 토스트). 나무 찾기는 노드의 키·값을 훑고 접힌 컨테이너 안이면 그 조상을 편다(`Tree.Find`). 글자 보기 찾기는 본문 바이트에서 찾아 그 조각을 맨 위에 두고 강조한다.
- 내 판단: (T15) 저장 이름 — 따옴표 `filename="…"` 은 역슬래시를 이스케이프로 풀지 않고 그대로 받아 `/`·`\` 로 자른다(`..\x.txt` → `x.txt`; `mime` 은 `\x` 를 `x` 로 풀어 `..x.txt` 가 됨). `filename*` 가 있으면 `mime` 결과. 이름이 없으면 URL 경로 마지막 조각, 그래도 비거나 `.`·`..` 면 `response.bin`. 기본 폴더 `~/Downloads`(없으면 `~`). 넘친 응답은 Spill 파일 **전체를 복사**한다(옮기지 않음 — 두 번째 저장도 되게; 넘침 파일은 응답이 바뀌거나 도구를 닫을 때 지움). 0600, 있는 파일은 덮어쓴다(사용자가 고른 경로).
- 내 판단: (T16) 환경 화면은 도구 위의 모달 오버레이(상태는 Tool 에). 첫 줄 보관 위치 = 이 계정 api 값 첫 이름의 `Where`, 없으면 고른 대체(이번 실행만/파일), 그것도 없으면 "아직 넣은 값이 없어요"(OS 저장소는 부르지 않음). 초소형 50×20 에서도 오버레이가 본문 전체를 써서 보인다.
- 내 판단: (T16) 변수 줄 = 고른 환경의 변수 + 내 전역("전역" 표시). 비밀 값은 화면을 열 때·값을 넣은 뒤 Cmd 안에서 읽어 `••••끝4`(짧으면 `••••`)만 들고 있다. 출처는 Vault 없이 `Lookup` — 개인 환경 층은 "내 값", 공유 환경 표기는 Task 18 에서 `DocID` 가 있을 때.
- 내 판단: (T16) 환경 화면 키: `h/l` 환경 바꾸기(마지막 = 환경 없음, 바꾸면 tools-state 저장 → 제목·허브 줄 갱신), `N` 새 환경, `a` 변수 추가(환경 없으면 전역), `⏎` 값(비밀이면 `secretsui.OverlayPrompt`, `Name` 에 줄 id `e:이름`/`g:이름`), `s` 비밀로(값은 Vault 로, 허용 호스트 기본값), `H` 허용 호스트(쉼표), `d` 지우기(비밀이면 Vault 값도), `C` 쿠키 지우기. 비밀 → 평문 되돌리기는 두지 않았다(값이 파일로 나가는 길).
- 내 판단: (T16) 쿠키 칸은 편집 중 요청의 펼친 URL 로 `Jars.List`(값 그대로 — 내 PC 화면), 디스크에 쓰지 않는다.
- 내 판단: (T16) 설정 "도구 로그인" 줄 — `describe` = `List` 이름 수로 `tool_login_account`("비밀 변수 {n}개", 0 이면 줄 없음), `forget` 은 **nil** 로 등록했다: settings 기본 동작(이 계정의 api 값 전부 `Delete`, Cmd 안)이 계획의 forget 과 같고 완료·오류 처리도 그대로 받기 위해서.
- 내 판단: (T17) 실행기 범위 = 나무에서 고른 폴더/컬렉션, 아니면 편집 중 요청의 컬렉션(저장 안 됐으면 `save_first`). 설정 창의 환경 `h/l` 은 도구의 환경을 바꾼다. 반복 기본 1·대기 0ms(숫자만), `space` 실패 시 멈춤, `⏎` 실행(1~1000 밖이면 토스트).
- 내 판단: (T17) job 은 실행기 채널을 닫힐 때까지 비워 큐에 쌓고, 50ms 폴마다 UI 고루틴에서 쌓인 이벤트를 모두 표로 옮긴다. 기록 `AddRun` 은 채널이 닫힌 뒤 job 안에서. 끝나면 첫 실패 줄을 고른다. 진행 표 `⏎` = 실패 줄의 응답 열기(그 요청 선택 + 응답 칸), `esc` = 실행 중이면 멈춤, 끝났으면 닫기.
- 내 판단: (T17) 공유 컬렉션 표시는 `Tool.shared[컬렉션 이름]` — Task 18 이 채운다(지금 시험은 직접 켬). 호스트 목록에서 "실행" 을 고르면 목록의 호스트 모두 `PrivateOK`.
- 내 판단: (T17) 초소형 `r` 은 컬렉션 전체를 1회, 창 없이 둘째 줄에 `run_progress_short`. 다음 `⏎` 보내기부터 둘째 줄은 다시 응답 상태. "s 카드로 공유" 는 카드 Task 20 몫이라 넣지 않았다.
- 내 판단: (검증) `go test -race` 로 패키지 전체를 돌리면 Task 12 의 `TestJSONTreeWindowOnlyVisible`(BuildTree 2초 상한)가 큰 본문 시험들과 함께 돌 때 3초로 넘친 적이 있다 — 단독 `-race` 와 race 없는 전체 실행은 통과. 시간 상한은 고치지 않았다.

**리뷰 반영 (Task 13~17 리뷰 14건)**

- 내 판단: (T14, 높음) form 줄이 파일인지는 **줄 ID 로** 기억한다(`editor.files`) — 값 글자 `@path` 를 다시 파일로 읽지 않는다. 불러올 때 ID 없는 form 줄(가져오기가 만든 줄)에 ID 를 붙이므로, `@` 로 시작하는 가져온·공유된 글자 값은 늘 글자로 간다. `@path` 가 파일이 되는 것은 원래 파일 줄(경로만 고침)이거나 이 화면에서 `a` 로 더한 줄(아직 ID 없음)뿐. 지운 파일 줄을 `u` 로 되살려도 ID 로 파일임을 안다. (`TestFormTextAtValueSendsText`, `TestFormFileFieldStaysFile`)
- 내 판단: (T17) 실패 줄의 응답은 실행 job 안에서 `newRespState`(나무 색인)까지 만들어 넘기고, `⏎` 는 만들어 둔 상태를 붙이기만 한다. 응답을 남기는 실패 줄은 앞 6개(`runKeep`)·본문 합 20MB(`runKeepBytes`)까지, 그 뒤 줄은 상태·사유만 — `⏎` 하면 `run_resp_not_kept` 토스트. (`TestRunnerFailRowBigResponseIndexedInJob`, `TestRunnerKeepsFewFailedResponses`)
- 내 판단: (T14) URL 칸에 붙여 넣은 비밀의 허용 호스트는 **붙여 넣은 URL**(값이 아직 든 글자, 새 변수는 아직 선언 전이라)로 정한다. 헤더·본문은 그대로 요청 URL.
- 내 판단: (T15) 응답 저장·Postman 내보내기는 `O_EXCL` 로 새 파일만 만들고, 있으면 `file_exists` 확인 창(덮어쓰기/취소) 뒤에만 덮어쓴다(앞 T14·T15 의 "있으면 덮어씀" 을 바꿈). 기본 폴더는 늘 `~/Downloads`(없으면 저장할 때 0700 으로 만듦 — 맨 홈 폴더는 제안하지 않음). 서버가 준 이름의 앞 점은 지운다(`.bashrc` → `bashrc`).
- 내 판단: (T14) 응답에서 추출한 **비밀 변수 값**은 UI 고루틴에서 Vault 에 쓰지 않고 `pendingPuts`(조용히 — 보낼 때마다 토스트 없음)로 `flushPuts` 에 넘긴다 → OS 저장소가 없으면 동의 창, 고르면 다시 씀. 비밀 아닌 추출 값은 그대로 UI 고루틴에서 환경·전역 파일로.
- 내 판단: (T14·T17) 화면에 그리는 메서드는 `methodOf` 가 `textwidth.Sanitize` 를 거친다(URL 줄 두 갈래·실행기 표). 가져오기에서 토큰 검사는 하지 않았다 — `Prepare` 가 보내기 전에 막고(`ErrBadMethod`), 화면은 정화로 충분.
- 내 판단: (T13) 넘침 파일 정리 — 도구 `Init` 이 `<Cache>/api` 의 `masil-api-*.body` 가운데 **1시간 넘은 것**을 Cmd 안에서 지운다. 다른 masil 이 지금 보여 주는 넘침 파일을 지우지 않으려고 나이로 가렸다(종료 훅은 P0 에 없음).
- 내 판단: (T13~16) 편집 표의 열 제목(`col_key·col_value·col_header·col_target·col_check·col_var·col_from·col_field`, max 12)을 카탈로그로 옮기고 그릴 때 `ctx.T` 로 정한다(`KVTable.SetTitles` 추가 — Task 12 의 `KVCols` 모양은 그대로). 첫 화면 골든 3개(200×60·120×40·80×24)의 열 제목 줄이 `header  value` → `헤더  값` 으로 바뀌어 `-args -update` 로 다시 만들었다(이 패키지 골든만, 다른 골든 변화 0). C3.6 시안은 표 상자 제목 "헤더" 만 그렸고 열 제목 줄은 없다 — 열 제목 줄은 Task 12 위젯 모양 그대로 둠.
- 내 판단: (T14·T15) 보내기·저장·내보내기 오류 문장은 `describeErr` — 엔진·파일이 이름 붙일 수 있는 것(`ErrTooManyRedirects`·`ErrTooLarge`·새 `ErrBadMethod`·`ErrBadURL`·`ErrFormFile`·인증서·권한·없음)은 `err_*` 카탈로그 줄, 나머지는 `errtext.Describe(ctx.T, err)`(연결 실패 = "서버에 연결할 수 없어요"). 가져오기의 **변환 오류**(curl·JSON 위치)는 입력 파일 설명이라 그대로 `errText`(파일 없음·권한만 `describeErr`) — P5 Task 6 이 볼 곳은 `describeErr` 하나.
- 내 판단: (T13) 배치 문턱을 계획 숫자에 맞췄다: 도구 칸 폭 `splitMinW = 118`(120칸 창), `narrowW = 78`(80칸 창). 100~119칸 창은 이제 탭 보기. 골든 크기(120·80 경계)는 그대로라 골든 변화 없음. 앞 T13 판단(100·76)을 바꿈.
- 내 판단: (T13) 열기 경로 계약 — 개인 컬렉션 Query `col`·`req`, 공유 컬렉션 `doc`·`req`(인스턴스 `api:doc:<doc_id>`), 팔레트 `cmd`·`n`. `doc` 은 Task 18 이 푼다(상수 `queryDoc` 만 예약, 지금은 마지막 개인 컬렉션). 계획 Task 13 줄·Task 18 시험 목록(`TestOpenSharedFromRoute`)·CA 5.6 P4 줄에 적었다.
- 내 판단: (T17) 실행기 "끝나면 s 카드로 공유" 는 카드 Task 20 의 몫으로 계획에 못 박았다(Task 17 Produces 줄 + Task 20 시험 `TestRunnerShareCard`). 지금 `s` 키 자리는 두지 않았다.
- 내 판단: (CA 2.2) `ui/apitool` 줄에 실제 import `ui/layout`·`errtext`·`features`·`termhost`(ToolSpec.Open 의 Mode 타입)·`theme` 를 더했다(더하기만).

**Task 18~21 (공유 컬렉션 · apicol 검사기 · 결과 카드 · 종합 시험)**

- 내 판단: (T18) `envvar` 에 `order` 수 필드를 더했다 — 환경 변수 순서를 문서에서 되살리려고 코드가 쓰고 읽는다. collab.md 7.1 표에 적었다(계획 Interfaces 의 envvar 필드 목록 + `order`).
- 내 판단: (T18) `HeldField` 에 `Text string` 을 더했다(계획 모양 + 한 칸) — `Finding` 의 위치가 가리키는 글자. "변수로 바꾸기" 가 그 값을 잘라 Vault 에 넣는 데 쓴다.
- 내 판단: (T18) `CheckSharedLimits` 는 본문 `Text` 가 65,536 바이트를 넘거나 **JSON 으로 61,440 바이트(필드 한도)** 를 넘으면 `ErrSharedBodyTooLarge` 로 본다 — 그래서 실제 본문 한도는 필드 한도가 먼저 건다. 설계서 "본문 64KB" 와의 차이는 collab.md 7.2 에 오너 확인 항목으로 적었다.
- 내 판단: (T18) `AssignIDs` 로 비거나 겹치는 id 에 무작위 id 를 붙이고(편집 중 줄은 그 id 로 다시 불러옴), `DocFromCollection` 은 그래도 남은 잘못된·겹친 id 에 sha256 으로 정한 id 를 준다(같은 입력 → 같은 문서).
- 내 판단: (T18) `Gate`: `put` 은 걸린 필드만 빼고 보내고, `set` 은 통째로 보류한다. 값 이름 힌트는 같은 객체의 `key`·`name`(형제 필드) — 줄의 `value` 하나만 바뀐 `set` 은 힌트 없이 패턴만 본다.
- 내 판단: (T18) 키: 방에 올리기 `U`(나무에서 고른 개인 컬렉션, 아니면 편집 중인 것), 버전 목록 `v`(나무 — `V` 는 KVTable 비밀 보기가 씀). 덮어써진 본문은 띠 대신 확인 창 [내 내용 복구 / 취소] — 고르면 내 본문이 새 편집으로 올라간다.
- 내 판단: (T18) 보류 창은 첫 보류 필드 하나만 묻고, 답을 기다리는 동안 새로 걸린 값은 조용히 보류만 한다. "취소"(여기에만 두기)를 고른 값은 기억해 다시 묻지도 보내지도 않는다. "변수로 바꾸기" 는 컬렉션 변수(비밀)로 선언하고 허용 호스트 = 지금 요청 URL 의 호스트(.mine.json).
- 내 판단: (T18) 공유 컬렉션의 환경은 문서 환경의 **복사본**을 `t.env` 로 쓴다(별칭이면 Diff 가 바뀐 것을 못 봄). 비밀 표시는 문서 `secret` 이 꺼져도 `.mine.json` 의 내 표시가 있으면 비밀(OR) — 값은 지우고 내 Vault 값·허용 호스트를 쓴다.
- 내 판단: (T18) 올리기는 지금 고른 개인 환경을 공유 환경으로 붙여 올리고, 그 환경의 내 비밀 값을 문서 칸(`SecretKey(doc, envID, name)`)으로 복사·`.mine.json` 에 표시·허용 호스트를 옮긴다(서버엔 값 0).
- 내 판단: (T18) 보낸 op 는 바로 내 상태에 적용하고(낙관), 세션 이벤트로 따라잡는다. 동료가 지금 편집 중인 요청을 바꾸면 입력 중이 아닐 때만 편집기를 다시 불러온다. 오프라인 사본은 주 인스턴스가 열릴 때(`ListShared`) 모두 연다.
- 내 판단: (T18) Task 18 Files 밖에 고친 파일: `ui/apitool` 의 tool.go·actions.go·envview.go·editor.go·tree.go·job.go·prompts.go(공유 갈래 연결만). 엔진 쪽에도 `TestPeerUnsetsSecretFlagKeepsMyValueLocal` 을 하나 더 두었다(UI 시험과 같은 이름, 패키지 다름).
- 내 판단: (T18 시험) 앱이 `tab` 을 창 전환에 쓰므로 표의 값 칸으로는 `right` 로 옮긴다. 쫓겨남 토스트는 상자에서 줄바꿈되어 이름 앞까지 맞춰 본다.
- 내 판단: (T19) `secret_value` 를 `col.vars` 원소(`secret:true` + 비지 않은 `value`)에도 적용했다 — collab.md 7.2 행에 더함. 모든 필드는 `null`(필드 지우기 — 클라이언트 Diff 가 씀)을 받고, `body_kind` 는 `""` 도 받는다(Go 가 빈 종류를 그렇게 씀).
- 내 판단: (T19) `limit_requests` 는 `validateOp` 가 먼저 낸다(공통 `limitReason` 은 `limit_request` 를 냄). `maxObjects = { request: 1000, col: 1 }`.
- 내 판단: (T19) P0 시험 두 곳을 최소로 맞췄다: `collab-hub.test.ts` "~1MB apicol" 의 id `r${i}` → `req:r${i}`, `collab-reducer.test.ts` "apicol 2MB" 는 채움 객체가 apicol 객체가 아니라 공통 한도만 보는 spec(`validateOp` 끔)으로. 시험 의도(1MB 스냅숏 한 프레임·2MB 경계)는 그대로.
- 내 판단: (T19 시험) 65,537 바이트 본문은 클라이언트 프레임(65,536)에 못 들어가므로 `applyFrame`/`validateOp` 로 직접 보고, 소켓으로는 62,000 바이트 본문이 공통 필드 한도 `too_large` 에 걸림을 본다. 만들기 스냅숏 경로는 REST 가 이유를 숨기고 400 `snapshot: invalid`. 프레임당 op 200개 한도라 1,000 요청은 다섯 프레임.
- 내 판단: (T20) 카드 렌더러는 v≠1·읽을 수 없는 data·플래그 꺼짐이면 `body` 를 그린다(옛 클라이언트와 같게). `⏎` 열기는 "카드가 올라온 채널이 내가 속한 방의 채널" 일 때만(DM·모르는 채널 = 내용만), 그리고 `doc_id` 가 있을 때만. 카드 data 에 문서의 방 id 가 없어서, 다른 방 문서를 가리키는 카드는 열기를 시도하고 서버가 거절한다.
- 내 판단: (T20) 미리보기 창: 채널(내 방 순서의 텍스트 채널, 그다음 DM) → 선택지 3개(호스트 보이기·요청 헤더·본문 앞 20줄, 기본 끔) → 취소. 선택지를 고르면 data 를 다시 만들어 창을 제자리에서 바꾼다(`dialog.chain`). 미리보기 줄은 등록 렌더러가 보낼 바이트 그대로 그린 것 — `dialog` 에 그대로 그리는 `pre` 줄을 더했다.
- 내 판단: (T20) 대체 본문의 경로는 `ResultPath(p)`(보낸 URL 의 경로, 비밀 가림)를 엔진에 더해 쓴다. 16KB 맞춤은 본문 줄부터, 다음 헤더를 줄이고, 그래도 넘으면 `ErrCardTooLarge`. `url` 은 요청 URL 템플릿(쿼리 표 줄은 넣지 않음).
- 내 판단: (T20) 서버 스키마는 모르는 키를 거절(strict). 길이는 계약 것만(헤더 ≤20, 본문 ≤20줄·줄당 ≤200 **코드 포인트** — Go 가 rune 으로 자르므로 UTF-16 길이가 아님), 나머지 문자열은 16KB 총량 검사에 맡긴다. P0 `cards.test.ts` 의 자리표시 data(`{status:200}`·`{ok:3}`)를 유효한 data 로 바꿨다.
- 내 판단: (T20) 실행기 끝 안내 `run_done_hint` 에 "s 카드로 공유" 를 더했다(골든에 없는 줄). 실행 중 `s` 는 무시.
- 내 판단: (T21) 종합 시험은 설정 폴더(`Dir`)·캐시·`LogDir` 전체 파일, masil 시험 서버 기록(경로·헤더·본문), 모든 문서의 Hub op·스냅숏, 클립보드(curl 두 가지), Postman 파일을 본다. 대상 서버가 받은 `Authorization` 에는 보내기·실행기·공유 보내기로 3회 이상. 가림을 하나 빼면(카드 본문) 이 시험이 잡는 것을 확인했다.

**Task 18~21 리뷰 (P4 review)**

- 내 판단: (리뷰 1·8) `Gate(ops)` 는 계획 모양 그대로 두고 `GateIn(base *Collection, ops)` 를 더했다 — `set` 의 `value` 이름은 같은 묶음의 `put`/`set key·name`, 없으면 `base`(지금 공유 상태)의 줄 `key`·변수 `name`. `url` 값은 쿼리 매개변수마다 그 이름으로 본다. 편집 경로(`submit`·변수로 바꾸기 뒤 재검사)는 `GateIn(sd.col, …)`. 앞 T18 판단("value 하나만 바뀐 set 은 힌트 없이")을 바꿈. "취소" 뒤 다음 편집이 값을 다시 `set` 해도 이름 힌트로 다시 걸리고, 기억한 "여기에만 두기" 라 올라가지 않는다.
- 내 판단: (리뷰 11) 요청 `auth` 의 손으로 친 `pass`·`token`·`value`(자리표시만이 아닌 것, `literalCredential` — `RedactRequest` 와 같은 규칙)는 이름과 상관없이 보류한다. 탐지 규칙에 걸리면 그 범위, 아니면 값 전체(규칙 이름 `literal_credential`, 변수 이름 제안 `secret`).
- 내 판단: (리뷰 9) `CollectionFromDoc` 의 폴더 부모는 한 번만 읽고 사슬마다 한 번 걷는다(기억, 선형). 서버 `folder` 개수 한도는 더하지 않았다 — 설계서 C3.8·collab.md 7.2 에 없는 새 한도라 오너 결정 몫이고, 선형이 되어 2MB 문서(약 2.4만 폴더)도 수십 ms 다.
- 내 판단: (리뷰 3) 결과 카드 대체 본문의 경로는 카드 `url` 과 같은 출처: 기본은 템플릿 경로(`{{base_url}}` 앞머리·호스트·쿼리 뺌, `/tenants/{{tenant}}/…`), 호스트 보이기일 때만 비밀 아닌 변수를 채운 경로. `ResultPath(r, p, o)` 로 모양을 바꿨다(앞 T20 판단의 "보낸 URL 의 경로" 를 바꿈).
- 내 판단: (리뷰 12) 카드는 `History` 처럼 요청 auth 의 손으로 친 비밀번호·토큰·키, 그리고 api-key 가 실려 나간 헤더 값을 가린다(`cardSecrets`).
- 내 판단: (리뷰 13) "변수로 바꾸기" 는 첫 보류 값과 같은 글자만 `{{이름}}` 으로 바꾸고, 바꾼 칸을 다시 `GateIn` 에 넣어 남은 다른 비밀과 나머지 보류 칸을 이어서 묻는다(값마다 변수 하나).
- 내 판단: (리뷰 2) 방에 올리기의 보류 창도 편집 경로와 같은 선택지 [변수로 바꾸기 / 그래도 올리기 / 취소] 로, 보류 칸마다 차례로 묻는다. 변수로 바꾸기 = 스냅숏의 그 값(같은 글자 전부)을 `{{이름}}` 으로 + `col.vars` 에 비밀 변수 + 새 문서 칸으로 Vault 저장·`.mine.json` 표시(허용 호스트 = 그 요청 URL 의 호스트). 그래도 올리기 = 그 칸만 허락. 취소 = 올리기 전체를 멈춤(아무것도 안 올라감) — 편집 경로의 취소(여기에만 두기)와 달리 올리기에는 "이 칸만 빼고 올리기" 를 두지 않았다.
- 내 판단: (리뷰 10) 사설 주소 확인의 "내가 아님" 을 넓혔다: 동료가 변수(`envvar:`·`env:`·`col` 의 `vars`)를 바꾸면 그 문서의 모든 요청을 그 사람 것으로, 지은이를 모르는 변경(첫 스냅숏·다시 잇기 스냅숏, 오프라인 사본을 열 때, 카드로 연 문서)은 `Unknown` 으로 표시한다. 질문 문장은 새 카탈로그 줄 `private_target_unknown`. 내가 방금 올린 문서는 첫 스냅숏을 내 것으로 본다(`ownSnapshot`). "마지막 변경" 줄은 요청을 직접 바꾼 경우에만. `TestOfflineSendWorks` 는 이제 127.0.0.1 로 보내기 전에 이 질문에 답한다.
- 내 판단: (리뷰 4) `TestPeerUnsetsSecretFlagKeepsMyValueLocal`(UI) 은 문서 보기에서 `envvar:v2` 가 `secret=false`·`value "x"` 가 된 뒤에 보고, 도구가 다시 만든 환경의 token 이 비밀·내 허용 호스트인지, 호스트 검사는 `tl.scope()` 에서 base_url 만 바꿔서 본다. `ApplyMine` 의 표시를 끄면 이 시험이 잡는 것을 확인했다.
- 내 판단: (리뷰 5) CA 2.2 `apitool` 줄 밑에 `collab` 에서 쓰는 것(타입 + 순수 함수 `NewDoc(…).Snapshot()`·`IsName`)을 한 줄 더했다(더하기만).
- 내 판단: (리뷰 6) collab.md 7.1 에 "모든 필드 값은 `null` 가능(비우기)·`body_kind` 는 `""` 도" 를 한 줄 더했다.
- 내 판단: (리뷰 7) P0 소유 서버 시험 세 파일(`server/test/unit/collab-reducer.test.ts`·`server/test/realtime/collab-hub.test.ts`·`server/test/api/cards.test.ts`)의 T19·T20 수정은 그대로 두고 **머지 교차 확인 때 P0 오너 확인 항목**으로 남긴다(색인 파일은 P4 소유가 아니라 고치지 않음). 특히 reducer 시험의 apicol 2MB 경계는 `validateOp` 를 끈 spec 으로 공통 한도만 본다 — 채움 객체를 유효한 `req:` 객체로 바꾸려면 P0 의 `sized()` 도우미까지 고쳐야 해서 하지 않았다.

(P4: 138줄)

