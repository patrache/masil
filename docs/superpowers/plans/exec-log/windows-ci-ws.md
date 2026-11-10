# v0.1.3 Windows CI — group ws

Branch `fix/v013-win-ws` (from dev/v0.1.3 a9276cc1). Log: GitHub Actions windows-latest run 42760118078
(Go 1.26.6). Packages: `internal/workspace/fsroot`, `internal/workspace/share`, `internal/ui/workspace`.
Windows 기기는 없다. **Windows CI not yet rerun** — 아래의 "고쳤다"는 macOS 실행, `GOOS=windows` vet·컴파일,
코드 읽기로 확인한 것이지 Windows 에서 본 결과가 아니다.

## 원인과 커밋

| # | 원인 | 분류 | 커밋 |
|---|------|------|------|
| A | `fsroot.follow` 가 링크 대상의 `..` 를 Unix 식(앞 링크를 먼저 따라간 뒤 한 칸 위)으로 풀었다. Windows 는 대상을 링크 폴더에 붙여 **글자 그대로 정리**한다(`CreateSymbolicLinkW` 는 절대 대상을 정리해서 저장, Go `os.Root` 도 Windows 에서 `rootCleanPath` 로 같은 식). | 제품 버그(Windows) | 569e986a fix(fsroot): resolve link targets lexically on Windows |
| B | `TestSaveAtomicAndAudited` 가 0640 이 그대로 읽히길 기대. Windows 는 그룹·기타 비트가 없어 Go 가 0666(읽기 전용이면 0444)으로 보고한다. | Unix 전용 가정(테스트) | b98ad1bf fix(share): check the kept file mode the way Windows reports it |
| C | 테스트가 Surface 를 닫지 않고 끝났다(`forgetSurfaces` 는 잊기만 하고, share rig 정리는 `CloseTool` 이 돌려준 명령 — 감사 로그를 닫는 `endHostCmd` — 를 버렸다). Windows 는 열린 파일·폴더를 지우지 못해 `t.TempDir` 정리가 `ws_01.jsonl` / `proj` 에서 "being used by another process" 로 실패. | 테스트 하네스 누수 | b1bc0664 fix(ui/workspace): close test surfaces before their temp dirs go on Windows |
| D | 공유 시작 대화상자의 Windows 이유(`windows_term_share_disabled`)가 터미널 줄 아래에만 그려졌는데, Windows 에서는 프로그램이 화면 전환(ModeExec)으로만 떠서 터미널 줄이 생기지 않는다 → 이유가 **한 번도 안 보였다**(스펙 B Windows·인수 기준 20 위반). | 제품 버그(Windows) | 9fc88e12 fix(ui/workspace): show why terminals cannot be shared in the share dialog on Windows |
| E | 내장 터미널·터미널 공유 로직 테스트(가짜 패널 `termpanetest`)가 `paneSupported()` 를 OS 기본값으로 둬서, Windows 에서는 시험 대상 로직이 꺼진 채 돌았다("shared terminals = 0" 등, 골든의 `n 새 터미널` → Windows 안내). Windows 동작 자체는 스펙대로였다. | Unix 전용 가정(테스트) | 41d7c998 fix(ui/workspace): run the pane tests with fake panes on Windows too |

## 실패 테스트별

