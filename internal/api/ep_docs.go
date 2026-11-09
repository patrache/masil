package api

import (
	"context"
	"encoding/json"
	"net/url"
	"strconv"
	"strings"
)

// ---- 5.24 room documents (v0.1.3, docs/contract/collab.md) ----

// CreateDocRequest is POST /v1/spaces/{space_id}/docs. Snapshot is optional
// (nil = an empty document).
type CreateDocRequest struct {
	Type     string          `json:"type"` // arch|apicol
	Title    string          `json:"title"`
	Snapshot json.RawMessage `json:"snapshot,omitempty"`
}

// UpdateDocRequest is PATCH /v1/docs/{doc_id}; nil fields are left as they are.
type UpdateDocRequest struct {
	Title  *string `json:"title,omitempty"`
	Locked *bool   `json:"locked,omitempty"`
}

// CreateDocCommentRequest is POST /v1/docs/{doc_id}/comments.
type CreateDocCommentRequest struct {
	Anchor string  `json:"anchor"`
	Body   string  `json:"body"`
	RootID *string `json:"root_id,omitempty"`
}

// UpdateDocCommentRequest is PATCH /v1/docs/{doc_id}/comments/{comment_id}.
type UpdateDocCommentRequest struct {
	Body     *string `json:"body,omitempty"`
	Resolved *bool   `json:"resolved,omitempty"`
}

// ListDocs: GET /v1/spaces/{space_id}/docs?type= (typ "" = both kinds).
func (c *Client) ListDocs(ctx context.Context, spaceID, typ string, p PageOpts) (*Page[Doc], error) {
	q := url.Values{}
	if typ != "" {
		q.Set("type", typ)
	}
	return page[Doc](ctx, c, request{Method: "GET", Path: escPath("v1", "spaces", spaceID, "docs"), Query: p.values(q), Auth: true})
}

// CreateDoc: POST /v1/spaces/{space_id}/docs (RL_DOC).
func (c *Client) CreateDoc(ctx context.Context, spaceID string, r CreateDocRequest) (*Doc, error) {
	v, _, err := one[Doc](ctx, c, request{Method: "POST", Path: escPath("v1", "spaces", spaceID, "docs"), Body: r, Limiter: rlDoc, Auth: true}, "doc")
	return v, err
}

// GetDoc: GET /v1/docs/{doc_id}
func (c *Client) GetDoc(ctx context.Context, docID string) (*Doc, error) {
	v, _, err := one[Doc](ctx, c, request{Method: "GET", Path: escPath("v1", "docs", docID), Auth: true}, "doc")
	return v, err
}

// UpdateDoc: PATCH /v1/docs/{doc_id} {title?, locked?} (RL_DOC).
func (c *Client) UpdateDoc(ctx context.Context, docID string, r UpdateDocRequest) (*Doc, error) {
	v, _, err := one[Doc](ctx, c, request{Method: "PATCH", Path: escPath("v1", "docs", docID), Body: r, Limiter: rlDoc, Auth: true}, "doc")
	return v, err
}

// DeleteDoc: DELETE /v1/docs/{doc_id} (RL_DOC).
func (c *Client) DeleteDoc(ctx context.Context, docID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "docs", docID), Limiter: rlDoc, Auth: true}, nil)
	return err
}

// ListDocVersions: GET /v1/docs/{doc_id}/versions (newest first, no pages).
func (c *Client) ListDocVersions(ctx context.Context, docID string) ([]DocVersion, error) {
	return list[DocVersion](ctx, c, request{Method: "GET", Path: escPath("v1", "docs", docID, "versions"), Auth: true})
}

// CreateDocVersion: POST /v1/docs/{doc_id}/versions {name} (RL_DOC).
func (c *Client) CreateDocVersion(ctx context.Context, docID, name string) (*DocVersion, error) {
	body := struct {
		Name string `json:"name"`
	}{name}
	v, _, err := one[DocVersion](ctx, c, request{Method: "POST", Path: escPath("v1", "docs", docID, "versions"), Body: body, Limiter: rlDoc, Auth: true}, "version")
	return v, err
}

// GetDocVersion: GET /v1/docs/{doc_id}/versions/{no} → the version and its
// snapshot (raw; collab.DecodeSnapshot).
func (c *Client) GetDocVersion(ctx context.Context, docID string, no int) (*DocVersionSnapshot, error) {
	var out DocVersionSnapshot
	if _, err := c.call(ctx, request{Method: "GET", Path: escPath("v1", "docs", docID, "versions", strconv.Itoa(no)), Auth: true}, &out); err != nil {
		return nil, err
	}
	if len(out.Snapshot) == 0 || string(out.Snapshot) == "null" {
		return nil, decodeError(200, `response has no "snapshot"`)
	}
	return &out, nil
}

// RestoreDocVersion: POST /v1/docs/{doc_id}/versions/{no}/restore (RL_DOC).
// It returns the version the restore created (restored_from = no).
func (c *Client) RestoreDocVersion(ctx context.Context, docID string, no int) (*DocVersion, error) {
	v, _, err := one[DocVersion](ctx, c, request{Method: "POST", Path: escPath("v1", "docs", docID, "versions", strconv.Itoa(no), "restore"), Limiter: rlDoc, Auth: true}, "version")
	return v, err
}

// ListDocComments: GET /v1/docs/{doc_id}/comments (oldest first).
func (c *Client) ListDocComments(ctx context.Context, docID string, p PageOpts) (*Page[DocComment], error) {
	return page[DocComment](ctx, c, request{Method: "GET", Path: escPath("v1", "docs", docID, "comments"), Query: p.values(nil), Auth: true})
}

// CreateDocComment: POST /v1/docs/{doc_id}/comments (RL_DOC).
func (c *Client) CreateDocComment(ctx context.Context, docID string, r CreateDocCommentRequest) (*DocComment, error) {
	v, _, err := one[DocComment](ctx, c, request{Method: "POST", Path: escPath("v1", "docs", docID, "comments"), Body: r, Limiter: rlDoc, Auth: true}, "comment")
	return v, err
}

// UpdateDocComment: PATCH /v1/docs/{doc_id}/comments/{comment_id} (RL_DOC).
func (c *Client) UpdateDocComment(ctx context.Context, docID, commentID string, r UpdateDocCommentRequest) (*DocComment, error) {
	v, _, err := one[DocComment](ctx, c, request{Method: "PATCH", Path: escPath("v1", "docs", docID, "comments", commentID), Body: r, Limiter: rlDoc, Auth: true}, "comment")
	return v, err
}

// DeleteDocComment: DELETE /v1/docs/{doc_id}/comments/{comment_id} (RL_DOC).
func (c *Client) DeleteDocComment(ctx context.Context, docID, commentID string) error {
	_, err := c.call(ctx, request{Method: "DELETE", Path: escPath("v1", "docs", docID, "comments", commentID), Limiter: rlDoc, Auth: true}, nil)
	return err
}

// DocSocketURL returns ws(s)://.../v1/docs/<doc_id>/ws, the document socket
// (collab.md 2) that internal/collab dials with AuthHeader. The id is escaped
// as one segment; "", "." and ".." (which a URL normaliser would rewrite)
// give "".
func (c *Client) DocSocketURL(docID string) string {
	if docID == "" || docID == "." || docID == ".." {
		return ""
	}
	u, err := url.Parse(c.base)
	if err != nil {
		return ""
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/") + "/v1/docs/" + url.PathEscape(docID) + "/ws"
}
