package notify_test

import (
	"encoding/json"
	"maps"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/i18n"
	"github.com/patrache/masil/internal/notify"
)

// alertTemplatesFile is the contract registry the server renders from.
const alertTemplatesFile = "../../docs/contract/i18n/alert-templates.json"

type templateDef struct {
	Params []string `json:"params"`
	Body   string   `json:"body"`
}

func loadTemplates(t *testing.T) map[string]templateDef {
	t.Helper()
	raw, err := os.ReadFile(alertTemplatesFile)
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version   int                    `json:"version"`
		Templates map[string]templateDef `json:"templates"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Version != 1 || len(doc.Templates) == 0 {
		t.Fatalf("alert-templates.json: version %d, %d templates", doc.Version, len(doc.Templates))
	}
	return doc.Templates
}

var placeholderRe = regexp.MustCompile(`\{([a-z][a-z0-9_]*)(\|[^{}]*)?\}`)

// usedParams is the set of {name} placeholders in every form of every language.
func usedParams(e i18n.Entry) (perLang map[i18n.Locale][]string, union []string) {
	perLang = map[i18n.Locale][]string{}
	all := map[string]bool{}
	for l, f := range e.Text {
		set := map[string]bool{}
		for _, s := range []string{f.One, f.Other, f.Short} {
			for _, m := range placeholderRe.FindAllStringSubmatch(s, -1) {
				set[m[1]] = true
				all[m[1]] = true
			}
		}
		perLang[l] = slices.Sorted(maps.Keys(set))
	}
	return perLang, slices.Sorted(maps.Keys(all))
}

// Every contract template has a catalog title (and body for body "template");
// each language uses a subset of the declared params and together they use all
// of them (the server rule — ko doc.comment_mention leaves {space} out).
func TestAlertTemplatesCatalogCoverage(t *testing.T) {
	entries := map[string]i18n.Entry{}
	for _, e := range i18n.Entries() {
		if e.Area == "alerts" {
			entries[e.Key] = e
		}
	}
	for id, def := range loadTemplates(t) {
		base := strings.ReplaceAll(id, ".", "_")
		keys := []string{base + "_title"}
		if def.Body == "template" {
			keys = append(keys, base+"_body")
		} else if _, ok := entries[base+"_body"]; ok {
			t.Errorf("%s: body %q but the catalog has %s_body", id, def.Body, base)
		}
		want := slices.Sorted(slices.Values(def.Params))
		// title and body together use the params (focus.summary splits them).
		unionAll := map[string]bool{}
		for _, key := range keys {
			e, ok := entries[key]
			if !ok {
				t.Errorf("%s: catalog lacks alerts.%s", id, key)
				continue
			}
			for _, l := range []i18n.Locale{i18n.Ko, i18n.En} {
				if _, ok := e.Text[l]; !ok {
					t.Errorf("alerts.%s: no %s text", key, l)
				}
			}
			per, union := usedParams(e)
			for l, used := range per {
				for _, p := range used {
					if !slices.Contains(want, p) {
						t.Errorf("alerts.%s %s uses {%s}, not a param of %s %v", key, l, p, id, want)
					}
				}
			}
			for _, p := range union {
				unionAll[p] = true
			}
		}
		if got := slices.Sorted(maps.Keys(unionAll)); !slices.Equal(got, want) {
			t.Errorf("%s: catalog languages use %v, contract params %v", id, got, want)
		}
	}
}

func follow(actor string) api.Alert {
	return api.Alert{
		ID: "A1", Kind: api.AlertSocial, Title: "server title", Body: "",
		Template: &api.AlertTemplate{ID: "social.follow", Params: map[string]any{"actor": actor}},
	}
}

func TestRenderKnownTemplate(t *testing.T) {
	title, body := notify.Render(follow("하나"), i18n.KoT())
	if title != "하나님이 팔로우했어요" || body != "" {
		t.Fatalf("Render = %q, %q", title, body)
	}
	title, _ = notify.Render(follow("\x1b[31m하나"), i18n.KoT())
	if title != "하나님이 팔로우했어요" {
		t.Fatalf("escape not stripped: %q", title)
	}
	// en catalog (MASIL_LANG / P5) renders the en wording.
	if title, _ := notify.Render(follow("하나"), i18n.For(i18n.En)); title != "하나 followed you" {
		t.Fatalf("en Render = %q", title)
	}
	// nil catalog = ko.
	if title, _ := notify.Render(follow("하나"), nil); title != "하나님이 팔로우했어요" {
		t.Fatalf("nil catalog Render = %q", title)
	}
}

func TestRenderVerbatimBody(t *testing.T) {
	a := api.Alert{
		Title: "x", Body: "안녕\x1b[1m하세요",
		Template: &api.AlertTemplate{ID: "message.channel", Params: map[string]any{"actor": "민지", "space": "마실", "channel": "일반"}},
	}
	title, body := notify.Render(a, i18n.KoT())
	if title != "민지 · 마실 #일반" || body != "안녕하세요" {
		t.Fatalf("Render = %q, %q", title, body)
	}
}

func TestRenderUnknownTemplateFallsBack(t *testing.T) {
	a := api.Alert{Title: "서버 제목", Body: "서버 본문", Template: &api.AlertTemplate{ID: "future.thing"}}
	if title, body := notify.Render(a, i18n.KoT()); title != "서버 제목" || body != "서버 본문" {
		t.Fatalf("unknown id: %q, %q", title, body)
	}
	a.Template = nil
	if title, body := notify.Render(a, i18n.KoT()); title != "서버 제목" || body != "서버 본문" {
		t.Fatalf("nil template: %q, %q", title, body)
	}
}

func TestRenderFocusSummaryBody(t *testing.T) {
	// JSON numbers arrive as float64.
	var tmpl api.AlertTemplate
	if err := json.Unmarshal([]byte(`{"id":"focus.summary","params":{"mention":2,"dm":1,"thread_reply":3,"invite":0,"friend_request":4,"other":5}}`), &tmpl); err != nil {
		t.Fatal(err)
	}
	a := api.Alert{Kind: api.AlertFocusSummary, Title: "s", Body: "b", Template: &tmpl}
	title, body := notify.Render(a, i18n.KoT())
	if title != "집중 모드 끝 — 멘션 2, DM 1" {
		t.Fatalf("title = %q", title)
	}
	if body != "스레드 답글 3 · 초대 0 · 친구 요청 4 · 기타 5" {
		t.Fatalf("body = %q", body)
	}
}

// Format renders through the catalog and keeps the length limits.
func TestFormatUsesTemplate(t *testing.T) {
	n := notify.Format(follow("하나"), i18n.KoT())
	if n.Title != "하나님이 팔로우했어요" || n.AlertID != "A1" {
		t.Fatalf("Format = %+v", n)
	}
	long := follow(strings.Repeat("가", 100))
	if got := []rune(notify.Format(long, nil).Title); len(got) != 80 || got[79] != '…' {
		t.Fatalf("long title = %d runes", len(got))
	}
}

// The wire shape: "template" decodes into Alert.Template, null leaves it nil.
func TestAlertTemplateWire(t *testing.T) {
	var a api.Alert
	if err := json.Unmarshal([]byte(`{"id":"A","template":{"id":"social.follow","params":{"actor":"하나"}},"target":{"type":"doc","id":"D","doc_type":"arch"}}`), &a); err != nil {
		t.Fatal(err)
	}
	if a.Template == nil || a.Template.ID != "social.follow" || a.Template.Params["actor"] != "하나" || a.Target.DocType != "arch" {
		t.Fatalf("decoded %+v / %+v", a.Template, a.Target)
	}
	var old api.Alert
	if err := json.Unmarshal([]byte(`{"id":"A","template":null,"target":{"type":"post","id":"P"}}`), &old); err != nil {
		t.Fatal(err)
	}
	if old.Template != nil {
		t.Fatalf("null template = %+v", old.Template)
	}
	b, _ := json.Marshal(old.Target)
	if strings.Contains(string(b), "doc_type") {
		t.Fatalf("doc_type must be omitted when empty: %s", b)
	}
}
