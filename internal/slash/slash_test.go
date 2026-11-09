package slash

import (
	"reflect"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
)

func intp(n int) *int { return &n }

func TestSlashParse(t *testing.T) {
	notSlash := []string{"hello", "/", "", "  ", "/ 띄어쓰기", "//escaped", "/usr/bin/env", "/없는명령 그대로", "a /me x"}
	for _, in := range notSlash {
		cmd, ok, err := Parse(in)
		if ok || err != nil || cmd != nil {
			t.Errorf("Parse(%q) = %+v, %v, %v; want not a command", in, cmd, ok, err)
		}
	}

	good := []struct {
		in   string
		want Command
	}{
		{"/me 춤춘다", Command{Name: Me, Text: "춤춘다"}},
		{"/ME  춤춘다 ", Command{Name: Me, Text: "춤춘다"}},
		{"/shrug 몰라", Command{Name: Shrug, Text: `몰라 ¯\_(ツ)_/¯`}},
		{"/shrug", Command{Name: Shrug, Text: `¯\_(ツ)_/¯`}},
		{`/poll "점심 메뉴" "국밥" "돈까스"`, Command{Name: Poll, Poll: &api.PollInput{Question: "점심 메뉴", Options: []string{"국밥", "돈까스"}}}},
		{`/poll “점심” “A” “B” --multi --for 1h`, Command{Name: Poll, Poll: &api.PollInput{Question: "점심", Options: []string{"A", "B"}, Multi: true, DurationSec: intp(3600)}}},
		{`/poll 점심 국밥 "돈 까스" --for 2d`, Command{Name: Poll, Poll: &api.PollInput{Question: "점심", Options: []string{"국밥", "돈 까스"}, DurationSec: intp(172800)}}},
		{"/status 🎧 듣는 중", Command{Name: Status, Emoji: "🎧", Text: "듣는 중"}},
		{"/status 회의 중", Command{Name: Status, Text: "회의 중"}},
		{"/status 中文 테스트", Command{Name: Status, Text: "中文 테스트"}},
		{"/focus", Command{Name: Focus}},
		{"/focus 25m", Command{Name: Focus, FocusDuration: 25 * time.Minute}},
		{"/focus 1h30m", Command{Name: Focus, FocusDuration: 90 * time.Minute}},
		{"/focus 30", Command{Name: Focus, FocusDuration: 30 * time.Minute}},
		{"/focus off", Command{Name: Focus, FocusOff: true}},
		{"/roll 2d6", Command{Name: Roll, Dice: "2d6"}},
		{"/roll 2d6+3", Command{Name: Roll, Dice: "2d6+3"}},
		{"/roll 1D1000-1000", Command{Name: Roll, Dice: "1d1000-1000"}},
		{"/invite @MinJi", Command{Name: Invite, InviteUser: "minji"}},
		{"/invite hana_01", Command{Name: Invite, InviteUser: "hana_01"}},
		{"/invite link 7d 10", Command{Name: Invite, InviteLink: true, LinkExpires: 168 * time.Hour, LinkMaxUses: 10}},
		{"/invite link 10", Command{Name: Invite, InviteLink: true, LinkMaxUses: 10}},
		{"/invite link 12h", Command{Name: Invite, InviteLink: true, LinkExpires: 12 * time.Hour}},
		{"/invite link", Command{Name: Invite, InviteLink: true}},
	}
	for _, c := range good {
		cmd, ok, err := Parse(c.in)
		if !ok || err != nil || cmd == nil {
			t.Errorf("Parse(%q) = %+v, %v, %v; want ok", c.in, cmd, ok, err)
			continue
		}
		if !reflect.DeepEqual(*cmd, c.want) {
			t.Errorf("Parse(%q) = %+v (poll %+v), want %+v (poll %+v)", c.in, *cmd, cmd.Poll, c.want, c.want.Poll)
		}
	}

	bad := []struct{ in, want string }{
		{`/poll "질문" "A"`, "선택지를 2개 이상 적어 주세요"},
		{`/poll "질문" "1" "2" "3" "4" "5" "6" "7" "8" "9" "10" "11"`, "선택지는 10개까지예요"},
		{"/nope x", "알 수 없는 명령이에요: /nope"},
		{"/focus abc", ""},
		{"/focus 0m", ""},
		{"/roll", ""},
		{"/roll 30d6", ""},
		{"/roll 2d1", ""},
		{"/roll 2d6+1001", ""},
		{"/roll 2d6 3", ""},
		{"/me", ""},
		{"/status", ""},
		{"/invite", ""},
		{"/invite @x", ""},
		{"/invite link 1s", ""},
		{"/invite link 7d 0", ""},
		{"/invite link 7d 10 3", ""},
		{`/poll "질문`, ""},
		{`/poll`, ""},
		{`/poll "질문" "A" "B" --for`, ""},
		{`/poll "질문" "A" "B" --for 10s`, ""},
		{`/poll "질문" "A" "B" --frobnicate`, ""},
	}
	for _, c := range bad {
		cmd, ok, err := Parse(c.in)
		if !ok || err == nil || cmd != nil {
			t.Errorf("Parse(%q) = %+v, %v, %v; want ok=true with error", c.in, cmd, ok, err)
			continue
		}
		if c.want != "" && err.Error() != c.want {
			t.Errorf("Parse(%q) err = %q, want %q", c.in, err.Error(), c.want)
		}
		if !hasHangul(err.Error()) {
			t.Errorf("Parse(%q) err %q is not Korean", c.in, err.Error())
		}
	}
}

