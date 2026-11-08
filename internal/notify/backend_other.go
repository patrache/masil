//go:build !darwin && !windows && !linux

package notify

import "errors"

type unsupportedPlatform struct{}

func newPlatform(string) platform { return unsupportedPlatform{} }

func (unsupportedPlatform) show(OSNotification) error {
	return errors.New("notify: desktop notifications are not supported on this OS")
}
