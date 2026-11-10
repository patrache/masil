// Package admincli implements the operator commands `masil admin …` (CA 9.2,
// design spec chapter 2 "global operator role"): list and inspect reports,
// resolve them, force-remove content, suspend and unsuspend accounts, and
// read the user list and the audit log. Output is plain text with
// textwidth-aligned tables; it never prints the session token.
package admincli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/errtext"
	"github.com/patrache/masil/internal/humanize"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/textwidth"
)

// IO is where Run writes and the clock it formats times with.
type IO struct {
	Stdout, Stderr io.Writer
	Now            time.Time
	Loc            *time.Location
}

// ErrUsage marks a usage error (bad subcommand, argument or flag). No request
// was sent and the usage line was written to Stderr.
var ErrUsage = errors.New("admincli: usage")

// Usage is the one-line usage of `masil admin`.
const Usage = "사용법: masil admin reports|report <id>|resolve <id> <resolution>|remove <type> <id>|suspend @h [--for 7d]|unsuspend @h|users|actions"

const (
	nameWidth       = 16  // users: display name column
	reasonWidth     = 30  // actions: reason column
	contentWidth    = 200 // report detail: snapshot line
	detailLabelPad  = 8   // report detail: label column
	minSuspendSec   = 60
	forHint         = "정지 기간은 7d, 12h, 30m 처럼 1분 이상으로 적어 주세요"
	missingCell     = "-"
	truncateTail    = "…"
	nextPagePrefix  = "다음 페이지: masil admin "
	notAdminMessage = "운영자 권한이 없습니다."
	adminConflict   = "운영자는 정지할 수 없습니다."
)

// Korean labels for server enums; unknown values are shown as sent (sanitized).
var (
	reasonLabel = map[string]string{
		"spam": "스팸", "harassment": "괴롭힘", "illegal": "불법",
		"sexual": "성적 콘텐츠", "violence": "폭력", "other": "기타",
	}
	statusLabel     = map[string]string{"open": "열림", "resolved": "처리됨", "dismissed": "기각"}
	resolutionLabel = map[string]string{"content_removed": "콘텐츠 삭제", "user_suspended": "계정 정지", "no_action": "조치 없음"}
	actionLabel     = map[string]string{
		"remove_content": "삭제", "suspend_user": "정지", "unsuspend_user": "정지 해제",
		"resolve_report": "신고 처리", "dismiss_report": "신고 기각",
	}
	roleLabel = map[string]string{"user": "사용자", "admin": "운영자"}

	removeTypes = []string{"message", "post", "story", "guestbook_entry", "upload", "space"}
	statuses    = []string{"open", "resolved", "dismissed"}
	resolutions = []string{"content_removed", "user_suspended", "no_action"}
)

var (
	handleRe = regexp.MustCompile(`^@[A-Za-z0-9_]{3,20}$`)
	ulidRe   = regexp.MustCompile(`^[0-9A-HJKMNP-TV-Z]{26}$`)
	daysRe   = regexp.MustCompile(`^[1-9][0-9]*d$`)
)

// sub is one subcommand: its positional argument count and its handler.
type sub struct {
	npos int
	run  func(r *runner, pos []string, flags []string) error
}

var subs = map[string]sub{
	"reports":   {0, (*runner).reports},
	"report":    {1, (*runner).report},
	"resolve":   {2, (*runner).resolve},
	"remove":    {2, (*runner).remove},
	"suspend":   {1, (*runner).suspend},
	"unsuspend": {1, (*runner).unsuspend},
	"users":     {0, (*runner).users},
	"actions":   {0, (*runner).actions},
	// leet-daily parses its own "set <date>" words and flags (leet.go)
	"leet-daily": {0, (*runner).leetDaily},
}

// runner carries one Run call's client and IO.
type runner struct {
	ctx  context.Context
	mk   ClientFunc
	c    *api.Client
	o    IO
	name string // the subcommand
}

