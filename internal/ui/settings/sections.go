package settings

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/termcaps"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/widgets"
)

// Settings sections (theme, layout, mouse/images/animations, notifications, privacy,
// focus, blocks, sessions, password, logout): the row model, the row actions and the
// API requests they send.

// Section ids (Route.Anchor values, plan 03 Task 34).
const (
	secTheme    = "theme"
	secLayout   = "layout"
	secDisplay  = "display"
	secNotify   = "notify"
	secPrivacy  = "privacy"
	secFocus    = "focus"
	secBlocks   = "blocks"
	secSessions = "sessions"
	secPassword = "password"
	secLogout   = "logout"
)

// section is one settings section.
type section struct {
	id, title string
}

// sections in display order.
var sections = []section{
	{secTheme, "테마"},
	{secLayout, "레이아웃"},
	{secDisplay, "화면"},
	{secNotify, "알림"},
	{secPrivacy, "개인정보"},
	{secFocus, "집중 모드"},
	{secBlocks, "차단 목록"},
	{secSessions, "기기·세션"},
	{secPassword, "비밀번호"},
	{secLogout, "로그아웃"},
}

// sectionIndex is the index of section id in sections (-1 = unknown).
func sectionIndex(id string) int {
	return slices.IndexFunc(sections, func(s section) bool { return s.id == id })
}

// Row ids.
const (
	rowTheme       = "theme"
	rowPin         = "layout.pin"
	rowMouse       = "ui.mouse"
	rowImages      = "ui.images"
	rowAnimations  = "ui.animations"
	rowPaintBG     = "ui.paint_background"
	rowClock       = "ui.clock"
	rowDesktop     = "notify.desktop"
	rowSettingsErr = "notify.load"
	rowDMPolicy    = "privacy.dm_policy"
	rowPrivate     = "privacy.is_private"
	rowFocusMin    = "focus.default"
	rowBlocksState = "blocks.state"
	rowBlockPrefix = "blocks."
	rowSessState   = "sessions.state"
	rowSessPrefix  = "sessions."
	rowPwCurrent   = "password.current"
	rowPwNew       = "password.new"
	rowPwConfirm   = "password.confirm"
	rowPwSubmit    = "password.submit"
	rowLogout      = "logout"
)

// Server notify switches (CA 4.3.2 NotifySettings), in display order.
var notifyRows = []struct {
	id, label string
	get       func(n *api.NotifySettings) *bool
	patch     func(p *api.NotifySettingsPatch) *api.Opt[bool]
}{
	{"notify.dm", "DM 알림", func(n *api.NotifySettings) *bool { return &n.DM }, func(p *api.NotifySettingsPatch) *api.Opt[bool] { return &p.DM }},
	{"notify.mention", "멘션 알림", func(n *api.NotifySettings) *bool { return &n.Mention }, func(p *api.NotifySettingsPatch) *api.Opt[bool] { return &p.Mention }},
	{"notify.invite", "초대 알림", func(n *api.NotifySettings) *bool { return &n.Invite }, func(p *api.NotifySettingsPatch) *api.Opt[bool] { return &p.Invite }},
	{"notify.friend_request", "친구 요청 알림", func(n *api.NotifySettings) *bool { return &n.FriendRequest }, func(p *api.NotifySettingsPatch) *api.Opt[bool] { return &p.FriendRequest }},
	{"notify.thread_reply", "스레드 답글 알림", func(n *api.NotifySettings) *bool { return &n.ThreadReply }, func(p *api.NotifySettingsPatch) *api.Opt[bool] { return &p.ThreadReply }},
	{"notify.social", "소셜 알림", func(n *api.NotifySettings) *bool { return &n.Social }, func(p *api.NotifySettingsPatch) *api.Opt[bool] { return &p.Social }},
}

// choice is one value of a cycle row.
type choice struct{ value, label string }

