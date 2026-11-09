package i18n

import (
	"os"
	"strings"
)

// Source names where a Resolution came from.
type Source string

const (
	SourceEnv     Source = "env"     // MASIL_LANG (skips the gate)
	SourceConfig  Source = "config"  // config.toml [ui] language
	SourceLegacy  Source = "legacy"  // existing-user protection (i18n spec 6.2.1)
	SourceOS      Source = "os"      // LC_ALL/LC_MESSAGES/LANG or the OS UI language
	SourceDefault Source = "default" // nothing matched => en
)

// Inputs are the raw facts Resolve decides from.
type Inputs struct {
	Env, Config             string // MASIL_LANG, [ui] language ("auto" or "" = not chosen)
	Legacy                  bool   // device used by 0.1.3 or earlier and language not chosen
	LCAll, LCMessages, Lang string
	Preferred               []string // OS UI languages, most preferred first
}

// Resolution is the requested language and the one actually shown.
type Resolution struct {
	Requested, Effective Locale
	Source               Source
}

// Resolve applies the i18n spec 6.2 order: MASIL_LANG > config > legacy > OS
// (LC_ALL > LC_MESSAGES > LANG, then the OS UI language when those are unset
// or "C"/"POSIX") > en. Every source but MASIL_LANG goes through the open gate.
func Resolve(in Inputs) Resolution {
	if l, ok := Parse(in.Env); ok {
		return Resolution{Requested: l, Effective: l, Source: SourceEnv}
	}
	if c := strings.TrimSpace(in.Config); c != "" && !strings.EqualFold(c, "auto") {
		if l, ok := Parse(c); ok {
			return gated(l, SourceConfig)
		}
	}
	if in.Legacy {
		return gated(Ko, SourceLegacy)
	}
	if l, ok := posixLocale(in); ok {
		return gated(l, SourceOS)
	}
	for _, p := range in.Preferred {
		if l, ok := Parse(p); ok {
			return gated(l, SourceOS)
		}
	}
	return gated(En, SourceDefault)
}

// posixLocale reads the first set variable of LC_ALL, LC_MESSAGES, LANG (POSIX
// precedence); "C"/"POSIX" there means "no language".
func posixLocale(in Inputs) (Locale, bool) {
	for _, v := range []string{in.LCAll, in.LCMessages, in.Lang} {
		if strings.TrimSpace(v) != "" {
			l, ok := Parse(v)
			if l == Pseudo {
				return "", false
			}
			return l, ok
		}
	}
	return "", false
}

func gated(l Locale, s Source) Resolution {
	return Resolution{Requested: l, Effective: gate(l), Source: s}
}

// DetectOS fills Inputs from the environment and the OS: MASIL_LANG, LC_ALL,
// LC_MESSAGES, LANG, and (only when the POSIX variables give no language) the
// OS UI language list. Failures leave fields empty. Config and Legacy are the
// caller's.
func DetectOS() Inputs {
	in := Inputs{
		Env:        os.Getenv("MASIL_LANG"),
		LCAll:      os.Getenv("LC_ALL"),
		LCMessages: os.Getenv("LC_MESSAGES"),
		Lang:       os.Getenv("LANG"),
	}
	if _, ok := posixLocale(in); !ok {
		in.Preferred = preferredUILanguages()
	}
	return in
}
