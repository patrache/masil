# P5 1묶음·문서 줄기·보안 (af) 통합 기록

- 날짜: 2026-11-10. 워크트리 `.claude/worktrees/dev-v013`, 브랜치 `dev/v0.1.3`, 기준 `a393a84e`(보안 a~ae 통합 기록).
- 색인 3절 ④ 단계: P5 1묶음(Task 5~10)과 Task 20~22. 그리고 보안 후속 (af).
- push·tag·배포·원격 wrangler·유료 리소스: 없음. 브랜치·워크트리는 지우지 않았다.

## 병합 (`--no-ff`, 이 순서)

| 순서 | 브랜치 | 줄기 커밋 | 병합 커밋 | 충돌 |
|---|---|---|---|---|
| 1 | `feat/v013-p5-b1` (P5 Task 5~10 + 검토 반영) | `0b8c94c0`…`428a1885` (13개) | `e0a1ad3e` | 없음 |
| 2 | `feat/v013-p5-docs` (P5 Task 20~22 + 검토 반영) | `131d1133`…`6633c830` (5개) | `9e7f13e0` | 2개 (아래) |
| — | 병합 뒤 고침 | — | `973097aa` | — |
| 3 | `feat/v013-sec-af` (보안 (af) + 검토 반영) | `986d0f65`·`2af6c19c`·`9afe38a5` | `5e51d51f` | 없음 |

### 충돌과 해결

- `internal/i18n/legacy_literals.txt` (p5-b1 ↔ p5-docs): 두 쪽이 낮추거나 지운 것을 모두 남겼다. p5-b1 은 `internal/ui/authui` 줄을 지웠다. p5-docs 는 `internal/ui/chat` 를 574 에서 573 으로 낮췄다. 그래서 authui 줄은 없고 chat 은 573 이다. 래칫 시험(`./internal/i18n`)은 통과했다.
- `docs/superpowers/plans/exec-log/P5.md` (p5-b1 ↔ p5-docs): 둘 다 파일 끝에 자기 절을 덧붙인 것이다. 두 절을 모두 남기고, 먼저 병합한 p5-b1 의 "1묶음" 절을 앞에 두었다.
- 생성 i18n 파일·골든: 충돌 없음. 생성 파일은 p5-b1 쪽에서만 바뀌었으므로 다시 생성하지 않았다.

### 병합 뒤 고친 것

- `973097aa` `fix(cli): one hasHangul helper after the p5-b1/p5-docs merge`.
  - 원인: p5-b1 의 `cmd/masil/cli_locale_test.go` 와 p5-docs 의 `cmd/masil/readme_parity_test.go` 가 둘 다 `hasHangul` 을 선언했다. 그래서 `cmd/masil` 시험 바이너리가 컴파일되지 않았다.
  - 실패 확인: `go vet ./cmd/masil` 이 `readme_parity_test.go:197:6: hasHangul redeclared in this block` 으로 실패했다.
  - 고침: 뜻이 같으므로 p5-docs 쪽 사본과 남는 `unicode` import 를 지우고, p5-b1 사본을 남겼다. 고친 뒤 `go test -count=1 ./cmd/masil` 은 ok 였다.
- `security-fixes.md`: sec-af 오너 확인 4번이 병합 때 갱신하라고 한 것을 이 커밋에서 갱신했다.
  - (af) 행: "열림 — 일부만 완화" 를 "고침(지금 쓰는 Ignorer 가 보는 출처 범위)" 으로 바꾸고 커밋을 적었다.
  - 오너 확인 1번(af/v)과 3번(같은 내용 링크 교체): 고쳤음으로 줄을 긋고, 남은 결정은 `security-fixes-af.md` 로 넘겼다.

## 검증 (병합된 트리, 이 세션, `5e51d51f` 위. 기록 문서만 더한 이 커밋 전)

- `go build ./...` → `BUILD_OK`
- `go test -count=1 ./...` → `TEST_EXIT=0`. 패키지 84개 ok, `ok`·`no test files` 밖의 줄 없음.
- `go vet ./...` → `VET_OK`. `GOOS=windows go vet ./...` → `VET_WIN_OK`. `GOOS=linux go vet ./...` → `VET_LINUX_OK`.
- `gofmt -l` (a393a84e 이후 바뀐 .go 파일 전부) → 출력 없음.
- `git status --porcelain -- '*.golden'` 과 `git diff --stat a393a84e HEAD -- '*.golden'` → 둘 다 출력 없음. 골든은 하나도 바뀌지 않았다.
- server/ 가 바뀌었다(p5-docs Task 20). 그래서 go test 가 끝난 뒤 따로 돌렸다.
  - `npm run typecheck` → exit 0
  - `npm test` → `Test Files 94 passed (94)`, `Tests 896 passed (896)`, exit 0

