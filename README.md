# masil

**masil**(마실)은 이미 열려 있는 터미널 안에서 친구·동료와 채팅하고 가볍게 SNS 도 하는 **터미널 채팅 + SNS** 입니다. 슬랙처럼 방·채널·스레드로 대화하고, 피드·스토리·방명록을 오가며, 다른 창에서 일하다가도 멘션·DM 을 데스크톱 알림으로 받습니다.

## 설치

### Homebrew (macOS · Linux)

```sh
brew install patrache/tap/masil
```

업그레이드는 `brew update && brew upgrade masil` 입니다.

### macOS · Linux — 설치 스크립트

```sh
curl -fsSL https://raw.githubusercontent.com/patrache/masil/main/scripts/install.sh | sh
```

- 기본 설치 경로는 `~/.local/bin/masil` 입니다. `sudo` 는 쓰지 않습니다.
- 내려받은 압축 파일은 `checksums.txt` 의 SHA-256 과 맞아야만 설치합니다.
- `~/.local/bin` 이 `PATH` 에 없으면 셸 설정 파일에 넣을 줄을 알려 줍니다.

### Windows — PowerShell

```powershell
irm https://raw.githubusercontent.com/patrache/masil/main/scripts/install.ps1 | iex
```

- 기본 설치 경로는 `%LOCALAPPDATA%\Programs\masil\masil.exe` 이고, 그 폴더를 사용자 `PATH` 에 추가합니다(새 터미널부터 적용). 관리자 권한은 필요 없습니다.
- Windows PowerShell 5.1 과 PowerShell 7 모두 됩니다.

### 설치 스크립트 환경변수

| 변수 | 뜻 | 기본값 |
|---|---|---|
| `MASIL_VERSION` | 설치할 버전 (예: `0.1.0`, 앞의 `v` 는 있어도 됨) | 최신 릴리스 |
| `MASIL_INSTALL_DIR` | 설치할 폴더 | macOS·Linux `~/.local/bin`, Windows `%LOCALAPPDATA%\Programs\masil` |

```sh
curl -fsSL https://raw.githubusercontent.com/patrache/masil/main/scripts/install.sh | MASIL_VERSION=0.1.0 MASIL_INSTALL_DIR="$HOME/bin" sh
```

```powershell
$env:MASIL_VERSION = '0.1.0'; irm https://raw.githubusercontent.com/patrache/masil/main/scripts/install.ps1 | iex
```

### 직접 내려받기

