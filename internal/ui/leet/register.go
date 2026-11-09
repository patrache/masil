// Package leet is the LeetCode daily problem tool (v0.1.3 spec A10): the hub
// row, the first-run notice, the settings, the problem view with its editor
// slot, the cookie login, remote run and submit, the solve card dialog, the
// #daily chat cards and the ranking overlay.
//
// The masil server never sees a LeetCode cookie or a problem body: this
// package talks to LeetCode through internal/leetcode with the user's own
// cookies (ctx.Secrets only) and reports nothing but the day's facts and the
// user's accepted solves to masil.
package leet

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/features"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/leetcode"
	"github.com/patrache/masil/internal/termhost"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/theme"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/palette"
)

// Ids.
const (
	toolID          = ui.ToolLeet
	instancePrefix  = "leet:"      // instance ids: "leet:today" or "leet:<date>/<slug>"
	todayInstance   = "leet:today" // today's problem
	queryTarget     = "target"     // OpenTool query: "<date>/<slug>" (a card's problem)
	OverlayFirstRun = "leet.firstrun"
	OverlaySettings = "leet.settings"
	OverlayBoard    = "leet.board" // the ranking overlay (args = space id)
	cmdOpen         = "leet.open"
	cmdBoard        = "leet.board"
	cmdSettings     = "leet.settings"
	cmdDailyToggle  = "leet.daily.toggle"
	hubIcon         = "🧩"
	hubOrder        = 1
	stateKey        = toolID           // tools-state.json section
	cacheRecheck    = 30 * time.Second // how long the hub trusts "nothing cached for today"
)

// leetState is the tool's tools-state.json section (A8.5): device-only memory.
type leetState struct {
	Lang         string `json:"lang"`           // the language last picked with l (or on the first run)
	FirstRunDone bool   `json:"first_run_done"` // the first-run notice was confirmed
	// Accounts is the LeetCode sign-in of each masil account ("<server>/<user
	// id>"), like the cookies themselves (CredNS): one device, many accounts.
	Accounts map[string]lcAccount `json:"accounts,omitempty"`
}

// lcAccount is who a masil account's cookies sign in as (not a secret).
type lcAccount struct {
	Username string `json:"username"`
	Premium  bool   `json:"premium"` // runs paced 1s instead of 10s
}

func init() {
	ui.RegisterTool(ui.ToolSpec{
		ID:        toolID,
		Order:     hubOrder,
		Icon:      hubIcon,
		Name:      func(t *i18n.T) string { return t.Leetcode.Name() },
		Status:    hubStatus,
		Available: func(*ui.Context) bool { return true },
		New:       newInstance,
		Open:      open,
		PrefixKeys: []ui.PrefixKey{
			{Key: "r", Help: func(t *i18n.T) string { return t.Leetcode.PrefixRun() }},
			{Key: "s", Help: func(t *i18n.T) string { return t.Leetcode.PrefixSubmit() }},
		},
		WideDefault: false,
	})
	palette.Register(palette.Command{
		ID:        cmdOpen,
		Label:     func(t *i18n.T) string { return t.Leetcode.CmdOpen() },
		Group:     palette.GroupCommand,
		Keywords:  []string{"leetcode", "leet", "daily"},
		Available: func(*ui.Context) bool { return features.On(ui.ToolLeet) },
		Run:       func(ctx *ui.Context, _ string) tea.Cmd { return open(ctx, termhost.ModeAuto) },
	})
	palette.Register(palette.Command{
		ID:        cmdBoard,
		Label:     func(t *i18n.T) string { return t.Leetcode.CmdBoard() },
		Group:     palette.GroupCommand,
		Keywords:  []string{"leetcode", "leet", "board", "rank"},
		Available: func(ctx *ui.Context) bool { return features.On(ui.ToolLeet) && currentSpaceID(ctx) != "" },
		Run: func(ctx *ui.Context, args string) tea.Cmd {
			space := strings.TrimSpace(args)
			if space == "" {
				space = currentSpaceID(ctx)
			}
			return ui.OpenRegisteredOverlay(OverlayBoard, space)
		},
	})
	palette.Register(palette.Command{
		ID:        cmdSettings,
		Label:     func(t *i18n.T) string { return t.Leetcode.CmdSettings() },
		Group:     palette.GroupCommand,
		Keywords:  []string{"leetcode", "leet", "settings"},
		Available: func(*ui.Context) bool { return features.On(ui.ToolLeet) },
		Run:       func(*ui.Context, string) tea.Cmd { return ui.OpenRegisteredOverlay(OverlaySettings, "") },
	})
	palette.Register(palette.Command{
		ID:       cmdDailyToggle,
		Label:    func(t *i18n.T) string { return t.Leetcode.CmdDailyToggle() },
		Group:    palette.GroupCommand,
		Keywords: []string{"leetcode", "leet", "daily"},
		Available: func(ctx *ui.Context) bool {
			return features.On(ui.ToolLeet) && canManage(ctx, currentSpaceID(ctx))
		},
		Run: func(ctx *ui.Context, args string) tea.Cmd {
			space := strings.TrimSpace(args)
			if space == "" {
				space = currentSpaceID(ctx)
			}
			if !canManage(ctx, space) {
				return nil
			}
			return setDaily(ctx, space, !dailyOn(ctx, space))
		},
	})
	ui.RegisterOverlay(OverlayFirstRun, newFirstRun)
	ui.RegisterOverlay(OverlaySettings, newSettings)
}

