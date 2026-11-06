package notify

import (
	"context"
	"encoding/xml"
	"slices"
	"strings"
)

// The builders below are pure and compiled on every OS so the tests can check
// each backend's argv/document for every GOOS from any machine.

// darwinOsascript is used by absolute path so $PATH cannot swap it.
const darwinOsascript = "/usr/bin/osascript"

// darwinShowStmt is the only statement that touches the text, and it reads it
// from argv: item 1 = title, item 2 = body.
const darwinShowStmt = "display notification (item 2 of argv) with title (item 1 of argv)"

// darwinCommand is the osascript invocation for n. The program is constant;
// title and body follow "--" so even "-e" is an argument, never an option.
func darwinCommand(n OSNotification) (name string, args []string) {
	return darwinOsascript, []string{
		"-e", "on run argv",
		"-e", darwinShowStmt,
		"-e", "end run",
		"--", n.Title, n.Body,
	}
}

// toastXML is the WinRT toast document for n. Title and body are XML-escaped
// character data (encoding/xml.EscapeText: & < > ' " and line breaks become
// entities, characters XML cannot hold become U+FFFD), never CDATA, so no
// element or attribute (e.g. a protocol <action>) can be injected.
func toastXML(n OSNotification) string {
	var b strings.Builder
	b.WriteString(`<toast duration="short"><visual><binding template="ToastGeneric">`)
	writeText(&b, n.Title)
	if n.Body != "" {
		writeText(&b, n.Body)
	}
	b.WriteString(`</binding></visual><audio silent="true"/></toast>`)
	return b.String()
}

func writeText(b *strings.Builder, s string) {
	b.WriteString("<text>")
	_ = xml.EscapeText(b, []byte(s)) // strings.Builder never fails
	b.WriteString("</text>")
}

// dbusNotify is one org.freedesktop.Notifications.Notify call
// (susssasa{sv}i; hints are always empty).
type dbusNotify struct {
	AppName    string
	ReplacesID uint32
	Icon       string
	Summary    string
	Body       string
	Actions    []string
	Timeout    int32
}

// dbusCall builds the Notify call for n. The body is markup only on servers
// that list "body-markup", and then & < > are escaped so no <a href> or
// <img src> can be injected. The summary is plain text by spec, but some
// servers render it as markup anyway, and entity-escaping it would show
// "&amp;" on the ones that follow the spec; so its angle brackets become
// look-alike quotes (summaryText), which read the same on both kinds and leave
// no '<' to start a tag (entities alone only make characters). No actions, no
// icon.
func dbusCall(appName string, n OSNotification, bodyMarkup bool) dbusNotify {
	body := n.Body
	if bodyMarkup {
		body = escapeMarkup(body)
	}
	return dbusNotify{
		AppName: appName,
		Summary: summaryText(n.Title),
		Body:    body,
		Actions: []string{},
		Timeout: -1, // server default
	}
}

var (
	markupEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	summaryQuoter = strings.NewReplacer("<", "‹", ">", "›") // U+2039, U+203A
)

// summaryText makes a summary that no markup-rendering server can read as a tag.
func summaryText(s string) string {
	return summaryQuoter.Replace(s)
}

// escapeMarkup escapes the characters the freedesktop body-markup subset
// interprets.
func escapeMarkup(s string) string {
	return markupEscaper.Replace(s)
}

// dbusSender is the part of the notification server the Linux backend talks
// to (backend_dbus.go wraps a godbus object; tests use a fake).
type dbusSender interface {
	capabilities(ctx context.Context) ([]string, error)
	notify(ctx context.Context, c dbusNotify) error
}

// dbusShow asks the server for its capabilities on every notification, not
// once per connection: the notification server can be replaced while the bus
// connection lives, and a body escaped by a stale "no body-markup" answer
// would reach a markup server raw. Unknown capabilities mean escape.
func dbusShow(ctx context.Context, s dbusSender, appName string, n OSNotification) error {
	caps, err := s.capabilities(ctx)
	markup := err != nil || slices.Contains(caps, "body-markup")
	return s.notify(ctx, dbusCall(appName, n, markup))
}
