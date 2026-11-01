// Package qr encodes short text (a pairing link) as a QR code: byte mode,
// error correction level M, versions 1–10 (up to 213 bytes), the mask with
// the lowest penalty. It follows ISO/IEC 18004 and Project Nayuki's
// reference description; only the standard library is used.
package qr

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Code is an encoded symbol: Size×Size modules, without the quiet zone.
type Code struct {
	Size    int
	modules []bool // row-major, true = dark
	isFunc  []bool
}

// Dark reports whether the module at column x, row y is dark.
func (c *Code) Dark(x, y int) bool { return c.modules[y*c.Size+x] }

// version parameters for error correction level M.
type version struct {
	ecPerBlock int
	groups     [][2]int // {blocks, data codewords per block}
	align      []int
}

var versions = []version{
	1:  {10, [][2]int{{1, 16}}, nil},
	2:  {16, [][2]int{{1, 28}}, []int{6, 18}},
	3:  {26, [][2]int{{1, 44}}, []int{6, 22}},
	4:  {18, [][2]int{{2, 32}}, []int{6, 26}},
	5:  {24, [][2]int{{2, 43}}, []int{6, 30}},
	6:  {16, [][2]int{{4, 27}}, []int{6, 34}},
	7:  {18, [][2]int{{4, 31}}, []int{6, 22, 38}},
	8:  {22, [][2]int{{2, 38}, {2, 39}}, []int{6, 24, 42}},
	9:  {22, [][2]int{{3, 36}, {2, 37}}, []int{6, 26, 46}},
	10: {26, [][2]int{{4, 43}, {1, 44}}, []int{6, 28, 50}},
}

func (v version) dataCodewords() int {
	n := 0
	for _, g := range v.groups {
		n += g[0] * g[1]
	}
	return n
}

// ErrTooLong is returned for text that doesn't fit version 10.
var ErrTooLong = errors.New("qr: text too long")

// Encode makes the smallest symbol that holds text.
func Encode(text string) (*Code, error) {
	data := []byte(text)
	for ver := 1; ver < len(versions); ver++ {
		countBits := 8
		if ver >= 10 {
			countBits = 16
		}
		capacityBits := versions[ver].dataCodewords() * 8
		if 4+countBits+len(data)*8 > capacityBits {
			continue
		}
		return encode(ver, countBits, data), nil
	}
	return nil, ErrTooLong
}

type bitBuffer []bool

func (b *bitBuffer) append(val uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		*b = append(*b, (val>>uint(i))&1 == 1)
	}
}

func encode(ver, countBits int, data []byte) *Code {
	v := versions[ver]
	capacityBits := v.dataCodewords() * 8
	var bb bitBuffer
	bb.append(0b0100, 4) // byte mode
	bb.append(uint32(len(data)), countBits)
	for _, d := range data {
		bb.append(uint32(d), 8)
	}
	bb.append(0, min(4, capacityBits-len(bb)))
	bb.append(0, (8-len(bb)%8)%8)
	for pad := uint32(0xEC); len(bb) < capacityBits; pad ^= 0xEC ^ 0x11 {
		bb.append(pad, 8)
	}
	codewords := make([]byte, len(bb)/8)
	for i, bit := range bb {
		if bit {
			codewords[i/8] |= 1 << (7 - uint(i%8))
		}
	}
	all := interleave(v, codewords)

	size := ver*4 + 17
	c := &Code{Size: size, modules: make([]bool, size*size), isFunc: make([]bool, size*size)}
	c.drawFunctionPatterns(ver, v)
	c.drawCodewords(all)
	best, bestPenalty := 0, -1
	for mask := range 8 {
		c.applyMask(mask)
		c.drawFormatBits(mask)
		if p := c.penalty(); bestPenalty < 0 || p < bestPenalty {
			best, bestPenalty = mask, p
		}
		c.applyMask(mask) // XOR again: undo
	}
	c.applyMask(best)
	c.drawFormatBits(best)
	return c
}