// ---- the LeetCode client (tests point it at leetcodetest: export_test.go) ----

var (
	clientMu    sync.Mutex
	leetcodeURL string // "" = leetcode.Origin
)

// newClient is a LeetCode client signed in with cr (nil = signed out).
func newClient(cr *leetcode.Credentials) *leetcode.Client {
	clientMu.Lock()
	base := leetcodeURL
	clientMu.Unlock()
	return leetcode.New(leetcode.Options{BaseURL: base, Creds: cr})
}

// fetcher reads problems through the local cache (A10.1 JA27).
func fetcher(ctx *ui.Context) *leetcode.Fetcher {
	return &leetcode.Fetcher{C: newClient(nil), Cache: leetcode.OpenCache(ctx.Paths.Cache), Now: nowFunc(ctx)}
}

func nowFunc(ctx *ui.Context) func() time.Time {
	if ctx != nil && ctx.Now != nil {
		return ctx.Now
	}
	return time.Now
}

// ---- per-account memory (the hub never asks LeetCode or masil) ----

// account is whose memory it is: the masil server host and user.
type account struct{ server, user string }

// dayFacts are the facts of one day's problem this device knows.
type dayFacts struct {
	ID, Slug, Title, Difficulty string
}

// memo is what the tool remembers in this run for one masil account.
type memo struct {
	me       *api.LeetMe          // the last GET /v1/leet/me answer
	days     map[string]dayFacts  // date -> today's problem of that date (fetched or cached)
	today    string               // the date of the last "today" this run loaded
	reported map[string]bool      // dates whose facts were reported to masil
	mismatch map[string]string    // date -> masil's slug when it differs from mine
	checked  map[string]time.Time // date -> when the hub last found nothing cached

	creds *leetcode.Credentials // this run's LeetCode cookies (nil = not loaded)
	pacer *leetcode.Pacer       // runs and submits of this account, paced
	lc    *lcAccount            // who this run signed in as (nil = see tools-state)
}

var (
	memoMu sync.Mutex
	memos  = map[account]*memo{}
)

func accountOf(ctx *ui.Context) account {
	server, user, _ := ctx.SecretScope()
	return account{server, user}
}

// withMemo runs fn on ctx's account memory under the lock.
func withMemo[T any](ctx *ui.Context, fn func(m *memo) T) T {
	a := accountOf(ctx)
	memoMu.Lock()
	defer memoMu.Unlock()
	m := memos[a]
	if m == nil {
		m = newMemo()
		memos[a] = m
	}
	return fn(m)
}

func newMemo() *memo {
	return &memo{days: map[string]dayFacts{}, reported: map[string]bool{}, mismatch: map[string]string{}, checked: map[string]time.Time{}}
}

// rememberMe keeps a LeetMe answer.
func rememberMe(ctx *ui.Context, me *api.LeetMe) {
	if me == nil {
		return
	}
	withMemo(ctx, func(m *memo) struct{} { m.me = me; return struct{}{} })
}

// rememberCreds keeps the cookies for this run (a login or a store read).
func rememberCreds(ctx *ui.Context, c *leetcode.Credentials, premium bool) {
	withMemo(ctx, func(m *memo) struct{} {
		m.creds = c
		if m.pacer == nil || m.pacer.Interval != leetcode.NewPacer(premium).Interval {
			m.pacer = leetcode.NewPacer(premium)
		}
		return struct{}{}
	})
}

