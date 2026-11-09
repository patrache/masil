package lazygit

// RecentMax is the most recent repositories kept (A8.5).
const RecentMax = 10

// PushRecent returns list with repo moved to the front, duplicates removed,
// at most RecentMax long. list is not modified; an empty repo changes nothing.
func PushRecent(list []string, repo string) []string {
	if repo == "" {
		return append([]string(nil), list...)
	}
	out := make([]string, 0, min(len(list)+1, RecentMax))
	out = append(out, repo)
	for _, r := range list {
		if len(out) == RecentMax {
			break
		}
		if r != repo && r != "" {
			out = append(out, r)
		}
	}
	return out
}
