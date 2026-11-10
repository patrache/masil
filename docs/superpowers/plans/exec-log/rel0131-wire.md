# v0.1.3-1 연결 작업 (Stage 1 항목 1–4) — 실행 기록

브랜치 `release/v0.1.3-1` (worktree `rel0131`), 기준 `2c7a01be` (feat/lsp-client 합친 커밋).
푸시·태그·배포 없음. Windows 기기 없음: Windows 는 `GOOS=windows go vet ./...` 만 확인했다.

## 커밋

| 항목 | 커밋 | 제목 |
|---|---|---|
| 3 로그인 셸 보안 | 82df59e3 | fix(workspace): login-shell claude starts in $HOME and cds to the folder |
| 4 불안정한 시간 테스트 | 2b95b40c | test(gittool): subprocess-free lazygit seam for the leet and e2e tests |
| 2 작업공간 커서 | 7bda81f7 | fix(editor): a real terminal cursor and a reverse-video cursor cell |
| 1 LSP → 추천 메뉴 (작업공간) | 1e55094e | feat(workspace): language-server items first in the code completion menu |
| 1 LSP → 추천 메뉴 (오늘의 문제) | 3b9a1bf6 | feat(leet): language-server items in the built-in editor; README |

## 항목별 요약

### 1. LSP 를 추천 메뉴에 연결
- `internal/codeassist/items.go`: 메뉴 항목 `Item` (Label, Detail, Text, Filter, Edit 범위). `Merge` 는 서버 항목을 앞에, 아직 없는 단어를 뒤에 둔다. `FilterPrefix`, `Span` 도 여기 있다.
- `internal/ui/codelsp`: `Complete` 가 tea.Cmd 로 요청한다 (1.5 s, UTF-16 열 변환). `Items` 는 SortText → FilterText 순으로 정렬하고, 라벨·짧은 detail(28칸, 없으면 kind)을 만든다. 커서 줄에서 커서를 감싸는 TextEdit 만 범위를 유지한다. `Get()` 은 테스트 바이너리 안에서 nil 이다 (실제 서버를 띄우지 않음).
- 작업공간 편집기: `SetServer` / `TakeCompletionRequest` / `SetServerItems`.
  - 묻는 때: 두 글자 이상, 트리거 문자, ctrl+n/ctrl+space.
  - 버리는 답: 지난 요청 번호의 답, 단어를 벗어난 커서의 답, esc 뒤의 답.
  - 앞의 답이 완전하면 다시 묻지 않고 걸러서 쓴다 (isIncomplete 이면 다시 묻는다).
  - 수락은 op 하나다.
- Surface (`lsp.go`): **주인만** 쓴다 (`guest == nil && docs != nil && root != nil`). 문서를 열면 Open 한다. 내 편집, 공동 편집자의 편집(applyRemote), 디스크 병합, undo/redo, resync 는 Change 로 보낸다. 문서를 닫거나 도구를 닫으면 Close 한다. 「코드 자동추천」을 끄면 Close 한다.
- 오늘의 문제 goeditor: root 는 풀이 폴더, 언어는 `LangForLeetCode` 로 정한다. 키·Reload 뒤에 Change 를 보내고, dropEditor/CloseTool 때 Close 한다. detail 은 goeditor 메뉴의 Type 칸에 넣는다. TextEdit 항목은 키 입력(→, Backspace, 글자)으로 적용한다 (goeditor 패치 7 `SelectedCompletion`).
- `internal/lsp`: `--version` 확인(rust-analyzer)이나 **첫** 시작이 실패한 서버는 조용히 「없음」으로 처리하고, 재시작하지 않는다. 한 번 돌았다가 죽은 서버는 예전처럼 MaxRestarts 까지 다시 띄운다.
- README ko/en: "LSP 없음" 문장을 언어 서버 설명으로 바꿨다. 주인만 쓴다는 점, `[lsp.<lang>]` 재정의, 자동추천을 끄면 서버도 꺼진다는 점을 적었다.

### 2. 작업공간 커서
- 작업공간 코드 칸은 command·standard·compact 레이아웃 모두에서 실제 커서를 이미 맞게 두고 있었다 (tiny 는 편집기를 그리지 않음). 다만 커서 칸을 따로 그리지 않았다. 이제 포커스가 있을 때 반전(reverse video) 칸을 그린다.
- 오늘의 문제 goeditor 는 실제 커서를 내보내지 않았다. 이제 `Cursor()` 를 낸다 (보통 모드 Block, 입력·명령 모드 Bar). 커서 칸도 반전으로 그린다.
- 테스트: 테마 4개, 한글, 양방향 스크롤, 명령 줄, 포커스 없음, 레이아웃 전부 (`workspace/cursor_test.go`, `editor/cursor_test.go`, `leet/cursor_test.go`).

