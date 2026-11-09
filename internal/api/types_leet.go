package api

import "time"

// ---- 5.25 LeetCode daily problem (v0.1.3 P1, A10.3) ----
//
// The masil server never calls LeetCode: it keeps only the facts clients
// report and each user's solve records. Links are always built from Slug on
// the fixed host (leetcode.LeetLink / ValidSlug in internal/leetcode).

// Card types of the two server-only leet cards (api.md 2 card table).
const (
	CardLeetDaily = "leet.daily"
	CardLeetSolve = "leet.solve"
)

// LeetDailyFacts is what a client reports for a day (no body, no HTML).
type LeetDailyFacts struct {
	FrontendID string   `json:"frontend_id"` // ^[0-9]{1,6}$
	Slug       string   `json:"slug"`        // ^[a-z0-9-]{1,100}$
	Title      string   `json:"title"`
	Difficulty string   `json:"difficulty"` // Easy|Medium|Hard (not translated)
	Tags       []string `json:"tags"`
	IsPaidOnly bool     `json:"is_paid_only"`
}

// LeetDaily is the stored facts of a UTC day (first report wins).
type LeetDaily struct {
	Date string `json:"date"` // YYYY-MM-DD (UTC)
	LeetDailyFacts
	FirstReporter   *UserSummary `json:"first_reporter"`
	FirstReportedAt time.Time    `json:"first_reported_at"`
	MismatchCount   int          `json:"mismatch_count"`
}

// LeetDailyRoot is a #daily root message of one space's channel.
type LeetDailyRoot struct {
	SpaceID   string `json:"space_id"`
	ChannelID string `json:"channel_id"`
	MessageID string `json:"message_id"`
}

// LeetDailyReport is the PUT /v1/leet/daily/{date} response.
type LeetDailyReport struct {
	Daily LeetDaily       `json:"daily"`
	Roots []LeetDailyRoot `json:"roots"`
}

// LeetSolveRequest is PUT /v1/leet/solves/{date} (status "accepted" is
// added by PutLeetSolve). Nil / empty optional fields are not sent.
type LeetSolveRequest struct {
	Slug              string
	Lang              string // LeetCode language slug, e.g. "golang"
	FrontendID        string
	RuntimePercentile *float64
	SubmissionID      *string
	ShareSpaceIDs     []string // <= 10
	Code              *string  // shared as a spoiler in the solve card
}

// LeetSolve is my record of one day.
type LeetSolve struct {
	Date              string    `json:"date"`
	Slug              string    `json:"slug"`
	Lang              string    `json:"lang"`
	PassedAt          time.Time `json:"passed_at"`
	RuntimePercentile *float64  `json:"runtime_percentile"`
	CountsForStreak   bool      `json:"counts_for_streak"`
}

// LeetSolveCardRef is a solve card the server wrote or rewrote.
type LeetSolveCardRef struct {
	SpaceID   string `json:"space_id"`
	MessageID string `json:"message_id"`
}

// LeetSolveResult is the PUT /v1/leet/solves/{date} response.
type LeetSolveResult struct {
	Solve LeetSolve          `json:"solve"`
	Cards []LeetSolveCardRef `json:"cards"`
}

// LeetDailySpace is a space of mine with #daily on.
type LeetDailySpace struct {
	SpaceID   string `json:"space_id"`
	ChannelID string `json:"channel_id"`
}

// LeetMe is GET /v1/leet/me.
type LeetMe struct {
	Current     int              `json:"current"`
	Best        int              `json:"best"`
	Recent      []LeetSolve      `json:"recent"` // last 30 UTC days, newest first
	DailySpaces []LeetDailySpace `json:"daily_spaces"`
}

// LeetBoard is GET /v1/spaces/{space_id}/leet/board.
type LeetBoard struct {
	Period string           `json:"period"` // week|month|all
	Items  []LeetBoardEntry `json:"items"`
}

// LeetBoardEntry is one member of a space board.
type LeetBoardEntry struct {
	User   UserSummary `json:"user"`
	Solved int         `json:"solved"`
	Streak int         `json:"streak"`
}

// LeetFriendsBoard is GET /v1/leet/friends/board.
type LeetFriendsBoard struct {
	Items []LeetFriendStreak `json:"items"`
}

// LeetFriendStreak is one person's streaks on the friends board.
type LeetFriendStreak struct {
	User    UserSummary `json:"user"`
	Current int         `json:"current"`
	Best    int         `json:"best"`
}

// LeetSpaceSettings is a space's #daily switch.
type LeetSpaceSettings struct {
	SpaceID   string     `json:"space_id"`
	Enabled   bool       `json:"enabled"`
	ChannelID *string    `json:"channel_id"`
	EnabledBy *string    `json:"enabled_by"`
	EnabledAt *time.Time `json:"enabled_at"`
}

// LeetDailyCard is Card.Data of a leet.daily card (v 1).
type LeetDailyCard struct {
	Date string `json:"date"`
	LeetDailyFacts
}

// LeetSolveCard is Card.Data of a leet.solve card (v 1).
type LeetSolveCard struct {
	Date              string         `json:"date"`
	FrontendID        string         `json:"frontend_id"`
	Slug              string         `json:"slug"`
	Lang              string         `json:"lang"`
	RuntimePercentile *float64       `json:"runtime_percentile"`
	Code              *LeetSolveCode `json:"code"`
	Spoiler           bool           `json:"spoiler"`
}

// LeetSolveCode is the code a solver chose to share (a spoiler).
type LeetSolveCode struct {
	Lang string `json:"lang"`
	Text string `json:"text"`
}
