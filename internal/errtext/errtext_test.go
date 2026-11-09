package errtext_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
)

func apiErr(code, details string) *api.Error {
	e := &api.Error{Status: 400, Code: code}
	if details != "" {
		e.Details = json.RawMessage(details)
	}
	return e
}

// TestDescribeKOMatchesOldTable: errtext.Describe in ko is byte for byte the
// 0.1.2 api.Describe table (P5 Task 6a; the pairs of api's TestDescribe).
func TestDescribeKOMatchesOldTable(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{apiErr("invalid_json", ""), "요청 형식이 올바르지 않아요"},
		{apiErr("invalid_request", `{"fields":{"handle":"invalid"}}`), "아이디: 형식이 맞지 않아요"},
		{apiErr("invalid_request", `{"fields":{"password":"too_short","body":"too_long"}}`), "본문: 너무 길어요"},
		{apiErr("invalid_request", `{"fields":{"display_name":"required"}}`), "표시 이름: 필요해요"},
		{apiErr("invalid_request", `{"fields":{"name":"not_allowed"}}`), "이름: 허용되지 않아요"},
		{apiErr("invalid_request", `{"fields":{"bio":"too_long"}}`), "소개: 너무 길어요"},
		{apiErr("invalid_request", `{"fields":{"text":"too_long"}}`), "상태 글: 너무 길어요"},
		{apiErr("invalid_request", `{"fields":{"q":"too_short"}}`), "검색어: 너무 짧아요"},
		{apiErr("invalid_request", `{"fields":{"password":"invalid"}}`), "비밀번호: 형식이 맞지 않아요"},
		{apiErr("invalid_request", `{"fields":{"cursor":"invalid"}}`), "cursor: 형식이 맞지 않아요"},
		{apiErr("invalid_request", `{"fields":{"path":"invalid"}}`), "ID·코드: 형식이 맞지 않아요"},
		{apiErr("invalid_request", `{"fields":{"topic":"weird"}}`), "topic: weird"},
		{apiErr("invalid_request", `{}`), "입력값을 확인해 주세요"},
		{apiErr("invalid_request", ""), "입력값을 확인해 주세요"},
		{apiErr("unauthorized", ""), "로그인이 필요해요"},
		{apiErr("invalid_credentials", ""), "아이디 또는 비밀번호가 맞지 않아요"},
		{apiErr("account_suspended", `{"until":null,"reason":"spam"}`), "정지된 계정이에요"},
		{apiErr("forbidden", ""), "권한이 없어요"},
		{apiErr("not_admin", ""), "운영자만 할 수 있어요"},
		{apiErr("dm_not_allowed", ""), "이 사용자에게는 DM을 보낼 수 없어요"},
		{apiErr("action_not_allowed", ""), "지금은 할 수 없는 동작이에요"},
		{apiErr("read_only", ""), "읽기 전용 대화예요"},
		{apiErr("private_account", ""), "비공개 계정이에요"},
		{apiErr("banned_from_space", ""), "이 방에 들어갈 수 없어요"},
		{apiErr("not_found", ""), "찾을 수 없어요"},
		{apiErr("handle_taken", ""), "이미 쓰는 아이디예요"},
		{apiErr("already_exists", ""), "이미 있어요"},
		{apiErr("already_member", ""), "이미 멤버예요"},
		{apiErr("conflict", `{"reason":"self"}`), "지금 상태에서는 할 수 없어요"},
		{apiErr("poll_closed", ""), "마감된 투표예요"},
		{apiErr("invite_invalid", `{"reason":"expired"}`), "초대가 만료됐거나 더 쓸 수 없어요"},
		{apiErr("payload_too_large", ""), "파일이 너무 커요 (최대 5MB)"},
		{apiErr("unsupported_media_type", ""), "지원하지 않는 이미지 형식이에요"},
		{apiErr("limit_exceeded", `{"what":"group_dm_members","limit":10}`), "한도를 넘었어요 (최대 10)"},
		{apiErr("limit_exceeded", ""), "한도를 넘었어요"},
		{apiErr("upgrade_required", `{"min_client_version":"0.2.0"}`), "masil을 업데이트해 주세요 (최소 0.2.0)"},
		{apiErr("upgrade_required", ""), "masil을 업데이트해 주세요"},
		{&api.Error{Status: 429, Code: "rate_limited", RetryAfter: 12 * time.Second}, "너무 빨라요. 12초 뒤에 다시 해 주세요"},
		{&api.Error{Status: 429, Code: "rate_limited", RetryAfter: 11200 * time.Millisecond}, "너무 빨라요. 12초 뒤에 다시 해 주세요"},
		{&api.Error{Status: 429, Code: "rate_limited"}, "너무 빨라요. 잠시 뒤에 다시 해 주세요"},
		{apiErr("internal_error", ""), "서버 오류가 났어요"},
		{apiErr("service_unavailable", ""), "서버가 잠시 바빠요. 다시 시도해 주세요"},
		{&url.Error{Op: "Get", URL: "https://h", Err: errors.New("connection refused")}, "서버에 연결할 수 없어요"},
		{fmt.Errorf("api: %w", context.Canceled), "취소됐어요"},
		{apiErr("teapot", ""), "알 수 없는 오류예요 (teapot)"},
		{fmt.Errorf("wrapped: %w", apiErr("not_found", "")), "찾을 수 없어요"},
		{errors.New("x"), "알 수 없는 오류예요"},
		{nil, ""},
	} {
		if got := errtext.Describe(i18n.KoT(), tc.err); got != tc.want {
			t.Errorf("Describe(ko, %v) = %q, want %q", tc.err, got, tc.want)
		}
		if got := errtext.Describe(nil, tc.err); got != tc.want {
			t.Errorf("Describe(nil, %v) = %q, want the ko %q", tc.err, got, tc.want)
		}
	}
}
