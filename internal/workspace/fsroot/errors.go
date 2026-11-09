package fsroot

// Reason says why Check rejected a path.
type Reason int

const (
	// ReasonEscape: the path, or a link it passes through, leaves the folder.
	ReasonEscape Reason = iota + 1
	// ReasonAbsolute: the path is absolute ("/x", "C:x").
	ReasonAbsolute
	// ReasonDenied: the path, or the real target of a link, is on the deny list.
	ReasonDenied
	// ReasonIgnored: the path is hidden from peers by .gitignore/.masilignore.
	ReasonIgnored
	// ReasonInvalid: empty, NUL, backslash, or a link loop.
	ReasonInvalid
)

// String is a stable lowercase name used in audit records.
func (r Reason) String() string {
	switch r {
	case ReasonEscape:
		return "escape"
	case ReasonAbsolute:
		return "absolute"
	case ReasonDenied:
		return "denied"
	case ReasonIgnored:
		return "ignored"
	case ReasonInvalid:
		return "invalid"
	}
	return "unknown"
}

// PathError is a rejected path. Error() never contains Rel so the message is
// safe to log; callers that need the path (audit) read the field directly.
type PathError struct {
	Rel    string
	Reason Reason
}

func (e *PathError) Error() string { return "fsroot: path rejected (" + e.Reason.String() + ")" }

func reject(rel string, reason Reason) error { return &PathError{Rel: rel, Reason: reason} }
