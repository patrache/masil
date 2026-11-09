package fixtures

import (
	"encoding/json"

	"github.com/patrache/masil/internal/api"
	"github.com/patrache/masil/internal/store"
)

// ChCards is the 노바 개발방 #카드 channel of the Cards set: a text message, a
// card of a type tests register a renderer for (arch.design) and one they leave
// unregistered (api.result, drawn from its body). Only Cards adds it, so goldens
// of the plain Seed never see it.
const ChCards = "01JB0000000000000000000CD1"

// Card message ids, ascending.
var msgCards = func(n int) string { return fid("01JDCRD", n) }

// Card fixture message ids.
var (
	MsgCardText   = msgCards(1)
	MsgCardDesign = msgCards(2) // arch.design by 준호
	MsgCardResult = msgCards(3) // api.result by sam (mine)
)

func cardMsg(id string, author api.UserSummary, body, typ, data string, hm [2]int) api.Message {
	m := msg(id, ChCards, author, body, at(2, hm[0], hm[1]))
	m.Kind = api.MessageKindCard
	m.Card = &api.Card{Type: typ, V: 1, Data: json.RawMessage(data)}
	return m
}

func cardMessages() []api.Message {
	return []api.Message{
		msg(MsgCardText, ChCards, minji(), "결제 설계 올려 주세요", at(1, 50, 0)),
		cardMsg(MsgCardDesign, junho(), "📐 결제 v3 · 노드 14", "arch.design",
			`{"title":"결제 v3","nodes":14,"edges":20}`, [2]int{0, 30}),
		cardMsg(MsgCardResult, sam(), "🧪 GET /health → 200", "api.result",
			`{"method":"GET","path":"/health","status":200}`, [2]int{1, 0}),
	}
}

// Cards adds #카드 to a seeded Store (call after Seed).
func Cards(s *store.Store) {
	s.ObserveChannel(textChannel(ChCards, SpaceNova, "카드", 9, api.ChannelMyState{Notify: "all"}))
}
