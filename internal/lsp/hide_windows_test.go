//go:build windows

package lsp

import (
	"runtime"
	"testing"
	"unsafe"
)

// TestJobLimitLayout: SetInformationJobObject rejects a wrong size, which
// would leave a server's node.exe running after a kill.
func TestJobLimitLayout(t *testing.T) {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("layout checked on 64-bit only")
	}
	if n := unsafe.Sizeof(jobExtendedLimitInformation{}); n != 144 {
		t.Errorf("JOBOBJECT_EXTENDED_LIMIT_INFORMATION is %d bytes, want 144", n)
	}
	if off := unsafe.Offsetof(jobExtendedLimitInformation{}.LimitFlags); off != 16 {
		t.Errorf("LimitFlags at %d, want 16", off)
	}
}
