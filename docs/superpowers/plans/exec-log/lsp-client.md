# lsp-client 실행 기록 (v0.1.3-1 — LSP 수준 코드 완성, 서버 쪽)

브랜치 `feat/lsp-client`, 기준 `origin/main` 309d23fb. 명세: 오너 2026-11-10 16:41 "lsp 수준의 추천이 있어야 한다." — 이 브랜치는 언어 서버 클라이언트 패키지만 만든다. 편집기 메뉴(`feat/editor-assist`)에 잇는 일은 통합 단계 몫이고 `internal/ui/**` 는 건드리지 않았다. push·tag·배포 없음.

## 통합하는 사람을 위한 API 요약 (`internal/lsp`)

편집기는 **`lsp.Shared()`** (프로세스 하나에 Manager 하나)만 쓰면 된다. 모든 메서드는 UI 고루틴에서 불러도 되게 짧다(서버 시작은 고루틴, 알림은 큐). `Complete` 만 최대 1.5 초 막으므로 `tea.Cmd` 안에서 부른다.

```go
m := lsp.Shared()

// 파일을 열 때 — root 는 로컬 사용자 자신의 폴더(워크스페이스 루트, LeetCode 풀이 폴더).
// 공유 손님(guest) 세션에서는 절대 부르지 않는다(호출하는 쪽이 막아야 한다).
err := m.Open(root, absPath, text)
//   lsp.ErrUnavailable → 이 언어 서버가 PATH 에 없음/포기함: 단어 완성으로 돌아간다
//   lsp.ErrNotLocal    → root 가 절대경로·존재하는 폴더가 아니거나 파일이 그 밖
m.Change(absPath, text) // 매 편집마다 불러도 된다(150 ms 디바운스, 전체 본문 동기화)
m.Close(absPath)

// 완성 — tea.Cmd 안에서
cmd := func() tea.Msg {
    col := lsp.UTF16Col(lineText, cursorRuneCol) // 한글 1, 이모지 2 단위
    l, err := m.Complete(context.Background(), absPath, cursorLine, col)
    // err != nil(ErrTimeout·ErrUnavailable·ErrClosed …) 은 모두 "항목 없음": 단어 완성만 보인다
    return lspItemsMsg{l, err}
}
// l.Items 는 SortText 순. it.Edit != nil 이면 it.Edit.Range(UTF-16 열; lsp.RuneCol 로 바꾼다)를
// it.Edit.NewText(== it.InsertText)로 바꾼다. nil 이면 커서 앞 단어를 it.InsertText 로 바꾼다.
// l.Incomplete 가 true 면 다음 글자를 칠 때 다시 묻는다(거르기만 하지 말고).

// 트리거 문자("." 등): 서버가 떴을 때만 있다
if c := m.Client(absPath); c != nil { trig := c.TriggerCharacters() }

// 언어: 파일 확장자 또는 LeetCode slug
lsp.LangForPath("main.go")         // "go"
lsp.LangForLeetCode("python3")     // "python"
srv, ok := lsp.Find("python")      // 서버가 PATH 에 있는지만 알고 싶을 때
```

공개 면 전체:

