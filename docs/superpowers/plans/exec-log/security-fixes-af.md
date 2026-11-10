# v0.1.3 보안 지적 후속 — (af) 폴더 밖 무시 규칙 (fail-closed)

- 브랜치 `feat/v013-sec-af`(워크트리 `.claude/worktrees/v013-sec-af`), 기준 `dev/v0.1.3` `a393a84e`, 2026-11-10.
- 범위: `internal/workspace/{fsroot,share}` (Go만). push·tag·배포·원격 wrangler·유료 리소스 없음. i18n·골든 변경 없음.
- 지적 원문: 자동 커밋 보안 검토 기록 (af) + `security-fixes.md` 오너 확인 1번.

## 결과

| id | 결과 | 근거 / 시험 |
|---|---|---|
| (af) 폴더 밖 무시 규칙 변경을 못 봄 | 고침 | `share/docs.go` `pollRules`: 규칙 파일이 바뀌었는지와 상관없이 **DiskPoll(0.5초)마다** 참가자가 연 문서 전부(숨김 표시 전인 것)를 `fsroot.Root.CheckBatch(rels, true)` 로 한 번에 다시 검사하고, 통과 못 한 문서는 `hide` → 같은 틱의 `closeHidden`(`30545481` 의 닫기 경로 그대로)으로 모든 자리에서 닫는다. 무시 규칙 질문은 한 번의 `Ignorer.Ignored`(git 이면 `git check-ignore` 한 번)로 묶는다. 출처를 하나하나 추적하지 않으므로 상위 저장소 `.gitignore`, gitfile 워크트리의 gitdir `info/exclude`, `core.excludesFile`, git 전역 제외 파일 무엇이 바뀌어도 다음 폴링에 닫힌다. `TestOutsideRuleClosesPeerDocNextPoll`(규칙 파일 변화 없이 규칙만 바뀜 → 다음 폴링에 두 자리 모두 `ignored` 로 닫힘, 뒤 편집 프레임 없음), `TestOutsideGitRuleSourcesClosePeerDoc`(실제 git: `core.excludesFile`·`--separate-git-dir` 의 gitdir `info/exclude`·폴더 위 저장소 top `.gitignore` 세 경우), `TestRecheckBatchedPerPoll` |
| 오너 확인 3 (내용이 같은 파일로의 링크 교체) | 고침(같은 변경으로) | 매 폴링의 `Check` 가 링크를 다시 따라가므로 디스크 병합이 없어도 닫힌다. `TestLinkRetargetedToSameContentCloses`(무시 폴더의 같은 내용 파일로 링크를 바꿈 → 고치기 전 열린 채, 고친 뒤 닫힘) |

### 만든 것·바꾼 것

- `fsroot.(*Root).CheckBatch(rels []string, forPeer bool) (cleans []string, errs []error)` — 경로마다 `Check` 와 같은 답, 무시 규칙은 전체에 한 번. `Check` 는 이것을 한 경로로 부른다(동작 같음). 무시 규칙 오류·답 개수 불일치는 물은 경로 전부 `Ignored`(fail closed).
- `fsroot` 내부: `checkIgnored` → `CheckBatch` 로 흡수, `checkPath`(문법·탈출·차단 목록·링크) 분리.
- 지움: `fsroot.(*Root).RuleFiles`, `fsroot.(*Watcher).TrackNow`, `TestRuleFiles`, `share.Host.rules`(규칙 파일 감시기). 매 폴링 재검사가 이들이 하던 일(폴더 안 규칙 파일 변화 감지)을 포함하므로 쓰는 곳이 없어졌다.
- 그대로: 디스크 병합 직전 재검사(`onDocDisk` → `recheckPeerDoc`, 한 문서 `Check`), 닫기 경로(`hide`·`closeHidden`·참가자 `onDocState`).

## 내 판단

- 내 판단: 두 안(출처 모두 추적 / 매 폴링 재검사) 중 **매 폴링 재검사만** 택했다. 출처 추적은 `rev-parse`·`git config` 로 파일 목록을 얻어도 `include`·`GIT_DIR`·`XDG_CONFIG_HOME` 변경, 규칙 파일 경로 자체의 교체 등을 다 따라가기 어렵고 빠뜨리면 다시 fail-open 이다. 재검사는 git 이 지금 하는 답을 그대로 쓰므로 출처 목록이 필요 없다.
- 내 판단: 비용 상한 — 참가자가 연 문서가 있을 때만, DiskPoll 마다 `git check-ignore` 프로세스 1개(경로 최대 `MaxOpenDocs`=20, 링크면 실제 경로 포함 40), 편집(키 입력)마다는 0, 아무도 연 게 없으면 0. 경로 문법·링크 따라가기(`diskName` 의 폴더 읽기 포함)는 문서마다 매 폴링 돈다(최대 20개).
- 내 판단: 재검사는 지금처럼 `Host.Tick` 의 `h.mu` 안에서 동기로 돈다(이전 규칙 변경 재검사·디스크 병합 재검사와 같은 자리). 보통 수 ms 지만 git 이 멈추면 한 번에 최대 2초(`gitTimeout`) 오너 엔진이 그동안 멈춘다. 비동기로 빼면 시험이 비결정적이 되고 숨김 전 틈이 늘어 택하지 않았다.
- 내 판단: 닫기 문구·코드는 기존 그대로(`doc_state{error: ignored|denied|...}` → 참가자 탭 닫힘, "열 수 없음"). 새 i18n 키 없음.

## 오너 확인

1. **폴더에 `.git` 이 없는 저장소 하위 폴더 공유**: `fsroot.Open` 은 폴더 자체에 `.git` 이 있을 때만 git 에게 묻고, 아니면 내장 해석기(폴더 안 `.gitignore`·`.masilignore` 만)를 쓴다. 그래서 저장소 하위 폴더를 공유하면 상위 저장소 top 의 `.gitignore`·`info/exclude`·`core.excludesFile` 은 **처음부터 적용되지 않는다**(연 뒤 숨김 문제가 아니라 적용 범위 문제). 이번 시험의 "폴더 위 저장소 top" 경우는 `NewGitIgnorer` 를 직접 넣어 재검사 경로를 확인한 것이다. 상위 work tree 를 감지해 git 을 쓰게 할지(`git rev-parse --is-inside-work-tree`) 결정 필요 — 쓰면 `$HOME` 이 `*` 를 무시하는 dotfiles 저장소인 사람은 `.git` 없는 폴더를 공유할 때 참가자에게 아무것도 안 보이게 된다(fail-closed 지만 사용성 문제).
2. **git 실패 시 대체 해석기**: `gitIgnorer` 는 git 이 실패(시간 초과·종료 128)하면 내장 해석기로 넘어가 폴더 밖 규칙을 못 본다. 그 폴링 동안은 fail-open 이다(다음 폴링에 git 이 살아나면 닫힘). 실패를 "숨김"으로 바꾸면 깨진 `.git`(끊긴 gitfile 등)인 폴더에서 참가자에게 아무것도 안 보인다 — 바꾸지 않았다.
3. 매 폴링 동기 `git check-ignore`(위 판단) 비용·멈춤 상한이 괜찮은지.
4. `security-fixes.md` 의 (af) 행·오너 확인 1·3번은 이 브랜치를 병합할 때 갱신할 것(통합 기록이라 이 브랜치에서는 손대지 않았다).
