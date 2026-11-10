package lsp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// maxFrame bounds one incoming message (a completion list from a big
// workspace is a few MB at most).
const maxFrame = 64 << 20

// JSON-RPC error codes used here.
const (
	codeMethodNotFound   = -32601
	codeRequestCancelled = -32800
)

// message is any JSON-RPC 2.0 message as read from the wire: a request
// (Method and ID), a notification (Method only) or a response (ID and
// Result or Error).
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// rpcError is a JSON-RPC error object.
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("lsp: server error %d: %s", e.Code, e.Message) }

// outRequest, outNotification and outResponse are the messages masil sends.
type outRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outNotification struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type outResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result"`
	Error   *rpcError       `json:"error,omitempty"`
}

var (
	// errFrame reports a header that cannot be read: where the body ends is
	// unknown, so the stream is out of sync for good.
	errFrame = errors.New("lsp: bad frame")
	// errBody reports a frame whose body was consumed whole (or skipped, when
	// longer than the limit) but is unusable: the next frame reads fine.
	errBody = errors.New("lsp: bad message body")
)

// readFrame reads one Content-Length framed body of at most maxFrame bytes.
func readFrame(r *bufio.Reader) ([]byte, error) { return readFrameLimit(r, maxFrame) }

// readFrameLimit reads one Content-Length framed body. Header names are
// matched case-insensitively and other headers (Content-Type) are ignored.
// A body longer than limit is skipped (errBody).
func readFrameLimit(r *bufio.Reader, limit int) ([]byte, error) {
	length := int64(-1)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" && length < 0 {
				return nil, io.EOF
			}
			if err == io.EOF {
				return nil, io.ErrUnexpectedEOF
			}
			return nil, err
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if length < 0 {
				continue // stray blank line between frames
			}
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("%w: header %q", errFrame, line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil || n < 0 {
				return nil, fmt.Errorf("%w: Content-Length %q", errFrame, value)
			}
			length = n
		}
	}
	if length > int64(limit) {
		n, err := io.CopyN(io.Discard, r, length)
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return nil, err
		}
		return nil, fmt.Errorf("%w: %d bytes skipped (limit %d)", errBody, n, limit)
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return body, nil
}

// readMessage reads and decodes one message of at most maxFrame bytes.
func readMessage(r *bufio.Reader) (*message, error) { return readMessageLimit(r, maxFrame) }

func readMessageLimit(r *bufio.Reader, limit int) (*message, error) {
	body, err := readFrameLimit(r, limit)
	if err != nil {
		return nil, err
	}
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("%w: %v", errBody, err)
	}
	return &m, nil
}

// encodeFrame marshals v and puts the Content-Length header in front.
func encodeFrame(v any) ([]byte, error) {
	body, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var b bytes.Buffer
	b.Grow(len(body) + 32)
	b.WriteString("Content-Length: ")
	b.WriteString(strconv.Itoa(len(body)))
	b.WriteString("\r\n\r\n")
	b.Write(body)
	return b.Bytes(), nil
}

// isNull reports whether raw is absent or the JSON null.
func isNull(raw json.RawMessage) bool {
	t := bytes.TrimSpace(raw)
	return len(t) == 0 || bytes.Equal(t, []byte("null"))
}
