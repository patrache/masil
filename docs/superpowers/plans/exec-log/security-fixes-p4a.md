# v0.1.3 security fixes — group p4a

Branch `fix/v013-sec-p4a` (from dev/v0.1.3). Findings: the commit security reviewer's list (ids b c d e i j k l m ad).

| id | state | commit | test |
|----|-------|--------|------|
| b | fixed | fix(apitool): treat non-canonical IPv4 literals as private… | TestNonCanonicalIPv4IsPrivate, TestForeignProxiedNonCanonicalIPv4Asks |
| c | fixed | fix(apitool): stop an https->http redirect while a secret is in use | TestRedirectHTTPSToHTTPStopsWithSecret |
| d | fixed | fix(apitool): a shared request's extract writes only its own secrets… | TestSharedExtractCannotOverwritePersonalSecret, TestResponseHostIsFinalHost, ui TestSharedExtractKeepsMyGlobalSecret |
| e | fixed | fix(apitool): key the dial-time proxy exemption by exact proxy host:port | TestViaProxyKeyedByHostPort |
| i, j | fixed | fix(apitool/convert): OpenAPI import skips host-changing path keys and caps $ref output; j follow-up: cap OpenAPI example encoding while writing | TestOpenAPIPathKeyMustBeAPath, TestOpenAPIRefExpansionBudget, TestOpenAPISingleExampleBudgetBounded, TestEncodeCappedStopsEarly |
| k | already fixed in fdb4cacf (postman.go formdata: FileName = base name, File never set, Dropped "file: <key> (<src>)") | pinned with the l commit | TestImportPostmanFileSrcNotKept |
| l | fixed | fix(apitool/convert): Postman export writes the form file name… | TestExportPostmanNoLocalPath |
| m | fixed | fix(apitool/convert): Postman collection variables with a credential-like name… | TestImportPostmanSensitiveCollectionVarIsSecret |
| j (2차) | fixed | fix(apitool/convert): bound OpenAPI $ref fan-out work, not only output (851b66ae) | TestOpenAPIRefFanOutWorkBudget, TestOpenAPILongRefWorkBudget, TestOpenAPIManyPathParamsLinear, TestOpenAPIRealisticReuseImports |
| i (2차) | fixed | fix(apitool/convert): OpenAPI path keys may carry '@' after the leading "/" | TestOpenAPIPathKeyMustBeAPath (갱신) |
| ad | fixed | fix(server/collab): apicol validator refuses Object.prototype field and settings names | test/unit/collab-proto-keys.test.ts |

## 내 판단

- **b** 마지막 점 구획이 숫자(10진 또는 0x 16진)인데 netip 가 정식 IP 로 못 읽는 호스트는 무엇으로 풀리든
  "내 PC·내 네트워크" 로 본다(예 `134744072` = 8.8.8.8 도 묻는다). inet_aton 식 해석(0 으로 시작 = 8진)과 다르게
  읽는 프록시가 있어 해석해서 판정하면 틀릴 수 있다. 공개 DNS 이름은 숫자로 끝나지 않으므로 정상 요청은 영향이 없고,
  남이 바꾼 요청만 묻는다(private_target 질문 그대로, 동의하면 간다).
- **c** https 요청이 비밀 변수를 쓸 때 http 로 내려가는 리다이렉트는 같은 호스트여도 따라가지 않는다(3xx 를 그대로
  돌려주고 RedirectBlock.Insecure). 화면 문구는 기존 `redirect_blocked` 토스트("{host} 로의 리다이렉트를 멈췄어요")를
  그대로 쓴다 — 새 문구를 만들지 않았다.
- **d** 공유 컬렉션에서 추출 → 비밀 변수 쓰기는 (1) 공유 문서(환경·컬렉션 층)에 선언된 비밀만, (2) 응답한 호스트가
  그 비밀의 허용 호스트(내 .mine.json)에 있을 때만 한다. 허용 호스트가 아직 비어 있으면 거절된다 — 로그인 응답이
  다른 호스트(auth.x)에서 오면 그 호스트를 비밀의 허용 목록에 넣어야 추출이 된다(넣으면 그 호스트로 보낼 수도 있게
  되므로 사용자가 고르는 것이 맞다고 봄). 개인 컬렉션은 내가 쓴 규칙이므로 지금처럼 호스트 조건 없이 쓴다(로그인
  호스트 ≠ API 호스트 흐름을 깨지 않으려고). 거절은 토스트 2개(`extract_not_shared`, `extract_host_blocked`, ko·en)로
  알리고, 실행기에서는 ReasonExtract 실패가 된다.
  - 이 때문에 `ApplyExtracts` 에 응답 호스트 인자가 생겼고 `Response.Host`(리다이렉트 뒤 실제로 답한 서버)를 더했다.
  - 화면 쪽 `internal/ui/apitool/actions.go` `sent()` 는 비밀 값을 직접 금고에 넣는 경로라 고치지 않으면 구멍이 남는다:
    영역 밖이지만 그 함수의 분기만 `ExtractSecretKey` 로 바꿨다(최소 변경).
