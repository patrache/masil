package leet

import (
	"image"
	"net/http"
	"testing"
)

// Test seams other packages' tests need too (internal/ui/e2e drives the
// real tool next to the others, P0 Task 31). Each panics outside a test
// binary, as termpane.SetLaunchForTest does.

func mustTest(name string) {
	if !testing.Testing() {
		panic("leet: " + name + " outside a test binary")
	}
}

// SetLeetcodeForTest points every LeetCode client this package makes at
// baseURL (a leetcodetest fake) instead of leetcode.com; the returned func
// restores it. Production code has no way to change the address.
func SetLeetcodeForTest(baseURL string) (restore func()) {
	mustTest("SetLeetcodeForTest")
	clientMu.Lock()
	saved := leetcodeURL
	leetcodeURL = baseURL
	clientMu.Unlock()
	return func() {
		clientMu.Lock()
		leetcodeURL = saved
		clientMu.Unlock()
	}
}

// SetImageTransportForTest answers every problem-image fetch with f (and
// forgets the images of this run); the returned func restores it.
func SetImageTransportForTest(f func(*http.Request) (*http.Response, error)) (restore func()) {
	mustTest("SetImageTransportForTest")
	reset := func(tr http.RoundTripper) {
		imageMu.Lock()
		defer imageMu.Unlock()
		imageTransport = tr
		images, asked = map[string]image.Image{}, map[string]bool{}
	}
	imageMu.Lock()
	saved := imageTransport
	imageMu.Unlock()
	reset(roundTrip(f))
	return func() { reset(saved) }
}

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