var (
	pinChoices = []choice{
		{"auto", "자동"}, {"command", "지휘소"}, {"standard", "표준"},
		{"compact", "컴팩트"}, {"widget", "위젯"}, {"tiny", "초소형"},
	}
	imageChoices = []choice{
		{"auto", "자동"}, {"kitty", "Kitty"}, {"iterm2", "iTerm2"}, {"halfblock", "하프블록"}, {"off", "끔"},
	}
	clockChoices    = []choice{{"24h", "24시간"}, {"12h", "12시간"}}
	dmPolicyChoices = []choice{{"friends_and_spaces", "친구·같은 방 멤버"}, {"friends_only", "친구만"}}
)

// choiceIndex is the index of v in cs (case-insensitive; unknown => 0).
func choiceIndex(cs []choice, v string) int {
	if i := slices.IndexFunc(cs, func(c choice) bool { return strings.EqualFold(c.value, v) }); i >= 0 {
		return i
	}
	return 0
}

// step moves i by dir in n values, wrapping around.
func step(i, dir, n int) int { return ((i+dir)%n + n) % n }

// Texts.
const (
	textOn            = "켜짐"
	textOff           = "꺼짐"
	textLoading       = "불러오는 중…"
	textRetry         = "enter 다시 시도"
	textNoBlocks      = "차단한 사람이 없어요"
	textNoSessions    = "다른 세션이 없어요"
	textCurrentDevice = "현재 기기"
	textSaveFailed    = "설정을 저장하지 못했어요"
	textFixConfig     = "config.toml 을 고친 뒤 다시 해 주세요"
	textSettingsLoad  = "서버 설정을 불러오지 못했어요"
	textFocusRange    = "1~720 사이의 분으로 입력해 주세요"
	textPwButton      = "[ 비밀번호 바꾸기 ]"
	textPwBusy        = "바꾸는 중…"
	textPwChanged     = "비밀번호를 바꿨어요"
	textPwWrong       = "현재 비밀번호가 맞지 않아요"
	textPwCurEmpty    = "현재 비밀번호를 입력해 주세요"
	textPwNewEmpty    = "새 비밀번호를 입력해 주세요"
	textPwShort       = "비밀번호는 8자 이상이에요"
	textPwLong        = "비밀번호는 128자 이하예요"
	textPwMismatch    = "비밀번호가 서로 달라요"
	textPwSame        = "새 비밀번호가 지금과 같아요"
	textPwEditHint    = "enter 입력"
	textLogoutHint    = "이 기기에서 로그아웃해요"
	textUnblockTitle  = "차단을 풀까요?"
	textRevokeTitle   = "이 기기를 로그아웃할까요?"
)

// Limits (api.md: focus_default_min 1..720, password 8..128 code points).
const (
	focusMinLo  = 1
	focusMinHi  = 720
	passwordMin = 8
	passwordMax = 128
)

// API tags (CA 5.3.4: "<plan>.<action>:<target>").
const (
	tagGet      = "core.settings.get"
	tagUpdate   = "core.settings.update:" // + row id
	tagBlocks   = "core.settings.blocks:" // + fetch generation
	tagUnblock  = "core.settings.unblock:"
	tagSessions = "core.settings.sessions:" // + fetch generation
	tagRevoke   = "core.settings.revoke:"
	tagPassword = "core.settings.password"
)

// rowKind is what a row does.
type rowKind int

const (
	kindToggle   rowKind = iota // enter/space flips
	kindCycle                   // ←/→ (enter/space = →) cycles values
	kindNumber                  // enter edits a number field
	kindPassword                // enter edits a password field
	kindButton                  // enter presses
	kindAction                  // enter acts (blocks, sessions, logout, retry)
	kindInfo                    // nothing to do (loading / empty)
)

