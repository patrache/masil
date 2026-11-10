# P5 2묶음 통합 기록 (p5-b2)

- 날짜: 2026-11-10. 워크트리 `.claude/worktrees/dev-v013`, 브랜치 `dev/v0.1.3`, 기준 `c90dbe96`(P5 1묶음 통합 기록).
- 색인 3절 순서: P5 2묶음(Task 11~16).
- push·tag·배포·원격 wrangler·유료 리소스: 없음. 브랜치·워크트리는 지우지 않았다.

## 병합 (`--no-ff`)

| 순서 | 브랜치 | 줄기 커밋 | 병합 커밋 | 충돌 |
|---|---|---|---|---|
| 1 | `feat/v013-p5-b2` (P5 Task 11~16 + 검토 반영) | `8cfce5f0`…`cc8425d6` (9개) | `cdae8c83` | 없음 |

줄기 커밋:

- `8cfce5f0` Task 11 fun
- `66cacabf` Task 12 feed ①
- `d44adec6` Task 13 feed ②
- `8be260ed` Task 14 chat ①
- `23128d5c` Task 15 chat ② + markup 꼬리 문구
- `e2046b6f` Task 16 chat ③
- `b0e7dfe1` 검토 반영: 확인 `*_body` 9개 `no_truncate`
- `935cb39d` 검토 반영: 좁은 자리 chat 키 52개에 `max`
- `cc8425d6` 검토 반영: `humanize.Formatter.MonthDay` 내보내기를 되돌림

### 충돌과 해결

- 충돌 없음. 줄기가 `dev/v0.1.3` 머리(`c90dbe96`) 위에서 갈라졌기 때문이다.
- `internal/i18n/legacy_literals.txt`: 줄기 쪽만 바뀌었다. `internal/markup 3`·`internal/ui/chat 573`·`internal/ui/feed 342`·`internal/ui/fun 85` 네 줄이 지워졌다.
- `internal/ui/e2e/testdata/pseudo_pending.txt`: 줄기 쪽만 바뀌었다. `pane/main/channel`·`pane/main/feed`·`pane/main/home`·`pane/main/notifications`·`pane/main/thread`·`pane/sidebar`·`pane/thread/thread` 가 지워졌다. 남은 줄은 `overlay/app.helpView`·`overlay/palette.overlay`·`pane/main/settings`·`pane/tabs`·`status`(Task 17~19 몫)다.
- 생성 i18n 파일: 병합 뒤 `go generate ./internal/i18n` 를 돌렸다. `git status --porcelain` 출력이 없었다. 생성 파일은 카탈로그와 맞다(zh-Hans missing 경고만 나옴, M4 허용).
- 골든: 바뀌지 않았다.
- 언어 열기: `internal/i18n` 의 손으로 쓴 .go 는 바뀌지 않았다. `Selectable`·`Open` 도 그대로다. en·zh-Hans 는 닫힌 채다.

### 병합 뒤 고친 것

- 없음. 전체 검증이 모두 통과했다.

## 검증 (병합된 트리 `cdae8c83`, 이 세션. 기록 문서만 더한 이 커밋 전)

- `go build ./...` → `BUILD_OK`
- `go test -count=1 ./...` → `TEST_EXIT=0`. 패키지 84개 ok, `FAIL`·`---` 줄 없음.
- `go vet ./...` → `VET_OK`. `GOOS=windows go vet ./...` → `VET_WIN_OK`. `GOOS=linux go vet ./...` → `VET_LINUX_OK`.
- `gofmt -l` (c90dbe96 이후 바뀐 .go 파일 전부) → 출력 없음.
- `git status --porcelain -- '*.golden'` → 출력 없음. `git diff --name-only c90dbe96 HEAD -- '*.golden'` → 0개.
- server/ 는 바뀌지 않았다. 그래도 지시대로 go test 가 끝난 뒤 따로 돌렸다.
  - `npm run typecheck` → exit 0
  - `npm test` → `Test Files 94 passed (94)`, `Tests 896 passed (896)`, exit 0

