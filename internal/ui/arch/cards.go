package arch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/archdoc"
	"github.com/patrache/masil/internal/archexport"
	"github.com/patrache/masil/internal/archsim"
	"github.com/patrache/masil/internal/archsim/pricing"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
)

// Chat cards arch.design and arch.sim (P3 Task 26, docs/contract/arch.md 3).
// The server checks their shape; this file re-checks everything before
// drawing — a card is someone else's input.

const (
	cardDataMax   = 16384 // CARD_DATA_MAX: the serialized data, UTF-8 bytes
	cardMiniLines = 8
	cardMiniCells = 60
	cardTitleMax  = 80 // code points
	cardBodyLines = 8  // the body fallback, wrapped
	cardNodesMax  = 300
	cardCopies    = 16 // card copies kept for arch:msg instances
)

// errCardTooBig: the card with its design copy is over 16KB (card_too_big).
var errCardTooBig = errors.New("arch: card data over 16KB")

// errNoRoomRef: a room document's card without the document's space (its
// metadata has not come yet) — it may not fall back to a copy.
var errNoRoomRef = errors.New("arch: room document card without its space")

// cardRef names the room document a card points at ("" docID = a personal
// design: the card carries a copy instead; versionNo 0 = unknown). room: the
// design is a room document — its card never carries a copy (room content
// stays in the room, JC12).
type cardRef struct {
	spaceID, docID string
	versionNo      int
	room           bool
}

// designCardData is arch.design v1 data.
type designCardData struct {
	Title         string          `json:"title"`
	SpaceID       *string         `json:"space_id"`
	DocID         *string         `json:"doc_id"`
	VersionNo     *int            `json:"version_no"`
	NodeCount     int             `json:"node_count"`
	CostUSDMonth  int             `json:"cost_usd_month"`
	PriceAsOf     string          `json:"price_asof"`
	EngineVersion int             `json:"engine_version"`
	Mini          []string        `json:"mini"`
	Design        json.RawMessage `json:"design"`
}

// simCardData is arch.sim v1 data.
type simCardData struct {
	Title         string          `json:"title"`
	SpaceID       *string         `json:"space_id"`
	DocID         *string         `json:"doc_id"`
	VersionNo     *int            `json:"version_no"`
	Seed          string          `json:"seed"`
	EngineVersion int             `json:"engine_version"`
	MaxRPS        int             `json:"max_rps"`
	ReadPermille  int             `json:"read_permille"`
	P50Ms         int             `json:"p50_ms"`
	P99Ms         int             `json:"p99_ms"`
	ErrorPermille int             `json:"error_permille"`
	CostUSDMonth  int             `json:"cost_usd_month"`
	PriceAsOf     string          `json:"price_asof"`
	Bottleneck    *cardBottleneck `json:"bottleneck"`
	Challenge     *cardChallenge  `json:"challenge"`
	Design        json.RawMessage `json:"design"`
}

type cardBottleneck struct {
	TargetLabel  string            `json:"target_label"`
	Limit        archsim.LimitKind `json:"limit"`
	UtilPermille int               `json:"util_permille"`
}

type cardChallenge struct {
	Date     string `json:"date"`
	Scenario string `json:"scenario"`
	Score    int    `json:"score"`
}

func init() {
	ui.RegisterCard(ui.CardArchDesign, ui.CardRenderer{Lines: cardLines, Open: openCard})
	ui.RegisterCard(ui.CardArchSim, ui.CardRenderer{Lines: cardLines, Open: openCard, Key: cardKey})
}

// ---- making cards ----

// cardTitle is the design's title cut to the card's 80 code points (the
// untitled name when empty).
func cardTitle(t *i18n.T, d *archdoc.Design) string {
	if s := cutCodePoints(cleanText(d.Title), cardTitleMax); s != "" {
		return s
	}
	return t.Archcanvas.Untitled()
}

// refFields are the card's space_id, doc_id and version_no.
func refFields(ref cardRef) (sp, doc *string, ver *int) {
	if ref.docID == "" || ref.spaceID == "" {
		return nil, nil, nil
	}
	s, d := ref.spaceID, ref.docID
	if ref.versionNo > 0 {
		v := ref.versionNo
		ver = &v
	}
	return &s, &d, ver
}

