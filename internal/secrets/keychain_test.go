package secrets

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSec is a secAPI over a map (service + "\x00" + account => value). It
// checks every query's shape, logs each call, and can force a status per call
// or hold every call until release is closed.
type fakeSec struct {
	mu      sync.Mutex
	items   map[string][]byte
	calls   []string
	status  map[string]int32 // op => status returned instead of doing it
	dupOnce bool             // the first add finds the item added by someone else
	hold    chan struct{}    // non-nil: each call waits for it to close
	started chan string      // non-nil: each call reports its op on entry
	t       *testing.T
}

func newFakeSec(t *testing.T) *fakeSec {
	return &fakeSec{items: map[string][]byte{}, status: map[string]int32{}, t: t}
}

func newTestKeychain(api secAPI) keychain { return keychain{api: api, slot: make(chan struct{}, 1)} }

func (f *fakeSec) enter(op string) (int32, bool) {
	if f.started != nil {
		f.started <- op
	}
	if f.hold != nil {
		<-f.hold
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, op)
	st, forced := f.status[op]
	return st, forced
}

func (f *fakeSec) log() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// item checks that q starts with the generic-password class, service and
// account and returns the map key.
func (f *fakeSec) item(q []kcPair) string {
	f.t.Helper()
	if len(q) < 3 || q[0] != (kcPair{kcClass, kcClassGenericPassword}) || q[1].key != kcAttrService || q[2].key != kcAttrAccount {
		f.t.Errorf("query = %v, want class, service, account first", q)
		return ""
	}
	return q[1].val.(string) + "\x00" + q[2].val.(string)
}

func (f *fakeSec) update(q, attrs []kcPair) (int32, error) {
	if st, ok := f.enter("update"); ok {
		return st, nil
	}
	if len(q) != 3 || len(attrs) != 1 || attrs[0].key != kcValueData {
		f.t.Errorf("update(%v, %v)", q, attrs)
	}
	k := f.item(q)
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[k]; !ok {
		return errSecItemNotFound, nil
	}
	f.items[k] = bytes.Clone(attrs[0].val.([]byte))
	return errSecSuccess, nil
}

func (f *fakeSec) add(attrs []kcPair) (int32, error) {
	if st, ok := f.enter("add"); ok {
		return st, nil
	}
	k := f.item(attrs)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.dupOnce {
		f.dupOnce = false
		f.items[k] = []byte("theirs")
		return errSecDuplicateItem, nil
	}
	if _, ok := f.items[k]; ok {
		return errSecDuplicateItem, nil
	}
	var v []byte
	for _, p := range attrs {
		if p.key == kcValueData {
			v = bytes.Clone(p.val.([]byte))
		}
	}
	f.items[k] = v
	return errSecSuccess, nil
}

func (f *fakeSec) copyData(q []kcPair) ([]byte, int32, error) {
	if st, ok := f.enter("copy"); ok {
		return nil, st, nil
	}
	if len(q) != 5 {
		f.t.Errorf("copy(%v)", q)
	}
	k := f.item(q)
	f.mu.Lock()
	defer f.mu.Unlock()
	v, ok := f.items[k]
	if !ok {
		return nil, errSecItemNotFound, nil
	}
	return bytes.Clone(v), errSecSuccess, nil
}

func (f *fakeSec) delete(q []kcPair) (int32, error) {
	if st, ok := f.enter("delete"); ok {
		return st, nil
	}
	if len(q) != 3 {
		f.t.Errorf("delete(%v)", q)
	}
	k := f.item(q)
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.items[k]; !ok {
		return errSecItemNotFound, nil
	}
	delete(f.items, k)
	return errSecSuccess, nil
}

