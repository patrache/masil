package secrets

import (
	"context"
	"errors"
	"fmt"
)

// macOS keychain (A9.2): one generic-password item of the login keychain per
// value (service, account), written and read in this process through
// Security.framework's SecItem calls (backend_darwin.go loads the frameworks
// with purego; no cgo, no child process). The item holds the value's bytes as
// they are: no encoding, no chunks.
//
// Why in process: an item is created with an access list that trusts the
// program that created it. Created by /usr/bin/security it trusts that tool,
// so any program of the same user could read it silently with
// `security find-generic-password -w`; created here it trusts masil, and any
// other program gets the keychain's access prompt. The keychain knows masil by
// its binary, so after an upgrade the prompt can come back once ("항상 허용").
//
// The logic here (queries, status mapping, update-or-add, context handling) is
// portable and tested on every OS with a fake secAPI.

// kcMaxValue is the largest value Set stores — the same as the Credential
// Manager's 64 chunks of 2,560 bytes, so a value fits every OS store.
const kcMaxValue = credMaxChunks * credMaxBlob

// OSStatus values of the SecItem calls (Security/SecBase.h).
const (
	errSecSuccess               int32 = 0
	errSecUserCanceled          int32 = -128   // the user canceled the prompt
	errSecAuthFailed            int32 = -25293 // the user denied access (or a wrong password)
	errSecDuplicateItem         int32 = -25299
	errSecItemNotFound          int32 = -25300
	errSecInteractionNotAllowed int32 = -25308 // a prompt was needed but cannot be shown (no GUI session, SSH)
)

// errKCDenied marks a refusal: the prompt was canceled or denied, or could not
// be shown. It is always ErrUnavailable as well, so the store asks for the
// fallback consent instead of prompting again.
var errKCDenied = errors.New("keychain access denied or not allowed")

// kcErr maps the OSStatus of op. The value is never in the message.
func kcErr(op string, st int32) error {
	switch st {
	case errSecSuccess:
		return nil
	case errSecItemNotFound:
		return ErrNotFound
	case errSecUserCanceled, errSecAuthFailed, errSecInteractionNotAllowed:
		return fmt.Errorf("%w: %w (%s, OSStatus %d)", ErrUnavailable, errKCDenied, op, st)
	default:
		return fmt.Errorf("%w: keychain %s failed (OSStatus %d)", ErrUnavailable, op, st)
	}
}

// kcName is a Security or CoreFoundation constant used in a SecItem query.
type kcName int

// Constants (kcSymbols names each one's symbol).
const (
	kcClass kcName = iota
	kcClassGenericPassword
	kcAttrService
	kcAttrAccount
	kcAttrLabel
	kcValueData
	kcReturnData
	kcMatchLimit
	kcMatchLimitOne
	kcTrue
	kcNames // the count
)

// The frameworks a constant comes from.
const (
	libSecurity       = "Security"
	libCoreFoundation = "CoreFoundation"
)

type kcSymbol struct{ lib, name string }

var kcSymbols = [kcNames]kcSymbol{
	kcClass:                {libSecurity, "kSecClass"},
	kcClassGenericPassword: {libSecurity, "kSecClassGenericPassword"},
	kcAttrService:          {libSecurity, "kSecAttrService"},
	kcAttrAccount:          {libSecurity, "kSecAttrAccount"},
	kcAttrLabel:            {libSecurity, "kSecAttrLabel"},
	kcValueData:            {libSecurity, "kSecValueData"},
	kcReturnData:           {libSecurity, "kSecReturnData"},
	kcMatchLimit:           {libSecurity, "kSecMatchLimit"},
	kcMatchLimitOne:        {libSecurity, "kSecMatchLimitOne"},
	kcTrue:                 {libCoreFoundation, "kCFBooleanTrue"},
}

// kcPair is one dictionary entry; val is a kcName, a string or a []byte.
type kcPair struct {
	key kcName
	val any
}

// kcItem selects the generic password (service, account).
func kcItem(service, account string) []kcPair {
	return []kcPair{{kcClass, kcClassGenericPassword}, {kcAttrService, service}, {kcAttrAccount, account}}
}

// kcAddAttrs is a new item: kcItem, the label Keychain Access shows (the
// service, as /usr/bin/security did) and the value.
func kcAddAttrs(service, account string, value []byte) []kcPair {
	return append(kcItem(service, account), kcPair{kcAttrLabel, service}, kcPair{kcValueData, value})
}

