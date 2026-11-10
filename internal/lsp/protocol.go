package lsp

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"
)

// Position is a place in a document: zero-based line and Character in UTF-16
// code units (the LSP default encoding). See UTF16Col / RuneCol.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

// Range is a half-open span of a document.
type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

// TextEdit replaces Range with NewText.
type TextEdit struct {
	Range   Range  `json:"range"`
	NewText string `json:"newText"`
}

// CompletionItemKind is the LSP CompletionItemKind (1 Text … 25 TypeParameter).
type CompletionItemKind int

// The LSP completion item kinds.
const (
	KindText CompletionItemKind = iota + 1
	KindMethod
	KindFunction
	KindConstructor
	KindField
	KindVariable
	KindClass
	KindInterface
	KindModule
	KindProperty
	KindUnit
	KindValue
	KindEnum
	KindKeyword
	KindSnippet
	KindColor
	KindFile
	KindReference
	KindFolder
	KindEnumMember
	KindConstant
	KindStruct
	KindEvent
	KindOperator
	KindTypeParameter
)

var kindNames = [...]string{"", "text", "method", "function", "constructor", "field", "variable",
	"class", "interface", "module", "property", "unit", "value", "enum", "keyword", "snippet",
	"color", "file", "reference", "folder", "enumMember", "constant", "struct", "event",
	"operator", "typeParameter"}

// String is the protocol name of k ("function", "variable" …), "" when unknown.
func (k CompletionItemKind) String() string {
	if k < 0 || int(k) >= len(kindNames) {
		return ""
	}
	return kindNames[k]
}

// Item is one completion candidate.
type Item struct {
	Label string
	// InsertText is the text to insert: the edit's new text when Edit is set,
	// else the server's insertText, else Label. Snippet syntax ($1, ${2:x})
	// is stripped should a server send it although masil asks for plain text.
	InsertText string
	Kind       CompletionItemKind
	Detail     string
	// FilterText is what the typed prefix is matched against (Label when the
	// server sends none); SortText orders items (Label when none).
	FilterText string
	SortText   string
	// Edit, when set, is the server's exact replacement: Edit.Range (UTF-16
	// columns, on the cursor's line in practice) is replaced by
	// Edit.NewText (== InsertText). When nil the caller replaces the word
	// before the cursor with InsertText.
	Edit *TextEdit
}

// List is a completion answer. Incomplete means the server did not send every
// match: ask again when the user types more instead of only filtering Items.
type List struct {
	Items      []Item
	Incomplete bool
}

// maxItems caps one answer (the menu shows a handful; a big project can send thousands).
const maxItems = 1000

// wireItem is the part of CompletionItem masil reads.
type wireItem struct {
	Label            string             `json:"label"`
	Kind             CompletionItemKind `json:"kind"`
	Detail           string             `json:"detail"`
	SortText         string             `json:"sortText"`
	FilterText       string             `json:"filterText"`
	InsertText       string             `json:"insertText"`
	InsertTextFormat int                `json:"insertTextFormat"`
	TextEdit         json.RawMessage    `json:"textEdit"`
}

// wireEdit is a TextEdit or an InsertReplaceEdit (masil does not ask for the
// latter, but takes its insert range if a server sends one).
type wireEdit struct {
	Range   *Range `json:"range"`
	Insert  *Range `json:"insert"`
	NewText string `json:"newText"`
}

const insertTextFormatSnippet = 2

// parseCompletion decodes a textDocument/completion result: null, a
// CompletionItem[] or a CompletionList. Items come back sorted by SortText
// (stable, so equal keys keep the server's order).
func parseCompletion(raw json.RawMessage) (List, error) {
	if isNull(raw) {
		return List{}, nil
	}
	var wire []wireItem
	var incomplete bool
	trimmed := strings.TrimSpace(string(raw))
	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(raw, &wire); err != nil {
			return List{}, err
		}
	} else {
		var l struct {
			IsIncomplete bool       `json:"isIncomplete"`
			Items        []wireItem `json:"items"`
		}
		if err := json.Unmarshal(raw, &l); err != nil {
			return List{}, err
		}
		wire, incomplete = l.Items, l.IsIncomplete
	}
	if len(wire) > maxItems {
		wire, incomplete = wire[:maxItems], true
	}
	out := List{Incomplete: incomplete, Items: make([]Item, 0, len(wire))}
	for _, w := range wire {
		if w.Label == "" {
			continue
		}
		it := Item{Label: w.Label, Kind: w.Kind, Detail: w.Detail, FilterText: w.FilterText, SortText: w.SortText}
		snippet := w.InsertTextFormat == insertTextFormatSnippet
		var e wireEdit
		if !isNull(w.TextEdit) && json.Unmarshal(w.TextEdit, &e) == nil {
			r := e.Range
			if r == nil {
				r = e.Insert
			}
			if r != nil {
				text := e.NewText
				if snippet {
					text = stripSnippet(text)
				}
				it.Edit = &TextEdit{Range: *r, NewText: text}
				it.InsertText = text
			}
		}
		if it.Edit == nil {
			it.InsertText = w.InsertText
			if it.InsertText == "" {
				it.InsertText = w.Label
			}
			if snippet {
				it.InsertText = stripSnippet(it.InsertText)
			}
		}
		if it.FilterText == "" {
			it.FilterText = it.Label
		}
		if it.SortText == "" {
			it.SortText = it.Label
		}
		out.Items = append(out.Items, it)
	}
	slices.SortStableFunc(out.Items, func(a, b Item) int { return cmp.Compare(a.SortText, b.SortText) })
	return out, nil
}

// stripSnippet turns LSP snippet syntax into plain text: $1 and $0 vanish,
// ${1:name} becomes name, ${1|a,b|} becomes a, \$ \} \\ unescape.
func stripSnippet(s string) string {
	if !strings.ContainsAny(s, `$\`) {
		return s
	}
	var b strings.Builder
	rs := []rune(s)
	var walk func(i int, stopAtBrace bool) int
	walk = func(i int, stopAtBrace bool) int {
		for i < len(rs) {
			r := rs[i]
			switch {
			case r == '\\' && i+1 < len(rs) && strings.ContainsRune(`$}\,|`, rs[i+1]):
				b.WriteRune(rs[i+1])
				i += 2
			case r == '}' && stopAtBrace:
				return i + 1
			case r == '$' && i+1 < len(rs) && isDigit(rs[i+1]):
				i++
				for i < len(rs) && isDigit(rs[i]) {
					i++
				}
			case r == '$' && i+1 < len(rs) && rs[i+1] == '{':
				j := i + 2
				for j < len(rs) && (isDigit(rs[j]) || isWordRune(rs[j])) {
					j++
				}
				switch {
				case j < len(rs) && rs[j] == ':':
					i = walk(j+1, true)
				case j < len(rs) && rs[j] == '|':
					k := j + 1
					for k < len(rs) && rs[k] != ',' && rs[k] != '|' {
						b.WriteRune(rs[k])
						k++
					}
					for k < len(rs) && rs[k] != '}' {
						k++
					}
					i = k + 1
				case j < len(rs) && rs[j] == '}':
					i = j + 1
				default:
					b.WriteRune(r)
					i++
				}
			default:
				b.WriteRune(r)
				i++
			}
		}
		return i
	}
	walk(0, false)
	return b.String()
}

func isDigit(r rune) bool    { return r >= '0' && r <= '9' }
func isWordRune(r rune) bool { return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') }
