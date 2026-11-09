package convert

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/apitool"
	"github.com/patrache/masil/internal/secrets"
	"github.com/patrache/masil/internal/secrets/secrettest"
)

type rq = apitool.Request

var noFollow = apitool.Settings{NoFollow: true}

const formCT = "application/x-www-form-urlencoded"

// curlCases are the 20 paste cases of C3.15 acceptance 2.
var curlCases = []struct {
	name string
	cmd  string
	want rq
}{
	{"1 plain", `curl https://x/a`,
		rq{Method: "GET", URL: "https://x/a", Settings: noFollow}},
	{"2 -X", `curl -X POST https://x/a`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow}},
	{"3 -XPOST", `curl -XPOST https://x/a`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow}},
	{"4 headers", `curl https://x/a -H 'Accept: application/json' -H "X-Two:2" --header 'X-Empty;'`,
		rq{Method: "GET", URL: "https://x/a", Settings: noFollow, Headers: []apitool.KV{
			{Key: "Accept", Value: "application/json"}, {Key: "X-Two", Value: "2"}, {Key: "X-Empty"}}}},
	{"5 json", `curl https://x/a -d '{"a":1}'`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow, Body: apitool.Body{Kind: apitool.BodyJSON, Text: `{"a":1}`}}},
	{"6 data-raw", `curl https://x/a --data-raw '@not-a-file'`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow, Body: apitool.Body{Kind: apitool.BodyRaw, Text: "@not-a-file", ContentType: formCT}}},
	{"7 data-binary", `curl https://x/a --data-binary 'x'`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow, Body: apitool.Body{Kind: apitool.BodyRaw, Text: "x", ContentType: formCT}}},
	{"8 two -d", `curl https://x/a -d a=1 -d b=2`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow, Body: apitool.Body{Kind: apitool.BodyURLEncoded, Form: []apitool.FormField{
			{KV: apitool.KV{Key: "a", Value: "1"}}, {KV: apitool.KV{Key: "b", Value: "2"}}}}}},
	{"9 data-urlencode", `curl https://x/a --data-urlencode 'q=a b&c'`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow, Body: apitool.Body{Kind: apitool.BodyURLEncoded, Form: []apitool.FormField{
			{KV: apitool.KV{Key: "q", Value: "a b&c"}}}}}},
	{"10 -F text", `curl https://x/a -F name=v`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow, Body: apitool.Body{Kind: apitool.BodyMultipart, Form: []apitool.FormField{
			{KV: apitool.KV{Key: "name", Value: "v"}}}}}},
	{"11 -F file", `curl https://x/a -F file=@/tmp/a.png -F 'doc=@/tmp/b.bin;filename=c.txt'`,
		rq{Method: "POST", URL: "https://x/a", Settings: noFollow, Body: apitool.Body{Kind: apitool.BodyMultipart, Form: []apitool.FormField{
			{KV: apitool.KV{Key: "file"}, File: "/tmp/a.png", FileName: "a.png"},
			{KV: apitool.KV{Key: "doc"}, File: "/tmp/b.bin", FileName: "c.txt"}}}}},
	{"12 -u", `curl -u user:pass https://x/a`,
		rq{Method: "GET", URL: "https://x/a", Settings: noFollow, Auth: apitool.Auth{Kind: apitool.AuthBasic, User: "user", Pass: "pass"}}},
	{"13 -G", `curl -G -d q=1 --data 'r=x%20y' https://x/a`,
		rq{Method: "GET", URL: "https://x/a", Settings: noFollow, Query: []apitool.KV{{Key: "q", Value: "1"}, {Key: "r", Value: "x y"}}}},
	{"14 -k", `curl -k https://x/a`,
		rq{Method: "GET", URL: "https://x/a", Settings: apitool.Settings{NoFollow: true, Insecure: true}}},
	{"15 -L", `curl --location https://x/a`,
		rq{Method: "GET", URL: "https://x/a"}},
	{"16 --url", `curl --url https://x/a`,
		rq{Method: "GET", URL: "https://x/a", Settings: noFollow}},
	{"17 --compressed", `curl --compressed https://x/a`,
		rq{Method: "GET", URL: "https://x/a", Settings: noFollow}},
	{"18 -b", `curl -b 'a=1; b=2' https://x/a`,
		rq{Method: "GET", URL: "https://x/a", Settings: noFollow, Headers: []apitool.KV{{Key: "Cookie", Value: "a=1; b=2"}}}},
	{"19 quotes", `curl "https://x/a" -H "X-Q: say \"hi\" \$HOME $HOME \\ \n" -H 'X-S: it'"'"'s'`,
		rq{Method: "GET", URL: "https://x/a", Settings: noFollow, Headers: []apitool.KV{
			{Key: "X-Q", Value: `say "hi" $HOME $HOME \ \n`}, {Key: "X-S", Value: "it's"}}}},
	{"20 continuation", "curl -X PUT \\\n  'https://x/a' \\\n  -H 'A: 1' \\\r\n  --data-raw '{\"k\":\n\"v\"}'",
		rq{Method: "PUT", URL: "https://x/a", Settings: noFollow, Headers: []apitool.KV{{Key: "A", Value: "1"}},
			Body: apitool.Body{Kind: apitool.BodyJSON, Text: "{\"k\":\n\"v\"}"}}},
}

