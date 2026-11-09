package api_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/patrache/masil/internal/api"
)

// typeSamples maps every wire type (CA 4.3.2 list plus the response bundles)
// to a constructor. Each has testdata/types/<name>.json with every field of
// api.md section 2 (or the endpoint response in section 4/5) filled in.
var typeSamples = map[string]func() any{
	// CA 4.3.2 types (api.md section 2).
	"UserStatus":          func() any { return new(api.UserStatus) },
	"UserSummary":         func() any { return new(api.UserSummary) },
	"Relationship":        func() any { return new(api.Relationship) },
	"FriendRequestRef":    func() any { return new(api.FriendRequestRef) },
	"Profile":             func() any { return new(api.Profile) },
	"Settings":            func() any { return new(api.Settings) },
	"NotifySettings":      func() any { return new(api.NotifySettings) },
	"Focus":               func() any { return new(api.Focus) },
	"PresenceState":       func() any { return new(api.PresenceState) },
	"Me":                  func() any { return new(api.Me) },
	"Session":             func() any { return new(api.Session) },
	"Media":               func() any { return new(api.Media) },
	"Upload":              func() any { return new(api.Upload) },
	"FriendRequest":       func() any { return new(api.FriendRequest) },
	"Friend":              func() any { return new(api.Friend) },
	"SpacePreview":        func() any { return new(api.SpacePreview) },
	"Space":               func() any { return new(api.Space) },
	"SpaceDetail":         func() any { return new(api.SpaceDetail) },
	"SpaceMember":         func() any { return new(api.SpaceMember) },
	"Invite":              func() any { return new(api.Invite) },
	"InvitePreview":       func() any { return new(api.InvitePreview) },
	"DirectInvite":        func() any { return new(api.DirectInvite) },
	"ChannelMyState":      func() any { return new(api.ChannelMyState) },
	"Channel":             func() any { return new(api.Channel) },
	"ChannelRef":          func() any { return new(api.ChannelRef) },
	"UnreadState":         func() any { return new(api.UnreadState) },
	"MentionRef":          func() any { return new(api.MentionRef) },
	"ReactionSummary":     func() any { return new(api.ReactionSummary) },
	"ThreadSummary":       func() any { return new(api.ThreadSummary) },
	"RollResult":          func() any { return new(api.RollResult) },
	"SystemEvent":         func() any { return new(api.SystemEvent) },
	"SystemEventData":     func() any { return new(api.SystemEventData) },
	"PollOption":          func() any { return new(api.PollOption) },
	"Poll":                func() any { return new(api.Poll) },
	"PollInput":           func() any { return new(api.PollInput) },
	"Message":             func() any { return new(api.Message) },
	"QuotedPost":          func() any { return new(api.QuotedPost) },
	"Post":                func() any { return new(api.Post) },
	"PostViewer":          func() any { return new(api.PostViewer) },
	"TimelineItem":        func() any { return new(api.TimelineItem) },
	"Story":               func() any { return new(api.Story) },
	"StoryGroup":          func() any { return new(api.StoryGroup) },
	"Notification":        func() any { return new(api.Notification) },
	"NotificationSubject": func() any { return new(api.NotificationSubject) },
	"NotificationContext": func() any { return new(api.NotificationContext) },
	"Alert":               func() any { return new(api.Alert) },
	"AlertTarget":         func() any { return new(api.AlertTarget) },
	"AlertTemplate":       func() any { return new(api.AlertTemplate) },
	"Card":                func() any { return new(api.Card) },
	"Doc":                 func() any { return new(api.Doc) },
	"DocVersion":          func() any { return new(api.DocVersion) },
	"DocComment":          func() any { return new(api.DocComment) },
	"DocVersionSnapshot":  func() any { return new(api.DocVersionSnapshot) },
	"FocusSummary":        func() any { return new(api.FocusSummary) },
	"FocusCounts":         func() any { return new(api.FocusCounts) },
	"GuestbookEntry":      func() any { return new(api.GuestbookEntry) },
	"SearchHighlight":     func() any { return new(api.SearchHighlight) },
	"AdminUserView":       func() any { return new(api.AdminUserView) },
	"Report":              func() any { return new(api.Report) },
	"AdminAction":         func() any { return new(api.AdminAction) },
	// Response bundles (CA 4.3.2). MediaBlob is not JSON and is excluded.
	"AuthResult":          func() any { return new(api.AuthResult) },
	"MetaInfo":            func() any { return new(api.MetaInfo) },
	"MetaLimits":          func() any { return new(api.MetaLimits) },
	"PresenceItem":        func() any { return new(api.PresenceItem) },
	"BlockEntry":          func() any { return new(api.BlockEntry) },
	"FollowRequestEntry":  func() any { return new(api.FollowRequestEntry) },
	"FriendRequestResult": func() any { return new(api.FriendRequestResult) },
	"ChannelMember":       func() any { return new(api.ChannelMember) },
	"SpaceBan":            func() any { return new(api.SpaceBan) },
	"MessageWindow":       func() any { return new(api.MessageWindow) },
	"ThreadWindow":        func() any { return new(api.ThreadWindow) },
	"MessageSearchHit":    func() any { return new(api.MessageSearchHit) },
	"PostSearchHit":       func() any { return new(api.PostSearchHit) },
	"HashtagSuggestion":   func() any { return new(api.HashtagSuggestion) },
	"TimelinePage":        func() any { return new(api.TimelinePage) },
	"PostContext":         func() any { return new(api.PostContext) },
	"LikeResult":          func() any { return new(api.LikeResult) },
	"RepostResult":        func() any { return new(api.RepostResult) },
	"Trend":               func() any { return new(api.Trend) },
	"Trends":              func() any { return new(api.Trends) },
	"StoryViewer":         func() any { return new(api.StoryViewer) },
	"NotificationPage":    func() any { return new(api.NotificationPage) },
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func strictDecode(raw []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}

// keyDiff compares two JSON documents structurally by object keys (values are
// not compared) and returns the paths whose keys differ. Arrays are compared
// element by element; a null on either side ends the descent.
func keyDiff(path string, a, b any) []string {
	switch av := a.(type) {
	case map[string]any:
		bv, ok := b.(map[string]any)
		if !ok {
			if b == nil {
				return []string{path + ": object became null"}
			}
			return []string{path + ": object became non-object"}
		}
		var out []string
		for k, x := range av {
			y, ok := bv[k]
			if !ok {
				out = append(out, path+"."+k+": missing after re-marshal")
				continue
			}
			out = append(out, keyDiff(path+"."+k, x, y)...)
		}
		for k := range bv {
			if _, ok := av[k]; !ok {
				out = append(out, path+"."+k+": extra key not in sample")
			}
		}
		return out
	case []any:
		bv, ok := b.([]any)
		if !ok {
			return []string{path + ": array became non-array"}
		}
		if len(av) != len(bv) {
			return []string{path + ": array length changed"}
		}
		var out []string
		for i := range av {
			out = append(out, keyDiff(path+"[]", av[i], bv[i])...)
		}
		return out
	}
	return nil
}

func sameKeysDeep(t *testing.T, raw, out []byte) []string {
	t.Helper()
	var a, b any
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatalf("sample is not JSON: %v", err)
	}
	if err := json.Unmarshal(out, &b); err != nil {
		t.Fatalf("re-marshal is not JSON: %v", err)
	}
	d := keyDiff("$", a, b)
	sort.Strings(d)
	return d
}