// row is one selectable line (plus extra lines) of a section.
type row struct {
	id    string
	sec   string
	kind  rowKind
	label string
	value string // plain value text (cycle / number / action)
	on    bool   // toggle state
	sub   string // dim text after the value
	good  string // success-styled text after sub ("현재 기기")
	err   string // inline error under the row
	info  string // kindInfo / state rows: the text
	bad   bool   // info is an error (Error style, enter retries)
	field *field // number / password: the edited field
}

// passwordRows are the password field rows, in order.
var passwordRows = []string{rowPwCurrent, rowPwNew, rowPwConfirm}

// rows builds every row of every section, in display order.
func (s *screen) rows(ctx *ui.Context) []row {
	cfg := ctx.Config
	if cfg == nil {
		cfg = config.Defaults()
	}
	t := themeOf(ctx)
	var out []row
	add := func(r row) {
		r.err = s.rowErr[r.id]
		out = append(out, r)
	}
	// theme
	add(row{id: rowTheme, sec: secTheme, kind: kindCycle, label: "테마", value: t.Letter + " · " + t.Label})
	// layout
	add(row{id: rowPin, sec: secLayout, kind: kindCycle, label: "레이아웃 고정",
		value: pinChoices[choiceIndex(pinChoices, cfg.Layout.Pin)].label})
	// display
	add(row{id: rowMouse, sec: secDisplay, kind: kindToggle, label: "마우스", on: cfg.UI.Mouse})
	add(row{id: rowImages, sec: secDisplay, kind: kindCycle, label: "이미지",
		value: imageChoices[choiceIndex(imageChoices, cfg.UI.Images)].label})
	add(row{id: rowAnimations, sec: secDisplay, kind: kindToggle, label: "애니메이션", on: cfg.UI.Animations})
	add(row{id: rowPaintBG, sec: secDisplay, kind: kindToggle, label: "배경색 칠하기", on: cfg.UI.PaintBackground})
	add(row{id: rowClock, sec: secDisplay, kind: kindCycle, label: "시계",
		value: clockChoices[choiceIndex(clockChoices, cfg.UI.Clock)].label})
	// notify: local master switch, then the server switches
	add(row{id: rowDesktop, sec: secNotify, kind: kindToggle, label: "데스크톱 알림", on: cfg.Notify.Desktop, sub: "이 기기"})
	if s.settingsErr != "" {
		add(row{id: rowSettingsErr, sec: secNotify, kind: kindAction, info: textSettingsLoad + ": " + s.settingsErr, bad: true})
	}
	for _, n := range notifyRows {
		add(row{id: n.id, sec: secNotify, kind: kindToggle, label: n.label, on: *n.get(&s.settings.Notify)})
	}
	// privacy
	add(row{id: rowDMPolicy, sec: secPrivacy, kind: kindCycle, label: "DM 받기",
		value: dmPolicyChoices[choiceIndex(dmPolicyChoices, s.settings.DMPolicy)].label})
	add(row{id: rowPrivate, sec: secPrivacy, kind: kindToggle, label: "비공개 계정", on: s.settings.IsPrivate})
	// focus
	add(row{id: rowFocusMin, sec: secFocus, kind: kindNumber, label: "집중 기본 시간",
		value: strconv.Itoa(s.settings.FocusDefaultMin) + "분", field: s.num})
	// blocks
	switch {
	case s.blocks.err != "":
		add(row{id: rowBlocksState, sec: secBlocks, kind: kindAction, info: s.blocks.err, bad: true})
	case !s.blocks.loaded:
		add(row{id: rowBlocksState, sec: secBlocks, kind: kindInfo, info: textLoading})
	case len(s.blocks.items) == 0:
		add(row{id: rowBlocksState, sec: secBlocks, kind: kindInfo, info: textNoBlocks})
	}
	for _, b := range s.blocks.items {
		u := b.User
		name := textwidth.Sanitize(u.DisplayName)
		if textwidth.Blank(name) { // nothing visible (pending-fixes 1)
			name = textwidth.Sanitize(u.Handle)
		}
		add(row{id: rowBlockPrefix + u.ID, sec: secBlocks, kind: kindAction, label: name,
			value: "@" + textwidth.Sanitize(u.Handle), sub: humanize.Relative(ctx.Now().In(ctx.Loc), b.CreatedAt) + " 차단"})
	}
	// sessions
	switch {
	case s.sessions.err != "":
		add(row{id: rowSessState, sec: secSessions, kind: kindAction, info: s.sessions.err, bad: true})
	case !s.sessions.loaded:
		add(row{id: rowSessState, sec: secSessions, kind: kindInfo, info: textLoading})
	case len(s.sessions.items) == 0:
		add(row{id: rowSessState, sec: secSessions, kind: kindInfo, info: textNoSessions})
	}
	for _, se := range s.sessions.items {
		name := textwidth.Sanitize(se.DeviceName)
		if strings.TrimSpace(name) == "" {
			name = clientLabel(se.Client)
		}
		r := row{id: rowSessPrefix + se.ID, sec: secSessions, kind: kindAction, label: name,
			value: clientLabel(se.Client), sub: "마지막 사용 " + humanize.Relative(ctx.Now().In(ctx.Loc), se.LastUsedAt)}
		if se.Current {
			r.good = textCurrentDevice
		}
		add(r)
	}
	// password
	for i, id := range passwordRows {
		add(row{id: id, sec: secPassword, kind: kindPassword, label: passwordLabels[i], field: s.pw[i]})
	}
	submit := row{id: rowPwSubmit, sec: secPassword, kind: kindButton, label: textPwButton}
	if s.pwBusy {
		submit.sub = textPwBusy
	}
	add(submit)
	// logout
	add(row{id: rowLogout, sec: secLogout, kind: kindAction, label: "로그아웃", sub: textLogoutHint})
	return out
}

