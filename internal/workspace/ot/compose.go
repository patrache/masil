package ot

// Compose returns one operation equivalent to applying a then b.
func Compose(a, b Op) (Op, error) {
	if err := a.validate(); err != nil {
		return nil, err
	}
	if err := b.validate(); err != nil {
		return nil, err
	}
	if a.TargetLen() != b.BaseLen() {
		return nil, ErrBaseLen
	}
	var out builder
	ia, ib := newIter(a), newIter(b)
	for ia.ok || ib.ok {
		if ia.ok && ia.cur.isDelete() {
			out.delete(ia.cur.Delete)
			ia.next()
			continue
		}
		if ib.ok && ib.cur.isInsert() {
			out.insert(ib.cur.Insert)
			ib.next()
			continue
		}
		if !ia.ok || !ib.ok {
			return nil, ErrBaseLen
		}
		n := min(ia.span(), ib.span())
		switch {
		case ia.cur.isRetain() && ib.cur.isRetain():
			out.retain(n)
			ia.consume(n)
		case ia.cur.isInsert() && ib.cur.isDelete():
			ia.consume(n) // inserted then deleted: nothing
		case ia.cur.isInsert() && ib.cur.isRetain():
			out.insert(ia.takeInsert(n))
		case ia.cur.isRetain() && ib.cur.isDelete():
			out.delete(n)
			ia.consume(n)
		}
		ib.consume(n)
	}
	return out.op, nil
}
