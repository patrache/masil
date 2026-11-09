package chat

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/slash"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/palette"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// sendSlash focuses the composer of h's channel view, types in and presses enter.
func sendSlash(t testing.TB, h *uitest.Harness, in string) {
	t.Helper()
	if !channelOf(t, h).composer.focused() {
		h.Press("i")
	}
	h.Type(in)
	h.Press("enter")
}

// lastPost is the body of the last POST to the channel's messages.
func lastPost(t testing.TB, h *uitest.Harness, ch string) []byte {
	t.Helper()
	r := reqs(h.API, "POST", "/v1/channels/"+ch+"/messages")
	if len(r) == 0 {
		t.Fatalf("no POST to %s", ch)
	}
	return r[len(r)-1].Body
}

// mustJSONObject fails unless body is JSON equal to want (a JSON object literal).
func mustJSONObject(t testing.TB, body []byte, want string) {
	t.Helper()
	var got, exp any
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("body is not JSON: %v: %s", err, body)
	}
	if err := json.Unmarshal([]byte(want), &exp); err != nil {
		t.Fatalf("bad want %q: %v", want, err)
	}
	if !reflect.DeepEqual(got, exp) {
		t.Fatalf("body %s, want %s", body, want)
	}
}

// pollOf is the JSON of the "poll" field of a send body.
func pollOf(t testing.TB, body []byte) string {
	t.Helper()
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body is not a JSON object: %v: %s", err, body)
	}
	return string(m["poll"])
}

// toastText is the text of the toast on screen, wrapped lines joined by one space.
func toastText(t testing.TB, h *uitest.Harness) string {
	t.Helper()
	scr := h.Screen()
	tb := h.Model.Frame().Toast
	if tb == nil {
		t.Fatalf("no toast:\n%s", scr)
	}
	r := tb.Rect
	if tb.Bordered {
		r = r.Inner()
	}
	f := strings.Fields(columnText(scr, r))
	if len(f) > 0 && (f[0] == "✕" || f[0] == "●" || f[0] == "★" || f[0] == "○") {
		f = f[1:]
	}
	return strings.Join(f, " ")
}

var (
	recordOnce sync.Once
	recorded   *[]string
)

// recordCommands registers the palette commands ids once per test binary
// (palette.Register panics on duplicates); each call starts a fresh record of
// "id|args" runs.
func recordCommands(t testing.TB, ids ...string) *[]string {
	t.Helper()
	got := &[]string{}
	recorded = got
	recordOnce.Do(func() {
		for _, id := range ids {
			if _, ok := palette.Lookup(id); ok {
				t.Fatalf("palette command %q already registered in this binary", id)
			}
			palette.Register(palette.Command{ID: id, Title: id, Group: palette.GroupCommand, Run: func(_ *ui.Context, args string) tea.Cmd {
				*recorded = append(*recorded, id+"|"+args)
				return nil
			}})
		}
	})
	return got
}

