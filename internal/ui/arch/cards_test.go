package arch

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/collab/collabtest"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
)

// sentCard is one POST /v1/channels/{id}/messages the harness's server got.
type sentCard struct {
	channel string
	Kind    string `json:"kind"`
	Body    string `json:"body"`
	Card    *struct {
		Type string                     `json:"type"`
		V    int                        `json:"v"`
		Data map[string]json.RawMessage `json:"data"`
	} `json:"card"`
}

// cardServer answers card sends with 201 and the message echoed back.
func cardServer(h *uitest.Harness) {
	h.API.Handle("POST /v1/channels/{channel_id}/messages", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Kind string    `json:"kind"`
			Body string    `json:"body"`
			Card *api.Card `json:"card"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		m := api.Message{ID: "01JDSENT000000000000000001", ChannelID: r.PathValue("channel_id"), Kind: in.Kind, Body: in.Body, Card: in.Card,
			Author: api.UserSummary{ID: fixtures.UserSam, Handle: "sam"}}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{"message": m})
	})
}

// sentCards are the message sends the server got.
func sentCards(t *testing.T, h *uitest.Harness) []sentCard {
	t.Helper()
	var out []sentCard
	for _, rq := range h.API.Requests() {
		if rq.Method != "POST" || !strings.HasPrefix(rq.Path, "/v1/channels/") || !strings.HasSuffix(rq.Path, "/messages") {
			continue
		}
		var s sentCard
		if err := json.Unmarshal(rq.Body, &s); err != nil {
			t.Fatalf("send body: %v: %s", err, rq.Body)
		}
		s.channel = strings.TrimSuffix(strings.TrimPrefix(rq.Path, "/v1/channels/"), "/messages")
		out = append(out, s)
	}
	return out
}

// pickChannel moves the send dialog's cursor to the conversation labelled label.
func pickChannel(t *testing.T, h *uitest.Harness, d *sendDialog, label string) {
	t.Helper()
	for range len(d.chans) + 1 {
		if d.chans[d.cur].label == label {
			return
		}
		h.Press("j")
	}
	var all []string
	for _, c := range d.chans {
		all = append(all, c.label)
	}
	t.Fatalf("no conversation %q in %v", label, all)
}

func fileRoute() ui.Route {
	return toolRoute("arch:file:t1", map[string]string{"file": fixtureName})
}

func TestSendDesignCardPreviewThenSend(t *testing.T) {
	r := fileRoute()
	d := loadFixture(t, "pay_v3")
	h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, d))
	cardServer(h)
	h.Resize(120, 40)
	c := canvasOf(t, h, r)
	h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	if c.sendDlg == nil {
		t.Fatal("arch.send_card opened no dialog")
	}
	scr := h.Screen()
	for _, want := range []string{"설계 카드 보내기", "#일반", "민지"} {
		if !strings.Contains(scr, want) {
			t.Errorf("the send dialog lacks %q:\n%s", want, scr)
		}
	}
	pickChannel(t, h, c.sendDlg, "#일반")
	h.Press("enter")
	if c.sendDlg.step != 1 {
		t.Fatal("enter did not reach the preview")
	}
	// the preview is the card as chat draws it
	res := archsim.Analyze(d, archsim.Options{})
	scr = h.Screen()
	for _, want := range []string{"#일반 에 이렇게 올라가요", "📐 결제 시스템 v3", "노드 " + fmt.Sprint(len(d.Nodes)), "추정"} {
		if !strings.Contains(scr, want) {
			t.Errorf("the preview lacks %q:\n%s", want, scr)
		}
	}
	h.Golden("120x40")
	h.Check()
	if n := len(sentCards(t, h)); n != 0 {
		t.Fatalf("%d sends before the confirm", n)
	}
	h.Press("enter")
	got := sentCards(t, h)
	if len(got) != 1 {
		t.Fatalf("sends = %d", len(got))
	}
	s := got[0]
	if s.channel != fixtures.ChGeneral || s.Kind != "card" || s.Card == nil || s.Card.Type != "arch.design" || s.Card.V != 1 {
		t.Fatalf("send = %+v", s)
	}
	want := h.Context().T.Archcanvas.CardBodyDesign("결제 시스템 v3", len(d.Nodes), res.P99Ms, humanize.Grouped(int64(res.Cost.TotalUSDMonth)))
	if s.Body != want || !strings.HasPrefix(s.Body, "📐 결제 시스템 v3 · 노드 ") || !strings.Contains(s.Body, "(추정)") {
		t.Errorf("body %q, want %q", s.Body, want)
	}
	if string(s.Card.Data["engine_version"]) != fmt.Sprint(archsim.EngineVersion) || string(s.Card.Data["doc_id"]) != "null" {
		t.Errorf("data = %s / %s", s.Card.Data["engine_version"], s.Card.Data["doc_id"])
	}
	var mini []string
	if err := json.Unmarshal(s.Card.Data["mini"], &mini); err != nil || len(mini) == 0 || len(mini) > 8 {
		t.Fatalf("mini = %s (%v)", s.Card.Data["mini"], err)
	}
	for _, l := range mini {
		if textwidth.Width(l) > 60 {
			t.Errorf("mini line wider than 60: %q", l)
		}
	}
	if c.sendDlg != nil && !strings.Contains(h.Screen(), "#일반 에 카드를 보냈어요") {
		t.Errorf("no sent toast:\n%s", h.Screen())
	}
}

// bigDesign is n API nodes in a row: well over the card's 16KB with its copy.
func bigDesign(n int) *archdoc.Design {
	d := archdoc.New("큰 설계")
	for i := range n {
		nd := archdoc.Defaults(archdoc.KindAPI)
		nd.ID = fmt.Sprintf("n%03d", i)
		nd.Label = fmt.Sprintf("서버 %d", i)
		nd.Pos = [2]int{(i % 20) * 14, (i / 20) * 6}
		d.Nodes = append(d.Nodes, nd)
	}
	return d
}

func TestDMCardCarriesCopy(t *testing.T) {
	r := fileRoute()
	d := loadFixture(t, "pay_v3")
	h := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, d))
	cardServer(h)
	h.Resize(120, 40)
	c := canvasOf(t, h, r)
	h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	pickChannel(t, h, c.sendDlg, "민지")
	h.Press("enter", "enter")
	got := sentCards(t, h)
	if len(got) != 1 || got[0].channel != fixtures.DMMinji || got[0].Card == nil {
		t.Fatalf("sends = %+v", got)
	}
	raw := got[0].Card.Data["design"]
	if len(raw) == 0 || string(raw) == "null" {
		t.Fatalf("a personal design to a DM carries no copy: %s", raw)
	}
	cp, err := archdoc.Decode(raw)
	if err != nil {
		t.Fatalf("the copy does not load: %v", err)
	}
	if string(mustEncode(t, cp)) != string(mustEncode(t, d)) {
		t.Error("the copy differs from the design")
	}

	// 280 nodes: the copy does not fit 16KB — a toast, nothing sent
	big := bigDesign(280)
	h2 := newHarness(t, uitest.WithRoute(r), withDesign(t, fixtureName, big))
	cardServer(h2)
	h2.Resize(120, 40)
	c2 := canvasOf(t, h2, r)
	h2.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	pickChannel(t, h2, c2.sendDlg, "민지")
	h2.Press("enter", "enter")
	if n := len(sentCards(t, h2)); n != 0 {
		t.Fatalf("an over-16KB card was sent (%d)", n)
	}
	if !strings.Contains(h2.Screen(), "방에 올려 공유하세요") {
		t.Errorf("no too-big toast:\n%s", h2.Screen())
	}
	if _, _, err := designCard(h2.Context().T, big, archsim.Analyze(big, archsim.Options{}), cardRef{}); err != errCardTooBig {
		t.Errorf("designCard(280 nodes) err = %v", err)
	}
}

// cardMessage is a kind "card" message carrying c.
func cardMessage(id string, c api.Card, body string) api.Message {
	return api.Message{ID: id, ChannelID: fixtures.DMMinji, Kind: api.MessageKindCard, Body: body, Card: &c,
		Author: api.UserSummary{ID: fixtures.UserMinji, Handle: "minji"}}
}

func TestSimCardReplayMatches(t *testing.T) {
	h := newHarness(t)
	h.Resize(120, 40)
	ctx := h.Context()
	d := loadFixture(t, "pay_v3")
	res := archsim.Analyze(d, archsim.Options{})
	card, body, err := simCard(ctx.T, d, res, cardRef{})
	if err != nil {
		t.Fatal(err)
	}
	if card.Type != ui.CardArchSim || !strings.Contains(body, "시뮬 · p99 ") {
		t.Fatalf("simCard = %s %q", card.Type, body)
	}
	var data simCardData
	if err := json.Unmarshal(card.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data.P99Ms != res.P99Ms || data.ErrorPermille != res.ErrorPermille || data.CostUSDMonth != res.Cost.TotalUSDMonth || data.Seed != fmt.Sprint(d.Seed) {
		t.Fatalf("sim data %+v vs result p99 %d err %d cost %d", data, res.P99Ms, res.ErrorPermille, res.Cost.TotalUSDMonth)
	}
	cr, ok := ui.CardFor(ui.CardArchSim)
	if !ok || cr.Key == nil {
		t.Fatal("arch.sim has no renderer with Key")
	}
	m := cardMessage("01JDSIM0000000000000000001", card, body)
	replay := func(m api.Message) string {
		t.Helper()
		if handled, _ := cr.Key(ctx, &m, tea.KeyPressMsg{Code: 'r', Text: "r"}); handled {
			t.Fatal("r (chat's reaction) was taken by the sim card")
		}
		handled, cmd := cr.Key(ctx, &m, tea.KeyPressMsg{Code: 'R', Text: "R"})
		if !handled || cmd == nil {
			t.Fatal("R was not handled on the sim card")
		}
		h.Send(cmd())
		return h.Screen()
	}
	scr := replay(m)
	if !strings.Contains(scr, "✓ 다시 돌려도 같아요") || !strings.Contains(scr, fmt.Sprintf("p99 %dms", res.P99Ms)) {
		t.Errorf("no match mark:\n%s", scr)
	}
	// a card whose numbers were changed: the replay says so
	data.P99Ms++
	tampered := card
	tampered.Data, _ = json.Marshal(data)
	if scr := replay(cardMessage("01JDSIM0000000000000000002", tampered, body)); !strings.Contains(scr, "✕ 다시 돌린 숫자가 달라요") {
		t.Errorf("no mismatch mark:\n%s", scr)
	}
	// the card is drawn with the replay hint and every number
	lines := ansi.Strip(strings.Join(cr.Lines(ctx, &m, 80), "\n"))
	for _, want := range []string{"📐 결제 시스템 v3 시뮬", fmt.Sprintf("p99 %dms", res.P99Ms), "추정", "R 다시 돌려 보기"} {
		if !strings.Contains(lines, want) {
			t.Errorf("sim card lacks %q:\n%s", want, lines)
		}
	}
}

// TestCardLinesHostile is the renderer alone on hostile data (the chat side is
// in archchat): never wider than asked, no control or bidi characters, and
// malformed data or an unknown v falls back to the body.
func TestCardLinesHostile(t *testing.T) {
	h := newHarness(t)
	ctx := h.Context()
	cr, ok := ui.CardFor(ui.CardArchDesign)
	if !ok {
		t.Fatal("arch.design has no renderer")
	}
	long := strings.Repeat("━", 200)
	var many []string
	for range 50 {
		many = append(many, "줄\x1b[31m빨강\u202Eevil"+long)
	}
	hostile, _ := json.Marshal(map[string]any{
		"title": "제목\x1b]0;x\x07\u202E" + strings.Repeat("가", 70), "space_id": nil, "doc_id": nil, "version_no": nil,
		"node_count": 3, "cost_usd_month": 12345, "price_asof": "2026-11", "engine_version": 99, "mini": many, "design": nil,
	})
	const body = "📐 대체 문구"
	cases := map[string]api.Card{
		"hostile":  {Type: ui.CardArchDesign, V: 1, Data: hostile},
		"v2":       {Type: ui.CardArchDesign, V: 2, Data: hostile},
		"not json": {Type: ui.CardArchDesign, V: 1, Data: json.RawMessage(`{"title":`)},
		"fixture":  {Type: ui.CardArchDesign, V: 1, Data: json.RawMessage(`{"title":"결제 v3","nodes":14,"edges":20}`)},
	}
	for name, c := range cases {
		m := cardMessage("01JDHOS0000000000000000001", c, body)
		for _, w := range []int{20, 40, 80, 120} {
			ls := cr.Lines(ctx, &m, w)
			if len(ls) == 0 || len(ls) > 16 {
				t.Errorf("%s @%d: %d lines", name, w, len(ls))
			}
			for _, l := range ls {
				if textwidth.Width(l) > w {
					t.Errorf("%s @%d: line wider than the pane: %q", name, w, ansi.Strip(l))
				}
				if s := ansi.Strip(l); strings.ContainsAny(s, "\x1b\x07\u202e") {
					t.Errorf("%s @%d: control or bidi character: %q", name, w, s)
				}
			}
			text := ansi.Strip(strings.Join(ls, "\n"))
			fallback := name != "hostile"
			if fallback != strings.Contains(text, body) {
				t.Errorf("%s @%d: body fallback = %v:\n%s", name, w, !fallback, text)
			}
			if name == "hostile" && w == 120 && (!strings.Contains(text, "엔진 버전이 달라") || strings.Count(text, "━") > 8*60) {
				t.Errorf("hostile card at 120:\n%s", text)
			}
		}
	}
}

// TestCardOpen: a room document's card opens it for a member; a card copy opens
// as a copy; with the flag off, or nothing to open, enter is the thread.
func TestCardOpen(t *testing.T) {
	h := newHarness(t)
	ctx := h.Context()
	d := loadFixture(t, "pay_v3")
	res := archsim.Analyze(d, archsim.Options{})
	cr, _ := ui.CardFor(ui.CardArchDesign)
	nav := func(m api.Message) (ui.Route, bool) {
		t.Helper()
		cmd := cr.Open(ctx, &m)
		if cmd == nil {
			return ui.Route{}, false
		}
		nm, ok := cmd().(ui.NavigateMsg)
		return nm.Route, ok
	}
	room, body, err := designCard(ctx.T, d, res, cardRef{spaceID: fixtures.SpaceNova, docID: "D0001", versionNo: 3})
	if err != nil {
		t.Fatal(err)
	}
	if r, ok := nav(cardMessage("01JDOPN0000000000000000001", room, body)); !ok || r.Kind != ui.RouteTool || r.Arg != ui.ToolArch || r.ID != keyDoc+"D0001" || r.Query[queryDoc] != "D0001" {
		t.Errorf("room card opens %+v", r)
	}
	// not a member of the room, no copy: the thread
	other, _, _ := designCard(ctx.T, d, res, cardRef{spaceID: "01JB0000000000000000000ZZ1", docID: "D0002"})
	if r, ok := nav(cardMessage("01JDOPN0000000000000000002", other, body)); !ok || r.Kind != ui.RouteThread {
		t.Errorf("a stranger's room card opens %+v", r)
	}
	personal, _, _ := designCard(ctx.T, d, res, cardRef{})
	id := "01JDOPN0000000000000000003"
	r, ok := nav(cardMessage(id, personal, body))
	if !ok || r.ID != keyMsg+id || r.Query[queryMsg] != id {
		t.Fatalf("copy card opens %+v", r)
	}
	h.Send(ui.NavigateMsg{Route: r})
	cv := canvasOf(t, h, r)
	if cv.sink.Status() != saveCopy || len(cv.design.Nodes) != len(d.Nodes) {
		t.Errorf("the copy opened as %v with %d nodes", cv.sink.Status(), len(cv.design.Nodes))
	}
	if ttl, _ := cv.Title(ctx); ttl != "결제 시스템 v3" {
		t.Errorf("copy title %q", ttl)
	}
	t.Setenv("MASIL_FEATURES", "")
	if r, ok := nav(cardMessage("01JDOPN0000000000000000004", room, body)); !ok || r.Kind != ui.RouteThread {
		t.Errorf("flag off: %+v", r)
	}
}

// TestCardCopyFetched: an arch:msg instance whose copy is not cached (a
// restart) loads the message.
func TestCardCopyFetched(t *testing.T) {
	d := loadFixture(t, "pay_v3")
	h0 := newHarness(t)
	card, body, err := designCard(h0.Context().T, d, archsim.Analyze(d, archsim.Options{}), cardRef{})
	if err != nil {
		t.Fatal(err)
	}
	id := "01JDFTC0000000000000000001"
	m := cardMessage(id, card, body)
	r := toolRoute(keyMsg+id, map[string]string{queryMsg: id})
	srv := apitest.New(t)
	fixtures.RegisterAPI(srv)
	srv.JSON("GET /v1/messages/{message_id}", http.StatusOK, map[string]any{"message": m})
	h := newHarness(t, uitest.WithAPI(srv), uitest.WithRoute(r))
	h.Resize(120, 40)
	waitUntil(t, h, "the copy", func() bool { return len(canvasOf(t, h, r).design.Nodes) == len(d.Nodes) })
	if canvasOf(t, h, r).sink.Status() != saveCopy {
		t.Error("not a copy")
	}
}

// docRequests counts GET /v1/docs/D1 (the document's metadata).
func docRequests(h *uitest.Harness) int {
	n := 0
	for _, r := range h.API.Requests() {
		if r.Method == "GET" && r.Path == "/v1/docs/D1" {
			n++
		}
	}
	return n
}

// TestRoomCardNeverCarriesCopy: a card from a room document never carries the
// document as a copy — not even before GetDoc has answered (the space is not
// known yet: arch.send_card asks for it again and says to wait) — so room
// content never leaves the room.
func TestRoomCardNeverCarriesCopy(t *testing.T) {
	hub := collabtest.New(t)
	sharedFixture(t, hub, api.Doc{SpaceID: fixtures.SpaceNova, Title: fixtureName})
	h, c, sh := docHarness(t, hub)
	cardServer(h)
	ctx := h.Context()
	d := c.design
	res := archsim.Analyze(d, archsim.Options{})
	for _, sim := range []bool{false, true} {
		mk := designCard
		if sim {
			mk = simCard
		}
		if _, _, err := mk(ctx.T, d, res, cardRef{room: true, docID: "D1"}); err != errNoRoomRef {
			t.Errorf("sim=%v: a room card without its space: err = %v", sim, err)
		}
	}

	// GetDoc has not answered (and fails again): no dialog, a toast, a retry
	sh.meta = nil
	h.API.Error("GET /v1/docs/{doc_id}", http.StatusInternalServerError, "internal")
	before := docRequests(h)
	h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	if c.sendDlg != nil {
		t.Fatal("arch.send_card opened before the document's space was known")
	}
	if !strings.Contains(h.Screen(), "문서 정보를 받는 중") {
		t.Errorf("no wait toast:\n%s", h.Screen())
	}
	if docRequests(h) == before {
		t.Error("GetDoc was not asked again")
	}
	if n := len(sentCards(t, h)); n != 0 {
		t.Fatalf("%d sends", n)
	}

	// GetDoc answers: the card names the document and carries no copy
	hub.Register(h.API)
	h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	waitUntil(t, h, "the document's metadata", func() bool { return sh.meta != nil })
	h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	if c.sendDlg == nil {
		t.Fatal("no send dialog once the metadata came")
	}
	pickChannel(t, h, c.sendDlg, "민지")
	h.Press("enter", "enter")
	got := sentCards(t, h)
	if len(got) != 1 || got[0].Card == nil {
		t.Fatalf("sends = %+v", got)
	}
	data := got[0].Card.Data
	if string(data["design"]) != "null" || string(data["doc_id"]) != `"D1"` || string(data["space_id"]) != fmt.Sprintf("%q", fixtures.SpaceNova) {
		t.Errorf("room card data: design %s doc %s space %s", data["design"], data["doc_id"], data["space_id"])
	}
}

// versionPosts counts POST /v1/docs/D1/versions (named versions made).
func versionPosts(h *uitest.Harness) int {
	n := 0
	for _, r := range h.API.Requests() {
		if r.Method == "POST" && r.Path == "/v1/docs/D1/versions" {
			n++
		}
	}
	return n
}

// TestRoomCardCarriesVersion: a room document's card names the document
// version its numbers come from — a version at the state I see (made at send
// time unless the newest already is that state) — and a member's replay of a
// sim card loads that version.
func TestRoomCardCarriesVersion(t *testing.T) {
	hub := collabtest.New(t)
	sharedFixture(t, hub, api.Doc{SpaceID: fixtures.SpaceNova, Title: fixtureName})
	h, c, _ := docHarness(t, hub)
	cardServer(h)
	send := func(sim bool) sentCard {
		t.Helper()
		before := len(sentCards(t, h))
		h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
		if c.sendDlg == nil {
			t.Fatal("no send dialog")
		}
		if sim {
			h.Press("s")
		}
		pickChannel(t, h, c.sendDlg, "#일반")
		h.Press("enter")
		waitUntil(t, h, "the preview", func() bool { return c.sendDlg.step == 1 })
		h.Press("enter")
		got := sentCards(t, h)
		if len(got) != before+1 {
			t.Fatalf("sends = %d", len(got))
		}
		return got[len(got)-1]
	}
	// my edits not saved yet: no version can name this state — a hint, nothing made or sent
	h.Send(ui.RunCommandMsg{ID: "arch.send_card"})
	c.sendDlg.unsaved = true
	h.Press("enter")
	if c.sendDlg.step != 0 || !strings.Contains(h.Screen(), "편집이 아직 저장 중") || versionPosts(h) != 0 {
		t.Fatalf("unsaved edits: step %d, %d versions:\n%s", c.sendDlg.step, versionPosts(h), h.Screen())
	}
	h.Press("esc")

	s := send(false)
	if v := string(s.Card.Data["version_no"]); v != "1" {
		t.Errorf("the design card's version_no = %s", v)
	}
	if n := versionPosts(h); n != 1 {
		t.Errorf("%d versions made", n)
	}
	vs, err := h.Context().API.ListDocVersions(t.Context(), "D1")
	if err != nil || len(vs) != 1 || vs[0].Name == nil || !strings.Contains(*vs[0].Name, "결제 시스템 v3") {
		t.Fatalf("versions %+v (%v)", vs, err)
	}
	// nothing changed since: the same version, none made
	s = send(true)
	if v := string(s.Card.Data["version_no"]); v != "1" || s.Card.Type != ui.CardArchSim {
		t.Errorf("the sim card's version_no = %s (%s)", v, s.Card.Type)
	}
	if n := versionPosts(h); n != 1 {
		t.Errorf("%d versions made for an unchanged document", n)
	}

	// a member replays the sim card from that version
	ctx := h.Context()
	cr, _ := ui.CardFor(ui.CardArchSim)
	m := api.Message{ID: "01JDVER0000000000000000001", ChannelID: fixtures.ChGeneral, Kind: api.MessageKindCard, Body: s.Body,
		Card: &api.Card{Type: s.Card.Type, V: s.Card.V, Data: mustJSON(t, s.Card.Data)}, Author: api.UserSummary{ID: fixtures.UserMinji, Handle: "minji"}}
	if lines := ansi.Strip(strings.Join(cr.Lines(ctx, &m, 100), "\n")); !strings.Contains(lines, "R 다시 돌려 보기") {
		t.Errorf("no replay hint on a member's room card:\n%s", lines)
	}
	handled, cmd := cr.Key(ctx, &m, tea.KeyPressMsg{Code: 'R', Text: "R"})
	if !handled || cmd == nil {
		t.Fatal("R not handled on a member's room sim card")
	}
	h.Send(cmd())
	if scr := h.Screen(); !strings.Contains(scr, "다시 돌려도 같아요") {
		t.Errorf("the replay of version 1 does not match:\n%s", scr)
	}
}

func mustJSON(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
