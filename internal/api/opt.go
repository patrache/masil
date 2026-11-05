package api

import (
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// PageOpts selects a cursor page.
type PageOpts struct {
	Cursor string // "" = first page
	Limit  int    // 0 = server default
}

// values encodes cursor/limit (omitted when zero) into q (allocated if nil).
func (p PageOpts) values(q url.Values) url.Values {
	if q == nil {
		q = url.Values{}
	}
	if p.Cursor != "" {
		q.Set("cursor", p.Cursor)
	}
	if p.Limit > 0 {
		q.Set("limit", strconv.Itoa(p.Limit))
	}
	return q
}

// Page is a cursor page of items.
type Page[T any] struct {
	Items      []T     `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// Done reports whether there is no next page.
func (p *Page[T]) Done() bool {
	return p == nil || p.NextCursor == nil
}

// Opt distinguishes "omit" from "send null" (api.md optional+nullable fields).
// Uses Go 1.24+ `omitzero`: fields are tagged `json:"x,omitzero"`.
type Opt[T any] struct {
	Set   bool
	Null  bool
	Value T
}

// Some returns a set Opt holding v.
func Some[T any](v T) Opt[T] {
	return Opt[T]{Set: true, Value: v}
}

// Null returns a set Opt that marshals to null.
func Null[T any]() Opt[T] {
	return Opt[T]{Set: true, Null: true}
}

// IsZero reports !Set (for omitzero).
func (o Opt[T]) IsZero() bool {
	return !o.Set
}

// MarshalJSON writes null or the value.
func (o Opt[T]) MarshalJSON() ([]byte, error) {
	if !o.Set || o.Null {
		return []byte("null"), nil
	}
	return json.Marshal(o.Value)
}

// UserRef is "{user}" (api.md 0.2): "@handle", "<ULID>", or "@me". Escaped per path segment.
type UserRef string

// MeRef is "@me", the authenticated user as a {user} path reference.
//
// CA 4.3.1 spells this constant `Me`, but CA 4.3.2 also declares `type Me struct`
// in the same package (used by store.Store.Me, ui.Context.Me, authui.LoggedInMsg);
// Go cannot declare both, so the constant is named MeRef.
const MeRef UserRef = "@me"

// Handle returns "@" + lowercase(h).
// A leading "@" in h is accepted and not doubled.
func Handle(h string) UserRef {
	return UserRef("@" + strings.ToLower(strings.TrimPrefix(h, "@")))
}

// UserID wraps a user ID.
func UserID(id string) UserRef {
	return UserRef(id)
}
