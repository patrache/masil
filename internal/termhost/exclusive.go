package termhost

import (
	"sync"
	"sync/atomic"
	"testing"
)

// exclusiveReq is the program of the current (or last) exec run.
var exclusiveReq atomic.Pointer[Request]

// ExclusiveProgram is Request.Program and Request.Title of the exec program
// that owns the terminal now, or did last ("" before any). The app reads it
// after Exclusive() turned false to name the program in its return toast.
func ExclusiveProgram() (program, title string) {
	if r := exclusiveReq.Load(); r != nil {
		return r.Program, r.Title
	}
	return "", ""
}

// setExclusiveRequest records req as the exec program owning the terminal.
func setExclusiveRequest(req Request) {
	r := req
	exclusiveReq.Store(&r)
}

// SetExclusiveForTest forces Exclusive() to on (and, with req, the program
// ExclusiveProgram names). restore puts both back; it is idempotent. Tests only.
func SetExclusiveForTest(on bool, req ...Request) (restore func()) {
	if !testing.Testing() {
		panic("termhost: SetExclusiveForTest called outside a test")
	}
	prevOn, prevReq := exclusive.Load(), exclusiveReq.Load()
	exclusive.Store(on)
	if len(req) > 0 {
		setExclusiveRequest(req[0])
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			exclusive.Store(prevOn)
			exclusiveReq.Store(prevReq)
		})
	}
}
