package ui

import tea "charm.land/bubbletea/v2"

// RouteKind is a route type; owners noted per constant.
type RouteKind string

// Route kinds.
const (
	RouteHome          RouteKind = "home"           // chat: DM·친구·피드 진입 목록 (레일 "홈")
	RouteChannel       RouteKind = "channel"        // chat: ID=channel id (text|dm|group_dm), Anchor=message id
	RouteThread        RouteKind = "thread"         // chat: ID=root message id, Arg=channel id
	RouteSearch        RouteKind = "search"         // chat: Arg=query, ID=channel|space scope ("" global)
	RouteInvite        RouteKind = "invite"         // chat: ID=invite code (link preview); Arg="direct" => direct invite, ID=invite id ("" = list of pending Store.DirectInvites)
	RouteSpaceSettings RouteKind = "space_settings" // chat: ID=space id
	RouteFriends       RouteKind = "friends"        // chat
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
	// TODO(core): Task 19
	return ""
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

// RouteUpdatedMsg is sent ONLY to a cached instance when navigation reuses it with a different Anchor/Query.
type RouteUpdatedMsg struct{ Route Route }

// Navigate emits NavigateMsg{Route: r}.
func Navigate(r Route) tea.Cmd {
	// TODO(core): Task 19
	return nil
}

// NavigateTo emits NavigateMsg{Route: r, Target: t}.
func NavigateTo(r Route, t Target) tea.Cmd {
	// TODO(core): Task 19
	return nil
}

// Back emits BackMsg.
func Back() tea.Cmd {
	// TODO(core): Task 19
	return nil
}
