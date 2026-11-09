package launch

import (
	"encoding/json"
	"os"
	"runtime"
	"strings"
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

// hasModeFlag reports a --permission-mode flag among args (before "--").
func hasModeFlag(args []string) bool {
	for _, a := range args {
		if flag, _, _ := strings.Cut(a, "="); flag == permissionModeFlag {
			return true
		}
	}
	return false
}

// Prepare returns the argv to run. For claude it writes a 0600 settings file
// in tmpDir that disables the bypass permission mode and points the
// Notification, PermissionRequest and Stop hooks at hookURL (omitted when
// hookURL is empty), and appends "--settings <file>" (before a "--", if any).
// Without a --permission-mode flag it also appends "--permission-mode
// manual": the command-line mode wins over permissions.defaultMode from any
// settings file (user, project, local), so the mode Check reported is the
// one claude starts in.
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
	at := len(argv)
	for i, a := range argv[1:] {
		if a == "--" {
			at = i + 1
			break
		}
	}
	out = make([]string, 0, len(argv)+4)
	out = append(out, argv[:at]...)
	if !hasModeFlag(argv[1:at]) {
		out = append(out, permissionModeFlag, defaultClaudeMode)
	}
	out = append(out, settingsFlag, p)
	out = append(out, argv[at:]...)
	return out, func() { _ = os.Remove(p) }, nil
}
