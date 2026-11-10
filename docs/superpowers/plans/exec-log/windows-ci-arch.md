# Windows CI — arch 그룹 (fix/v013-win-arch)

CI 로그: windows-latest run 42760118078. **Windows CI not yet rerun** — 아래 결과는 전부 macOS 로컬과 `GOOS=windows` 교차 검사로 본 것이다.

## 실패한 테스트

| 테스트 | 증상 (Windows만) | 원인 | 처리 |
|---|---|---|---|
| `internal/archsim` TestPureImports | archdoc, archsim, archsim/pricing, archsim/challenge, archdraw, archexport "imports net" | `internal/config/rename_windows.go` 가 errno 상수 3개 때문에 `golang.org/x/sys/windows` 를 import. 그 패키지는 `types_windows.go` 의 `SocketAddress.IP() net.IP` 때문에 `net` 을 import. 6개 패키지 모두 `archdoc → config` 를 거치므로 Windows 빌드에서만 `net` 이 의존성에 들어옴 | 제품 코드 수정 (ec80742b) |
| `internal/archsim/pricing` TestNoNetworkImports | "pricing depends on net" | 같은 간선 (`pricing → archdoc → config → x/sys/windows → net`) | 같은 커밋 |

확인 방법: `GOOS=windows go list -deps -f '{{.ImportPath}}: {{join .Imports " "}}' ./internal/archsim/pricing` 에서 `net` 을 import 하는 패키지는 `golang.org/x/sys/windows` 하나뿐이고, 그것을 import 하는 것은 `internal/config` 하나뿐. darwin/linux 에는 `net/netip`, `net/url` 만 있고 `net` 은 없다.

## 수정

- `rename_windows.go`: `x/sys/windows` 대신 표준 `syscall.ERROR_ACCESS_DENIED`, `syscall.ERROR_FILE_NOT_FOUND`, 그리고 로컬 상수 `errSharingViolation syscall.Errno = 32` (ERROR_SHARING_VIOLATION, 표준 syscall 에는 없음). 재시도 판정 동작은 그대로.
- `rename_windows_test.go`: `errSharingViolation == windows.ERROR_SHARING_VIOLATION` 단언 추가 (테스트 전용 import 는 `go list -deps` 에 안 잡힌다).
- 두 import 테스트를 강화: host GOOS 만이 아니라 darwin, linux, windows 각각으로 `go list -deps`. 이제 macOS/Linux CI 도 이런 OS별 간선을 잡는다. 수정 전 코드로 macOS 에서 돌려 Windows 로그와 같은 7줄 실패가 나는 것을 확인함.

내 판단: 테스트를 느슨하게(예: Windows 에서 `net` 허용) 하지 않고 간선을 없앴다. `net` 은 여기서 실제 네트워크 호출이 아니라 타입 하나 때문에 끌려온 것이라 "의도 위반"은 아니지만, 끊는 비용이 상수 하나라 테스트의 뜻(순수 패키지는 net 에 닿지 않는다)을 그대로 지키는 쪽이 맞다.

내 판단: 테스트를 skip 하지 않았다. 순수성 규칙은 Windows 에도 똑같이 있다 (P3 Task 27, CA 2.1·2.2).

## 검증 (macOS, 2026-11-10)

- `GOOS=windows go vet ./...` — 출력 없음, exit 0
- `GOOS=windows go test -c -o /dev/null` — internal/config, internal/archsim, internal/archsim/pricing 모두 컴파일 됨
- `go test -count=1 ./internal/config/... ./internal/archsim/... ./internal/archdoc/... ./internal/archdraw/... ./internal/archexport/...` — 전부 ok
- 6개 패키지 `GOOS=windows go list -deps` 에 `net`, `net/http`, `os/exec` 없음
- `gofmt -l` 깨끗, `go mod tidy -diff` 변경 없음 (`x/sys` 는 secrets 등에서 계속 직접 의존성)

## 아직 불확실 (다음 Windows CI 까지)

- `TestIsTransientRenameErrWindows` 는 Windows 에서만 돌므로 새 상수 단언은 Windows CI 에서 처음 실행된다 (값 32 는 winerror.h 와 x/sys 소스로 확인).
- 강화된 테스트는 Windows 러너에서 `GOOS=darwin`/`GOOS=linux` 로 `go list` 를 돈다. 교차 GOOS `go list` 는 빌드를 안 하므로 문제없을 것으로 보지만 Windows 에서 직접 본 적은 없다. 테스트 시간은 go list 3배 (macOS 기준 약 0.7초).
- 같은 로그의 다른 그룹 실패는 이 브랜치에서 다루지 않았다.
