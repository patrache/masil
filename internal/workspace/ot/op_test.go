package ot

import (
	"encoding/json"
	"errors"
	"math/rand"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func mustApply(t *testing.T, d []rune, o Op) []rune {
	t.Helper()
	out, err := o.Apply(d)
	if err != nil {
		t.Fatalf("apply %v to %q: %v", o, string(d), err)
	}
	return out
}

func TestApply(t *testing.T) {
	d := []rune("abc한글👩‍💻") // 👩‍💻 is 3 runes
	op := Op{{Retain: 3}, {Insert: "X"}, {Delete: 2}, {Retain: 3}}
	if op.BaseLen() != 8 || op.TargetLen() != 7 {
		t.Fatalf("lens %d %d", op.BaseLen(), op.TargetLen())
	}
	got := mustApply(t, d, op)
	if string(got) != "abcX👩‍💻" {
		t.Fatalf("got %q", string(got))
	}
	if _, err := op.Apply(got); !errors.Is(err, ErrBaseLen) {
		t.Fatalf("7-rune doc: %v", err)
	}
	if !(Op{{Retain: 4}}).IsNoop() || !(Op{}).IsNoop() || op.IsNoop() {
		t.Fatal("IsNoop")
	}
	if _, err := (Op{{Retain: -1}}).Apply(nil); err == nil {
		t.Fatal("negative retain accepted")
	}
	if _, err := (Op{{Retain: 1, Insert: "x"}}).Apply([]rune("a")); err == nil {
		t.Fatal("two-field component accepted")
	}
}

func TestJSONFormat(t *testing.T) {
	op := Op{{Retain: 3}, {Insert: "X"}, {Delete: 2}, {Retain: 3}}
	b, err := json.Marshal(op)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != `[3,"X",-2,3]` {
		t.Fatalf("marshal %s", b)
	}
	var got Op
	if err := json.Unmarshal([]byte(`[0,1,"",2,-1,-1]`), &got); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, Op{{Retain: 3}, {Delete: 2}}) {
		t.Fatalf("unmarshal %#v", got)
	}
	if err := json.Unmarshal([]byte(`["a","b",1]`), &got); err != nil || !reflect.DeepEqual(got, Op{{Insert: "ab"}, {Retain: 1}}) {
		t.Fatalf("merge inserts %#v %v", got, err)
	}
	for _, bad := range []string{`[1.5]`, `[true]`, `{}`, `[null]`, `[[1]]`} {
		if err := json.Unmarshal([]byte(bad), &got); err == nil {
			t.Errorf("accepted %s", bad)
		}
	}
	if b, _ := json.Marshal(Op(nil)); string(b) != `[]` {
		t.Fatalf("nil op %s", b)
	}
}

func TestTransformTP1(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	for i := range 5000 {
		d := randDoc(r)
		a, b := randOp(r, len(d)), randOp(r, len(d))
		a2, b2, err := Transform(a, b)
		if err != nil {
			t.Fatal(err)
		}
		x := mustApply(t, mustApply(t, d, a), b2)
		y := mustApply(t, mustApply(t, d, b), a2)
		if string(x) != string(y) {
			t.Fatalf("#%d d=%q a=%v b=%v: %q != %q", i, string(d), a, b, string(x), string(y))
		}
	}
	if _, _, err := Transform(Op{{Retain: 1}}, Op{{Retain: 2}}); !errors.Is(err, ErrBaseLen) {
		t.Fatalf("mismatched bases: %v", err)
	}
}

func TestTransformTieBreak(t *testing.T) {
	d := []rune("")
	a, b := Insert(0, "A", 0), Insert(0, "B", 0)
	a2, b2, err := Transform(a, b)
	if err != nil {
		t.Fatal(err)
	}
	x := mustApply(t, mustApply(t, d, a), b2)
	y := mustApply(t, mustApply(t, d, b), a2)
	if string(x) != "BA" || string(y) != "BA" {
		t.Fatalf("%q %q", string(x), string(y))
	}
}

func TestComposeInvert(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	for i := range 3000 {
		d := randDoc(r)
		a := randOp(r, len(d))
		da := mustApply(t, d, a)
		b := randOp(r, len(da))
		c, err := Compose(a, b)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := mustApply(t, d, c), mustApply(t, da, b); string(got) != string(want) {
			t.Fatalf("#%d compose: %q != %q", i, string(got), string(want))
		}
		if back := mustApply(t, da, a.Invert(d)); string(back) != string(d) {
			t.Fatalf("#%d invert: %q != %q", i, string(back), string(d))
		}
	}
	if _, err := Compose(Op{{Retain: 1}}, Op{{Retain: 2}}); !errors.Is(err, ErrBaseLen) {
		t.Fatalf("mismatched compose: %v", err)
	}
}

