package authui_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/api/apitest"
	"github.com/patrache/masil/internal/auth"
	"github.com/patrache/masil/internal/textwidth"
	"github.com/patrache/masil/internal/ui"
	"github.com/patrache/masil/internal/ui/authui"
	"github.com/patrache/masil/internal/ui/layout"
	"github.com/patrache/masil/internal/ui/uitest"
	"github.com/patrache/masil/internal/ui/uitest/fixtures"
	"github.com/patrache/masil/internal/ui/uitest/stub"
)

func TestMain(m *testing.M) {
	stub.Install() // home/channel stand-ins for the shell shown after login
	os.Exit(m.Run())
}

// goldenServer is the fixed server URL shown in goldens (the apitest port is random).
const goldenServer = "https://masil.example"

// loggedOut is a logged-out harness (the login screen) on its own apitest server.
func loggedOut(t *testing.T, opts ...uitest.Option) *uitest.Harness {
	t.Helper()
	return uitest.New(t, append([]uitest.Option{uitest.WithCredentials(nil)}, opts...)...)
}

// authResult is a canned signup/login response for the fixture user sam.
func authResult(token string) api.AuthResult {
	return api.AuthResult{
		Token:   token,
		Session: api.Session{ID: "01JSESSION0000000000000001", Client: "tui"},
		User:    api.Me{UserSummary: api.UserSummary{ID: fixtures.UserSam, Handle: "sam", DisplayName: "샘"}},
	}
}

// lastBody decodes the body of the last recorded request to path.
func lastBody(t *testing.T, srv *apitest.Server, method, path string) map[string]any {
	t.Helper()
	var out map[string]any
	for _, r := range srv.Requests() {
		if r.Method == method && r.Path == path {
			out = nil
			if err := json.Unmarshal(r.Body, &out); err != nil {
				t.Fatalf("decode %s %s body: %v", method, path, err)
			}
		}
	}
	if out == nil {
		t.Fatalf("no %s %s request", method, path)
	}
	return out
}

// count counts the recorded requests to method path.
func count(srv *apitest.Server, method, path string) int {
	n := 0
	for _, r := range srv.Requests() {
		if r.Method == method && r.Path == path {
			n++
		}
	}
	return n
}

// rows returns the screen rows above the status line.
func bodyRows(h *uitest.Harness) []string {
	lines := strings.Split(h.Screen(), "\n")
	return lines[:len(lines)-1]
}

func bodyContains(h *uitest.Harness, s string) bool {
	return strings.Contains(strings.Join(bodyRows(h), "\n"), s)
}

func hostname() string {
	n, err := os.Hostname()
	if err != nil {
		return ""
	}
	return n
}

func TestLoginGolden(t *testing.T) {
	for _, mode := range []string{"login", "signup"} {
		t.Run(mode, func(t *testing.T) {
			uitest.ForEachSize(t, uitest.AllSizes(), func(t *testing.T, h *uitest.Harness) {
				h.Context().API = api.New(goldenServer) // deterministic server line
				h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteLogin, Arg: mode}})
				h.Golden(mode)
			})
		})
	}
}

func TestLoginScreenOnStart(t *testing.T) {
	h := loggedOut(t)
	for _, sz := range uitest.AllSizes() {
		h.Resize(sz.W, sz.H)
		h.Check()
		if h.Model.Frame().Layout.Mode == layout.ModeTooSmall {
			continue
		}
		if !strings.Contains(h.Screen(), "아이디") || !strings.Contains(h.Screen(), "비밀번호") {
			t.Fatalf("%dx%d: login fields missing:\n%s", sz.W, sz.H, h.Screen())
		}
	}
}