// ClientFunc makes the API client. RunLazy calls it only after the arguments
// parsed, so help and usage errors never need credentials. An error from it is
// returned as is (the function explains it on its own).
type ClientFunc func() (*api.Client, error)

// client returns the API client, building it on first use.
func (r *runner) client() (*api.Client, error) {
	if r.c == nil {
		c, err := r.mk()
		if err != nil {
			return nil, err
		}
		r.c = c
	}
	return r.c, nil
}

// Run dispatches args (without the leading "admin") to one of the eight
// subcommands. Positional arguments come first, flags after them. A usage
// error sends no request and returns an error wrapping ErrUsage; an API error
// is explained on Stderr and returned.
func Run(ctx context.Context, c *api.Client, args []string, o IO) error {
	return RunLazy(ctx, func() (*api.Client, error) { return c, nil }, args, o)
}

// RunLazy is Run with a client made by mk only once the arguments are valid:
// -h/--help and usage errors are handled without calling mk (CA 2.4, CR-D).
func RunLazy(ctx context.Context, mk ClientFunc, args []string, o IO) error {
	if o.Loc == nil {
		o.Loc = time.Local
	}
	if len(args) == 0 {
		return usage(o, "", "no subcommand")
	}
	switch args[0] {
	case "help", "-h", "-help", "--help":
		fmt.Fprintln(o.Stdout, Usage)
		return nil
	}
	s, ok := subs[args[0]]
	if !ok {
		return usage(o, "알 수 없는 명령입니다: "+args[0], "unknown subcommand "+strconv.Quote(args[0]))
	}
	rest := args[1:]
	if len(rest) < s.npos {
		return usage(o, "", args[0]+": missing arguments")
	}
	pos := rest[:s.npos]
	for _, p := range pos {
		if strings.HasPrefix(p, "-") {
			return usage(o, "", args[0]+": missing arguments")
		}
	}
	r := &runner{ctx: ctx, mk: mk, o: o, name: args[0]}
	return s.run(r, pos, rest[s.npos:])
}

// usage writes an optional reason and Usage to Stderr and returns ErrUsage.
func usage(o IO, reason, detail string) error {
	if reason != "" {
		fmt.Fprintln(o.Stderr, textwidth.Sanitize(reason))
	}
	fmt.Fprintln(o.Stderr, Usage)
	return fmt.Errorf("%w: %s", ErrUsage, detail)
}

// parse parses flags with a fresh FlagSet; extra positional arguments are a
// usage error. done=true means return err (nil after -h printed the usage).
func (r *runner) parse(flags []string, setup func(fs *flag.FlagSet)) (done bool, err error) {
	fs := flag.NewFlagSet(r.name, flag.ContinueOnError)
	fs.SetOutput(r.o.Stderr)
	fs.Usage = func() {} // our own Korean usage line follows
	setup(fs)
	if err := fs.Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(r.o.Stdout, Usage)
			return true, nil
		}
		return true, usage(r.o, "", r.name+": "+err.Error())
	}
	if fs.NArg() > 0 {
		return true, usage(r.o, "필요 없는 인자가 있습니다: "+strings.Join(fs.Args(), " "), r.name+": extra arguments")
	}
	return false, nil
}

// pageFlags registers --cursor and --limit.
func pageFlags(fs *flag.FlagSet, p *api.PageOpts) {
	fs.StringVar(&p.Cursor, "cursor", "", "")
	fs.IntVar(&p.Limit, "limit", 0, "")
}

// checkLimit rejects a negative --limit.
func (r *runner) checkLimit(p api.PageOpts) error {
	if p.Limit < 0 {
		return usage(r.o, "--limit 은 0 이상이어야 합니다", r.name+": negative limit")
	}
	return nil
}

