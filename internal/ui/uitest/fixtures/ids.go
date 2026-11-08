// Package fixtures is seed data matching the mockups plus the hostile set (CA 10.4).
package fixtures

import (
	"fmt"
	"strings"
)

// Fixture IDs (26-char Crockford, core test convenience; other plans use Store lookups).
const (
	UserSam      = "01JA0000000000000000000SAM"
	UserMinji    = "01JA00000000000000000000M1"
	UserJunho    = "01JA00000000000000000000J1"
	UserHana     = "01JA00000000000000000000H1"
	UserDevKim   = "01JA00000000000000000000D1"
	SpaceNova    = "01JB00000000000000000000S1"
	ChGeneral    = "01JB00000000000000000000C1"
	ChChat       = "01JB00000000000000000000C2"
	ChDev        = "01JB00000000000000000000C3"
	ChDeploy     = "01JB00000000000000000000C4"
	DMMinji      = "01JB0000000000000000000DM1"
	DMJunho      = "01JB0000000000000000000DM2"
	SpaceHostile = "01JB00000000000000000000X1"
	ChHostile    = "01JB00000000000000000000X2"
)

// Hostile-set IDs (unexported: other plans find them through the Store).
const (
	userHostileLong = "01JA00000000000000000000X1" // 32-char handle, 32-rune name, long status
	userHostileCtrl = "01JA00000000000000000000X2" // control characters / ANSI in the name
	chHostileCtrl   = "01JB00000000000000000000X3" // control characters / ANSI in the name
	dmHostile       = "01JB0000000000000000000DX1" // DM with userHostileCtrl
)

// fid pads prefix with a zero-filled n to a 26-char Crockford ID ("01JDDEV" + 1 =>
// "01JDDEV0000000000000000001"). Prefixes use no I, L, O, U.
func fid(prefix string, n int) string {
	digits := fmt.Sprintf("%d", n)
	return prefix + strings.Repeat("0", 26-len(prefix)-len(digits)) + digits
}

// Message, post, poll, media and notification IDs, ascending per channel / feed.
var (
	msgGeneral = func(n int) string { return fid("01JDGEN", n) }
	msgChat    = func(n int) string { return fid("01JDCHT", n) }
	msgDev     = func(n int) string { return fid("01JDDEV", n) }
	msgThread  = func(n int) string { return fid("01JDTHR", n) }
	msgDeploy  = func(n int) string { return fid("01JDDPY", n) }
	msgDMMinji = func(n int) string { return fid("01JDDMM", n) }
	msgDMJunho = func(n int) string { return fid("01JDDMJ", n) }
	msgHostile = func(n int) string { return fid("01JDXXX", n) }

	postID        = func(n int) string { return fid("01JEPST", n) }
	hostilePostID = func(n int) string { return fid("01JEHPS", n) } // < "01JEPST": older than the normal feed
	repostID      = func(n int) string { return fid("01JEHRP", n) }
	pollID        = func(n int) string { return fid("01JGPQ", n) }
	mediaID       = func(n int) string { return fid("01JHMED", n) }
	notifID       = func(n int) string { return fid("01JFNTF", n) }
)
