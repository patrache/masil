//go:build windows

package clipboard

import "golang.org/x/sys/windows"

// systemDir is the Windows system directory (GetSystemDirectory, not
// %SystemRoot%, so the environment cannot swap clip.exe).
func systemDir() string {
	d, err := windows.GetSystemDirectory()
	if err != nil {
		return ""
	}
	return d
}