// passwordLabels are the labels of passwordRows.
var passwordLabels = []string{"현재 비밀번호", "새 비밀번호", "새 비밀번호 확인"}

// clientLabel is the Korean name of a session client.
func clientLabel(c string) string {
	switch c {
	case "tui":
		return "터미널"
	case "daemon":
		return "알림 데몬"
	case "admin":
		return "관리 도구"
	}
	return "기타"
}

// themeOf is ctx.Theme (the default theme without one).
func themeOf(ctx *ui.Context) *theme.Theme {
	if ctx != nil && ctx.Theme != nil {
		return ctx.Theme
	}
	return theme.Get(theme.Default)
}

// ---- actions ----

// activate is enter/space on row r (dir = +1) or ←/→ on a cycle row (dir = ∓1).
func (s *screen) activate(ctx *ui.Context, r row, dir int) tea.Cmd {
	switch r.kind {
	case kindToggle:
		return s.toggle(ctx, r)
	case kindCycle:
		return s.cycle(ctx, r, dir)
	case kindNumber:
		s.startEdit(r.id)
		return nil
	case kindPassword:
		s.startEdit(r.id)
		return nil
	case kindButton:
		return s.submitPassword(ctx)
	case kindAction:
		return s.act(ctx, r)
	}
	return nil
}

// toggle flips a switch row.
func (s *screen) toggle(ctx *ui.Context, r row) tea.Cmd {
	v := !r.on
	switch r.id {
	case rowMouse:
		return s.saveLocal(ctx, r.id, func(c *config.Config) { c.UI.Mouse = v })
	case rowAnimations:
		return s.saveLocal(ctx, r.id, func(c *config.Config) { c.UI.Animations = v })
	case rowPaintBG:
		return s.saveLocal(ctx, r.id, func(c *config.Config) { c.UI.PaintBackground = v })
	case rowDesktop:
		return s.saveLocal(ctx, r.id, func(c *config.Config) { c.Notify.Desktop = v })
	case rowPrivate:
		return s.patch(ctx, r.id, api.UpdateSettingsRequest{IsPrivate: api.Some(v)}, func(st *api.Settings) { st.IsPrivate = v })
	}
	for _, n := range notifyRows {
		if n.id == r.id {
			var p api.NotifySettingsPatch
			*n.patch(&p) = api.Some(v)
			get := n.get
			return s.patch(ctx, r.id, api.UpdateSettingsRequest{Notify: &p}, func(st *api.Settings) { *get(&st.Notify) = v })
		}
	}
	return nil
}