func TestTransformIndex(t *testing.T) {
	cases := []struct {
		op   Op
		want int
	}{
		{Insert(2, "xy", 10), 7},
		{Insert(5, "xy", 10), 5},
		{Delete(1, 3, 10), 2},
		{Delete(3, 5, 10), 3},
		{Insert(6, "xy", 10), 5},
	}
	for _, c := range cases {
		if got := TransformIndex(c.op, 5); got != c.want {
			t.Errorf("%v: %d, want %d", c.op, got, c.want)
		}
	}
}

func TestBuilders(t *testing.T) {
	if got := Insert(1, "한👩", 3); !reflect.DeepEqual(got, Op{{Retain: 1}, {Insert: "한👩"}, {Retain: 2}}) {
		t.Fatalf("Insert %v", got)
	}
	if got := Delete(0, 2, 2); !reflect.DeepEqual(got, Op{{Delete: 2}}) {
		t.Fatalf("Delete %v", got)
	}
}

// A participant-supplied operation whose counts wrap the length sums must be
// rejected, never panic the owner's process (P2 review).
func TestOverflowingCountsRejected(t *testing.T) {
	const maxInt = int(^uint(0) >> 1)
	doc := []rune("hello")
	cases := []string{
		`[9223372036854775807,-9223372036854775807,7]`,
		`[9223372036854775807,9223372036854775807,7]`,
		`[-9223372036854775808,5]`,
		`[1073741825]`,
	}
	for _, js := range cases {
		var o Op
		if err := json.Unmarshal([]byte(js), &o); err == nil {
			if _, err := o.Apply(doc); !errors.Is(err, ErrInvalid) && !errors.Is(err, ErrBaseLen) {
				t.Fatalf("%s: apply err %v", js, err)
			}
			s := NewSequencer(doc)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("%s: sequencer panicked: %v", js, r)
					}
				}()
				if _, _, err := s.Submit(0, o); err == nil {
					t.Fatalf("%s: sequencer accepted", js)
				}
			}()
		}
	}
	// built directly (not via JSON): each component in range, the sum wraps
	direct := Op{{Retain: maxInt}, {Delete: maxInt}, {Retain: 7}}
	func() {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("direct op panicked: %v", r)
			}
		}()
		if _, err := direct.Apply(doc); !errors.Is(err, ErrInvalid) {
			t.Fatalf("direct: %v", err)
		}
		if _, _, err := NewSequencer(doc).Submit(0, direct); !errors.Is(err, ErrInvalid) {
			t.Fatalf("direct submit: %v", err)
		}
	}()
}

// v0.1.3 security (a): a frame of many one-rune inserts (optionally split by
// deletes) must decode in linear memory; merging with string += copied the
// growing insert on every element (quadratic within one frame).
func TestUnmarshalInsertMergeLinear(t *testing.T) {
	const n = 60000
	for _, sep := range []string{"", `,-1`} {
		var sb strings.Builder
		sb.WriteByte('[')
		for i := range n {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(`"a"`)
			sb.WriteString(sep)
		}
		sb.WriteByte(']')
		data := []byte(sb.String())
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		var got Op
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		runtime.ReadMemStats(&after)
		if alloc := after.TotalAlloc - before.TotalAlloc; alloc > 64<<20 {
			t.Fatalf("sep %q: decoding a %d-byte frame allocated %d bytes", sep, len(data), alloc)
		}
		want := Op{{Insert: strings.Repeat("a", n)}}
		if sep != "" {
			want = append(want, Component{Delete: n})
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("sep %q: wrong normalization (len %d)", sep, len(got))
		}
	}
}

// The decoder's normalization must match the builder's canonical form.
func TestUnmarshalMatchesBuilder(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for range 2000 {
		var b builder
		items := []any{}
		for range r.Intn(12) {
			switch r.Intn(3) {
			case 0:
				n := r.Intn(3)
				items = append(items, n)
				b.retain(n)
			case 1:
				n := r.Intn(3)
				items = append(items, -n)
				b.delete(n)
			default:
				s := []string{"", "x", "é", "yz"}[r.Intn(4)]
				items = append(items, s)
				b.insert(s)
			}
		}
		data, _ := json.Marshal(items)
		var got Op
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, b.op) {
			t.Fatalf("%s: got %#v want %#v", data, got, b.op)
		}
	}
}
