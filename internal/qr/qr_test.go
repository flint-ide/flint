package qr

import (
	"strings"
	"testing"
)

// evalPoly evaluates a polynomial (highest coefficient first) at x in GF(256).
func evalPoly(p []byte, x byte) byte {
	var r byte
	for _, c := range p {
		r = gfMul(r, x) ^ c
	}
	return r
}

func TestReedSolomonCodewordsHaveZeroSyndromes(t *testing.T) {
	data := []byte("flint-remote://pair?secret=ABCD")
	for _, n := range []int{10, 16, 18, 22, 24, 26} {
		block := append(append([]byte{}, data...), rsRemainder(data, rsGenerator(n))...)
		root := byte(1)
		for i := range n {
			if s := evalPoly(block, root); s != 0 {
				t.Fatalf("ec %d: syndrome %d at α^%d = %d", n, s, i, root)
			}
			root = gfMul(root, 2)
		}
	}
}

func TestGFMul(t *testing.T) {
	// α^8 = α^4+α^3+α^2+1 = 0x1D.
	a := byte(1)
	for range 8 {
		a = gfMul(a, 2)
	}
	if a != 0x1D || gfMul(0, 7) != 0 || gfMul(1, 0x53) != 0x53 {
		t.Fatalf("α^8 = %#x", a)
	}
}

func TestEncodePicksTheSmallestVersion(t *testing.T) {
	for _, tc := range []struct {
		n    int
		size int
	}{{1, 21}, {14, 21}, {15, 25}, {84, 37}, {85, 41}, {213, 57}} {
		c, err := Encode(strings.Repeat("a", tc.n))
		if err != nil || c.Size != tc.size {
			t.Fatalf("%d bytes: size %v, %v; want %d", tc.n, c, err, tc.size)
		}
	}
	if _, err := Encode(strings.Repeat("a", 214)); err != ErrTooLong {
		t.Fatalf("214 bytes: %v", err)
	}
}

// readFormat reads the format bits of the first copy back.
func readFormat(c *Code) int {
	bits := 0
	get := func(x, y, i int) {
		if c.Dark(x, y) {
			bits |= 1 << i
		}
	}
	for i := 0; i <= 5; i++ {
		get(8, i, i)
	}
	get(8, 7, 6)
	get(8, 8, 7)
	get(7, 8, 8)
	for i := 9; i < 15; i++ {
		get(14-i, 8, i)
	}
	return bits
}

func TestStructure(t *testing.T) {
	c, err := Encode("flint-remote://pair?url=https%3A%2F%2Flaptop.tail1234.ts.net&secret=ABCDEFGHIJKLMNOP")
	if err != nil {
		t.Fatal(err)
	}
	n := c.Size
	// Finder patterns: dark corners, a light ring, a dark centre.
	for _, o := range [][2]int{{0, 0}, {n - 7, 0}, {0, n - 7}} {
		if !c.Dark(o[0], o[1]) || c.Dark(o[0]+1, o[1]+1) || !c.Dark(o[0]+3, o[1]+3) {
			t.Fatalf("finder at %v", o)
		}
	}
	// Timing pattern and the dark module.
	for i := 8; i < n-8; i++ {
		if c.Dark(i, 6) != (i%2 == 0) || c.Dark(6, i) != (i%2 == 0) {
			t.Fatalf("timing at %d", i)
		}
	}
	if !c.Dark(8, n-8) {
		t.Fatal("dark module missing")
	}
	// The format bits carry level M and a valid BCH code.
	f := readFormat(c) ^ 0x5412
	data := f >> 10
	if data>>3 != 0 {
		t.Fatalf("format level bits %02b, want M (00)", data>>3)
	}
	rem := data
	for range 10 {
		rem = (rem << 1) ^ ((rem >> 9) * 0x537)
	}
	if f&0x3ff != rem {
		t.Fatalf("format BCH %#x, want %#x", f&0x3ff, rem)
	}
	if p := c.Path(); !strings.HasPrefix(p, "M0 0h7v1h-7z") {
		t.Fatalf("path starts %q", p[:20])
	}
}