```go
// 서버 표
type Server struct{ Lang, Name string; Command []string }
func Find(lang string) (Server, bool)
func SetOverrides(m map[string][]string)          // config [lsp.<lang>] command
func LangForPath(path string) string
func LangForLeetCode(slug string) string

// 한 서버 프로세스
func Start(ctx context.Context, srv Server, rootDir string) (*Client, error)
func (o Options) Start(ctx context.Context, srv Server, rootDir string) (*Client, error)
type Options struct{ Clock Clock; Logger *slog.Logger; CompletionTimeout, Debounce, ShutdownGrace time.Duration }
func (c *Client) DidOpen(uri, lang, text string)
func (c *Client) DidChange(uri, text string)
func (c *Client) DidClose(uri string)
func (c *Client) Complete(ctx context.Context, uri string, line, utf16col int) ([]Item, error)
func (c *Client) CompleteList(ctx context.Context, uri string, line, utf16col int) (List, error)
func (c *Client) Shutdown(ctx context.Context) error
func (c *Client) Version(uri string) int
func (c *Client) TriggerCharacters() []string
func (c *Client) HasCompletion() bool
func (c *Client) Server() Server
func (c *Client) Root() string
func (c *Client) Done() <-chan struct{}
func (c *Client) Err() error
func (c *Client) Stopping() bool

// 편집기용 관리자
func NewManager(o ManagerOptions) *Manager
type ManagerOptions struct{ Client Options; IdleTimeout time.Duration; MaxRestarts int; Find func(string) (Server, bool); Start func(context.Context, Server, string) (*Client, error) }
func (m *Manager) Open(root, path, text string) error
func (m *Manager) Change(path, text string)
func (m *Manager) Close(path string)
func (m *Manager) Complete(ctx context.Context, path string, line, utf16col int) (List, error)
func (m *Manager) Client(path string) *Client
func (m *Manager) StopAll(ctx context.Context)
func Shared() *Manager
func StopAll(ctx context.Context)

// 자료형·도우미
type Item struct{ Label, InsertText string; Kind CompletionItemKind; Detail, FilterText, SortText string; Edit *TextEdit }
type List struct{ Items []Item; Incomplete bool }
type TextEdit struct{ Range Range; NewText string }
type Range struct{ Start, End Position }
type Position struct{ Line, Character int } // Character = UTF-16
type CompletionItemKind int                  // KindText … KindTypeParameter, String() = "function" 등
func UTF16Col(line string, runeCol int) int
func RuneCol(line string, utf16Col int) int
func URIFromPath(path string) string
func PathFromURI(uri string) (string, error)
type Clock interface{ Now() time.Time; AfterFunc(time.Duration, func()) Timer }
type Timer interface{ Stop() bool }
var ErrTimeout, ErrClosed, ErrUnavailable, ErrNotLocal error
const CompletionTimeout = 1.5s, ChangeDebounce = 150ms, ShutdownGrace = 2s, InitTimeout = 30s, IdleTimeout = 10m, MaxRestarts = 3
```

서버 표(앞에서부터 PATH 에 있는 것): go `gopls` · python `pyright-langserver --stdio`, `basedpyright-langserver --stdio`, `pylsp` · c/cpp `clangd` · rust `rust-analyzer` · typescript/javascript(+react) `typescript-language-server --stdio` · java `jdtls` · csharp `csharp-ls` · php `intelephense --stdio` · swift `sourcekit-lsp` · kotlin `kotlin-language-server` · ruby `solargraph stdio` · scala `metals`. LeetCode 의 dart·racket·erlang·elixir 는 서버 없음(`LangForLeetCode` = "").

설정: `config.toml` 의 `[lsp.<languageId>] command = ["prog", "arg"…]` 를 `internal/config`(`Config.LSP`, `Config.LSPCommands()`)가 읽고, `cmd/masil/main.go` 의 `realMain` 이 `setupLSP(cfg)` 로 `lsp.SetOverrides` 에 넘긴다. 같은 `defer` 가 masil 종료 때 `lsp.StopAll`(최대 3 초, 서버마다 2 초 뒤 kill)을 부른다. 설정 화면에서 바꾼 값을 실행 중 다시 넘기려면 통합 때 `lsp.SetOverrides(cfg.LSPCommands())` 를 한 번 더 부르면 된다(이미 뜬 서버는 그대로).

## 커밋

- 692c9eba feat(lsp): minimal LSP client over stdio for editor completion — `internal/lsp` 전체(프레이밍·Client·Manager·서버 표·UTF-16·가짜 서버 시험·gopls 시험·의존 규칙 시험)
- 3aa7a6c5 feat(config,cmd): [lsp.<lang>] command overrides and stop servers on exit — `internal/config`(`Config.LSP`·`LSPCommands`), `cmd/masil/lsp.go`(`setupLSP`), `main.go` defer, `importgraph_test.go` 에 `internal/lsp`
- (이 커밋) docs: 이 실행 기록

## 시험

