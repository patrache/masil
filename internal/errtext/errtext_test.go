package errtext_test

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
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

func apiErr(code, details string) *api.Error {
	e := &api.Error{Status: 400, Code: code}
	if details != "" {
		e.Details = json.RawMessage(details)
	}
	return e
}

// TestDescribeKOMatchesOldTable: errtext.Describe in ko is byte for byte the
// 0.1.2 Describe table (P5 Task 6a; the pairs of api's TestDescribe).
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
		// "reason" is a real server field (admin reason, report reason); its
		// lookup key field_reason is the line template, not a label (review).
		{apiErr("invalid_request", `{"fields":{"reason":"too_long"}}`), "reason: 너무 길어요"},
		{apiErr("invalid_request", `{"fields":{"reason":"invalid"}}`), "reason: 형식이 맞지 않아요"},
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

// errorsKeys lists the catalog's errors-area keys with the given prefix
// ("code_", "field_", "reason_") minus the prefix.
func errorsKeys(prefix string) []string {
	var out []string
	for _, e := range i18n.Entries() {
		if e.Area == "errors" && strings.HasPrefix(e.Key, prefix) {
			out = append(out, strings.TrimPrefix(e.Key, prefix))
		}
	}
	return out
}

func hasHangul(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

// TestDescribeENNoHangul: every code, field × reason and detail form renders
// in English with no Korean left.
func TestDescribeENNoHangul(t *testing.T) {
	en := i18n.For(i18n.En)
	codes, fields, reasons := errorsKeys("code_"), errorsKeys("field_"), errorsKeys("reason_")
	if len(codes) < 24 || len(fields) < 9 || len(reasons) < 5 {
		t.Fatalf("errors area too small: %d codes, %d fields, %d reasons", len(codes), len(fields), len(reasons))
	}
	var errs []error
	for _, c := range codes {
		errs = append(errs, apiErr(c, ""))
	}
	for _, f := range fields {
		for _, r := range reasons {
			errs = append(errs, apiErr("invalid_request", `{"fields":{"`+f+`":"`+r+`"}}`))
		}
	}
	errs = append(errs,
		apiErr("invalid_request", ""),
		apiErr("limit_exceeded", `{"limit":10}`),
		apiErr("upgrade_required", `{"min_client_version":"0.2.0"}`),
		&url.Error{Op: "Get", URL: "https://h", Err: errors.New("refused")},
		fmt.Errorf("api: %w", context.Canceled),
		apiErr("teapot", ""),
		errors.New("x"),
	)
	for _, err := range errs {
		got := errtext.Describe(en, err)
		if got == "" || hasHangul(got) {
			t.Errorf("Describe(en, %v) = %q: empty or Korean", err, got)
		}
	}
	got := errtext.Describe(en, &api.Error{Status: 429, Code: "rate_limited", RetryAfter: 12 * time.Second})
	if !strings.Contains(got, "12") || hasHangul(got) {
		t.Errorf("rate_limited en = %q, want 12 in it and no Korean", got)
	}
	if got := errtext.Describe(en, apiErr("limit_exceeded", `{"limit":10}`)); !strings.Contains(got, "10") {
		t.Errorf("limit_exceeded en = %q, want 10 in it", got)
	}
	if got := errtext.Describe(en, apiErr("invalid_request", `{"fields":{"reason":"too_long"}}`)); got != "reason: Too long" {
		t.Errorf("reason field en = %q, want %q", got, "reason: Too long")
	}
	if got := errtext.Describe(en, apiErr("teapot", "")); !strings.Contains(got, "teapot") {
		t.Errorf("unknown code en = %q, want the code in it", got)
	}
}

// TestDescribeHostileCode: a server code carrying an OSC 52 clipboard write
// renders with no ESC/BEL in ko and en, and the code piece is capped.
func TestDescribeHostileCode(t *testing.T) {
	hostile := apiErr("\x1b]52;c;x\a", "")
	long := apiErr(strings.Repeat("x", 200)+"\x1b[2J", "")
	for _, tt := range []*i18n.T{i18n.KoT(), i18n.For(i18n.En)} {
		for _, err := range []error{hostile, long} {
			got := errtext.Describe(tt, err)
			if strings.ContainsAny(got, "\x1b\a") || got != textwidth.Sanitize(got) {
				t.Errorf("%s Describe = %q: carries an escape", tt.Locale(), got)
			}
			if x := api.Explain(err); textwidth.Width(x.Code) > 40 {
				t.Errorf("%s code piece %q is %d cells, want <= 40", tt.Locale(), x.Code, textwidth.Width(x.Code))
			}
		}
	}
}

// TestErrorsCoreNoTruncate: the core error sentences are marked no_truncate
// (i18n 6.6 rule 4).
func TestErrorsCoreNoTruncate(t *testing.T) {
	for _, e := range i18n.Entries() {
		if e.Area != "errors" || strings.HasPrefix(e.Key, "field_") || strings.HasPrefix(e.Key, "reason_") {
			continue
		}
		if !e.NoTruncate {
			t.Errorf("errors.%s: core error sentence without no_truncate", e.Key)
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

// TestDescribeSanitizesServerStrings (pending-fixes 0-b, moved from api): every string the
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
		for _, tt := range []*i18n.T{i18n.KoT(), i18n.For(i18n.En)} {
			got := errtext.Describe(tt, err)
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
	}
	// Ordinary unknown values still show.
	if got := errtext.Describe(nil, apiErr("invalid_request", `{"fields":{"topic":"weird"}}`)); got != "topic: weird" {
		t.Errorf("plain unknown field = %q", got)
	}
}
