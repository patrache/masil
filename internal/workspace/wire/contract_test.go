package wire

import (
	"os"
	"strconv"
	"strings"
	"testing"
)

// contractRow is one row of docs/contract/workspace.md section 3.
type contractRow struct {
	value    uint8
	name     string
	class    string
	ownerCls string // second class after "/" (doc_presence), "" otherwise
	minLevel string
}

func readContractTable(t *testing.T) []contractRow {
	t.Helper()
	data, err := os.ReadFile("../../../docs/contract/workspace.md")
	if err != nil {
		t.Fatal(err)
	}
	var rows []contractRow
	inSection := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "## ") {
			inSection = strings.HasPrefix(line, "## 3.")
			continue
		}
		if !inSection || !strings.HasPrefix(line, "| 0x") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		if len(cells) != 4 {
			t.Fatalf("bad row %q", line)
		}
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		v, err := strconv.ParseUint(strings.TrimPrefix(cells[0], "0x"), 16, 8)
		if err != nil {
			t.Fatalf("bad value %q", cells[0])
		}
		r := contractRow{value: uint8(v), name: strings.Trim(cells[1], "`")}
		classes := strings.Split(cells[2], "/")
		r.class = stripParen(classes[0])
		if len(classes) > 1 {
			r.ownerCls = stripParen(classes[1])
		}
		r.minLevel = cells[3]
		if strings.HasPrefix(r.minLevel, "—") {
			r.minLevel = "—"
		}
		rows = append(rows, r)
	}
	if len(rows) == 0 {
		t.Fatal("no kind rows found in section 3")
	}
	return rows
}

func stripParen(s string) string {
	if i := strings.Index(s, "("); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// contractLevel maps the MinLevel column to a Level.
var contractLevel = map[string]Level{
	"—": LevelNoPeer, "None": LevelNone, "Viewer": LevelViewer,
	"Prompter": LevelPrompter, "Editor": LevelEditor, "Keyboard": LevelKeyboard,
}

func TestKindTableMatchesContract(t *testing.T) {
	rows := readContractTable(t)
	seen := map[Kind]int{}
	for _, r := range rows {
		k := Kind(r.value)
		seen[k]++
		if !k.Valid() {
			t.Errorf("0x%02x %s: not a Kind in kinds.go", r.value, r.name)
			continue
		}
		if k.String() != r.name {
			t.Errorf("0x%02x: name %q, contract %q", r.value, k.String(), r.name)
		}
		if k.Class().String() != r.class {
			t.Errorf("%s: class %q, contract %q", r.name, k.Class(), r.class)
		}
		wantOwner := r.class
		if r.ownerCls != "" {
			wantOwner = r.ownerCls
		}
		if got := ClassFor(k, SeatOwner).String(); got != wantOwner {
			t.Errorf("%s: owner class %q, contract %q", r.name, got, wantOwner)
		}
		lv, ok := contractLevel[r.minLevel]
		if !ok {
			t.Errorf("%s: unknown MinLevel column %q", r.name, r.minLevel)
		} else if k.MinLevel() != lv {
			t.Errorf("%s: MinLevel %v, contract %q", r.name, k.MinLevel(), r.minLevel)
		}
	}
	for _, k := range Kinds() {
		if seen[k] != 1 {
			t.Errorf("%s appears %d times in the contract table", k, seen[k])
		}
	}
	if len(rows) != len(Kinds()) {
		t.Errorf("contract rows %d, kinds %d", len(rows), len(Kinds()))
	}
}