가짜 언어 서버는 시험 바이너리 자신(`TestMain` 이 `fake-lsp-server <모드>` 인자를 보면 서버로 돈다; 환경변수 없이 인자로만 고르므로 병렬·Windows 에서도 같다). 프레이밍을 말하고, 받은 문서를 기억해 추가 요청 `fake/state` 로 돌려준다.

- 초기화 왕복(`initialize`→`initialized`, snippetSupport=false, rootUri, processId, triggerCharacters) — `TestInitializeHandshake`
- 서버가 먼저 묻는 요청(`workspace/configuration` 에 null 배열, `window/workDoneProgress/create` 에 null, 모르는 메서드에 -32601, 문자열 id 도) — 답하지 않으면 가짜가 initialize 를 안 끝낸다 — `TestServerRequestsAnswered`
- didOpen/didChange/didClose 내용·버전, 디바운스 150 ms(가짜 시계), 열리지 않은 문서 변경 무시, 닫을 때 대기 중 변경 폐기 — `TestDocumentSync`
- 완성 직전 대기 중 변경 즉시 전송 — `TestCompleteSendsPendingChange`
- CompletionList / 배열 / isIncomplete / textEdit(한글·이모지 줄, UTF-16 범위) / 스니펫 벗기기 / 이름 없는 항목 버림 / sortText 정렬 — `TestCompleteShapes`
- completionProvider 없는 서버 — `TestCompleteNoProvider`
- 느린 응답 → ErrTimeout·`$/cancelRequest`, 늦은 취소 답이 다음 요청을 흐리지 않음, 명세 기본값(1.5 s·150 ms·2 s·10 분·3 회) 고정 — `TestCompleteTimeout`
- shutdown/exit 정상 종료(종료 코드 0), 두 번 불러도 됨 — `TestShutdownExit`; exit 를 무시하는 서버는 유예 뒤 kill — `TestShutdownKillsStuckServer`
- 충돌 → 요청 ErrClosed·Done — `TestCrashFailsRequests`; 시작 실패 — `TestStartFailure`
- stderr → 로그(화면 아님), window/logMessage → 로그, 줄 수 상한 200 — `TestStderrGoesToLog`, `TestStderrLogCap`
- 환경: LEETCODE_* 빠짐, 다른 변수는 그대로; 이름 목록이 `leetcode.Env*` 와 같음 — `TestEnvScrubbed`, `TestScrubEnvNamesMatchLeetCode`
- Manager: 바로 돌아오는 Open, 같은 (언어, 루트)는 서버 하나, 다른 루트는 따로, Change·Close — `TestManagerOpenCompleteClose`, `TestManagerStopAll`
- 로컬 아닌 root/path 거부(상대·밖·`..`·없는 폴더·파일을 root 로) — `TestManagerRefusesNonLocal`
- 충돌 뒤 재시작 최대 3 회(재시작 때 현재 본문으로 다시 didOpen), 그 뒤 조용히 ErrUnavailable·더 안 띄움 — `TestManagerRestartLimit`; 시작 실패도 같은 셈 — `TestManagerStartFailuresCountAsCrashes`
- 유휴 10 분 종료(가짜 시계; 6 분에 쓰면 12 분엔 살아 있고 16 분에 종료), 정상 종료로 끝남, 다음 사용 때 새 서버(재시작 횟수에 안 셈) — `TestManagerIdleShutdown`
- StopAll(시작 중이어도) — `TestManagerStopAll`, `TestManagerStopAllDuringStart`, `TestSharedStopAll`
- UTF-16 ↔ 룬 열 표(한글, 이모지, 서로게이트 중간, ZWJ 이모지, 잘못된 UTF-8, 범위 밖) — `TestUTF16Columns`
- 프레이밍(바이트 길이, 대소문자 무관 헤더, Content-Type, 잘린 본문, 깨진 헤더) — `TestFraming`; 결과 모양 끝 사례(null, InsertReplaceEdit, 1000 개 상한) — `TestParseCompletionEdgeCases`; 스니펫 — `TestStripSnippet`
- 서버 표·확장자·LeetCode slug(새 LeetCode 언어가 생기면 실패)·설정 덮어쓰기 — `TestLanguages`, `TestFind`
- 진짜 gopls(PATH 에 없거나 -short 면 건너뜀): 임시 모듈에서 `fmt.Pr` → `Println` — `TestGoplsCompletesPrintln` (이 Mac: gopls 의 답 `Println`, textEdit 5:5–5:7, isIncomplete=true)
- 의존 규칙: `internal/lsp` 는 표준 라이브러리와 `internal/version` 만 — `internal/lsp.TestImportRules`; `internal/api`·`internal/ui/chat` 은 `internal/lsp` 에 기대지 않는다(`cmd/masil` `toolPackagePrefixes` 에 추가)
- 설정 `[lsp.<lang>]` 읽기·잘못된 값은 그 키만 버림·저장 왕복 — `config.TestLoadLSPCommands`, `config.TestSaveKeepsLSPCommands`; main 의 연결 — `cmd/masil.TestSetupLSP`
- 돌연변이 확인(손으로 되돌림): 환경 정리를 빼면 `TestEnvScrubbed` 가, 재시작 한도를 하나 늘리면 두 재시작 시험이 실패했다.

