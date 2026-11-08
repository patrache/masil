package store

import "github.com/patrache/masil/internal/api"

// Friend changes made by my own REST answers (accept, auto-accepted request,
// decline, unfriend), applied at once by every screen that offers the action
// (the friends screen, the profile menu); the friend.* events that follow only
// confirm them. A nil Store is a no-op.

// AddFriend records f as my friend and drops the received requests from that user.
func (s *Store) AddFriend(f api.Friend) {
	if s == nil || f.User.ID == "" {
		return
	}
	s.ObserveUser(f.User)
	if s.Friends == nil {
		s.Friends = map[string]api.Friend{}
	}
	s.Friends[f.User.ID] = f
	s.removeFriendRequest(func(r api.FriendRequest) bool { return r.From.ID == f.User.ID })
}

// RemoveFriend ends the friendship with userID.
func (s *Store) RemoveFriend(userID string) {
	if s != nil {
		delete(s.Friends, userID)
	}
}

// DropFriendRequests removes the received requests matching drop.
func (s *Store) DropFriendRequests(drop func(api.FriendRequest) bool) {
	if s != nil {
		s.removeFriendRequest(drop)
	}
}