```
== go build
BUILD_OK
== go test
TEST_EXIT=0
== vet
VET_OK
VET_WIN_OK
VET_LINUX_OK
== gofmt
GOFMT_DONE
== golden
GOLDEN_DONE
TYPECHECK_EXIT=0
 Test Files  94 passed (94)
      Tests  896 passed (896)
NPMTEST_EXIT=0
```

## 통합하며 내 판단

- 내 판단: `hasHangul` 이 겹친 것은 병합에서 생긴 컴파일 오류다. 그래서 병합 커밋을 고치지 않고 따로 커밋(`973097aa`)했다. 남긴 쪽은 먼저 병합한 p5-b1 사본이다.
- 내 판단: sec-af 줄기 보고의 검토 반영 칸은 "시험 재실행을 기다리는 중" 으로 끝나 있었다. 그 결과를 줄기에서 받지 못했다. 그래서 검토 반영 커밋(`2af6c19c`·`9afe38a5`)은 병합된 트리의 전체 검증(위)으로만 확인했다.
- 내 판단: p5-docs 와 p5-b1 이 색인 2절 범위 밖에서 계약 문서를 고친 것(아래 오너 확인)은 되돌리지 않았다. 색인 표도 넓히지 않았다. 둘 다 P0·오너 몫이기 때문이다.

## 줄기별 내 판단 (요약. 전문은 각 실행 기록)

### p5-b1 (Task 5~10) — `P5.md` "1묶음" 절

- Task 5
  - ko `Relative` 는 0.1.2 그대로라 올해 9칸, 지난해 13칸까지 간다. en·zh 는 지난해까지 8칸이다. 이를 위해 `time.relative_past_year` 를 새로 두었다.
  - en 의 어제는 "1d" 다.
  - 달·요일은 별도 키로 두었다. 세는 단위의 문턱값은 코드에, 단위 글자는 카탈로그 `count_tier1..3` 에 두었다.
- Task 6
  - en 은 "아이디" 를 "Username" 으로 옮겼다.
  - zh-Hans 의 errors·chat·auth·settings·cli·daemon 은 비웠다.
  - nil 안전한 `tOf(ctx)` 를 chat·feed·widgets 에 하나씩 두었다.
  - admincli 는 `errtext.Describe(i18n.KoT(), …)` 로 쓴다.
  - 서버 칸 `reason` 은 인자 없는 키만 찾게 했다(`plainKeys`).
- Task 7: 슬래시 오류를 타입 있는 오류로 바꿨다. `Error()` 는 ko 를 돌려준다.
- Task 8·9
  - 문구는 `func(*i18n.T) string` 으로 담아 그릴 때 렌더한다. 설정 화면의 오류 줄도 같다.
  - `navW` 22 는 그대로 두고 절 이름 키에 `max = 20` 을 걸었다.
  - `Theme.Label` 필드는 지웠다.
  - `pseudo_pending.txt` 의 `pane/main/settings` 는 Task 19 까지 남긴다.
- Task 10
  - `cmd/masil` 래칫은 4 다(admin 경로).
  - `Env.T` 가 nil 이면 ko 다.
  - `Status.ExeUnknown` 을 더했다. 이때 0.1.2 ko 출력과 바이트까지 같다.
  - 계획 Files 밖인 `cmd_workspace.go` 도 고쳤다.
  - 계약 2.4·5.8·9.2 를 고쳤다. 색인상 P5 범위 밖이다.

### p5-docs (Task 20~22) — `P5.md` "Task 20~22" 절

- Task 20
  - 기본 채널은 `createTextChannel` 이 아니다. 기존 batch 를 그대로 두고 이름만 `defaultChannelName(langOf(Accept-Language))` 로 정한다. 이벤트·시스템 메시지가 더 생기지 않게 하려는 것이다.
  - 서버와 Go 가 같은 입력표 `server/test/fixtures/channel-names.json` 을 공유한다.
  - api.md 0.1·3.5·5.7 과 data-model `channels.name` 주석을 함께 고쳤다.
  - `badChannelName` 안내는 그대로 두었다.