func noID(r rq) rq {
	r.ID = ""
	return r
}

func TestParseCurlCases(t *testing.T) {
	if len(curlCases) != 20 {
		t.Fatalf("%d cases", len(curlCases))
	}
	for _, c := range curlCases {
		t.Run(c.name, func(t *testing.T) {
			got, rep, err := ParseCurl(c.cmd)
			if err != nil {
				t.Fatal(err)
			}
			if got.ID == "" {
				t.Fatal("no request id")
			}
			if !reflect.DeepEqual(noID(*got), c.want) {
				t.Fatalf("got  %+v\nwant %+v", noID(*got), c.want)
			}
			if len(rep.Ignored) != 0 {
				t.Fatalf("ignored = %v", rep.Ignored)
			}
		})
	}
}

func TestParseCurlURLQuery(t *testing.T) {
	got, _, err := ParseCurl(`curl 'https://x/a?b=2&c=x%20y&{{k}}={{v}}'`)
	if err != nil {
		t.Fatal(err)
	}
	if got.URL != "https://x/a" || !reflect.DeepEqual(got.Query, []apitool.KV{{Key: "b", Value: "2"}, {Key: "c", Value: "x y"}, {Key: "{{k}}", Value: "{{v}}"}}) {
		t.Fatalf("got %q %+v", got.URL, got.Query)
	}
	// A query that does not decode stays in the URL.
	got, _, _ = ParseCurl(`curl 'https://x/a?b=%zz'`)
	if got.URL != "https://x/a?b=%zz" || got.Query != nil {
		t.Fatalf("got %q %+v", got.URL, got.Query)
	}
}

func TestParseCurlIgnoredFlags(t *testing.T) {
	got, rep, err := ParseCurl(`curl --retry 3 -s -o out -d @file https://x/a --data-urlencode name@f -F 'x=<f' -b jar.txt https://y/b`)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--retry 3", "-s", "-o out", "-d @file", "--data-urlencode name@f", "-F x=<f", "-b jar.txt", "https://y/b"}
	if !slices.Equal(rep.Ignored, want) {
		t.Fatalf("ignored = %q", rep.Ignored)
	}
	if got.URL != "https://x/a" || got.Method != "GET" || got.Body.Kind != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestParseCurlErrors(t *testing.T) {
	for _, cmd := range []string{"", "wget https://x", "curl", "curl -H 'A: 1'", `curl 'https://x`, `curl "https://x`, `curl -X`} {
		if _, _, err := ParseCurl(cmd); err == nil {
			t.Errorf("ParseCurl(%q) ok", cmd)
		}
	}
	if _, _, err := ParseCurl("curl https://x/" + strings.Repeat("a", MaxImportBytes)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("big = %v", err)
	}
}

func TestParseCurlANSIQuote(t *testing.T) {
	got, _, err := ParseCurl(`curl https://x/a --data-raw $'{"a":"it\'s\n\x41"}' -H $'X: \t1'`)
	if err != nil {
		t.Fatal(err)
	}
	if got.Body.Text != "{\"a\":\"it's\nA\"}" || got.Headers[0].Value != "1" {
		t.Fatalf("got %q %+v", got.Body.Text, got.Headers)
	}
}

func TestParseCurlSuggestsSecrets(t *testing.T) {
	jwt := "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	_, rep, err := ParseCurl(`curl https://x/a -H 'Accept: */*' -H 'Authorization: Bearer ` + jwt + `' -H 'X-Ok: {{token}}'`)
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Suggestions) != 1 {
		t.Fatalf("suggestions = %+v", rep.Suggestions)
	}
	s := rep.Suggestions[0]
	if s.Where != "header:Authorization" || s.Finding.Rule != "jwt" {
		t.Fatalf("suggestion = %+v", s)
	}
	_, rep, _ = ParseCurl(`curl 'https://x/a?api_key=Zx81kQ0pLm2Vn7Rt5Yb4Wc9' -u me:` + jwt)
	var where []string
	for _, s := range rep.Suggestions {
		where = append(where, s.Where)
	}
	if !slices.Equal(where, []string{"query:api_key", "auth:pass"}) {
		t.Fatalf("where = %v", where)
	}
}