// TestKeychainQueries: the SecItem dictionaries (A9.2): a generic password
// keyed by service and account; the value only where it is written; a read
// asks for the data of one item.
func TestKeychainQueries(t *testing.T) {
	item := []kcPair{{kcClass, kcClassGenericPassword}, {kcAttrService, "masil"}, {kcAttrAccount, "leet/h/U1/c"}}
	if got := kcItem("masil", "leet/h/U1/c"); !reflect.DeepEqual(got, item) {
		t.Errorf("kcItem = %v", got)
	}
	v := []byte("v\x00")
	if got, want := kcAddAttrs("masil", "leet/h/U1/c", v), append(item[:3:3], kcPair{kcAttrLabel, "masil"}, kcPair{kcValueData, v}); !reflect.DeepEqual(got, want) {
		t.Errorf("kcAddAttrs = %v", got)
	}
	if got := kcValueAttrs(v); !reflect.DeepEqual(got, []kcPair{{kcValueData, v}}) {
		t.Errorf("kcValueAttrs = %v", got)
	}
	if got, want := kcGetQuery("masil", "leet/h/U1/c"), append(item[:3:3], kcPair{kcReturnData, kcTrue}, kcPair{kcMatchLimit, kcMatchLimitOne}); !reflect.DeepEqual(got, want) {
		t.Errorf("kcGetQuery = %v", got)
	}
	// Every constant has a symbol in the framework it comes from.
	for n := range kcNames {
		s := kcSymbols[n]
		if s.name == "" || (s.lib != libSecurity && s.lib != libCoreFoundation) {
			t.Errorf("kcSymbols[%d] = %+v", n, s)
		}
	}
	if kcSymbols[kcTrue] != (kcSymbol{libCoreFoundation, "kCFBooleanTrue"}) || kcSymbols[kcClass] != (kcSymbol{libSecurity, "kSecClass"}) {
		t.Errorf("kcSymbols = %+v", kcSymbols)
	}
}

// TestKeychainStatusMapping: errSecItemNotFound is ErrNotFound; a refusal
// (canceled prompt, denied, no way to ask) and every other failure are
// ErrUnavailable — the store then asks for the fallback consent — and the
// message names the OSStatus.
func TestKeychainStatusMapping(t *testing.T) {
	if err := kcErr("read", errSecSuccess); err != nil {
		t.Errorf("success = %v", err)
	}
	if err := kcErr("read", -25300); err != ErrNotFound {
		t.Errorf("errSecItemNotFound = %v", err)
	}
	for _, st := range []int32{-128, -25293, -25308} {
		err := kcErr("add", st)
		if !errors.Is(err, ErrUnavailable) || !errors.Is(err, errKCDenied) || !strings.Contains(err.Error(), strconv.Itoa(int(st))) || !strings.Contains(err.Error(), "denied") {
			t.Errorf("OSStatus %d = %v, want a denied ErrUnavailable naming the status", st, err)
		}
	}
	for _, st := range []int32{-25291, -25294, -50, -34018, -25299} {
		err := kcErr("update", st)
		if !errors.Is(err, ErrUnavailable) || errors.Is(err, errKCDenied) || errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "OSStatus "+strconv.Itoa(int(st))) {
			t.Errorf("OSStatus %d = %v, want ErrUnavailable naming the status", st, err)
		}
	}
}

func TestKeychainName(t *testing.T) {
	if n := newTestKeychain(newFakeSec(t)).Name(); n != BackendKeychain {
		t.Fatalf("Name = %q", n)
	}
}

// TestKeychainSetUpdatesThenAdds: Set updates the item in place and adds it
// only when it is missing — never delete-then-add, which would leave a moment
// with no value. An add that loses a race to another writer updates again.
func TestKeychainSetUpdatesThenAdds(t *testing.T) {
	api := newFakeSec(t)
	kc := newTestKeychain(api)
	if err := kc.Set(bg, Service, "a/b/c/d", "one"); err != nil {
		t.Fatal(err)
	}
	if err := kc.Set(bg, Service, "a/b/c/d", "two"); err != nil {
		t.Fatal(err)
	}
	if got, want := api.log(), []string{"update", "add", "update"}; !reflect.DeepEqual(got, want) {
		t.Errorf("calls = %v, want %v", got, want)
	}
	if v, err := kc.Get(bg, Service, "a/b/c/d"); err != nil || v != "two" {
		t.Errorf("Get = %q, %v", v, err)
	}

	race := newFakeSec(t)
	race.dupOnce = true
	if err := newTestKeychain(race).Set(bg, Service, "a/b/c/d", "mine"); err != nil {
		t.Fatal(err)
	}
	if got, want := race.log(), []string{"update", "add", "update"}; !reflect.DeepEqual(got, want) {
		t.Errorf("race calls = %v, want %v", got, want)
	}
	if v := string(race.items[Service+"\x00a/b/c/d"]); v != "mine" {
		t.Errorf("after the race the item holds %q", v)
	}
}

