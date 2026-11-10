// Package hooks is the observe-only receiver for program hooks (claude
// Notification, PermissionRequest and Stop over HTTP). It listens on
// 127.0.0.1 with a fresh random token per run, never answers with a
// decision (always an empty 200) and never logs bodies or headers.
package hooks

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	eventBuffer = 64
	maxBody     = 64 << 10
	maxTermLen  = 64
)

// EventKind is what the program reported.
type EventKind int

const (
	// EventAttention: waiting for the user (Notification, PermissionRequest).
	EventAttention EventKind = iota
	// EventStop: the program finished its turn.
	EventStop
)

// Event is one hook call for the terminal Term.
type Event struct {
	Term string
	Kind EventKind
}

// Receiver is a running hook endpoint.
type Receiver struct {
	ln    net.Listener
	srv   *http.Server
	token string
	port  int

	mu     sync.Mutex
	closed bool
	events chan Event
	once   sync.Once
}

// Listen starts a receiver on 127.0.0.1 at a random port.
func Listen() (*Receiver, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	r := &Receiver{
		ln:     ln,
		token:  hex.EncodeToString(raw[:]),
		port:   ln.Addr().(*net.TCPAddr).Port,
		events: make(chan Event, eventBuffer),
	}
	r.srv = &http.Server{
		Handler:           http.HandlerFunc(r.serve),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		ErrorLog:          log.New(io.Discard, "", 0),
	}
	go func() { _ = r.srv.Serve(ln) }()
	return r, nil
}

// URL is the hook address for one terminal.
func (r *Receiver) URL(term string) string {
	return "http://127.0.0.1:" + strconv.Itoa(r.port) + "/h/" + r.token + "/" + term
}

// Events delivers hook calls; when 64 are waiting new ones are dropped.
func (r *Receiver) Events() <-chan Event { return r.events }

// Close stops the server and closes Events. Safe to call twice.
func (r *Receiver) Close() error {
	var err error
	r.once.Do(func() {
		err = r.srv.Close()
		r.mu.Lock()
		r.closed = true
		close(r.events)
		r.mu.Unlock()
	})
	return err
}

func (r *Receiver) serve(w http.ResponseWriter, req *http.Request) {
	term, ok := r.route(req)
	if !ok {
		http.NotFound(w, req)
		return
	}
	switch eventName(io.LimitReader(req.Body, maxBody)) {
	case "Notification", "PermissionRequest":
		r.deliver(Event{Term: term, Kind: EventAttention})
	case "Stop":
		r.deliver(Event{Term: term, Kind: EventStop})
	}
	w.WriteHeader(http.StatusOK)
}

// eventName streams the top-level object up to its hook_event_name key and
// returns that string, skipping the values before it. The body is cut at
// 64 KiB, so a whole-body parse would fail for a large tool input; claude
// sends the name before it. "" when the key is missing, not a string, or
// past the cut.
func eventName(body io.Reader) string {
	dec := json.NewDecoder(body)
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return ""
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		key, _ := tok.(string)
		if key == "hook_event_name" {
			var name string
			if dec.Decode(&name) != nil {
				return ""
			}
			return name
		}
		if dec.Decode(&json.RawMessage{}) != nil {
			return ""
		}
	}
	return ""
}

func (r *Receiver) route(req *http.Request) (string, bool) {
	if req.Method != http.MethodPost {
		return "", false
	}
	rest, ok := strings.CutPrefix(req.URL.Path, "/h/")
	if !ok {
		return "", false
	}
	token, term, ok := strings.Cut(rest, "/")
	if !ok || subtle.ConstantTimeCompare([]byte(token), []byte(r.token)) != 1 {
		return "", false
	}
	if term == "" || len(term) > maxTermLen || strings.ContainsFunc(term, func(c rune) bool {
		return !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.')
	}) {
		return "", false
	}
	return term, true
}

func (r *Receiver) deliver(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	select {
	case r.events <- ev:
	default:
	}
}
