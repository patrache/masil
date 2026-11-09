package secrets

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// securityPath is the macOS keychain tool (tests swap in a fake script).
var securityPath = "/usr/bin/security"

// security exit codes (the low byte of the OSStatus).
const secExitNotFound = 44 // errSecItemNotFound

// defaultOS is the login keychain through /usr/bin/security.
func defaultOS() OSBackend { return keychain{} }

// keychain stores generic passwords with /usr/bin/security (no cgo). A value is
// written through the interactive mode's standard input, never as an argument
// (arguments are visible to every user in ps). Reading prints the value on the
// tool's standard output.
//
// Layout (A9.2, like the Credential Manager's chunks [빌더 재량]): the item
// holds secEncoded + base64(value) — `security -w` prints a value with a byte
// outside printable ASCII as hex, and base64 keeps any value on one quoted
// line. The interactive mode cuts a line at ~4 KiB and runs the rest as another
// command, so a command line is never longer than secMaxLine: a longer value
// goes into items "<account>#0" … of service "<service>.chunk" and the head
// item holds secChunked + the piece count.
type keychain struct{}

const (
	secMaxLine   = 4000 // the real tool cuts at ~4096 (probed with read-only commands)
	secMaxChunks = 64
	secEncoded   = "masil:b64:"
	secChunked   = "masil:chunks:"
)

func (keychain) Name() Backend { return BackendKeychain }

func secChunkService(service string) string { return service + ".chunk" }

func secChunkAccount(account string, i int) string { return account + "#" + strconv.Itoa(i) }

// secAddLine is the interactive-mode command that stores raw (already safe:
// base64 or the chunk marker) under service/account.
func secAddLine(service, account, raw string) string {
	return "add-generic-password -U -s " + secArg(service) + " -a " + secQuote(account) + " -w " + secQuote(raw) + "\n"
}

func (k keychain) Set(ctx context.Context, service, account, value string) error {
	for _, s := range []string{service, account} {
		if strings.ContainsAny(s, "\n\r\x00") {
			return errors.New("secrets: keychain: a line break or NUL cannot be in a name")
		}
	}
	enc := secEncoded + base64.StdEncoding.EncodeToString([]byte(value))
	if line := secAddLine(service, account, enc); len(line) <= secMaxLine {
		old, err := k.chunks(ctx, service, account)
		if err != nil {
			return err
		}
		if err := k.add(ctx, line); err != nil {
			return err
		}
		k.dropChunks(ctx, service, account, 0, old)
		return nil
	}
	room := secMaxLine - len(secAddLine(secChunkService(service), secChunkAccount(account, secMaxChunks), ""))
	if room < 512 {
		return errors.New("secrets: keychain: the name is too long")
	}
	n := (len(enc) + room - 1) / room
	if n > secMaxChunks {
		return fmt.Errorf("secrets: keychain: value too large (%d bytes)", len(value))
	}
	old, err := k.chunks(ctx, service, account)
	if err != nil {
		return err
	}
	// Pieces first, then the head: a reader never follows a head to missing pieces.
	for i := range n {
		piece := enc[i*room : min(len(enc), (i+1)*room)]
		if err := k.add(ctx, secAddLine(secChunkService(service), secChunkAccount(account, i), piece)); err != nil {
			return err
		}
	}
	if err := k.add(ctx, secAddLine(service, account, secChunked+strconv.Itoa(n))); err != nil {
		return err
	}
	k.dropChunks(ctx, service, account, n, old)
	return nil
}

// add runs one interactive-mode command (one line, one process: the exit code
// is that command's).
func (keychain) add(ctx context.Context, line string) error {
	_, err := runSecurity(ctx, strings.NewReader(line), "-i")
	return err
}

// raw is the item's stored text.
func (keychain) raw(ctx context.Context, service, account string) (string, error) {
	out, err := runSecurity(ctx, nil, "find-generic-password", "-s", service, "-a", account, "-w")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// chunks is the piece count of the head item (0 = a single item or none).
func (k keychain) chunks(ctx context.Context, service, account string) (int, error) {
	head, err := k.raw(ctx, service, account)
	if errors.Is(err, ErrNotFound) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	n, ok := secChunkCount(head)
	if !ok {
		return 0, nil
	}
	return n, nil
}

func secChunkCount(head string) (int, bool) {
	s, ok := strings.CutPrefix(head, secChunked)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 1 || n > secMaxChunks {
		return 0, false
	}
	return n, true
}

// dropChunks deletes pieces from..to-1 (left over from a longer old value).
func (keychain) dropChunks(ctx context.Context, service, account string, from, to int) {
	for i := from; i < to; i++ {
		_, _ = runSecurity(ctx, nil, "delete-generic-password", "-s", secChunkService(service), "-a", secChunkAccount(account, i))
	}
}

func (k keychain) Get(ctx context.Context, service, account string) (string, error) {
	head, err := k.raw(ctx, service, account)
	if err != nil {
		return "", err
	}
	if strings.HasPrefix(head, secChunked) {
		n, ok := secChunkCount(head)
		if !ok {
			return "", errors.New("secrets: keychain: broken chunked value")
		}
		var b strings.Builder
		for i := range n {
			piece, err := k.raw(ctx, secChunkService(service), secChunkAccount(account, i))
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					return "", errors.New("secrets: keychain: a piece is missing")
				}
				return "", err
			}
			b.WriteString(piece)
		}
		head = b.String()
	}
	enc, ok := strings.CutPrefix(head, secEncoded)
	if !ok {
		return head, nil // a plain item (not written by this layout)
	}
	v, err := base64.StdEncoding.DecodeString(enc)
	if err != nil {
		return "", errors.New("secrets: keychain: broken encoded value")
	}
	return string(v), nil
}

func (k keychain) Delete(ctx context.Context, service, account string) error {
	n, err := k.chunks(ctx, service, account)
	if err != nil {
		return err
	}
	for i := range n {
		_, err := runSecurity(ctx, nil, "delete-generic-password", "-s", secChunkService(service), "-a", secChunkAccount(account, i))
		if err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	_, err = runSecurity(ctx, nil, "delete-generic-password", "-s", service, "-a", account)
	return err
}

// runSecurity runs the tool and maps its exit code: 44 = ErrNotFound, any other
// failure (denied, locked, canceled, no keychain, no tool) = ErrUnavailable.
// The tool's output is never put in an error.
func runSecurity(ctx context.Context, stdin *strings.Reader, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, securityPath, args...)
	if stdin != nil {
		cmd.Stdin = stdin
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err == nil {
		return out.Bytes(), nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ee.ExitCode() == secExitNotFound {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("%w: keychain (security exit %d)", ErrUnavailable, ee.ExitCode())
	}
	return nil, fmt.Errorf("%w: keychain: %v", ErrUnavailable, err)
}

// secQuote quotes s for security's interactive mode: single quotes, with \ and
// ' escaped by a backslash.
func secQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// secArg leaves a plain word (the service name) bare and quotes anything else.
func secArg(s string) string {
	if s != "" && !strings.ContainsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_')
	}) {
		return s
	}
	return secQuote(s)
}
