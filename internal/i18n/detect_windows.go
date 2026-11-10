//go:build windows

package i18n

import "golang.org/x/sys/windows"

// preferredUILanguages returns the user's UI languages
// (GetUserPreferredUILanguages). Any failure returns nil.
func preferredUILanguages() []string {
	ls, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil {
		return nil
	}
	return ls
}