// kcValueAttrs is what an update changes: the value only.
func kcValueAttrs(value []byte) []kcPair { return []kcPair{{kcValueData, value}} }

// kcGetQuery asks for the value of one item.
func kcGetQuery(service, account string) []kcPair {
	return append(kcItem(service, account), kcPair{kcReturnData, kcTrue}, kcPair{kcMatchLimit, kcMatchLimitOne})
}

// secAPI is the SecItem subset (fakes in tests). Each call returns the
// OSStatus; err is a failure before the call (the frameworks did not load —
// ErrUnavailable — or a name cannot be a CFString).
type secAPI interface {
	update(query, attrs []kcPair) (int32, error)
	add(attrs []kcPair) (int32, error)
	copyData(query []kcPair) ([]byte, int32, error)
	delete(query []kcPair) (int32, error)
}

// keychain is the macOS keychain OSBackend.
type keychain struct {
	api  secAPI
	slot chan struct{} // capacity 1: one SecItem call at a time (kcCall)
}

func (keychain) Name() Backend { return BackendKeychain }

// Set updates the item in place, or adds it when there is none. It never
// deletes first, so there is no moment without a value. An add that finds the
// item (another writer added it in between) updates again.
func (k keychain) Set(ctx context.Context, service, account, value string) error {
	if len(value) > kcMaxValue {
		return fmt.Errorf("secrets: keychain: value too large (%d bytes)", len(value))
	}
	data := []byte(value)
	_, err := kcCall(ctx, k.slot, func() (struct{}, error) {
		defer clear(data)
		return struct{}{}, k.set(service, account, data)
	})
	return err
}

func (k keychain) set(service, account string, data []byte) error {
	for range 2 {
		st, err := k.api.update(kcItem(service, account), kcValueAttrs(data))
		if err != nil {
			return err
		}
		if st != errSecItemNotFound {
			return kcErr("update", st)
		}
		if st, err = k.api.add(kcAddAttrs(service, account, data)); err != nil {
			return err
		}
		if st != errSecDuplicateItem {
			return kcErr("add", st)
		}
	}
	return kcErr("add", errSecDuplicateItem)
}

func (k keychain) Get(ctx context.Context, service, account string) (string, error) {
	return kcCall(ctx, k.slot, func() (string, error) {
		b, st, err := k.api.copyData(kcGetQuery(service, account))
		if err != nil {
			return "", err
		}
		if err := kcErr("read", st); err != nil {
			return "", err
		}
		v := string(b)
		clear(b)
		return v, nil
	})
}

func (k keychain) Delete(ctx context.Context, service, account string) error {
	_, err := kcCall(ctx, k.slot, func() (struct{}, error) {
		st, err := k.api.delete(kcItem(service, account))
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, kcErr("delete", st)
	})
	return err
}

// kcCall runs f on its own goroutine, one call at a time (slot). A SecItem
// call can block on the keychain's prompt for as long as the prompt stays
// open, and it cannot be interrupted. When ctx ends first, kcCall returns
// ctx's error at once and f runs on in the background until the prompt is
// answered, keeping the slot: at most that one call is ever left blocked — a
// later call waits for the slot or for its own ctx, never starting a second
// blocked call. An abandoned Set can therefore still store its value when the
// prompt is answered later.
func kcCall[T any](ctx context.Context, slot chan struct{}, f func() (T, error)) (T, error) {
	var zero T
	if err := ctx.Err(); err != nil {
		return zero, fmt.Errorf("secrets: keychain: %w", err)
	}
	select {
	case slot <- struct{}{}:
	case <-ctx.Done():
		return zero, fmt.Errorf("secrets: keychain: %w", ctx.Err())
	}
	type result struct {
		v   T
		err error
	}
	done := make(chan result, 1)
	go func() {
		defer func() { <-slot }()
		v, err := f()
		done <- result{v, err}
	}()
	select {
	case r := <-done:
		return r.v, r.err
	case <-ctx.Done():
		select {
		case r := <-done: // finished as ctx ended: keep the result
			return r.v, r.err
		default:
		}
		return zero, fmt.Errorf("secrets: keychain: %w", ctx.Err())
	}
}
