# 운영자 (전역 admin)

공개 가입·공개 이미지 서비스라 불법·유해 콘텐츠에 대응할 수단이 첫 출시부터 필요합니다. 전역 운영자는 `masil admin` 명령으로 신고를 처리하고 콘텐츠를 지우고 계정을 정지합니다.

```
masil admin reports|report <id>|resolve <id> <resolution>|remove <type> <id>|suspend @h [--for 7d]|unsuspend @h|users|actions
```

```sh
masil admin reports [--status open|resolved|dismissed] [--type <대상 종류>]                   # 신고 목록
masil admin report <id>                                                                     # 신고 상세
masil admin resolve <id> content_removed|user_suspended|no_action [--note <메모>]            # 신고 처리
masil admin remove message|post|story|guestbook_entry|upload|space <id> --reason <사유> [--report <신고 id>]   # 콘텐츠 강제 삭제
masil admin suspend @handle [--for 7d|12h|30m] --reason <사유> [--report <신고 id>]          # 계정 정지 (--for 없으면 무기한)
masil admin unsuspend @handle                                                               # 정지 해제
masil admin users [--q <검색어>] [--suspended true|false]                                    # 사용자 목록
masil admin actions [--target-type <종류>] [--target <id>]                                   # 운영 조치 기록
```

- `remove`·`suspend` 는 `--reason` 이 필수입니다(감사 기록이 비지 않도록).
- 목록 명령(`reports`·`users`·`actions`)은 `--cursor <c>`·`--limit <n>` 으로 다음 페이지를 봅니다. 다음 페이지가 있으면 끝에 이어 볼 명령을 알려 줍니다.
- `masil admin` 은 **숨김 명령이라 `masil help` 목록에 나오지 않습니다.**
- 운영자가 아니면 `운영자 권한이 없습니다.` 가 나옵니다.

## 운영자 지정

운영자로 올리는 API 는 없습니다. 오너가 D1 데이터베이스에서 다음 SQL 을 한 번 실행합니다.

```sql
UPDATE users SET role = 'admin' WHERE handle = '<handle>';
```

로컬 개발 서버에서는:

```sh
cd server && npx wrangler d1 execute DB --local --command "UPDATE users SET role = 'admin' WHERE handle = '<handle>'"
```

운영(원격) D1 에서의 실행은 **오너 승인 후 오너가** 직접 합니다.

