package chat

import (
	"encoding/json"
	"os"
	"testing"
)

// channelNamesFile is the one table of channel-name inputs the server's CHANNEL_NAME_RE test also reads (api.md 0.3).
const channelNamesFile = "../../../server/test/fixtures/channel-names.json"

// TestChannelNameRuleMatchesServer: channelNameRe accepts and rejects exactly what the server's rule does — CJK
// unified ideographs (U+4E00–U+9FFF) and extension A (U+3400–U+4DBF) included (P5 Task 20).
func TestChannelNameRuleMatchesServer(t *testing.T) {
	raw, err := os.ReadFile(channelNamesFile)
	if err != nil {
		t.Fatal(err)
	}
	var table struct {
		Valid   []string `json:"valid"`
		Invalid []string `json:"invalid"`
	}
	if err := json.Unmarshal(raw, &table); err != nil {
		t.Fatal(err)
	}
	if len(table.Valid) == 0 || len(table.Invalid) == 0 {
		t.Fatalf("empty table: %d valid, %d invalid", len(table.Valid), len(table.Invalid))
	}
	for _, n := range table.Valid {
		if !channelNameRe.MatchString(n) {
			t.Errorf("%q rejected, server accepts it", n)
		}
		if msg := validChannelName(koCat)(n); msg != "" {
			t.Errorf("validChannelName(%q) = %q", n, msg)
		}
	}
	for _, n := range table.Invalid {
		if channelNameRe.MatchString(n) {
			t.Errorf("%q accepted, server rejects it", n)
		}
	}
}
