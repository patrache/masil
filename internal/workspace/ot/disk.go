package ot

// DiskTracker merges outside changes to an open file with the document's
// unsaved edits (spec B7.5): disk is what the file holds, unsaved turns disk
// into the current document. Not safe for concurrent use.
type DiskTracker struct {
	disk    []rune
	unsaved Op
}

func NewDiskTracker(disk []rune) *DiskTracker {
	d := append([]rune(nil), disk...)
	return &DiskTracker{disk: d, unsaved: Noop(len(d))}
}

// Edited records a sequenced edit of the document. Call it for every
// sequenced edit except the disk edits DiskChanged returned (those are
// already accounted for).
func (d *DiskTracker) Edited(op Op) error {
	u, err := Compose(d.unsaved, op)
	if err != nil {
		return err
	}
	d.unsaved = u
	return nil
}

// DiskChanged takes the file's new contents and returns the edit to
// sequence on top of the current document (author = disk) so that both the
// outside change and the unsaved edits survive, three-way-merge style.
func (d *DiskTracker) DiskChanged(newDisk []rune) (Op, error) {
	nd := append([]rune(nil), newDisk...)
	x := Diff(d.disk, nd)
	x2, u2, err := Transform(x, d.unsaved)
	if err != nil {
		return nil, err
	}
	d.disk, d.unsaved = nd, u2
	return x2, nil
}

// Saved records that doc was written to disk.
func (d *DiskTracker) Saved(doc []rune) {
	d.disk = append([]rune(nil), doc...)
	d.unsaved = Noop(len(d.disk))
}

// Dirty reports unsaved edits.
func (d *DiskTracker) Dirty() bool { return !d.unsaved.IsNoop() }

// Disk is a copy of what the file holds.
func (d *DiskTracker) Disk() []rune { return append([]rune(nil), d.disk...) }