```
BUILD_OK
TEST_EXIT=0
84
ok  	github.com/patrache/masil/internal/workspace/share	2.349s
ok  	github.com/patrache/masil/internal/workspace/share/sharetest	1.337s
ok  	github.com/patrache/masil/internal/workspace/wire	1.729s
ok  	github.com/patrache/masil/internal/workspace/wscrypto	2.012s
VET_OK
VET_WIN_OK
VET_LINUX_OK
GOFMT_DONE
TC_EXIT=0
NPM_EXIT=0
 Test Files  94 passed (94)
      Tests  896 passed (896)
```

## 통합하며 내 판단

- 내 판단: 충돌이 없고 검증이 모두 통과해서 병합 뒤 고친 커밋은 없다.
- 내 판단: 줄기가 P0 소유 패키지(`internal/ui/palette`)와 P1 패키지(`internal/ui/leet/cards.go`)를 고친 것은 되돌리지 않았다. 계약 4.8 도 고치지 않았다. 셋 다 오너·P0 결정 몫이라 아래 오너 확인에 올렸다.
- 내 판단: 줄기 보고는 Task 16 에서 래칫과 두 시험이 "먼저 실패" 하는 것을 보지 못했다고 적었다. 통합에서 이를 다시 만들지는 않았다. 병합된 트리의 전체 검증(위)으로만 확인했다.

## 줄기 내 판단 (요약. 전문은 `P5.md` "2묶음 (p5-b2 — Task 11~16)" 절)

