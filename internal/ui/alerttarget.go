package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
)

// AlertTargetFunc opens the screen of an alert target core does not know
// (ui/app's alert toast). A nil Cmd (the tool's feature flag is off, the
// target is gone) leaves core's fallback: the notifications inbox.
type AlertTargetFunc func(ctx *Context, a api.Alert) tea.Cmd

// RegisterAlertTarget registers the opener of alert target.type targetType —
// or "<type>:<doc_type>" for one document kind ("doc:arch"). Call it from
// init(). Empty key, nil func or a duplicate => panic. Registrations: P2
// "workspace_share", P3 "doc:arch", P4 "doc:apicol".
func RegisterAlertTarget(targetType string, f func(ctx *Context, a api.Alert) tea.Cmd) {
	if targetType == "" || f == nil {
		panic(fmt.Sprintf("ui: RegisterAlertTarget: empty target type or nil func (%q)", targetType))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := reg.AlertTargets[targetType]; dup {
		panic(fmt.Sprintf("ui: RegisterAlertTarget: duplicate target type %q", targetType))
	}
	reg.AlertTargets[targetType] = f
}

// AlertTargetCmd runs the registered opener of a's target: "<type>:<doc_type>"
// first when target.doc_type is set, then "<type>". ok is false when neither
// is registered; a registered opener may still return a nil Cmd.
func AlertTargetCmd(ctx *Context, a api.Alert) (tea.Cmd, bool) {
	t := a.Target
	keys := []string{t.Type}
	if t.DocType != "" {
		keys = []string{t.Type + ":" + t.DocType, t.Type}
	}
	regMu.Lock()
	var f AlertTargetFunc
	for _, k := range keys {
		if f = reg.AlertTargets[k]; f != nil {
			break
		}
	}
	regMu.Unlock()
	if f == nil {
		return nil, false
	}
	return f(ctx, a), true
}
