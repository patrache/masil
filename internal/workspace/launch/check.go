// Package launch holds the start rules for programs in a shareable terminal:
// the permission-mode allowlist for AI coding tools, the temporary claude
// --settings file that adds observe-only hooks and disables the bypass
// mode, and the "new terminal" program list.
package launch

import (
	"path/filepath"
	"strings"
	"sync"
)

// Reason says why Check blocked a program.
type Reason int

const (
	ReasonOK Reason = iota
	// ReasonBypassFlag: a flag whose name contains "dangerously".
	ReasonBypassFlag
	// ReasonModeNotAllowed: --permission-mode outside ClaudeModes (or no value).
	ReasonModeNotAllowed
	// ReasonEmpty: no program at all.
	ReasonEmpty
	// ReasonUserSettings: a --settings of the user's own (file or inline
	// JSON). It can set permissions.defaultMode outside ClaudeModes, and how
	// claude combines it with masil's --settings is not known, so it blocks.
	// masil's own file from Prepare (that exact path, until its cleanup) is
	// allowed.
	ReasonUserSettings
)

// Warning is shown with an allowed program.
type Warning int

const (
	// WarnShell: masil cannot see the modes of programs run inside a shell.
	WarnShell Warning = iota
	// WarnUnknownProgram: same sentence, for programs masil does not know.
	WarnUnknownProgram
	// WarnAcceptEdits: claude edits files without asking.
	WarnAcceptEdits
)

// Verdict is the result of Check. Mode is the claude permission mode (""
// for other programs).
type Verdict struct {
	Allowed  bool
	Reason   Reason
	Mode     string
	Warnings []Warning
}

// ClaudeModes is the allowlist of claude permission modes; no flag means
// "manual". Anything else (bypassPermissions, auto, dontAsk, future modes)
// is blocked.
var ClaudeModes = []string{"manual", "plan", "acceptEdits"}

var shells = []string{"sh", "bash", "zsh", "fish", "dash", "ksh", "tcsh", "nu", "pwsh"}

const (
	// defaultClaudeMode is the mode with no --permission-mode flag.
	defaultClaudeMode  = "manual"
	permissionModeFlag = "--permission-mode"
	settingsFlag       = "--settings"
	// settingsPrefix/settingsSuffix name Prepare's temp file (the name
	// alone never makes a --settings masil's; see isMasilSettings).
	settingsPrefix = "masil-claude-"
	settingsSuffix = ".json"
)

// prepared holds the settings files Prepare wrote and has not cleaned up.
// Only those exact paths pass Check as masil's own --settings: a file that
// merely follows the name pattern may hold the user's own settings
// (v0.1.3 security (n)).
var prepared = struct {
	sync.Mutex
	paths map[string]bool
}{paths: map[string]bool{}}

func markPrepared(p string, on bool) {
	prepared.Lock()
	defer prepared.Unlock()
	if on {
		prepared.paths[p] = true
	} else {
		delete(prepared.paths, p)
	}
}

// isMasilSettings reports a --settings value that is a live Prepare file.
func isMasilSettings(v string) bool {
	prepared.Lock()
	defer prepared.Unlock()
	return prepared.paths[v]
}

// ProgramName is the lowercased base name of argv[0] without a Windows
// executable extension.
func ProgramName(arg0 string) string {
	name := strings.ToLower(filepath.Base(filepath.ToSlash(arg0)))
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1"} {
		name = strings.TrimSuffix(name, ext)
	}
	return name
}

// IsClaude reports whether argv starts the claude CLI.
func IsClaude(argv []string) bool { return len(argv) > 0 && ProgramName(argv[0]) == "claude" }

// Check applies the start rules to argv.
func Check(argv []string) Verdict {
	if len(argv) == 0 {
		return Verdict{Reason: ReasonEmpty}
	}
	name := ProgramName(argv[0])
	if name != "claude" {
		for _, s := range shells {
			if name == s {
				return Verdict{Allowed: true, Warnings: []Warning{WarnShell}}
			}
		}
		return Verdict{Allowed: true, Warnings: []Warning{WarnUnknownProgram}}
	}
	// Every option-like token counts, after "--" too: claude's parser can
	// take a "--" as the value of the option before it ("--append-system-
	// prompt -- --permission-mode dontAsk"), leaving what follows live
	// (v0.1.3 security (n)(o)). A prompt word after "--" that merely starts
	// with "-" and names one of these flags is blocked as well; that costs
	// nothing real.
	mode, explicit := defaultClaudeMode, false
	args := argv[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" || !strings.HasPrefix(a, "-") {
			continue
		}
		flag, value, hasValue := strings.Cut(a, "=")
		if strings.Contains(strings.ToLower(flag), "dangerously") {
			return Verdict{Reason: ReasonBypassFlag}
		}
		if flag == settingsFlag {
			if !hasValue {
				if i+1 >= len(args) {
					return Verdict{Reason: ReasonUserSettings}
				}
				i++
				value = args[i]
			}
			if !isMasilSettings(value) {
				return Verdict{Reason: ReasonUserSettings}
			}
			continue
		}
		if flag != permissionModeFlag {
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return Verdict{Reason: ReasonModeNotAllowed}
			}
			i++
			value = args[i]
		}
		if !allowedMode(value) {
			return Verdict{Reason: ReasonModeNotAllowed, Mode: value}
		}
		// Two different modes: which one claude keeps depends on how it
		// parses the options in between, so the mode cannot be named.
		if explicit && value != mode {
			return Verdict{Reason: ReasonModeNotAllowed, Mode: value}
		}
		mode, explicit = value, true
	}
	v := Verdict{Allowed: true, Mode: mode}
	if mode == "acceptEdits" {
		v.Warnings = []Warning{WarnAcceptEdits}
	}
	return v
}

func allowedMode(m string) bool {
	for _, ok := range ClaudeModes {
		if m == ok {
			return true
		}
	}
	return false
}
