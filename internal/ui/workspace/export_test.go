package workspace

// SetPaneSupportedForTest makes embedded panes (un)available, as on Windows,
// and returns the restore func.
func SetPaneSupportedForTest(on bool) (restore func()) {
	old := paneSupported
	paneSupported = func() bool { return on }
	return func() { paneSupported = old }
}
