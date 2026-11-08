package store

import (
	"testing"

	"github.com/patrache/masil/internal/api"
)

// C2 (v0.1.1): the friend changes my own REST answers make, shared by the
// friends screen and the profile menu.
func TestFriendMutations(t *testing.T) {
	hana := api.UserSummary{ID: "U-hana", Handle: "hana", DisplayName: "하나"}
	kim := api.UserSummary{ID: "U-kim", Handle: "dev_kim", DisplayName: "김개발"}
	s := New()
	s.FriendReqIn = []api.FriendRequest{{ID: "R1", From: hana}, {ID: "R2", From: kim}}
	before := s.FriendReqIn

	s.AddFriend(api.Friend{User: hana, Presence: api.PresenceOnline})
	if f, ok := s.Friends[hana.ID]; !ok || f.Presence != api.PresenceOnline || s.User(hana.ID) == nil {
		t.Fatalf("AddFriend: %+v ok=%v user=%v", f, ok, s.User(hana.ID))
	}
	if len(s.FriendReqIn) != 1 || s.FriendReqIn[0].ID != "R2" || before[0].ID != "R1" {
		t.Fatalf("AddFriend drops hana's request (copy-on-write): %+v / old %+v", s.FriendReqIn, before)
	}
	s.DropFriendRequests(func(r api.FriendRequest) bool { return r.ID == "R2" })
	if len(s.FriendReqIn) != 0 {
		t.Fatalf("DropFriendRequests: %+v", s.FriendReqIn)
	}
	s.RemoveFriend(hana.ID)
	if _, ok := s.Friends[hana.ID]; ok {
		t.Fatal("RemoveFriend kept hana")
	}

	var none *Store // nil Store: no-ops
	none.AddFriend(api.Friend{User: hana})
	none.RemoveFriend(hana.ID)
	none.DropFriendRequests(func(api.FriendRequest) bool { return true })
	s.AddFriend(api.Friend{}) // no user: ignored
	if len(s.Friends) != 0 {
		t.Fatalf("empty friend stored: %+v", s.Friends)
	}
}
