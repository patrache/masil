package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/instance"
	"github.com/patrache/masil/internal/notify"
	"github.com/patrache/masil/internal/realtime"
)

// Options configures Run (CA 9.2).
type Options struct {
	Paths           config.Paths
	Config          *config.Config
	Backend         notify.Backend // nil => notify.NewOSBackend("masil")
	Logger          *slog.Logger   // nil => discard
	Now             func() time.Time
	PollCredentials time.Duration // default 30s
}

// ErrRestartRequired is returned by Run when the daemon's own executable was
// replaced (brew upgrade, install.sh): `masil daemon run` exits non-zero so
// launchd/systemd relaunch the new binary.
var ErrRestartRequired = errors.New("daemon: executable replaced; restart required")

const (
	defaultPoll     = 30 * time.Second
	acquireAttempts = 3
	acquireRetry    = 200 * time.Millisecond
	stateSaveDelay  = 5 * time.Second // debounce for daemon-state.json after a mark
)

// deps are the seams tests replace.
type deps struct {
	dial       func(ctx context.Context, url string, h http.Header) (realtime.Conn, error) // nil => realtime default
	after      func(time.Duration) <-chan time.Time
	tuiRunning func(config.Paths) (bool, error)
	exe        string // watched executable; "" => no restart-on-upgrade
}

// Run is the daemon: single instance, wait for matching credentials, keep a
// notify-mode session, show alerts, persist the resume point. It returns nil
// when ctx ends, instance.ErrAlreadyRunning if another daemon holds the lock,
// or ErrRestartRequired after the executable was replaced.
func Run(ctx context.Context, o Options) error {
	return run(ctx, o, deps{after: time.After, tuiRunning: instance.TUIRunning, exe: watchedExecutable()})
}

// daemon is the state of one run. The main loop and a session's watcher never
// touch it at the same time (the loop waits for the watcher to finish).
type daemon struct {
	o        Options
	d        deps
	p        config.Paths
	log      *slog.Logger
	notifier alertSink
	deviceID string
	xw       *exeWatch

	cfg          *config.Config
	osLang       i18n.Inputs // MASIL_LANG + OS language, read once at start
	lang         i18n.Locale // effective language: Accept-Language (config + OS; P5 adds state.json)
	desktop      atomic.Bool
	reloadWarned bool

	waitKey       string     // current waiting reason; its log line is written once per change
	lastStatus    *runStatus // last daemon-status.json written (UpdatedAt zeroed)
	statusWarned  bool
	rejectedToken string // never redial with this token (CA 9.2)
	haltedKey     string // haltKey of a session the realtime client halted (4009 twice); never redial it
}

// haltKey identifies what a 4009 halt is tied to: the same token against the
// same server. A new login or a different server lifts the halt.
func haltKey(creds *auth.Credentials, server string) string {
	return creds.Token + "\x00" + server
}

