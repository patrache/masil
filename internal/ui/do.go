package ui

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
)

// ResultMsg carries an API result. Tag rule: "<plan>.<action>:<targetID>".
type ResultMsg[T any] struct {
	Tag   string
	Value T
	Err   error
}

// Do runs fn off the UI goroutine and returns ResultMsg[T]{Tag, Value, Err}.
// In the uitest harness the returned Cmd runs synchronously (apitest server).
// The client is captured when Do is called; fn runs with context.Background()
// (timeouts are the api client's defaults).
func Do[T any](c *Context, tag string, fn func(ctx context.Context, api *api.Client) (T, error)) tea.Cmd {
	var client *api.Client
	if c != nil {
		client = c.API
	}
	return func() tea.Msg {
		v, err := fn(context.Background(), client)
		return ResultMsg[T]{Tag: tag, Value: v, Err: err}
	}
}