// TestKeychainRawBytes: any value (64 KiB, line breaks, quotes, NUL, Hangul)
// is stored and read back as its bytes; there is no encoding or chunking.
func TestKeychainRawBytes(t *testing.T) {
	api := newFakeSec(t)
	kc := newTestKeychain(api)
	big := strings.Repeat("eyJhbGciOiJIUzI1NiJ9.", 64<<10/21+1)[:64<<10]
	for _, v := range []string{big, "a\nb\r'\"\\\x00z", "토큰abc", ""} {
		if err := kc.Set(bg, Service, "api/h/U1/v", v); err != nil {
			t.Fatalf("Set(%d bytes) = %v", len(v), err)
		}
		if raw := api.items[Service+"\x00api/h/U1/v"]; string(raw) != v {
			t.Fatalf("stored %d bytes, want the %d raw bytes", len(raw), len(v))
		}
		if got, err := kc.Get(bg, Service, "api/h/U1/v"); err != nil || got != v {
			t.Fatalf("Get = %d bytes, %v; want %d bytes", len(got), err, len(v))
		}
	}
	if len(api.items) != 1 {
		t.Errorf("%d items, want 1", len(api.items))
	}
	if err := kc.Delete(bg, Service, "api/h/U1/v"); err != nil {
		t.Fatal(err)
	}
	if _, err := kc.Get(bg, Service, "api/h/U1/v"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete = %v, want ErrNotFound", err)
	}
	if err := kc.Delete(bg, Service, "api/h/U1/v"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Delete of a missing item = %v, want ErrNotFound", err)
	}
}

// TestKeychainTooLarge: a value past kcMaxValue is refused without calling
// the keychain, and not as ErrUnavailable (the keychain works; the store must
// not mark it down).
func TestKeychainTooLarge(t *testing.T) {
	api := newFakeSec(t)
	kc := newTestKeychain(api)
	for _, n := range []int{kcMaxValue + 1, 1 << 20} {
		err := kc.Set(bg, Service, "api/h/U1/big", strings.Repeat("x", n))
		if err == nil || errors.Is(err, ErrUnavailable) {
			t.Fatalf("Set(%d bytes) = %v, want a too-large error that is not ErrUnavailable", n, err)
		}
	}
	if calls := api.log(); len(calls) != 0 {
		t.Errorf("the keychain was called for a value that cannot fit: %v", calls)
	}
	if err := kc.Set(bg, Service, "api/h/U1/big", strings.Repeat("x", kcMaxValue)); err != nil {
		t.Errorf("Set(kcMaxValue) = %v", err)
	}
	s := Open(Options{OS: kc, Paths: testPaths(t)})
	if _, err := s.Put(bg, key("U1", "big"), New(strings.Repeat("x", 1<<20))); err == nil || errors.Is(err, ErrNeedConsent) {
		t.Fatalf("Put(1 MiB) = %v, want the too-large error", err)
	}
	if b, err := s.Put(bg, key("U1", "small"), New("v")); err != nil || b != BackendKeychain {
		t.Fatalf("Put(small) after a too-large value = %q, %v; want the keychain", b, err)
	}
}

// TestPutKeychainDeniedNeedsConsent: a denied, canceled or unavailable
// keychain makes the store ask for the fallback (mistake 4 of the plan: no
// crash, no endless retry).
func TestPutKeychainDeniedNeedsConsent(t *testing.T) {
	for _, st := range []int32{-128, -25293, -25308, -25291} {
		t.Run(strconv.Itoa(int(st)), func(t *testing.T) {
			api := newFakeSec(t)
			api.status["update"] = st
			kc := newTestKeychain(api)
			if err := kc.Set(bg, Service, "leet/h/U1/c", "v"); !errors.Is(err, ErrUnavailable) {
				t.Fatalf("Set = %v, want ErrUnavailable", err)
			}
			s := Open(Options{OS: kc, Paths: testPaths(t)})
			if _, err := s.Put(bg, key("U1", "c"), New("v")); !errors.Is(err, ErrNeedConsent) {
				t.Fatalf("Put = %v, want ErrNeedConsent", err)
			}
			if s.Fallback() != FallbackUnset {
				t.Fatal("fallback chosen without consent")
			}
		})
	}
}

// TestKeychainErrorsCarryNoValue: no failure message holds the value.
func TestKeychainErrorsCarryNoValue(t *testing.T) {
	const value = "zq81-secret-value"
	for _, op := range []string{"update", "add", "copy", "delete"} {
		for _, st := range []int32{-128, -25293, -50} {
			api := newFakeSec(t)
			api.status[op] = st
			kc := newTestKeychain(api)
			errs := []error{kc.Set(bg, Service, "a/b/c/d", value), kc.Delete(bg, Service, "a/b/c/d")}
			_, err := kc.Get(bg, Service, "a/b/c/d")
			errs = append(errs, err)
			for _, e := range errs {
				if e != nil && strings.Contains(e.Error(), value) {
					t.Errorf("%s %d: error holds the value: %v", op, st, e)
				}
			}
		}
	}
}