func run(ctx context.Context, o Options, d deps) error {
	if o.Logger == nil {
		o.Logger = slog.New(slog.DiscardHandler)
	}
	if o.Backend == nil {
		o.Backend = notify.NewOSBackend("masil")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.PollCredentials <= 0 {
		o.PollCredentials = defaultPoll
	}
	if d.after == nil {
		d.after = time.After
	}
	if d.tuiRunning == nil {
		d.tuiRunning = instance.TUIRunning
	}
	p := o.Paths
	if o.Config == nil {
		o.Config, _ = config.Load(p) // never nil; ErrPartial values are usable
	}
	if err := config.EnsureDir(p); err != nil {
		return err
	}

	release, err := acquire(ctx, p, d.after)
	if err != nil || release == nil {
		return err
	}
	defer release()

	deviceID, err := auth.DeviceID(p)
	if err != nil {
		return fmt.Errorf("daemon: device id: %w", err)
	}
	dm := &daemon{
		o: o, d: d, p: p, log: o.Logger,
		notifier: newLedgerSink(o.Backend, notify.OpenLedger(p), o.Logger),
		deviceID: deviceID,
		xw:       newExeWatch(d.exe),
		cfg:      o.Config,
		osLang:   i18n.DetectOS(),
	}
	dm.lang = resolveLanguage(dm.osLang, o.Config)
	dm.desktop.Store(o.Config.Notify.Desktop)
	return dm.loop(ctx)
}

// resolveLanguage decides the daemon's language from [ui] language and the
// OS (i18n spec 6.2.2; the TUI-recorded language is P5's). It runs at start
// and on every config reload, so a changed [ui] language reaches the next
// session (Accept-Language) without a restart.
func resolveLanguage(in i18n.Inputs, cfg *config.Config) i18n.Locale {
	in.Config = cfg.UI.Language
	return i18n.Resolve(in).Effective
}

// acquire takes daemon.lock, retrying briefly so a `masil daemon status` probe
// that holds it for an instant does not make the daemon give up. A nil release
// with a nil error means ctx ended first.
func acquire(ctx context.Context, p config.Paths, after func(time.Duration) <-chan time.Time) (func(), error) {
	for attempt := 1; ; attempt++ {
		release, err := instance.AcquireDaemon(p)
		if err == nil {
			return release, nil
		}
		if !errors.Is(err, instance.ErrAlreadyRunning) || attempt >= acquireAttempts {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, nil
		case <-after(acquireRetry):
		}
	}
}

// loop judges credentials every round and runs a session while they are usable.
func (dm *daemon) loop(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return nil
		}
		dm.reloadConfig()
		server := dm.cfg.ServerURL()
		creds, cerr := auth.Load(dm.p)
		if cerr != nil {
			creds = nil
		}
		dm.recordStatus(server, creds)

		switch {
		case creds == nil:
			if dm.enterWait("not_logged_in") {
				if !errors.Is(cerr, auth.ErrNoCredentials) {
					dm.log.Warn("credentials load failed", "err", cerr)
				}
				dm.log.Info("not logged in; waiting for credentials")
			}
		case !creds.Matches(server):
			if dm.enterWait("mismatch\x00" + creds.Server + "\x00" + server) {
				dm.log.Warn("credentials server mismatch", "credentials_server", creds.Server, "config_server", server)
			}
		case creds.Token == dm.rejectedToken:
			dm.enterWait("rejected") // logged when the session was rejected
		case haltKey(creds, server) == dm.haltedKey:
			dm.enterWait("halted") // logged when the client halted
		default:
			dm.waitKey = ""
			again, err := dm.session(ctx, creds, server)
			if err != nil || again {
				if err != nil {
					return err
				}
				continue
			}
		}
		if err := dm.pause(ctx); err != nil {
			return err
		}
	}
}

// enterWait records the waiting reason and reports whether it just changed.
func (dm *daemon) enterWait(key string) bool {
	if dm.waitKey == key {
		return false
	}
	dm.waitKey = key
	return true
}

// pause waits one poll interval, then checks whether the executable was replaced.
func (dm *daemon) pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return nil
	case <-dm.d.after(dm.o.PollCredentials):
	}
	if dm.xw.changed() {
		return dm.restart()
	}
	return nil
}

func (dm *daemon) restart() error {
	dm.log.Info("executable replaced; exiting for restart", "exe", dm.xw.path)
	return ErrRestartRequired
}

// reloadConfig re-reads config.toml. Per-field ErrPartial values (a wrong-typed
// or invalid key reset to its default) are used as they are. A file that
// cannot be read or does not parse at all — config.Load then returns plain
// Defaults() wrapped in ErrPartial (CA 3.2) — and any other error keep the
// previous config and warn once until a reload succeeds again.
func (dm *daemon) reloadConfig() {
	c, err := loadConfigFile(dm.p)
	if err != nil {
		if !dm.reloadWarned {
			dm.log.Warn("config reload failed", "err", err)
			dm.reloadWarned = true
		}
		return
	}
	dm.reloadWarned = false
	dm.cfg = c
	if l := resolveLanguage(dm.osLang, c); l != dm.lang {
		dm.lang = l // read when the next session starts
		dm.log.Info("language changed", "lang", string(l))
	}
	if old := dm.desktop.Swap(c.Notify.Desktop); old != c.Notify.Desktop {
		dm.log.Info("desktop setting changed", "desktop", c.Notify.Desktop)
	}
}