// interleave splits the data into blocks, adds each block's error
// correction, and interleaves them as the symbol stores them.
func interleave(v version, data []byte) []byte {
	var blocks [][]byte
	var ecs [][]byte
	gen := rsGenerator(v.ecPerBlock)
	off := 0
	for _, g := range v.groups {
		for range g[0] {
			b := data[off : off+g[1]]
			off += g[1]
			blocks = append(blocks, b)
			ecs = append(ecs, rsRemainder(b, gen))
		}
	}
	var out []byte
	for i := 0; ; i++ {
		added := false
		for _, b := range blocks {
			if i < len(b) {
				out = append(out, b[i])
				added = true
			}
		}
		if !added {
			break
		}
	}
	for i := range v.ecPerBlock {
		for _, e := range ecs {
			out = append(out, e[i])
		}
	}
	return out
}

func (c *Code) set(x, y int, dark bool) {
	c.modules[y*c.Size+x] = dark
	c.isFunc[y*c.Size+x] = true
}

func (c *Code) drawFunctionPatterns(ver int, v version) {
	n := c.Size
	for i := range n { // timing patterns
		c.set(6, i, i%2 == 0)
		c.set(i, 6, i%2 == 0)
	}
	c.drawFinder(3, 3)
	c.drawFinder(n-4, 3)
	c.drawFinder(3, n-4)
	for i, ax := range v.align {
		for j, ay := range v.align {
			last := len(v.align) - 1
			if (i == 0 && j == 0) || (i == 0 && j == last) || (i == last && j == 0) {
				continue // the finders are there
			}
			for dy := -2; dy <= 2; dy++ {
				for dx := -2; dx <= 2; dx++ {
					c.set(ax+dx, ay+dy, max(abs(dx), abs(dy)) != 1)
				}
			}
		}
	}
	c.drawFormatBits(0) // reserves the area; drawn for real after masking
	if ver >= 7 {
		rem := ver
		for range 12 {
			rem = (rem << 1) ^ ((rem >> 11) * 0x1F25)
		}
		bits := ver<<12 | rem
		for i := range 18 {
			dark := (bits>>uint(i))&1 == 1
			a, b := n-11+i%3, i/3
			c.set(a, b, dark)
			c.set(b, a, dark)
		}
	}
}

// drawFinder draws a finder pattern centred at (x, y) with its separator.
func (c *Code) drawFinder(x, y int) {
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			xx, yy := x+dx, y+dy
			if xx < 0 || xx >= c.Size || yy < 0 || yy >= c.Size {
				continue
			}
			d := max(abs(dx), abs(dy))
			c.set(xx, yy, d != 2 && d != 4)
		}
	}
}

func (c *Code) drawFormatBits(mask int) {
	data := 0b00<<3 | mask // level M
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	bits := (data<<10 | rem) ^ 0x5412
	bit := func(i int) bool { return (bits>>uint(i))&1 == 1 }
	n := c.Size
	for i := 0; i <= 5; i++ {
		c.set(8, i, bit(i))
	}
	c.set(8, 7, bit(6))
	c.set(8, 8, bit(7))
	c.set(7, 8, bit(8))
	for i := 9; i < 15; i++ {
		c.set(14-i, 8, bit(i))
	}
	for i := range 8 {
		c.set(n-1-i, 8, bit(i))
	}
	for i := 8; i < 15; i++ {
		c.set(8, n-15+i, bit(i))
	}
	c.set(8, n-8, true) // the dark module
}

// drawCodewords places the bits in the zigzag order, two columns at a time
// from the bottom right, skipping function modules and the timing column.
func (c *Code) drawCodewords(data []byte) {
	n := c.Size
	i := 0
	for right := n - 1; right >= 1; right -= 2 {
		if right == 6 {
			right = 5
		}
		for vert := range n {
			for j := range 2 {
				x := right - j
				y := vert
				if (right+1)&2 == 0 {
					y = n - 1 - vert
				}
				if c.isFunc[y*n+x] || i >= len(data)*8 {
					continue
				}
				c.modules[y*n+x] = (data[i/8]>>(7-uint(i%8)))&1 == 1
				i++
			}
		}
	}
}

