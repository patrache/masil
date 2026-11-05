package config

import (
	"errors"
	"testing"
	"time"
)

var errTransient = errors.New("transient")

func isTestTransient(err error) bool { return errors.Is(err, errTransient) }

func TestRenameRetryTransientThenSuccess(t *testing.T) {
	calls := 0
	rename := func(_, _ string) error {
		calls++
		if calls < 3 {
			return errTransient
		}
		return nil
	}
	if err := renameRetry(rename, isTestTransient, "a", "b", 500*time.Millisecond); err != nil {
		t.Fatalf("renameRetry: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestRenameRetryPermanentFailsFast(t *testing.T) {
	calls := 0
	perm := errors.New("permanent")
	rename := func(_, _ string) error { calls++; return perm }
	if err := renameRetry(rename, isTestTransient, "a", "b", 500*time.Millisecond); !errors.Is(err, perm) {
		t.Fatalf("err = %v, want permanent", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1", calls)
	}
}

func TestRenameRetryGivesUpAfterBudget(t *testing.T) {
	calls := 0
	rename := func(_, _ string) error { calls++; return errTransient }
	start := time.Now()
	err := renameRetry(rename, isTestTransient, "a", "b", 50*time.Millisecond)
	if !errors.Is(err, errTransient) {
		t.Fatalf("err = %v, want transient", err)
	}
	if el := time.Since(start); el < 50*time.Millisecond || el > 2*time.Second {
		t.Fatalf("elapsed = %v, want about the 50ms budget", el)
	}
	if calls < 2 {
		t.Fatalf("calls = %d, want retries", calls)
	}
}

func TestWriteFileAtomicUsesBoundedBudget(t *testing.T) {
	if renameBudget <= 0 || renameBudget > time.Second {
		t.Fatalf("renameBudget = %v, want a short positive bound", renameBudget)
	}
}
