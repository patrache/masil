package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Shared decoding helpers for the endpoint methods (ep_*.go).

// items is the {"items": [...]} envelope of unpaginated lists.
type items[T any] struct {
	Items []T `json:"items"`
}

// one performs r and decodes the single-object envelope {"<key>": T}.
// A JSON null value yields (nil, status, nil); a missing key is an
// internal_error (the server always sends the key, api.md header).
func one[T any](ctx context.Context, c *Client, r request, key string) (*T, int, error) {
	var env map[string]json.RawMessage
	st, err := c.call(ctx, r, &env)
	if err != nil {
		return nil, st, err
	}
	raw, ok := env[key]
	if !ok {
		return nil, st, decodeError(st, fmt.Sprintf("response has no %q key", key))
	}
	if string(raw) == "null" {
		return nil, st, nil
	}
	v := new(T)
	if err := json.Unmarshal(raw, v); err != nil {
		return nil, st, decodeError(st, "decode response "+key+": "+err.Error())
	}
	return v, st, nil
}

// list performs r and returns the items of {"items": [...]}.
func list[T any](ctx context.Context, c *Client, r request) ([]T, error) {
	var out items[T]
	if _, err := c.call(ctx, r, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []T{}
	}
	return out.Items, nil
}

// page performs r and returns the cursor page {"items", "next_cursor"}.
func page[T any](ctx context.Context, c *Client, r request) (*Page[T], error) {
	var out Page[T]
	if _, err := c.call(ctx, r, &out); err != nil {
		return nil, err
	}
	if out.Items == nil {
		out.Items = []T{}
	}
	return &out, nil
}

// decodeError is the *Error for a 2xx body that is not the expected shape.
func decodeError(status int, msg string) *Error {
	return &Error{Status: status, Code: "internal_error", Message: msg, Details: json.RawMessage(`{}`)}
}

// invalidRequest is a client-side 400 invalid_request (no request sent).
func invalidRequest(field, reason, msg string) *Error {
	d, _ := json.Marshal(map[string]any{"fields": map[string]string{field: reason}})
	return &Error{Status: http.StatusBadRequest, Code: "invalid_request", Message: msg, Details: d}
}

// maxIDList is the api.md 5.1/5.2 cap for ?user_ids= and ?ids= lists.
const maxIDList = 100

// idList encodes ids as one comma-joined query value, rejecting more than
// maxIDList locally.
func idList(field string, ids []string) (string, error) {
	if len(ids) > maxIDList {
		return "", invalidRequest(field, "invalid", fmt.Sprintf("%s: at most %d ids (got %d)", field, maxIDList, len(ids)))
	}
	return strings.Join(ids, ","), nil
}

// setIf sets q[k]=v when v is non-empty.
func setIf(q url.Values, k, v string) {
	if v != "" {
		q.Set(k, v)
	}
}

// setIntIf sets q[k]=n when n > 0.
func setIntIf(q url.Values, k string, n int) {
	if n > 0 {
		q.Set(k, strconv.Itoa(n))
	}
}

// userBody is {"user": "<ref>"}.
type userBody struct {
	User UserRef `json:"user"`
}

// usersBody is {"users": ["<ref>", ...]}.
type usersBody struct {
	Users []UserRef `json:"users"`
}
