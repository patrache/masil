// Package slash parses composer slash commands (CA 4.9).
package slash

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
)

// Name is a slash command name.
type Name string

// Slash command names (CA 4.9, verbatim).
const (
	Me     Name = "me"
	Shrug       = "shrug"
	Poll        = "poll"
	Status      = "status"
	Focus       = "focus"
	Roll        = "roll"
	Invite      = "invite"
)

// Command is a parsed slash command.
type Command struct {
	Name          Name
	Text          string // /me, /shrug, /status text
	Emoji         string // /status
	Poll          *api.PollInput
	Dice          string // /roll "2d6"
	FocusOff      bool
	FocusDuration time.Duration // /focus 25m | /focus off | /focus (default)
	InviteUser    string
	InviteLink    bool
	LinkExpires   time.Duration
	LinkMaxUses   int // /invite @h | /invite link [7d] [10]
}

// Spec describes one slash command for completion and help.
type Spec struct {
	Name        Name
	Usage, Desc string
}

// shrugFace is appended to /shrug text (api.md 5.10).
const shrugFace = `¯\_(ツ)_/¯`

// Limits mirrored from api.md (server re-validates).
const (
	maxPollOptions  = 10
	minPollDuration = 60
	maxPollDuration = 604800
	minFocusSec     = 60
	maxFocusSec     = 43200
	minInviteSec    = 300
	maxInviteSec    = 2592000
	maxInviteUses   = 1000
	maxDiceCount    = 20
	minDiceFaces    = 2
	maxDiceFaces    = 1000
	maxDiceModifier = 1000
)

var catalog = []Spec{
	{Me, "/me <행동>", "행동을 3인칭으로 보내요"},
	{Shrug, "/shrug [글]", shrugFace + " 를 붙여요"},
	{Poll, `/poll "질문" "선택지"… [--multi] [--for 1h]`, "투표를 만들어요"},
	{Status, "/status [이모지] <글>", "상태 메시지를 바꿔요"},
	{Focus, "/focus [25m|off]", "집중 모드를 켜거나 꺼요"},
	{Roll, "/roll 2d6", "주사위를 굴려요"},
	{Invite, "/invite @아이디 | link [7d] [횟수]", "방에 초대해요"},
}

var (
	nameRE   = regexp.MustCompile(`^[A-Za-z]+$`)
	diceRE   = regexp.MustCompile(`^(\d{1,2})d(\d{1,4})([+-]\d{1,4})?$`) // api.md 2.1
	handleRE = regexp.MustCompile(`^[a-z0-9_]{3,20}$`)
	daysRE   = regexp.MustCompile(`^(\d+)d(.*)$`)
	digitsRE = regexp.MustCompile(`^\d+$`)
)

// Parse: ok=false => not a slash command (send as text); err => Korean usage message.
//
// Only "/" followed by ASCII letters is a command; anything else ("/", "/ x",
// "//x", "/usr/bin", "/없는명령") is ordinary text. Unknown ASCII names are
// reported as errors so typos are not sent by accident.
func Parse(input string) (cmd *Command, ok bool, err error) {
	s := strings.TrimSpace(input)
	if !strings.HasPrefix(s, "/") {
		return nil, false, nil
	}
	head, rest := cutSpace(s[1:])
	if !nameRE.MatchString(head) {
		return nil, false, nil
	}
	name := Name(strings.ToLower(head))
	var c *Command
	switch name {
	case Me:
		c, err = parseMe(rest)
	case Shrug:
		c = &Command{Name: Shrug, Text: strings.TrimSpace(rest + " " + shrugFace)}
	case Poll:
		c, err = parsePoll(rest)
	case Status:
		c, err = parseStatus(rest)
	case Focus:
		c, err = parseFocus(rest)
	case Roll:
		c, err = parseRoll(rest)
	case Invite:
		c, err = parseInvite(rest)
	default:
		return nil, true, errors.New("알 수 없는 명령이에요: /" + head)
	}
	if err != nil {
		return nil, true, err
	}
	return c, true, nil
}