func TestRawArgs(t *testing.T) {
	for in, want := range map[string]string{
		"/status 🎧 듣는 중":  "🎧 듣는 중",
		"  /focus   25m ": "25m",
		"/focus":          "",
		"/status\t바쁨  ":   "바쁨",
		"/status a\nb":    "a\nb",
	} {
		if got := rawArgs(in); got != want {
			t.Errorf("rawArgs(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSlashSendsKinds(t *testing.T) {
	cases := []struct{ in, kind, body string }{
		{"/me 커피 마시는 중", "me", "커피 마시는 중"},
		{"/shrug 몰라요", "text", `몰라요 ¯\_(ツ)_/¯`},
		{"/shrug", "text", `¯\_(ツ)_/¯`},
		{"/roll 2d6", "roll", "2d6"},
		{"/없는명령 그대로", "text", "/없는명령 그대로"},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			h := devChannel(t, uitest.Size{W: 120, H: 40})
			sendSlash(t, h, c.in)
			ch := devID(t, h)
			if n := len(reqs(h.API, "POST", "/v1/channels/"+ch+"/messages")); n != 1 {
				t.Fatalf("one POST, got %d", n)
			}
			b := lastPost(t, h, ch)
			mustJSONEq(t, b, "kind", c.kind)
			mustJSONEq(t, b, "body", c.body)
			if v := channelOf(t, h).composer.c.Value(); v != "" {
				t.Fatalf("composer cleared after a send, got %q", v)
			}
			if p := chatOf(h.Context()).Pending(ch); len(p) != 0 {
				t.Fatalf("echoed: no pending left %+v", p)
			}
			h.Check()
		})
	}
}

func TestSlashPoll(t *testing.T) {
	const in = `/poll "점심 메뉴" "국밥" "돈까스" --multi`
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	sendSlash(t, h, in)
	ch := devID(t, h)
	b := lastPost(t, h, ch)
	mustJSONEq(t, b, "kind", "poll")
	var got struct {
		Poll struct {
			Question string   `json:"question"`
			Options  []string `json:"options"`
			Multi    bool     `json:"multi"`
		} `json:"poll"`
	}
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.Poll.Question != "점심 메뉴" || !slices.Equal(got.Poll.Options, []string{"국밥", "돈까스"}) || !got.Poll.Multi {
		t.Fatalf("poll %+v in %s", got.Poll, b)
	}
	mustContain(t, h.Screen(), "점심 메뉴", "국밥", "돈까스")

	// a thread composer refuses /poll (api.md: polls are top-level only)
	root := "01JDDEV0000000000000000001"
	cc := newChatComposer(ch, &root, "")
	cc.c.SetValue(in)
	before := len(h.API.Requests())
	cmd := cc.submit(h.Context())
	if cmd == nil {
		t.Fatal("refusal toast expected")
	}
	if tm, ok := cmd().(ui.ToastMsg); !ok || tm.Text != "스레드에서는 투표를 만들 수 없어요" {
		t.Fatalf("toast %#v", tm)
	}
	if n := len(h.API.Requests()); n != before {
		t.Fatalf("no request from a thread /poll (%d new)", n-before)
	}
	if cc.c.Value() != in {
		t.Fatalf("draft kept, got %q", cc.c.Value())
	}
	if p := pendingFor(h.Context(), ch, root); len(p) != 0 {
		t.Fatalf("no pending reply: %+v", p)
	}
}

func TestSlashPollRetryKeepsPoll(t *testing.T) {
	srv := newAPI(t)
	ch := chanID(t, seeded(), "개발")
	fail := true
	// exact path: more specific than chatRoutes' {id} pattern, so it wins without a conflict
	srv.Handle("POST /v1/channels/"+ch+"/messages", func(w http.ResponseWriter, r *http.Request) {
		if fail {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(503)
			io.WriteString(w, `{"error":{"code":"service_unavailable","message":"x","details":{}}}`)
			return
		}
		r.SetPathValue("id", ch)
		echoSend(false)(w, r)
	})
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: ch}, srv, uitest.WithSeed(bootstrappedSeed))
	sendSlash(t, h, `/poll "점심 메뉴" "국밥" "돈까스"`)
	p := chatOf(h.Context()).Pending(ch)
	if len(p) != 1 || p[0].Kind != "poll" || !api.IsCode(p[0].Failed, "service_unavailable") {
		t.Fatalf("one failed poll pending: %+v", p)
	}
	mustContain(t, h.Screen(), "📊 점심 메뉴", "전송 실패") // the pending row reads like the poll's preview
	fail = false
	h.Press("esc", "G", "enter") // the failed row is last & selected; enter = resend
	r := reqs(srv, "POST", "/v1/channels/"+ch+"/messages")
	if len(r) != 2 {
		t.Fatalf("2 POSTs, got %d", len(r))
	}
	if jsonField(r[0].Body, "client_nonce") == "" || jsonField(r[0].Body, "client_nonce") != jsonField(r[1].Body, "client_nonce") {
		t.Fatal("same nonce on resend")
	}
	for _, x := range r {
		mustJSONEq(t, x.Body, "kind", "poll")
		mustJSONObject(t, []byte(pollOf(t, x.Body)), `{"question":"점심 메뉴","options":["국밥","돈까스"]}`)
	}
	if strings.Contains(h.Screen(), "전송 실패") {
		t.Fatal("cleared after the resend")
	}

	// discard: the kept poll input goes with the row
	fail = true
	h.Press("i")
	h.Type(`/poll "저녁" "피자" "치킨"`)
	h.Press("enter")
	p = chatOf(h.Context()).Pending(ch)
	if len(p) != 1 || p[0].Failed == nil {
		t.Fatalf("one failed pending: %+v", p)
	}
	nonce := p[0].Nonce
	if chatOf(h.Context()).PendingPoll(ch, nonce) == nil {
		t.Fatal("the poll input is kept for resend")
	}
	h.Press("esc", "G", "d")
	if strings.Contains(h.Screen(), "저녁") {
		t.Fatal("discarded row gone")
	}
	if chatOf(h.Context()).PendingPoll(ch, nonce) != nil {
		t.Fatal("discard drops the kept poll input")
	}
}

