# masil — 계획/구현 공통 브리프 (모든 에이전트의 단일 출처)

- 설계서(최우선 출처): `docs/superpowers/specs/2026-11-05-masil-design.md` — 반드시 처음부터 끝까지 읽을 것.
- 화면 시안(HTML): `.superpowers/brainstorm/35477-1793846636/content/visual-style.html`(테마 4종), `.superpowers/brainstorm/35477-1793846636/content/layouts.html`(창 크기별 4모드). 색상값·배치는 이 시안의 CSS 변수가 기준.
- 레포 루트: `/Users/sam/Develop/test/terminal-sns` (GitHub `patrache/masil`, private). 문서 언어: 한국어. 코드 식별자·주석·커밋 메시지는 영어.

## 레포 구조 (확정)

```
go.mod                       # module github.com/patrache/masil  (Go 1.26)
cmd/masil/                   # 단일 바이너리 진입점: `masil` (TUI), `masil daemon ...`, `masil admin ...`, `masil login/signup/logout`
internal/...                 # Go 클라이언트 패키지들 (구조는 docs/contract/client-architecture.md 가 확정)
server/                      # Cloudflare Worker (TypeScript, Hono) — package.json, wrangler.jsonc, src/, migrations/, test/
docs/contract/api.md         # REST API 계약 (단일 출처)
docs/contract/events.md      # WebSocket 실시간 이벤트 계약 (단일 출처)
docs/contract/data-model.md  # D1 테이블·인덱스·R2 키 규칙 (단일 출처)
docs/contract/client-architecture.md  # Go 클라이언트 패키지 구조·핵심 인터페이스 (단일 출처)
docs/superpowers/plans/      # 구현 계획들
```

## 확인된 최신 버전 (2026-11-05 기준 `npm view` / `go list -m -versions`로 확인)

- Node: wrangler 4.157.0, hono 4.13.16, @cloudflare/vitest-pool-workers 0.23.0, vitest 5.1.0(※ pool-workers의 peerDependency가 허용하는 vitest 버전을 `npm view @cloudflare/vitest-pool-workers peerDependencies`로 확인해 맞출 것), typescript 7.0.2, @cloudflare/workers-types 5.20261104.1
- Go 1.26.1: charm.land/bubbletea/v2 v2.1.1, charm.land/lipgloss/v2 v2.0.6, charm.land/bubbles/v2 v2.2.1, github.com/charmbracelet/glamour v1.0.0, github.com/alecthomas/chroma/v2 v2.27.0, github.com/coder/websocket v1.8.15, github.com/mattn/go-runewidth v0.0.31, github.com/gen2brain/beeep v0.11.3
- Bubble Tea/Lip Gloss/Bubbles는 **v2 (charm.land 경로)** 를 쓴다. v2 API는 v1과 다르다(예: View 반환 타입, 키 메시지 타입, 색상 처리). **기억에 의존하지 말고** `go doc charm.land/bubbletea/v2` 등으로 실제 API를 확인할 것. teatest의 v2 호환 경로도 직접 확인할 것.
- Cloudflare API(DO Hibernation WebSocket, D1, R2, Rate Limiting 바인딩, Cron)는 `cloudflare` / `durable-objects` / `wrangler` 스킬 또는 공식 문서로 확인할 것.

## 오너 확정 사항 (설계서 요약 — 충돌 시 설계서가 우선)

- Cloudflare 계정은 Workers Paid. 실제 배포(`wrangler deploy`, D1/R2 생성)는 오너 건별 승인 필요 → 계획/구현 단계에서는 **로컬(`wrangler dev`, vitest)만**. 계획서에 배포 단계를 넣을 땐 "오너 승인 후"라고 명시.
- 테마 4종(A Charm 보라 / B 미드나잇 블루 / C 크림 파스텔 / D 레트로 앰버), 기본 D.
- 창 크기별 4모드(지휘소/표준/컴팩트/위젯) + 초소형 축약. 어떤 크기에서도 깨지지 않음, 모드 전환 시 입력·스크롤·선택 유지.
- masil이 꺼져 있어도 알림: `masil daemon` + OS 자동 시작 등록, TUI 실행 중엔 중복 알림 없음.
- 배포: GitHub Releases 바이너리(goreleaser) + 설치 스크립트 + Homebrew tap. 레포는 개발 중 비공개.
- 금지: 토큰·비밀정보를 코드/문서에 하드코딩 금지. 비밀은 `.dev.vars`(gitignore됨) / `wrangler secret`.