### internal/workspace/fsroot
- **TestReadTextLinkDotDotReadsOSTarget** (`ReadText(abs) = "root"; want "sub"`) → A. `abs` 는 절대 대상이라
  Windows 가 만들 때 `…\s\..\config` 를 `…\config` 로 정리해 저장했고 Go `Readlink` 는 그 SubstituteName 을 준다 →
  OS 가 여는 파일은 실제로 `root`. 같은 테스트의 `a`(상대 대상)는 CI 에서 "sub" 로 통과했지만, 그건 Unix 식 계산이
  맞아서가 아니라 `ReadText` 가 계산한 경로를 읽기 때문이다; Windows(그리고 os.Root)는 `proj\config` 를 연다.
  테스트는 이제 `windowsLinks` 이면 두 링크 모두 "root" 를 기대한다.
  - 보안 의미: `Create`·`Rename`·`Remove` 는 `clean` 경로를 os.Root 에 넘겨 **os.Root 가 링크를 따라간다**. 두 모델이
    다르면 Check 가 본 파일과 실제로 건드리는 파일이 달라진다. 예: `k -> .ssh`, `.ssh/s -> ../sub`,
    `.ssh/d -> s/../config` 에서 Unix 식 계산은 `config`(허용), Windows 는 `.ssh/config` 를 연다.
    새 테스트 `TestCheckLinkWindowsModelDeniesLexicalSecret`(Denied), `TestCheckLinkWindowsModelEscape`(글자 정리로
    루트 위 = Escape), `TestCheckLinkDotDotWindowsModel` 이 모든 OS 에서 Windows 모델을 돌린다(Unix 모델로 바꾸면
    실패하는 것을 macOS 에서 확인).
- **TestCheckLinkDotDotFollowsOS** (CI 에서는 통과) — Unix 링크 모델 전용이 되어 `windowsLinks` 이면 Skip
  (이유: Windows 에서는 `d -> s/..` 가 폴더 자신이라 `.ssh` 로 가지 않는다; 같은 상황의 Windows 기대값은
  `TestCheckLinkDotDotWindowsModel` 이 확인). 기능이 없는 게 아니라 OS 가 다른 파일을 여는 것이므로 그 테스트가 대신한다.

### internal/workspace/share
- **TestSaveAtomicAndAudited** (`mode = -rw-rw-rw-`) → B. 이제 "저장 전과 같은 모드"를 모든 OS 에서, 정확한 0640 은
  Windows 가 아닌 곳에서 확인한다. 이 0640 은 비밀 보호 단언이 아니라 "저장이 모드를 유지한다"는 단언이다.
  Windows 에서 접근을 정하는 것은 ACL 이다: 임시 파일은 폴더가 새 파일에 물려주는 ACL 을 받고 rename 으로 바뀐다 —
  **원래 파일에 따로 준 ACL 은 옮겨지지 않는다**(아래 "남은 것").
- **TestNoFolderDataInAnyRequest** — 이 이름은 `internal/ui/workspace/acceptance_test.go` 의 테스트다(share 패키지
  아님). 원인 C 뿐.
- share 패키지에서 다른 실패는 로그에 없다.

### internal/ui/workspace
- C 만(단언은 통과, TempDir 정리만 실패): TestNoFolderDataInAnyRequest, TestPaletteCommands,
  TestOverlayClosedSurfacePrefixBack, TestJoinRequestWaitsForOwner, TestJoinRequestRingsBell, TestAuditLogInConfigDir,
  TestFilesOffShareStillTicksDocs, TestBandShowsShareTitle, TestPaletteCommandsFollowSharing,
  TestShareInvitesSpaceMembers, TestShareStartsOnlyOnEnter, TestJoinApprovalFlow, TestSASMismatchWarns,
  TestKeyboardConfirmRequiresHandle, TestKeyboardWarningNeverTruncated(5 크기), TestOwnerActivityFeedsHost,
  TestOpenFolderSolo, TestOpenEditSaveSolo, TestRecentFoldersRemembered, TestRecoveryPrompt,
  TestWindowsTerminalNotice, TestTamperDialogIgnoresTypedKeys, TestPrefixXWithoutShareIsPaneQuit,
  TestGoldenOwner/{80x24,50x20,40x14}, TestGoldenSolo/{80x24,50x20,40x14}.
