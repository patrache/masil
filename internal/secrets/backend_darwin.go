package secrets

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
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
type keychain struct{}

func (keychain) Name() Backend { return BackendKeychain }

func (keychain) Set(ctx context.Context, service, account, value string) error {
	for _, s := range []string{service, account, value} {
		if strings.ContainsAny(s, "\n\r\x00") {
			return errors.New("secrets: keychain: a line break or NUL cannot be stored")
		}
	}
	line := "add-generic-password -U -s " + secArg(service) + " -a " + secQuote(account) + " -w " + secQuote(value) + "\n"
	_, err := runSecurity(ctx, strings.NewReader(line), "-i")
	return err
}

func (keychain) Get(ctx context.Context, service, account string) (string, error) {
	out, err := runSecurity(ctx, nil, "find-generic-password", "-s", service, "-a", account, "-w")
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

func (keychain) Delete(ctx context.Context, service, account string) error {
	_, err := runSecurity(ctx, nil, "delete-generic-password", "-s", service, "-a", account)
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
