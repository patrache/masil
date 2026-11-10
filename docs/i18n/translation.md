# masil 번역 안내 (Translation guide)

카탈로그 문구를 쓰고 번역·검수하는 규칙이다. 근거: `docs/superpowers/specs/2026-11-09-masil-v0.1.4-i18n-design.md`(6.4~6.7, 9.5), P5 계획 Global Constraints.

## 파일과 생성

- 문구는 `internal/i18n/locales/<언어>/<영역>.toml` 에 있다. 언어: `ko`(원문) · `en` · `zh-Hans`. 개발용 `x-pseudo` 는 `MASIL_LANG=x-pseudo` 로만 켠다(목록에 없음).
- ko 파일이 키의 원본이다: 키·`desc`(번역자용 맥락)·`params`·`max`·`short`·`no_truncate`·`legal` 은 ko 에만 쓴다. en·zh-Hans 파일에는 `other`(en 은 복수형이 있으면 `one` 도)와 `short` 만 쓴다.
- 키 이름은 `snake_case`, `<화면>_<무엇>`. 원본 TOML 을 고친 뒤 `go generate ./internal/i18n` — 생성 파일(`zz_*_gen.go`)은 손대지 않는다.
- 비어 있는 번역은 경고일 뿐 실패가 아니다(대체 순서 `zh-Hans → en → ko`, `en → ko`). 출시 관문에서는 누락 0 이어야 한다.

## 말투

- ko: 해요체("~했어요", "~할 수 없어요"). 사람 이름은 `{name}님` 붙여 쓰기, 그 뒤 조사는 고정(`이/은/을/과`). 그 밖의 조사는 표시 `{x|이/가}` — ko 파일에서만.
- en: 짧고 친근하게, 문장형 대문자(Sentence case), 버튼·라벨에는 마침표 없음, 성별 대명사 없음(`they` 나 이름). 복수형은 `one`/`other`.
- zh-Hans: 간결하게, 상대는 `你`. 문장에는 전각 문장부호(`，。：`), 라벨 끝에는 문장부호 없음. 가운뎃점은 `·`(전각 `・` 금지). 복수형 없음, 양사는 문구 안에.
- '님'·존칭은 en·zh-Hans 에서 없앤다(Mr., 先生 쓰지 않음).
- 문구에 스타일·ANSI·제어 문자·양방향 제어 문자·탭을 넣지 않는다. 강조는 인자로 넘기고 코드가 스타일을 입힌다.
- 인자는 이름으로만 쓴다(`{actor}`) — 어순은 언어마다 자유.

## 번역하지 않는 것

슬래시 명령 이름(`/poll`), CLI 하위 명령, 키 이름(`enter`, `ctrl+k`), 제품 이름 masil, HTTP 메서드·상태 문구, LeetCode 제목·난이도 값, 사용자 글, 서버가 보낸 모르는 오류 코드·칸 이름, `internal/admincli`(운영자 CLI — 영구 한국어), 로그 문구.

## 폭 규칙

- 좁은 자리(초소형·위젯 탭, 상태줄 모드 이름·단축키 힌트, 버튼, 표 머리, 설정 줄 이름, 시간 표기)의 키에는 `max`(칸 수)가 있다. 생성기가 세 언어 모두 실제 폭으로 재서 넘으면 실패한다.
- 넘칠 때는 `short` 를 먼저 쓰고, 그래도 넘치면 화면이 `…` 로 자른다. 단어 중간이 잘리는 것보다 짧은 형이 낫다.
- 표 열 너비는 그 열 문구 중 가장 넓은 것으로 계산된다 — 번역이 길어지면 열이 넓어진다.
- 한글·한자는 한 글자 2칸, `·` `…` `—` 는 1칸.
- 상대 시각(`time.relative_*`)은 8칸 이하.

## `no_truncate` 와 `legal`

- `no_truncate = true`: 보안 경고, 확인 대화상자의 "무엇을 하는지" 문장, 오류의 핵심 문장. 화면이 자르지 않고 줄바꿈한다 — 길어도 되지만 뜻이 빠지면 안 된다.
- `legal = true`: 경고·법적·개인정보 설정 문구. 검수자는 "뜻이 원문과 정확히 같은지"를 따로 확인한다.

## 번역 흐름 (설계서 9.5)

1. 개발자가 키를 만든다: ko + `desc` (+ `max`/`short`), en 도 같은 PR 에서. zh-Hans 는 비워도 된다.
2. 비어 있는 en·zh-Hans 는 어시스턴트가 묶어서 초벌한다. 생성기가 번역자용 표(키, ko, en, zh, desc, max, 그 문구가 보이는 골든 경로)를 뽑는다.
3. 원어민 검수: en 1명, zh-Hans 1명. 표와 언어별 골든(실제 화면)을 보고 고친다.
4. 검수 장부 `internal/i18n/locales/<언어>/.reviewed.json`: 검수한 키 → (ko 원문 + 번역문)의 해시. 원문이나 번역이 바뀌면 다시 검수 대상이 된다.
5. 용어는 `docs/i18n/glossary.md` 를 따른다.
6. `legal` 문구는 따로 확인한다.

## 검수 장부 명령 (P5 Task 25)

생성기 `internal/i18n/gen` 의 플래그 — Task 25 가 만든다:

- `go run ./internal/i18n/gen -report` — 언어별 누락·검수율 보고
- `go run ./internal/i18n/gen -table <언어>` — 번역자용 표(Markdown/CSV)
- `go run ./internal/i18n/gen -mark-reviewed <언어> <키…>` — 검수 장부에 기록