// Catalog lists every command for composer completion & help.
func Catalog() []Spec {
	out := make([]Spec, len(catalog))
	copy(out, catalog)
	return out
}

func usage(n Name) error {
	for _, s := range catalog {
		if s.Name == n {
			return errors.New("사용법: " + s.Usage)
		}
	}
	return errors.New("사용법을 확인해 주세요")
}

// cutSpace splits s at the first whitespace run; rest is trimmed.
func cutSpace(s string) (head, rest string) {
	i := strings.IndexFunc(s, unicode.IsSpace)
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i:])
}

func parseMe(rest string) (*Command, error) {
	if rest == "" {
		return nil, usage(Me)
	}
	return &Command{Name: Me, Text: rest}, nil
}

func parseStatus(rest string) (*Command, error) {
	if rest == "" {
		return nil, usage(Status)
	}
	first, after := cutSpace(rest)
	if isEmoji(first) {
		return &Command{Name: Status, Emoji: first, Text: after}, nil
	}
	return &Command{Name: Status, Text: rest}, nil
}

// isEmoji: one grapheme, two cells wide, and not a Hangul/CJK/kana/fullwidth letter.
func isEmoji(tok string) bool {
	if tok == "" {
		return false
	}
	g, _ := ansi.FirstGraphemeCluster(tok, ansi.GraphemeWidth)
	if g != tok || ansi.StringWidth(tok) != 2 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(tok)
	switch {
	case unicode.In(r, unicode.Hangul, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Bopomofo):
		return false
	case r >= 0x3000 && r <= 0x303F, r >= 0xFF00 && r <= 0xFFEF, r >= 0x3130 && r <= 0x318F:
		return false // CJK punctuation, fullwidth forms, Hangul compatibility jamo
	}
	return true
}

func parseFocus(rest string) (*Command, error) {
	if rest == "" {
		return &Command{Name: Focus}, nil
	}
	if strings.ContainsFunc(rest, unicode.IsSpace) {
		return nil, usage(Focus)
	}
	if strings.EqualFold(rest, "off") {
		return &Command{Name: Focus, FocusOff: true}, nil
	}
	var d time.Duration
	if digitsRE.MatchString(rest) {
		n, err := strconv.Atoi(rest)
		if err != nil || n > maxFocusSec {
			return nil, errFocusRange
		}
		d = time.Duration(n) * time.Minute
	} else {
		var err error
		if d, err = parseDuration(rest); err != nil {
			return nil, usage(Focus)
		}
	}
	if d < minFocusSec*time.Second || d > maxFocusSec*time.Second {
		return nil, errFocusRange
	}
	return &Command{Name: Focus, FocusDuration: d}, nil
}

var errFocusRange = errors.New("집중 시간은 1분~12시간이에요")

func parseRoll(rest string) (*Command, error) {
	expr := strings.ToLower(rest)
	m := diceRE.FindStringSubmatch(expr)
	if m == nil {
		return nil, errDice
	}
	count, _ := strconv.Atoi(m[1])
	faces, _ := strconv.Atoi(m[2])
	mod := 0
	if m[3] != "" {
		mod, _ = strconv.Atoi(m[3])
	}
	if count < 1 || count > maxDiceCount || faces < minDiceFaces || faces > maxDiceFaces ||
		mod < -maxDiceModifier || mod > maxDiceModifier {
		return nil, errDice
	}
	return &Command{Name: Roll, Dice: expr}, nil
}

var errDice = errors.New("주사위는 2d6 처럼 적어 주세요 (1~20개, 2~1000면, 보정 ±1000)")

