package ui

import tea "charm.land/bubbletea/v2"

// Poster delivers msgs from any goroutine to Update (Context.Post): the only way
// a goroutine reaches the screen (CA 5.3.2). It never blocks the caller and keeps
// FIFO order; past its limit the oldest plain post is dropped (logged).
type Poster interface {
	// Post queues msg for Update.
	Post(msg tea.Msg)
	// Coalesce queues msg under key, replacing a not-yet-delivered msg of the same
	// key in place (a burst of "dirty" signals becomes one, carrying the latest).
	Coalesce(key string, msg tea.Msg)
}

// DiscardPoster drops every msg (a Context without an app, e.g. unit tests).
type DiscardPoster struct{}

func (DiscardPoster) Post(tea.Msg)             {}
func (DiscardPoster) Coalesce(string, tea.Msg) {}