func TestSlashStatusFocusRunCommands(t *testing.T) {
	got := recordCommands(t, "status.set", "focus.start", "focus.stop")
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	for _, in := range []string{"/status 🎧 듣는 중", "/focus 25m", "/focus", "/focus off"} {
		sendSlash(t, h, in) // the composer keeps the focus after each command
		if v := channelOf(t, h).composer.c.Value(); v != "" {
			t.Fatalf("%q: composer cleared, got %q", in, v)
		}
	}
	want := []string{"status.set|🎧 듣는 중", "focus.start|25m", "focus.start|", "focus.stop|"}
	if !slices.Equal(*got, want) {
		t.Fatalf("%v", *got)
	}
	if n := len(reqs(h.API, "POST", "/v1/channels/"+devID(t, h)+"/messages")); n != 0 {
		t.Fatalf("status/focus never send a message (%d)", n)
	}
}

func TestSlashUsageError(t *testing.T) {
	for _, in := range []string{"/roll", "/roll 99d6", "/abc 몰라"} {
		t.Run(in, func(t *testing.T) {
			_, ok, err := slash.Parse(in)
			if !ok || err == nil {
				t.Fatalf("%q must be a usage error", in)
			}
			h := devChannel(t, uitest.Size{W: 120, H: 40})
			n := len(h.API.Requests())
			sendSlash(t, h, in)
			if got := h.API.Requests(); len(got) != n {
				t.Fatalf("no request on a usage error: %+v", got[n:])
			}
			if v := channelOf(t, h).composer.c.Value(); v != in {
				t.Fatalf("draft kept, got %q", v)
			}
			if got := toastText(t, h); got != strings.Join(strings.Fields(err.Error()), " ") {
				t.Fatalf("toast %q, want the usage error %q", got, err.Error())
			}
			h.Check()
		})
	}
}

// directInviteAnswer is a 201 answer to POST /v1/spaces/{id}/direct-invites.
func directInviteAnswer(t testing.TB, s string) map[string]any {
	t.Helper()
	st := seeded()
	return map[string]any{"invite": api.DirectInvite{
		ID: "01JHDIRECT000000000000000A", Space: api.SpacePreview{ID: s, Name: st.Spaces[s].Name},
		Inviter: st.Me.UserSummary, Invitee: userBy(t, st, "hana"), Status: "pending", CreatedAt: uitest.Start,
	}}
}

func TestSlashInviteUser(t *testing.T) {
	srv := newAPI(t)
	nova := fixtures.SpaceNova
	srv.JSON("POST /v1/spaces/"+nova+"/direct-invites", http.StatusCreated, directInviteAnswer(t, nova))
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, srv, uitest.WithSeed(bootstrappedSeed))
	sendSlash(t, h, "/invite @hana")
	r := reqs(srv, "POST", "/v1/spaces/"+nova+"/direct-invites")
	if len(r) != 1 {
		t.Fatalf("one direct invite, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"user":"@hana"}`)
	hana := userBy(t, seeded(), "hana")
	mustContain(t, h.Screen(), hana.DisplayName+"님을 노바 개발방에 초대했어요")
	if v := channelOf(t, h).composer.c.Value(); v != "" {
		t.Fatalf("composer cleared, got %q", v)
	}
	if n := len(reqs(srv, "POST", "/v1/channels/"+devID(t, h)+"/messages")); n != 0 {
		t.Fatalf("an invite sends no message (%d)", n)
	}

	// a DM has no space
	d := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: fixtures.DMMinji}, srv, uitest.WithSeed(bootstrappedSeed))
	n := len(srv.Requests())
	sendSlash(t, d, "/invite @hana")
	mustContain(t, d.Screen(), "방 채널에서만 초대할 수 있어요")
	if len(srv.Requests()) != n {
		t.Fatalf("no request from a DM: %+v", srv.Requests()[n:])
	}
	if v := channelOf(t, d).composer.c.Value(); v != "/invite @hana" {
		t.Fatalf("draft kept, got %q", v)
	}
}

func TestSlashInviteUserFails(t *testing.T) {
	srv := newAPI(t)
	srv.Error("POST /v1/spaces/"+fixtures.SpaceNova+"/direct-invites", http.StatusForbidden, "action_not_allowed")
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, srv, uitest.WithSeed(bootstrappedSeed))
	sendSlash(t, h, "/invite @hana")
	mustContain(t, toastText(t, h), inviteFailedText)
	if strings.Contains(h.Screen(), "초대했어요") {
		t.Fatal("no success toast on failure")
	}
}

// inviteAnswer is a 201 answer to POST /v1/spaces/{id}/invites.
func inviteAnswer(code string, maxUses *int, expires *time.Time) map[string]any {
	st := seeded()
	return map[string]any{"invite": api.Invite{
		Code: code, Space: api.SpacePreview{ID: fixtures.SpaceNova, Name: st.Spaces[fixtures.SpaceNova].Name},
		CreatedBy: st.Me.UserSummary, MaxUses: maxUses, ExpiresAt: expires, CreatedAt: uitest.Start,
	}}
}