// designCopy is the compact design JSON a card carries when it names no room
// document (never for a room document: errNoRoomRef without its space).
func designCopy(d *archdoc.Design, ref cardRef) (json.RawMessage, error) {
	if ref.room && (ref.docID == "" || ref.spaceID == "") {
		return nil, errNoRoomRef
	}
	if ref.docID != "" && ref.spaceID != "" {
		return nil, nil
	}
	b, err := d.Encode()
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := json.Compact(&out, b); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

// sealCard marshals data into a card of typ; over 16KB = errCardTooBig.
func sealCard(typ string, data any) (api.Card, error) {
	b, err := json.Marshal(data)
	if err != nil {
		return api.Card{}, err
	}
	if len(b) > cardDataMax {
		return api.Card{}, errCardTooBig
	}
	return api.Card{Type: typ, V: 1, Data: b}, nil
}

func asOfOf(r *archsim.Result) string {
	if r != nil && r.Cost.AsOf != "" {
		return r.Cost.AsOf
	}
	return pricing.AsOf()
}

// designCard is the arch.design card of d (r = its instant analysis) and its
// body: the fallback line older clients show.
func designCard(t *i18n.T, d *archdoc.Design, r *archsim.Result, ref cardRef) (api.Card, string, error) {
	cp, err := designCopy(d, ref)
	if err != nil {
		return api.Card{}, "", err
	}
	mini := archexport.Mini(d, r)
	if len(mini) > cardMiniLines {
		mini = mini[:cardMiniLines]
	}
	for i, l := range mini {
		mini[i] = textwidth.Truncate(cleanText(l), cardMiniCells, "")
	}
	sp, doc, ver := refFields(ref)
	title := cardTitle(t, d)
	data := designCardData{
		Title: title, SpaceID: sp, DocID: doc, VersionNo: ver,
		NodeCount: min(len(d.Nodes), cardNodesMax), CostUSDMonth: max(r.Cost.TotalUSDMonth, 0),
		PriceAsOf: asOfOf(r), EngineVersion: archsim.EngineVersion, Mini: append([]string{}, mini...), Design: cp,
	}
	c, err := sealCard(ui.CardArchDesign, data)
	if err != nil {
		return api.Card{}, "", err
	}
	body := t.Archcanvas.CardBodyDesign(title, len(d.Nodes), r.P99Ms, humanize.Grouped(int64(data.CostUSDMonth)))
	return c, body, nil
}

// simCard is the arch.sim card of d's run r (same seed, same numbers
// anywhere); ch is a daily challenge result (nil = none, Task 28).
func simCard(t *i18n.T, d *archdoc.Design, r *archsim.Result, ref cardRef, ch *cardChallenge) (api.Card, string, error) {
	cp, err := designCopy(d, ref)
	if err != nil {
		return api.Card{}, "", err
	}
	sp, doc, ver := refFields(ref)
	title := cardTitle(t, d)
	data := simCardData{
		Title: title, SpaceID: sp, DocID: doc, VersionNo: ver,
		Seed: strconv.FormatUint(d.Seed, 10), EngineVersion: archsim.EngineVersion,
		MaxRPS: max(r.RPS, 0), ReadPermille: readPermille(d),
		P50Ms: max(r.P50Ms, 0), P99Ms: max(r.P99Ms, 0), ErrorPermille: min(max(r.ErrorPermille, 0), 1000),
		CostUSDMonth: max(r.Cost.TotalUSDMonth, 0), PriceAsOf: asOfOf(r), Challenge: ch, Design: cp,
	}
	if b := r.Bottleneck; b != nil {
		data.Bottleneck = &cardBottleneck{TargetLabel: targetLabel(d, b.Target), Limit: b.Limit, UtilPermille: max(b.UtilPermille, 0)}
	}
	c, err := sealCard(ui.CardArchSim, data)
	if err != nil {
		return api.Card{}, "", err
	}
	body := t.Archcanvas.CardBodySim(title, data.P99Ms, pct1(data.ErrorPermille), humanize.Grouped(int64(data.CostUSDMonth)))
	return c, body, nil
}

// readPermille is the first client's read share (by node id).
func readPermille(d *archdoc.Design) int {
	var ids []string
	byID := map[string]*archdoc.Traffic{}
	for i := range d.Nodes {
		if n := &d.Nodes[i]; n.Kind == archdoc.KindClient && n.Traffic != nil {
			ids = append(ids, n.ID)
			byID[n.ID] = n.Traffic
		}
	}
	if len(ids) == 0 {
		return archdoc.DefaultTraffic().ReadPermille
	}
	sort.Strings(ids)
	return min(max(byID[ids[0]].ReadPermille, 0), 1000)
}

// targetLabel is a node or edge's label for the card (its id when unlabelled).
func targetLabel(d *archdoc.Design, id string) string {
	l := id
	for _, n := range d.Nodes {
		if n.ID == id && cleanText(n.Label) != "" {
			l = n.Label
		}
	}
	if s := cutCodePoints(cleanText(l), cardTitleMax); s != "" {
		return s
	}
	return "?"
}

// ---- reading cards ----

var (
	reAsOf  = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)
	reSeed  = regexp.MustCompile(`^(0|[1-9]\d{0,19})$`)
	reDate  = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])$`)
	reScene = regexp.MustCompile(`^[a-z0-9_-]{1,64}$`)
	reToken = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`) // ids that go into instance keys and queries
)

