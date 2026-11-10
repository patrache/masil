package launch

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
	"sync"
)

type httpHook struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

type hookGroup struct {
	Hooks []httpHook `json:"hooks"`
}

type claudeSettings struct {
	DisableBypassPermissionsMode string                 `json:"disableBypassPermissionsMode"`
	Hooks                        map[string][]hookGroup `json:"hooks,omitempty"`
}

// hookEvents are the claude hooks the receiver observes.
var hookEvents = []string{"Notification", "PermissionRequest", "Stop"}

// hasModeFlag reports a --permission-mode flag among args.
func hasModeFlag(args []string) bool {
	for _, a := range args {
		if flag, _, _ := strings.Cut(a, "="); flag == permissionModeFlag {
			return true
		}
	}
	return false
}

// pinnedMode is the --permission-mode Prepare adds: the mode Check
// reports for an allowed argv, "manual" for a blocked one without a mode
// flag, none for a blocked one with its own (a local-only terminal keeps
// the mode the user asked for; sharing it stays blocked).
func pinnedMode(argv []string, opts []string) string {
	if v := Check(argv); v.Allowed {
		return v.Mode
	}
	if !hasModeFlag(opts) {
		return defaultClaudeMode
	}
	return ""
}

// Prepare returns the argv to run. For claude it writes a 0600 settings file
// in tmpDir that disables the bypass permission mode and points the
// Notification, PermissionRequest and Stop hooks at hookURL (omitted when
// hookURL is empty), and puts "--permission-mode <the mode Check reported>
// --settings <file>" right after the program name (manual without a flag,
// and even when the user gave one). The command-line mode wins over
// permissions.defaultMode from any settings file (user, project, local).
// Right after the program name no option of the user's can take either
// flag as its value (claude's parser may take the next token, even "--" or
// another flag, as an option's value), and Check refuses any user mode
// token that differs from the reported one, so the mode Check reported is
// the one claude starts in (v0.1.3 security (o)). The file is registered as
// masil's own for Check until cleanup.
// cleanup removes the file; call it when the program exits or the workspace
// closes. The user's own settings files are not touched. Other programs get
// argv unchanged and a no-op cleanup.
func Prepare(argv []string, hookURL, tmpDir string) (out []string, cleanup func(), err error) {
	if !IsClaude(argv) {
		return append([]string(nil), argv...), func() {}, nil
	}
	s := claudeSettings{DisableBypassPermissionsMode: "disable"}
	if hookURL != "" {
		s.Hooks = map[string][]hookGroup{}
		for _, ev := range hookEvents {
			s.Hooks[ev] = []hookGroup{{Hooks: []httpHook{{Type: "http", URL: hookURL}}}}
		}
	}
	data, err := json.Marshal(s)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.CreateTemp(tmpDir, settingsPrefix+"*"+settingsSuffix)
	if err != nil {
		return nil, nil, err
	}
	p := f.Name()
	fail := func(e error) ([]string, func(), error) {
		_ = f.Close()
		_ = os.Remove(p)
		return nil, nil, e
	}
	if runtime.GOOS != "windows" {
		if err := f.Chmod(0o600); err != nil {
			return fail(err)
		}
	}
	if _, err := f.Write(data); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		return fail(err)
	}
	out = make([]string, 0, len(argv)+4)
	out = append(out, argv[0])
	if mode := pinnedMode(argv, argv[1:]); mode != "" {
		out = append(out, permissionModeFlag, mode)
	}
	out = append(out, settingsFlag, p)
	out = append(out, argv[1:]...)
	markPrepared(p, true)
	var once sync.Once
	return out, func() {
		once.Do(func() {
			markPrepared(p, false)
			_ = os.Remove(p)
		})
	}, nil
}
