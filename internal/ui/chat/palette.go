package chat

// chatCommandIDs are the palette commands ui/chat registers: the CA 5.6 chat row,
// in its order. Each command is registered by the init() of the file that owns
// it; TestChatPaletteCommandsRegistered checks every one of them exists.
var chatCommandIDs = []string{
	"space.create", "space.join", "space.leave", "space.settings",
	"channel.create", "channel.prefs",
	"dm.new", "friends", "friend.add",
	"invite.create", "invite.direct", "invites.inbox",
	"search.messages", "pins.show",
}
