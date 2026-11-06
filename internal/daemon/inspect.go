package daemon

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/instance"
)

const (
	// problemTail is how much of the end of daemon.log Inspect scans.
	problemTail = 64 << 10
	// maxProblems is how many WARN/ERROR lines a Report keeps (the newest).
	maxProblems = 3
)

// Server sources for Report.ServerSource.
const (
	ServerSourceDaemon = "daemon" // daemon-status.json of the running daemon
	ServerSourceConfig = "config" // config.toml server > config.DefaultServer
)

// Report is what `masil daemon status` shows. It describes the daemon's own
// view, never the CLI shell's MASIL_SERVER (plan 06 Review Focus 1).
type Report struct {
	Running        bool // daemon.lock held by another process
	LoggedIn       bool
	Handle         string
	CredServer     string
	ConfigServer   string // the server the DAEMON uses (see ServerSource), never the CLI shell's MASIL_SERVER
	ServerSource   string // "daemon": read from daemon-status.json of the running daemon; "config": config.toml server > DefaultServer
	ShellServer    string // this shell's MASIL_SERVER when set and != ConfigServer (the autostarted daemon cannot see it); "" otherwise
	ServerMismatch bool   // LoggedIn && !creds.Matches(ConfigServer)
	DesktopEnabled bool
	State          *State   // nil if no daemon-state.json
	Problems       []string // last <= 3 lines of daemon.log containing level=WARN or level=ERROR (from the last 64 KiB)
}

// Inspect gathers the daemon's status without disturbing a running daemon:
// the lock probe is released at once (Run retries its acquire to absorb it).
// A nil c is loaded from config.toml.
func Inspect(p config.Paths, c *config.Config) Report {
	if c == nil {
		c, _ = config.Load(p) // never nil; partial values are usable
	}
	r := Report{
		Running:        daemonRunning(p),
		DesktopEnabled: c.Notify.Desktop,
		ConfigServer:   AutostartServer(c),
		ServerSource:   ServerSourceConfig,
	}
	if r.Running {
		if rs, err := readRunStatus(p); err == nil && rs != nil && rs.ConfigServer != "" {
			r.ConfigServer, r.ServerSource = rs.ConfigServer, ServerSourceDaemon
		}
	}
	// Recomputed from today's credentials.json rather than the run status's flag:
	// the daemon only catches up on its next credential poll.
	if creds, err := auth.Load(p); err == nil {
		r.LoggedIn, r.Handle, r.CredServer = true, creds.Handle, creds.Server
		r.ServerMismatch = !creds.Matches(r.ConfigServer)
	}
	if shell := trimServer(os.Getenv("MASIL_SERVER")); shell != "" && shell != r.ConfigServer {
		r.ShellServer = shell
	}
	if s, err := loadState(p); err == nil {
		r.State = s
	}
	r.Problems = recentProblems(logFilePath(p))
	return r
}

// AutostartServer is the server an autostarted daemon (launchd/systemd/Run key:
// no shell env) will use: c.Server (trailing "/" trimmed) if non-empty, else
// config.DefaultServer. MASIL_SERVER is ignored on purpose.
func AutostartServer(c *config.Config) string {
	s := ""
	if c != nil {
		s = trimServer(c.Server)
	}
	if s == "" {
		s = trimServer(config.DefaultServer)
	}
	return s
}

// trimServer normalizes a server URL the way config.(*Config).ServerURL does.
func trimServer(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "/")
}

// daemonRunning reports whether another process holds daemon.lock.
func daemonRunning(p config.Paths) bool {
	release, err := instance.AcquireDaemon(p)
	if err == nil {
		release()
		return false
	}
	return errors.Is(err, instance.ErrAlreadyRunning)
}

// recentProblems returns the newest maxProblems WARN/ERROR lines within the
// last problemTail bytes of the log at path; nil if there are none or the log
// cannot be read.
func recentProblems(path string) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil
	}
	off := max(st.Size()-problemTail, 0)
	buf := make([]byte, st.Size()-off)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return nil
	}
	if off > 0 { // drop the partial first line
		if i := bytes.IndexByte(buf, '\n'); i >= 0 {
			buf = buf[i+1:]
		} else {
			return nil
		}
	}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(buf))
	sc.Buffer(make([]byte, 0, 4096), problemTail)
	for sc.Scan() {
		if line := sc.Text(); isProblemLine(line) {
			out = append(out, line)
		}
	}
	if len(out) > maxProblems {
		out = out[len(out)-maxProblems:]
	}
	return out
}

// isProblemLine reports whether a slog text line has level WARN or ERROR
// (including offsets such as WARN+2). Only the level attribute counts, so a
// message or value that merely mentions "level=WARN" is not a problem.
func isProblemLine(line string) bool {
	if strings.HasPrefix(line, "time=") {
		_, line, _ = strings.Cut(line, " ")
	}
	lvl, ok := strings.CutPrefix(line, "level=")
	if !ok {
		return false
	}
	lvl, _, _ = strings.Cut(lvl, " ")
	return strings.HasPrefix(lvl, "WARN") || strings.HasPrefix(lvl, "ERROR")
}
