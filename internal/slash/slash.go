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
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
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
	// Tools opens the tool hub (v0.1.3 A8.1) — only while a tool feature flag
	// is on (features.Any); otherwise it is an unknown command, as before.
	Tools Name = "tools"
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

// rows is the command order of Catalog (completion & help).
var rows = []Name{Me, Shrug, Poll, Status, Focus, Roll, Invite}

// spec is n's catalog row in t's language.
func spec(t *i18n.T, n Name) Spec {
	c := t.Chat
	switch n {
	case Me:
		return Spec{Me, c.SlashMeUsage(), c.SlashMeDesc()}
	case Shrug:
		return Spec{Shrug, c.SlashShrugUsage(), c.SlashShrugDesc(shrugFace)}
	case Poll:
		return Spec{Poll, c.SlashPollUsage(), c.SlashPollDesc()}
	case Status:
		return Spec{Status, c.SlashStatusUsage(), c.SlashStatusDesc()}
	case Focus:
		return Spec{Focus, c.SlashFocusUsage(), c.SlashFocusDesc()}
	case Roll:
		return Spec{Roll, c.SlashRollUsage(), c.SlashRollDesc()}
	case Invite:
		return Spec{Invite, c.SlashInviteUsage(), c.SlashInviteDesc()}
	}
	return Spec{Name: n}
}

// UsageError is a command written the wrong way: the message shows Name's
// usage line.
type UsageError struct{ Name Name }

// Error is the ko message (screens use Message(ctx.T, err)).
func (e UsageError) Error() string { return Message(nil, e) }

// UnknownError is an ASCII name that is no command (here); Head is as typed.
type UnknownError struct{ Head string }

// Error is the ko message (screens use Message(ctx.T, err)).
func (e UnknownError) Error() string { return Message(nil, e) }

// parseError is a fixed parse error; its value is the catalog key suffix
// (chat.slash_err_<value>).
type parseError string

// Error is the ko message (screens use Message(ctx.T, err)).
func (e parseError) Error() string { return Message(nil, e) }

// Parse errors with a fixed message (errors.Is).
var (
	ErrFocusRange    error = parseError("focus_range")
	ErrDice          error = parseError("dice")
	ErrInviteUses    error = parseError("invite_uses")
	ErrInviteExpiry  error = parseError("invite_expiry")
	ErrInviteHandle  error = parseError("invite_handle")
	ErrUnclosedQuote error = parseError("unclosed_quote")
	ErrPollDuration  error = parseError("poll_duration")
	ErrPollEmpty     error = parseError("poll_empty")
	ErrPollOptions   error = parseError("poll_few")
	ErrPollTooMany   error = parseError("poll_many")
)

// Message is err (from Parse) in t's language (nil t = ko); any other error
// is its own text, nil is "".
func Message(t *i18n.T, err error) string {
	if err == nil {
		return ""
	}
	if t == nil {
		t = i18n.KoT()
	}
	var u UsageError
	var un UnknownError
	var p parseError
	switch {
	case errors.As(err, &u):
		if s := spec(t, u.Name); s.Usage != "" {
			return t.Chat.SlashUsage(s.Usage)
		}
		return t.Chat.SlashUsageCheck()
	case errors.As(err, &un):
		return t.Chat.SlashUnknown(un.Head)
	case errors.As(err, &p):
		if s, ok := t.Lookup("chat", "slash_err_"+string(p), nil); ok {
			return s
		}
		return "slash: " + string(p) // never: every parseError has a key
	}
	return err.Error()
}

var (
	nameRE   = regexp.MustCompile(`^[A-Za-z]+$`)
	diceRE   = regexp.MustCompile(`^(\d{1,2})d(\d{1,4})([+-]\d{1,4})?$`) // api.md 2.1
	handleRE = regexp.MustCompile(`^[a-z0-9_]{3,20}$`)
	daysRE   = regexp.MustCompile(`^(\d+)d(.*)$`)
	digitsRE = regexp.MustCompile(`^\d+$`)
)

// Parse: ok=false => not a slash command (send as text); err => a typed parse
// error (UsageError, UnknownError, Err*) — show it with Message(ctx.T, err).
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
	case Tools:
		if !features.Any() {
			return nil, true, unknownCommand(head)
		}
		c = &Command{Name: Tools} // arguments are ignored
	default:
		return nil, true, unknownCommand(head)
	}
	if err != nil {
		return nil, true, err
	}
	return c, true, nil
}

// Catalog lists every command for composer completion & help in t's language
// (nil = ko). Names are never translated.
func Catalog(t *i18n.T) []Spec {
	if t == nil {
		t = i18n.KoT()
	}
	out := make([]Spec, 0, len(rows)+1)
	for _, n := range rows {
		out = append(out, spec(t, n))
	}
	if features.Any() {
		out = append(out, Spec{Tools, "/tools", t.Tools.SlashDesc()})
	}
	return out
}

// unknownCommand is Parse's error for an ASCII name that is no command (here).
func unknownCommand(head string) error { return UnknownError{Head: head} }

func usage(n Name) error { return UsageError{Name: n} }

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
			return nil, ErrFocusRange
		}
		d = time.Duration(n) * time.Minute
	} else {
		var err error
		if d, err = parseDuration(rest); err != nil {
			return nil, usage(Focus)
		}
	}
	if d < minFocusSec*time.Second || d > maxFocusSec*time.Second {
		return nil, ErrFocusRange
	}
	return &Command{Name: Focus, FocusDuration: d}, nil
}

func parseRoll(rest string) (*Command, error) {
	expr := strings.ToLower(rest)
	m := diceRE.FindStringSubmatch(expr)
	if m == nil {
		return nil, ErrDice
	}
	count, _ := strconv.Atoi(m[1])
	faces, _ := strconv.Atoi(m[2])
	mod := 0
	if m[3] != "" {
		mod, _ = strconv.Atoi(m[3])
	}
	if count < 1 || count > maxDiceCount || faces < minDiceFaces || faces > maxDiceFaces ||
		mod < -maxDiceModifier || mod > maxDiceModifier {
		return nil, ErrDice
	}
	return &Command{Name: Roll, Dice: expr}, nil
}

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
					return nil, ErrInviteUses
				}
				c.LinkMaxUses, seenUses = n, true
				continue
			}
			d, err := parseDuration(a)
			if seenExp || err != nil {
				return nil, usage(Invite)
			}
			if d < minInviteSec*time.Second || d > maxInviteSec*time.Second {
				return nil, ErrInviteExpiry
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
		return nil, ErrInviteHandle
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
				return nil, ErrUnclosedQuote
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
					return nil, ErrPollDuration
				}
				in.DurationSec = &sec
			default:
				return nil, usage(Poll)
			}
			continue
		}
		if t.text == "" {
			return nil, ErrPollEmpty
		}
		pos = append(pos, t.text)
	}
	if len(pos) == 0 {
		return nil, usage(Poll)
	}
	in.Question, in.Options = pos[0], pos[1:]
	if len(in.Options) < 2 {
		return nil, ErrPollOptions
	}
	if len(in.Options) > maxPollOptions {
		return nil, ErrPollTooMany
	}
	return &Command{Name: Poll, Poll: in}, nil
}
