// Package keymap maps stable action ids to key bindings (CA 5.5).
package keymap

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// Action is a stable action id, used in config [keys].
type Action string

// Actions (values = CA 5.5 ids).
const (
	AppQuit           Action = "app.quit"
	PaletteOpen       Action = "palette.open"
	SearchGlobal      Action = "search.global"
	NotificationsOpen Action = "notifications.open"
	WindowPrefix      Action = "window.prefix"
	FocusNext         Action = "focus.next"
	FocusPrev         Action = "focus.prev"
	HelpToggle        Action = "help.toggle"
	FeedToggle        Action = "feed.toggle"
	TabPrev           Action = "tab.prev"
	TabNext           Action = "tab.next"
	NavBack           Action = "nav.back"
	ListUp            Action = "list.up"
	ListDown          Action = "list.down"
	ListTop           Action = "list.top"
	ListBottom        Action = "list.bottom"
	ListPageUp        Action = "list.pageup"
	ListPageDown      Action = "list.pagedown"
	ListOpen          Action = "list.open"
	ComposeFocus      Action = "compose.focus"
	ComposeSend       Action = "compose.send"
	ComposeNewline    Action = "compose.newline"
	ComposeComplete   Action = "compose.complete"
	ComposeEditLast   Action = "compose.edit_last"
	ComposeLeave      Action = "compose.leave"
	MsgThread         Action = "msg.thread"
	MsgReact          Action = "msg.react"
	MsgEdit           Action = "msg.edit"
	MsgDelete         Action = "msg.delete"
	MsgPin            Action = "msg.pin"
	MsgCopy           Action = "msg.copy"
	MsgActions        Action = "msg.actions"
	ChannelSearch     Action = "channel.search"
	PostLike          Action = "post.like"
	PostRepost        Action = "post.repost"
	PostQuote         Action = "post.quote"
	PostReply         Action = "post.reply"
	PostOpenMedia     Action = "post.open_media"
	PostAuthor        Action = "post.author"
	PostCompose       Action = "post.compose"
)

// Keymap maps actions to bindings.
type Keymap struct { /* action -> key.Binding */
}

// Default returns the CA 5.5 default bindings.
func Default() *Keymap {
	// TODO(core): Task 19
	return nil
}

// Load applies overrides; unknown action/invalid key => error, default kept.
func Load(overrides map[string][]string) (*Keymap, []error) {
	// TODO(core): Task 19
	return nil, nil
}

// Binding returns the binding for a.
func (k *Keymap) Binding(a Action) key.Binding {
	// TODO(core): Task 19
	return key.Binding{}
}

// Matches reports whether msg triggers a.
func (k *Keymap) Matches(msg tea.KeyPressMsg, a Action) bool {
	// TODO(core): Task 19
	return false
}

// Help returns the bindings for as, for help/status hints.
func (k *Keymap) Help(as ...Action) []key.Binding {
	// TODO(core): Task 19
	return nil
}
