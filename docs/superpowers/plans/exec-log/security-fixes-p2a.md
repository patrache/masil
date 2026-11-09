# v0.1.3 보안 지적 후속 — 묶음 p2a (OT·fsroot·launch·share)

- 브랜치 `fix/v013-sec-p2a`(워크트리 `.claude/worktrees/sec-p2a`), 기준 `dev/v0.1.3` `2ec16d7c`, 2026-11-10.
- 범위: `internal/workspace/{ot,fsroot,launch,share}` (Go만). push·tag·배포·원격 wrangler·유료 리소스 없음.
- 지적 원문은 자동 커밋 보안 검토 기록의 (a)(f)(g)(h)(n)(o)(v)(w)(x).

## 항목별 결과

| id | 결과 | 근거 / 시험 |
|---|---|---|
| (a) OT 삽입 병합 제곱 비용 | 고침 | `ot/op.go` `UnmarshalJSON` 이 retain 사이의 삽입·삭제 묶음을 `strings.Builder` 로 모아 한 번에 넣는다. `TestUnmarshalInsertMergeLinear`(6만 원소 한 프레임 할당 64 MiB 이하 — 고치기 전 1.9 GB), `TestUnmarshalMatchesBuilder`(builder 정규형과 같음) |
| (f) 링크 + 글자 그대로의 `..` 차단 목록 우회 | 이미 고쳐짐 | `d381bbd7` — `fsroot/root.go` `follow` 가 링크 대상을 정리하지 않고 이어 붙여 `..` 를 앞서 풀린 경로에서 한 칸씩 뺀다. `TestCheckLinkDotDotFollowsOS`(지적 그대로의 `d -> s/..`, `s -> .ssh/keys`) |
| (g) Windows 이름 꼴 | 고침 | `fsroot/root.go` `cleanRel` → `windowsAlias`: Windows 에서 `:`·끝 점·끝 공백·`~숫자` 원소를 Invalid. `TestCheckRejectsWindowsAliasForms`, `TestCheckKeepsUnixNamesOffWindows`. 있는 파일의 별칭(8.3·끝 점)은 `b73cfd2e` 의 `diskName` 이 이미 디스크 이름으로 바꿔 검사했고, 남은 구멍은 **없는** 이름을 그 꼴로 만들기·열기였다 |
| (h) 대소문자 접기 | 이미 고쳐짐 | `b73cfd2e` — `diskName` 이 디스크에 저장된 이름으로 차단·무시 규칙을 검사(`ſ`·대문자 포함). `TestCheckSpellingAliasDenied`·`TestCheckSpellingAliasIgnored` |
| (n) `--settings` 이름 꼴 일치 | 고침 | `launch/check.go` — `Prepare` 가 만든 경로를 정리 전까지 등록, `Check` 는 그 경로와 정확히 같을 때만 통과. `TestCheckSettingsExactPreparedPath` |
| (o) 검사한 모드를 늘 붙이기 | 고침 | `launch/settings.go` `pinnedMode` — 허용된 argv 는 Check 가 말한 모드를 `--settings` 앞에 늘 붙인다. `TestPrepareAlwaysPinsCheckedMode` |
| (v) doc_bcast·presence 가 차단·무시 문서를 모두에게 | 이미 고쳐짐(나머지 아래) | `3a99f453` — `share/docs.go` `onDocSequenced`·`Presence`·`pumpDocEvents` 가 `peerVisible`(어떤 참가자가 연 문서)일 때만 내보내고, 참가자는 `Check(rel, true)` 를 통과한 문서만 연다. `TestOwnerOnlyDocsNotRelayed`(오너가 `.env` 편집 → 참가자 프레임에 없음) |
| (w) 폴더 이름 바꾸기·지우기가 숨은 항목을 옮김 | 고침 | `fsroot/tree.go` `CheckSubtree` + `share/fsops.go` 요청 시·승인 시 재검사(`recheckFs`). `TestPeerFolderMoveKeepsHiddenEntries`, `TestCheckSubtree` |
| (x) 이름 바꾸기가 있는 경로를 덮어씀 | 고침 | `share/fsops.go` `renameTargetTaken` → `exists`, 승인 때 다시 확인. `TestPeerRenameDoesNotOverwrite` |