// apiErr explains err on Stderr and returns it.
func (r *runner) apiErr(err error) error {
	switch {
	case api.IsCode(err, "not_admin"):
		fmt.Fprintln(r.o.Stderr, notAdminMessage)
	case r.name == "suspend" && api.IsCode(err, "conflict"):
		fmt.Fprintln(r.o.Stderr, adminConflict)
	default:
		fmt.Fprintln(r.o.Stderr, "오류: "+errtext.Describe(i18n.KoT(), err))
	}
	return err
}

// ---- subcommands ----

func (r *runner) reports(_ []string, flags []string) error {
	var q api.AdminReportQuery
	if done, err := r.parse(flags, func(fs *flag.FlagSet) {
		fs.StringVar(&q.Status, "status", "", "")
		fs.StringVar(&q.TargetType, "type", "", "")
		pageFlags(fs, &q.PageOpts)
	}); done {
		return err
	}
	if q.Status != "" && !slices.Contains(statuses, q.Status) {
		return usage(r.o, "--status 는 open, resolved, dismissed 중 하나입니다", "reports: bad status")
	}
	if err := r.checkLimit(q.PageOpts); err != nil {
		return err
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	p, err := c.AdminListReports(r.ctx, q)
	if err != nil {
		return r.apiErr(err)
	}
	if len(p.Items) == 0 {
		fmt.Fprintln(r.o.Stdout, "신고가 없습니다.")
	} else {
		rows := make([][]string, 0, len(p.Items))
		for _, it := range p.Items {
			rows = append(rows, []string{
				clean(it.ID), reportTarget(it), label(reasonLabel, it.Reason), at(it.Reporter),
				strconv.Itoa(it.OpenReportsOnTarget), label(statusLabel, it.Status), r.when(it.CreatedAt),
			})
		}
		fmt.Fprint(r.o.Stdout, Table([]string{"ID", "대상", "사유", "신고자", "누적", "상태", "시각"}, rows))
	}
	r.nextPage(p.NextCursor, "reports", "--status", q.Status, "--type", q.TargetType, "--limit", limitArg(q.Limit))
	return nil
}

func (r *runner) report(pos []string, flags []string) error {
	if done, err := r.parse(flags, func(*flag.FlagSet) {}); done {
		return err
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	rep, err := c.AdminGetReport(r.ctx, pos[0])
	if err != nil {
		return r.apiErr(err)
	}
	var b strings.Builder
	line := func(lbl, val string) {
		b.WriteString("  " + textwidth.PadRight(lbl, detailLabelPad) + val + "\n")
	}
	b.WriteString("신고 " + clean(rep.ID) + "\n")
	line("상태", label(statusLabel, rep.Status))
	line("대상", reportTarget(*rep))
	reason := label(reasonLabel, rep.Reason)
	if note := oneLine(rep.Note); note != "" {
		reason += " — " + note
	}
	line("사유", reason)
	line("신고자", at(rep.Reporter)+" · "+r.when(rep.CreatedAt))
	line("누적", fmt.Sprintf("열린 신고 %d건", rep.OpenReportsOnTarget))
	line("내용", snapshotLine(rep.Snapshot))
	if rep.Status != "open" {
		parts := []string{}
		if rep.Resolution != nil {
			parts = append(parts, label(resolutionLabel, *rep.Resolution))
		}
		if rep.ResolvedBy != nil {
			parts = append(parts, at(*rep.ResolvedBy))
		}
		if rep.ResolvedAt != nil {
			parts = append(parts, r.when(*rep.ResolvedAt))
		}
		v := strings.Join(parts, " · ")
		if rep.ResolutionNote != nil {
			if note := oneLine(*rep.ResolutionNote); note != "" {
				v += " — " + note
			}
		}
		line("처리", v)
	}
	fmt.Fprint(r.o.Stdout, b.String())
	return nil
}

func (r *runner) resolve(pos []string, flags []string) error {
	var note string
	if done, err := r.parse(flags, func(fs *flag.FlagSet) { fs.StringVar(&note, "note", "", "") }); done {
		return err
	}
	if !slices.Contains(resolutions, pos[1]) {
		return usage(r.o, "처리는 content_removed, user_suspended, no_action 중 하나입니다", "resolve: bad resolution")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	rep, err := c.AdminResolveReport(r.ctx, pos[0], pos[1], note)
	if err != nil {
		return r.apiErr(err)
	}
	res := pos[1]
	if rep.Resolution != nil {
		res = *rep.Resolution
	}
	fmt.Fprintf(r.o.Stdout, "신고 %s: %s (%s)\n", clean(rep.ID), label(statusLabel, rep.Status), label(resolutionLabel, res))
	return nil
}

func (r *runner) remove(pos []string, flags []string) error {
	var reason, reportID string
	if done, err := r.parse(flags, func(fs *flag.FlagSet) {
		fs.StringVar(&reason, "reason", "", "")
		fs.StringVar(&reportID, "report", "", "")
	}); done {
		return err
	}
	if !slices.Contains(removeTypes, pos[0]) {
		return usage(r.o, "삭제할 종류는 "+strings.Join(removeTypes, ", ")+" 중 하나입니다", "remove: bad type")
	}
	if strings.TrimSpace(reason) == "" {
		return usage(r.o, "--reason 으로 삭제 사유를 적어 주세요", "remove: missing reason")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	act, err := c.AdminRemoveContent(r.ctx, api.AdminRemoveRequest{TargetType: pos[0], TargetID: pos[1], Reason: reason, ReportID: reportID})
	if err != nil {
		return r.apiErr(err)
	}
	fmt.Fprintf(r.o.Stdout, "삭제했습니다: %s %s (조치 %s)\n", pos[0], clean(pos[1]), clean(act.ID))
	return nil
}

func (r *runner) suspend(pos []string, flags []string) error {
	u, err := r.userRef(pos[0])
	if err != nil {
		return err
	}
	var forStr, reason, reportID string
	if done, err := r.parse(flags, func(fs *flag.FlagSet) {
		fs.StringVar(&forStr, "for", "", "")
		fs.StringVar(&reason, "reason", "", "")
		fs.StringVar(&reportID, "report", "", "")
	}); done {
		return err
	}
	req := api.AdminSuspendRequest{Reason: reason, ReportID: reportID}
	if forStr != "" {
		sec, err := ParseFor(forStr)
		if err != nil {
			return usage(r.o, forHint, "suspend: "+err.Error())
		}
		req.DurationSec = api.Some(sec)
	}
	if strings.TrimSpace(reason) == "" {
		return usage(r.o, "--reason 으로 정지 사유를 적어 주세요", "suspend: missing reason")
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	v, err := c.AdminSuspend(r.ctx, u, req)
	if err != nil {
		return r.apiErr(err)
	}
	until := "무기한"
	if v.SuspendedUntil != nil {
		until = r.when(*v.SuspendedUntil) + "까지"
	}
	fmt.Fprintf(r.o.Stdout, "정지했습니다: %s — %s\n", r.shownUser(pos[0], u, v), until)
	return nil
}

func (r *runner) unsuspend(pos []string, flags []string) error {
	u, err := r.userRef(pos[0])
	if err != nil {
		return err
	}
	if done, err := r.parse(flags, func(*flag.FlagSet) {}); done {
		return err
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	if _, err := c.AdminUnsuspend(r.ctx, u); err != nil {
		return r.apiErr(err)
	}
	fmt.Fprintf(r.o.Stdout, "정지를 풀었습니다: %s\n", r.shownUser(pos[0], u, nil))
	return nil
}

func (r *runner) users(_ []string, flags []string) error {
	var q api.AdminUserQuery
	var suspended string
	if done, err := r.parse(flags, func(fs *flag.FlagSet) {
		fs.StringVar(&q.Q, "q", "", "")
		fs.StringVar(&suspended, "suspended", "", "")
		pageFlags(fs, &q.PageOpts)
	}); done {
		return err
	}
	switch suspended {
	case "":
	case "true", "false":
		b := suspended == "true"
		q.Suspended = &b
	default:
		return usage(r.o, "--suspended 는 true 또는 false 입니다", "users: bad suspended")
	}
	if err := r.checkLimit(q.PageOpts); err != nil {
		return err
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	p, err := c.AdminListUsers(r.ctx, q)
	if err != nil {
		return r.apiErr(err)
	}
	if len(p.Items) == 0 {
		fmt.Fprintln(r.o.Stdout, "사용자가 없습니다.")
	} else {
		rows := make([][]string, 0, len(p.Items))
		for _, u := range p.Items {
			rows = append(rows, []string{
				clean(u.ID), at(u.UserSummary), textwidth.Truncate(clean(u.DisplayName), nameWidth, truncateTail),
				label(roleLabel, u.Role), r.userState(u), strconv.Itoa(u.OpenReportCount), r.when(u.CreatedAt),
			})
		}
		fmt.Fprint(r.o.Stdout, Table([]string{"ID", "사용자", "이름", "역할", "상태", "열린 신고", "가입"}, rows))
	}
	r.nextPage(p.NextCursor, "users", "--q", q.Q, "--suspended", suspended, "--limit", limitArg(q.Limit))
	return nil
}

func (r *runner) actions(_ []string, flags []string) error {
	var q api.AdminActionQuery
	if done, err := r.parse(flags, func(fs *flag.FlagSet) {
		fs.StringVar(&q.TargetType, "target-type", "", "")
		fs.StringVar(&q.TargetID, "target", "", "")
		pageFlags(fs, &q.PageOpts)
	}); done {
		return err
	}
	if err := r.checkLimit(q.PageOpts); err != nil {
		return err
	}
	c, err := r.client()
	if err != nil {
		return err
	}
	p, err := c.AdminListActions(r.ctx, q)
	if err != nil {
		return r.apiErr(err)
	}
	if len(p.Items) == 0 {
		fmt.Fprintln(r.o.Stdout, "조치 기록이 없습니다.")
	} else {
		rows := make([][]string, 0, len(p.Items))
		for _, a := range p.Items {
			report := missingCell
			if a.ReportID != nil && *a.ReportID != "" {
				report = clean(*a.ReportID)
			}
			rows = append(rows, []string{
				clean(a.ID), at(a.Admin), label(actionLabel, a.Action), clean(a.TargetType) + " " + clean(a.TargetID),
				report, textwidth.Truncate(oneLine(a.Reason), reasonWidth, truncateTail), r.when(a.CreatedAt),
			})
		}
		fmt.Fprint(r.o.Stdout, Table([]string{"ID", "운영자", "조치", "대상", "신고", "사유", "시각"}, rows))
	}
	r.nextPage(p.NextCursor, "actions", "--target-type", q.TargetType, "--target", q.TargetID, "--limit", limitArg(q.Limit))
	return nil
}

// ---- helpers ----

// ParseFor parses a suspension length: "<n>d" (days) or a Go duration such as
// "12h" or "1h30m". The result is whole seconds and at least 60.
func ParseFor(s string) (sec int, err error) {
	bad := fmt.Errorf("admincli: bad duration %q", s)
	if daysRe.MatchString(s) {
		n, err := strconv.Atoi(strings.TrimSuffix(s, "d"))
		if err != nil || n > math.MaxInt32/86400 {
			return 0, bad
		}
		return n * 86400, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d%time.Second != 0 || d < minSuspendSec*time.Second || d/time.Second > math.MaxInt32 {
		return 0, bad
	}
	return int(d / time.Second), nil
}

// userRef turns "@handle" or a 26-char ULID into an api.UserRef.
func (r *runner) userRef(s string) (api.UserRef, error) {
	switch {
	case handleRe.MatchString(s):
		return api.Handle(s), nil
	case ulidRe.MatchString(s):
		return api.UserID(s), nil
	}
	return "", usage(r.o, "사용자는 @아이디 또는 사용자 ID 로 적어 주세요: "+s, r.name+": bad user")
}

// shownUser is "@handle" for messages: the server's handle when known, else the argument.
func (r *runner) shownUser(arg string, u api.UserRef, v *api.AdminUserView) string {
	if v != nil && v.Handle != "" {
		return "@" + clean(v.Handle)
	}
	if strings.HasPrefix(arg, "@") {
		return clean(string(u))
	}
	return clean(arg)
}

// userState is 정상 / 정지(무기한) / 정지(~<time>). It follows the server's
// "suspended now" flag: the server keeps suspended_at/until after a timed
// suspension ends (until <= now is active) and reports suspended=false then.
func (r *runner) userState(u api.AdminUserView) string {
	if !u.Suspended {
		return "정상"
	}
	if u.SuspendedUntil == nil {
		return "정지(무기한)"
	}
	return "정지(~" + r.when(*u.SuspendedUntil) + ")"
}

// nextPage prints the command for the next page when there is one; kv are
// flag/value pairs repeated from this call (empty values are skipped).
func (r *runner) nextPage(cursor *string, name string, kv ...string) {
	if cursor == nil || *cursor == "" {
		return
	}
	parts := []string{name}
	for i := 0; i+1 < len(kv); i += 2 {
		if kv[i+1] != "" {
			parts = append(parts, kv[i], shellWord(kv[i+1]))
		}
	}
	parts = append(parts, "--cursor", shellWord(*cursor))
	fmt.Fprintln(r.o.Stdout, nextPagePrefix+strings.Join(parts, " "))
}

// shellWord sanitizes s and single-quotes it when it is not a plain word.
func shellWord(s string) string {
	s = textwidth.Sanitize(s)
	plain := s != ""
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || strings.ContainsRune("-_.:/=+@,", c)) {
			plain = false
			break
		}
	}
	if plain {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func limitArg(n int) string {
	if n <= 0 {
		return ""
	}
	return strconv.Itoa(n)
}

// when formats t like a chat message time, in the IO's clock and zone.
func (r *runner) when(t time.Time) string {
	return humanize.MessageTime(r.o.Now, t, r.o.Loc)
}

// reportTarget is "<type> <id>[ @handle][ (삭제됨)]".
func reportTarget(rep api.Report) string {
	s := clean(rep.TargetType) + " " + clean(rep.TargetID)
	if rep.TargetUser != nil && rep.TargetUser.Handle != "" {
		s += " " + at(*rep.TargetUser)
	}
	if !rep.TargetExists {
		s += " (삭제됨)"
	}
	return s
}

// snapshotLine is the report snapshot's body on one line, or its sorted
// "k=v" pairs when it has no string body.
func snapshotLine(raw json.RawMessage) string {
	var m map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &m) != nil || len(m) == 0 {
		return "(없음)"
	}
	if body, ok := m["body"].(string); ok {
		return textwidth.Truncate(oneLine(body), contentWidth, truncateTail)
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, 0, len(keys))
	for _, k := range keys {
		var v string
		switch x := m[k].(type) {
		case string:
			v = x
		default:
			b, _ := json.Marshal(x)
			v = string(b)
		}
		pairs = append(pairs, k+"="+v)
	}
	return textwidth.Truncate(oneLine(strings.Join(pairs, " · ")), contentWidth, truncateTail)
}

// oneLine collapses whitespace runs (newlines included) to one space, then sanitizes.
func oneLine(s string) string {
	return strings.Join(strings.Fields(textwidth.Sanitize(strings.Join(strings.Fields(s), " "))), " ")
}

func clean(s string) string { return textwidth.Sanitize(s) }

func at(u api.UserSummary) string { return "@" + clean(u.Handle) }

func label(m map[string]string, v string) string {
	if l, ok := m[v]; ok {
		return l
	}
	return clean(v)
}