// loadConfigFile is config.Load minus its whole-file fallback: it returns an
// error when config.toml exists but cannot be read or is not valid TOML, so the
// caller keeps what it had instead of silently reverting to Defaults(). The
// file is read before and after Load; if the bytes differ (saved mid-reload)
// the result is reported as an error and the next round tries again.
func loadConfigFile(p config.Paths) (*config.Config, error) {
	before, err := os.ReadFile(p.Config)
	switch {
	case errors.Is(err, os.ErrNotExist):
		before = nil // missing file => Defaults() is the real config
	case err != nil:
		return nil, fmt.Errorf("read %s: %w", p.Config, err)
	default:
		var raw map[string]any
		if _, err := toml.Decode(string(before), &raw); err != nil {
			return nil, fmt.Errorf("parse %s: %w", p.Config, err)
		}
	}
	c, err := config.Load(p)
	if err != nil && !errors.Is(err, config.ErrPartial) {
		return nil, err
	}
	after, rerr := os.ReadFile(p.Config)
	if errors.Is(rerr, os.ErrNotExist) {
		after, rerr = nil, nil
	}
	if rerr != nil || !bytes.Equal(before, after) {
		return nil, fmt.Errorf("%s changed while reloading", p.Config)
	}
	return c, nil
}

// recordStatus writes daemon-status.json when what the daemon sees changed
// (always on the first judgement).
func (dm *daemon) recordStatus(server string, creds *auth.Credentials) {
	s := runStatus{
		Version:      runStatusVersion,
		PID:          os.Getpid(),
		ConfigServer: server,
		EnvServer:    strings.TrimSpace(os.Getenv("MASIL_SERVER")) != "",
	}
	if creds != nil {
		s.CredentialsServer = creds.Server
		s.ServerMismatch = !creds.Matches(server)
	}
	if dm.lastStatus != nil && *dm.lastStatus == s {
		return
	}
	w := s
	w.UpdatedAt = dm.o.Now()
	if err := writeRunStatus(dm.p, w); err != nil {
		if !dm.statusWarned {
			dm.log.Warn("run status write failed", "err", err)
			dm.statusWarned = true
		}
		return
	}
	dm.statusWarned = false
	dm.lastStatus = &s
}