func TestLoginSuccess(t *testing.T) {
	h := loggedOut(t)
	h.API.JSON("POST /v1/auth/login", http.StatusOK, authResult("msl_new"))
	h.Type("Sam")
	h.Press("tab")
	h.Type("correct horse")
	h.Press("enter")

	body := lastBody(t, h.API, "POST", "/v1/auth/login")
	want := map[string]any{"handle": "sam", "password": "correct horse", "client": "tui"}
	if hn := hostname(); hn != "" {
		want["device_name"] = hn
	}
	if fmt.Sprint(body) != fmt.Sprint(want) {
		t.Fatalf("login body = %v, want %v", body, want)
	}

	p := h.Context().Paths
	st, err := os.Stat(p.Credentials)
	if err != nil {
		t.Fatalf("credentials.json not saved: %v", err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("credentials.json mode = %v, want 0600", st.Mode().Perm())
	}
	cr, err := auth.Load(p)
	if err != nil {
		t.Fatal(err)
	}
	if cr.Server != h.API.URL || cr.Token != "msl_new" || cr.UserID != fixtures.UserSam || cr.Handle != "sam" ||
		cr.Version != 1 || cr.SessionID != "01JSESSION0000000000000001" || !cr.SavedAt.Equal(uitest.Start) {
		t.Errorf("saved credentials = %+v", *cr)
	}
	if got := h.Context().API.Token(); got != "msl_new" {
		t.Errorf("API token not set after login")
	}
	if me := h.Context().Store.Me; me == nil || me.ID != fixtures.UserSam {
		t.Errorf("Store.Me = %+v", me)
	}
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteHome {
		t.Errorf("Main shows %+v after login, want home", r)
	}
	if strings.Contains(h.Screen(), "터미널 마실 — 로그인") {
		t.Errorf("login screen still shown:\n%s", h.Screen())
	}
	h.Check()
}

func TestLoginErrors(t *testing.T) {
	t.Run("invalid_credentials", func(t *testing.T) {
		h := loggedOut(t)
		h.API.Error("POST /v1/auth/login", http.StatusUnauthorized, "invalid_credentials")
		h.Type("sam")
		h.Press("tab")
		h.Type("wrong password")
		h.Press("enter")
		if !bodyContains(h, "아이디 또는 비밀번호가 맞지 않아요") {
			t.Fatalf("error missing:\n%s", h.Screen())
		}
		if strings.Contains(h.Screen(), "다른 곳에서 로그아웃됐어요") {
			t.Fatalf("a failed login is not a logout:\n%s", h.Screen())
		}
		if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteLogin {
			t.Fatalf("Main = %+v, want the login screen", r)
		}
		// the password is cleared and focused: typing goes into it again
		h.Type("correct horse")
		h.API.JSON("POST /v1/auth/login", http.StatusOK, authResult("msl_new"))
		h.Press("enter")
		if b := lastBody(t, h.API, "POST", "/v1/auth/login"); b["password"] != "correct horse" || b["handle"] != "sam" {
			t.Fatalf("retry body = %v (password not cleared/focused?)", b)
		}
		if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind == ui.RouteLogin {
			t.Fatalf("retry did not log in")
		}
	})
	t.Run("rate_limited", func(t *testing.T) {
		h := loggedOut(t)
		h.API.Handle("POST /v1/auth/login", func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "30")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":{"code":"rate_limited","message":"slow down","details":{}}}`))
		})
		h.Type("sam")
		h.Press("tab")
		h.Type("correct horse")
		h.Press("enter")
		if !bodyContains(h, "너무 빨라요. 30초 뒤에 다시 해 주세요") {
			t.Fatalf("rate limit error missing:\n%s", h.Screen())
		}
	})
	t.Run("network", func(t *testing.T) {
		h := loggedOut(t)
		h.API.Handle("POST /v1/auth/login", func(w http.ResponseWriter, _ *http.Request) {
			hj, ok := w.(http.Hijacker)
			if !ok {
				panic("no hijacker")
			}
			conn, _, err := hj.Hijack()
			if err == nil {
				conn.Close()
			}
		})
		h.Type("sam")
		h.Press("tab")
		h.Type("correct horse")
		h.Press("enter")
		if !bodyContains(h, "서버에 연결할 수 없어요") {
			t.Fatalf("network error missing:\n%s", h.Screen())
		}
		for _, sz := range uitest.Required {
			h.Resize(sz.W, sz.H)
			h.Check()
		}
	})
}