- **e** 프록시 예외는 net/http 가 실제로 다이얼하는 "host:port"(포트 없으면 http 80, https 443, socks5 1080)로만 준다.
- **i** `/` 로 시작하지 않거나 `@` `\` 가 든 경로 키는 요청을 만들지 않고 Report.Ignored 에 `path: <key>` 로 남긴다.
- **j** 가져오기 결과 글자 예산(MaxImportOutput)을 MaxImportBytes(10MB)와 같게 잡았다: 입력 한도보다 큰 결과는
  만들지 않는다(행·요청마다 16바이트 가산). 같은 큰 예시를 수백 번 재사용하는 정상 명세가 10MB 를 넘으면 거절되는데,
  이때는 기존 "너무 큼" 오류 문구가 나온다. 비밀 탐지기(정규식) 비용이 결과 크기에 비례하므로 더 크게 잡지 않았다.
  - 리뷰 후속(326b36fe): 예산을 결과를 다 만든 뒤가 아니라 **쓰는 동안** 건다. 예시·본문 JSON 은 남은 예산만큼만
    자라는 버퍼(encodeCapped)로 쓰고 넘으면 그 자리에서 멈춰 ErrTooLarge. 들여쓰기 배치는 json.Indent 와 같다.
    입력 해독(decodeJSON) 비용은 입력 크기에 비례하는 기존 비용이라 손대지 않았다(1MB 입력 해독에 약 100MB 할당 —
    별건으로 볼 만함).
- **j 2차(리뷰 후속)** 글자 예산만으로는 $ref 로 재사용되는 경로 항목의 메서드 아닌 키, 이름 없는·객체 아닌 매개변수,
  value 없는 examples 처럼 **결과를 만들지 않는 반복**이 공짜라 작은 파일로 수 분~수십 분 걸렸다. 작업 예산
  `MaxImportWork`(= MaxImportBytes, 10M 걸음)을 더했다: 방문한 키·원소마다 1, 따라간 $ref 마다 1 + 참조 길이/16.
  넘으면 같은 ErrTooLarge(기존 "너무 큼" 문구). 기준: 경로 1000개 × 메서드 4 × 공유 매개변수 30 + 폼 필드 30 인
  정상 명세(결과 약 5MB)가 약 0.98M 걸음을 써서 예산의 1/10 이고, 결과 예산(10MB)을 다 채우는 정상 명세도 약 2M
  걸음이라 작업 예산이 먼저 막히지 않는다. 최악(이름 없는 매개변수 팬아웃)은 이 기계에서 약 2.3초에 끝난다.
  JSON 포인터는 참조 전체를 미리 쪼개지 않고 한 구획씩 걷는다. colVar 는 이름 색인 맵으로 바꿨다(경로 매개변수
  80k 개 단일 연산 15.3s → 0.23s). 화면 쪽 취소(prompts.go 가져오기)는 손대지 않았다 — 시간이 유한해졌으므로.
- **i 2차(리뷰 후속)** `@` 전면 거절을 풀었다: 키가 "/" 하나로 시작하면 권한부(authority)는 {{base_url}} 에서 이미
  끝나므로 뒤의 `@` 는 경로일 뿐이다(Discord `/users/@me`, `/applications/@me` 가 다시 요청이 된다). 대신 `//` 로
  시작하는 키를 거절에 더했다 — base_url 이 `https:` 처럼 스킴만 있으면 `//evil.com` 이 호스트가 되기 때문.
  `\` 거절은 유지. 기존 테스트의 `/a@evil.com` 은 이제 받아들이는 쪽으로 옮겼다(호스트는 base_url 그대로임을
  테스트가 url.Parse 로 확인).
- **m** 컬렉션 변수도 환경처럼 `type: "secret"` 또는 이름이 자격 증명처럼 보이면(SensitiveHeader: token, secret, key,
  auth, session, password …) 비밀로 가져오고 값은 버리며 Dropped `variable value: <name>` 로 알린다(환경 가져오기와
  달리 금고에 넣을 값을 돌려주지 않는다 — 기존 컬렉션 비밀 처리와 같게). 기존 테스트
  TestImportPostmanSuggestsVariableSecrets 의 변수 이름을 `api_token` → `gh` 로 바꿨다(이제 api_token 은 비밀이라
  제안 대상이 아님).
- **ad** `__proto__` `constructor` `prototype` 은 이미 op 파서(RESERVED)가 거절한다. 나머지 Object.prototype 이름은
  apicol 이 Object.hasOwn / Map 으로 막고 checkValue 기본값을 invalid 로 했다. arch(hasOwnProperty)와 P0 reducer
  (null-prototype 객체, Map)는 확인 결과 해당 없음.
- 골든 변경 없음.
