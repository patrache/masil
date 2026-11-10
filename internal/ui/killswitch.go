package ui

import (
	"slices"

	tea "charm.land/bubbletea/v2"
)

// The kill-switch registry (v0.1.3 B8.5, A7.5 JX6): a feature that shares
// something live (P2's workspace share) registers how to tell it is on and how
// to cut it at once. While any is active, ctrl+\ x anywhere in masil — a
// focused termpane or any other screen — cuts every active one without asking
// (failing safe), instead of the termpane's "end the program?" question.

// KillSwitch is one registered switch. Active is read on every ctrl+\ (keep it
// cheap, never block, never call the registry); Kill cuts it at once.
type KillSwitch struct {
	Active func() bool
	Kill   func(ctx *Context) tea.Cmd
}

// RegisterKillSwitch registers k (init()-time or when the feature starts). A
// nil Active or Kill panics.
func RegisterKillSwitch(k KillSwitch) {
	if k.Active == nil || k.Kill == nil {
		panic("ui: RegisterKillSwitch: Active and Kill are required")
	}
	regMu.Lock()
	defer regMu.Unlock()
	reg.KillSwitches = append(reg.KillSwitches, k)
}

// killSwitches copies the registered switches (called outside the lock).
func killSwitches() []KillSwitch {
	regMu.Lock()
	defer regMu.Unlock()
	return slices.Clone(reg.KillSwitches)
}

// KillSwitchActive reports whether any registered switch is active.
func KillSwitchActive() bool {
	return slices.ContainsFunc(killSwitches(), func(k KillSwitch) bool { return k.Active() })
}

// KillAll cuts every active switch, in registration order (nil when none is).
func KillAll(ctx *Context) tea.Cmd {
	var cmds []tea.Cmd
	for _, k := range killSwitches() {
		if k.Active() {
			cmds = append(cmds, k.Kill(ctx))
		}
	}
	return tea.Batch(cmds...)
}