// card is a decoded, checked arch.* card (design or sim; the other is nil).
type card struct {
	design *designCardData
	sim    *simCardData
}

func (c card) title() string {
	if c.sim != nil {
		return c.sim.Title
	}
	return c.design.Title
}

func (c card) ref() cardRef {
	sp, doc, ver := c.design.refs()
	if c.sim != nil {
		sp, doc, ver = c.sim.refs()
	}
	return cardRef{spaceID: sp, docID: doc, versionNo: ver}
}

func (c card) copyRaw() json.RawMessage {
	if c.sim != nil {
		return c.sim.Design
	}
	return c.design.Design
}

func (d *designCardData) refs() (string, string, int) {
	if d == nil {
		return "", "", 0
	}
	return deref(d.SpaceID), deref(d.DocID), derefInt(d.VersionNo)
}

func (d *simCardData) refs() (string, string, int) {
	if d == nil {
		return "", "", 0
	}
	return deref(d.SpaceID), deref(d.DocID), derefInt(d.VersionNo)
}

func derefInt(n *int) int {
	if n == nil {
		return 0
	}
	return *n
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// parseCard decodes m's card: an unknown type or v, malformed data or a value
// out of range => false (the body is drawn instead).
func parseCard(m *api.Message) (card, bool) {
	if m == nil || m.Card == nil || m.Card.V != 1 {
		return card{}, false
	}
	switch m.Card.Type {
	case ui.CardArchDesign:
		var d designCardData
		if json.Unmarshal(m.Card.Data, &d) != nil || !d.valid() {
			return card{}, false
		}
		return card{design: &d}, true
	case ui.CardArchSim:
		var s simCardData
		if json.Unmarshal(m.Card.Data, &s) != nil || !s.valid() {
			return card{}, false
		}
		return card{sim: &s}, true
	}
	return card{}, false
}

func validTitle(s string) bool {
	return utf8.RuneCountInString(s) <= cardTitleMax && cleanText(s) != ""
}

func validRef(sp, doc *string, ver *int, cp json.RawMessage) bool {
	hasCopy := len(cp) > 0 && string(cp) != "null"
	if doc != nil && (sp == nil || !reToken.MatchString(*doc)) {
		return false
	}
	if sp != nil && !reToken.MatchString(*sp) {
		return false
	}
	return (ver == nil || *ver >= 1) && !(hasCopy && doc != nil)
}

func (d *designCardData) valid() bool {
	return validTitle(d.Title) && validRef(d.SpaceID, d.DocID, d.VersionNo, d.Design) &&
		d.NodeCount >= 0 && d.NodeCount <= cardNodesMax && d.CostUSDMonth >= 0 &&
		reAsOf.MatchString(d.PriceAsOf) && d.EngineVersion >= 1
}

func (s *simCardData) valid() bool {
	if !validTitle(s.Title) || !validRef(s.SpaceID, s.DocID, s.VersionNo, s.Design) ||
		!reSeed.MatchString(s.Seed) || s.EngineVersion < 1 || !reAsOf.MatchString(s.PriceAsOf) ||
		s.MaxRPS < 0 || s.ReadPermille < 0 || s.ReadPermille > 1000 || s.P50Ms < 0 || s.P99Ms < 0 ||
		s.ErrorPermille < 0 || s.ErrorPermille > 1000 || s.CostUSDMonth < 0 {
		return false
	}
	if _, err := strconv.ParseUint(s.Seed, 10, 64); err != nil {
		return false
	}
	if b := s.Bottleneck; b != nil && (!validTitle(b.TargetLabel) || b.UtilPermille < 0 || !knownLimit(b.Limit)) {
		return false
	}
	if c := s.Challenge; c != nil && (!reDate.MatchString(c.Date) || !reScene.MatchString(c.Scenario) || c.Score < 0) {
		return false
	}
	return true
}

func knownLimit(l archsim.LimitKind) bool {
	switch l {
	case archsim.LimitCPU, archsim.LimitIOPS, archsim.LimitNIC, archsim.LimitPool, archsim.LimitExt, archsim.LimitQueue, archsim.LimitLink:
		return true
	}
	return false
}

// copyDesign loads the card's design copy (nil when it has none or it is broken).
func (c card) copyDesign() *archdoc.Design {
	raw := c.copyRaw()
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	d, err := archdoc.Decode(raw)
	if err != nil {
		return nil
	}
	return d
}

// ---- drawing ----

// cardLine is one line of a card before styling (the send preview draws the
// same lines into its buffer).
type cardLine struct {
	s  string
	st sty
}

// cardLines is the renderer's Lines.
func cardLines(ctx *ui.Context, m *api.Message, width int) []string {
	var out []string
	for _, l := range cardLinesOf(ctx, m, width) {
		out = append(out, lipStyle(ctx.Theme, l.st).Render(l.s))
	}
	return out
}

// cardLinesOf draws m's card at width: the title, the mini picture (re-cut to
// 8 lines of 60 cells), the summary (the cost wrapped, never cut), an engine
// version note and the key hint. Widget and tiny modes get the title alone.
func cardLinesOf(ctx *ui.Context, m *api.Message, width int) []cardLine {
	width = max(width, 1)
	c, ok := parseCard(m)
	if !ok {
		return bodyLines(m, width)
	}
	a := ctx.T.Archcanvas
	title := cleanText(c.title())
	var out []cardLine
	add := func(s string, st sty, wrap bool) {
		if !wrap {
			out = append(out, cardLine{s: textwidth.Truncate(s, width, "…"), st: st})
			return
		}
		for _, l := range textwidth.Wrap(s, width) {
			out = append(out, cardLine{s: l, st: st})
		}
	}
	compact := ctx.Mode == layout.ModeWidget || ctx.Mode == layout.ModeTiny
	cost := func(usd int, asof string) string {
		return a.CostShort(humanize.Grouped(int64(usd)), cleanText(asof))
	}
	var engine int
	if d := c.design; d != nil {
		engine = d.EngineVersion
		add(a.CardDesignTitle(title), sAccBold, false)
		if compact {
			return out
		}
		for i, l := range d.Mini {
			if i == cardMiniLines {
				break
			}
			add(textwidth.Truncate(cleanText(l), min(cardMiniCells, width), ""), sLine, false)
		}
		add(a.CardNodes(d.NodeCount)+" · "+cost(d.CostUSDMonth, d.PriceAsOf), sBase, true)
	} else {
		s := c.sim
		engine = s.EngineVersion
		add(a.CardSimTitle(title), sAccBold, false)
		if compact {
			return out
		}
		add(a.CardSimSummary(humanize.Grouped(int64(s.MaxRPS)), pct1(s.ReadPermille), s.P50Ms, s.P99Ms, pct1(s.ErrorPermille)), sBase, true)
		if b := s.Bottleneck; b != nil {
			add(a.CardBottleneck(cleanText(b.TargetLabel), limitName(ctx.T, b.Limit), pct1(b.UtilPermille)), sWarn, false)
		}
		if ch := s.Challenge; ch != nil {
			add(a.CardChallenge(ch.Date, ch.Scenario, ch.Score), sAcc, false)
		}
		add(cost(s.CostUSDMonth, s.PriceAsOf), sBase, true)
	}
	if engine != archsim.EngineVersion {
		add(a.EngineMismatch(), sWarn, true)
	}
	if hint := cardHint(ctx, c); hint != "" {
		add(hint, sDim, false)
	}
	return out
}

// cardHint says what enter (and R) do on the card for me.
func cardHint(ctx *ui.Context, c card) string {
	if !features.On(ui.ToolArch) {
		return ""
	}
	a := ctx.T.Archcanvas
	var parts []string
	switch {
	case member(ctx, c.ref()):
		parts = append(parts, a.CardOpenHint())
	case c.copyDesign() != nil:
		parts = append(parts, a.CardOpenCopyHint())
	}
	if canReplay(ctx, c) {
		parts = append(parts, a.CardReplayHint())
	}
	return strings.Join(parts, " · ")
}

// bodyLines is the fallback: the message body, sanitized and wrapped.
func bodyLines(m *api.Message, width int) []cardLine {
	body := ""
	if m != nil {
		body = cleanText(m.Body)
	}
	var out []cardLine
	for i, l := range textwidth.Wrap(body, width) {
		if i == cardBodyLines {
			break
		}
		out = append(out, cardLine{s: l, st: sBase})
	}
	return out
}

// member: the card names a room document of a space I am in.
func member(ctx *ui.Context, ref cardRef) bool {
	return ref.docID != "" && ref.spaceID != "" && ctx != nil && ctx.Store != nil && ctx.Store.Spaces[ref.spaceID] != nil
}

// ---- opening ----

// openCard is enter on a card: the room document for a member, else the
// card's copy, else (and with the flag off) the message's thread as for any
// message.
func openCard(ctx *ui.Context, m *api.Message) tea.Cmd {
	thread := messageThread(m)
	if !features.On(ui.ToolArch) {
		return thread
	}
	c, ok := parseCard(m)
	if !ok {
		return thread
	}
	if ref := c.ref(); member(ctx, ref) {
		return ui.OpenTool(ui.ToolArch, keyDoc+ref.docID, map[string]string{queryDoc: ref.docID})
	}
	if d := c.copyDesign(); d != nil {
		putCopy(m.ID, d)
		return ui.OpenTool(ui.ToolArch, keyMsg+m.ID, map[string]string{queryMsg: m.ID})
	}
	return thread
}

// messageThread opens m's thread (a reply: its root's).
func messageThread(m *api.Message) tea.Cmd {
	if m == nil {
		return nil
	}
	root := m.ID
	if m.ThreadRootID != nil && *m.ThreadRootID != "" {
		root = *m.ThreadRootID
	}
	return ui.Navigate(ui.Route{Kind: ui.RouteThread, ID: root, Arg: m.ChannelID})
}

// replayKey replays a sim card. Chat binds no action to it on a message row
// (r is the reaction there); a key the keymap binds (an override) is never
// taken.
const replayKey = "R"

// canReplay: c is a sim card with something to run again — its copy, or
// (for a member of its room) the document version it names.
func canReplay(ctx *ui.Context, c card) bool {
	if c.sim == nil {
		return false
	}
	if c.copyDesign() != nil {
		return true
	}
	ref := c.ref()
	return ref.versionNo > 0 && member(ctx, ref) && ctx.API != nil
}

// cardKey: R on a sim card replays it (the same design and seed, compared
// number by number). Any other key, a bound key, or a card with nothing to
// replay is left to chat.
func cardKey(ctx *ui.Context, m *api.Message, k tea.KeyPressMsg) (bool, tea.Cmd) {
	if k.Text != replayKey || k.Mod&^tea.ModShift != 0 || !features.On(ui.ToolArch) || ctx.Keys.Bound(k) {
		return false, nil
	}
	c, ok := parseCard(m)
	if !ok || !canReplay(ctx, c) {
		return false, nil
	}
	return true, replay(ctx, c)
}

// replay runs the card's design again — its copy, or the room document
// version it names — and says whether the numbers match.
func replay(ctx *ui.Context, c card) tea.Cmd {
	t := ctx.T
	if d := c.copyDesign(); d != nil {
		return func() tea.Msg { return replayToast(t, c, d) }
	}
	ref, cl := c.ref(), ctx.API
	if ref.versionNo < 1 || cl == nil {
		return ui.Toast(t.Archcanvas.CardReplayNone(), ui.ToastInfo)
	}
	return func() tea.Msg {
		failed := ui.ToastMsg{Text: t.Archcanvas.CardCopyFailed(), Kind: ui.ToastError}
		snap, err := cl.GetDocVersion(context.Background(), ref.docID, ref.versionNo)
		if err != nil {
			return failed
		}
		objs, err := snapObjects(snap)
		if err != nil {
			return failed
		}
		d, _ := archdoc.FromObjects(objs, nil)
		if d == nil {
			return failed
		}
		return replayToast(t, c, d)
	}
}

// replayToast analyzes d with the card's seed and compares the numbers.
func replayToast(t *i18n.T, c card, d *archdoc.Design) tea.Msg {
	a := t.Archcanvas
	if seed, err := strconv.ParseUint(c.sim.Seed, 10, 64); err == nil {
		d.Seed = seed
	}
	r := archsim.Analyze(d, archsim.Options{})
	s := c.sim
	cost := a.CostShort(humanize.Grouped(int64(r.Cost.TotalUSDMonth)), cleanText(asOfOf(r)))
	if r.P99Ms == s.P99Ms && r.P50Ms == s.P50Ms && r.ErrorPermille == s.ErrorPermille &&
		r.Cost.TotalUSDMonth == s.CostUSDMonth && r.RPS == s.MaxRPS && s.EngineVersion == archsim.EngineVersion {
		return ui.ToastMsg{Text: a.CardReplaySame(r.P99Ms, pct1(r.ErrorPermille), cost), Kind: ui.ToastSuccess}
	}
	return ui.ToastMsg{Text: a.CardReplayDiff(r.P99Ms, pct1(r.ErrorPermille), cost), Kind: ui.ToastAlert}
}

// ---- card copies (arch:msg instances) ----

var copies = struct {
	sync.Mutex
	order []string
	by    map[string]*archdoc.Design
}{by: map[string]*archdoc.Design{}}

// putCopy keeps the design copy of message id for its arch:msg instance.
func putCopy(id string, d *archdoc.Design) {
	copies.Lock()
	defer copies.Unlock()
	if _, ok := copies.by[id]; !ok {
		copies.order = append(copies.order, id)
	}
	copies.by[id] = d
	for len(copies.order) > cardCopies {
		delete(copies.by, copies.order[0])
		copies.order = copies.order[1:]
	}
}

func getCopy(id string) *archdoc.Design {
	copies.Lock()
	defer copies.Unlock()
	return copies.by[id]
}

// cardCopyMsg is a card copy loaded from the server (an arch:msg instance
// opened without the copy at hand: a restart).
type cardCopyMsg struct {
	instance string
	d        *archdoc.Design
}

// fetchCopy loads message id's card copy.
func fetchCopy(ctx *ui.Context, instance, id string) tea.Cmd {
	cl := ctx.API
	if cl == nil {
		return nil
	}
	return func() tea.Msg {
		m, err := cl.GetMessage(context.Background(), id)
		if err != nil {
			return cardCopyMsg{instance: instance}
		}
		c, ok := parseCard(m)
		if !ok {
			return cardCopyMsg{instance: instance}
		}
		d := c.copyDesign()
		if d != nil {
			putCopy(id, d)
		}
		return cardCopyMsg{instance: instance, d: d}
	}
}