[Releases](https://github.com/patrache/masil/releases) 에서 내 OS·CPU 에 맞는 `masil_<os>_<arch>` 압축 파일과 `checksums.txt` 를 함께 받습니다.

| OS | CPU | 파일 |
|---|---|---|
| macOS | Apple Silicon / Intel | `masil_darwin_arm64.tar.gz` / `masil_darwin_amd64.tar.gz` |
| Linux | arm64 / x86_64 | `masil_linux_arm64.tar.gz` / `masil_linux_amd64.tar.gz` |
| Windows | arm64 / x64 | `masil_windows_arm64.zip` / `masil_windows_amd64.zip` |

받은 파일을 풀기 **전에** 체크섬을 확인하세요.

```sh
# macOS (Linux 는 shasum -a 256 대신 sha256sum)
grep ' masil_darwin_arm64.tar.gz$' checksums.txt | shasum -a 256 -c
tar -xzf masil_darwin_arm64.tar.gz && mv masil ~/.local/bin/
```

```powershell
# Windows: 두 값이 같아야 합니다
(Get-FileHash .\masil_windows_amd64.zip -Algorithm SHA256).Hash
Select-String 'masil_windows_amd64.zip' .\checksums.txt
```

브라우저로 받은 macOS 바이너리를 macOS 가 실행하지 못하게 막으면 `xattr -d com.apple.quarantine ~/.local/bin/masil` 로 격리 표시를 지우세요(설치 스크립트·Homebrew 로 설치하면 필요 없습니다).

## 시작하기

```sh
masil signup          # 가입 화면에서 아이디·비밀번호를 정합니다
masil login           # 이미 계정이 있으면 로그인
masil                 # 터미널 마실 열기 (masil tui 와 같음)
masil join <코드>     # 초대 코드로 방에 들어가기
```

그 밖의 명령: `masil logout`(이 기기에서 로그아웃), `masil version`, `masil config path`(설정 폴더 위치), `masil help`(명령 목록).

## 화면 모드

창 크기에 따라 레이아웃이 즉시 바뀝니다. 크기는 `칸×줄` 입니다.

| 모드 | 조건 |
|---|---|
| 지휘소 | 200칸 이상 · 50줄 이상 |
| 표준 | 100칸 이상 · 40줄 이상 (지휘소가 아닐 때) |
| 컴팩트 | 100칸 이상 · 40줄 미만 |
| 위젯 | 44~99칸 · 24줄 이상 |
| 초소형 | 그 밖 (43칸 이하, 15줄 이하, 또는 99칸 이하이면서 24줄 미만) |

20×6 보다 작으면 "창을 조금만 키워 주세요" 만 보입니다.

모드 고정: `ctrl+w` 다음 `1` 지휘소 · `2` 표준 · `3` 컴팩트 · `4` 위젯 · `5` 초소형, `ctrl+w 0` 은 자동으로 되돌립니다. 고정한 모드의 최소 크기보다 창이 작으면 자동 모드로 보이고 상태줄에 알려 줍니다. `config.toml` 의 `[layout] pin` 으로도 고정할 수 있습니다.

## 주요 키

| 키 | 동작 |
|---|---|
| `ctrl+k` | 명령 팔레트 — 방·채널·DM·사람으로 이동 |
| `ctrl+f` | 전체 검색 |
| `ctrl+n` | 알림 |
| `?` (`f1`) | 도움말 |
| `f` | 피드 열기/닫기 |
| `t` / `enter` | 선택한 메시지의 스레드 열기 |
| `c` (스레드에서) | 그 스레드의 채널·DM 으로 — 루트 메시지가 선택된 채로 |
| `i` (`a`) | 입력창으로 |
| `enter` / `shift+enter` | 보내기 / 줄바꿈 |
| `esc` | 뒤로 — 한 단계 위로 (스레드 → 그 대화). 입력창에서는 입력창 나가기 (한 번 더 누르면 뒤로) |
| `ctrl+o` / `alt+←` | 이전 위치 — 방금 보던 곳으로 (탭·DM·채널·스레드·피드·알림을 가리지 않음, 채팅 입력 중에도 `ctrl+o`). macOS 에서는 보통 `option+←` 로 들어옵니다 |
| `alt+→` | 다음 위치 (이전 위치를 되돌림) |
| `j` / `k`, `↓` / `↑` | 목록 아래/위 |
| `tab` / `shift+tab` | 다음/이전 창 |
| `ctrl+c` 두 번 (1초 안) | 종료 — 입력창에 글이 있으면 첫 번째는 글 지우기 |

키는 `config.toml` 의 `[keys]` 에서 바꿀 수 있습니다. 전체 목록은 앱 안의 `?` 도움말에 있습니다.

## 알림 데몬

masil 을 꺼 두어도 멘션·DM·초대를 OS 데스크톱 알림으로 받으려면 알림 데몬을 자동 시작에 등록하세요.

```sh
masil daemon install     # 로그인할 때 자동 시작 (macOS launchd · Linux systemd --user · Windows 시작프로그램)
masil daemon status      # 등록·실행·계정·알림 설정·동기화 상태 확인
masil daemon uninstall   # 자동 시작 해제
```

`masil daemon status` 출력 예:

```
masil 알림 데몬
  자동 시작  등록됨 · launchd ~/Library/LaunchAgents/com.patrache.masil.daemon.plist
  실행       실행 중
  계정       @me · https://…
  알림       켜짐
  동기화     11:04 저장 · seq 1041
```

문제가 있으면 같은 출력 아래에 `⚠` 경고(서버 불일치 등)와 `최근 경고`(데몬 로그의 경고·오류 줄)가 붙습니다.

- 등록 위치: macOS `~/Library/LaunchAgents/com.patrache.masil.daemon.plist`, Linux `~/.config/systemd/user/masil-daemon.service`, Windows `HKCU\Software\Microsoft\Windows\CurrentVersion\Run` 의 `masil-daemon` 값.
- macOS·Linux 는 `install` 하면 바로 뜨고 `uninstall` 하면 바로 멈춥니다. Windows 는 다음 로그인부터 뜨므로, 지금 바로 띄우려면 `masil daemon run` 을 한 번 실행하세요(창 없이 백그라운드로 뜹니다). Windows 에서 `uninstall` 해도 지금 떠 있는 데몬은 로그아웃할 때까지 남습니다.
- **TUI 가 떠 있으면 데몬은 알리지 않습니다** — 같은 기기에서 masil 이 열려 있으면 masil 이 직접 알립니다. 같은 알림이 두 번 뜨지 않습니다.
- **집중 모드** 중에는 알림을 보류했다가, 집중 모드가 끝날 때 요약 알림(예: "집중 모드 끝 — 멘션 3, DM 2")을 한 번만 띄웁니다.
- 노트북을 재웠다 깨우면 놓친 알림 중 10분 이내의 것만 띄웁니다.
- 로그: `~/.config/masil/logs/daemon.log` (5MB 를 넘으면 `daemon.log.1` 로 한 번 회전).
- 문제를 직접 보려면 자동 시작 데몬을 멈추고(`masil daemon uninstall`, 데몬은 한 기기에 하나만 뜹니다) 앞에서 띄워 보세요. 확인이 끝나면 `masil daemon install` 로 다시 등록합니다.

  ```sh
  masil daemon run --foreground --verbose   # 로그를 터미널에도 찍음, ctrl+c 로 종료
  ```

### 업그레이드

`brew upgrade masil` 이나 설치 스크립트를 다시 실행한 뒤, macOS·Linux 에 등록된 데몬은 실행 파일이 바뀐 것을 감지해 **새 버전으로 스스로 다시 뜹니다**. `masil daemon status` 로 확인하고, 그래도 안 바뀌었으면 `masil daemon install` 을 다시 실행하세요 — 재등록이 실행 중인 데몬을 새 버전으로 다시 띄웁니다. Windows 는 다음 로그인부터 새 버전이 뜹니다.

## 설정

설정 폴더는 **모든 OS 에서** `~/.config/masil/` 입니다(Windows 는 `%USERPROFILE%\.config\masil`). 환경변수 `MASIL_CONFIG_DIR` 로 바꿀 수 있습니다. 위치는 `masil config path` 로 확인합니다.

`config.toml` 의 주요 키:

```toml
server = "https://…"       # 접속할 서버
theme = "amber"            # charm | midnight | cream | amber

[notify]
desktop = true             # OS 데스크톱 알림 (masil 과 알림 데몬 모두)
```

서버 주소는 `MASIL_SERVER` 환경변수 > `config.toml` 의 `server` > 내장 기본값 순서로 정합니다.

> **자동 시작된 알림 데몬은 셸 환경변수를 보지 않습니다.** 셸에서 `MASIL_SERVER=… masil login` 으로 다른 서버에 로그인하면, 자동 시작된 데몬은 여전히 `config.toml` 의 `server`(없으면 내장 기본값)에 접속하므로 알림이 오지 않습니다. 서버를 바꿀 때는 **`config.toml` 의 `server` 를 고치세요.** `masil daemon install` 과 `masil daemon status` 는 셸의 `MASIL_SERVER` 와 데몬 쪽 주소가 다르면 경고합니다.

## 제거

1. **먼저** `masil daemon uninstall` 로 자동 시작을 해제합니다(실행 파일을 먼저 지우면 자동 시작 등록만 남습니다).
2. Homebrew 로 설치했다면 `brew uninstall masil`, 아니면 실행 파일(`~/.local/bin/masil` 또는 `%LOCALAPPDATA%\Programs\masil`)을 지웁니다.
3. 계정 정보·설정까지 지우려면 `~/.config/masil` 폴더를 지웁니다.

## 문제 해결

- **macOS 에서 알림이 안 뜸**: masil 은 `/usr/bin/osascript`(AppleScript)로만 알림을 띄웁니다(`terminal-notifier` 등 다른 도구는 쓰지 않음). 알림은 **"스크립트 편집기"** 이름으로 뜨므로 시스템 설정 → 알림 에서 "스크립트 편집기" 알림을 허용하세요.
- **Linux 에서 알림이 안 뜸**: masil 은 D-Bus 세션 버스로 데스크톱 알림 데몬(`org.freedesktop.Notifications`, GNOME·KDE 등에 기본 포함)에 직접 알림을 보냅니다. `notify-send` 등 다른 프로그램으로 대신 시도하지 않고, 세션 버스를 새로 띄우지도(`dbus-launch`) 않습니다 — 알림 데몬과 세션 버스(`DBUS_SESSION_BUS_ADDRESS`)가 있어야 합니다. 화면 없는 서버에서는 알림이 뜨지 않습니다.
- **알림이 전혀 안 옴**: `masil daemon status` 를 보세요. 로그인한 서버와 데몬이 쓰는 서버가 다르다는 `⚠` 경고가 있으면 같은 서버로 다시 로그인하거나 `config.toml` 의 `server` 를 맞추세요([설정](#설정) 참고). `알림 꺼짐` 이면 `config.toml` 의 `[notify] desktop` 을 `true` 로 바꾸세요.
- **테두리가 어긋나거나 한글·상자 문자가 겹침**: iTerm2 의 "Ambiguous characters are double-width"(Profiles → Text)를 끄세요. 모호 폭 문자를 2칸으로 그리는 터미널 설정은 지원하지 않습니다.
- **한글이 "명 령  팔 레 트" 처럼 띄엄띄엄 보임**: 터미널 글꼴에 한글이 없어서, 터미널이 대신 가져온 더 좁은 글꼴의 글자가 2칸 자리에 놓인 것입니다. 한글이 든 고정폭 글꼴(D2Coding, Sarasa Mono K, 나눔고딕코딩 등)로 바꾸거나, 터미널 설정에서 그 글꼴을 비 ASCII 문자용 글꼴로 지정하세요(iTerm2: Profiles → Text → "Use a different font for non-ASCII text").

## 로컬 개발

필요한 것: Go 1.26, Node 24.

```sh
# 서버 (로컬 Workers 시뮬레이션 — 로컬 D1·R2·Durable Objects)
cd server && npm install && npx wrangler d1 migrations apply DB --local && npx wrangler dev --test-scheduled
# → http://localhost:8787

# 클라이언트 (다른 터미널에서, 저장소 루트)
# 버전을 넣지 않은 개발 빌드는 masil/0.0.0-dev 라서, 서버의 MIN_CLIENT_VERSION(0.1.0)에 걸려
# "masil을 업데이트해 주세요 (최소 0.1.0)" 가 나온다. 개발 빌드에는 버전을 넣어 실행한다.
MASIL_SERVER=http://localhost:8787 go run -ldflags "-X github.com/patrache/masil/internal/version.Version=0.1.0-dev" ./cmd/masil

# 시험
go test ./...
cd server && npm test
```

운영 서버 배포와 원격 D1·R2 작업은 오너 승인 사항이라 이 문서에 명령을 적지 않습니다.

## 라이선스

MIT — [LICENSE](LICENSE) 를 보세요.
