package secrets

import (
	"context"
	"errors"
	"fmt"
	"strconv"
)

// Windows Credential Manager layout (A9.2, chunking [빌더 재량]): one generic
// credential "masil/<account>" holds a value of up to credMaxBlob bytes; a
// longer value goes into chunks "masil/<account>#0" … and the head item holds
// the chunk count (comment credChunked). The chunk logic is portable so it is
// tested on every OS; backend_windows.go is the credAPI over advapi32.
const (
	credMaxBlob   = 2560 // CRED_MAX_CREDENTIAL_BLOB_SIZE (5 * 512)
	credMaxChunks = 64
	credChunked   = "masil:chunked"
)

// credAPI is the generic-credential subset of advapi32 (fakes in tests). read
// and delete return ErrNotFound for a missing target.
type credAPI interface {
	write(target, comment string, blob []byte) error
	read(target string) (comment string, blob []byte, err error)
	delete(target string) error
}

// credman is the Credential Manager OSBackend.
type credman struct{ api credAPI }

func (credman) Name() Backend { return BackendCredMan }

func credTarget(service, account string) string { return service + "/" + account }

func credChunk(head string, i int) string { return head + "#" + strconv.Itoa(i) }

// chunks is the chunk count of head (0 = a single item or missing).
func (c credman) chunks(head string) int {
	comment, blob, err := c.api.read(head)
	if err != nil || comment != credChunked {
		return 0
	}
	n, err := strconv.Atoi(string(blob))
	if err != nil || n < 0 || n > credMaxChunks {
		return 0
	}
	return n
}

func (c credman) Set(_ context.Context, service, account, value string) error {
	head := credTarget(service, account)
	old := c.chunks(head)
	data := []byte(value)
	defer clear(data)
	n := 0
	if len(data) <= credMaxBlob {
		if err := c.api.write(head, "", data); err != nil {
			return err
		}
	} else {
		n = (len(data) + credMaxBlob - 1) / credMaxBlob
		if n > credMaxChunks {
			return fmt.Errorf("secrets: credential manager: value too large (%d bytes)", len(data))
		}
		// Chunks first, then the head: a reader never follows a head to missing chunks.
		for i := range n {
			if err := c.api.write(credChunk(head, i), "", data[i*credMaxBlob:min(len(data), (i+1)*credMaxBlob)]); err != nil {
				return err
			}
		}
		if err := c.api.write(head, credChunked, []byte(strconv.Itoa(n))); err != nil {
			return err
		}
	}
	for i := n; i < old; i++ {
		_ = c.api.delete(credChunk(head, i))
	}
	return nil
}

func (c credman) Get(_ context.Context, service, account string) (string, error) {
	head := credTarget(service, account)
	comment, blob, err := c.api.read(head)
	if err != nil {
		return "", err
	}
	if comment != credChunked {
		return string(blob), nil
	}
	n, err := strconv.Atoi(string(blob))
	if err != nil || n < 1 || n > credMaxChunks {
		return "", errors.New("secrets: credential manager: broken chunked value")
	}
	var out []byte
	for i := range n {
		_, part, err := c.api.read(credChunk(head, i))
		if err != nil {
			if errors.Is(err, ErrNotFound) {
				return "", errors.New("secrets: credential manager: a chunk is missing")
			}
			return "", err
		}
		out = append(out, part...)
	}
	return string(out), nil
}

func (c credman) Delete(_ context.Context, service, account string) error {
	head := credTarget(service, account)
	n := c.chunks(head)
	for i := range n {
		if err := c.api.delete(credChunk(head, i)); err != nil && !errors.Is(err, ErrNotFound) {
			return err
		}
	}
	return c.api.delete(head)
}