- C + E("shared terminals = 0"): TestEndWithUnsavedPeerEdits, TestEndStopsProgramsWhenAsked, TestTinyOwnerSummary,
  TestGoldenOwner2(7개 하위), TestProposalInsertSend, TestProposalMultilineConfirm, TestProposalEditAndReject,
  TestAttentionFromHook, TestAttentionFromBell, TestTinyFsRequestNeedsApproveKey, TestProposalQueueEnterDoesNotRun,
  TestProposalBandNamesRealKey, TestOwnerSeesAndSendsCursors, TestKeyboardInputBandOnStatusLine.
- C + E(다른 문구): TestShareEndReleasesTerminals("share: taps 0"), TestBypassProgramCannotBeShared("no block reason" —
  Windows 이유가 우회 모드 이유 대신 나왔다), TestPrefixMovesFocus("the terminal does not hold the keyboard").
- C + E(골든 차이 `n 새 터미널` → `Windows에서는 masil 안 터미널이 아직 안 돼요. e로 전체 화면 실행`):
  TestGoldenOwner/{200x60,120x40}/{sharing_band,people}, TestGoldenSolo/{200x60,120x40}/solo,
  TestGoldenSolo/widget/solo_widget_term. 공용 골든은 그대로 두고(모든 OS 가 같은 화면), Windows 화면은
  새 골든 `testdata/TestGoldenSolo/windows/solo_windows.golden`(120x40, 패널 없음)으로 모든 OS 에서 고정했다 —
  Windows 출력이 공용 골든에서 몰래 갈라지는 게 아니라 따로 이름 붙은 골든이 된다.
- 새 테스트: **TestWindowsShareDialogNoTerminals**(D) — 패널 없음 모델에서 대화상자에 이유가 있고 화면에 보이며,
  터미널 줄은 없고, 파일만 공유가 시작된다. 고치기 전 코드로 돌리면 실패하는 것을 확인.

C 를 macOS 에서 확인한 방법: 정리 직전·직후에 `lsof -p <pid>` 로 테스트 임시 폴더 아래 열린 것을 세는 임시 검사를
넣어 패키지 전체를 돌렸다(커밋 안 함). 닫기 전에는 감사 로그·`proj` 가 열려 있고, 닫은 뒤에는 Surface 의 것이 하나도
남지 않았다. 남은 것은 guest rig 가 직접 연 호스트 쪽 `fsroot.Root`(`…/004/proj`)뿐인데, 그 rig 는 자기 정리
(`t.Cleanup(root.Close)`, TempDir 보다 늦게 등록 → 먼저 실행)에서 닫는다 — guest 테스트가 Windows CI 에서 통과한 것과 맞다.

## 내 판단

- **내 판단 (A)**: Windows 에서는 링크 대상의 `..` 를 글자 그대로 정리하는 모델을 쓴다. 근거는 Go 표준 라이브러리
  (`os/root_windows.go` 의 "On Windows, file names are lexically cleaned at the start of a file operation",
  링크를 따라갈 때 `rootCleanPath(target, prefix, suffix)`)와 `CreateSymbolicLinkW` 가 절대 대상을 정리해서 저장하는
  것(CI 의 `abs` = "root" 가 그 관측). 가장 중요한 건 **Check 가 os.Root 와 같은 모델로 판정하는 것**이다 —
  Create/Rename/Remove 가 os.Root 를 통해 링크를 따라가기 때문이다.
- **내 판단 (B)**: 0640 을 Windows 에서 확인하지 못하는 것은 보안 약화가 아니다(이 테스트는 모드 유지 테스트).
  Windows 에서는 ACL 이 접근을 정한다; 아래 "남은 것"의 per-file ACL 손실은 따로 적었다.
- **내 판단 (C)**: 제품 쪽 누수가 아니다 — 앱은 도구를 닫을 때 `CloseTool` 과 그 명령을 돌리고, 끝날 때는 프로세스가
  핸들을 닫는다. 테스트가 앱처럼 닫도록 고쳤다. `closeLiveAtEnd` 는 `uitest.New` 뒤(= 첫 `t.TempDir` 뒤)에 등록해야
  한다(Cleanup 은 LIFO) — `openFolder` 와 guest rig 에서 부른다.