// forgetCreds drops the cookies of this run (expired or deleted).
func forgetCreds(ctx *ui.Context) {
	withMemo(ctx, func(m *memo) struct{} { m.creds = nil; return struct{}{} })
}

// knownCreds is this run's cookies (nil = load them first).
func knownCreds(ctx *ui.Context) *leetcode.Credentials {
	return withMemo(ctx, func(m *memo) *leetcode.Credentials { return m.creds })
}

// key is the account's tools-state key.
func (a account) key() string { return a.server + "/" + a.user }

// lcAccountOf is who ctx's masil account signed in to LeetCode as: this
// run's login, else the one tools-state remembers for the account.
func lcAccountOf(ctx *ui.Context) lcAccount {
	if a := withMemo(ctx, func(m *memo) *lcAccount { return m.lc }); a != nil {
		return *a
	}
	return loadState(ctx).Accounts[accountOf(ctx).key()]
}

// setLCAccount remembers (nil: forgets) ctx's account's LeetCode user, for
// this run and in tools-state.
func setLCAccount(ctx *ui.Context, a *lcAccount) {
	withMemo(ctx, func(m *memo) struct{} {
		m.lc = nil
		if a != nil {
			cp := *a
			m.lc = &cp
		}
		return struct{}{}
	})
	st, key := loadState(ctx), accountOf(ctx).key()
	if a == nil {
		delete(st.Accounts, key)
	} else {
		if st.Accounts == nil {
			st.Accounts = map[string]lcAccount{}
		}
		st.Accounts[key] = *a
	}
	saveState(ctx, st)
}

// pacerOf is the account's pacer.
func pacerOf(ctx *ui.Context) *leetcode.Pacer {
	premium := lcAccountOf(ctx).Premium
	return withMemo(ctx, func(m *memo) *leetcode.Pacer {
		if m.pacer == nil {
			m.pacer = leetcode.NewPacer(premium)
		}
		return m.pacer
	})
}

// knownMe is the last LeetMe answer (nil = not asked yet).
func knownMe(ctx *ui.Context) *api.LeetMe {
	return withMemo(ctx, func(m *memo) *api.LeetMe { return m.me })
}

// rememberDay keeps date's problem (today = it was loaded as today's).
func rememberDay(ctx *ui.Context, date string, q *leetcode.Question, today bool) {
	if q == nil || date == "" {
		return
	}
	withMemo(ctx, func(m *memo) struct{} {
		m.days[date] = dayFacts{ID: q.FrontendID, Slug: q.Slug, Title: q.Title, Difficulty: q.Difficulty}
		if today {
			m.today = date
		}
		return struct{}{}
	})
}

// solvedOn reports whether my solves (the last LeetMe) include date.
func solvedOn(me *api.LeetMe, date string) bool {
	if me == nil {
		return false
	}
	for _, s := range me.Recent {
		if s.Date == date {
			return true
		}
	}
	return false
}

// hubToday is the day the hub talks about and its facts (ok=false: none
// known). It reads this run's memory, else the local cache for the clock's
// UTC day (at most every cacheRecheck while nothing is there).
func hubToday(ctx *ui.Context) (string, dayFacts, bool) {
	now := nowFunc(ctx)()
	clock := leetcode.UTCDate(now)
	type look struct {
		date string
		f    dayFacts
		ok   bool
		read bool
	}
	r := withMemo(ctx, func(m *memo) (r look) {
		d := m.today
		if _, has := m.days[clock]; d == "" || d < clock && has {
			d = clock
		}
		if f, ok := m.days[d]; ok {
			return look{date: d, f: f, ok: true}
		}
		at, seen := m.checked[clock]
		r.date, r.read = clock, !seen || now.Sub(at) >= cacheRecheck || now.Before(at)
		if r.read {
			m.checked[clock] = now
		}
		return r
	})
	if r.ok || !r.read || ctx.Paths.Cache == "" {
		return r.date, r.f, r.ok
	}
	q, hit := leetcode.OpenCache(ctx.Paths.Cache).Daily(clock)
	if !hit {
		return clock, dayFacts{}, false
	}
	rememberDay(ctx, clock, q, false)
	return clock, dayFacts{ID: q.FrontendID, Slug: q.Slug, Title: q.Title, Difficulty: q.Difficulty}, true
}