func TestLoginCtrlC(t *testing.T) {
	h := loggedOut(t)
	h.Type("sam")
	h.Press("ctrl+c")
	if strings.Contains(h.Screen(), "한 번 더 누르면 종료해요") {
		t.Fatalf("ctrl+c on a non-empty field must only clear it:\n%s", h.Screen())
	}
	h.API.Error("POST /v1/auth/login", http.StatusUnauthorized, "invalid_credentials")
	h.Type("abc")
	h.Press("tab")
	h.Type("password1")
	h.Press("enter")
	if b := lastBody(t, h.API, "POST", "/v1/auth/login"); b["handle"] != "abc" {
		t.Fatalf("handle = %v, want the field cleared by ctrl+c then retyped", b["handle"])
	}
	// password was cleared by the failure; the focused password field is empty now
	h.Press("ctrl+c")
	if !strings.Contains(h.Screen(), "한 번 더 누르면 종료해요") {
		t.Fatalf("ctrl+c on an empty field must arm quitting:\n%s", h.Screen())
	}
	for _, m := range h.TeaMsgs() {
		if _, ok := m.(tea.QuitMsg); ok {
			t.Fatal("one ctrl+c must not quit")
		}
	}
}

func TestClientValidation(t *testing.T) {
	t.Run("handle", func(t *testing.T) {
		h := loggedOut(t)
		h.Type("ab")
		h.Press("tab")
		h.Type("correct horse")
		h.Press("enter")
		if !bodyContains(h, "아이디는 영문 소문자·숫자·_ 3~20자예요") {
			t.Fatalf("handle validation missing:\n%s", h.Screen())
		}
		if n := count(h.API, "POST", "/v1/auth/login"); n != 0 {
			t.Fatalf("%d login requests, want none", n)
		}
	})
	t.Run("password", func(t *testing.T) {
		h := loggedOut(t)
		h.Type("sam")
		h.Press("tab")
		h.Type("short")
		h.Press("enter")
		if !bodyContains(h, "비밀번호는 8자 이상이에요") {
			t.Fatalf("password validation missing:\n%s", h.Screen())
		}
		if n := count(h.API, "POST", "/v1/auth/login"); n != 0 {
			t.Fatalf("%d login requests, want none", n)
		}
	})
	t.Run("confirm", func(t *testing.T) {
		h := loggedOut(t)
		h.Send(ui.NavigateMsg{Route: ui.Route{Kind: ui.RouteLogin, Arg: "signup"}})
		h.Type("minji")
		h.Press("tab", "tab") // display name stays empty
		h.Type("correct horse")
		h.Press("tab")
		h.Type("correct house")
		h.Press("enter")
		if !bodyContains(h, "비밀번호가 서로 달라요") {
			t.Fatalf("confirm validation missing:\n%s", h.Screen())
		}
		if n := count(h.API, "POST", "/v1/auth/signup"); n != 0 {
			t.Fatalf("%d signup requests, want none", n)
		}
	})
}

func TestSignupFlow(t *testing.T) {
	h := loggedOut(t)
	h.Press("shift+tab", "enter") // the switch link (focus wraps from the first field)
	if !strings.Contains(h.Screen(), "터미널 마실 — 가입") || !strings.Contains(h.Screen(), "비밀번호 확인") {
		t.Fatalf("signup screen not shown:\n%s", h.Screen())
	}
	h.API.Error("POST /v1/auth/signup", http.StatusConflict, "handle_taken")
	h.Type("minji")
	h.Press("tab")
	h.Type("민지 ❄️")
	h.Press("tab")
	h.Type("correct horse")
	h.Press("tab")
	h.Type("correct horse")
	h.Press("enter")
	if !bodyContains(h, "이미 쓰는 아이디예요") {
		t.Fatalf("handle_taken missing:\n%s", h.Screen())
	}
	b := lastBody(t, h.API, "POST", "/v1/auth/signup")
	if b["handle"] != "minji" || b["display_name"] != "민지 ❄" || b["password"] != "correct horse" || b["client"] != "tui" {
		t.Fatalf("signup body = %v", b)
	}

	h.API.JSON("POST /v1/auth/signup", http.StatusCreated, authResult("msl_signed"))
	h.Press("tab", "tab", "tab", "tab") // handle -> display -> password -> confirm -> button
	h.Press("enter")
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteHome {
		t.Fatalf("Main = %+v after signup, want home\n%s", r, h.Screen())
	}
	if cr, err := auth.Load(h.Context().Paths); err != nil || cr.Token != "msl_signed" {
		t.Fatalf("credentials after signup: %+v, %v", cr, err)
	}
}

