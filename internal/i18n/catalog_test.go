package i18n_test

import (
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

func TestRenderPluralAndFallback(t *testing.T) {
	en := i18n.For(i18n.En)
	if got := en.Testonly.Members(1); got != "1 member" {
		t.Errorf("en Members(1) = %q", got)
	}
	if got := en.Testonly.Members(2); got != "2 members" {
		t.Errorf("en Members(2) = %q", got)
	}
	if got := i18n.For(i18n.Ko).Testonly.Members(3); got != "멤버 3명" {
		t.Errorf("ko Members(3) = %q", got)
	}
	zh := i18n.For(i18n.ZhHans)
	if got := zh.Testonly.Members(3); got != "3 位成员" {
		t.Errorf("zh Members(3) = %q", got)
	}
	if got := zh.Testonly.EnOnly(); got != "Only in English" {
		t.Errorf("zh falls back to en: %q", got)
	}
	if got := zh.Testonly.KoOnly(); got != "한국어만" {
		t.Errorf("zh falls back to ko: %q", got)
	}
	if got := en.Testonly.KoOnly(); got != "한국어만" {
		t.Errorf("en falls back to ko: %q", got)
	}
	if got := i18n.KoT().Testonly.MembersShort(5); got != "5명" {
		t.Errorf("ko MembersShort = %q", got)
	}
	if got := en.Testonly.MembersShort(5); got != "5" {
		t.Errorf("en MembersShort = %q", got)
	}
	if got := zh.Testonly.MembersShort(5); got != "5 位成员" {
		t.Errorf("zh without short uses its other: %q", got)
	}
	if got := i18n.KoT().Testonly.Greet("하나"); got != "하나님, 안녕하세요" {
		t.Errorf("ko Greet = %q", got)
	}
	if got := i18n.KoT().Testonly.MoveTo("서울"); got != "서울로 옮겼어요" {
		t.Errorf("ko MoveTo particle = %q", got)
	}
	if got := i18n.KoT().Testonly.MoveTo("부산 {x}"); got != "부산 {x}(으)로 옮겼어요" {
		t.Errorf("args are not templates: %q", got)
	}
}

func TestForCachesAndLocale(t *testing.T) {
	if i18n.For(i18n.En) != i18n.For(i18n.En) {
		t.Fatal("For must return a cached catalog")
	}
	if i18n.KoT() != i18n.For(i18n.Ko) {
		t.Fatal("KoT() must equal For(Ko)")
	}
	for _, l := range []i18n.Locale{i18n.Ko, i18n.En, i18n.ZhHans, i18n.Pseudo} {
		if got := i18n.For(l).Locale(); got != l {
			t.Errorf("For(%q).Locale() = %q", l, got)
		}
	}
	if got := i18n.For("fr").Locale(); got != i18n.Ko {
		t.Errorf("unknown locale => ko, got %q", got)
	}
}

func TestCommonLanguageNames(t *testing.T) {
	for _, l := range []i18n.Locale{i18n.Ko, i18n.En, i18n.ZhHans} {
		c := i18n.For(l).Common
		if c.LangNameKo() != "한국어" || c.LangNameEn() != "English" || c.LangNameZh() != "简体中文" {
			t.Errorf("%s: %q %q %q", l, c.LangNameKo(), c.LangNameEn(), c.LangNameZh())
		}
	}
}