// hubStatus is the hub row (A8.2): "1606 · Hard · 아직 안 풂 · 🔥 3일" from
// the cache and the remembered LeetMe — never a request.
func hubStatus(ctx *ui.Context) string {
	t := ctx.T
	date, f, ok := hubToday(ctx)
	if !ok {
		return t.Leetcode.HubNotLoaded()
	}
	me := knownMe(ctx)
	if me == nil {
		return t.Leetcode.HubStatusShort(f.ID, f.Difficulty)
	}
	solved := t.Leetcode.Unsolved()
	if solvedOn(me, date) {
		solved = t.Leetcode.Solved()
	}
	return t.Leetcode.HubStatus(f.ID, f.Difficulty, solved, me.Current)
}

// open is ToolSpec.Open and leet.open: today's problem (the first run's
// notice comes up from the instance).
func open(*ui.Context, termhost.Mode) tea.Cmd {
	return ui.OpenTool(toolID, todayInstance, nil)
}

// ---- settings and device memory ----

// toolConfig is [tools.leet] (defaults when there is no config).
func toolConfig(ctx *ui.Context) config.LeetToolConfig {
	c := config.DefaultLeetToolConfig()
	if ctx != nil && ctx.Config != nil {
		_ = ctx.Config.ToolSection(toolID, &c) // a wrong-typed key keeps its default
	}
	return c
}

// loadState reads the tool's section of tools-state.json.
func loadState(ctx *ui.Context) leetState {
	var st leetState
	if ctx != nil {
		ctx.ToolState.Section(stateKey, &st)
	}
	return st
}

// saveState writes it (device memory only: a failed write is not worth a toast).
func saveState(ctx *ui.Context, st leetState) {
	if ctx != nil {
		_ = ctx.ToolState.SetSection(stateKey, st)
	}
}

// currentLang is the language to solve in: [tools.leet] lang, else the one
// last picked (l, first run), else Go.
func currentLang(ctx *ui.Context) leetcode.Lang {
	for _, slug := range []string{toolConfig(ctx).Lang, loadState(ctx).Lang, leetcode.DefaultLang} {
		if l, ok := leetcode.LangBySlug(slug); ok {
			return l
		}
	}
	return leetcode.Languages[0]
}

// homeDir is the user's home ("" when unknown).
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}

