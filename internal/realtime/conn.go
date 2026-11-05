package realtime

import "context"

// Conn abstracts the socket for tests.
type Conn interface {
	Read(ctx context.Context) ([]byte, error)
	Write(ctx context.Context, data []byte) error
	Close(code int, reason string) error
}
