package chat

import "github.com/patrache/masil/internal/i18n"

// The ko catalog texts under the names the tests used before the strings moved
// to the catalog (P5 Task 14-16); tests compare the ko screen with these.
var (
	koCat              = i18n.KoT()
	groupDMRefusedText = koCat.Chat.GroupDmRefused()
	membersNone        = koCat.Chat.MembersNone()
	homeEmptyTitle     = koCat.Chat.HomeEmptyTitle()
	blockedText        = koCat.Chat.BlockedMsg()
	deletedText        = koCat.Chat.MsgDeleted()
	unknownUser        = koCat.Chat.UnknownUser()
	loadingText        = koCat.Chat.Loading()
	reactFailed        = koCat.Chat.ReactFailed()
	deleteFailed       = koCat.Chat.DeleteFailed()
	msgCopied          = koCat.Chat.MsgCopied()
	itemThread         = koCat.Chat.ItemThread()
	itemReact          = koCat.Chat.ItemReact()
	itemEdit           = koCat.Chat.ItemEdit()
	pinsFailed         = koCat.Chat.PinsFailed()
	goneTitle          = koCat.Chat.GoneTitle()
	inviteFailedText   = koCat.Chat.InviteFailed()
	nameLabel          = koCat.Chat.NameLabel()
	spaceCreateTitle   = koCat.Chat.CmdSpaceCreate()
	spaceJoinTitle     = koCat.Chat.SpaceJoinTitle()
	spaceJoinSubmit    = koCat.Chat.SpaceJoinSubmit()
)
