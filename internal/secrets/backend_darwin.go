package secrets

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
)

// defaultOS is the login keychain through Security.framework, in process
// (keychain.go). Every keychain shares one call slot (kcCall).
func defaultOS() OSBackend { return keychain{api: secFramework{}, slot: kcSlot} }

var kcSlot = make(chan struct{}, 1)

const (
	securityFramework       = "/System/Library/Frameworks/Security.framework/Security"
	coreFoundationFramework = "/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation"

	kCFStringEncodingUTF8 uint32 = 0x08000100
)

// secLib is the loaded frameworks: the SecItem calls, the CoreFoundation calls
// that build and read their dictionaries, and the constants (kcSymbols).
type secLib struct {
	sec, cf uintptr // dlopen handles

	consts                       [kcNames]uintptr
	keyCallBacks, valueCallBacks uintptr // &kCFTypeDictionaryKeyCallBacks, &kCFTypeDictionaryValueCallBacks

	CFRelease               func(cf uintptr)
	CFDictionaryCreate      func(alloc uintptr, keys, values []uintptr, n int, keyCB, valueCB uintptr) uintptr
	CFStringCreateWithBytes func(alloc uintptr, b []byte, n int, enc uint32, external bool) uintptr
	CFDataCreate            func(alloc uintptr, b []byte, n int) uintptr
	CFDataGetLength         func(d uintptr) int
	CFDataGetBytePtr        func(d uintptr) *byte
	CFGetTypeID             func(cf uintptr) uint
	CFDataGetTypeID         func() uint
	SecItemAdd              func(attrs uintptr, result *uintptr) int32
	SecItemUpdate           func(query, attrs uintptr) int32
	SecItemCopyMatching     func(query uintptr, result *uintptr) int32
	SecItemDelete           func(query uintptr) int32
}

var (
	secOnce    sync.Once
	secLoaded  *secLib
	secLoadErr error
)

// loadSec opens the frameworks once. A failure is ErrUnavailable.
func loadSec() (*secLib, error) {
	secOnce.Do(func() {
		secLoaded, secLoadErr = openSec()
		if secLoadErr != nil {
			secLoadErr = fmt.Errorf("%w: keychain: %v", ErrUnavailable, secLoadErr)
		}
	})
	return secLoaded, secLoadErr
}

func openSec() (*secLib, error) {
	l := &secLib{}
	var err error
	if l.sec, err = purego.Dlopen(securityFramework, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return nil, err
	}
	if l.cf, err = purego.Dlopen(coreFoundationFramework, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
		return nil, err
	}
	funcs := []struct {
		lib  uintptr
		name string
		fptr any
	}{
		{l.cf, "CFRelease", &l.CFRelease},
		{l.cf, "CFDictionaryCreate", &l.CFDictionaryCreate},
		{l.cf, "CFStringCreateWithBytes", &l.CFStringCreateWithBytes},
		{l.cf, "CFDataCreate", &l.CFDataCreate},
		{l.cf, "CFDataGetLength", &l.CFDataGetLength},
		{l.cf, "CFDataGetBytePtr", &l.CFDataGetBytePtr},
		{l.cf, "CFGetTypeID", &l.CFGetTypeID},
		{l.cf, "CFDataGetTypeID", &l.CFDataGetTypeID},
		{l.sec, "SecItemAdd", &l.SecItemAdd},
		{l.sec, "SecItemUpdate", &l.SecItemUpdate},
		{l.sec, "SecItemCopyMatching", &l.SecItemCopyMatching},
		{l.sec, "SecItemDelete", &l.SecItemDelete},
	}
	for _, f := range funcs {
		p, err := purego.Dlsym(f.lib, f.name)
		if err != nil {
			return nil, err
		}
		purego.RegisterFunc(f.fptr, p)
	}
	for n, s := range kcSymbols {
		lib := l.sec
		if s.lib == libCoreFoundation {
			lib = l.cf
		}
		p, err := purego.Dlsym(lib, s.name)
		if err != nil {
			return nil, err
		}
		l.consts[n] = derefPtr(p) // the symbol is a `const CFStringRef` (or CFBooleanRef) variable
		if l.consts[n] == 0 {
			return nil, fmt.Errorf("%s is NULL", s.name)
		}
	}
	// The callbacks are structs: their address is what CFDictionaryCreate takes.
	if l.keyCallBacks, err = purego.Dlsym(l.cf, "kCFTypeDictionaryKeyCallBacks"); err != nil {
		return nil, err
	}
	if l.valueCallBacks, err = purego.Dlsym(l.cf, "kCFTypeDictionaryValueCallBacks"); err != nil {
		return nil, err
	}
	return l, nil
}