실행 결과는 아래 "검증" 절.

## 내 판단

- 내 판단: 명세의 `Complete(ctx, uri, line, utf16col) ([]Item, error)` 는 `Client.Complete` 그대로 두고, isIncomplete 를 넘기려고 `CompleteList`(→ `List{Items, Incomplete}`)를 더했다. `Manager.Complete` 는 `List` 를 돌려준다(편집기가 다시 물을지 알아야 해서).
- 내 판단: 버전은 Client 가 센다(열 때 1, 보낸 변경마다 +1). 호출하는 쪽이 버전을 들고 다닐 일이 없다. 디바운스는 "마지막 편집 뒤 150 ms"(계속 치는 동안은 안 보냄) — 완성 요청이 대기 중 변경을 먼저 보내므로 늦은 본문으로 묻는 일은 없다.
- 내 판단: 시간 초과(1.5 s)는 `nil, ErrTimeout` 이고 서버에 `$/cancelRequest` 를 보낸다. Manager 는 서버가 아직 뜨는 중이면 같은 1.5 s 안에서 기다린다 — 그래서 gopls 가 처음 뜨는 순간의 첫 완성은 비어 있을 수 있다(단어 완성만 보임). 다음 글자부터 LSP 항목이 온다.
- 내 판단: 재시작은 "다음에 쓸 때" 한다(충돌 즉시가 아니라). 계속 죽는 서버를 빈 시간에 돌리지 않으려는 것이고, 시작 실패도 충돌로 센다. 최초 1 회 + 재시작 3 회 = 최대 4 번 띄우고, 그 뒤 그 실행 동안은 그 (언어, 루트)를 포기한다(경고 로그 한 줄, 화면엔 아무것도 없음).
- 내 판단: 유휴 종료는 그 (언어, 루트) 항목을 지워서, 다음 사용은 새 서버(재시작 횟수 0)로 시작하고 열린 문서를 현재 본문으로 다시 연다. 문서를 열어만 두고 10 분 쓰지 않아도 "안 쓴 것"으로 본다.
- 내 판단: "끄기 전 정리 등록부"는 아직 없다(P3·tracks-merge 기록의 오너 결정 대기 항목). 그래서 `cmd/masil/main.go` `realMain` 의 defer 로 걸었다 — `closeLog` 보다 먼저 돌아 서버의 마지막 줄이 로그에 남는다. 쓴 적 없으면(`Shared()` 미호출) 아무것도 안 한다. 등록부가 생기면 `lsp.StopAll` 을 거기로 옮기면 된다.
- 내 판단: 환경은 다른 실행 프로그램처럼 `os.Environ()` 을 물려준다(termhost 와 같음). masil 은 시작할 때 LEETCODE_* 를 이미 프로세스 환경에서 빼지만(`internal/ui/leet/envcookies.go`), 그 이동보다 먼저 서버가 뜨는 경우를 막으려고 같은 세 이름을 한 번 더 뺀다(Windows 는 대소문자 무시). `internal/leetcode` 를 가져오지 않으려고 이름을 복사했고, 시험이 두 목록이 같은지 본다. masil 의 다른 비밀(토큰)은 키체인에 있고 환경변수로 들어오지 않는다.
- 내 판단: 로컬 한정은 패키지 안에서 강제할 수 있는 만큼만 강제한다 — root 는 존재하는 절대 폴더, 파일은 그 안. "공유 손님이 아님"은 이 패키지가 알 수 없으므로 문서 주석과 이 기록에 호출 쪽 책임으로 적었다.
- 내 판단: 서버 stderr 는 Info 수준으로 로그에 남기되 서버마다 200 줄까지(로그는 5 MB 에서 돌아간다). `window/logMessage` 는 많아서 Debug(현재 로그 수준 Info 라 안 남음), 오류(type 1)만 Warn.
- 내 판단: 클라이언트 능력은 snippetSupport=false, insertReplaceSupport=false, workspace/configuration=false(그래도 서버가 물으면 null 로 답함), positionEncodings=["utf-16"]. 스니펫을 보내는 서버가 있으면 `$1`·`${1:x}` 를 벗겨 평문으로 넣는다. InsertReplaceEdit 이 오면 insert 범위를 쓴다.
- 내 판단: 한 답의 항목은 1000 개까지(넘으면 자르고 Incomplete=true). 항목은 SortText(없으면 Label) 안정 정렬.
- 내 판단: 서버 stdout 은 `io.Pipe` 로 받고 `cmd.WaitDelay = 1s` — 서버가 띄운 자식이 stdout 을 쥐고 있어도 Wait 가 매달리지 않게. Windows 에서는 콘솔 창이 뜨지 않게 `CREATE_NO_WINDOW`.
- 내 판단: 서버의 자식 프로세스(typescript-language-server 의 tsserver 등)까지 프로세스 그룹으로 죽이지는 않는다. 정상 경로(shutdown/exit, stdin EOF)에서는 서버가 자식을 정리한다. kill 까지 간 경우에만 자식이 남을 수 있다.