func TestPasswordNotSanitizedOrShown(t *testing.T) {
	const pw = "❄️pässwörd"
	h := loggedOut(t)
	h.API.Error("POST /v1/auth/login", http.StatusUnauthorized, "invalid_credentials")
	h.Type("sam")
	h.Press("tab")
	h.Type(pw)
	for _, sz := range uitest.AllSizes() {
		h.Resize(sz.W, sz.H)
		h.Check()
		for _, frag := range []string{pw, "pässwörd", "ässw", "❄"} {
			if strings.Contains(h.Styled(), frag) {
				t.Fatalf("%dx%d: the password (%q) is drawn:\n%s", sz.W, sz.H, frag, h.Screen())
			}
		}
	}
	h.Resize(120, 40)
	if !strings.Contains(h.Screen(), strings.Repeat("*", len([]rune(pw)))) {
		t.Fatalf("mask missing:\n%s", h.Screen())
	}
	h.Press("enter")
	var raw []byte
	for _, r := range h.API.Requests() {
		if r.Path == "/v1/auth/login" {
			raw = r.Body
		}
	}
	var body struct{ Password string }
	if err := json.Unmarshal(raw, &body); err != nil {
		t.Fatal(err)
	}
	if body.Password != pw {
		t.Fatalf("password sent %q (% x), want byte-exact %q", body.Password, body.Password, pw)
	}
	if strings.Contains(h.Styled(), "pässwörd") {
		t.Fatal("password drawn after a failed login")
	}
}

func TestServerLineShown(t *testing.T) {
	h := loggedOut(t)
	if !strings.Contains(h.Screen(), "서버: "+h.API.URL) {
		t.Fatalf("server line missing:\n%s", h.Screen())
	}
	h.Resize(24, 20)
	h.Check()
	var line string
	for _, l := range bodyRows(h) {
		if strings.Contains(l, "서버: ") {
			line = l
		}
	}
	if !strings.Contains(line, "서버: http") || !strings.Contains(line, "…") {
		t.Fatalf("narrow server line = %q, want it truncated with …\n%s", line, h.Screen())
	}
}

func TestUnauthorizedNotice(t *testing.T) {
	h := uitest.New(t)
	h.API.Error("GET /v1/me", http.StatusUnauthorized, "unauthorized")
	if _, err := h.Context().API.GetMe(context.Background()); api.CodeOf(err) != "unauthorized" {
		t.Fatalf("GetMe err = %v", err)
	}
	h.Send(tea.FocusMsg{}) // the next Update takes the 401 flag => logout
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteLogin {
		t.Fatalf("Main = %+v, want the login screen", r)
	}
	if !bodyContains(h, "다른 곳에서 로그아웃됐어요. 다시 로그인해 주세요") {
		t.Fatalf("the login screen must show the notice:\n%s", h.Screen())
	}
	for _, sz := range uitest.Required {
		h.Resize(sz.W, sz.H)
		h.Check()
	}
}

func TestLoginMouse(t *testing.T) {
	h := loggedOut(t)
	y, x := -1, -1
	for i, l := range bodyRows(h) {
		if j := strings.Index(l, "계정이 없나요?"); j >= 0 {
			y, x = i, textwidth.Width(l[:j])
		}
	}
	if y < 0 {
		t.Fatalf("link not found:\n%s", h.Screen())
	}
	h.Click(x+2, y)
	if !strings.Contains(h.Screen(), "터미널 마실 — 가입") {
		t.Fatalf("clicking the link must switch to signup:\n%s", h.Screen())
	}
	h.Check()
}

func TestLoggedInMsgWithoutCredentialsIgnored(t *testing.T) {
	h := loggedOut(t)
	h.Send(authui.LoggedInMsg{})
	if r := h.Model.Frame().Routes[layout.SlotMain]; r.Kind != ui.RouteLogin {
		t.Fatalf("Main = %+v, want the login screen", r)
	}
}
