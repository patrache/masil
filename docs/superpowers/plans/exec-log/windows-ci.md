# v0.1.3 Windows CI — 통합 기록

GitHub Actions windows-latest run 42760118078 (Go 1.26.6) 실패를 네 그룹으로 나눠 고친 뒤 dev/v0.1.3 에 합쳤다.
Windows 기기는 없다. **Windows CI not yet rerun** — 아래 "고침"은 macOS 실행, `GOOS=windows` vet·테스트 컴파일,
코드 읽기로 확인한 것이지 Windows 에서 본 결과가 아니다.

그룹별 상세: `windows-ci-arch.md`, `windows-ci-git.md`, `windows-ci-api.md`, `windows-ci-ws.md` (같은 폴더).

## 합치기 (dev/v0.1.3, 기준 a9276cc1, 모두 `--no-ff`, 충돌 없음)

| 순서 | 브랜치 | merge 커밋 | 들어온 커밋 |
|---|---|---|---|
| 1 | fix/v013-win-arch | 6d64f910 | ec80742b, d8eaed0f |
| 2 | fix/v013-win-git | 05ffb420 | 8e0d0383, 698f2289, 843ed9b0, 2a621695 |
| 3 | fix/v013-win-api | 580b3209 | 25a7319f, e00683b9, c84096c4 |
| 4 | fix/v013-win-ws | 26c0d365 | 569e986a, b98ad1bf, b1bc0664, 9fc88e12, 41d7c998, 2dc38ba9, 8e885ec7, 36d7aec2, 28e6211d |

i18n 생성물은 어느 브랜치도 바꾸지 않았다. 합친 뒤 `go generate ./internal/i18n` 를 돌려도 작업 트리에 차이가 없었다.

그룹 사이 겹침 확인: git 그룹의 `termpanetest/fake.go` (가짜 패널이 `filepath.Base(program)` 을 그림)는
ws 그룹의 가짜 패널 테스트에도 쓰인다. 합친 뒤 macOS 에서 `internal/ui/workspace`, `leet`, `e2e` 전부 통과했고
골든 파일도 바뀌지 않았다. ws 의 `solo_windows.golden` 은 패널이 없는 화면이라 이 변경의 영향을 받지 않는다.

## 실패 패키지와 원인 (로그의 FAIL 패키지 8개, `--- FAIL` 테스트 84개 — 전부 아래 그룹이 맡음)

