package slash

import (
	"errors"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/i18n"
)

// oldMessages are the 0.1.2 parse error sentences (byte for byte).
var oldMessages = []struct{ in, want string }{
	{"/nope x", "알 수 없는 명령이에요: /nope"},
	{"/me", "사용법: /me <행동>"},
	{"/status", "사용법: /status [이모지] <글>"},
	{"/focus a b", "사용법: /focus [25m|off]"},
	{"/focus abc", "사용법: /focus [25m|off]"},
	{"/focus 0m", "집중 시간은 1분~12시간이에요"},
	{"/focus 13h", "집중 시간은 1분~12시간이에요"},
	{"/roll", "주사위는 2d6 처럼 적어 주세요 (1~20개, 2~1000면, 보정 ±1000)"},
	{"/roll 30d6", "주사위는 2d6 처럼 적어 주세요 (1~20개, 2~1000면, 보정 ±1000)"},
	{"/invite", "사용법: /invite @아이디 | link [7d] [횟수]"},
	{"/invite link 7d 0", "초대 횟수는 1~1000번이에요"},
	{"/invite link 1s", "초대 링크 만료는 5분~30일이에요"},
	{"/invite @x", "아이디는 영문 소문자·숫자·_ 3~20자예요"},
	{`/poll "질문`, "따옴표가 닫히지 않았어요"},
	{`/poll`, `사용법: /poll "질문" "선택지"… [--multi] [--for 1h]`},
	{`/poll "질문" "A" "B" --for 10s`, "투표 기간은 1분~7일이에요"},
	{`/poll "" "A" "B"`, "질문과 선택지는 비울 수 없어요"},
	{`/poll "질문" "A"`, "선택지를 2개 이상 적어 주세요"},
	{`/poll "질문" "1" "2" "3" "4" "5" "6" "7" "8" "9" "10" "11"`, "선택지는 10개까지예요"},
}

// TestSlashMessagesKO: Message in ko (and err.Error()) is the 0.1.2 sentence.
func TestSlashMessagesKO(t *testing.T) {
	for _, c := range oldMessages {
		_, _, err := Parse(c.in)
		if err == nil {
			t.Errorf("Parse(%q): no error", c.in)
			continue
		}
		if got := Message(i18n.KoT(), err); got != c.want {
			t.Errorf("Message(ko, Parse(%q)) = %q, want %q", c.in, got, c.want)
		}
		if got := Message(nil, err); got != c.want {
			t.Errorf("Message(nil, Parse(%q)) = %q, want the ko %q", c.in, got, c.want)
		}
		if got := err.Error(); got != c.want {
			t.Errorf("Parse(%q).Error() = %q, want %q", c.in, got, c.want)
		}
	}
	// Typed errors: callers tell them apart with errors.Is / errors.As.
	if _, _, err := Parse("/focus 13h"); !errors.Is(err, ErrFocusRange) {
		t.Errorf("/focus 13h err = %v, want ErrFocusRange", err)
	}
	if _, _, err := Parse("/roll x"); !errors.Is(err, ErrDice) {
		t.Errorf("/roll x err = %v, want ErrDice", err)
	}
	var u UsageError
	if _, _, err := Parse("/me"); !errors.As(err, &u) || u.Name != Me {
		t.Errorf("/me err = %v, want UsageError{Me}", err)
	}
	if got := Message(i18n.KoT(), errors.New("plain")); got != "plain" {
		t.Errorf("Message(other error) = %q, want its text", got)
	}
	if got := Message(i18n.KoT(), nil); got != "" {
		t.Errorf("Message(nil err) = %q", got)
	}
}

// TestSlashMessagesEN: every parse error says it in English.
func TestSlashMessagesEN(t *testing.T) {
	en := i18n.For(i18n.En)
	for _, c := range oldMessages {
		_, _, err := Parse(c.in)
		got := Message(en, err)
		if got == "" || hasHangul(got) {
			t.Errorf("Message(en, Parse(%q)) = %q: empty or Korean", c.in, got)
		}
	}
	_, _, err := Parse("/focus 13h")
	if got := Message(en, err); !strings.Contains(got, "12") {
		t.Errorf("en /focus 13h = %q, want 12 in it", got)
	}
	_, _, err = Parse("/nope")
	if got := Message(en, err); !strings.Contains(got, "/nope") {
		t.Errorf("en unknown = %q, want /nope in it", got)
	}
}

// TestCatalogNamesUntranslated: command names, and the names inside usages,
// stay as they are in every language.
func TestCatalogNamesUntranslated(t *testing.T) {
	ko, en := Catalog(i18n.KoT()), Catalog(i18n.For(i18n.En))
	if len(ko) != len(en) {
		t.Fatalf("Catalog ko %d rows, en %d", len(ko), len(en))
	}
	for i := range en {
		if en[i].Name != ko[i].Name {
			t.Errorf("row %d name en %q, ko %q", i, en[i].Name, ko[i].Name)
		}
		if !strings.HasPrefix(en[i].Usage, "/"+string(en[i].Name)) {
			t.Errorf("en usage %q must start with /%s", en[i].Usage, en[i].Name)
		}
		if hasHangul(en[i].Usage) || hasHangul(en[i].Desc) || en[i].Desc == "" {
			t.Errorf("en row %+v: Korean or empty", en[i])
		}
	}
	for _, s := range en {
		if s.Name == Poll && !strings.Contains(s.Usage, "--multi") {
			t.Errorf("en poll usage %q lost --multi", s.Usage)
		}
		if s.Name == Shrug && !strings.Contains(s.Desc, shrugFace) {
			t.Errorf("en shrug desc %q lost the face", s.Desc)
		}
	}
}
