package wscrypto

// setCounterForTest sets the last used counter value.
func setCounterForTest(s *Sealer, n uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter = n
}