// TestKeychainCanceledContext: a call blocked on the keychain's prompt
// returns the context's error as soon as the context ends (not
// ErrUnavailable); the blocked call is the only one left running — the next
// call waits for it (or for its own context) instead of starting another.
func TestKeychainCanceledContext(t *testing.T) {
	api := newFakeSec(t)
	api.hold = make(chan struct{})
	api.started = make(chan string, 4)
	kc := newTestKeychain(api)

	short, cancel := context.WithTimeout(bg, 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := kc.Set(short, Service, "a/b/c/d", "v"); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnavailable) {
		t.Errorf("Set(timeout) = %v, want context.DeadlineExceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("Set returned after %v, want promptly", d)
	}
	if op := <-api.started; op != "update" {
		t.Fatalf("first call = %q", op)
	}

	second, cancel2 := context.WithTimeout(bg, 100*time.Millisecond)
	defer cancel2()
	if _, err := kc.Get(second, Service, "a/b/c/d"); !errors.Is(err, context.DeadlineExceeded) || errors.Is(err, ErrUnavailable) {
		t.Errorf("Get while one call is blocked = %v, want context.DeadlineExceeded", err)
	}
	select {
	case op := <-api.started:
		t.Fatalf("a second keychain call (%s) started while the first was blocked", op)
	default:
	}

	done, cancel3 := context.WithCancel(bg)
	cancel3()
	if _, err := kc.Get(done, Service, "a/b/c/d"); !errors.Is(err, context.Canceled) || errors.Is(err, ErrUnavailable) {
		t.Errorf("Get(canceled) = %v, want context.Canceled", err)
	}

	close(api.hold) // the prompt is answered: the abandoned Set finishes
	if v, err := kc.Get(bg, Service, "a/b/c/d"); err != nil || v != "v" {
		t.Errorf("Get after the prompt = %q, %v (the abandoned Set still stores)", v, err)
	}
}

// TestKeychainLegacyCLIItem: an item the old /usr/bin/security backend wrote
// on a dev build of v0.1.3 holds "masil:b64:<base64>" or "masil:chunks:N" (the
// pieces under service "masil.chunk"). Get never hands that marker back as the
// value: it is ErrNotFound, so the tool asks to log in again. Set refuses a
// value that starts like one, so Get never hides a value Set stored.
func TestKeychainLegacyCLIItem(t *testing.T) {
	api := newFakeSec(t)
	kc := newTestKeychain(api)
	for _, raw := range []string{"masil:b64:eyJ0b2tlbiI6IngifQ==", "masil:chunks:3", "masil:b64:", "masil:chunks:"} {
		api.items[Service+"\x00api/h/U1/old"] = []byte(raw)
		got, err := kc.Get(bg, Service, "api/h/U1/old")
		if !errors.Is(err, ErrNotFound) || got != "" {
			t.Errorf("Get(old item %q) = %q, %v; want ErrNotFound", raw, got, err)
		}
		if err != nil && strings.Contains(err.Error(), raw) {
			t.Errorf("error %q carries the stored text", err)
		}
	}
	api.items[Service+"\x00"+key("U1", "old").Account()] = []byte("masil:b64:eyJ0b2tlbiI6IngifQ==")
	s := Open(Options{OS: kc, Paths: testPaths(t)})
	if _, _, err := s.Get(bg, key("U1", "old")); !errors.Is(err, ErrNotFound) {
		t.Errorf("Store.Get(old item) = %v, want ErrNotFound", err)
	}

	api.calls = nil
	for _, v := range []string{"masil:b64:abc", "masil:chunks:2"} {
		err := kc.Set(bg, Service, "api/h/U1/new", v)
		if err == nil || errors.Is(err, ErrUnavailable) || strings.Contains(err.Error(), v) {
			t.Errorf("Set(%q) = %v, want a refusal that is not ErrUnavailable and has no value", v, err)
		}
	}
	if calls := api.log(); len(calls) != 0 {
		t.Errorf("the keychain was called for a refused value: %v", calls)
	}
	for _, v := range []string{"masil:b6", "xmasil:b64:abc", "masil:chunk"} {
		if err := kc.Set(bg, Service, "api/h/U1/new", v); err != nil {
			t.Fatalf("Set(%q) = %v", v, err)
		}
		if got, err := kc.Get(bg, Service, "api/h/U1/new"); err != nil || got != v {
			t.Errorf("Get = %q, %v; want %q", got, err, v)
		}
	}
}
