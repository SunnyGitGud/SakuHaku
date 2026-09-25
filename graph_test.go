package main

import (
	"testing"
	"unicode/utf8"
)

func TestBrailleGraph(t *testing.T) {
	// Two samples: 0 and max -> the right half of one cell fully set, left empty
	g := brailleGraph([]float64{0, 10}, 1, 1, 10)
	if g[0] != string(rune(0x2800|0x08|0x10|0x20|0x80)) {
		t.Fatalf("got %q", g[0])
	}

	// Half height over two rows: top row empty, bottom row full
	g = brailleGraph([]float64{5, 5}, 1, 2, 10)
	if g[0] != "⠀" || g[1] != "⣿" {
		t.Fatalf("got %q", g)
	}

	// Few samples grow in from the right, lines are always width wide
	g = brailleGraph([]float64{1, 2, 3}, 10, 3, 3)
	for _, line := range g {
		if utf8.RuneCountInString(line) != 10 {
			t.Fatalf("line %q is not 10 wide", line)
		}
	}
	if g[2][:3] != "⠀" {
		t.Fatalf("left side should be empty: %q", g[2])
	}

	// Small non-zero values still show a dot
	g = brailleGraph([]float64{0.001, 0.001}, 1, 1, 1000)
	if g[0] == "⠀" {
		t.Fatal("tiny speed should still be visible")
	}
}

func TestNiceMax(t *testing.T) {
	for peak, want := range map[float64]float64{0: 1, 9: 10, 1.5e6: 2e6, 4.6e6: 5e6, 2.2e6: 2.5e6} {
		if got := niceMax(peak); got != want {
			t.Errorf("niceMax(%v) = %v, want %v", peak, got, want)
		}
	}
}

func TestNiceByteMax(t *testing.T) {
	const mib = 1 << 20
	for peak, want := range map[float64]float64{4.6 * mib: 5 * mib, 700 * 1024: 1000 * 1024, 300: 500} {
		if got := niceByteMax(peak); got != want {
			t.Errorf("niceByteMax(%v) = %v, want %v", peak, got, want)
		}
	}
}