## 내 판단

- 내 판단: **(g) Windows 에서만** 막는다. macOS·Linux 는 `notes.`·`file~1.txt`·`a:b` 를 글자 그대로 저장하므로 그대로 둔다. 거절 이유는 `Invalid`(새 문구 없음). `~숫자` 는 원소 어디에 있든 막아서 Windows 에서 `Copy~1.txt` 같은 정상 이름도 참가자가 열 수 없다 — 8.3 짧은 이름과 구별할 길이 없어 안전 쪽.
- 내 판단: **(n)** 등록은 `cleanup` 까지만 유효하다. 프로그램이 끝나 `cleanup` 이 불린 뒤의 같은 argv 는 Check 에서 `ReasonUserSettings` 로 막힌다(끝난 터미널은 공유 대상이 아니므로 영향 없음).
- 내 판단: **(o)** 사용자가 `--permission-mode` 를 줬어도 검사한 모드를 뒤에 한 번 더 붙인다(P2 기록 57행의 "사용자 플래그는 그대로 둔다"를 바꿈 — `TestPreparePinsMode`·`TestPrepareSettings` 기대값을 그에 맞게 고침). Check 가 막는 argv(공유 불가) 중 자기 모드 플래그가 있으면 덧붙이지 않는다 — 공유하지 않는 로컬 터미널은 사용자가 고른 모드로 뜨고, 공유는 여전히 막힌다. 플래그가 없으면 예전처럼 `manual`. claude 가 같은 플래그 두 번을 "마지막이 이김"으로 읽는지는 실제 claude 로 확인하지 않았다 — P2 Task 25 Step 4 항목 2 확인 때 함께 볼 것.
- 내 판단: **(w)** 참가자 요청의 대상이 폴더면 그 안(링크는 따라 내려가지 않음) 어디에든 참가자에게 숨은 것(차단 이름·특수 파일·밖을 가리키는 링크 → `denied`, 무시 규칙·규칙 파일·무시 규칙 오류·2만 항목 초과 → `ignored`)이 있으면 거절한다. 오류 코드는 기존 `denied`/`ignored` 를 다시 써서 새 문구·카탈로그 변경이 없다. 폴더 자체를 참가자에게 통째로 막지는 않았다(숨은 것 없는 폴더 이름 바꾸기는 계속 승인 대기열로). 요청 때 검사를 통과해 대기열에 들어가도 승인 때 경로 검사·폴더 검사·덮어쓰기 검사를 다시 한다.
- 내 판단: **(x)** 같은 항목의 다른 철자(대소문자만 바꾸는 이름 바꾸기, 대소문자 무시 디스크)는 막지 않는다(`os.SameFile`). 확인과 `Rename` 사이의 짧은 틈(오너 쪽 다른 프로그램이 그 사이 같은 이름을 만드는 경우)은 남는다 — `os.Root` 에 덮어쓰지 않는 이름 바꾸기가 없다.
- 내 판단: **(v) 나머지는 미룬다.** (1) doc_bcast 는 여전히 `SeatAll`(K_s) 로 가서 그 문서를 열지 않은 참가자도 받는다. 참가자가 열 수 있는 문서는 자리와 상관없이 같으므로(`Check(rel, true)` 는 자리를 보지 않음) 새는 것은 없고, 자리별로 보내려면 자리마다 따로 봉인하는 프로토콜 변경이 필요하다. (2) 참가자가 이미 연 문서를 오너가 나중에 무시 규칙에 넣으면 닫힐 때까지 계속 중계된다. 편집마다 `Check` 를 다시 돌리면 git 무시 규칙에서 키 입력마다 `git check-ignore` 를 띄우게 되므로, 규칙 파일이 바뀔 때 연 문서를 다시 검사하는 방식이 맞다 — 오너 판단 필요(색인 오너 확인 목록 후보).

