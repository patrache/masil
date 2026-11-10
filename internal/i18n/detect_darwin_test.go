//go:build darwin

package i18n

import (
	"slices"
	"testing"
)

func TestParseAppleLanguages(t *testing.T) {
	got := parseAppleLanguages("(\n    \"ko-KR\",\n    en,\n    \"zh-Hans-CN\"\n)\n")
	if want := []string{"ko-KR", "en", "zh-Hans-CN"}; !slices.Equal(got, want) {
		t.Fatalf("parseAppleLanguages = %q, want %q", got, want)
	}
	if got := parseAppleLanguages(""); got != nil {
		t.Fatalf("empty = %q", got)
	}
}
