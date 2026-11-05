package main

import (
	"context"
	"errors"
	"testing"
)

func TestUsageErr(t *testing.T) {
	if !errors.Is(usageErr(nil), errUsage) {
		t.Fatal("usageErr(nil) must be errUsage")
	}
	x := errors.New("admincli: usage")
	e := usageErr(x)
	if !errors.Is(e, errUsage) || !errors.Is(e, x) {
		t.Fatalf("usageErr(x) = %v; must wrap both errUsage and x", e)
	}
	if errors.Is(errors.New("boom"), errUsage) {
		t.Fatal("an unrelated error must not be errUsage")
	}
}

func TestRegisterDuplicateNamePanics(t *testing.T) {
	run := func(context.Context, *Env, []string) error { return nil }
	register(command{Name: "zz-phase0-dup", Hidden: true, Run: run})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate command name")
		}
	}()
	register(command{Name: "zz-phase0-dup", Hidden: true, Run: run})
}