| 패키지 | 실패 테스트 | 원인 | 분류 | 처리 (커밋) |
|---|---|---|---|---|
| internal/archsim | TestPureImports (6개 패키지 "imports net") | `config/rename_windows.go` 가 `x/sys/windows` 를 import 하고, 그것이 `net` 을 끌어옴 (archdoc→config 경로) | 제품 (Windows 전용 의존성) | 표준 `syscall` 상수와 로컬 `errSharingViolation=32` 로 바꿈. 테스트는 darwin·linux·windows 세 OS 의 deps 를 모두 검사하도록 강화 (ec80742b) |
| internal/archsim/pricing | TestNoNetworkImports | 같은 원인 | 제품 | 같은 커밋 ec80742b, 테스트 강화 |
| internal/lazygit | TestLocateConfigPathMissing, TestResolveRepoOrder | 픽스처의 `/opt/...` 처럼 드라이브 없는 경로가 Windows 의 `filepath.Abs` 에서 `D:\...` 가 됨. 제품 동작은 맞음 | 테스트 (Unix 경로 가정) | `native()` 로 드라이브가 붙은 픽스처를 쓰고, 가짜 git 은 `C:/...` 형식으로 답함. 새 테스트 TestLocateMakesAbsolute, TestRepoRootAbsoluteNative. 빈 출력 검사 강화 (8e0d0383) |
| internal/ui/gittool | TestOpenTwoPresses, TestPaneThemeOverlay/pane (panic → 나머지 테스트 안 돎) | 같은 경로 원인. 게다가 `Started()[0]` 를 바로 꺼내서 panic | 테스트 | `native()` 경로, `firstStarted` 도우미 (698f2289). 가짜 패널은 base name 을 그리므로 OS 마다 골든을 따로 둘 필요가 없음 (2a621695, 패널 줄만 바뀐 macOS 골든 4개를 다시 만듦) |
| internal/ui/apitool | TestExportCurlCopies, TestSaveBodyToFile, TestSaveBodyAsksBeforeOverwrite | Windows 에는 0600 이 없음 (Go 는 0666 이나 0444 로 보고함). 저장 자체는 성공 | 테스트 (Unix 권한 가정) | `assertPrivateMode`: Unix 는 정확히 0600, Windows 는 읽기 전용이 아닌 일반 파일 0666 인지 확인. Windows 에서 파일을 지키는 것은 폴더 ACL 상속 (`%USERPROFILE%\Downloads`) (25a7319f) |
| internal/workspace/fsroot | TestReadTextLinkDotDotReadsOSTarget | `follow` 가 링크 대상의 `..` 를 Unix 방식으로 풀었음. Windows 와 os.Root 는 글자 그대로 정리함. Check 와 Create/Rename/Remove 가 서로 다른 파일을 볼 수 있었음 | **제품 (보안 판정 불일치)** | Windows 에서 `windowsLinks` 로 글자 그대로 정리 (569e986a). OS 가 실제로 여는 파일과 대조하는 `assertOSOpens` 추가 (8e885ec7). TestCheckLinkDotDotFollowsOS 는 Windows 에서 Skip: 그 OS 에는 이 Unix 링크 의미가 없고, Windows 기대값은 TestCheckLinkDotDotWindowsModel 이 확인함 |
| internal/workspace/share | TestSaveAtomicAndAudited | 0640 이 Windows 에서 0666 으로 보임 | 테스트 | 모든 OS 에서 "저장 전과 같은 모드"를 확인하고, 정확한 0640 은 Windows 가 아닌 OS 에서만 확인 (b98ad1bf). 비밀 보호 단언이 아니라 모드 유지 단언임 |
| internal/ui/workspace | 약 70개 (TempDir 정리 실패 "being used by another process", "shared terminals = 0", 골든 `n 새 터미널` 차이 등) | C: 테스트가 Surface 와 감사 로그를 닫지 않음. E: 가짜 패널 테스트가 Windows 에서 `paneSupported()=false` 인 채로 돎 | 테스트 하네스 (C, E) | `closeLiveAtEnd` 와 `runCmds` 로 앱과 같은 방식으로 닫음 (b1bc0664, 36d7aec2). `setup` 이 패널 지원을 켬. Windows 화면은 `solo_windows.golden` 과 패널 없는 테스트 4개로 모든 OS 에서 고정 (41d7c998) |
| internal/ui/workspace | (실패 없음, 고치다가 발견) | D: Windows 공유 대화상자에 "터미널 공유 불가" 이유가 한 번도 보이지 않음 (스펙 B Windows, 인수 기준 20) | **제품** | 이유를 "공유할 것" 아래에 보임. 새 테스트 TestWindowsShareDialogNoTerminals (9fc88e12) |

ui/workspace 에서 실패한 테스트별 C/E 분류는 `windows-ci-ws.md` 의 "internal/ui/workspace" 절에 있다. 로그의
`--- FAIL` 이름 목록과 대조했고, 빠진 테스트는 없다.

## 검증 (이 Mac, darwin/arm64, go1.26.6, merge 뒤 HEAD 26c0d365)

- `go build ./...`: 성공
- `go test -count=1 ./...`: exit 0, 84개 패키지 ok, FAIL 없음
- `GOOS=darwin go vet ./...`, `GOOS=windows go vet ./...`, `GOOS=linux go vet ./...`: 셋 다 exit 0, 출력 없음
- `GOOS=windows go test -c -o /dev/null`: 바뀐 패키지와, 바뀐 테스트 도우미(termpanetest, config, fsroot, apitool)를
  import 하는 패키지 30개가 모두 컴파일됨
