package gittool_test

import (
	"slices"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/uitest"
)

// cmdMsgs runs cmd and returns its msgs, tea.Batch expanded.
func cmdMsgs(t *testing.T, cmd tea.Cmd) []tea.Msg {
	t.Helper()
	if cmd == nil {
		return nil
	}
	switch m := cmd().(type) {
	case nil:
		return nil
	case tea.BatchMsg:
		var out []tea.Msg
		for _, c := range m {
			out = append(out, cmdMsgs(t, c)...)
		}
		return out
	default:
		return []tea.Msg{m}
	}
}

// instanceOf is the live component of instance id.
func instanceOf(t *testing.T, h *uitest.Harness, id string) ui.Component {
	t.Helper()
	for _, r := range h.Model.Nav().Tools {
		if r.Kind == ui.RouteTool && r.ID == id {
			if c := h.Instance(r.Key()); c != nil {
				return c
			}
		}
	}
	t.Fatalf("no instance %s (open: %v)", id, toolIDs(h))
	return nil
}

// choose presses keys on c directly, answers its repository check and
// delivers what the choice asks for in reverse order — bubbletea runs
// batched Cmds on their own goroutines, so their msgs arrive in any order.
func choose(t *testing.T, h *uitest.Harness, c ui.Component, keys ...tea.KeyPressMsg) {
	t.Helper()
	ctx := h.Context()
	var cmd tea.Cmd
	for _, k := range keys {
		_, cmd = c.(ui.KeyHandler).HandleKey(ctx, k)
	}
	var out []tea.Msg
	for _, m := range cmdMsgs(t, cmd) { // the repository check
		out = append(out, cmdMsgs(t, c.Update(ctx, m))...)
	}
	if len(out) == 0 {
		t.Fatal("choosing asked for nothing")
	}
	slices.Reverse(out)
	for _, m := range out {
		h.Send(m)
	}
}

var (
	keyEnter = tea.KeyPressMsg{Code: tea.KeyEnter}
	keyDown  = tea.KeyPressMsg{Code: tea.KeyDown}
)

// A full tool surface (maxTools-1 instances, the picker one of them): the
// picker's place is free before the chosen repository opens, whatever order
// the msgs land in.
func TestPickerOpensOnFullSurface(t *testing.T) {
	f, _ := running(t, macEnv, native("/r/masil"), native("/r/a"), native("/r/b"), native("/r/c"), native("/r/d"))
	f.SetLimit(8)
	h := uitest.New(t)
	for _, r := range []string{native("/r/a"), native("/r/b"), native("/r/c"), native("/r/d")} {
		h.Send(ui.OpenTool("git", "git:"+r, nil)())
	}
	setRecent(t, h, native("/r/masil")) // after the launches (they push theirs)
	h.Send(ui.OpenTool("git", "git:pick", nil)())
	if ids := toolIDs(h); len(ids) != 5 || !slices.Contains(ids, "git:pick") {
		t.Fatalf("instances = %v, want 4 + the picker", ids)
	}
	choose(t, h, instanceOf(t, h, "git:pick"), keyEnter)
	ids := toolIDs(h)
	if !slices.Contains(ids, "git:"+native("/r/masil")) || slices.Contains(ids, "git:pick") {
		t.Fatalf("instances = %v, want git:/r/masil in the picker's place", ids)
	}
}

// The same for an instance that went back to picking and chooses another
// repository.
func TestInstancePickOtherOnFullSurface(t *testing.T) {
	f, _ := running(t, macEnv, native("/r/masil"), native("/r/blog"), native("/r/a"), native("/r/b"), native("/r/c"), native("/r/d"))
	f.SetLimit(0) // every launch fails: the instances are picking
	h := uitest.New(t)
	setRecent(t, h, native("/r/masil"), native("/r/blog"))
	for _, r := range []string{native("/r/masil"), native("/r/a"), native("/r/b"), native("/r/c"), native("/r/d")} {
		h.Send(ui.OpenTool("git", "git:"+r, nil)())
	}
	if ids := toolIDs(h); len(ids) != 5 {
		t.Fatalf("instances = %v, want 5", ids)
	}
	choose(t, h, instanceOf(t, h, "git:"+native("/r/masil")), keyDown, keyEnter)
	ids := toolIDs(h)
	if !slices.Contains(ids, "git:"+native("/r/blog")) || slices.Contains(ids, "git:"+native("/r/masil")) {
		t.Fatalf("instances = %v, want git:/r/blog in masil's place", ids)
	}
}
