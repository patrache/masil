package daemon

import (
	"os"
	"runtime"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	p := testPaths(t)
	if s, err := loadState(p); s != nil || err != nil {
		t.Fatalf("missing file: got %v, %v; want nil, nil", s, err)
	}
	in := State{UserID: "01JA0000000000000000000SAM", Epoch: "01JC2000000000000000000000", LastSeq: 1041, SavedAt: t0}
	if err := saveState(p, in); err != nil {
		t.Fatal(err)
	}
	got, err := loadState(p)
	if err != nil {
		t.Fatal(err)
	}
	in.Version = 1
	if *got != in {
		t.Fatalf("got %+v want %+v", *got, in)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(p.DaemonState)
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("perm %v", fi.Mode().Perm())
		}
	}
}

func TestStateCorrupt(t *testing.T) {
	p := testPaths(t)
	os.WriteFile(p.DaemonState, []byte("{not json"), 0o600)
	if s, err := loadState(p); s != nil || err == nil {
		t.Fatalf("corrupt: got %v, %v; want nil, error", s, err)
	}
}

func TestResumePoint(t *testing.T) {
	st := &State{Version: 1, UserID: "U1", Epoch: "E1", LastSeq: 41}
	rp := resumePoint(st, "U1")
	if rp.UseDeviceAck || rp.Epoch == nil || *rp.Epoch != "E1" || rp.LastSeq == nil || *rp.LastSeq != 41 {
		t.Fatalf("same user: %+v", rp)
	}
	for name, s := range map[string]*State{
		"nil":        nil,
		"other_user": {Version: 1, UserID: "U2", Epoch: "E1", LastSeq: 41},
		"no_epoch":   {Version: 1, UserID: "U1"},
	} {
		if rp := resumePoint(s, "U1"); !rp.UseDeviceAck || rp.Epoch != nil || rp.LastSeq != nil {
			t.Errorf("%s: want use_device_ack only, got %+v", name, rp)
		}
	}
}
