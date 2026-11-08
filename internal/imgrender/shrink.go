package imgrender

import (
	"image"
	"image/draw"
)

// shrinkChunk is how many source pixels shrinkInto converts at a time.
const shrinkChunk = 4096

// shrinkInto writes src (bounds sb) scaled into all of dst by area averaging:
// each output pixel is the coverage-weighted mean of the source pixels under
// it, in premultiplied 8-bit RGBA (draw.Src semantics). It streams the source
// a few rows at a time, so beyond dst it needs O(dst width) memory and a
// 16 KB conversion buffer whatever the source size; x/image/draw's kernel
// scalers instead keep a dst-width x src-height float buffer (32 B an entry)
// and per-column weight lists that grow with the shrink factor (pending
// item 13). dst must be no larger than sb on either axis.
func shrinkInto(dst *image.RGBA, src image.Image, sb image.Rectangle) {
	dw, dh := dst.Rect.Dx(), dst.Rect.Dy()
	sw, sh := sb.Dx(), sb.Dy()
	if dw <= 0 || dh <= 0 || sw <= 0 || sh <= 0 || dw > sw || dh > sh {
		return
	}
	// Integer coverage: source column x spans [x*dw, (x+1)*dw) and output
	// column c spans [c*sw, (c+1)*sw) on a common axis (rows likewise), so
	// the weights are exact and every output pixel's weights sum to sw*sh.
	DW, DH, SW, SH := int64(dw), int64(dh), int64(sw), int64(sh)
	norm := 1 / float64(SW*SH)

	cw := min(sw, shrinkChunk)
	bh := max(1, shrinkChunk/cw)
	buf := image.NewRGBA(image.Rect(0, 0, cw, bh))
	rows := make([]float64, 4*dw*bh) // horizontally reduced source rows
	acc := make([]float64, 4*dw)     // the output row being gathered
	outY := 0
	emit := func() {
		o := dst.PixOffset(dst.Rect.Min.X, dst.Rect.Min.Y+outY)
		for i, v := range acc {
			v = v*norm + 0.5
			if v > 255 {
				v = 255
			}
			dst.Pix[o+i] = uint8(v)
			acc[i] = 0
		}
		outY++
	}

	for y0 := 0; y0 < sh; y0 += bh {
		n := min(bh, sh-y0)
		clear(rows[:4*dw*n])
		for x0 := 0; x0 < sw; x0 += cw {
			m := min(cw, sw-x0)
			draw.Draw(buf, image.Rect(0, 0, m, n), src, image.Pt(sb.Min.X+x0, sb.Min.Y+y0), draw.Src)
			for r := range n {
				row := rows[4*dw*r : 4*dw*(r+1)]
				px := buf.Pix[r*buf.Stride:]
				for i := range m {
					x := int64(x0 + i)
					lo := x * DW
					c := lo / SW
					edge := (c + 1) * SW
					p := px[4*i : 4*i+4]
					if lo+DW <= edge {
						addPix(row[4*c:], p, float64(DW))
					} else {
						addPix(row[4*c:], p, float64(edge-lo))
						addPix(row[4*(c+1):], p, float64(lo+DW-edge))
					}
				}
			}
		}
		for r := range n {
			row := rows[4*dw*r : 4*dw*(r+1)]
			lo := int64(y0+r) * DH
			edge := (lo/SH + 1) * SH
			if lo+DH <= edge {
				addRow(acc, row, float64(DH))
				if lo+DH == edge {
					emit()
				}
			} else {
				addRow(acc, row, float64(edge-lo))
				emit()
				addRow(acc, row, float64(lo+DH-edge))
			}
		}
	}
}

func addPix(dst []float64, p []uint8, w float64) {
	dst[0] += w * float64(p[0])
	dst[1] += w * float64(p[1])
	dst[2] += w * float64(p[2])
	dst[3] += w * float64(p[3])
}

func addRow(dst, src []float64, w float64) {
	for i, v := range src {
		dst[i] += w * v
	}
}

// flattenOverWhite composites premultiplied m over opaque white in place.
func flattenOverWhite(m *image.RGBA) {
	for y := m.Rect.Min.Y; y < m.Rect.Max.Y; y++ {
		o := m.PixOffset(m.Rect.Min.X, y)
		row := m.Pix[o : o+4*m.Rect.Dx()]
		for i := 0; i < len(row); i += 4 {
			k := 255 - row[i+3]
			row[i] += k
			row[i+1] += k
			row[i+2] += k
			row[i+3] = 255
		}
	}
}