- Task 21
  - 두 README 모두 `masil join <invite-code>` 로 맞췄다.
  - 두 README 에 Language 절을 두었다. 앱 안 경로 줄은 "언어가 둘 이상 열리면" 이라는 조건을 붙였다.
  - 영어판에만 "화면은 한국어" 머리말과 `MASIL_SERVER` 안내를 두었다.
  - 영어 `daemon status` 예는 예상한 모양이다.
  - Homebrew caveats 는 영어·한국어 쌍이다.
  - `TestReadmeParityParser` 와 PR 템플릿 체크리스트를 더했다.
- Task 22
  - 설치 스크립트 언어는 `LC_ALL` 다음 `LANG` 순으로 정한다(`LC_MESSAGES` 는 보지 않음). ps1 은 UI culture 를 본다. `MASIL_INSTALL_LANG` 가 먼저다.
  - `TestInstallScriptMessagesPaired` 를 더했다.
  - ps1 은 정적 시험만 돌렸다.

### sec-af — `security-fixes-af.md`

- 출처를 추적하지 않고 매 DiskPoll 재검사만 택했다. `CheckBatch` 로 git 을 폴링마다 한 번 부르고, 실패하면 닫는다(fail closed).
- 비용 상한: 참가자가 연 문서가 있을 때만 폴링마다 git 1회, 경로 최대 20개(링크면 40).
- 재검사는 `h.mu` 안에서 동기로 돈다. git 이 멈추면 최대 2초 멈춘다.
- 묶음이 128 로 거부되면 빈 탐침을 한 번 보낸 뒤 반씩 나눠 다시 묻는다. 최대 2n+1 회다.
- 시간 초과는 나누지 않고 내장 해석기로 넘긴다.
- 새 i18n 키는 없다.

## 오너 확인 (줄기 기록에서 모음)

1. **(P5 Task 5) ko `Relative` 너비**: ko 에는 8칸 예산을 걸지 않았다. 올해는 9칸, 지난해는 13칸까지다(0.1.2 값 그대로). 이대로 좋은지 정해 주세요.
2. **(P5 Task 6·5) 번역 검수**: en "1d", "Username", 날짜 순서 "11/30/25" 를 색인 4절 16번 검수 때 봐 주세요.
3. **(P5 Task 10) Task 28 관문**: 래칫 파일에 `cmd/masil 4`(admin 경로)가 남는다. 둘 중 하나를 정해 주세요.
   - `cmd_admin.go` 의 네 줄을 admincli 로 옮긴다. admincli 허용 수가 늘어난다.
   - 관문의 예외로 둔다.
4. **(P5 Task 6·10) 계약 문서가 색인 2절 P5 범위 밖에서 바뀌었다(P0 확인 필요)**:
   - p5-b1: `client-architecture.md` 2.4·5.8·9.2
   - p5-docs: `api.md` 0.1·3.5·5.7, `data-model.md` 의 `channels.name` 주석
   - 받아들인다면 색인 2절 행을 넓혀야 하고, 아니면 P0 후속 커밋으로 옮겨야 한다. 이번 통합에서는 둘 다 하지 않았다.
5. **(P5 Task 20) `badChannelName` 안내에 한자를 넣을지**: 넣으면 한국어 화면 문구가 바뀐다.
6. **(P5 Task 21) 영어 README 검수**: 모드·탭 이름을 봐 주세요. Task 28 에서 en 을 열 때 "Korean only" 문장을 고쳐야 한다.
7. **(P5 Task 22) install.ps1**: 실제 Windows 에서 두 언어로 한 번 돌려 봐야 한다.
8. **(af) 1 — `.git` 없는 저장소 하위 폴더를 공유할 때**: 상위 저장소의 규칙이 처음부터 적용되지 않는다. 상위 work tree 를 감지해 git 을 쓸지 정해 주세요. 쓰면 `$HOME` 이 dotfiles 저장소인 사람은 참가자에게 아무것도 안 보이게 될 수 있다.
9. **(af) 2 — git 실패 때 대체 해석기**: 시간 초과나 저장소 자체가 거부될 때, 그 폴링 동안은 fail-open 이다.
10. **(af) 3 — 매 폴링 동기 git**: 그 비용과 최대 2초 멈춤을 받아들여도 될지.