- `gofmt -l` (a9276cc1..HEAD 에서 바뀐 .go 파일): 출력 없음
- `git status --porcelain -- '*.golden'`: 출력 없음 (테스트를 돌려도 골든이 바뀌지 않음)
- `go generate ./internal/i18n`: 작업 트리에 차이 없음

## 내 판단

- 내 판단: 네 브랜치는 서로 다른 파일을 고쳤고, 공유하는 것은 `termpanetest/fake.go` 하나다. 그래서 충돌 없이 합쳐졌고,
  합친 뒤 macOS 전체 테스트가 그대로 통과한 것이 이를 뒷받침한다.
- 내 판단: 제품 코드 수정은 세 군데뿐이다. arch (Windows 에서 config 의 의존성), fsroot (Windows 링크 모델),
  ui/workspace 공유 대화상자 (Windows 이유 표시). 나머지는 Unix 가정에 맞춰 짠 테스트를 고친 것이고, 건너뛴 테스트는
  TestCheckLinkDotDotFollowsOS 하나다. 그 Windows 대응 테스트는 따로 있다.
- 내 판단: 보안 단언을 몰래 약하게 만든 곳은 없다. apitool 의 0600 은 Windows 에서 0666 일반 파일인지 확인하는
  것으로 바꿨고, 파일을 지키는 것은 폴더 ACL 이라고 기록했다. 다만 그 ACL 자체는 시험하지 않는다 (아래 오너 결정).

## 남은 것 (다음 Windows CI 까지 불확실)

- **Windows CI not yet rerun.** 위 8개 패키지가 통과하는지는 다음 실행에서 봐야 한다.
- 이번 실패에서 panic 이나 앞선 단언에 막혀 Windows 에서 한 번도 돌지 않은 부분이 처음 돌게 된다. gittool 의 나머지
  테스트 (picker, probe, golden), apitool 의 큰 응답 이동과 rename 덮어쓰기, ui/workspace 의 TestAttentionFromHook
  (`hooks.Listen`) 등이다.
- fsroot: 일반 열기(커널)가 상대 대상 `s\..\config` 를 os.Root 와 같이 글자 그대로 푸는지는 Windows 에서 보지 못했다.
  다르면 `assertOSOpens` 가 커널과 os.Root 중 어느 쪽이 다른지 알려 준다. 그 경우 모델을 고르기 전에 오너께 여쭌다.
- arch: 새 `errSharingViolation == windows.ERROR_SHARING_VIOLATION` 단언은 Windows 에서만 돈다. 강화한 deps 테스트는
  Windows 러너에서 `GOOS=darwin` 과 `GOOS=linux` 로 `go list` 를 돌린다 (빌드는 하지 않음).
- git: `native()` 는 러너에 `C:\r`, `C:\usr\local\bin`, `C:\cfg` 가 없다고 가정한다. 실제 Windows lazygit 이면 상태줄에
  `lazygit.exe` 가 나오는 겉모양 문제가 있다 (고치지 않음).
- **오너 결정 필요 (api)**: P4 계획서 569행과 595행은 OS 구분 없이 0600 이라고 적혀 있다. Windows 에서 사용자가
  공유 폴더(`C:\Users\Public` 등)에 저장하면 같은 PC 의 다른 사용자가 읽을 수 있다. 선택지는 두 가지다.
  (가) 계획서에 "0600 (windows 제외: 폴더 ACL 상속)" 을 적는다.
  (나) 소유자 전용 DACL 을 거는 공통 함수를 만들어 masil 의 모든 0600 파일에 적용한다.
- ws 제품 한계 (확인 안 함, 고치지 않음): Windows 에서 원자 저장을 하면 파일에 따로 준 ACL 이 사라진다. 읽기 전용
  파일은 rename 덮어쓰기가 거부될 수 있다. 폴더를 연 동안 탐색기에서 그 폴더를 지우지 못할 수 있다.
- 푸시, 태그, 배포는 하지 않았다.