- 팔레트: `palette.Command.KeywordsIn func(*i18n.T) []string` 와 `palette.Words(s)` 를 더했다. `Keywords` 는 ASCII 만 남기고, 한국어 검색어는 `kw_*` 키로 옮겼다. 계약 5.6 에 적었다.
- `widgets.(*Composer).SetPlaceholder` 를 더했다. 언어를 바꾼 뒤 자리표시 글을 다시 넣는 데 쓴다.
- 상태 미리 고르기와 접속 상태 별칭은 지금 언어 문구와 ASCII 별칭만 받는다.
- pseudo 목록: Task 11(fun)은 지울 키가 없었다. Task 16 은 쓸어보기 상태가 그 화면을 열지 않아 지울 줄이 없었다.
- 확인 대화상자·메뉴·폼은 열 때의 언어로 문구를 정한다. 판·제목·도움말 줄은 그릴 때마다 렌더한다.
- en 트렌드 메타 줄은 "라벨 값" 꼴이다.
- 이관 전 상수 이름을 쓰던 시험은 고치지 않았다. 시험 전용 `catalog_names_test.go`(feed·chat)에 같은 이름의 ko 값을 두었다.
- 언어를 바꾸면 feed 는 다시 그리고, 채널·스레드는 행을 다시 만든다(`TestRowsFollowLocaleSwitch`). chat 의 ko 고정 `humanize.X` 호출은 모두 `hz(ctx).X` 로 바꿨다.
- 래칫: fun 85 → 0, feed 342 → 177 → 0, chat 573 → 459 → 281 → 0, markup 3 → 0.
- 멤버 판 제목은 골든 때문에 "멤버 3"(`members_title_n`)이다. "멤버 N명" 은 `member_count` 로 따로 두어 초대 카드와 방 설정 개요에 썼다.
- 스레드 도움말 "c #개발로" 의 조사는 받침 판정과 두 키(`thread_parent_ro`·`thread_parent_euro`)로 처리했다. 조사 문법을 쓰면 ko 출력이 바뀐다.
- 테두리 "─ " 머리는 카탈로그 밖에 둔다. pseudo 검사 때문이다.
- 이모지 자동완성 이름 49개를 chat 영역의 `emoji_*` 키로 넣었다.
- `markup.Plain(body, maxCells, codeSummary)` 로 서명이 바뀌었다. `markup.RenderOptions` 에 `MoreTail`·`CodeSummary` 를 더했다. 문구는 `common.more_tail`·`common.code_summary` 다.
- 답글 수는 en 도 "Replies {n}" 꼴이고 복수형이 없다.
- 취소 단추는 `chat.cancel`("Cancel")을 쓴다.
- `feed.notif_title` 의 en 짧은 꼴은 "Alerts" 다.
- Task 12·13·15 가 뒤 Task 파일을 조금 건드렸다(컴파일, 그리고 Task 16 파일의 `cancelText`·`deleteYes`·`itemCopy`).
- 폼 검사 함수는 언어를 받는다(`validChannelName(c)` 등). 방 아이콘의 "없음" 은 `spaceIconChoices(c)` 가 맨 앞에 넣는다.
- 초대 만료 날짜는 chat 이 time 카탈로그로 직접 만든다. `humanize.Formatter.MonthDay` 내보내기는 검토 뒤 되돌렸다(`TestInviteExpiryDateLocales`). 다른 해는 `chat.date_with_year` 로 만들어 ko 출력이 그대로다.
- chat 확인 대화상자의 `*_ask` 와 `*_body` 9개는 모두 `no_truncate` 다. `TestDestructiveConfirmsNoTruncate` 가 40x14 en 화면에서 방 삭제·나가기·강퇴를 본다.
- `channelNameRe` 는 Task 20 에서 이미 이스케이프 꼴이라 `TestChannelNameRegexEscaped` 는 처음부터 통과했다. `bad_channel_name` 의 ko 문구는 그대로다.
- 좁은 자리 chat 키 52개에 `max` 를 붙였다(단추·`help_*` 12, 친구 탭 14, 방 설정 탭 10, `help_pick_then` 8, `thread_parent_*` 6). `short` 는 더하지 않았다. 시험은 `TestChatNarrowSlotsBudget` 이다.
- 계획 Files 밖 수정: palette 는 남겼다(오너 확인). humanize 는 되돌렸다. leet `cards.go` 는 markup 새 필드의 호출부라 같이 고쳤다. widgets `SetPlaceholder` 는 세 줄이다.
- TDD 순서: 일부 카탈로그 시험은 첫 실행부터 통과했다. Task 16 은 이관을 먼저 해서, 래칫과 두 시험이 먼저 실패하는 것을 보지 못했다.

## 오너 확인 (줄기 기록에서 모음)

1. **en 문구 검수**: 특히 "Remove {name} from the space?", "Expires {date}", "Invited by {name}", "Alerts" 를 색인 4절 16번 검수 때 봐 주세요. zh-Hans 는 비워 두었습니다(M4 허용).
2. **이모지 이름 49개**: chat 영역의 `emoji_*` 를 Task 17 의 이모지 영역과 합칠지 정해 주세요.
3. **`internal/ui/palette` 변경(P0 소유)**: `Command.KeywordsIn`·`palette.Words` 를 그대로 둘지, Task 18 로 옮기고 chat·feed·fun 래칫 줄을 잠시 되살릴지 정해 주세요. 계약 5.6 에는 적혀 있습니다.
4. **계약 4.8 미반영**: `markup.Plain` 서명 변경과 `RenderOptions.MoreTail`·`CodeSummary` 가 계약 4.8 에 없습니다. 4.8 은 P5 가 고칠 수 있는 절이 아닙니다. P0 쪽이 적을지, P5 에 허락할지 정해 주세요.
5. **P1 패키지 수정**: `internal/ui/leet/cards.go` 를 markup 꼬리 문구 때문에 고쳤습니다. P1 쪽 확인이 필요하면 봐 주세요.
6. **(Task 20 에서 이어짐) `badChannelName` 안내에 한자를 넣을지**: 아직 결정을 기다립니다(1묶음 통합 기록 오너 확인 5번과 같은 건).