func TestCurlRoundTrip(t *testing.T) {
	for _, c := range curlCases {
		t.Run(c.name, func(t *testing.T) {
			orig, _, err := ParseCurl(c.cmd)
			if err != nil {
				t.Fatal(err)
			}
			out, err := ToCurl(orig, nil, CurlTemplate, apitool.Dyn{})
			if err != nil {
				t.Fatal(err)
			}
			back, _, err := ParseCurl(out)
			if err != nil {
				t.Fatalf("%v\n%s", err, out)
			}
			// Exports hide typed-in credentials (C3.10 layer 4).
			want := apitool.RedactRequest(orig, nil)
			if !reflect.DeepEqual(noID(*back), noID(want)) {
				t.Fatalf("out %s\ngot  %+v\nwant %+v", out, noID(*back), noID(want))
			}
		})
	}
	tmpl := &rq{Method: "PATCH", URL: "{{base_url}}/v1/{{id}}",
		Query:    []apitool.KV{{Key: "a b", Value: "c&d=e+f%"}, {Key: "off", Value: "1", Off: true}},
		Headers:  []apitool.KV{{Key: "Authorization", Value: "Bearer {{token}}"}, {Key: "X-It", Value: "it's {{x}}"}},
		Body:     apitool.Body{Kind: apitool.BodyURLEncoded, Form: []apitool.FormField{{KV: apitool.KV{Key: "k", Value: "{{v}} & ü"}}}},
		Settings: apitool.Settings{Insecure: true, MaxRedirects: 3, TimeoutMS: 2500}}
	out, err := ToCurl(tmpl, nil, CurlTemplate, apitool.Dyn{})
	if err != nil {
		t.Fatal(err)
	}
	back, _, err := ParseCurl(out)
	if err != nil {
		t.Fatal(err)
	}
	want := *tmpl
	want.Query = want.Query[:1]
	if !reflect.DeepEqual(noID(*back), want) {
		t.Fatalf("out %s\ngot  %+v\nwant %+v", out, noID(*back), want)
	}
	if !strings.Contains(out, "{{token}}") {
		t.Fatalf("placeholder lost: %s", out)
	}
}

func TestToCurlAuthHelpers(t *testing.T) {
	r := &rq{Method: "GET", URL: "https://x/a", Auth: apitool.Auth{Kind: apitool.AuthAPIKey, Name: "api_key", Value: "{{k}}", In: "query"}}
	out, _ := ToCurl(r, nil, CurlTemplate, apitool.Dyn{})
	if !strings.Contains(out, "'https://x/a?api_key={{k}}'") {
		t.Fatalf("apikey query: %s", out)
	}
	r.Auth = apitool.Auth{Kind: apitool.AuthBearer, Token: "{{token}}"}
	out, _ = ToCurl(r, nil, CurlTemplate, apitool.Dyn{})
	if !strings.Contains(out, "-H 'Authorization: Bearer {{token}}'") {
		t.Fatalf("bearer: %s", out)
	}
	r.Auth = apitool.Auth{Kind: apitool.AuthBearer, Token: "literal-token-value"}
	out, _ = ToCurl(r, nil, CurlTemplate, apitool.Dyn{})
	if strings.Contains(out, "literal-token-value") || !strings.Contains(out, apitool.Mask) {
		t.Fatalf("literal bearer not masked: %s", out)
	}
}

