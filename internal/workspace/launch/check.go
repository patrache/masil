// Package launch holds the start rules for programs in a shareable terminal:
// the permission-mode allowlist for AI coding tools, the temporary claude
// --settings file that adds observe-only hooks and disables the bypass
// mode, and the "new terminal" program list.
package launch

import (
	"path/filepath"
	"strings"
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

const permissionModeFlag = "--permission-mode"

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
	mode := "manual"
	args := argv[1:]
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			break
		}
		if !strings.HasPrefix(a, "-") {
			continue
		}
		flag, value, hasValue := strings.Cut(a, "=")
		if strings.Contains(strings.ToLower(flag), "dangerously") {
			return Verdict{Reason: ReasonBypassFlag}
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
		mode = value
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
