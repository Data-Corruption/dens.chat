package media

// quantizer picks a GIF frame's palette, of at most 255 colors, leaving
// room for the transparent one: the frame's colors themselves while there
// are no more, as in a GIF made smaller, and otherwise by median cut over
// the colors at five bits a channel, each box of them giving its mean.
type quantizer struct {
	// The colors themselves, while there are at most 255: by a hash of
	// each, 0 being empty, with its index.
	keys  [1024]uint32
	idx   [1024]byte
	exact []uint32

	// The colors at five bits a channel, and once cut, each bin's index.
	bins   [1 << 15]colorBin
	used   []uint16
	binIdx [1 << 15]byte
	cut    bool

	// Once cut, the palette, and the entry nearest each bin's middle, or
	// 0xFFFF until it's asked for: dithering asks for colors between the
	// frame's.
	colors  []byte
	nearest [1 << 15]uint16
}

type colorBin struct{ n, r, g, b uint32 }

// maxColors is a frame's palette at most, the transparent index aside.
const maxColors = 255

func (q *quantizer) reset() {
	clear(q.keys[:])
	q.exact = q.exact[:0]
	for _, u := range q.used {
		q.bins[u] = colorBin{}
	}
	q.used, q.cut = q.used[:0], false
}

// near is the palette index nearest a color, once the palette is cut.
func (q *quantizer) near(r, g, b int) byte {
	u := r>>3<<10 | g>>3<<5 | b>>3
	if n := q.nearest[u]; n != 0xFFFF {
		return byte(n)
	}
	r, g, b = r|4, g|4, b|4
	best, at := 1<<30, 0
	for i := 0; i < len(q.colors); i += 3 {
		dr, dg, db := r-int(q.colors[i]), g-int(q.colors[i+1]), b-int(q.colors[i+2])
		if d := 2*dr*dr + 4*dg*dg + 3*db*db; d < best {
			best, at = d, i/3
		}
	}
	q.nearest[u] = uint16(at)
	return byte(at)
}

func hashColor(c uint32) int { return int(c * 2654435761 >> 22 & 1023) }

func binOf(c uint32) uint16 {
	return uint16(c>>19&0x1F<<10 | c>>11&0x1F<<5 | c>>3&0x1F)
}

// add counts an opaque color, 0xRRGGBB with bit 24 set.
func (q *quantizer) add(c uint32) {
	if len(q.exact) <= maxColors {
		h := hashColor(c)
		for q.keys[h] != 0 && q.keys[h] != c {
			h = (h + 1) & 1023
		}
		if q.keys[h] == 0 {
			q.keys[h], q.idx[h] = c, byte(len(q.exact))
			q.exact = append(q.exact, c)
		}
	}
	b := &q.bins[binOf(c)]
	if b.n == 0 {
		q.used = append(q.used, binOf(c))
	}
	b.n++
	b.r += c >> 16 & 0xFF
	b.g += c >> 8 & 0xFF
	b.b += c & 0xFF
}

// index is the palette index of a color added since the last reset.
func (q *quantizer) index(c uint32) byte {
	if q.cut {
		return q.binIdx[binOf(c)]
	}
	h := hashColor(c)
	for q.keys[h] != c {
		h = (h + 1) & 1023
	}
	return q.idx[h]
}

// palette is the colors added's palette, as RGB triples.
func (q *quantizer) palette() []byte {
	if len(q.exact) <= maxColors {
		out := make([]byte, 0, 3*len(q.exact))
		for _, c := range q.exact {
			out = append(out, byte(c>>16), byte(c>>8), byte(c))
		}
		return out
	}
	q.cut = true
	type box struct {
		lo, hi   int // its bins, in q.used
		n        uint32
		min, max [3]int
	}
	channel := func(u uint16, ch int) int { return int(u) >> (10 - 5*ch) & 0x1F }
	measure := func(b *box) {
		b.n, b.min, b.max = 0, [3]int{31, 31, 31}, [3]int{}
		for _, u := range q.used[b.lo:b.hi] {
			b.n += q.bins[u].n
			for ch := range 3 {
				b.min[ch], b.max[ch] = min(b.min[ch], channel(u, ch)), max(b.max[ch], channel(u, ch))
			}
		}
	}
	boxes := []box{{lo: 0, hi: len(q.used)}}
	measure(&boxes[0])
	for len(boxes) < maxColors {
		// The box to split: the one with the most pixels over the widest
		// range of a channel, which it's split along.
		best, ch, score := -1, 0, uint64(0)
		for i := range boxes {
			b := &boxes[i]
			for c := range 3 {
				if s := uint64(b.n) * uint64(b.max[c]-b.min[c]); s > score {
					best, ch, score = i, c, s
				}
			}
		}
		if best < 0 {
			break
		}
		b := &boxes[best]
		// It's cut where half its pixels are on each side, as near as its
		// bins allow, with a bin on each side.
		var hist [32]uint32
		for _, u := range q.used[b.lo:b.hi] {
			hist[channel(u, ch)] += q.bins[u].n
		}
		cut, below := b.min[ch], hist[b.min[ch]]
		for cut+1 < b.max[ch] && below < b.n/2 {
			cut++
			below += hist[cut]
		}
		i, j := b.lo, b.hi-1
		for i <= j {
			if channel(q.used[i], ch) <= cut {
				i++
			} else {
				q.used[i], q.used[j] = q.used[j], q.used[i]
				j--
			}
		}
		upper := box{lo: i, hi: b.hi}
		b.hi = i
		measure(b)
		measure(&upper)
		boxes = append(boxes, upper)
	}
	out := make([]byte, 0, 3*len(boxes))
	for i, b := range boxes {
		var r, g, bl uint32
		for _, u := range q.used[b.lo:b.hi] {
			r, g, bl = r+q.bins[u].r, g+q.bins[u].g, bl+q.bins[u].b
			q.binIdx[u] = byte(i)
		}
		out = append(out, byte((r+b.n/2)/b.n), byte((g+b.n/2)/b.n), byte((bl+b.n/2)/b.n))
	}
	q.colors = out
	for i := range q.nearest {
		q.nearest[i] = 0xFFFF
	}
	return out
}