### 3. 로그인 셸 보안
- 셸은 `$HOME` 에서 시작한다 (`Request.ExecDir`). 폴더는 `$1` 로 넘기고, 스크립트가 `cd -- "$1" || exit 1` 한다. 그래서 rc 파일이 믿을 수 없는 저장소 안에서 돌지 않는다.
- 실제 zsh·bash 로 확인했다: rc 는 HOME 에서 돌고, claude 는 폴더에서 돈다. 적대적인 폴더 이름은 코드가 아니라 데이터로 남는다. 없는 폴더면 claude 를 실행하지 않는다.
- 공유 참가자 화면에서는 launchTerm 을 거부한다 (방어 심층).

### 4. 불안정한 시간 테스트
- 원인: 실패는 모두 `gittool.(*instance).start.func1` 안에서 났다. 시작 Cmd 안에서 진짜 `lazygit -cd` 를 실행하고 있었고, 부하가 걸리면 하네스의 1 s 제한을 넘었다.
- 고침: `gittool.StandInEnvForTest` 를 썼다 (서브프로세스 없는 lazygit 대역; --version 과 -cd 만 답함). leet together 테스트와 e2e tools 테스트가 이 대역을 쓴다. 제한 시간은 **늘리지 않았다.**
- 하네스 감시 메시지가 막힌 Cmd 의 함수 이름을 보여 준다.
- 증명 (부하 평균 11–15, 전체 go test 를 동시에 돌림):
  - 고친 뒤: leet `-count=20`, e2e 두 테스트 `-count=20` 모두 실패 0. 전체 실행 EXIT=0.
  - 고치기 전, 같은 조건: leet 는 20번 중 5번 이상, e2e 는 40번 중 5번 이상 실패. 출력이 head 로 잘려서 정확한 수는 이보다 많을 수 있다.

## 확인 결과 (최종 HEAD 3b9a1bf6 + 이 기록)
- `go test -count=1 ./...` (-short 없음): EXIT=0, 87개 패키지 ok.
- `go vet ./...` 를 GOOS=darwin, windows, linux 로 돌림: 모두 0.
- `go test -race` 를 바뀐 패키지에 돌림: lsp, codeassist, ui/codelsp, ui/workspace, ui/workspace/editor, ui/leet, third_party/goeditor, termhost, ui/uitest, ui/gittool, workspace/launch, termpanetest. 모두 ok.
  - ui/e2e 는 race 에서 기본 10분 제한에 걸렸다 (데이터 경쟁 보고 없음, 시간 초과). `-timeout 40m` 으로 다시 돌린 결과는 아래 「race e2e」 줄에 적는다.
- `gofmt -l`: 없음.
- `go generate ./internal/i18n`: 차이 없음. 이번에 새 문구는 없다. Go 코드에 새 한글 리터럴도 없다 (주석만 있음).
- 골든: 바뀐 것 없음.

## 내 판단
- **편집기는 LSP Cmd 를 HandleKey 에서 돌려주지 않는다.** Surface.codeKey 가 편집기의 cmd 를 그 자리에서 실행해 결과를 읽기 때문이다. 대신 편집기가 요청을 쌓아 두고(`TakeCompletionRequest`), Surface 가 `codelsp.Complete` Cmd 를 만든다. 답은 `ResultMsg` 로 돌아온다 (Key = 인스턴스 id + "\n" + 경로).
- **오래된 답의 기준**: 요청 번호가 같아야 하고, 커서가 같은 줄의 같은 단어 시작에 있어야 하며, 단어 앞 글자가 같아야 한다. 단어가 더 길어졌으면 버리지 않고 걸러 쓴다. 이렇게 판단했다: 「문서 버전이 바뀌면 버림」을 그대로 따르면 한 글자 칠 때마다 답이 버려져서 메뉴가 서버 항목을 보여 주지 못한다.
- **서버 답이 오면 선택은 맨 위로 간다.** 사용자가 이미 ↓ 로 골랐으면 그 항목을 유지한다.
- **트리거 문자(".") 뒤**에는 단어가 없어도 서버 항목만으로 메뉴를 연다. 단어 추천은 두 글자부터라는 기존 규칙을 지킨다.
- **lsp 첫 시작 실패 = 없음**: 스펙은 「--version 이나 initialize 가 실패하면 없는 것으로」라고 했다. 그런데 기존 규칙은 시작 실패도 crash 로 세어 재시작을 반복했다. 그래서 첫 시작 실패만 조용히 포기하도록 바꿨다. 한 번 돌았던 서버의 재시작 실패는 예전처럼 센다.
- **오늘의 문제 TextEdit 를 키 입력으로 적용**: goeditor 의 실행 취소 기록과 어긋나지 않게 하려는 것이다. 새 줄은 Enter 로 넣으므로, goeditor 가 자동 들여쓰기를 하면 그만큼 다를 수 있다 (서버가 줄바꿈 있는 항목을 보내는 일은 드묾).
- **「LSP 없음」 한 번 알림(선택 사항)은 넣지 않았다.** 토스트 금지에 맞는 조용한 자리가 없고, 서버가 없으면 단어 추천이 그대로 나오므로 동작에 차이가 없다. 그래서 새 i18n 문구도 없다.
- **고치다가 발견한 기존 버그**: goeditor 가 한글을 키의 첫 바이트로 읽어서 깨진 Latin-1 글자를 넣고 있었다 (패치 6). 커서 작업 중에 발견해서 고쳤다.
- **테스트 도구 추가**: `uitest.ReverseCells`, `termpanetest.Fake.SetCursor`, 하네스가 막힌 Cmd 이름을 보여 주기, `codelsp.Fake`, `gittool.StandInEnvForTest`. 모두 `testing.Testing()` 밖에서는 panic 하거나 _test 파일에 있다.
- 공유 참가자 차단 테스트(`TestGuestNeverUsesLSP`)는 결과만 확인한다. 참가자 화면은 원래 `docs == nil` 이라서 `guest != nil` 조건 하나를 지워도 테스트는 통과한다. 그 조건은 방어 심층으로 남겼다.

