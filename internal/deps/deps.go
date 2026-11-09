//go:build tools

// Package deps pins every direct dependency so `go mod tidy` never drops
// a module that another plan's package will import later.
package deps

import (
	_ "charm.land/bubbles/v2/key"
	_ "charm.land/bubbles/v2/spinner"
	_ "charm.land/bubbles/v2/textarea"
	_ "charm.land/bubbles/v2/textinput"
	_ "charm.land/bubbles/v2/viewport"
	_ "charm.land/bubbletea/v2"
	_ "charm.land/lipgloss/v2"
	_ "git.sr.ht/~jackmordaunt/go-toast/wintoast"
	_ "github.com/BurntSushi/toml"
	_ "github.com/alecthomas/chroma/v2"
	_ "github.com/alecthomas/chroma/v2/lexers"
	_ "github.com/charmbracelet/x/ansi"
	_ "github.com/charmbracelet/x/ansi/iterm2"
	_ "github.com/charmbracelet/x/ansi/kitty"
	_ "github.com/charmbracelet/x/exp/golden"
	_ "github.com/charmbracelet/x/exp/teatest/v2"
	_ "github.com/charmbracelet/x/vt"
	_ "github.com/charmbracelet/x/xpty"
	_ "github.com/coder/websocket"
	_ "github.com/godbus/dbus/v5"
	_ "github.com/gofrs/flock"
	_ "github.com/ionut-t/goeditor"
	_ "github.com/oklog/ulid/v2"
	_ "github.com/sahilm/fuzzy"
	_ "go.yaml.in/yaml/v3"
	_ "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
	_ "golang.org/x/sys/windows/registry"
	_ "golang.org/x/text/language"
	_ "golang.org/x/text/unicode/norm"
)
