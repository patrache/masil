package notify

import (
	"encoding/xml"
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

// dbusCall builds the Notify call for n. The summary is plain text by spec; the
// body is markup only on servers that list "body-markup", and then & < > are
// escaped so no <a href> or <img src> can be injected. No actions, no icon.
func dbusCall(appName string, n OSNotification, bodyMarkup bool) dbusNotify {
	body := n.Body
	if bodyMarkup {
		body = escapeMarkup(body)
	}
	return dbusNotify{
		AppName: appName,
		Summary: n.Title,
		Body:    body,
		Actions: []string{},
		Timeout: -1, // server default
	}
}

var markupEscaper = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")

// escapeMarkup escapes the characters the freedesktop body-markup subset
// interprets.
func escapeMarkup(s string) string {
	return markupEscaper.Replace(s)
}