// cycle moves a cycle row's value by dir.
func (s *screen) cycle(ctx *ui.Context, r row, dir int) tea.Cmd {
	cfg := ctx.Config
	if cfg == nil {
		return nil
	}
	switch r.id {
	case rowTheme:
		all := theme.All()
		i := slices.IndexFunc(all, func(t *theme.Theme) bool { return t.Name == themeOf(ctx).Name })
		name := all[step(max(i, 0), dir, len(all))].Name
		delete(s.rowErr, r.id)
		return func() tea.Msg { return ui.SetThemeMsg{Name: name} } // the app applies and saves it
	case rowPin:
		v := pinChoices[step(choiceIndex(pinChoices, cfg.Layout.Pin), dir, len(pinChoices))].value
		cmd := s.saveLocal(ctx, r.id, func(c *config.Config) { c.Layout.Pin = v })
		id := "layout.auto"
		if v != "auto" {
			id = "layout.pin." + v
		}
		return tea.Batch(cmd, ui.RunCommand(id, ""))
	case rowImages:
		v := imageChoices[step(choiceIndex(imageChoices, cfg.UI.Images), dir, len(imageChoices))].value
		cmd := s.saveLocal(ctx, r.id, func(c *config.Config) { c.UI.Images = v })
		// Re-detect for the new override but keep what was learned at run time (Task 25).
		caps := termcaps.Detect(os.Environ(), v)
		caps.UnicodeCore, caps.FocusReports = ctx.Caps.UnicodeCore, ctx.Caps.FocusReports
		ctx.Caps = caps
		return cmd
	case rowClock:
		v := clockChoices[step(choiceIndex(clockChoices, cfg.UI.Clock), dir, len(clockChoices))].value
		return s.saveLocal(ctx, r.id, func(c *config.Config) { c.UI.Clock = v })
	case rowDMPolicy:
		v := dmPolicyChoices[step(choiceIndex(dmPolicyChoices, s.settings.DMPolicy), dir, len(dmPolicyChoices))].value
		return s.patch(ctx, r.id, api.UpdateSettingsRequest{DMPolicy: api.Some(v)}, func(st *api.Settings) { st.DMPolicy = v })
	}
	return nil
}

// saveLocal applies a config.toml change at once (ctx.Config is shared with the app)
// and saves it; a failed save keeps the change for this run and shows an inline error
// (a config.toml that did not load is never overwritten: "fix the file" instead).
func (s *screen) saveLocal(ctx *ui.Context, id string, apply func(*config.Config)) tea.Cmd {
	if ctx.Config == nil {
		return nil
	}
	apply(ctx.Config)
	delete(s.rowErr, id)
	if err := config.Save(ctx.Paths, ctx.Config); errors.Is(err, config.ErrNotLoaded) {
		s.rowErr[id] = textFixConfig // config.toml did not load: never overwrite it
	} else if err != nil {
		slog.Warn("save config", "err", err)
		s.rowErr[id] = textSaveFailed
	}
	return nil
}

// patch sends one server setting (only that Opt filled) after applying it
// optimistically; the response replaces Store.Settings, a failure reverts the rows to
// Store.Settings and shows api.Describe under the row.
func (s *screen) patch(ctx *ui.Context, id string, req api.UpdateSettingsRequest, optimistic func(*api.Settings)) tea.Cmd {
	optimistic(&s.settings)
	delete(s.rowErr, id)
	return ui.Do(ctx, tagUpdate+id, func(c context.Context, a *api.Client) (*api.Settings, error) {
		return a.UpdateSettings(c, req)
	})
}

