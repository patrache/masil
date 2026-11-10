package ot

// DiffMaxEdits caps the line edit distance Diff searches; beyond it the
// changed middle is replaced whole.
const DiffMaxEdits = 2000

// Diff returns an operation turning a into b: the common prefix and suffix
// are kept, the middle is diffed by lines (Myers), and inside each changed
// hunk the common prefix and suffix are kept again. Apply(a, Diff(a, b)) == b
// always holds.
func Diff(a, b []rune) Op {
	p := 0
	for p < len(a) && p < len(b) && a[p] == b[p] {
		p++
	}
	s := 0
	for s < len(a)-p && s < len(b)-p && a[len(a)-1-s] == b[len(b)-1-s] {
		s++
	}
	var bl builder
	bl.retain(p)
	diffLines(&bl, a[p:len(a)-s], b[p:len(b)-s])
	bl.retain(s)
	return bl.result()
}

// splitLines cuts after every '\n' (the last line may lack one).
func splitLines(r []rune) [][]rune {
	var out [][]rune
	start := 0
	for i, c := range r {
		if c == '\n' {
			out = append(out, r[start:i+1])
			start = i + 1
		}
	}
	if start < len(r) {
		out = append(out, r[start:])
	}
	return out
}

func diffLines(bl *builder, a, b []rune) {
	if len(a) == 0 || len(b) == 0 {
		bl.delete(len(a))
		bl.insert(string(b))
		return
	}
	la, lb := splitLines(a), splitLines(b)
	ids := map[string]int32{}
	intern := func(lines [][]rune) []int32 {
		out := make([]int32, len(lines))
		for i, l := range lines {
			k := string(l)
			id, ok := ids[k]
			if !ok {
				id = int32(len(ids))
				ids[k] = id
			}
			out[i] = id
		}
		return out
	}
	script, ok := myers(intern(la), intern(lb), DiffMaxEdits)
	if !ok {
		bl.delete(len(a))
		bl.insert(string(b))
		return
	}
	var oldHunk, newHunk []rune
	flush := func() {
		if len(oldHunk) > 0 || len(newHunk) > 0 {
			for _, c := range trimDiff(oldHunk, newHunk) {
				switch {
				case c.isRetain():
					bl.retain(c.Retain)
				case c.isDelete():
					bl.delete(c.Delete)
				default:
					bl.insert(c.Insert)
				}
			}
			oldHunk, newHunk = nil, nil
		}
	}
	i, j := 0, 0
	for _, e := range script {
		switch e {
		case editEqual:
			flush()
			bl.retain(len(la[i]))
			i++
			j++
		case editDelete:
			oldHunk = append(oldHunk, la[i]...)
			i++
		case editInsert:
			newHunk = append(newHunk, lb[j]...)
			j++
		}
	}
	flush()
}

type edit uint8

const (
	editEqual edit = iota
	editDelete
	editInsert
)

// myers returns the shortest edit script from a to b (greedy O((N+M)D)
// with a stored trace), or false when the distance exceeds maxD.
func myers(a, b []int32, maxD int) ([]edit, bool) {
	n, m := len(a), len(b)
	// trace[d][k+d] = furthest x on diagonal k after d edits
	var trace [][]int32
	get := func(d, k int) int {
		if d < 0 {
			return 0 // virtual start: v[1] = 0
		}
		return int(trace[d][k+d])
	}
	for d := 0; d <= n+m; d++ {
		if d > maxD {
			return nil, false
		}
		v := make([]int32, 2*d+1)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && get(d-1, k-1) < get(d-1, k+1)) {
				x = get(d-1, k+1) // down: insert
			} else {
				x = get(d-1, k-1) + 1 // right: delete
			}
			if d == 0 {
				x = 0
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[k+d] = int32(x)
			if x >= n && y >= m {
				trace = append(trace, v)
				return backtrack(trace, n, m), true
			}
		}
		trace = append(trace, v)
	}
	return nil, false // unreachable
}

func backtrack(trace [][]int32, n, m int) []edit {
	var rev []edit
	x, y := n, m
	at := func(d, k int) int { return int(trace[d][k+d]) }
	for d := len(trace) - 1; d > 0; d-- {
		k := x - y
		var pk int
		if k == -d || (k != d && at(d-1, k-1) < at(d-1, k+1)) {
			pk = k + 1
		} else {
			pk = k - 1
		}
		px := at(d-1, pk)
		py := px - pk
		// the edit leaves (px,py) to (sx,sy); the snake then runs to (x,y)
		sx, sy := px, py+1
		e := editInsert
		if pk == k-1 {
			sx, sy, e = px+1, py, editDelete
		}
		for x > sx && y > sy {
			rev = append(rev, editEqual)
			x--
			y--
		}
		rev = append(rev, e)
		x, y = px, py
	}
	for x > 0 && y > 0 {
		rev = append(rev, editEqual)
		x--
		y--
	}
	out := make([]edit, len(rev))
	for i, e := range rev {
		out[len(rev)-1-i] = e
	}
	return out
}
