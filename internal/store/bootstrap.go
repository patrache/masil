package store

import (
	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
)

// BootstrapMsg carries the events.md 5.3 step-1 REST results.
type BootstrapMsg struct {
	Me            *api.Me
	Spaces        []api.SpaceDetail
	DMs           []api.Channel
	Unreads       []api.UnreadState
	NotifUnread   int
	Friends       []api.Friend
	FriendReqIn   []api.FriendRequest
	DirectInvites []api.DirectInvite
	Blocks        []api.BlockEntry
	Err           error
}

// Bootstrap fetches core entities and emits BootstrapMsg.
func Bootstrap(c *api.Client) tea.Cmd {
	// TODO(core): Task 14
	return nil
}

// ApplyBootstrap replaces core entities, then batches every Resyncer.
func (s *Store) ApplyBootstrap(m BootstrapMsg) tea.Cmd {
	// TODO(core): Task 14
	return nil
}
