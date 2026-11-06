package autostart

import (
	"bufio"
	"bytes"
	"encoding/xml"
	"regexp"
	"strings"
)

// xmlText escapes s for use as XML character data (&, <, >, quotes, control chars).
func xmlText(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s)) // bytes.Buffer writes never fail
	return b.String()
}

// launchdPlist renders the LaunchAgent plist. configDir == "" => no EnvironmentVariables.
func launchdPlist(exe, configDir string) []byte {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + xmlText(exe) + `</string>
		<string>daemon</string>
		<string>run</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
`)
	if configDir != "" {
		b.WriteString(`	<key>EnvironmentVariables</key>
	<dict>
		<key>MASIL_CONFIG_DIR</key>
		<string>` + xmlText(configDir) + `</string>
	</dict>
`)
	}
	b.WriteString("</dict>\n</plist>\n")
	return []byte(b.String())
}

var plistExeRE = regexp.MustCompile(`(?s)<key>ProgramArguments</key>\s*<array>\s*<string>(.*?)</string>`)

// plistExe returns ProgramArguments[0], XML-unescaped; "" if absent.
func plistExe(content []byte) string {
	m := plistExeRE.FindSubmatch(content)
	if m == nil {
		return ""
	}
	var s string
	if err := xml.Unmarshal(append(append([]byte("<s>"), m[1]...), "</s>"...), &s); err != nil {
		return ""
	}
	return s
}

// systemdQuote quotes s as one systemd word: C-style escapes for \ and ", %% for specifiers,
// and (when dollar) $$ so ExecStart does not expand environment variables.
func systemdQuote(s string, dollar bool) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch {
		case r == '\\' || r == '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '%':
			b.WriteString("%%")
		case r == '$' && dollar:
			b.WriteString("$$")
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// systemdUnit renders the systemd --user unit. configDir == "" => no Environment line.
func systemdUnit(exe, configDir string) []byte {
	var b strings.Builder
	b.WriteString(`[Unit]
Description=masil notification daemon
Documentation=https://github.com/patrache/masil

[Service]
Type=simple
ExecStart=` + systemdQuote(exe, true) + ` daemon run
Restart=on-failure
RestartSec=10
`)
	if configDir != "" {
		// Environment= performs specifier expansion but no $ expansion.
		b.WriteString("Environment=" + systemdQuote("MASIL_CONFIG_DIR="+configDir, false) + "\n")
	}
	b.WriteString(`
[Install]
WantedBy=default.target
`)
	return []byte(b.String())
}

// unitExe returns the first ExecStart word, unquoted/unescaped; "" if absent.
func unitExe(content []byte) string {
	sc := bufio.NewScanner(bytes.NewReader(content))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		v, ok := strings.CutPrefix(line, "ExecStart=")
		if !ok {
			continue
		}
		return systemdFirstWord(strings.TrimSpace(v))
	}
	return ""
}

// systemdFirstWord undoes systemdQuote for the first word of v (quoted or bare).
func systemdFirstWord(v string) string {
	var b strings.Builder
	quoted := strings.HasPrefix(v, `"`)
	if quoted {
		v = v[1:]
	}
	for i := 0; i < len(v); i++ {
		c := v[i]
		switch {
		case quoted && c == '"':
			return b.String()
		case !quoted && (c == ' ' || c == '\t'):
			return b.String()
		case c == '\\' && i+1 < len(v):
			i++
			b.WriteByte(v[i])
		case (c == '%' || c == '$') && i+1 < len(v) && v[i+1] == c:
			i++
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// runValue renders the HKCU Run value: `"<exe>" daemon run`. Windows paths cannot contain '"'.
func runValue(exe string) string {
	return `"` + exe + `" daemon run`
}

// runValueExe returns the executable of a Run value (quoted or bare first word); "" if absent.
func runValueExe(v string) string {
	v = strings.TrimSpace(v)
	if rest, ok := strings.CutPrefix(v, `"`); ok {
		if i := strings.IndexByte(rest, '"'); i >= 0 {
			return rest[:i]
		}
		return rest
	}
	if i := strings.IndexAny(v, " \t"); i >= 0 {
		return v[:i]
	}
	return v
}
