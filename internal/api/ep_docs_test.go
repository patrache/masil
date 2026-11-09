package api_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
)

// docEndpoints is every 5.24 room-document method (P0 Task 30).
var docEndpoints = []string{
	"ListDocs", "CreateDoc", "GetDoc", "UpdateDoc", "DeleteDoc",
	"ListDocVersions", "CreateDocVersion", "GetDocVersion", "RestoreDocVersion",
	"ListDocComments", "CreateDocComment", "UpdateDocComment", "DeleteDocComment",
}

func docEndpointCases(t *testing.T) []epCase {
	docEnv := env(t, "doc", "Doc")
	verEnv := env(t, "version", "DocVersion")
	comEnv := env(t, "comment", "DocComment")
	title := "새 제목"
	locked := true
	root := "K1"
	body := "고침"
	resolved := true
	return []epCase{
		{name: "ListDocs", route: "GET /v1/spaces/{id}/docs", wantPath: "/v1/spaces/S/docs", resp: pageOf(t, "Doc"),
			call: func(c *api.Client) (any, error) {
				return c.ListDocs(ctx, "S", "arch", api.PageOpts{Cursor: "c1", Limit: 20})
			},
			wantQuery: "cursor=c1&limit=20&type=arch"},
		{name: "ListDocs/both", route: "GET /v1/spaces/{id}/docs", wantPath: "/v1/spaces/S/docs", resp: `{"items":[],"next_cursor":null}`,
			call: func(c *api.Client) (any, error) { return c.ListDocs(ctx, "S", "", api.PageOpts{}) },
			check: func(t *testing.T, got any) {
				p := got.(*api.Page[api.Doc])
				if p.Items == nil || len(p.Items) != 0 || !p.Done() {
					t.Errorf("page = %+v, want empty and done", p)
				}
			}},
		{name: "CreateDoc", route: "POST /v1/spaces/{id}/docs", wantPath: "/v1/spaces/S/docs", status: 201, resp: docEnv, limiter: "RL_DOC",
			call: func(c *api.Client) (any, error) {
				return c.CreateDoc(ctx, "S", api.CreateDocRequest{Type: "arch", Title: "결제", Snapshot: json.RawMessage(`{"seq":0,"objects":[]}`)})
			},
			wantBody: `{"type":"arch","title":"결제","snapshot":{"seq":0,"objects":[]}}`},
		{name: "CreateDoc/noSnapshot", route: "POST /v1/spaces/{id}/docs", wantPath: "/v1/spaces/S/docs", status: 201, resp: docEnv, limiter: "RL_DOC",
			call: func(c *api.Client) (any, error) {
				return c.CreateDoc(ctx, "S", api.CreateDocRequest{Type: "apicol", Title: "컬렉션"})
			},
			wantBody: `{"type":"apicol","title":"컬렉션"}`},
		{name: "GetDoc", route: "GET /v1/docs/{id}", wantPath: "/v1/docs/D", resp: docEnv,
			call: func(c *api.Client) (any, error) { return c.GetDoc(ctx, "D") }},
		{name: "UpdateDoc", route: "PATCH /v1/docs/{id}", wantPath: "/v1/docs/D", resp: docEnv, limiter: "RL_DOC",
			call: func(c *api.Client) (any, error) {
				return c.UpdateDoc(ctx, "D", api.UpdateDocRequest{Title: &title, Locked: &locked})
			},
			wantBody: `{"title":"새 제목","locked":true}`},
		{name: "UpdateDoc/titleOnly", route: "PATCH /v1/docs/{id}", wantPath: "/v1/docs/D", resp: docEnv, limiter: "RL_DOC",
			call:     func(c *api.Client) (any, error) { return c.UpdateDoc(ctx, "D", api.UpdateDocRequest{Title: &title}) },
			wantBody: `{"title":"새 제목"}`},
		{name: "DeleteDoc", route: "DELETE /v1/docs/{id}", wantPath: "/v1/docs/D", limiter: "RL_DOC",
			call: func(c *api.Client) (any, error) { return nil, c.DeleteDoc(ctx, "D") }},
		{name: "ListDocVersions", route: "GET /v1/docs/{id}/versions", wantPath: "/v1/docs/D/versions", resp: itemsOf(t, "DocVersion"),
			call: func(c *api.Client) (any, error) { return c.ListDocVersions(ctx, "D") }},
		{name: "CreateDocVersion", route: "POST /v1/docs/{id}/versions", wantPath: "/v1/docs/D/versions", status: 201, resp: verEnv, limiter: "RL_DOC",
			call:     func(c *api.Client) (any, error) { return c.CreateDocVersion(ctx, "D", "리뷰 전") },
			wantBody: `{"name":"리뷰 전"}`},
		{name: "GetDocVersion", route: "GET /v1/docs/{id}/versions/{no}", wantPath: "/v1/docs/D/versions/3", resp: sample(t, "DocVersionSnapshot"),
			call: func(c *api.Client) (any, error) { return c.GetDocVersion(ctx, "D", 3) }},
		{name: "RestoreDocVersion", route: "POST /v1/docs/{id}/versions/{no}/restore", wantPath: "/v1/docs/D/versions/3/restore", limiter: "RL_DOC",
			resp: `{"doc":` + sample(t, "Doc") + `,"version":` + sample(t, "DocVersion") + `}`,
			call: func(c *api.Client) (any, error) { return c.RestoreDocVersion(ctx, "D", 3) }},
		{name: "ListDocComments", route: "GET /v1/docs/{id}/comments", wantPath: "/v1/docs/D/comments", resp: pageOf(t, "DocComment"),
			call:      func(c *api.Client) (any, error) { return c.ListDocComments(ctx, "D", api.PageOpts{Limit: 50}) },
			wantQuery: "limit=50"},
		{name: "CreateDocComment", route: "POST /v1/docs/{id}/comments", wantPath: "/v1/docs/D/comments", status: 201, resp: comEnv, limiter: "RL_DOC",
			call: func(c *api.Client) (any, error) {
				return c.CreateDocComment(ctx, "D", api.CreateDocCommentRequest{Anchor: "node:n1", Body: "@hana 봐 주세요", RootID: &root})
			},
			wantBody: `{"anchor":"node:n1","body":"@hana 봐 주세요","root_id":"K1"}`},
		{name: "CreateDocComment/root", route: "POST /v1/docs/{id}/comments", wantPath: "/v1/docs/D/comments", status: 201, resp: comEnv, limiter: "RL_DOC",
			call: func(c *api.Client) (any, error) {
				return c.CreateDocComment(ctx, "D", api.CreateDocCommentRequest{Anchor: "a", Body: "b"})
			},
			wantBody: `{"anchor":"a","body":"b"}`},
		{name: "UpdateDocComment", route: "PATCH /v1/docs/{id}/comments/{cid}", wantPath: "/v1/docs/D/comments/K", resp: comEnv, limiter: "RL_DOC",
			call: func(c *api.Client) (any, error) {
				return c.UpdateDocComment(ctx, "D", "K", api.UpdateDocCommentRequest{Body: &body, Resolved: &resolved})
			},
			wantBody: `{"body":"고침","resolved":true}`},
		{name: "DeleteDocComment", route: "DELETE /v1/docs/{id}/comments/{cid}", wantPath: "/v1/docs/D/comments/K", limiter: "RL_DOC",
			call: func(c *api.Client) (any, error) { return nil, c.DeleteDocComment(ctx, "D", "K") }},
	}
}