// Every response field always exists (api.md header), so a strict decode of a
// full sample proves no Go field is missing or misspelled, and the key-set
// comparison of the re-marshal proves no Go field has an extra/wrong tag.
func TestTypesStrictRoundTrip(t *testing.T) {
	for name, newV := range typeSamples {
		raw := mustRead(t, filepath.Join("testdata", "types", name+".json"))
		v := newV()
		if err := strictDecode(raw, v); err != nil {
			t.Errorf("%s: strict decode: %v", name, err)
			continue
		}
		out, err := json.Marshal(v)
		if err != nil {
			t.Errorf("%s: marshal: %v", name, err)
			continue
		}
		if d := sameKeysDeep(t, raw, out); len(d) > 0 {
			t.Errorf("%s: key set differs after re-marshal:\n  %s", name, strings.Join(d, "\n  "))
		}
	}
}

// Every sample file has an entry, and every entry has a non-empty struct type.
func TestTypesSampleCoverage(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("testdata", "types", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, f := range files {
		name := strings.TrimSuffix(filepath.Base(f), ".json")
		seen[name] = true
		if _, ok := typeSamples[name]; !ok {
			t.Errorf("sample %s has no typeSamples entry", name)
		}
	}
	for name, newV := range typeSamples {
		if !seen[name] {
			t.Errorf("type %s has no sample file", name)
		}
		rt := reflect.TypeOf(newV()).Elem()
		if rt.Kind() != reflect.Struct || rt.NumField() == 0 {
			t.Errorf("type %s is an empty struct", name)
		}
	}
}

// Every exported field of every wire type carries an explicit json tag
// (embedded structs excepted: they flatten).
func TestTypesAllFieldsTagged(t *testing.T) {
	for name, newV := range typeSamples {
		rt := reflect.TypeOf(newV()).Elem()
		for i := 0; i < rt.NumField(); i++ {
			f := rt.Field(i)
			if f.Anonymous || !f.IsExported() {
				continue
			}
			if _, ok := f.Tag.Lookup("json"); !ok {
				t.Errorf("%s.%s has no json tag", name, f.Name)
			}
		}
	}
}

func TestEventsExamplesDecode(t *testing.T) {
	// events.md 4.2 example frame.
	msgFrame := `{ "type": "message.created", "seq": 1042, "ts": "2026-11-05T02:05:00.120Z",
  "data": { "message": { "id": "01JC9Z...", "channel_id": "01JB...", "author": { "id": "01JA...", "handle": "sam", "display_name": "sam", "avatar_url": null, "status": null, "is_private": false, "suspended": false },
    "kind": "text", "body": "@minji 점심 먹고 ㄱㄱ", "mentions": [ { "user_id": "01JA2...", "handle": "minji" } ], "mention_here": false,
    "thread_root_id": null, "thread": null, "reactions": [], "pinned": false, "poll": null, "roll": null, "system": null,
    "client_nonce": "01JC9X...", "created_at": "2026-11-05T02:05:00.100Z", "edited_at": null, "deleted": false } } }`
	var mf struct {
		Data struct {
			Message json.RawMessage `json:"message"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(msgFrame), &mf); err != nil {
		t.Fatal(err)
	}
	var m api.Message
	if err := strictDecode(mf.Data.Message, &m); err != nil {
		t.Fatalf("message: %v", err)
	}
	if m.Author.Handle != "sam" || len(m.Mentions) != 1 || m.Mentions[0].Handle != "minji" || m.Mentions[0].UserID != "01JA2..." {
		t.Fatalf("message decoded wrong: %+v", m)
	}
	if m.ClientNonce == nil || *m.ClientNonce != "01JC9X..." || m.EditedAt != nil || m.Thread != nil || m.Reactions == nil {
		t.Fatalf("message nullable fields wrong: %+v", m)
	}
	if want := time.Date(2026, 11, 5, 2, 5, 0, 100e6, time.UTC); !m.CreatedAt.Equal(want) {
		t.Fatalf("created_at = %v, want %v", m.CreatedAt, want)
	}

	// events.md 4.7 example frame.
	alertFrame := `{ "type": "alert", "seq": 1043, "ts": "2026-11-05T02:05:00.140Z",
  "data": { "alert": { "id": "01JC9ZA...", "kind": "mention", "title": "sam · 노바 개발방 #개발", "body": "@minji 점심 먹고 ㄱㄱ",
    "actor": { "id": "01JA...", "handle": "sam", "display_name": "sam", "avatar_url": null, "status": null, "is_private": false, "suspended": false },
    "target": { "type": "message", "id": "01JC9Z...", "channel_id": "01JB...", "message_id": "01JC9Z...", "thread_root_id": null, "post_id": null, "space_id": "01JB0..." },
    "created_at": "2026-11-05T02:05:00.100Z", "suppressed": false, "suppressed_reason": null } } }`
	var af struct {
		Data struct {
			Alert json.RawMessage `json:"alert"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(alertFrame), &af); err != nil {
		t.Fatal(err)
	}
	var a api.Alert
	if err := strictDecode(af.Data.Alert, &a); err != nil {
		t.Fatalf("alert: %v", err)
	}
	if a.Kind != api.AlertMention || a.Actor == nil || a.Actor.Handle != "sam" || a.Title != "sam · 노바 개발방 #개발" {
		t.Fatalf("alert decoded wrong: %+v", a)
	}
	if a.Target.Type != "message" || a.Target.ChannelID == nil || *a.Target.ChannelID != "01JB..." ||
		a.Target.ThreadRootID != nil || a.Target.PostID != nil || a.Target.SpaceID == nil || *a.Target.SpaceID != "01JB0..." {
		t.Fatalf("alert target wrong: %+v", a.Target)
	}
	if a.Suppressed || a.SuppressedReason != nil {
		t.Fatalf("alert suppression wrong: %+v", a)
	}
}

func TestQuotedPostUnion(t *testing.T) {
	var ok api.QuotedPost
	if err := strictDecode(mustRead(t, "testdata/types/QuotedPost.json"), &ok); err != nil {
		t.Fatal(err)
	}
	if !ok.Available || ok.Post == nil || ok.Post.ID == "" || ok.ID != "" || ok.Reason != "" {
		t.Fatalf("available union wrong: %+v", ok)
	}

	var gone api.QuotedPost
	raw := `{"available":false,"id":"01JE0000000000000000000000","reason":"deleted"}`
	if err := strictDecode([]byte(raw), &gone); err != nil {
		t.Fatal(err)
	}
	if gone.Available || gone.Post != nil || gone.ID != "01JE0000000000000000000000" || gone.Reason != "deleted" {
		t.Fatalf("unavailable union wrong: %+v", gone)
	}
	out, _ := json.Marshal(gone)
	if d := sameKeysDeep(t, []byte(raw), out); len(d) > 0 {
		t.Fatalf("unavailable re-marshal keys: %v (%s)", d, out)
	}
	out, _ = json.Marshal(ok)
	var keys map[string]json.RawMessage
	_ = json.Unmarshal(out, &keys)
	if _, has := keys["id"]; has {
		t.Fatalf("available re-marshal leaks id: %s", out)
	}
	if _, has := keys["reason"]; has {
		t.Fatalf("available re-marshal leaks reason: %s", out)
	}
}

func TestNullableTimes(t *testing.T) {
	var m api.Message
	raw := `{"id":"M","created_at":"2026-11-05T02:03:04.567Z","edited_at":null}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if m.EditedAt != nil {
		t.Fatalf("edited_at null => %v, want nil", m.EditedAt)
	}
	if want := time.Date(2026, 11, 5, 2, 3, 4, 567e6, time.UTC); !m.CreatedAt.Equal(want) {
		t.Fatalf("created_at = %v, want %v", m.CreatedAt, want)
	}
	raw = `{"id":"M","created_at":"2026-11-05T02:03:04.567Z","edited_at":"2026-11-05T02:04:00.001Z"}`
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 11, 5, 2, 4, 0, 1e6, time.UTC); m.EditedAt == nil || !m.EditedAt.Equal(want) {
		t.Fatalf("edited_at = %v, want %v", m.EditedAt, want)
	}
	// A nil pointer marshals back as null (the field always exists on the wire).
	m.EditedAt = nil
	out, _ := json.Marshal(m)
	if !bytes.Contains(out, []byte(`"edited_at":null`)) {
		t.Fatalf("nil edited_at marshals as %s", out)
	}
}

func TestEmbeddedFlatten(t *testing.T) {
	cases := map[string]struct {
		v    any
		want []string // keys from the embedded type that must sit at top level
	}{
		"Profile":       {api.Profile{UserSummary: api.UserSummary{ID: "U", Handle: "minji"}}, []string{"id", "handle", "display_name", "bio"}},
		"Me":            {api.Me{UserSummary: api.UserSummary{ID: "U", Handle: "minji"}}, []string{"id", "handle", "is_private", "settings", "presence"}},
		"Space":         {api.Space{SpacePreview: api.SpacePreview{ID: "S", Name: "노바"}}, []string{"id", "name", "member_count", "my_role"}},
		"SpaceDetail":   {api.SpaceDetail{Space: api.Space{SpacePreview: api.SpacePreview{ID: "S"}}}, []string{"id", "name", "owner_id", "channels"}},
		"Upload":        {api.Upload{Media: api.Media{ID: "M", URL: "/v1/media/M"}}, []string{"id", "url", "content_type", "purpose"}},
		"AdminUserView": {api.AdminUserView{UserSummary: api.UserSummary{ID: "U"}}, []string{"id", "handle", "role", "open_report_count"}},
	}
	for name, c := range cases {
		out, err := json.Marshal(c.v)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(out, &keys); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, bad := range []string{"UserSummary", "SpacePreview", "Space", "Media"} {
			if _, has := keys[bad]; has {
				t.Errorf("%s: nested %q key in %s", name, bad, out)
			}
		}
		for _, k := range c.want {
			if _, has := keys[k]; !has {
				t.Errorf("%s: missing top-level %q in %s", name, k, out)
			}
		}
	}
}

func TestEnumConstants(t *testing.T) {
	pairs := [][2]string{
		{string(api.PresenceOnline), "online"}, {string(api.PresenceAway), "away"}, {string(api.PresenceDND), "dnd"}, {string(api.PresenceOffline), "offline"},
		{string(api.RoleOwner), "owner"}, {string(api.RoleAdmin), "admin"}, {string(api.RoleMember), "member"},
		{string(api.ChannelText), "text"}, {string(api.ChannelDM), "dm"}, {string(api.ChannelGroupDM), "group_dm"},
		{string(api.AlertDM), "dm"}, {string(api.AlertMention), "mention"}, {string(api.AlertThreadReply), "thread_reply"},
		{string(api.AlertInvite), "invite"}, {string(api.AlertFriendRequest), "friend_request"},
		{string(api.AlertChannelMessage), "channel_message"}, {string(api.AlertSocial), "social"}, {string(api.AlertFocusSummary), "focus_summary"},
	}
	for _, p := range pairs {
		if p[0] != p[1] {
			t.Errorf("constant %q, want %q", p[0], p[1])
		}
	}
	// Typed enum fields (plan decision).
	var f api.Friend
	var ps api.PresenceState
	var pi api.PresenceItem
	var sm api.SpaceMember
	var sp api.Space
	var ch api.Channel
	var cr api.ChannelRef
	var al api.Alert
	var n api.Notification
	checks := []struct {
		name string
		got  reflect.Type
		want reflect.Type
	}{
		{"Friend.Presence", reflect.TypeOf(f.Presence), reflect.TypeOf(api.PresenceOnline)},
		{"PresenceState.Status", reflect.TypeOf(ps.Status), reflect.TypeOf(api.PresenceOnline)},
		{"PresenceItem.Status", reflect.TypeOf(pi.Status), reflect.TypeOf(api.PresenceOnline)},
		{"SpaceMember.Role", reflect.TypeOf(sm.Role), reflect.TypeOf(api.RoleOwner)},
		{"Space.MyRole", reflect.TypeOf(sp.MyRole), reflect.TypeOf(api.RoleOwner)},
		{"Channel.Kind", reflect.TypeOf(ch.Kind), reflect.TypeOf(api.ChannelText)},
		{"ChannelRef.Kind", reflect.TypeOf(cr.Kind), reflect.TypeOf(api.ChannelText)},
		{"Alert.Kind", reflect.TypeOf(al.Kind), reflect.TypeOf(api.AlertDM)},
		{"Notification.Type", reflect.TypeOf(n.Type), reflect.TypeOf(api.NotificationType(""))},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s is %v, want %v", c.name, c.got, c.want)
		}
	}
}

func TestFriendRequestResultAutoAcceptedNotOnWire(t *testing.T) {
	out, _ := json.Marshal(api.FriendRequestResult{AutoAccepted: true})
	if bytes.Contains(out, []byte("AutoAccepted")) || bytes.Contains(out, []byte("auto_accepted")) {
		t.Fatalf("AutoAccepted leaked to JSON: %s", out)
	}
}
