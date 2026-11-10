# P5 0단계 통합 기록

- 통합 브랜치: `dev/v0.1.3` (워크트리 `.claude/worktrees/dev-v013`), 2026-11-09. 병합 전 기준 `0515f83`(keychain purego 병합 뒤).
- 병합: `feat/v013-p5-s0` (브랜치 끝 `d50c2ab`, 기준 `67c55df`) → 머지 커밋 `ff2c447` (`--no-ff`). 들어온 커밋 9개: Task 1(축소)·2·4·6a + 리뷰 반영 5개.
- push·tag·배포·원격 wrangler 없음. 작업 브랜치·워크트리는 지우지 않았다.

## 충돌과 해결

- 충돌 없음. 사이에 들어온 keychain purego 커밋(`internal/secrets/*`, `go.mod`)과 파일이 겹치지 않았다.
- i18n 생성물: 병합 뒤 `go generate ./internal/i18n` 을 돌려 `git status` 가 깨끗함을 확인. 경고는 zh-Hans 등 누락 번역뿐.
- 골든: `git diff --name-only 0515f83 HEAD -- '*.golden'` 출력 없음(바뀐 골든 0). `server/` 변경 없음.

## 이제 쓸 수 있는 인터페이스

- `internal/errtext`: `func Describe(t *i18n.T, err error) string` — nil err → `""`, nil t → ko, 결과는 늘 sanitize. Task 6 본편 전까지 모든 언어에 0.1.2 한국어 줄.
- `internal/api`: `type Explanation struct{ Code, Field, Reason string; Limit string; MinVersion string; RetryAfterSec int }`, `func Explain(err error) Explanation`, `func DescribeKo(x Explanation) string`(임시 다리). `api.Describe` 는 남아 있음.
- `internal/i18n`: `func Selectable() bool` (Pseudo 제외 실제 언어가 둘 이상 열렸을 때 참).
- `internal/ui`: `type SetLocaleMsg struct{ Choice string }`(앱이 처리, 뿌리지 않음), `type LocaleChangedMsg struct{ From, To i18n.Locale }`(From ≠ To 일 때만 뿌림).
- `internal/ui/app`: `func (m *Model) Locale() i18n.Resolution`, `Box.ID string`.
- `internal/ui/uitest`: `(*Harness).SetLocale(choice string)`, `var Tiny = Size{40, 14}`, `PseudoRegions(h *Harness) map[string]string`, `AssertPseudoWrapped(tb testing.TB, region, text string, allow []string)`, `PseudoAllow(h *Harness) []string`; `uitest/fixtures.UserData() []string`.
- `internal/ui/e2e`: `TestPseudoSweep`, `TestPseudoAfterSwitch`, `TestPseudoPendingNoStale`, 대기 목록 `testdata/pseudo_pending.txt`(13개 영역).

## 내 판단

- 내 판단: 병합 전에 dev 가 `67c55df` 에서 `0515f83` 으로 앞서 있었지만(keychain purego) 겹치는 파일이 없어 다시 기준을 맞추지 않고 그대로 `--no-ff` 병합했다.
- 내 판단: 실행 중 언어 바꾸기에서 `MASIL_LANG` 이 이긴다(설계서 6.2, 리뷰 반영 `a9944cb`). 앱 안에서 고른 언어가 `MASIL_LANG` 을 이겨야 하는지는 오너 확인 거리로 남긴다(세부는 `P5.md`).
- 내 판단: 통합 중 고칠 깨짐이 없어 통합용 수정 커밋은 없다.

## 검증 (머지 커밋 `ff2c447` 기준, 이 세션)

- `go build ./...` → BUILD_OK
- `go test -count=1 ./...` → rc=0, ok 패키지 53개, FAIL 없음
- `go vet ./...` / `GOOS=windows go vet ./...` / `GOOS=linux go vet ./...` → VET_OK / VET_WIN_OK / VET_LINUX_OK
- 바뀐 .go 파일 `gofmt -l` → 출력 없음