func parseInvite(rest string) (*Command, error) {
	fields := strings.Fields(rest)
	if len(fields) == 0 {
		return nil, usage(Invite)
	}
	if strings.EqualFold(fields[0], "link") {
		c := &Command{Name: Invite, InviteLink: true}
		args := fields[1:]
		if len(args) > 2 {
			return nil, usage(Invite)
		}
		seenUses, seenExp := false, false
		for _, a := range args {
			if digitsRE.MatchString(a) {
				n, err := strconv.Atoi(a)
				if seenUses || err != nil || n < 1 || n > maxInviteUses {
					return nil, errors.New("초대 횟수는 1~1000번이에요")
				}
				c.LinkMaxUses, seenUses = n, true
				continue
			}
			d, err := parseDuration(a)
			if seenExp || err != nil {
				return nil, usage(Invite)
			}
			if d < minInviteSec*time.Second || d > maxInviteSec*time.Second {
				return nil, errors.New("초대 링크 만료는 5분~30일이에요")
			}
			c.LinkExpires, seenExp = d, true
		}
		return c, nil
	}
	if len(fields) != 1 {
		return nil, usage(Invite)
	}
	h := strings.ToLower(strings.TrimPrefix(fields[0], "@"))
	if !handleRE.MatchString(h) {
		return nil, errors.New("아이디는 영문 소문자·숫자·_ 3~20자예요")
	}
	return &Command{Name: Invite, InviteUser: h}, nil
}

// parseDuration is time.ParseDuration plus a leading "<n>d" day component ("7d", "1d12h").
func parseDuration(s string) (time.Duration, error) {
	if m := daysRE.FindStringSubmatch(s); m != nil {
		days, err := strconv.Atoi(m[1])
		if err != nil || days > 365 {
			return 0, errors.New("bad duration")
		}
		d := time.Duration(days) * 24 * time.Hour
		if m[2] == "" {
			return d, nil
		}
		r, err := time.ParseDuration(m[2])
		if err != nil || r < 0 {
			return 0, errors.New("bad duration")
		}
		return d + r, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, errors.New("bad duration")
	}
	return d, nil
}

type token struct {
	text   string
	quoted bool
}

// tokenize splits on whitespace; "…" and “…” group words (a straight quote
// closes only a straight quote, “ closes on ” or ").
func tokenize(s string) ([]token, error) {
	var toks []token
	rs := []rune(s)
	for i := 0; i < len(rs); {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '"' || r == '“':
			j := i + 1
			for j < len(rs) && !closes(r, rs[j]) {
				j++
			}
			if j >= len(rs) {
				return nil, errors.New("따옴표가 닫히지 않았어요")
			}
			toks = append(toks, token{text: strings.TrimSpace(string(rs[i+1 : j])), quoted: true})
			i = j + 1
		default:
			j := i
			for j < len(rs) && !unicode.IsSpace(rs[j]) {
				j++
			}
			toks = append(toks, token{text: string(rs[i:j])})
			i = j
		}
	}
	return toks, nil
}

func closes(open, r rune) bool {
	if open == '"' {
		return r == '"'
	}
	return r == '”' || r == '"'
}

func parsePoll(rest string) (*Command, error) {
	toks, err := tokenize(rest)
	if err != nil {
		return nil, err
	}
	in := &api.PollInput{}
	var pos []string
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		if !t.quoted && strings.HasPrefix(t.text, "--") {
			switch t.text {
			case "--multi":
				in.Multi = true
			case "--for":
				if i+1 >= len(toks) {
					return nil, usage(Poll)
				}
				i++
				d, err := parseDuration(toks[i].text)
				if err != nil {
					return nil, usage(Poll)
				}
				sec := int(d / time.Second)
				if sec < minPollDuration || sec > maxPollDuration {
					return nil, errors.New("투표 기간은 1분~7일이에요")
				}
				in.DurationSec = &sec
			default:
				return nil, usage(Poll)
			}
			continue
		}
		if t.text == "" {
			return nil, errors.New("질문과 선택지는 비울 수 없어요")
		}
		pos = append(pos, t.text)
	}
	if len(pos) == 0 {
		return nil, usage(Poll)
	}
	in.Question, in.Options = pos[0], pos[1:]
	if len(in.Options) < 2 {
		return nil, errors.New("선택지를 2개 이상 적어 주세요")
	}
	if len(in.Options) > maxPollOptions {
		return nil, errors.New("선택지는 10개까지예요")
	}
	return &Command{Name: Poll, Poll: in}, nil
}