func (c *Code) applyMask(mask int) {
	n := c.Size
	for y := range n {
		for x := range n {
			if c.isFunc[y*n+x] {
				continue
			}
			var invert bool
			switch mask {
			case 0:
				invert = (x+y)%2 == 0
			case 1:
				invert = y%2 == 0
			case 2:
				invert = x%3 == 0
			case 3:
				invert = (x+y)%3 == 0
			case 4:
				invert = (x/3+y/2)%2 == 0
			case 5:
				invert = x*y%2+x*y%3 == 0
			case 6:
				invert = (x*y%2+x*y%3)%2 == 0
			case 7:
				invert = ((x+y)%2+x*y%3)%2 == 0
			}
			if invert {
				c.modules[y*n+x] = !c.modules[y*n+x]
			}
		}
	}
}

// penalty scores how hard the symbol is to scan (lower is better): long
// runs, 2×2 blocks, finder-like patterns and an unbalanced dark ratio.
func (c *Code) penalty() int {
	n := c.Size
	p := 0
	line := func(get func(i int) bool) {
		run := 1
		var bits strings.Builder
		for i := range n {
			if get(i) {
				bits.WriteByte('1')
			} else {
				bits.WriteByte('0')
			}
			if i > 0 {
				if get(i) == get(i-1) {
					run++
				} else {
					run = 1
				}
				if run == 5 {
					p += 3
				} else if run > 5 {
					p++
				}
			}
		}
		s := "0000" + bits.String() + "0000"
		p += 40 * (strings.Count(s, "10111010000") + strings.Count(s, "00001011101"))
	}
	for y := range n {
		line(func(x int) bool { return c.Dark(x, y) })
	}
	for x := range n {
		line(func(y int) bool { return c.Dark(x, y) })
	}
	dark := 0
	for y := range n {
		for x := range n {
			d := c.Dark(x, y)
			if d {
				dark++
			}
			if x < n-1 && y < n-1 && d == c.Dark(x+1, y) && d == c.Dark(x, y+1) && d == c.Dark(x+1, y+1) {
				p += 3
			}
		}
	}
	total := n * n
	k := (abs(dark*20-total*10)+total-1)/total - 1
	return p + max(0, k)*10
}

// Path returns an SVG path drawing the dark modules (one unit each, merged
// along rows), for a viewBox of "-4 -4 Size+8 Size+8" with the quiet zone.
func (c *Code) Path() string {
	var b strings.Builder
	for y := range c.Size {
		for x := 0; x < c.Size; {
			if !c.Dark(x, y) {
				x++
				continue
			}
			start := x
			for x < c.Size && c.Dark(x, y) {
				x++
			}
			fmt.Fprintf(&b, "M%d %dh%dv1h-%dz", start, y, x-start, x-start)
		}
	}
	return b.String()
}

// String draws the symbol with "#" and " " (tests, debugging).
func (c *Code) String() string {
	var b strings.Builder
	for y := range c.Size {
		for x := range c.Size {
			if c.Dark(x, y) {
				b.WriteString("##")
			} else {
				b.WriteString("  ")
			}
		}
		b.WriteByte('\n')
	}
	return b.String() + strconv.Itoa(c.Size)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// Reed–Solomon over GF(256) with the polynomial x^8+x^4+x^3+x^2+1.

func gfMul(a, b byte) byte {
	var r byte
	for i := 7; i >= 0; i-- {
		r = (r << 1) ^ byte((uint(r)>>7)*0x1D)
		if (b>>uint(i))&1 == 1 {
			r ^= a
		}
	}
	return r
}

// rsGenerator returns the coefficients of the generator polynomial of
// degree n (highest first, the leading 1 left out).
func rsGenerator(n int) []byte {
	g := make([]byte, n)
	g[n-1] = 1
	root := byte(1)
	for range n {
		for j := range n {
			g[j] = gfMul(g[j], root)
			if j+1 < n {
				g[j] ^= g[j+1]
			}
		}
		root = gfMul(root, 0x02)
	}
	return g
}

func rsRemainder(data, gen []byte) []byte {
	r := make([]byte, len(gen))
	for _, b := range data {
		factor := b ^ r[0]
		copy(r, r[1:])
		r[len(r)-1] = 0
		for i, g := range gen {
			r[i] ^= gfMul(g, factor)
		}
	}
	return r
}
