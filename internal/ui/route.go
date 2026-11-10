package ui

import tea "charm.land/bubbletea/v2"

// RouteKind is a route type; owners noted per constant.
type RouteKind string

// Route kinds.
const (
	RouteHome          RouteKind = "home"           // chat: 방·DM·친구·피드 진입 목록 (레일 "홈")
	RouteChannel       RouteKind = "channel"        // chat: ID=channel id (text|dm|group_dm), Anchor=message id
	RouteDMs           RouteKind = "dms"            // chat: the DM list (DM surface root: 1:1 and group DMs by recent activity)
	RouteThread        RouteKind = "thread"         // chat: ID=root message id, Arg=channel id
	RouteSearch        RouteKind = "search"         // chat: Arg=query, ID=channel|space scope ("" global)
	RouteInvite        RouteKind = "invite"         // chat: ID=invite code (link preview); Arg="direct" => direct invite, ID=invite id ("" = list of pending Store.DirectInvites)
	RouteSpaceSettings RouteKind = "space_settings" // chat: ID=space id
	RouteFriends       RouteKind = "friends"        // chat: Anchor=FriendsIncoming|FriendsList opens that tab ("" = the remembered one)
	RouteFeed          RouteKind = "feed"           // sns-fun: Arg="following"|"discover"
	RoutePost          RouteKind = "post"           // sns-fun: ID=post id
	RouteProfile       RouteKind = "profile"        // sns-fun: ID=user id or "@handle"
	RouteHashtag       RouteKind = "hashtag"        // sns-fun: ID=tag (no '#')
	RouteTrends        RouteKind = "trends"         // sns-fun
	RouteNotifications RouteKind = "notifications"  // sns-fun
	RouteStories       RouteKind = "stories"        // sns-fun: ID=user id (start group)
	RouteCompose       RouteKind = "compose"        // sns-fun: Arg="post"|"story", ID=reply_to/quote target, Extra in Query
	RouteSettings      RouteKind = "settings"       // core
	RouteLogin         RouteKind = "login"          // core
	RouteHelp          RouteKind = "help"           // core
	RouteTools         RouteKind = "tools"          // core ui/tools: the tool hub (tool surface only)
	RouteTool          RouteKind = "tool"           // core ui/tools: a tool instance, ID=instance id, Arg=tool id (tool surface only)
)

// RouteFriends anchors: the tab the friends screen opens on (a home row, a
// friend-request alert or notification).
const (
	FriendsList     = "friends"  // 친구
	FriendsIncoming = "incoming" // 받은 요청
)

// RouteChannel Query: {QueryJump: JumpUnread} with Anchor = my read pointer
// selects the first message after it that someone else wrote (the 알림함's
// "안 읽은 대화" rows), instead of the anchor itself.
const (
	QueryJump  = "jump"
	JumpUnread = "unread"
)

// Route is a logical place.
type Route struct {
	Kind   RouteKind
	ID     string
	Arg    string
	Anchor string            // jump target (message id / post id)
	Query  map[string]string // rare extras (e.g. compose: {"mode":"quote"})
}

// Key is Kind + "/" + ID + "/" + Arg (Anchor/Query excluded => same instance).
func (r Route) Key() string {
	return string(r.Kind) + "/" + r.ID + "/" + r.Arg
}

// Target selects the destination stack for pushable kinds.
type Target int

// Targets.
const (
	TargetAuto Target = iota // placement table (CA 5.3.3)
	TargetMain               // push onto the active tab's stack (pushable kinds only; root/lifted kinds ignore Target)
	TargetSide               // push onto the feed surface stack (pushable kinds only; root/lifted kinds ignore Target)
)

// NavigateMsg navigates. Replace: replace the destination stack's top instead of pushing.
type NavigateMsg struct {
	Route   Route
	Target  Target
	Replace bool
}

// BackMsg goes back one step.
type BackMsg struct{}

// RouteUpdatedMsg is sent ONLY to a cached instance when navigation reuses it
// with a different Anchor/Query, or with a non-empty Anchor that navigation asked
// for again (rejump: the same Anchor re-sends it so the jump happens again). A
// plain resize or re-projection sends none.
type RouteUpdatedMsg struct{ Route Route }

// Navigate emits NavigateMsg{Route: r}.
func Navigate(r Route) tea.Cmd {
	return func() tea.Msg { return NavigateMsg{Route: r} }
}

// NavigateTo emits NavigateMsg{Route: r, Target: t}.
func NavigateTo(r Route, t Target) tea.Cmd {
	return func() tea.Msg { return NavigateMsg{Route: r, Target: t} }
}

// Back emits BackMsg.
func Back() tea.Cmd {
	return func() tea.Msg { return BackMsg{} }
}