- **내 판단 (D)**: 새 문구 없이 기존 `windows_term_share_disabled` 를 "공유할 것" 아래에 한 번 보인다(패널이 없고 터미널
  줄도 없을 때만). 스펙의 "'터미널 공유'는 비활성(이유 표시)"을, 체크할 수 없는 터미널 줄 대신 이유 한 줄로 읽었다.
- **내 판단 (E)**: Skip 하지 않았다. 이 테스트들은 OS 와 무관한 로직(가짜 패널)을 시험하므로 `setup` 이
  `paneSupported` 를 켜고, Windows 동작은 `SetPaneSupportedForTest(false)` 테스트 4개(TestWindowsTerminalNotice,
  TestWindowsGuestCanWatch, TestWindowsShareDialogNoTerminals, TestGoldenSolo/windows)가 모든 OS 에서 확인한다.
  실제 Windows 바이너리에서 `paneSupported()` 는 여전히 false 다(제품 코드 변경 없음).

## 남은 것 (다음 Windows CI 까지 불확실)

- **Windows CI not yet rerun.** 이 그룹의 실패가 다 사라지는지는 다음 실행에서 확인해야 한다.
- A: 상대 링크 대상의 `..` 를 Windows 가 정말 글자 그대로 푸는지는 Go os.Root 의 모델에 기댄 것이고 Windows 에서
  직접 보지 않았다. 다음 CI 에서 `TestReadTextLinkDotDotReadsOSTarget`(a, abs 모두 "root")가 통과해야 한다. 리뷰 뒤
  (아래 R1) 이 테스트와 Windows 모델 테스트들은 계산값만이 아니라 **OS 가 실제로 여는 파일**(`os.ReadFile`, 상대 링크는
  `os.Root.ReadFile` 도)과 대조하므로, 모델이 틀리면 다음 Windows CI 가 "link model differs from the OS / os.Root" 로
  어느 쪽이 다른지까지 알려 준다.
- A: CI 러너는 개발자 모드라 심링크가 만들어졌다. 일반 사용자 Windows 에서는 `symlink()` 도우미가 Skip 한다.
- B 관련 제품 한계(이번에 고치지 않음): Windows 에서 원자 저장(임시 파일 + rename)은 원래 파일에 따로 준 ACL 을
  옮기지 않고, 읽기 전용 속성 파일은 rename 덮어쓰기가 거부될 수 있다(MoveFileEx 가 읽기 전용 대상에 ACCESS_DENIED).
  둘 다 Windows 에서 확인하지 못했다.
- C 관련: Windows 에서 masil 이 폴더를 연 동안 `os.Root` 핸들(공유 삭제 없이 열림) 때문에 탐색기에서 그 폴더를
  지우거나 이름을 바꾸지 못할 수 있다 — os.Root 의 성질이고 이번 실패와는 별개, 확인 안 함.
- E: 가짜 패널로 Windows 에서 처음 실제로 돌게 되는 테스트 중 `TestAttentionFromHook` 은 `hooks.Listen` 을 쓴다.
  `internal/workspace/hooks` 패키지는 Windows CI 에서 통과했지만 ui 쪽 경로는 Windows 에서 아직 안 돌아 봤다.

## 리뷰 지적 반영 (2차)

