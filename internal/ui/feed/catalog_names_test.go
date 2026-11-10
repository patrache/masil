package feed

import "github.com/patrache/masil/internal/i18n"

// The ko catalog texts under the names the tests used before the strings moved
// to the catalog (P5 Task 12-13); tests compare the ko screen with these.
var (
	ko                    = i18n.KoT()
	textEmptyFollow       = ko.Feed.EmptyFollowing()
	hintEmptyFollow       = ko.Feed.EmptyFollowingHint()
	textEmptyDiscover     = ko.Feed.EmptyDiscover()
	hintEmptyDiscover     = ko.Feed.EmptyDiscoverHint()
	textMediaFailed       = ko.Feed.MediaFailed()
	textMediaNone         = ko.Feed.MediaNone()
	textDeleted           = ko.Feed.CardDeleted()
	textEditTitle         = ko.Feed.EditProfile()
	textFollowReqsTitle   = ko.Feed.FollowRequests()
	textNotifEmpty        = ko.Feed.NotifEmpty()
	textProfileLoadFailed = ko.Feed.ProfileLoadFailed()
	textSuspendedTitle    = ko.Feed.SuspendedTitle()
)
