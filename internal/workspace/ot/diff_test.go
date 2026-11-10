package ot

import (
	"math/rand"
	"strings"
	"testing"
)

func randLines(r *rand.Rand, n int) []rune {
	var b strings.Builder
	for range n {
		b.WriteString(randString(r, 8))
		b.WriteByte('\n')
	}
	return []rune(b.String())
}

// mutate applies a few random line and rune edits.
func mutate(r *rand.Rand, a []rune) []rune {
	out := append([]rune(nil), a...)
	for range r.Intn(6) {
		op := randOp(r, len(out))
		out, _ = op.Apply(out)
	}
	return out
}

func TestDiffProperty(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	for i := range 3000 {
		var a, b []rune
		switch i % 3 {
		case 0:
			a, b = randDoc(r), randDoc(r)
		case 1:
			a = randLines(r, r.Intn(40))
			b = mutate(r, a)
		default:
			a = randLines(r, r.Intn(40))
			b = randLines(r, r.Intn(40))
		}
		d := Diff(a, b)
		if err := d.validate(); err != nil {
			t.Fatalf("#%d invalid diff %v", i, d)
		}
		got, err := d.Apply(a)
		if err != nil || string(got) != string(b) {
			t.Fatalf("#%d a=%q b=%q diff=%v got=%q err=%v", i, string(a), string(b), d, string(got), err)
		}
	}
	if d := Diff(nil, nil); !d.IsNoop() || d.BaseLen() != 0 {
		t.Fatalf("empty diff %v", d)
	}
}

func countKinds(o Op) (ins, del int) {
	for _, c := range o {
		if c.isInsert() {
			ins++
		}
		if c.isDelete() {
			del++
		}
	}
	return
}

func TestDiffSmallForLocalEdit(t *testing.T) {
	var sb strings.Builder
	for i := range 10000 {
		sb.WriteString("line number ")
		sb.WriteString(strings.Repeat("x", i%17))
		sb.WriteString("\n")
	}
	a := []rune(sb.String())
	b := append([]rune(nil), a...)
	at := 0
	for line := 0; line < 5000; at++ {
		if b[at] == '\n' {
			line++
		}
	}
	b[at+3] = '한'
	d := Diff(a, b)
	if ins, del := countKinds(d); ins != 1 || del != 1 {
		t.Fatalf("diff has %d inserts, %d deletes: %v", ins, del, d)
	}
}

func TestDiffManyHunksUnderTheCap(t *testing.T) {
	var x, y strings.Builder
	for i := range 300 {
		if i%10 == 0 {
			x.WriteString("same\n")
			y.WriteString("same\n")
			continue
		}
		x.WriteString("a\n")
		y.WriteString("b\n")
	}
	a, b := []rune(x.String()), []rune(y.String())
	d := Diff(a, b)
	if got, _ := d.Apply(a); string(got) != string(b) {
		t.Fatal("diff wrong")
	}
	if ins, _ := countKinds(d); ins < 20 {
		t.Fatalf("expected a hunk per changed block, got %d inserts", ins)
	}
}

func TestDiffLineHunks(t *testing.T) {
	a := []rune("alpha\nbeta\ngamma\ndelta\nepsilon\n")
	b := []rune("alpha\nBETA\ngamma\ndelta\nepsilon!\n")
	d := Diff(a, b)
	if got, _ := d.Apply(a); string(got) != string(b) {
		t.Fatalf("got %q", string(got))
	}
	// two separate hunks, each trimmed to the changed runes
	if ins, del := countKinds(d); ins != 2 || del != 1 {
		t.Fatalf("%v", d)
	}
}

func TestDiffFallsBackWhenFarApart(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	var x, y strings.Builder
	for i := range 5000 {
		if i%10 == 0 { // shared lines: a real line diff would give many hunks
			x.WriteString("same\n")
			y.WriteString("same\n")
			continue
		}
		x.WriteString("a" + randString(r, 6) + "\n")
		y.WriteString("b" + randString(r, 6) + "\n")
	}
	a, b := []rune("start\n"+x.String()+"end\n"), []rune("start\n"+y.String()+"end\n")
	d := Diff(a, b)
	if got, _ := d.Apply(a); string(got) != string(b) {
		t.Fatal("fallback diff wrong")
	}
	if ins, del := countKinds(d); ins != 1 || del != 1 {
		t.Fatalf("fallback should replace the middle whole: %d %d", ins, del)
	}
}