// act is enter on an action row: unblock, revoke, logout or a retry.
func (s *screen) act(ctx *ui.Context, r row) tea.Cmd {
	switch {
	case r.id == rowLogout:
		return ui.RunCommand("logout", "") // the same Confirm as the palette command
	case r.id == rowSettingsErr:
		s.settingsErr = ""
		return s.fetchSettings(ctx)
	case r.id == rowBlocksState:
		return s.fetchBlocks(ctx)
	case r.id == rowSessState:
		return s.fetchSessions(ctx)
	case strings.HasPrefix(r.id, rowBlockPrefix):
		return s.confirmUnblock(ctx, strings.TrimPrefix(r.id, rowBlockPrefix))
	case strings.HasPrefix(r.id, rowSessPrefix):
		return s.confirmRevoke(ctx, strings.TrimPrefix(r.id, rowSessPrefix))
	}
	return nil
}

// confirmUnblock asks before DELETE /v1/blocks/@handle.
func (s *screen) confirmUnblock(ctx *ui.Context, userID string) tea.Cmd {
	i := slices.IndexFunc(s.blocks.items, func(b api.BlockEntry) bool { return b.User.ID == userID })
	if i < 0 {
		return nil
	}
	u := s.blocks.items[i].User
	ref := api.UserRef(u.ID)
	if u.Handle != "" {
		ref = api.Handle(u.Handle)
	}
	body := "@" + textwidth.Sanitize(u.Handle) + " 님이 다시 DM 을 보내고 글을 볼 수 있어요."
	run := ui.Do(ctx, tagUnblock+userID, func(c context.Context, a *api.Client) (struct{}, error) {
		return struct{}{}, a.Unblock(c, ref)
	})
	return ui.OpenOverlay(widgets.Confirm(textUnblockTitle, body, "차단 풀기", "취소", run))
}

// confirmRevoke asks before DELETE /v1/auth/sessions/{id}; the current session is
// not revocable here (logout is).
func (s *screen) confirmRevoke(ctx *ui.Context, id string) tea.Cmd {
	i := slices.IndexFunc(s.sessions.items, func(se api.Session) bool { return se.ID == id })
	if i < 0 || s.sessions.items[i].Current {
		return nil
	}
	se := s.sessions.items[i]
	name := textwidth.Sanitize(se.DeviceName)
	if strings.TrimSpace(name) == "" {
		name = clientLabel(se.Client)
	}
	run := ui.Do(ctx, tagRevoke+id, func(c context.Context, a *api.Client) (struct{}, error) {
		return struct{}{}, a.RevokeSession(c, id)
	})
	return ui.OpenOverlay(widgets.Confirm(textRevokeTitle, name+" 의 masil 이 로그아웃돼요.", "로그아웃", "취소", run))
}

// ---- fetches ----

// listState is a fetched list (blocks or sessions).
type listState[T any] struct {
	items  []T
	loaded bool
	err    string
	gen    int // generation of the latest fetch (stale results are dropped)
}

func (s *screen) fetchSettings(ctx *ui.Context) tea.Cmd {
	return ui.Do(ctx, tagGet, func(c context.Context, a *api.Client) (*api.Settings, error) {
		return a.GetSettings(c)
	})
}

func (s *screen) fetchBlocks(ctx *ui.Context) tea.Cmd {
	s.blocks.gen++
	return ui.Do(ctx, tagBlocks+strconv.Itoa(s.blocks.gen), func(c context.Context, a *api.Client) ([]api.BlockEntry, error) {
		return a.ListBlocks(c)
	})
}

