# P0 wave 4 통합 기록

- 통합 브랜치: `dev/v0.1.3` (워크트리 `.claude/worktrees/dev-v013`), 2026-11-09. 기준 `58376da`(wave 3 끝).
- push·tag·배포·원격 wrangler 없음. 작업 브랜치·워크트리는 지우지 않았다.

## 병합한 것 (순서대로, 모두 `--no-ff`)

| 브랜치 | 머지 커밋 | 브랜치 끝 | 들어온 커밋 수 | Task |
|---|---|---|---|---|
| `feat/v013-p0-secrets` | `41daad5` | `e821ebd` | 7 | 21·22·23 + 리뷰 반영 4개 |
| `feat/v013-p0-termpane` | `a331b1b` | `fd091bc` | 12 | 16·16a·20 + 리뷰 반영 9개 |
| `feat/v013-p0-collab` | `8234556` | `68fb0a7` | 4 | 29·30 + 리뷰 반영 2개 |

## 충돌과 해결

- secrets: 충돌 없음.
- termpane: 5개 파일 충돌. 모두 양쪽이 줄을 덧붙이기만 한 자리라 둘 다 남겼다(index 2절: `internal/ui/app/*` 는 P0 소유).
  - `app/model.go`: `Options.Secrets`(secrets) + `Options.NestedPane`(termpane).
  - `app/run.go`: import `secrets` + `ui`, `openSecrets`(secrets) + `nestedPaneWarning`(termpane) 두 함수.
  - `app/export_test.go`: secrets 의 import `testing`(`SetLogoutTimeoutForTest`) 유지.
  - `uitest/harness.go`: `Secrets: sec` + `NestedPane: c.nestedPane`.
  - `uitest/options.go`: config 에 `secrets`·`nestedPane` 둘 다, `WithSecrets` 유지.
  - 자동 병합: `app/commands.go`(로그아웃 ConfirmCheck + 끌 때 확인).
- collab: 2개 파일 충돌, 역시 덧붙이기만.
  - `internal/ui/context.go`: `Context.Secrets` + `Context.CollabDial`.
  - `uitest/options.go`: config 에 `collab` 더함, `WithNestedPane` + `WithCollab` 둘 다.
  - 자동 병합: `uitest/harness.go`(WithCollab 배선은 `app.New` 뒤).
- i18n 생성물: 병합마다 `go generate ./internal/i18n` 을 다시 돌려 `git status` 가 깨끗함을 확인(손으로 합친 생성 파일 없음). 영역이 갈려 있어(secrets = `tools.toml`, termpane = `termpane.toml`) 생성물 충돌도 없었다. 경고는 zh-Hans 누락뿐.
- 골든: `git diff --name-status 58376da HEAD -- '*.golden'` 에서 새 골든 6개(`internal/ui/termpane/testdata/TestPaneGolden/{200x60,120x40,80x24,50x20,40x14,shared_120x40}/pane.golden`), 바뀐 골든 2개:
  - `internal/ui/tools/testdata/TestHubGolden/120x40/{hub_open,open_menu}.golden` — wave 3(Task 19)이 만든 0.1.3 골든이고 기능 플래그를 켜야만 보인다. 0.1.2 골든은 하나도 바뀌지 않았다.
  - 차이는 마지막 상태줄 한 줄에 ` 💬 안 읽음 3 · ` 이 더해진 것뿐(Task 20, 설계서 A8.3 "표준 모드 상태줄에 안 읽은 수", termpane 리뷰 `0abf8ee`). 의도된 변화로 받았다.
  - 병합 뒤 `TestHubGolden`·`TestPaneGolden` 을 `-args -update` 로 다시 만들어도 `git status` 가 깨끗했다(손으로 고친 골든 없음).

## 통합 수정

- 없음. 코드 수정 커밋은 없다.
- 첫 전체 `go test -count=1 ./...` 에서 `internal/termhost` `TestCloseEscalates` 가 한 번 실패했다(`Code:137, want -1`). 부하 평균 약 100 이었다.
  - `internal/termhost` 는 이 wave 에서 바뀐 파일이 0개다. 그래서 통합 고장이 아니라 원래 있던 시험의 부하 흔들림으로 봤다.
  - `-count=10`(그 시험만), `-count=3`(패키지 전체) 다시 돌려 모두 통과했고, 전체 시험 재실행도 통과했다.

## 다음 wave 가 쓸 수 있는 인터페이스

비밀값 (Task 21~23)
- `secrets.Secret`: `New(s)`, `Reveal()`, `Masked()`, `IsZero()`. `String`·`GoString`·`Format`·`MarshalJSON`·`MarshalText`·`LogValue` 는 `"(redacted)"`.
- `secrets.Key{Tool, Server, UserID, Name}`, `Key.Account()` = `"<tool>/<host>/<user>/<name>"`, `secrets.Service = "masil"`.
- `secrets.Backend`: `BackendKeychain`·`BackendCredMan`·`BackendSecretService`·`BackendFile`·`BackendMemory`. `secrets.Fallback`: `FallbackUnset`·`FallbackFile`·`FallbackSession`.
- 오류: `secrets.ErrNotFound`·`ErrNeedConsent`·`ErrUnavailable`. ctx 가 끝난 뒤의 실패는 ctx 의 오류로 돌아온다(OS 를 막지 않음).
- `secrets.OSBackend{Name; Set; Get; Delete}`, `secrets.Options{OS, Paths}`, `secrets.Open(o) *Store`(I/O 없음).
- `*Store`: `Put(ctx, k, v) (Backend, error)`, `Get(ctx, k) (Secret, Backend, error)`, `Delete(ctx, k) error`, `List(ctx, tool, server, userID) ([]string, error)`, `DeleteAll(ctx, server, userID) error`, `SetFallback(f)`, `Fallback()`, `Where(k) Backend`. `secrets.ValuesForTest(s)`.
- `secrettest.NewStore(tb, osAvailable bool)`, `secrettest.Values(s)`, `secrettest.AssertNoLeak(tb, srv *apitest.Server, logDir string, values...)`, `secrettest.AssertNoLeakFrames(tb, frames [][]byte, values...)`.
- `ui.Context.Secrets *secrets.Store`(nil 아님), `(*ui.Context).SecretScope() (server, userID string, ok bool)`, `(*ui.Context).SecretKey(tool, name) (secrets.Key, bool)` — 키는 언제나 `ctx.SecretKey` 로.
- `uitest.WithSecrets(s)`, `app.Options.Secrets`.
- `secretsui.OverlayConsent = "secrets.consent"`(args = 도구 ID), `secretsui.OverlayPrompt = "secrets.prompt"`(args = JSON `{tool, name, label}`), `secretsui.ConsentMsg{Tool, Choice}`, `secretsui.PromptMsg{Tool, Name, Value, Canceled}`, `secretsui.BackendLabel(t, b)`.
- `settings.RegisterToolLogin(tool, describe func(*ui.Context) (string, bool), forget func(*ui.Context) tea.Cmd)`, `settings.ResetToolLoginsForTest() (restore func())`.
- `widgets.ConfirmCheck(title, body, check, yes, no string, onYes func(checked bool) tea.Cmd) ui.Overlay`, `apitest.Recorded.Header`.
- `cmd/masil/importgraph_test.go` 의 `toolPackagePrefixes`: 각 줄기가 자기 접두사 한 줄을 더한다.