// clipboards are the texts of the tea.SetClipboard messages h's Cmds produced.
func clipboards(h *uitest.Harness) []string {
	var out []string
	for _, m := range h.TeaMsgs() {
		if reflect.TypeOf(m).Name() == "setClipboardMsg" {
			out = append(out, fmt.Sprint(m))
		}
	}
	return out
}

func TestSlashInviteLink(t *testing.T) {
	srv := newAPI(t)
	nova := fixtures.SpaceNova
	exp := uitest.Start.Add(7 * 24 * time.Hour)
	srv.JSON("POST /v1/spaces/"+nova+"/invites", http.StatusCreated, inviteAnswer("AB12CD34", ptrTo(10), &exp))
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, srv, uitest.WithSeed(bootstrappedSeed))
	sendSlash(t, h, "/invite link 7d 10")
	r := reqs(srv, "POST", "/v1/spaces/"+nova+"/invites")
	if len(r) != 1 {
		t.Fatalf("one invite, got %d", len(r))
	}
	mustJSONObject(t, r[0].Body, `{"max_uses":10,"expires_in_sec":604800}`)
	if len(h.Model.Frame().Overlays) != 1 {
		t.Fatalf("the invite overlay opens, overlays %d", len(h.Model.Frame().Overlays))
	}
	mustContain(t, h.Screen(), "AB12CD34", "masil join AB12CD34", "11월 12일 11:05", "10번")
	h.Press("y")
	if c := clipboards(h); !slices.Equal(c, []string{"masil join AB12CD34"}) {
		t.Fatalf("clipboard %q", c)
	}
	mustContain(t, h.Screen(), "복사했어요")
	h.Press("esc")
	if len(h.Model.Frame().Overlays) != 0 {
		t.Fatal("esc closes")
	}

	srv.JSON("POST /v1/spaces/"+nova+"/invites", http.StatusCreated, inviteAnswer("ZZ99YY88", nil, nil))
	sendSlash(t, h, "/invite link")
	r = reqs(srv, "POST", "/v1/spaces/"+nova+"/invites")
	if len(r) != 2 {
		t.Fatalf("second invite, got %d", len(r))
	}
	mustJSONObject(t, r[1].Body, `{}`) // server default 7d, unlimited uses
	mustContain(t, h.Screen(), "ZZ99YY88", "masil join ZZ99YY88", "무제한")
}

func TestSlashInviteLinkOneOverlayPerRequest(t *testing.T) {
	// two conversations of the same space are alive (two tabs): the result opens one overlay
	srv := newAPI(t)
	srv.JSON("POST /v1/spaces/"+fixtures.SpaceNova+"/invites", http.StatusCreated, inviteAnswer("AB12CD34", nil, nil))
	h := openWith(t, uitest.Size{W: 120, H: 40}, ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "개발")}, srv, uitest.WithSeed(bootstrappedSeed))
	other := newChannel(h.Context(), ui.Route{Kind: ui.RouteChannel, ID: chanID(t, seeded(), "일반")}).(*channelView)
	sendSlash(t, h, "/invite link")
	res := ui.ResultMsg[*api.Invite]{Tag: "chat.invite_create:" + fixtures.SpaceNova, Value: &api.Invite{Code: "AB12CD34"}}
	if cmd := other.Update(h.Context(), res); cmd != nil {
		if m := cmd(); m != nil {
			t.Fatalf("a conversation that did not ask ignores the result, got %#v", m)
		}
	}
	if len(h.Model.Frame().Overlays) != 1 {
		t.Fatalf("one overlay, got %d", len(h.Model.Frame().Overlays))
	}
}

func TestInviteCreatedArgsOnlyCode(t *testing.T) {
	h := devChannel(t, uitest.Size{W: 120, H: 40})
	h.Send(ui.OpenOverlayMsg{ID: "chat.invite_created", Args: "AB12CD34"})
	scr := h.Screen()
	mustContain(t, scr, "AB12CD34", "masil join AB12CD34")
	if strings.Contains(scr, "무제한") || strings.Contains(scr, "만료") {
		t.Fatal("code-only overlay shows no expiry/uses")
	}
}

func TestInviteCreatedGolden(t *testing.T) {
	forEachSize(t, overlaySizes, devChannel, func(t *testing.T, h *uitest.Harness) {
		h.Send(ui.OpenOverlayMsg{ID: "chat.invite_created", Args: "AB12CD34"})
		h.Golden("invite_created")
	})
}
