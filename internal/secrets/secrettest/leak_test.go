package secrettest_test

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/secrets/secrettest"
)

// fakeTB records failures instead of failing the test.
type fakeTB struct {
	testing.TB
	errs []string
}

func (f *fakeTB) Helper() {}
func (f *fakeTB) Errorf(format string, a ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, a...))
}
func (f *fakeTB) Fatalf(format string, a ...any) {
	f.errs = append(f.errs, fmt.Sprintf(format, a...))
}

const leaked = "lc-sess-9f8e7d"

func send(t *testing.T, srv *apitest.Server, method, path string, hdr http.Header, body string) {
	t.Helper()
	req, err := http.NewRequest(method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, vs := range hdr {
		req.Header[k] = vs
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
}

func TestAssertNoLeakCatches(t *testing.T) {
	cases := map[string]func(t *testing.T, srv *apitest.Server, logDir string){
		"header": func(t *testing.T, srv *apitest.Server, _ string) {
			send(t, srv, "GET", "/v1/me", http.Header{"X-Leak": {"a " + leaked}}, "")
		},
		"cookie header": func(t *testing.T, srv *apitest.Server, _ string) {
			send(t, srv, "GET", "/v1/me", http.Header{"Cookie": {"LEETCODE_SESSION=" + leaked}}, "")
		},
		"body": func(t *testing.T, srv *apitest.Server, _ string) {
			send(t, srv, "POST", "/v1/messages", nil, `{"text":"`+leaked+`"}`)
		},
		"query": func(t *testing.T, srv *apitest.Server, _ string) {
			send(t, srv, "GET", "/v1/search?q="+url.QueryEscape(leaked+" x"), nil, "")
		},
		"path": func(t *testing.T, srv *apitest.Server, _ string) {
			send(t, srv, "GET", "/v1/users/"+leaked, nil, "")
		},
		"log file": func(t *testing.T, _ *apitest.Server, logDir string) {
			if err := os.WriteFile(filepath.Join(logDir, "masil.log"), []byte("put value="+leaked+"\n"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, leak := range cases {
		t.Run(name, func(t *testing.T) {
			srv := apitest.New(t)
			logDir := t.TempDir()
			send(t, srv, "GET", "/v1/clean", http.Header{"Authorization": {"Bearer msl_x"}}, `{"ok":true}`)
			leak(t, srv, logDir)
			ft := &fakeTB{TB: t}
			secrettest.AssertNoLeak(ft, srv, logDir, leaked)
			if len(ft.errs) == 0 {
				t.Fatal("the leak was not reported")
			}
			for _, e := range ft.errs {
				if strings.Contains(e, leaked) {
					t.Errorf("the report repeats the value: %s", e)
				}
			}
		})
	}
	t.Run("clean", func(t *testing.T) {
		srv := apitest.New(t)
		logDir := t.TempDir()
		send(t, srv, "POST", "/v1/messages", http.Header{"X-Other": {"zzz"}}, `{"text":"hello"}`)
		_ = os.WriteFile(filepath.Join(logDir, "masil.log"), []byte("nothing here\n"), 0o600)
		ft := &fakeTB{TB: t}
		secrettest.AssertNoLeak(ft, srv, logDir, leaked, "")
		if len(ft.errs) != 0 {
			t.Fatalf("clean traffic reported: %v", ft.errs)
		}
	})
}

// TestAssertNoLeakFrames: WebSocket frames (realtime, room documents) are
// checked too (A9.5) — raw and JSON-escaped; reports name the frame index only.
func TestAssertNoLeakFrames(t *testing.T) {
	frames := [][]byte{
		[]byte(`{"type":"typing","channel_id":"C1"}`),
		[]byte(`{"type":"doc.update","text":"a<` + leaked + `"}`),
	}
	ft := &fakeTB{}
	secrettest.AssertNoLeakFrames(ft, frames, "unused", leaked)
	if len(ft.errs) != 1 || !strings.Contains(ft.errs[0], "frame 1") || !strings.Contains(ft.errs[0], "value #1") {
		t.Fatalf("errs = %q, want one report naming frame 1 and value #1", ft.errs)
	}
	for _, e := range ft.errs {
		if strings.Contains(e, leaked) {
			t.Fatalf("the report repeats the value: %q", e)
		}
	}
	quoted := `he said "` + leaked + `"`
	ft = &fakeTB{}
	secrettest.AssertNoLeakFrames(ft, [][]byte{[]byte(`{"t":"he said \"` + leaked + `\""}`)}, quoted)
	if len(ft.errs) != 1 {
		t.Fatalf("a JSON-escaped value in a frame was missed: %q", ft.errs)
	}
	ft = &fakeTB{}
	secrettest.AssertNoLeakFrames(ft, frames[:1], leaked)
	if len(ft.errs) != 0 {
		t.Fatalf("clean frames reported: %q", ft.errs)
	}
}
