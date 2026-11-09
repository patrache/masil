package hooks

import (
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func post(t *testing.T, u, body string) (int, string) {
	t.Helper()
	resp, err := http.Post(u, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func nextEvent(t *testing.T, r *Receiver) (Event, bool) {
	t.Helper()
	select {
	case ev := <-r.Events():
		return ev, true
	case <-time.After(200 * time.Millisecond):
		return Event{}, false
	}
}

func TestReceiverObservesOnly(t *testing.T) {
	r, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	u := r.URL("t1")
	pu, err := url.Parse(u)
	if err != nil {
		t.Fatal(err)
	}
	if host, _, _ := net.SplitHostPort(pu.Host); host != "127.0.0.1" {
		t.Errorf("listener host = %q", host)
	}
	parts := strings.Split(pu.Path, "/")
	if len(parts) != 4 || parts[1] != "h" || len(parts[2]) != 64 || parts[3] != "t1" {
		t.Errorf("URL path = %q", pu.Path)
	}

	for body, want := range map[string]EventKind{
		`{"hook_event_name":"PermissionRequest","tool_name":"Bash"}`: EventAttention,
		`{"hook_event_name":"Notification"}`:                         EventAttention,
		`{"hook_event_name":"Stop"}`:                                 EventStop,
	} {
		code, resp := post(t, u, body)
		if code != 200 || resp != "" {
			t.Errorf("POST %s = %d %q, want 200 and empty body", body, code, resp)
		}
		ev, ok := nextEvent(t, r)
		if !ok || ev.Term != "t1" || ev.Kind != want {
			t.Errorf("event for %s = %+v, %v", body, ev, ok)
		}
	}

	if code, _ := post(t, u, `{"hook_event_name":"PreToolUse"}`); code != 200 {
		t.Errorf("unknown event code = %d", code)
	}
	if _, ok := nextEvent(t, r); ok {
		t.Errorf("unknown hook produced an event")
	}

	bad := strings.Replace(u, parts[2], strings.Repeat("0", 64), 1)
	if code, _ := post(t, bad, `{"hook_event_name":"Stop"}`); code != 404 {
		t.Errorf("wrong token code = %d, want 404", code)
	}
	if code, _ := post(t, r.URL(""), `{"hook_event_name":"Stop"}`); code != 404 {
		t.Errorf("empty term code = %d, want 404", code)
	}
	if _, ok := nextEvent(t, r); ok {
		t.Errorf("rejected request produced an event")
	}

	big := `{"hook_event_name":"Stop","pad":"` + strings.Repeat("x", 100<<10) + `"}`
	if code, _ := post(t, u, big); code != 200 {
		t.Errorf("large body code = %d", code)
	}
	nextEvent(t, r) // whatever happens, no panic and the server still answers
	if code, _ := post(t, u, `{"hook_event_name":"Stop"}`); code != 200 {
		t.Errorf("after large body code = %d", code)
	}
}

func TestReceiverDropsWhenFull(t *testing.T) {
	r, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	for i := 0; i < eventBuffer+10; i++ {
		if code, _ := post(t, r.URL("t"), `{"hook_event_name":"Stop"}`); code != 200 {
			t.Fatalf("POST %d = %d", i, code)
		}
	}
	if n := len(r.Events()); n != eventBuffer {
		t.Errorf("buffered = %d, want %d", n, eventBuffer)
	}
}

func TestReceiverTokensDiffer(t *testing.T) {
	a, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if strings.Split(a.URL("x"), "/")[4] == strings.Split(b.URL("x"), "/")[4] {
		t.Errorf("two receivers share a token")
	}
	if err := a.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	_ = a.Close()
}

// claude puts hook_event_name before the tool input, so a PermissionRequest
// for a large Write still raises the alert even though the body is cut at
// 64 KiB. Only the top-level key counts.
func TestReceiverLargeBodyNameFirst(t *testing.T) {
	r, err := Listen()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	u := r.URL("t1")
	pad := strings.Repeat("x", 100<<10)
	cases := map[string]bool{
		`{"session_id":"s","hook_event_name":"PermissionRequest","tool_name":"Write","tool_input":{"content":"` + pad + `"}}`: true,
		`{"session_id":"s","pad":{"a":[1,2,"` + strings.Repeat("y", 10<<10) + `"]},"hook_event_name":"Notification"}`:         true,
		`{"tool_input":{"hook_event_name":"Stop"},"x":1}`:                                                                     false,
		`{"pad":"` + pad + `","hook_event_name":"Stop"}`:                                                                      false,
		`["hook_event_name","Stop"]`: false,
	}
	for body, want := range cases {
		if code, _ := post(t, u, body); code != 200 {
			t.Errorf("code = %d", code)
		}
		ev, ok := nextEvent(t, r)
		if ok != want || (ok && ev.Kind != EventAttention) {
			t.Errorf("body %.80q: event %+v, %v; want event %v", body, ev, ok, want)
		}
	}
}