func (s *screen) fetchSessions(ctx *ui.Context) tea.Cmd {
	s.sessions.gen++
	return ui.Do(ctx, tagSessions+strconv.Itoa(s.sessions.gen), func(c context.Context, a *api.Client) ([]api.Session, error) {
		return a.ListSessions(c)
	})
}

// describe is api.Describe with a fallback for a nil-coded error.
func describe(err error) string {
	if d := api.Describe(err); d != "" {
		return d
	}
	return api.Describe(&api.Error{Code: "internal_error"})
}

// onSettings handles GET/PATCH /v1/me/settings results.
func (s *screen) onSettings(ctx *ui.Context, m ui.ResultMsg[*api.Settings]) {
	id, isUpdate := strings.CutPrefix(m.Tag, tagUpdate)
	if m.Err != nil || m.Value == nil {
		if isUpdate {
			s.settings = ctx.Store.Settings // revert to the last known server state
			s.rowErr[id] = describe(m.Err)
		} else {
			s.settingsErr = describe(m.Err)
		}
		return
	}
	s.settingsErr = ""
	s.settings = *m.Value
	ctx.Store.Settings = *m.Value
	if ctx.Store.Me != nil {
		ctx.Store.Me.Settings = *m.Value
	}
	if isUpdate && id == rowFocusMin {
		delete(s.rowErr, id)
	}
}

// onBlocks handles a ListBlocks result: the list and Store.Blocks become the same set.
func (s *screen) onBlocks(ctx *ui.Context, m ui.ResultMsg[[]api.BlockEntry]) {
	if strings.TrimPrefix(m.Tag, tagBlocks) != strconv.Itoa(s.blocks.gen) {
		return
	}
	if m.Err != nil {
		s.blocks.err = describe(m.Err) + " · " + textRetry
		return
	}
	s.blocks.items, s.blocks.loaded, s.blocks.err = m.Value, true, ""
	if ctx.Store.Blocks == nil {
		return
	}
	keep := map[string]bool{}
	for _, b := range m.Value {
		keep[b.User.ID] = true
		ctx.Store.ObserveUser(b.User)
		if _, ok := ctx.Store.Blocks[b.User.ID]; !ok {
			ctx.Store.Blocks[b.User.ID] = b.CreatedAt
		}
	}
	for id := range ctx.Store.Blocks {
		if !keep[id] {
			delete(ctx.Store.Blocks, id)
		}
	}
}

// onUnblock handles an Unblock result.
func (s *screen) onUnblock(ctx *ui.Context, m ui.ResultMsg[struct{}]) {
	userID := strings.TrimPrefix(m.Tag, tagUnblock)
	if m.Err != nil {
		s.rowErr[rowBlockPrefix+userID] = describe(m.Err)
		return
	}
	s.blocks.items = slices.DeleteFunc(slices.Clone(s.blocks.items), func(b api.BlockEntry) bool { return b.User.ID == userID })
	delete(s.rowErr, rowBlockPrefix+userID)
	delete(ctx.Store.Blocks, userID)
}

// onSessions handles a ListSessions result (the current session first).
func (s *screen) onSessions(m ui.ResultMsg[[]api.Session]) {
	if strings.TrimPrefix(m.Tag, tagSessions) != strconv.Itoa(s.sessions.gen) {
		return
	}
	if m.Err != nil {
		s.sessions.err = describe(m.Err) + " · " + textRetry
		return
	}
	items := slices.Clone(m.Value)
	slices.SortStableFunc(items, func(a, b api.Session) int {
		switch {
		case a.Current == b.Current:
			return 0
		case a.Current:
			return -1
		}
		return 1
	})
	s.sessions.items, s.sessions.loaded, s.sessions.err = items, true, ""
}

// onRevoke handles a RevokeSession result.
func (s *screen) onRevoke(m ui.ResultMsg[struct{}]) {
	id := strings.TrimPrefix(m.Tag, tagRevoke)
	if m.Err != nil {
		s.rowErr[rowSessPrefix+id] = describe(m.Err)
		return
	}
	s.sessions.items = slices.DeleteFunc(slices.Clone(s.sessions.items), func(se api.Session) bool { return se.ID == id })
	delete(s.rowErr, rowSessPrefix+id)
}