// session runs one notify-mode connection with creds against server until it
// is rejected, the credentials/server change, the executable is replaced or ctx
// ends. again means "judge again right away"; a non-nil error ends Run.
func (dm *daemon) session(ctx context.Context, creds *auth.Credentials, server string) (again bool, err error) {
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()

	st, lerr := loadState(dm.p)
	if lerr != nil {
		dm.log.Warn("daemon state unreadable; resuming from device ack", "err", lerr)
	}
	pr := newPresence(func() (bool, error) { return dm.d.tuiRunning(dm.p) }, dm.o.Now)
	saveReq := make(chan struct{}, 1)
	var client *realtime.Client
	mark := func(seq int64) {
		client.MarkProcessed(seq)
		select {
		case saveReq <- struct{}{}:
		default:
		}
	}
	h := newHandler(dm.notifier, pr, dm.desktop.Load, dm.o.Now, dm.d.after, mark, dm.log)
	// halted is set when the client stops on its own for a reason that is not
	// a rejection (two 4009 closes within 60s). Run then only waits for its
	// ctx, so the session is cancelled here. The daemon then treats it like a
	// rejection for redial purposes (events.md 1.2/7, CA 4.4: the TUI does not
	// keep redialing either): 4009 is an eviction for having more than 10
	// connections, and redialing would evict the user's oldest one again. It
	// stays offline until the credentials or server change, or the process
	// restarts. OnState runs on this goroutine (inside Run), so reading halted
	// after Run returned needs no lock.
	var halted error
	onState := func(s realtime.State) {
		pr.observe(s.TUIOnDevice)
		if s.Phase == realtime.PhaseStopped && s.Err != nil && !isRejection(s.Err) && sctx.Err() == nil {
			halted = s.Err
			cancel()
		}
	}
	rest := api.New(server, api.WithToken(creds.Token))
	rest.SetLanguage(string(dm.lang))
	client = realtime.New(realtime.Options{
		API:      rest,
		DeviceID: dm.deviceID,
		Mode:     realtime.ModeNotify,
		Resume:   resumePoint(st, creds.UserID),
		OnEvent:  h.onEvent,
		OnState:  onState,
		Logger:   dm.log,
		Now:      dm.o.Now,
		Dial:     dm.d.dial,
	})
	save := func() {
		pos := client.Position()
		if pos.Epoch == nil || pos.LastSeq == nil {
			return
		}
		if err := saveState(dm.p, State{UserID: creds.UserID, Epoch: *pos.Epoch, LastSeq: *pos.LastSeq, SavedAt: dm.o.Now()}); err != nil {
			dm.log.Warn("daemon state save failed", "err", err)
		}
	}

	// The handler and the saver outlive the socket a little: they stop only
	// after Run returned, so an alert being shown still gets its mark, and the
	// final save below sees it.
	hctx, hcancel := context.WithCancel(context.Background())
	var workers, watcher sync.WaitGroup
	workers.Go(func() { h.run(hctx) })
	workers.Go(func() {
		for {
			select {
			case <-hctx.Done():
				return
			case <-saveReq:
			}
			select {
			case <-hctx.Done():
				return
			case <-dm.d.after(stateSaveDelay):
			}
			save()
		}
	})
	var restart atomic.Bool
	watcher.Go(func() { dm.watch(sctx, cancel, creds, server, &restart) })

	runErr := client.Run(sctx)
	cancel()
	watcher.Wait()
	hcancel()
	workers.Wait()
	save()

	switch {
	case restart.Load():
		return false, dm.restart()
	case ctx.Err() != nil:
		return false, nil
	case isRejection(runErr):
		dm.log.Warn("session rejected; waiting for new credentials", "reason", rejectReason(runErr), "token", creds.Redacted())
		dm.rejectedToken = creds.Token
		return true, nil
	case runErr != nil:
		dm.log.Error("unexpected realtime error", "err", runErr)
		return false, nil
	case halted != nil:
		dm.log.Warn("realtime stopped; staying offline until credentials or server change", "reason", halted.Error())
		dm.haltedKey = haltKey(creds, server)
		return true, nil // the loop waits in "halted" until the key changes
	}
	return true, nil // the watcher ended the session: credentials or server changed
}

// watch polls credentials, config and the executable while a session is live,
// cancelling it when the session must change.
func (dm *daemon) watch(sctx context.Context, cancel context.CancelFunc, creds *auth.Credentials, server string, restart *atomic.Bool) {
	for {
		select {
		case <-sctx.Done():
			return
		case <-dm.d.after(dm.o.PollCredentials):
		}
		cur, err := auth.Load(dm.p)
		switch {
		case errors.Is(err, auth.ErrNoCredentials):
			cancel() // logged out
			return
		case err == nil && (cur.Token != creds.Token || cur.Server != creds.Server):
			cancel() // new login
			return
		}
		// Any other read error is transient: keep the session.
		dm.reloadConfig()
		if dm.cfg.ServerURL() != server {
			cancel()
			return
		}
		if dm.xw.changed() {
			restart.Store(true)
			cancel()
			return
		}
	}
}

// isRejection reports whether err is a terminal rejection of the credentials
// or the client version: the daemon must not redial with the same token.
func isRejection(err error) bool {
	return errors.Is(err, realtime.ErrUnauthorized) ||
		errors.Is(err, realtime.ErrSuspended) ||
		errors.Is(err, realtime.ErrUpgradeRequired)
}

func rejectReason(err error) string {
	switch {
	case errors.Is(err, realtime.ErrUnauthorized):
		return "unauthorized"
	case errors.Is(err, realtime.ErrSuspended):
		return "suspended"
	default:
		return "upgrade_required"
	}
}