func hasHangul(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Hangul, r) {
			return true
		}
	}
	return false
}

func TestCatalogOrder(t *testing.T) {
	want := []Name{Me, Shrug, Poll, Status, Focus, Roll, Invite}
	cat := Catalog(nil)
	if len(cat) != len(want) {
		t.Fatalf("Catalog has %d entries, want %d", len(cat), len(want))
	}
	for i, s := range cat {
		if s.Name != want[i] {
			t.Errorf("Catalog[%d].Name = %q, want %q", i, s.Name, want[i])
		}
		if !strings.HasPrefix(s.Usage, "/"+string(s.Name)) {
			t.Errorf("Usage %q must start with /%s", s.Usage, s.Name)
		}
		if s.Desc == "" || !hasHangul(s.Desc) {
			t.Errorf("Desc %q must be non-empty Korean", s.Desc)
		}
	}
	if cat[0].Usage != "/me <행동>" || cat[0].Desc != "행동을 3인칭으로 보내요" {
		t.Errorf("me spec = %+v", cat[0])
	}
	if cat[1].Desc != `¯\_(ツ)_/¯ 를 붙여요` {
		t.Errorf("shrug desc = %q", cat[1].Desc)
	}
	cat[0].Usage = "mutated"
	if Catalog(nil)[0].Usage == "mutated" {
		t.Error("Catalog must return a copy")
	}
}

func TestSlashCatalogLocale(t *testing.T) {
	want := []Spec{
		{Me, "/me <행동>", "행동을 3인칭으로 보내요"},
	}
	cat := Catalog(i18n.KoT())
	if len(cat) != 7 {
		t.Fatalf("Catalog(ko) has %d rows, want 7", len(cat))
	}
	if cat[0] != want[0] {
		t.Fatalf("Catalog(ko)[0] = %+v, want %+v", cat[0], want[0])
	}
	if !reflect.DeepEqual(cat, catalog) {
		t.Fatalf("Catalog(ko) must equal the 0.1.2 table:\n%+v\n%+v", cat, catalog)
	}
	if got := Catalog(nil); !reflect.DeepEqual(got, cat) {
		t.Fatalf("Catalog(nil) = %+v, want the ko table", got)
	}
}
