package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
)

// describeText is the fixed Korean line per error code (plan 03 Task 8 table).
var describeText = map[string]string{
	"invalid_json":           "요청 형식이 올바르지 않아요",
	"unauthorized":           "로그인이 필요해요",
	"invalid_credentials":    "아이디 또는 비밀번호가 맞지 않아요",
	"account_suspended":      "정지된 계정이에요",
	"forbidden":              "권한이 없어요",
	"not_admin":              "운영자만 할 수 있어요",
	"dm_not_allowed":         "이 사용자에게는 DM을 보낼 수 없어요",
	"action_not_allowed":     "지금은 할 수 없는 동작이에요",
	"read_only":              "읽기 전용 대화예요",
	"private_account":        "비공개 계정이에요",
	"banned_from_space":      "이 방에 들어갈 수 없어요",
	"not_found":              "찾을 수 없어요",
	"handle_taken":           "이미 쓰는 아이디예요",
	"already_exists":         "이미 있어요",
	"already_member":         "이미 멤버예요",
	"conflict":               "지금 상태에서는 할 수 없어요",
	"poll_closed":            "마감된 투표예요",
	"invite_invalid":         "초대가 만료됐거나 더 쓸 수 없어요",
	"payload_too_large":      "파일이 너무 커요 (최대 5MB)",
	"unsupported_media_type": "지원하지 않는 이미지 형식이에요",
	"internal_error":         "서버 오류가 났어요",
	"service_unavailable":    "서버가 잠시 바빠요. 다시 시도해 주세요",
	"network":                "서버에 연결할 수 없어요",
	"canceled":               "취소됐어요",
}

// fieldLabel names request fields for invalid_request; unknown fields keep their wire name.
var fieldLabel = map[string]string{
	"handle":       "아이디",
	"password":     "비밀번호",
	"display_name": "표시 이름",
	"body":         "본문",
	"name":         "이름",
	"bio":          "소개",
	"text":         "상태 글",
	"q":            "검색어",
}

// fieldReason names details.fields values; unknown reasons are shown as-is.
var fieldReason = map[string]string{
	"required":    "필요해요",
	"too_long":    "너무 길어요",
	"too_short":   "너무 짧아요",
	"invalid":     "형식이 맞지 않아요",
	"not_allowed": "허용되지 않아요",
}

// Describe returns a Korean, user-facing, one-line description (table per code + fields).
// nil => "".
func Describe(err error) string {
	if err == nil {
		return ""
	}
	code := CodeOf(err)
	var e *Error
	errors.As(err, &e)
	switch code {
	case "":
		return "알 수 없는 오류예요"
	case "invalid_request":
		var f map[string]string
		if e != nil {
			f = e.Fields()
		}
		if len(f) == 0 {
			return "입력값을 확인해 주세요"
		}
		names := make([]string, 0, len(f))
		for k := range f {
			names = append(names, k)
		}
		sort.Strings(names)
		name := names[0]
		label, ok := fieldLabel[name]
		if !ok {
			label = name
		}
		reason, ok := fieldReason[f[name]]
		if !ok {
			reason = f[name]
		}
		return label + ": " + reason
	case "limit_exceeded":
		var d struct {
			Limit *json.Number `json:"limit"`
		}
		if e != nil && e.details(&d) && d.Limit != nil {
			return fmt.Sprintf("한도를 넘었어요 (최대 %s)", d.Limit.String())
		}
		return "한도를 넘었어요"
	case "upgrade_required":
		var d struct {
			Min string `json:"min_client_version"`
		}
		if e != nil && e.details(&d) && d.Min != "" {
			return fmt.Sprintf("masil을 업데이트해 주세요 (최소 %s)", d.Min)
		}
		return "masil을 업데이트해 주세요"
	case "rate_limited":
		if e != nil && e.RetryAfter > 0 {
			return fmt.Sprintf("너무 빨라요. %d초 뒤에 다시 해 주세요", int(math.Ceil(e.RetryAfter.Seconds())))
		}
		return "너무 빨라요. 잠시 뒤에 다시 해 주세요"
	}
	if s, ok := describeText[code]; ok {
		return s
	}
	return fmt.Sprintf("알 수 없는 오류예요 (%s)", code)
}
