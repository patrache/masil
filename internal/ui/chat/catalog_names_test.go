package chat

import "github.com/patrache/masil/internal/i18n"

// The ko catalog texts under the names the tests used before the strings moved
// to the catalog (P5 Task 14-16); tests compare the ko screen with these.
var (
	koCat              = i18n.KoT()
	groupDMRefusedText = koCat.Chat.GroupDmRefused()
	membersNone        = koCat.Chat.MembersNone()
	homeEmptyTitle     = koCat.Chat.HomeEmptyTitle()
)