// ---- editing ----

// startEdit starts editing a number / password row.
func (s *screen) startEdit(id string) {
	s.editing = id
	if id == rowFocusMin {
		s.num.set(strconv.Itoa(s.settings.FocusDefaultMin))
	}
}

// stopEdit leaves the edited field (password values are kept).
func (s *screen) stopEdit() {
	s.editing = ""
}

// editedField is the field being edited, or nil.
func (s *screen) editedField() *field {
	switch s.editing {
	case rowFocusMin:
		return s.num
	case "":
		return nil
	}
	if i := slices.Index(passwordRows, s.editing); i >= 0 {
		return s.pw[i]
	}
	return nil
}

// commitNumber validates the focus default minutes and sends it.
func (s *screen) commitNumber(ctx *ui.Context) tea.Cmd {
	v, err := strconv.Atoi(s.num.value())
	if err != nil || v < focusMinLo || v > focusMinHi {
		s.rowErr[rowFocusMin] = textFocusRange
		return nil
	}
	s.stopEdit()
	if v == s.settings.FocusDefaultMin {
		delete(s.rowErr, rowFocusMin)
		return nil
	}
	return s.patch(ctx, rowFocusMin, api.UpdateSettingsRequest{FocusDefaultMin: api.Some(v)}, func(st *api.Settings) { st.FocusDefaultMin = v })
}

// submitPassword validates the password form and sends POST /v1/auth/password.
func (s *screen) submitPassword(ctx *ui.Context) tea.Cmd {
	if s.pwBusy {
		return nil
	}
	for _, id := range passwordRows {
		delete(s.rowErr, id)
	}
	cur, next, confirm := s.pw[0].value(), s.pw[1].value(), s.pw[2].value()
	fail := func(id, text string) tea.Cmd {
		s.rowErr[id] = text
		s.selectID(id)
		s.startEdit(id)
		return nil
	}
	n := utf8.RuneCountInString(next)
	switch {
	case cur == "":
		return fail(rowPwCurrent, textPwCurEmpty)
	case n == 0:
		return fail(rowPwNew, textPwNewEmpty)
	case n < passwordMin:
		return fail(rowPwNew, textPwShort)
	case n > passwordMax:
		return fail(rowPwNew, textPwLong)
	case next == cur:
		return fail(rowPwNew, textPwSame)
	case confirm != next:
		return fail(rowPwConfirm, textPwMismatch)
	}
	s.stopEdit()
	s.pwBusy = true
	return ui.Do(ctx, tagPassword, func(c context.Context, a *api.Client) (struct{}, error) {
		return struct{}{}, a.ChangePassword(c, cur, next)
	})
}

// onPassword handles the ChangePassword result. invalid_credentials (a wrong current
// password) never logs out (api OnUnauthorized fires only for "unauthorized").
func (s *screen) onPassword(ctx *ui.Context, m ui.ResultMsg[struct{}]) tea.Cmd {
	if !s.pwBusy {
		return nil
	}
	s.pwBusy = false
	if m.Err != nil {
		text := describe(m.Err)
		id := rowPwSubmit
		if api.CodeOf(m.Err) == "invalid_credentials" {
			text, id = textPwWrong, rowPwCurrent
			s.pw[0].clear()
		}
		s.rowErr[id] = text
		s.selectID(id)
		if id != rowPwSubmit {
			s.startEdit(id)
		}
		return nil
	}
	for _, f := range s.pw {
		f.clear()
	}
	// Every other session was revoked (api.md 5.1): refresh the list.
	return tea.Batch(ui.Toast(textPwChanged, ui.ToastSuccess), s.fetchSessions(ctx))
}
