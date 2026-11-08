package api_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/textwidth"
)

func apiErr(code, details string) *api.Error {
	e := &api.Error{Status: 400, Code: code}
	if details != "" {
		e.Details = json.RawMessage(details)
	}
	return e
}

func TestDescribe(t *testing.T) {
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
		if got := api.Describe(tc.err); got != tc.want {
			t.Errorf("Describe(%v) = %q, want %q", tc.err, got, tc.want)
		}
	}
}

// hasEscape reports ESC, BEL or any other C0/DEL/C1 control, a format
// character (Cf: bidi, zero-width, BOM, soft hyphen, tags), U+2028/U+2029, or
// invalid UTF-8.
func hasEscape(s string) bool {
	if !utf8.ValidString(s) {
		return true
	}
	for _, r := range s {
		if r < 0x20 || r >= 0x7f && r <= 0x9f || unicode.In(r, unicode.Cf, unicode.Zl, unicode.Zp) {
			return true
		}
	}
	return false
}

// TestDescribeSanitizesServerStrings (pending-fixes 0-b): every string the
// server supplies (code, field name, field reason, min version, message,
// request id) reaches the screen without escape sequences and length-capped.
func TestDescribeSanitizesServerStrings(t *testing.T) {
	const clear, osc52, osc0 = "\x1b[2J", "\x1b]52;c;ZXZpbA==\x07", "\x1b]0;PWNED\x07"
	long := strings.Repeat("가", 500)
	for _, err := range []error{
		apiErr("teapot"+clear+osc52, ""),
		apiErr("\x9b2J\u009b2J"+osc0, ""),
		apiErr("\xff\xfe"+clear, ""),
		apiErr("invalid_request", `{"fields":{"\u001b]52;c;ZXZpbA==\u0007":"invalid"}}`),
		apiErr("invalid_request", `{"fields":{"handle":"\u001b[2J\u001b]0;PWNED\u0007\u009b31m"}}`),
		apiErr("invalid_request", `{"fields":{"\u001b[2J":"\u001b]52;c;ZXZpbA==\u0007"}}`),
		apiErr("invalid_request", `{"fields":{"x":"`+long+`"}}`),
		apiErr("invalid_request", `{"fields":{"`+long+`":"invalid"}}`),
		apiErr("upgrade_required", `{"min_client_version":"9.9.9\u001b[2J\u001b]52;c;ZXZpbA==\u0007"}`),
		apiErr("upgrade_required", `{"min_client_version":"`+long+`"}`),
		apiErr(long+clear, ""),
		// bidi controls, invisible format characters and line separators
		// make the line read differently (pending-fixes 1)
		apiErr("teapot\u202etxt.exe\u2067x\u2069\u200b\ufeff\u00ad\u2028", ""),
		apiErr("invalid_request", `{"fields":{"\u202ehandle\u2066":"\u200e\u200f\u061c\u2060\u2029bad"}}`),
		apiErr("upgrade_required", `{"min_client_version":"9.9.9\u202e\u200b"}`),
	} {
		got := api.Describe(err)
		if hasEscape(got) || strings.Contains(got, "[2J") || strings.Contains(got, "]52;") || strings.Contains(got, "]0;") {
			t.Errorf("Describe(%q) = %q: carries an escape", err.(*api.Error).Code, got)
		}
		if got != textwidth.Sanitize(got) {
			t.Errorf("Describe = %q: not sanitized", got)
		}
		if w := textwidth.Width(got); w > 80 {
			t.Errorf("Describe = %q: width %d > 80", got, w)
		}
	}
	// Ordinary unknown values still show.
	if got := api.Describe(apiErr("invalid_request", `{"fields":{"topic":"weird"}}`)); got != "topic: weird" {
		t.Errorf("plain unknown field = %q", got)
	}
}

// TestErrorStringSanitized: (*Error).Error() reaches stderr and toasts as is,
// so the server's code, message and X-Request-Id carry no escapes either.
func TestErrorStringSanitized(t *testing.T) {
	e := &api.Error{Status: 400, Code: "x\x1b[2J\u202e", Message: "m\x1b]52;c;ZXZpbA==\x07\u009b31m\u200b\u2028", RequestID: "r\x1b]0;T\x07\u2067"}
	if got := e.Error(); hasEscape(got) || strings.Contains(got, "]52;") || strings.Contains(got, "]0;") || strings.Contains(got, "[2J") {
		t.Errorf("Error() = %q: carries an escape", got)
	}
	if got := (&api.Error{Status: 404, Code: "not_found", Message: "no such user", RequestID: "abc"}).Error(); got != "api: 404 not_found: no such user (request abc)" {
		t.Errorf("plain Error() = %q", got)
	}
}