// TestEndpointsWire: every room-document method's method, path, query, body
// and RL_DOC limiter (api.md 5.24 / 6).
func TestEndpointsWire(t *testing.T) {
	cases := docEndpointCases(t)
	checkCoverage(t, docEndpoints, cases)
	runCases(t, cases)
}

// TestGetDocVersionWire: the version and its raw snapshot both decode; a
// response without a snapshot is an error.
func TestGetDocVersionWire(t *testing.T) {
	s := apitest.New(t)
	s.JSON("GET /v1/docs/{id}/versions/{no}", 200, json.RawMessage(sample(t, "DocVersionSnapshot")))
	got, err := s.Client().GetDocVersion(ctx, "D", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version.No != 3 || got.Version.Seq != 40 || got.Version.Name == nil || *got.Version.Name != "리뷰 전" {
		t.Errorf("version = %+v", got.Version)
	}
	var snap struct {
		Seq     int64             `json:"seq"`
		Objects []json.RawMessage `json:"objects"`
	}
	if err := json.Unmarshal(got.Snapshot, &snap); err != nil || snap.Seq != 40 || len(snap.Objects) != 1 {
		t.Errorf("snapshot = %s (%v)", got.Snapshot, err)
	}

	s2 := apitest.New(t)
	s2.JSON("GET /v1/docs/{id}/versions/{no}", 200, json.RawMessage(`{"version":`+sample(t, "DocVersion")+`}`))
	if _, err := s2.Client().GetDocVersion(ctx, "D", 3); !api.IsCode(err, "internal_error") {
		t.Errorf("missing snapshot: err = %v, want internal_error", err)
	}
}

// TestRestoreReturnsVersion: RestoreDocVersion returns the new restore
// version (restored_from = the restored no), not the doc.
func TestRestoreReturnsVersion(t *testing.T) {
	s := apitest.New(t)
	s.Handle("POST /v1/docs/{id}/versions/{no}/restore", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Write([]byte(`{"doc":` + sample(t, "Doc") + `,"version":{"no":7,"seq":43,"name":null,"auto":false,"restored_from":3,"created_by":null,"created_at":"2026-11-05T02:07:00Z"}}`))
	})
	v, err := s.Client().RestoreDocVersion(ctx, "D", 3)
	if err != nil {
		t.Fatal(err)
	}
	if v.No != 7 || v.Seq != 43 || v.Name != nil || v.Auto || v.RestoredFrom == nil || *v.RestoredFrom != 3 {
		t.Errorf("restore version = %+v", v)
	}
}

// TestDocSocketURL: ws(s) scheme, the base path kept, the doc id escaped as
// one segment.
func TestDocSocketURL(t *testing.T) {
	for base, want := range map[string]string{
		"https://h":             "wss://h/v1/docs/D1/ws",
		"http://localhost:8787": "ws://localhost:8787/v1/docs/D1/ws",
		"https://h/api/":        "wss://h/api/v1/docs/D1/ws",
	} {
		if got := api.New(base).DocSocketURL("D1"); got != want {
			t.Errorf("DocSocketURL(%s) = %q, want %q", base, got, want)
		}
	}
	if got := api.New("https://h").DocSocketURL("a/b"); got != "wss://h/v1/docs/a%2Fb/ws" {
		t.Errorf("escaped = %q", got)
	}
	for _, bad := range []string{"", ".", ".."} {
		if got := api.New("https://h").DocSocketURL(bad); got != "" {
			t.Errorf("DocSocketURL(%q) = %q, want \"\"", bad, got)
		}
	}
}