터미널 패널 (Task 16·16a·20)
- `ui.KeyGrabber{GrabsKeys() bool}`, `ui.StatusTexter{StatusText(ctx) (string, bool)}`.
- `ui.ShellAction`(`ShellFocusLeft`·`ShellFocusDown`·`ShellFocusUp`·`ShellFocusRight`·`ShellPalette`), `ui.ShellActionMsg{Action}`.
- `ui.KillSwitch{Active func() bool; Kill func(ctx *Context) tea.Cmd}`, `ui.RegisterKillSwitch(k)`(nil 이면 panic), `ui.KillSwitchActive()`, `ui.KillAll(ctx)`, `Registry.KillSwitches`.
- `keymap.TermhostEscape = "termhost.escape"`(기본 `ctrl+\`·`ctrl+4`), `(*keymap.Keymap).MatchesEscape(msg)`(0x1c 도 받음). 전역 키와 겹치면 `Load` 가 거절하고 기본으로 되돌림.
- `termpane.Handle`, `termpane.Options{ToolID, InstanceID; SingleCell; OnPrefix; OnExit}`(OnExit 은 pane 모드만), `termpane.New(h, opts) *Pane`, `(*Pane).Handle()`·`Exited()`·`GrabsKeys()`·`SetCellFocus(bool)`.
- `termpane.DirtyMsg{InstanceID}`, `termpane.ExitedMsg{InstanceID; Info}`(모든 모드에서 끝을 알리는 길), `termpane.CwdMsg{InstanceID, Dir}`, `termpane.NotifyMsg{InstanceID, Kind}`.
- 접두키: `termpane.PrefixAction`(`PrefixNone` … `PrefixCancel`, `PrefixArm`), `PrefixResult{Action, Dir, Key, Cmd}`, `PrefixTimeout = 2s`, `NewPrefix(extra []string)`(예약 키는 panic), `Active()`·`Cancel()`·`Key(ctx, msg)`·`Update(msg) bool`.
- `termpane.Launched{Mode; Pane *termhost.Pane; View *Pane}`, `termpane.Launch(ctx, req, opts) (Launched, tea.Cmd, error)`, `termpane.LaunchFunc`, `termpane.SetLaunchForTest(f) (restore func())`, `termpane.DowngradedToast(t, req, env)`, `termpane.PaneLimitToast(t, max)`.
- `termpanetest.Install(tb) *Fake`, `(*Fake)`: `Started()`, `Exit(id, code) tea.Msg`, `Cwd(id, dir) tea.Msg`, `Notify(id, kind) tea.Msg`, `SetLimit(n)`, `SetEnv(e)`, `Keys(id)`. pane 모드에서 `View` 는 `"<program> pane WxH"` 를 그리는 진짜 `*termpane.Pane`.
- `app.SetPanesForTest(running, closeAll) (restore func())`(시험용), `uitest.WithNestedPane()`, `app.Options.NestedPane`.

CollabHub 클라이언트 (Task 29·30)
- `api.Doc`·`DocVersion`·`DocVersionSnapshot`·`DocComment`, `CreateDocRequest`·`UpdateDocRequest`·`CreateDocCommentRequest`·`UpdateDocCommentRequest`.
- `api.Client`: `ListDocs`·`CreateDoc`·`GetDoc`·`UpdateDoc`·`DeleteDoc`·`ListDocVersions`·`CreateDocVersion`·`GetDocVersion`·`RestoreDocVersion`·`ListDocComments`·`CreateDocComment`·`UpdateDocComment`·`DeleteDocComment`, `api.DocSocketURL(docID)`. 쓰기는 리미터 `RL_DOC`.
- `realtime.DocCreatedEvent`·`DocUpdatedEvent`·`DocCommentCreatedEvent`, `realtime.DialConn(ctx, url, h, limit)`, `realtime.NewBackoff(rnd)`(`Next`·`Reset`).
- `collab`: `OpPut`·`OpSet`·`OpDel`·`OpUndel`·`OpReset`, `OpsMax=200`, `MaxClientFrame=65536`, `ReadLimit=2162688`; `Op`·`Object`·`Snapshot`·`Change`; `IsName`·`ErrInvalid`·`ParseOps`·`CheckOps`·`DecodeSnapshot`.
- `collab.NewDoc(s) *Doc`: `Seq`·`Apply(seq, ops)`·`Object(id)`·`Field(id, field)`·`Objects(typ)`·`Snapshot`·`Clone`.
- `collab.Options{API; DocID; UserID; OnEvent; Dial}`, `collab.Open(ctx, o) *Session`: `Submit(ops) string`·`SetPresence`·`View`·`Peers`·`Pending`·`Close`(실행 중인 OnEvent 를 기다림). `collab.Presence`.
- 이벤트: `EventState`·`EventChanged`·`EventRejected`·`EventOverwritten`·`EventPresence`·`EventKicked`(`ReasonNotFound` 포함). 닫힘 코드 `CloseUnauthorized=4001`·`CloseAccountSuspended=4003`·`CloseRateLimited=4008`·`CloseKicked=4100`, `ErrKicked`, `collab.SetBackoffForTest(rnd)`.
- `collabtest.New(tb) *Hub`: `Seed`·`SeedDoc(meta api.Doc, s)`·`Dial`·`Ops`·`Snapshot`·`Inject`·`RejectNext`·`Kick`·`SetOnline`·`Conns`·`MapToken`·`Register(*apitest.Server)`, `collabtest.ErrOffline`, `Hub.Clock`.
- `ui.Context.CollabDial`(nil = 실제 연결), `uitest.WithCollab(hub)`.
- 서버: 문서 댓글 REST + `doc.comment_mention` 알림(멘션은 공간 구성원에게만), `pendingLimiterRows` 는 이제 빈 표.

## 미룬 것 (오너·다음 wave 확인)

- 허브의 안 읽은 수: termpane 리뷰가 허브에서도 보이게 바꿨다(위 골든 2개). 허브에 보일지는 오너 확인.
- secrets: 로그아웃 때 도구 로그인 지우기가 실패하면 토스트로 알리는 것(설계서 A9.4 밖, 지금은 로그만).
- secrets: 실제 키체인·Credential Manager·Secret Service 동작(index 4절 오너 확인 #3·#4). macOS 키체인에 실제 값을 써 보지는 않았다.
- termpane: 오너 확인 1번(`ctrl+\` 실기 표, 한국어 자판 ₩ 키).
- termpane: `internal/ui/e2e` 의 `-race` 30분 초과는 기준 `58376da` 에서도 같다(이 batch 탓 아님).
- `internal/termhost` `TestCloseEscalates` 가 부하가 높을 때 가끔 137 을 낸다(위 통합 수정 절). 시험이나 코드를 손볼지는 termhost 소유 쪽 판단.
- `AssertNoLeakFrames` 에 프레임을 모아 줄 공용 가짜 소켓은 아직 없다.
- 시험은 macOS 에서만 실제로 돌았다(Linux·Windows 는 vet 만).
- wave 3 의 미룬 것(Task 10 Step 1 문구, `ui/fun/focus_summary.go`, zh-Hans 누락 경고 등)은 그대로.

## 검증 (이 세션, `8234556` 기준 = 세 병합 뒤, 이 기록 커밋 직전)

- `go build ./...` → BUILD_OK
- `go test -count=1 ./...` → EXIT=0, ok 패키지 52개, FAIL/panic 0. 마지막 줄 `ok github.com/patrache/masil/internal/version 0.846s`. 첫 실행은 위의 `TestCloseEscalates` 흔들림 한 번으로 EXIT=1 이었다.
- `go vet ./...` → VET_OK · `GOOS=windows go vet ./...` → VET_WIN_OK · `GOOS=linux go vet ./...` → VET_LINUX_OK
- `58376da` 이후 더하거나 바뀐 .go 파일 전체 `gofmt -l` → 빈 출력
- `go generate ./internal/i18n` 뒤 `git status` 깨끗
- server/(collab Task 29 가 바꿈): `npm run typecheck` rc=0. `npm test -- --maxWorkers=4` rc=0, `Test Files 76 passed (76)`, `Tests 741 passed (741)`.

## 배치별 "내 판단" 모음

wave 4 통합
- 내 판단: 충돌 7곳(termpane 5, collab 2)은 모두 두 쪽의 필드·함수 덧붙이기라 양쪽을 다 남겼다. 다른 손질은 하지 않았다.
- 내 판단: `TestHubGolden/120x40` 의 두 골든 변화는 Task 20(A8.3) 의 상태줄 안 읽은 수 한 줄뿐이고, 기능 플래그를 켜야 보이는 0.1.3 골든이라 의도된 변화로 받았다.
- 내 판단: `TestCloseEscalates` 한 번 실패는 이 wave 에서 바뀌지 않은 `internal/termhost` 의 부하 흔들림으로 보고, 고치지 않고 미룬 것에 적었다.

batch secrets (`docs/superpowers/plans/exec-log/P0-secrets.md`)
- 내 판단: `Secret` 은 계획의 `struct{ v string }` 대신 `struct{ p *string }` 이다. 비공개 필드에 Secret 을 넣은 남의 구조체를 `%v` 로 찍으면 fmt 가 리플렉션으로 문자열을 그대로 보여 주는데, 포인터면 주소만 보인다. 바깥 겉모양(New·Reveal·Masked·IsZero·redacted 출력)은 같다.
- 내 판단: `Masked` 는 바이트가 아니라 글자(rune) 기준으로 마지막 4자를 보인다.
- 내 판단: `Store` 에 계획 밖의 `Fallback()` 읽기를 더했다(시험 `TestConsentSetsFallbackThenBroadcasts` 가 Session 이 됐는지 봐야 해서). `Service = "masil"` 상수도 내보냈다.
- 내 판단: OS 저장소가 한 번 `ErrUnavailable` 을 내면 그 실행 동안 Put·Get 은 OS 를 다시 부르지 않는다(키체인 거절 창이 거듭 뜨지 않게). Delete·DeleteAll 은 늘 OS 를 부른다(로그아웃 지우기가 빠짐없이).
- 내 판단: Put 은 성공한 곳 말고 다른 로컬 자리(파일·세션)에 남은 같은 키 값을 지운다. Get 순서는 세션 → (색인이 file 이면 파일) → OS → 파일.
- 내 판단: `index.json` 은 `{"version":1,"entries":{"<account>":"<backend>"}}` — 값 없이 이름과 보관 위치만. 세션 값은 색인 파일에 쓰지 않는다(재시작 뒤 없어야 하므로). `tools.json` 은 `{"version":1,"values":{...}}`, 0600, 폴더 0700(더 넓으면 좁힘), `.gitignore` 는 `*\n`. 깨졌거나 1 MiB 넘는 파일은 빈 것으로 읽고 경고만 남긴다 — 이때 json 오류 문구는 로그에 쓰지 않는다(파일 글자를 인용할 수 있음).
- 내 판단: `Key` 는 Tool·Server·UserID 에 `/` 와 제어 문자를 받지 않는다(Name 은 `/` 허용 — API 환경/변수 이름). `List`·`DeleteAll` 이 계정 문자열을 거꾸로 풀 수 있게.
- 내 판단: `Open` 은 I/O 를 하지 않는다(시작할 때 키체인 창 없음). 그래서 계획의 "run.go 에서 secrets.Open" 은 `run.go` 의 `openSecrets(o)` 를 `app.New` 가 부르는 모양이다(`app.Options.Secrets`, nil 이면 이 OS 기본 저장소). 하네스와 Run 이 같은 길을 타고 `ctx.Secrets` 는 언제나 nil 이 아니다. `model.go` 에는 줄 둘(옵션 필드·호출)만 더했다.
- 내 판단: `uitest.WithSecrets` 를 쓰려고 Files 밖의 `internal/ui/uitest/harness.go` 를 고쳤다(기본 = `secrettest.NewStore(tb, true)`).
- 내 판단: `secrettest.Values` 가 세션·파일 값을 읽도록 `secrets.ValuesForTest(s)` 를 내보냈다. 시험 밖에서 부르면 panic(`testing.Testing()`, termhost 와 같은 방식).
- 내 판단: macOS `security -i` 의 따옴표 규칙은 이 세션에서 실제 `/usr/bin/security` 에 읽기 전용 명령(`show-keychain-info` 에 없는 경로)을 넣어 확인했다 — 작은따옴표 안에서 `\'`·`\\` 가 풀리고, POSIX 식 `'\''` 이어붙이기는 안 된다. 그래서 값은 `'...'` 안에서 `\` → `\\`, `'` → `\'` 로 적는다. 키체인 항목은 하나도 만들지 않았다. `-i` 모드의 종료 코드는 마지막 명령의 OSStatus 아래 바이트다(확인함).
- 내 판단: 줄바꿈·CR·NUL 이 든 값은 한 줄 명령으로 안전하게 못 넘기므로 macOS 백엔드가 오류로 거절한다(OS 를 부르지 않음).
- 내 판단: `security` 종료 코드 44 = `ErrNotFound`, 그 밖의 실패(51 거절, 45, 128 취소, 36, 도구 없음 등)는 모두 `ErrUnavailable` → 저장소가 동의를 묻는다. 도구 출력은 오류에 넣지 않는다.
- 내 판단: Windows 조각 나누기와 Linux Secret Service 흐름은 OS 와 상관없는 파일(`credman.go`, `secretservice.go`)에 두고 가짜(`credAPI`, `dbusConn`)로 모든 OS 에서 시험한다. 그래서 `TestCredManChunks`·`TestSecretServiceFlow` 는 계획의 `backend_windows_test.go`·`backend_linux_test.go` 가 아니라 `credman_test.go`·`secretservice_test.go` 에 있다(이 Mac 에서 실제로 돈다). OS 파일에는 syscall·godbus 어댑터와 `defaultOS` 만 있고, 그 시험 파일은 기본 백엔드 이름·오류 번호 대응·버스 없음 확인만 한다.
- 내 판단: Windows 는 계획의 `syscall.NewLazyDLL` 대신 `golang.org/x/sys/windows.NewLazySystemDLL("advapi32.dll")`(시스템 폴더에서만 읽음)을 쓴다. `CRED_PERSIST_LOCAL_MACHINE`(이 PC·이 사용자, 로밍 안 함). 값 ≤ 2,560바이트는 머리 항목 하나, 넘으면 `masil/<account>#0…` 조각 + 머리 항목(Comment `masil:chunked`, 내용 = 조각 수), 최대 64조각. 조각을 먼저 쓰고 머리를 나중에 쓴다. `ERROR_NOT_FOUND` = 없음, `ERROR_NO_SUCH_LOGON_SESSION` = `ErrUnavailable`.
- 내 판단: Linux 는 `DBUS_SESSION_BUS_ADDRESS` 가 비면 바로 `ErrUnavailable`(dbus-launch 자동 실행 안 함, `$XDG_RUNTIME_DIR/bus` 로 따로 찾지도 않음 — 계획 문구 그대로). 같은 항목이 있으면 `SetSecret`, 없으면 기본 모음(`ReadAlias("default")`, 없으면 unavailable)에 `CreateItem(replace=true)`. 잠긴 항목·모음은 `Unlock` 의 Prompt 를 `Completed` 신호까지 기다린다. D-Bus 실패는 모두 `ErrUnavailable` 로 감싼다.
- 내 판단: 도구가 비밀값 키를 만들 수 있게 `ui.Context.SecretScope() (server, userID, ok)` 와 `ui.Context.SecretKey(tool, name) (secrets.Key, bool)` 를 새 파일 `internal/ui/secretkey.go` 에 더했다(서버 = API 기본 URL 의 host, 없으면 설정 서버; 사용자 = `ctx.Me().ID`). 설정 [지우기]·로그아웃 지우기도 이것을 쓴다. 계획 Files 밖.
- 내 판단: 동의 대화상자는 ↑/1 = ①, ↓/2 = ②(기본), enter/space 확인. esc 는 아무것도 정하지 않고 `ConsentMsg{Choice: FallbackUnset}` 을 방송한다(도구가 기다림을 끝낼 수 있게). 확인은 `SetFallback` 을 먼저 부른 뒤 닫기 + 방송.
- 내 판단: 동의 문장은 줄바꿈으로 전문을 보이고, 높이가 모자라면 빈 줄 → 바닥 안내 순으로 뺀다. 문장과 선택지는 빼지 않는다.
- 내 판단: 입력 대화상자는 붙여넣기를 받고(쿠키는 보통 붙여넣음) 줄바꿈·제어 문자는 버린다. 칸에는 글자 수만큼 `•` 만 보이고(칸보다 길면 끝쪽), enter 뒤 버퍼를 지운다. 깨진 args 는 아무것도 열지 않는다.
- 내 판단: 로그아웃 선택지는 계획의 `overlays.go` 가 아니라 `widgets.ConfirmCheck`(새 파일 `internal/ui/widgets/confirmcheck.go`, Confirm 내부 도우미 재사용) + `commands.go` 의 `logoutDialog` 로 했다. 로그아웃 대화상자가 실제로 `commands.go` 에 있다. 기존 골든을 지키려고 **기능 플래그가 하나라도 켜졌을 때만** 선택지를 보인다(모두 꺼짐 = 0.1.2 Confirm 그대로). space/x 로 켜고 끈다(기본 꺼짐). 지우기는 POST /v1/auth/logout 뒤 같은 고루틴에서 `DeleteAll(server, user)` — 범위는 세션이 끝나기 전에 읽는다. 실패는 로그만.
- 내 판단: `logoutConfirmedMsg` 에 `wipeTools` 칸을 더하고 `model.go` 의 한 줄(`m.logoutConfirmed(msg.wipeTools)`)과 `sync.go` 의 `logoutConfirmed` 를 고쳤다.
- 내 판단: 설정 "도구 로그인" 묶음은 `sections` 표의 로그아웃 바로 앞에 자리만 두고, 줄이 하나라도 있을 때만 왼쪽 목록·머리에 그린다(제목은 카탈로그 `tools.logins_section`). 줄은 그 도구가 등록돼 있고 플래그가 켜져 있고 `describe` 가 ok 일 때만. 줄 모양 `<도구 이름> · <계정> · <보관 위치> · [지우기]`, 보관 위치는 그 도구 첫 값 이름의 `Where`(OS 를 부르지 않음). 긴 줄(일반 파일 경고)은 자르지 않고 줄바꿈한다(`row.wrapSub`, 이 줄에만).
- 내 판단: [지우기] 는 바로 지우지 않고 확인 대화상자(`{tool} 로그인을 지울까요?`)를 먼저 띄운다. 확인 뒤 forget 이 있으면 forget 만, 없으면 그 도구·서버·사용자의 모든 이름을 고루틴에서 Delete. 실패하면 줄 아래 `지우지 못했어요`.
- 내 판단: `RegisterToolLogin` 은 빈 ID·nil describe·중복을 panic 으로 막는다. `ResetToolLoginsForTest()` 는 restore 함수를 돌려준다(`t.Cleanup(settings.ResetToolLoginsForTest())`).
- 내 판단: `AssertNoLeak` 이 헤더를 보도록 `apitest.Recorded` 에 `Header http.Header` 를 더했다(Files 밖, 칸 하나 + 기록 한 줄, masil 토큰은 가림). 값은 날것·URL 쿼리/경로 이스케이프·JSON 이스케이프 꼴까지 찾고, 보고에는 요청 번호와 자리만 적는다(값·경로를 되풀이하지 않음). WebSocket 프레임은 apitest 가 기록하지 않아 보지 않는다.
- 내 판단: `TestImportGraph` 는 `go list -deps` 대신 `go list -json <패키지>` 의 `Deps`(전이)·`Imports`(직접)를 읽는다. 도구 접두사 표 `toolPackagePrefixes` 는 각 줄기가 한 줄씩 더한다. 시험이 정말 잡는지 `internal/api` 에 `secrets` 를 import 하는 임시 파일로 실패를 확인하고 지웠다.
- 내 판단: 새 문구는 모두 `tools.toml`(ko·en)에. `consent_question`·`backend_file` 은 `no_truncate`+`legal`, `logins_forget_body`·`logout_wipe` 는 `no_truncate`. zh-Hans 는 비움(경고만).
- 내 판단: 키체인 값은 `masil:b64:` + base64 로 저장한다(`security -w` 가 ASCII 밖 바이트가 든 값을 16진수로 찍기 때문, go-keyring 과 같은 이유). 그래서 줄바꿈·CR·NUL 이 든 값도 이제 한 줄로 안전하게 저장된다(이름에 든 것은 여전히 거절). 실제 키체인에 비ASCII 값을 써서 16진수 출력을 직접 확인하지는 않았다(오너 키체인에 항목을 만들어야 해서) — 시험은 진짜 도구처럼 16진수로 찍는 에뮬레이터로 한다. 접두사가 없는 항목은 그대로 돌려준다.
- 내 판단: 긴 값은 거절하지 않고 Windows 처럼 나눈다. 명령 한 줄은 4,000바이트를 넘지 않고, 넘으면 서비스 `masil.chunk` 의 `<account>#0…` 조각 + 머리 항목(`masil:chunks:<개수>`), 최대 64조각. 조각을 먼저, 머리를 나중에 쓴다. 조각용 서비스를 따로 둔 것은 값 이름에 `#0` 이 든 진짜 항목과 겹치지 않게. Set 은 남은 옛 조각을 지우려고 머리를 먼저 한 번 읽는다(`security` 한 번 더). 64조각을 넘는 값은 `security` 를 부르지 않고 `ErrUnavailable` 이 아닌 오류로 거절한다(키체인을 내려 두지 않음). 한 명령 = 한 프로세스(종료 코드가 그 명령의 것이 되게).
- 내 판단: 시험용 키체인 에뮬레이터는 시험 바이너리 자신이다(`init` 에서 환경변수 `MASIL_FAKE_KEYCHAIN` 을 보면 security 처럼 돎). 4,095바이트에서 줄을 끊고 남은 부분을 따로 실행하고, ASCII 밖 값은 16진수로 찍는다 — 둘 다 진짜 도구의 동작.
- 내 판단: ctx 가 끝난 뒤의 실패는 `ErrUnavailable` 이 아니라 ctx 의 오류다(`ctxErr`, store.go). `runSecurity` 와 Secret Service 어댑터가 그렇게 돌려주고, Store 도 OS 호출 뒤 한 번 더 걸러 어느 어댑터든 취소로 OS 를 내려 두거나 동의를 묻지 않는다. Windows Cred*W 는 ctx 를 쓰지 않는 동기 호출이라 어댑터는 그대로 두고 Store 쪽 거름으로 충분하다고 봤다.
- 내 판단: 로그아웃 지우기는 POST /v1/auth/logout **앞에** 자기 ctx(30초, `wipeTimeout`)로 돈다. 시험에서 요청 시간을 줄이려고 `logoutTimeout` 을 상수에서 변수로 바꾸고 `SetLogoutTimeoutForTest` 를 export_test.go 에 더했다. `secrettest` 의 가짜 OS 저장소도 끝난 ctx 로 부르면 ctx 오류를 내게 했다(진짜 저장소처럼).
- 내 판단: A9.5 의 WebSocket 프레임은 `secrettest.AssertNoLeakFrames(tb, frames [][]byte, values...)` 로 본다. apitest 는 소켓을 기록하지 않으므로 시험의 가짜 소켓이 모은 보낸 프레임을 넘긴다. 이 갈래에는 프레임을 모으는 공용 가짜가 없어 그쪽은 고치지 않았다.
- 내 판단: 이 batch 가 늘린 겉모양(`Recorded.Header`, `ConfirmCheck`, `SecretScope`·`SecretKey` 와 키 규칙, `app.Options.Secrets`, `Store.Fallback`, 설정 `도구 로그인` 묶음·`RegisterToolLogin`, 로그아웃 선택지, ctx 오류 규칙, `AssertNoLeakFrames`)을 계약 `client-architecture.md`(4.3.3·5.3.2·5.4·5.7·5.8·10.2)와 P0 계획의 "Interfaces 요약 — 비밀값" 줄에 적었다. P1·P4 계획 파일은 그 줄기 소유라 고치지 않았다 — 두 계획 모두 P0 Interfaces 를 출처로 삼는다.

batch termpane (`docs/superpowers/plans/exec-log/P0-termpane.md`)
- 내 판단: `termpane.shared_badge` 의 `max` 는 계획의 8 이 아니라 9 로 했다. `● 공유 중` 은 textwidth 로 9칸이라 생성기가 8 을 거절했다(en `● Shared` 는 8칸).
- 내 판단: `termpane.input_hint` 와 `prefix_hint` 에 `key` 인자를 더했다(`input_hint(program, key)`, `prefix_hint(key)`). `[keys] "termhost.escape"` 를 바꾸면 안내 글자도 따라가야 해서다. 기본 키에서 글자는 계획과 같다.
- 내 판단: 키맵 `termhost.escape` 의 기본 키는 `ctrl+\` 와 `ctrl+4` 두 개(도움말 표기는 `ctrl+\`)이고, 날 바이트 0x1c(Mod 없음)는 새 메서드 `(*keymap.Keymap).MatchesEscape` 가 받는다(`ctrl+\` 가 묶여 있을 때만). 키맵 파일에는 이 행과 메서드만 더했다.
- 내 판단: `ctrl+\ esc`(나가기)는 포커스를 다른 칸으로 옮기지 않고, 패널이 키보드를 놓는 것으로 했다(`GrabsKeys()==false`, 그 칸은 그대로 포커스). 위젯·초소형처럼 칸이 하나뿐인 모드에서도 같은 뜻이 되도록. 다시 들어가기는 그 칸에서 `enter`·`i` 또는 클릭이다. 상태줄 도움에 `enter 패널에 입력` 이 뜬다.
- 내 판단: `ctrl+\` 다음 h/j/k/l·화살표(포커스 이동)와 `p`(팔레트)는 패널이 앱에 부탁하는 msg 로 했다 — `ui.ShellActionMsg{Action}`·`ui.ShellAction`(`ShellFocusLeft/Down/Up/Right`, `ShellPalette`)를 `internal/ui/component.go` 에 더했다. `t` 는 `ui.ToolSurfaceMsg{Toggle}`, `z` 는 `Options.SingleCell` 이면 `ui.ToolSurfaceMsg{Zoom}`, 아니면 `Options.OnPrefix(ctx,"z")`.
- 내 판단: 접두키 결과에 `PrefixArm`(접두키를 눌러 기다리기 시작함, `Cmd` = 2초 타이머)을 맨 끝에 더했고, 타이머 msg 는 `(*Prefix).Update(msg) bool` 이 받는다. 계획 목록에는 "무장" 을 알리는 값과 시간 초과를 받는 길이 없었다. `(*Prefix).Cancel()` 도 더했다.
- 내 판단: `termpane.ExitedMsg` 는 Task 16a 목록에 있지만 패널이 스스로 끝남을 알리는 데 필요해 Task 16 의 `pane.go` 에서 정의했다. 패널은 `Init` 에서 `Handle.Done()` 을 기다리는 고루틴을 하나 두고 `ctx.Post` 로 보낸다.
- 내 판단: 초소형(테두리 없음)에서는 공유 중이거나 끝났을 때 패널 첫 줄을 제목 줄(`<제목> · ● 공유 중` / `… · 끝남 · 종료 코드 n`)로 쓰고 프로그램에는 한 줄 작은 크기를 준다. 초소형에는 맥락 줄이 없어서다. 19×10 은 TooSmall 이라 아무 패널도 그리지 않으므로 공유 표시 시험은 그 크기에서 "부서지지 않음" 만 본다.
- 내 판단: 테두리 제목의 공유 표시는 패널이 미리 자른다 — 제목을 (안쪽 폭 − 6)칸에 맞춰 먼저 자르고 ` · ● 공유 중` 을 붙인다. 앱의 테두리 제목 자르기(안쪽 폭 − 4)와 위젯 맥락 줄(아이콘·` ▾`)이 표시를 자르지 않게 하려는 것이다. 앱 쪽 그리기는 고치지 않았다.
- 내 판단: 키 독점은 앱 `routeKey` 의 맨 앞(공유 중 `ctrl+\` 대기 처리 바로 다음)에서 본다. 오버레이가 떠 있거나 로그인 화면이면 독점하지 않는다(종료 확인 같은 대화상자가 키를 받아야 하므로). 붙여넣기는 원래부터 포커스 컴포넌트로만 가서 따로 고치지 않았다.
- 내 판단: 공유 중(`ui.KillSwitchActive()`) 앱 전역 `ctrl+\` 는 오버레이보다 먼저 본다("masil 어느 화면에서든"). 다음 키가 `x` 면 `ui.KillAll`, 그 밖의 키는 버리고 취소, 2초가 지나면 취소. 패널이 키를 쥐고 있으면 패널의 접두키가 같은 일을 한다.
- 내 판단: 앱 `model.go` 에 두 줄을 더했다 — `escGen` 필드와 dispatch 맨 앞의 `m.grabMsg(msg)` 훅(`keys.go`). Files 목록 밖이다.
- 내 판단: 패널 도움말(`ctrl+\ ?`)은 `pane.go` 안의 작은 오버레이로 그렸다(예약 키 표, 도구의 `PrefixKeys`, `kbd_limits`, `kbd_nvim`). 앱 도움말(`?`)에는 `termhost.escape` 줄을 더하지 않았다(도움말 골든·`TestHelpToolRowsOnlyWithFlags` 그대로).
- 내 판단: 마우스 모드에서는 누름뿐 아니라 끌기·놓기도 프로그램에 보내려고 `Pane` 이 `ui.DragHandler` 도 구현한다. 마우스 모드가 아니면 휠은 보기 스크롤, 클릭은 다시 키보드 쥐기.
- 내 판단: 보기 스크롤 중에는 모든 키를 패널이 삼킨다(esc·q·end 끝, pgup/pgdn·위아래·home 이동). 휠을 끝까지 내리면 저절로 끝난다.
- 내 판단: Files 목록 밖 시험 파일 — `internal/ui/app/killswitch_test.go`(`TestGlobalKillWhileSharing`), `internal/ui/killswitch_test.go`, `internal/ui/keymap/keymap_test.go` 끝의 `TestMatchesEscapeVariants`.
- 내 판단: `opts.OnExit` 는 pane 방식에서만 불린다(패널 컴포넌트가 `ExitedMsg` 를 받을 때). exec·window 방식에는 그 msg 를 받을 컴포넌트가 없으므로 도구가 방송된 `ExitedMsg` 를 `InstanceID` 로 직접 받는다. 계획의 "OnExit 는 그대로 불리고" 를 "New 에 그대로 넘긴다" 로 읽었다.
- 내 판단: pane 방식의 `ExitedMsg` 는 패널 컴포넌트가 `Init` 된 뒤에야 온다(앱이 도구 인스턴스를 만들 때 Init 함). 도구가 `Launched.View` 를 자기 컴포넌트 안에 넣어 쓰면 그 `Init` 도 불러야 한다(문서 주석에 적음).
- 내 판단: `Launch` 가 `req.ToolID` 가 비었으면 `opts.ToolID` 로 채운다(`CurrentEnv` 의 `[tools.<id>] open` 을 읽으려고).
- 내 판단: 줄어든 방식 토스트 `termpane.downgraded(mode)` 의 mode 문구는 기존 `tools.mode_pane`·`tools.mode_window`(패널·외부 창)를 쓴다. 원한 방식은 `req.Mode`, 없으면 도구 설정 `open`.
- 내 판단: 패널 시작 색은 `ctx.Theme.P.FG/BG` 를 `PaneOptions.FG/BG` 로 넘긴다(OSC 10/11 답).
- 내 판단: `termpanetest` 와 실제 `Launch` 가 같은 토스트를 내도록 `termpane.DowngradedToast(t, req, env)`·`termpane.PaneLimitToast(t, max)` 와 형 `termpane.LaunchFunc` 를 내보냈다(계획 목록 밖, 더하기만).
- 내 판단: 시험 이음매 — `export_hook.go` 의 `SetLaunchForTest(f LaunchFunc) (restore func())`(시험 바이너리 밖이면 panic, `atomic.Pointer` 로 바꿔 끼움), `export_test.go` 의 `SetLaunchEnvForTest`·`SetStartPaneForTest`·`SetExecForTest`·`SetPopupForTest`(이 패키지 시험 전용).
- 내 판단: 가짜(`termpanetest.Fake`)는 기본 환경을 macOS·패널 켬·tmux 밖·초소형은 레이아웃에서·도구 설정 `open` 으로 둔다. pane 방식의 `Launched.View` 는 실제 `termpane.Pane` 이고, 그 안의 가짜 Handle 이 `"<program> pane WxH"` 를 그린다(키는 `Fake.Keys(instanceID)` 로 확인 — 계획 목록 밖, 더하기만). 가짜 Handle 의 `Done()` 은 nil 이라 끝은 `Fake.Exit` 로만 온다(같은 끝이 두 번 오지 않게). 패널 상한 토스트의 수는 `SetLimit` 값이다.
- 내 판단: `TestLaunchCwdMsg`·`TestLaunchNotifyMsg`·`TestLaunchExitedMsgAllModes` 의 pane 부분과 `TestLaunchChoosesMode` 의 pane 부분은 실제 PTY(`/bin/sh`·`/bin/cat`)로 돌고 Windows 에서는 건너뛴다. 상한 시험은 `StartPane` 이음매가 `ErrPaneLimit` 를 돌려주게 해서 본다.
- 내 판단: 종료 길은 `ctrl+c` 두 번과 팔레트 `종료` 둘이다(`/quit` 슬래시 명령은 없음). 둘 다 `requestQuit` 을 지난다. 팔레트 `종료` 는 패널이 없으면 예전 그대로 "종료할까요?" 확인(`*widgets.confirmDialog`, `theme_test` 그대로)이고, 패널이 있으면 바로 패널 확인을 띄운다(확인 두 번을 피함). 그래서 `commands.go` 의 `quit` 실행을 `quitRequestMsg{confirm: true}` 로 바꿨다 — Files 목록 밖이고, 한글 리터럴 4개는 `quit.go` 로 옮겨 패키지 수는 그대로다.
- 내 판단: 기본 선택 "아니오" 는 `widgets.Confirm` 을 만든 뒤 `→` 키를 한 번 넣어 맞췄다(`widgets` 는 고치지 않음). 확인 창 단추 문구는 `termpane.quit_yes`(함께 끄기)·`quit_no`(아니오), 제목 `termpane.quit_title`.
- 내 판단: 목록은 프로그램마다 한 번, 처음 시작한 순서, `count_item` 을 `", "` 로 잇는다(`lazygit 2개, nvim 1개`).
- 내 판단: 확인 창이 떠 있는 동안 `ctrl+c` 두 번은 두 번째 창을 띄우지 않는다(`Model.quitAsk`).
- 내 판단: "예" 는 `termhost.CloseAll(2초)` 를 부르고 `tea.QuitMsg` 를 돌려주는 Cmd 하나다(Cmd 고루틴에서 기다림). 시험 이음매는 `app.SetPanesForTest(running, closeAll)`(export_test.go).
- 내 판단: `MASIL_PANE=1` 경고 토스트는 `run.go` 의 `(*Model).nestedPaneWarning` 이 만들고 `Init` 이 한 번 낸다(시험 하네스는 `runProgram` 을 거치지 않으므로 `Init` 에 묶었다 — `model.go` 한 줄).
- 내 판단: A8.3 "상태줄에 안 읽은 수" 는 표준 모드에서 도구 표면이 앞에 있고 활성 항목이 도구 인스턴스(`RouteTool`)일 때만 오른쪽 맨 앞에 `💬 안 읽음 n`(`termpane.chat_unread`, `Store.TotalUnread` 의 안 읽음 수, 0 이면 없음)으로 보인다. 허브가 앞에 있을 때는 넣지 않았다 — 넣으면 Task 19 의 `TestHubGolden/120x40/*` 골든 3개가 바뀌기 때문이다. 허브에서도 보일지는 오너·다음 wave 판단으로 남긴다. 문구 키는 `tools.toml` 이 아니라 이 Task 의 `termpane.toml` 에 두었다(secrets 배치가 `tools.toml` 을 고칠 수 있어 병합 충돌을 피함).
- 내 판단: 이 때문에 Task 16 에서 만든 `TestPaneGolden/{120x40,shared_120x40}/pane.golden` 두 개의 상태줄이 바뀌어 `-args -update` 로 다시 만들었다(이 배치에서 새로 만든 골든, 기존 골든 아님).
- 내 판단: `TestFeaturesOnShellUnchanged` 는 `TestShellGoldens` 의 세 화면을 `uitest.AllSizes()` 모두에서 `Styled()`(색 포함 문자열)로 비교한다.
- 내 판단: Files 목록 밖 — `internal/ui/app/commands.go`(위), `model.go`(`quitRequestMsg` 처리 한 줄, `quitAsk` 필드, `Init` 의 경고 한 줄), `export_test.go`(`SetPanesForTest`).
- 내 판단: pane 방식의 끝 감시는 `Launch` 가 패널을 만들 때 바로 시작한다(`(*Pane).watchExit`). 도구가 `Launched.View` 를 자기 컴포넌트 안에 두고 `Init` 을 부르지 않아도 `ExitedMsg` 가 온다. `Init` 은 그대로 두되 두 번째 감시를 만들지 않는다(`New` 로 직접 만든 패널은 지금처럼 `Init` 에서 시작). 위 Task 16a 의 "Init 해야 온다" 줄은 이것으로 바뀌었다.
- 내 판단: `Options.OnExit` 는 pane 방식 전용으로 문서에 못박았다(exec·window 에는 msg 를 받을 패널이 없어 ctx 와 함께 부를 곳이 없다). 모든 방식에서 할 일은 방송되는 `ExitedMsg` 를 받아서 한다. 계획·계약 문서는 고치지 않았다(계약에는 `OnExit` 이 없다).
- 내 판단: 패널의 `View` 는 이제 읽기만 한다 — 크기는 `Resize` 에서만, 포커스 보고는 `Update` 에서만 보낸다. 포커스는 `ui.FocusChangedMsg` 의 `RouteKey` 가 자기 도구 인스턴스 경로(`tool/<InstanceID>/<ToolID>`)인지로 정한다(처음 값은 참). 숨겨지면(`ctrl+\ t` 등) 프로그램에 포커스 아웃이 가고, 숨은 동안 터미널 blur/focus 가 와도 포커스 인을 보내지 않는다. 칸이 여럿인 도구(P2 작업공간)를 위해 `(*Pane).SetCellFocus(bool)` 를 더했다(계획 목록 밖, 더하기만).
- 내 판단: 초소형의 공유 표시는 패널 첫 줄이 아니라 탭 줄의 `도` 칸 점(`●`, `ui.Badge{Dot}`)으로 옮겼다(A7.7 "초소형은 탭 줄에"). 패널 `Title` 이 초소형·공유 중일 때 `Badge{Dot: true}` 를 돌려주고, 앱 `tabbar.go` 가 열린 도구 인스턴스 중 하나라도 그러면 `도` 칸에 점을 단다. 그래서 공유를 켜고 꺼도 프로그램 크기는 그대로다. 초소형에서 첫 줄 제목은 프로그램이 끝난 뒤(`끝남 · 종료 코드 n`)에만 쓴다. `TestPaneGolden` 골든은 바뀌지 않았다.
- 내 판단: 화면에 그리는 프로그램 이름(`input_hint`·`exit_confirm`·끌 때 목록)을 `textwidth.Sanitize` 로 거른다.
- 내 판단: `TestFeaturesOnShellUnchanged` 는 두 번 그릴 때 모두 `installStubTools` 로 다섯 도구와 도구 경로를 등록한다(실제 바이너리처럼). 제품 코드는 바꾸지 않았고 시험은 그대로 통과했다.
- 내 판단: A7.5 "masil 전역 키와 겹치게 바꾸면 경고" — `keymap.Load` 가 `termhost.escape` 와 전역 키(`app.quit`·`palette.open`·`search.global`·`notifications.open`·`window.prefix`·`focus.next`·`focus.prev`·`tools.toggle`, 곧 포커스 컴포넌트보다 먼저 판단하는 키)가 같은 키를 나누면 오류를 하나 내고, 바꾼 쪽(`termhost.escape` 먼저)을 기본값으로 되돌린다. 앱은 기존대로 시작 토스트 "키 설정 일부를 무시했어요" 를 띄운다. 경고만 하고 겹친 채로 두지 않은 것은, 겹치면 공유 중 즉시 끊기 접두키와 전역 키가 서로를 먹기 때문이다.
- 내 판단: A8.3 안 읽은 수는 표준 모드에서 도구 표면이 앞에 있고 도구 인스턴스가 하나라도 열려 있으면 허브가 앞에 있어도 보인다(허브만 있고 도구가 없으면 0.1.2 상태줄 그대로). 그래서 Task 19 의 0.1.3 골든 `internal/ui/tools/testdata/TestHubGolden/120x40/{hub_open,open_menu}.golden` 두 개의 상태줄이 바뀌어 `-args -update` 로 다시 만들었다(상태줄 한 줄씩, `💬 안 읽음 3 · ` 이 더해짐). 0.1.2 골든 변화는 없다.
- 내 판단: `MASIL_PANE` 은 `Run` 만 읽는다(`optionsFromEnv` → `Options.NestedPane`). 시험 하네스는 환경 변수를 읽지 않고 `uitest.WithNestedPane()` 으로만 켠다. `MASIL_PANE=1 go test -count=1 ./internal/ui/app/` 통과를 확인했다.

batch collab (`docs/superpowers/plans/exec-log/P0-collab.md`)
- 내 판단: 잠긴 문서에도 댓글은 쓸 수 있다. 댓글은 문서 편집이 아니고, 잠금은 op·제목(5b4d9d2: 만든 사람·owner·admin만)에만 걸린다.
- 내 판단: `root_id` 는 같은 문서의 루트 댓글(`root_id IS NULL`)만 받는다. 답글에 다시 답글을 다는 것은 400 `invalid_request {root_id:"invalid"}` 로 막아 답글을 한 단계로 둔다.
- 내 판단: 삭제된 댓글에 대한 PATCH·DELETE 는 404 다. 목록에는 `deleted:true`, `body:""` 로 남아 스레드 모양을 지킨다.
- 내 판단: 이벤트는 계약에 있는 `doc.comment_created` 만 보낸다. PATCH·DELETE 는 이벤트가 없고, 본문을 고쳐 멘션이 새로 생겨도 알림을 다시 보내지 않는다(계약에 이벤트가 없어 가장 좁게 읽음).
- 내 판단: 댓글 DELETE 권한은 글쓴이 또는 방의 owner·admin(`getSpaceRole`)이다. 문서를 만든 사람은 따로 넣지 않았다.
- 내 판단: 멘션 대상은 그 방 멤버의 handle 이고 글쓴이 자신은 뺀다. 차단 관계와 방 밖 사람은 기존 `sendAlerts` 규칙대로 알림이 가지 않는다. alert 는 `mention` 종류라 받은편지함(inbox) 줄을 만들지 않는다.
- 내 판단: Task 29 커밋에서는 Go 쪽 doc 메서드가 아직 없어 `internal/api/contract_limiters_test.go` 의 `pendingLimiterRows` 에 댓글 3행(POST/PATCH/DELETE)을 임시로 넣었다. Task 30 에서 doc 메서드 사례(`docEndpointCases`)를 더해, 그 3행과 이미 있던 문서 REST 5행(Task 26·28 몫)을 모두 지우고 waiver 를 비웠다(빈 map).
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
- 내 판단: `rate_limited` 로 붙잡힌 op 가 있으면 그 뒤 op 는 보내지 않는다(`flush` 에서 `continue` 대신 `break`). 이미 전선에 올라 먼저 순번을 받은 뒤 op(B)는 되돌릴 수 없으므로, B 의 ack 가 "앞선 내 op 가 아직 대기 중"일 때나 "앞선 내 op 가 받은 seq 보다 작을" 때 B 의 ops 를 새 `client_op_id` 로 그 자리에 다시 넣어 앞선 op 뒤에 한 번 더 보낸다. 옛 id 는 서버가 중복으로 보아 적용하지 않으므로 새 id 가 필요하다. 그래서 그런 B 는 Submit 이 돌려준 id 로 ack·reject 가 오지 않을 수 있다(EventRejected 에는 Ops 가 실린다).
- 내 판단: 재연결 업그레이드가 404 이고 그 세션이 한 번이라도 hello 를 받았으면 `EventKicked{Reason: ReasonNotFound("not_found"), Pending}` 를 낸 뒤 마지막 `EventState{Err}` 를 낸다. 처음 여는 404 는 강퇴가 아니라서 내지 않는다. 401·403 은 강퇴가 아니라 계정 문제라 EventKicked 를 내지 않고, 문서 주석에 "마지막 EventState 뒤에도 Pending() 이 답한다"고 적었다.
- 내 판단: 덮어쓰기 판단의 값 비교는 바이트가 아니라 뜻으로 한다(`sameJSON`: 둘 다 `any` 로 풀어 `reflect.DeepEqual`, 숫자는 JS 처럼 float64). collab.md "JSON 값은 의미가 같으면 같다(키 순서 무관)" 를 따랐다.
- 내 판단: `Options.UserID` 를 더했다. 같은 사용자의 다른 터미널 op(author == UserID)는 "남이 덮어씀"으로 보지 않고 내 쓰기로 기록한다. 비어 있으면 예전처럼 모든 원격 author 를 남으로 본다.
- 내 판단: 이벤트 전달 고루틴은 큐가 생길 때 띄우고 큐가 비면 끝낸다. 그래서 ctx 만 취소되고 Close 가 불리지 않아도 고루틴이 남지 않는다. Close 는 실행 중인 OnEvent 를 기다리되, OnEvent 안에서 부른 Close 는 기다리지 않는다(고루틴 ID 로 구별). `sync.Once` 를 빼서, OnEvent 안의 Close 와 바깥 Close 가 겹쳐도 교착이 없다.
- 내 판단: 연결이 끊길 때 다른 사람 presence 가 있었으면 `EventPresence{Peers: nil}` 을 한 번 낸다.
- 내 판단: `rejectRetry` 를 상수에서 시험용 변수로 바꿨다(패키지 시험은 150ms). `flush` 에서 인코딩이 실패한 op(Submit 이 이미 검사해 실제로는 없음)는 건너뛰지 않고 `EventRejected{invalid}` 로 뺀다. 남겨 두면 위 순서 규칙 때문에 뒤 op 가 영영 막히기 때문이다.
- 내 판단: `collabtest` 시험 `TestHubSessions` 를 계획 이름 `TestHubKickAndOffline` 으로 바꿨다. `Hub.SeedDoc(meta, s)` 를 더해 SpaceID·CreatedBy 를 줄 수 있게 했고, `Seed` 는 그 얇은 포장이다(space "").