// derefPtr reads the pointer stored at addr (a C global variable).
func derefPtr(addr uintptr) uintptr { return **(**uintptr)(unsafe.Pointer(&addr)) }

// errKCName: a name that CoreFoundation does not take as UTF-8 text. It is
// not ErrUnavailable (the keychain works; the name is wrong).
var errKCName = errors.New("secrets: keychain: a name is not valid UTF-8")

// dict builds the CFDictionary of pairs; the caller releases it. The strings
// and data made for it are released here (the dictionary retains them).
func (l *secLib) dict(pairs []kcPair) (uintptr, error) {
	keys := make([]uintptr, len(pairs))
	vals := make([]uintptr, len(pairs))
	var made []uintptr
	defer func() {
		for _, o := range made {
			l.CFRelease(o)
		}
	}()
	for i, p := range pairs {
		keys[i] = l.consts[p.key]
		switch v := p.val.(type) {
		case kcName:
			vals[i] = l.consts[v]
		case string:
			b := []byte(v)
			s := l.CFStringCreateWithBytes(0, b, len(b), kCFStringEncodingUTF8, false)
			if s == 0 {
				return 0, errKCName
			}
			made = append(made, s)
			vals[i] = s
		case []byte:
			d := l.CFDataCreate(0, v, len(v))
			if d == 0 {
				return 0, fmt.Errorf("%w: keychain: CFDataCreate failed", ErrUnavailable)
			}
			made = append(made, d)
			vals[i] = d
		default:
			panic(fmt.Sprintf("secrets: keychain: pair value %T", p.val))
		}
	}
	d := l.CFDictionaryCreate(0, keys, vals, len(pairs), l.keyCallBacks, l.valueCallBacks)
	runtime.KeepAlive(keys)
	runtime.KeepAlive(vals)
	if d == 0 {
		return 0, fmt.Errorf("%w: keychain: CFDictionaryCreate failed", ErrUnavailable)
	}
	return d, nil
}

// secFramework is the secAPI over the loaded frameworks. Every CF object it
// makes or receives is released before it returns.
type secFramework struct{}

// with loads the frameworks and builds the dictionaries of qs for f.
func (secFramework) with(f func(l *secLib, ds []uintptr) int32, qs ...[]kcPair) (int32, error) {
	l, err := loadSec()
	if err != nil {
		return 0, err
	}
	ds := make([]uintptr, 0, len(qs))
	defer func() {
		for _, d := range ds {
			l.CFRelease(d)
		}
	}()
	for _, q := range qs {
		d, err := l.dict(q)
		if err != nil {
			return 0, err
		}
		ds = append(ds, d)
	}
	return f(l, ds), nil
}

func (s secFramework) update(query, attrs []kcPair) (int32, error) {
	return s.with(func(l *secLib, d []uintptr) int32 { return l.SecItemUpdate(d[0], d[1]) }, query, attrs)
}

func (s secFramework) add(attrs []kcPair) (int32, error) {
	return s.with(func(l *secLib, d []uintptr) int32 { return l.SecItemAdd(d[0], nil) }, attrs)
}

func (s secFramework) delete(query []kcPair) (int32, error) {
	return s.with(func(l *secLib, d []uintptr) int32 { return l.SecItemDelete(d[0]) }, query)
}

func (s secFramework) copyData(query []kcPair) ([]byte, int32, error) {
	var out []byte
	var bad bool
	st, err := s.with(func(l *secLib, d []uintptr) int32 {
		var res uintptr
		st := l.SecItemCopyMatching(d[0], &res)
		if res == 0 {
			bad = st == errSecSuccess
			return st
		}
		defer l.CFRelease(res)
		if st != errSecSuccess {
			return st
		}
		if l.CFGetTypeID(res) != l.CFDataGetTypeID() {
			bad = true
			return st
		}
		if n := l.CFDataGetLength(res); n > 0 {
			out = append([]byte(nil), unsafe.Slice(l.CFDataGetBytePtr(res), n)...)
		} else {
			out = []byte{}
		}
		return st
	}, query)
	if err != nil {
		return nil, 0, err
	}
	if bad {
		return nil, 0, fmt.Errorf("%w: keychain read returned no data", ErrUnavailable)
	}
	return out, st, nil
}