## 오너 확인

- 첫 완성 지연: gopls 처럼 처음 뜰 때 1.5 s 를 넘는 서버는 파일을 연 뒤 첫 완성이 비어 있을 수 있다(Open 때 바로 띄우므로 대개 첫 타이핑 전에 뜬다). 괜찮은지.
- 종료 고리: 등록부가 없어 `realMain` defer 로 걸었다. masil 이 끝날 때 서버가 떠 있으면 최대 3 초 더 걸릴 수 있다(보통 수십 ms).
- `[lsp.<lang>]` 키 이름은 LSP languageId(go, python, cpp, typescript …)다 — LeetCode slug(golang, python3)가 아니다. 이 이름으로 괜찮은지.
- 공유 손님 막기는 통합하는 쪽(editor-assist 연결)이 `Open` 을 부르기 전에 해야 한다.

## 검증 (macOS arm64, go1.26.6, 커밋 3aa7a6c5 위)

- `go vet ./...` · `GOOS=windows go vet ./...` · `GOOS=linux go vet ./...` — 셋 다 exit 0
- `go test -count=1 ./...` (-short 없음) — exit 0, 85 패키지 ok(시험 파일 없는 패키지 2), 254 초(vet 포함). 진짜 gopls 시험 포함 통과.
- `go test -race -count=5 ./internal/lsp` — ok (race 빌드의 가짜 서버는 종료 때 1 초 쉬어서 느리다: race 런타임의 atexit sleep)
- `GOOS=windows go test -c` — `./internal/lsp`, `./internal/config`, `./cmd/masil` 컴파일됨
- `gofmt -l internal cmd` — 깨끗함
- **Windows 에서 실제로 돌려 보지는 않았다.** 추론으로만 본 것: 시험 바이너리 재실행(`os.Executable`), stdio 파이프, `Process.Kill`(TerminateProcess → ExitError), `file:///C:/…` URI, 드라이브가 다른 경로는 `filepath.Rel` 오류 → ErrNotLocal.