- **R1 (fsroot, medium) — 진짜 문제, 고침** `8e885ec7 fix(fsroot): check the link model against what the OS opens on Windows`.
  지적대로 `TestReadTextLinkDotDotReadsOSTarget` 등은 `ReadText` 가 `follow` 로 계산한 경로를 읽을 뿐이라 `windowsLinks`
  계산이 틀려도 통과했다(원래 CI 의 `a` 통과가 그 예). `assertOSOpens(proj, rel, want, osRoot)` 를 넣었다:
  `os.ReadFile(proj/rel)`(커널이 링크를 따라감)과, 상대 링크면 `os.OpenRoot(proj).ReadFile(rel)`(Create/Rename/Remove 가
  쓰는 경로)가 모델의 답과 같아야 한다. `os.Root` 는 절대 링크 대상을 거부하므로(`splitPathInRoot`, Go 1.26 소스 확인)
  `abs` 는 커널 읽기만 대조한다.
  - `TestReadTextLinkDotDotReadsOSTarget`: 모든 OS 에서 대조(macOS 에서 커널·os.Root 모두 "sub" 로 통과 확인).
  - `TestCheckLinkDotDotWindowsModel`: `runtime.GOOS == "windows"` 일 때 `d/config`, `a`, `e`(커널+os.Root), `abs`(커널)가
    "root". 다른 OS 에서는 모델을 강제로 켠 것이라 OS 는 Unix 식으로 열므로 대조하지 않는다.
  - `TestCheckLinkWindowsModelDeniesLexicalSecret`: Windows 에서 `k/d` 를 커널·os.Root 로 읽으면 "SECRET" — 막는 링크가
    실제 비밀 파일을 가리킨다는 것까지 확인한다.
  - **내 판단**: os.Root 의 Windows 동작(`rootCleanPath` 로 링크 폴더+대상+나머지를 함께 정리)은 Go 소스로 확인했다.
    커널(plain open)이 상대 대상 `s\..\config` 를 같은 식으로 푸는지는 소스로 확인할 수 없다 — 다르면 다음 CI 에서
    `os.ReadFile` 단언만 실패할 것이고, 그때는 os.Root 경로(Create/Rename/Remove)와 터미널 쪽 plain open 이 서로 다른
    파일을 연다는 뜻이므로 모델을 고르기 전에 오너께 여쭌다.
- **R2 (ui/workspace helpers, low) — 진짜(잠재) 문제, 고침** `36d7aec2 fix(ui/workspace): run the cleanup close commands through
  the harness on Windows`. `closeLiveAtEnd` 의 대비 경로 `runCmds` 는 `tea.BatchMsg` 만 펼치고 나머지 메시지를 버렸다 —
  `tea.Sequence` 나 메시지 뒤에 이어지는 명령이 생기면 `endHostCmd` 가 조용히 사라져 Windows TempDir 정리 실패가 다시 난다.
  이제 `runCmds(t, h, cmd)` 는 Batch 를 펼치고, 결과 메시지는 `h.Send` 로 앱에 넘기며(하네스가 후속 명령을 돌리고
  `tea.Sequence` 는 하네스가 Errorf), 돌릴 수 없는 bubbletea 내부 메시지는 버리지 않고 `t.Errorf` 로 알린다.
  - **내 판단**: 지적의 첫 안 `h.Send(cmd())` 그대로는 쓰지 않았다 — `Harness.Send` 는 받은 메시지를 바로 `Model.Update`
    에 넘기고 최상위 `tea.BatchMsg` 는 펼치지 않는다(`runCmd` 만 펼친다, `internal/ui/uitest/harness.go`). 그래서
    `CloseTool` 의 `tea.Batch(ownerClose, guestClose)` 결과를 그대로 Send 하면 오히려 명령이 다 버려진다. 두 번째 안
    (못 돌리는 타입은 Errorf)과 결과 메시지 Send 를 합쳤다. 하네스(uitest)는 건드리지 않았다.
- 재검증(macOS): `go test -count=1 ./internal/ui/workspace/ ./internal/workspace/... ./internal/ui/uitest/` 통과,
  `GOOS=windows go vet ./...` 깨끗, `GOOS=windows go test -c` (fsroot, share, ui/workspace) 컴파일 성공, `gofmt -l .` 비어 있음.
  **Windows CI not yet rerun.**