## 오너 확인
- **Warp 에서 커서가 안 보이던 근본 원인은 확인하지 못했다.** bubbletea 의 synchronized output 과 커서 표시 문제일 가능성이 있다. 이제 반전 칸을 직접 그리므로, 원인과 상관없이 커서 위치는 보인다. 실제 Warp 에서 한 번 봐 주십시오.
- **오늘의 문제 Go 풀이 파일에는 `package` 줄이 없다** (LeetCode 스니펫 그대로). 그래서 gopls 추천이 제한될 수 있다. 실제 gopls 로는 확인하지 않았다 (테스트는 가짜 서버로만 돌았음).
- **오늘의 문제 LSP root 는 풀이 폴더**(모든 날짜의 풀이가 모인 곳)다. 언어 서버가 그 폴더 전체를 읽는다.
- **Windows 는 vet 만 했다.** 실제 Windows 실행과 CI 는 돌리지 않았다.
- ui/e2e 의 race 실행은 오래 걸려 기본 10분 제한을 넘는다. CI 에 race 가 있으면 timeout 값을 확인해 주십시오.

## 리뷰 반영
- **[minor] codelsp.go:112 — 잘린 답이 완전한 답으로 표시됨: 고침 (7e906490).**
  - 확인: `Items()` 는 SortText 순으로 앞의 `codeassist.MaxMenuItems`(200)개만 남긴다. 그런데 서버가 isIncomplete=false 를 보내면 답은 완전한 것으로 표시됐다. 편집기(workspace `assist.go`, leet `goeditor_lsp.go`)는 완전한 답이면 다시 묻지 않고 잘린 목록을 거른다. 그래서 200번째 뒤에 정렬되는 항목(`os.` 다음 `Wr` 같은 경우)은 끝내 나오지 않았다. 리뷰가 맞다.
  - 고침: `Items` 가 `(items, cut)` 를 돌려준다. `cut` 은 쓸 수 있는 항목이 200개를 넘어 실제로 버려졌을 때만 참이다 (빈 라벨처럼 원래 빠지는 항목은 세지 않음). `Complete` 는 `Incomplete: l.Incomplete || cut` 로 답한다. 편집기의 기존 「불완전하면 다시 묻기」 경로를 그대로 쓰므로 편집기 코드는 바꾸지 않았다.
  - TDD: `TestCompleteCutIsIncomplete` (250개 + 잘린 뒤에 정렬되는 `WriteFile`, 완전한 답) 를 먼저 써서 실패를 확인한 뒤 고쳤다. 정확히 200개면 잘리지 않으니 완전한 답으로 남는지도 확인한다.
  - 다른 방법(전체 목록을 보관하고 표시할 때만 200개로 자르기)은 쓰지 않았다. 다시 묻는 비용은 한 번의 요청이고, 편집기 두 곳의 상태 구조를 바꾸지 않아도 된다.
- 확인: `go test -count=1 ./...` (-short 없음) EXIT=0. `go vet ./...` 를 GOOS=windows 와 darwin 으로 돌림: 모두 0. `gofmt -l`: 없음.