func TestToCurlNonSecretOnly(t *testing.T) {
	vault := apitool.NewVault(context.Background(), secrettest.NewStore(t, true), "masil.test", "u1")
	if err := vault.Put(apitool.SecretKey("", "e1", "token"), secrets.New("sv-token-1")); err != nil {
		t.Fatal(err)
	}
	s := &apitool.Scope{Vault: vault, Env: &apitool.Env{ID: "e1", Vars: []apitool.Var{
		{Name: "base_url", Value: "https://api.test"},
		{Name: "token", Secret: true, AllowedHosts: []string{"api.test"}},
		{Name: "plain_key", Value: "plain-key-value"},
	}}}
	r := &rq{Method: "GET", URL: "{{base_url}}/v1?u={{undefined}}",
		Headers: []apitool.KV{{Key: "Authorization", Value: "Bearer {{token}}"}, {Key: "X-Api-Key", Value: "{{plain_key}}"}, {Key: "X-Id", Value: "{{$uuid}}"}}}
	out, err := ToCurl(r, s, CurlNonSecret, apitool.Dyn{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "https://api.test/v1?u={{undefined}}") || !strings.Contains(out, "Bearer {{token}}") {
		t.Fatalf("out = %s", out)
	}
	if strings.Contains(out, "sv-token-1") || strings.Contains(out, "plain-key-value") || !strings.Contains(out, "X-Api-Key: "+apitool.Mask) {
		t.Fatalf("leak or no mask: %s", out)
	}
	if strings.Contains(out, "$uuid") {
		t.Fatalf("dynamic variable not expanded: %s", out)
	}
	tmpl, _ := ToCurl(r, s, CurlTemplate, apitool.Dyn{})
	if !strings.Contains(tmpl, "{{base_url}}/v1") {
		t.Fatalf("template mode expanded: %s", tmpl)
	}
}

func TestToCurlShellQuoting(t *testing.T) {
	r := &rq{Method: "POST", URL: "https://x/a", Headers: []apitool.KV{{Key: "X-S", Value: "it's"}},
		Body: apitool.Body{Kind: apitool.BodyRaw, Text: "a'b\n$c `d`", ContentType: "text/plain"}}
	out, err := ToCurl(r, nil, CurlTemplate, apitool.Dyn{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `'X-S: it'\''s'`) {
		t.Fatalf("quoting: %s", out)
	}
	back, _, err := ParseCurl(out)
	if err != nil {
		t.Fatal(err)
	}
	if back.Body.Text != r.Body.Text || back.Headers[0].Value != "it's" || back.Headers[1].Key != "Content-Type" || back.Headers[1].Value != "text/plain" {
		t.Fatalf("back = %+v", back)
	}
}

func FuzzParseCurl(f *testing.F) {
	for _, c := range curlCases {
		f.Add(c.cmd)
	}
	f.Add(`curl $'\x' "\`)
	f.Add("curl -")
	f.Add("curl --")
	f.Fuzz(func(t *testing.T, cmd string) {
		r, _, err := ParseCurl(cmd)
		if err != nil {
			return
		}
		if _, err := ToCurl(r, nil, CurlTemplate, apitool.Dyn{}); err != nil {
			t.Fatalf("ToCurl after parse: %v", err)
		}
	})
}

// A secret variable's value typed in literally anywhere is masked in the
// export (C3.10 layer 4), in both modes.
func TestToCurlMasksScopeSecretValues(t *testing.T) {
	vault := apitool.NewVault(context.Background(), secrettest.NewStore(t, true), "masil.test", "u1")
	if err := vault.Put(apitool.SecretKey("", "e1", "token"), secrets.New("sk-live-abc123xyz")); err != nil {
		t.Fatal(err)
	}
	s := &apitool.Scope{Vault: vault, Env: &apitool.Env{ID: "e1", Vars: []apitool.Var{
		{Name: "token", Secret: true, AllowedHosts: []string{"api.test"}},
	}}}
	r := &rq{Method: "POST", URL: "https://api.test/v1",
		Headers: []apitool.KV{{Key: "X-Trace", Value: "sk-live-abc123xyz"}},
		Body:    apitool.Body{Kind: apitool.BodyJSON, Text: `{"k":"sk-live-abc123xyz"}`}}
	for _, mode := range []CurlMode{CurlTemplate, CurlNonSecret} {
		out, err := ToCurl(r, s, mode, apitool.Dyn{})
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, "sk-live") || strings.Count(out, apitool.Mask) != 2 {
			t.Fatalf("mode %d: secret value not masked: %s", mode, out)
		}
	}
}

// A URL that starts with '-' is not read by curl as an option.
func TestToCurlURLNotAnOption(t *testing.T) {
	r := &rq{Method: "GET", URL: "-o/Users/me/.zshrc"}
	out, err := ToCurl(r, nil, CurlTemplate, apitool.Dyn{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "--url -o/Users/me/.zshrc") {
		t.Fatalf("url passed positionally: %s", out)
	}
	back, _, err := ParseCurl(out)
	if err != nil || back.URL != r.URL {
		t.Fatalf("back = %+v %v", back, err)
	}
}
