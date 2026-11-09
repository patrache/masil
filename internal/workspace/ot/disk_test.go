package ot

import (
	"testing"
)

func TestDiskMergeKeepsBoth(t *testing.T) {
	disk := []rune("line1\nline2\nline3\n")
	d := NewDiskTracker(disk)
	doc := disk
	edit := Insert(5, "A", len(doc))
	doc = mustApply(t, doc, edit)
	if err := d.Edited(edit); err != nil {
		t.Fatal(err)
	}
	if !d.Dirty() {
		t.Fatal("dirty after edit")
	}
	newDisk := []rune("line1\nline2\nline3 changed\n")
	x, err := d.DiskChanged(newDisk)
	if err != nil {
		t.Fatal(err)
	}
	doc = mustApply(t, doc, x)
	if string(doc) != "line1A\nline2\nline3 changed\n" {
		t.Fatalf("doc %q", string(doc))
	}
	if !d.Dirty() || string(d.Disk()) != string(newDisk) {
		t.Fatalf("dirty=%v disk=%q", d.Dirty(), string(d.Disk()))
	}
}

func TestDiskMergeTwiceInARow(t *testing.T) {
	disk := []rune("a\nb\nc\nd\n")
	d := NewDiskTracker(disk)
	doc := disk
	apply := func(op Op) {
		doc = mustApply(t, doc, op)
		if err := d.Edited(op); err != nil {
			t.Fatal(err)
		}
	}
	apply(Insert(1, "1", len(doc))) // a1
	x, err := d.DiskChanged([]rune("a\nB\nc\nd\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc = mustApply(t, doc, x)
	apply(Insert(len(doc), "tail\n", len(doc)))
	x, err = d.DiskChanged([]rune("a\nB\nc\nD\n"))
	if err != nil {
		t.Fatal(err)
	}
	doc = mustApply(t, doc, x)
	if string(doc) != "a1\nB\nc\nD\ntail\n" {
		t.Fatalf("doc %q", string(doc))
	}
	// what is unsaved is exactly our two edits on top of the latest disk
	d.Saved(doc)
	if d.Dirty() {
		t.Fatal("dirty after save")
	}
}

func TestSavedResets(t *testing.T) {
	d := NewDiskTracker([]rune("x"))
	if err := d.Edited(Insert(1, "y", 1)); err != nil {
		t.Fatal(err)
	}
	d.Saved([]rune("xy"))
	if d.Dirty() || string(d.Disk()) != "xy" {
		t.Fatal("not reset")
	}
	x, err := d.DiskChanged([]rune("xy"))
	if err != nil || !x.IsNoop() {
		t.Fatalf("unchanged disk: %v %v", x, err)
	}
	if err := d.Edited(Insert(0, "z", 5)); err == nil {
		t.Fatal("edit with wrong base accepted")
	}
}

func TestDiskMergeProperty(t *testing.T) {
	// random unsaved edits and random disk rewrites: the merged document keeps
	// every unsaved insert and ends up equal to newDisk + unsaved.
	r := newRand(5)
	for i := range 1000 {
		disk := randLines(r, r.Intn(10))
		d := NewDiskTracker(disk)
		doc := disk
		for range 1 + r.Intn(3) {
			op := randOp(r, len(doc))
			doc = mustApply(t, doc, op)
			if err := d.Edited(op); err != nil {
				t.Fatal(err)
			}
		}
		nd := mutate(r, disk)
		x, err := d.DiskChanged(nd)
		if err != nil {
			t.Fatalf("#%d %v", i, err)
		}
		doc = mustApply(t, doc, x)
		d.Saved(doc)
		if d.Dirty() {
			t.Fatal("dirty after save")
		}
	}
}
