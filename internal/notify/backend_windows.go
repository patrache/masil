//go:build windows

package notify

import (
	"git.sr.ht/~jackmordaunt/go-toast/wintoast"
)

// newPlatform wires the WinRT toast COM API in (show: backend_platform.go).
func newPlatform(appName string) platform {
	return windowsPlatform{appName: appName, setAppID: setToastAppID, push: pushToast}
}

func setToastAppID(appID string) error {
	return wintoast.SetAppData(wintoast.AppData{AppID: appID})
}

// pushToast pushes doc through the WinRT COM API only. wintoast.Push is called
// without options: its PowerShell paths (a script built from the XML) stay off.
func pushToast(doc string) error {
	return wintoast.Push(doc)
}
