package secrets

import "context"

// noOS is the OS store of a platform without one: every call is ErrUnavailable,
// so Put asks for consent and Get falls through to the file.
type noOS struct{}

func (noOS) Name() Backend { return "" }

func (noOS) Set(context.Context, string, string, string) error { return ErrUnavailable }

func (noOS) Get(context.Context, string, string) (string, error) { return "", ErrUnavailable }

func (noOS) Delete(context.Context, string, string) error { return ErrUnavailable }