// solutionDir is the solutions folder: [tools.leet] dir (a leading ~ is the
// home), else ~/masil/leetcode.
func solutionDir(ctx *ui.Context) string {
	d := strings.TrimSpace(toolConfig(ctx).Dir)
	home := homeDir()
	switch {
	case d == "":
		return leetcode.DefaultDir(home)
	case d == "~":
		return home
	case strings.HasPrefix(d, "~/") || strings.HasPrefix(d, `~\`):
		return filepath.Join(home, d[2:])
	}
	return d
}

// shortPath shows p with the home folder as ~ (sanitized).
func shortPath(p string) string {
	home := homeDir()
	if home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			if rel == "." {
				p = "~"
			} else {
				p = "~/" + filepath.ToSlash(rel)
			}
		}
	}
	return textwidth.Sanitize(p)
}

// setToolKey sets [tools.leet] key = v (nil deletes it) and saves config.toml.
func setToolKey(ctx *ui.Context, key string, v any) tea.Cmd {
	if ctx == nil || ctx.Config == nil {
		return nil
	}
	sec := config.ToolConfigSection(toolID)
	if ctx.Config.Tools == nil {
		ctx.Config.Tools = map[string]map[string]any{}
	}
	tbl := ctx.Config.Tools[sec]
	if tbl == nil {
		tbl = map[string]any{}
		ctx.Config.Tools[sec] = tbl
	}
	if v == nil {
		delete(tbl, key)
	} else {
		tbl[key] = v
	}
	if err := config.Save(ctx.Paths, ctx.Config); err != nil {
		return ui.Toast(ctx.T.Leetcode.SaveFailed(), ui.ToastError)
	}
	return nil
}

// ---- spaces ----

// currentSpaceID is the space of the channel shown in a route slot ("" = none).
func currentSpaceID(ctx *ui.Context) string {
	if ctx == nil || ctx.Store == nil {
		return ""
	}
	for _, slot := range []layout.SlotID{layout.SlotMain, layout.SlotThread} {
		r, ok := ctx.Routes[slot]
		if !ok {
			continue
		}
		ch := ""
		switch r.Kind {
		case ui.RouteChannel:
			ch = r.ID
		case ui.RouteThread:
			ch = r.Arg
		}
		if c := ctx.Store.Channels[ch]; c != nil && c.SpaceID != nil {
			return *c.SpaceID
		}
	}
	return ""
}

// canManage: I am the space's owner or admin.
func canManage(ctx *ui.Context, spaceID string) bool {
	if ctx == nil || ctx.Store == nil || spaceID == "" {
		return false
	}
	sd := ctx.Store.Spaces[spaceID]
	return sd != nil && (sd.MyRole == api.RoleOwner || sd.MyRole == api.RoleAdmin)
}

// spaceName is a space's name, sanitized ("" unknown).
func spaceName(ctx *ui.Context, spaceID string) string {
	if ctx == nil || ctx.Store == nil {
		return ""
	}
	if sd := ctx.Store.Spaces[spaceID]; sd != nil {
		return textwidth.Sanitize(sd.Name)
	}
	return ""
}

// dailyOn reports whether #daily is on in spaceID as far as this run knows
// (the last LeetMe and the switches flipped since).
func dailyOn(ctx *ui.Context, spaceID string) bool {
	me := knownMe(ctx)
	if me == nil {
		return false
	}
	for _, s := range me.DailySpaces {
		if s.SpaceID == spaceID {
			return true
		}
	}
	return false
}

// setDaily turns #daily on or off in spaceID (owner/admin; the server
// checks). The answer is remembered (dailyOn) and toasted from the Cmd itself,
// so it lands whether or not a leet screen is open.
func setDaily(ctx *ui.Context, spaceID string, enabled bool) tea.Cmd {
	client, t, name, a := ctx.API, ctx.T, spaceName(ctx, spaceID), accountOf(ctx)
	return func() tea.Msg {
		if client == nil {
			return nil
		}
		res, err := client.SetSpaceLeet(context.Background(), spaceID, enabled, nil)
		if err != nil {
			return ui.ErrorMsg{Err: err}
		}
		applyDailySet(a, spaceID, res)
		if res.Enabled {
			return ui.ToastMsg{Text: t.Leetcode.DailyEnabled(name), Kind: ui.ToastSuccess}
		}
		return ui.ToastMsg{Text: t.Leetcode.DailyDisabled(name), Kind: ui.ToastSuccess}
	}
}

// applyDailySet remembers a #daily switch answer in a's memory.
func applyDailySet(a account, spaceID string, res *api.LeetSpaceSettings) {
	if res == nil {
		return
	}
	memoMu.Lock()
	defer memoMu.Unlock()
	mm := memos[a]
	if mm == nil {
		mm = newMemo()
		memos[a] = mm
	}
	var cp api.LeetMe
	if mm.me != nil {
		cp = *mm.me
	}
	spaces := []api.LeetDailySpace{}
	for _, s := range cp.DailySpaces {
		if s.SpaceID != spaceID {
			spaces = append(spaces, s)
		}
	}
	if res.Enabled {
		ch := ""
		if res.ChannelID != nil {
			ch = *res.ChannelID
		}
		spaces = append(spaces, api.LeetDailySpace{SpaceID: spaceID, ChannelID: ch})
	}
	cp.DailySpaces = spaces
	if cp.Recent == nil {
		cp.Recent = []api.LeetSolve{}
	}
	mm.me = &cp
}

// ---- drawing helpers ----

func themeOf(ctx *ui.Context) *theme.Theme {
	if ctx != nil && ctx.Theme != nil {
		return ctx.Theme
	}
	return theme.Get(theme.Default)
}

// padStyled pads a rendered line to w cells with base-styled spaces.
func padStyled(base func(...string) string, s string, w int) string {
	if n := w - textwidth.Width(s); n > 0 {
		return s + base(strings.Repeat(" ", n))
	}
	return s
}

// fitStyled cuts a rendered line to w cells and pads it.
func fitStyled(th *theme.Theme, s string, w int) string {
	if w <= 0 {
		return ""
	}
	if textwidth.Width(s) > w {
		s = textwidth.Truncate(s, w, "…")
	}
	return padStyled(th.S.Base.Render, s, w)
}
