//go:build windows

package notify

import (
	"fmt"

	"git.sr.ht/~jackmordaunt/go-toast/wintoast"
)

type windowsPlatform struct{ appName string }

func newPlatform(appName string) platform { return windowsPlatform{appName: appName} }

// show pushes the escaped toast XML through the WinRT COM API only. wintoast.Push
// is called without options: its PowerShell paths (a script built from the XML)
// stay off.
func (p windowsPlatform) show(n OSNotification) error {
	if err := wintoast.SetAppData(wintoast.AppData{AppID: p.appName}); err != nil {
		return fmt.Errorf("notify: toast app data: %w", err)
	}
	if err := wintoast.Push(toastXML(n)); err != nil {
		return fmt.Errorf("notify: toast: %w", err)
	}
	return nil
}
