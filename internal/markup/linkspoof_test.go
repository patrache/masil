package markup

import "testing"

// Pending fix 0-c: a link is only an OSC 8 hyperlink when what the screen
// shows (textwidth.Sanitize) is the target itself, up to NFC normalization.
func TestHyperlinkableRejectsDisplayTargetMismatch(t *testing.T) {
	bad := map[string]string{
		"VS15":       "https://a.example/x︎",
		"VS16":       "https://a.example/x️",
		"skin tone":  "https://a.example/x\U0001F3FB",
		"keycap":     "https://a.example/1⃣",
		"tag char":   "https://a.example/x\U000E0041",
		"tag cancel": "https://a.example/x\U000E007F",
		"zwsp":       "https://a.example/x​",
		"bidi":       "https://a.example/‮x",
	}
	for name, u := range bad {
		if hyperlinkable(u) {
			t.Errorf("%s: %q must not be hyperlinkable", name, u)
		}
	}
	good := []string{
		"https://a.example/path?q=1#f",
		"https://a.example/한글/경로",
		"https://a.example/é", // NFD only: NFC differs, allowed
	}
	for _, u := range good {
		if !hyperlinkable(u) {
			t.Errorf("%q should be hyperlinkable", u)
		}
	}
}
