// Package toolstate is tools-state.json (v0.1.3 spec A8.5, CA 3.2): the tools'
// device-only memory — recent repositories and folders, the last language, each
// tool's last open mode and wide view. It never leaves this PC.
//
// The file is one JSON object: "version" (1) and one object per tool id (the
// tool's own section, read and written only by that tool, plus the shared
// last_open/zoomed fields of Common). Unknown keys are kept as they are. A
// missing or broken file reads as empty; every change is written at once,
// atomically, 0600.
package toolstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"sync"

	"github.com/patrache/masil/internal/config"
	"github.com/patrache/masil/internal/termhost"
)

// version is the tools-state.json format this build writes.
const version = 1

// versionKey is the top-level key holding the format version (never a section).
const versionKey = "version"

// maxBytes bounds what Load reads (a hand-edited or hostile file).
const maxBytes = 1 << 20

// Common section keys (Common): kept when a tool replaces its own section.
const (
	keyLastOpen = "last_open"
	keyZoomed   = "zoomed"
)

// File is the loaded tools-state.json. Its methods are safe for concurrent use;
// a nil File reads as empty and refuses writes.
type File struct {
	mu   sync.Mutex
	path string                     // "" = in memory only (zero config.Paths)
	top  map[string]json.RawMessage // every top-level key but version
}

// Common is the part of every tool's section the tool surface shares.
type Common struct {
	LastOpen termhost.Mode `json:"last_open,omitempty"` // the hub's last open mode for the tool
	Zoomed   *bool         `json:"zoomed,omitempty"`    // the tool's wide view (nil = its WideDefault)
}

// Load reads p.ToolsState. A missing, unreadable, oversized or malformed file
// gives an empty File (logged, never an error).
func Load(p config.Paths) *File {
	f := &File{path: p.ToolsState, top: map[string]json.RawMessage{}}
	if f.path == "" {
		return f
	}
	fh, err := os.Open(f.path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("tools-state.json unreadable; starting empty", "err", err)
		}
		return f
	}
	defer fh.Close()
	b, err := io.ReadAll(io.LimitReader(fh, maxBytes+1))
	if err != nil || len(b) > maxBytes {
		slog.Warn("tools-state.json unreadable or too large; starting empty", "err", err, "bytes", len(b))
		return f
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(b, &top); err != nil || top == nil {
		slog.Warn("tools-state.json is not a JSON object; starting empty", "err", err)
		return f
	}
	delete(top, versionKey)
	f.top = top
	return f
}

// section returns tool's section as an object (nil when missing or not an object).
func (f *File) section(tool string) map[string]json.RawMessage {
	raw, ok := f.top[tool]
	if !ok {
		return nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return nil
	}
	return obj
}

// Section decodes tool's section into v; false when it is missing or does not decode.
func (f *File) Section(tool string, v any) bool {
	if f == nil {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	obj := f.section(tool)
	if obj == nil {
		return false
	}
	b, err := json.Marshal(obj)
	if err != nil {
		return false
	}
	return json.Unmarshal(b, v) == nil
}

// SetSection replaces tool's own fields with v (a value encoding to a JSON object)
// and saves the file. The Common fields stay unless v sets them.
func (f *File) SetSection(tool string, v any) error {
	obj, err := object(v)
	if err != nil {
		return fmt.Errorf("toolstate: %s: %w", tool, err)
	}
	return f.update(tool, func(old map[string]json.RawMessage) map[string]json.RawMessage {
		for _, k := range []string{keyLastOpen, keyZoomed} {
			if _, set := obj[k]; !set {
				if raw, ok := old[k]; ok {
					obj[k] = raw
				}
			}
		}
		return obj
	})
}

// Common returns tool's shared fields (zero when missing).
func (f *File) Common(tool string) Common {
	var c Common
	if f == nil {
		return c
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	obj := f.section(tool)
	if raw, ok := obj[keyLastOpen]; ok {
		_ = json.Unmarshal(raw, &c.LastOpen)
	}
	if raw, ok := obj[keyZoomed]; ok {
		var z bool
		if json.Unmarshal(raw, &z) == nil {
			c.Zoomed = &z
		}
	}
	return c
}

// SetCommon sets tool's shared fields (an empty LastOpen or nil Zoomed removes
// that field) and saves the file; the tool's own fields stay.
func (f *File) SetCommon(tool string, c Common) error {
	return f.update(tool, func(old map[string]json.RawMessage) map[string]json.RawMessage {
		obj := maps.Clone(old)
		if obj == nil {
			obj = map[string]json.RawMessage{}
		}
		delete(obj, keyLastOpen)
		delete(obj, keyZoomed)
		if c.LastOpen != "" {
			obj[keyLastOpen], _ = json.Marshal(c.LastOpen)
		}
		if c.Zoomed != nil {
			obj[keyZoomed], _ = json.Marshal(*c.Zoomed)
		}
		return obj
	})
}

// update rewrites tool's section with fn(old section) and saves.
func (f *File) update(tool string, fn func(old map[string]json.RawMessage) map[string]json.RawMessage) error {
	if f == nil {
		return errors.New("toolstate: no file")
	}
	if tool == "" || tool == versionKey {
		return fmt.Errorf("toolstate: bad section name %q", tool)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b, err := json.Marshal(fn(f.section(tool)))
	if err != nil {
		return fmt.Errorf("toolstate: encode %s: %w", tool, err)
	}
	f.top[tool] = b
	return f.save()
}

// save writes the whole file (f.mu held).
func (f *File) save() error {
	if f.path == "" {
		return nil
	}
	out := maps.Clone(f.top)
	out[versionKey] = json.RawMessage(fmt.Sprint(version))
	b, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("toolstate: encode: %w", err)
	}
	return config.WriteFileAtomic(f.path, append(b, '\n'), 0o600)
}

// object encodes v as a JSON object.
func object(v any) (map[string]json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(b, &obj); err != nil || obj == nil {
		return nil, errors.New("section is not a JSON object")
	}
	return obj, nil
}
