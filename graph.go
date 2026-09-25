package main

import "math"

// Braille cells are 2 dots wide and 4 tall, so each character draws two
// samples at four levels. dotBits[column][row] with row 0 at the top.
var dotBits = [2][4]rune{
	{0x01, 0x02, 0x04, 0x40},
	{0x08, 0x10, 0x20, 0x80},
}

// brailleGraph draws values as a filled area chart width characters wide and
// rows characters tall, scaled so maxValue reaches the top. The newest sample
// is at the right edge; when there are fewer samples than fit, the graph
// grows in from the right.
func brailleGraph(values []float64, width, rows int, maxValue float64) []string {
	if width <= 0 || rows <= 0 {
		return nil
	}
	cols := width * 2
	if len(values) > cols {
		values = values[len(values)-cols:]
	}
	offset := cols - len(values)
	levels := rows * 4

	heights := make([]int, cols)
	for i, v := range values {
		h := 0
		if maxValue > 0 && v > 0 {
			h = int(math.Round(v / maxValue * float64(levels)))
			h = max(1, min(levels, h)) // anything moving shows at least a dot
		}
		heights[offset+i] = h
	}

	out := make([]string, rows)
	for r := range rows {
		line := make([]rune, width)
		// Dot levels covered by this character row, counted from the bottom
		rowBottom := (rows - 1 - r) * 4
		for c := range width {
			cell := rune(0x2800)
			for side := range 2 {
				h := heights[c*2+side] - rowBottom
				for dot := 0; dot < min(4, h); dot++ {
					cell |= dotBits[side][3-dot]
				}
			}
			line[c] = cell
		}
		out[r] = string(line)
	}
	return out
}

// niceByteMax is niceMax in the 1024-based units FormatSpeed prints, so a
// byte rate scale reads "5.0 MB/s" rather than "4.8 MB/s"
func niceByteMax(peak float64) float64 {
	unit := 1.0
	for peak/unit >= 1024 {
		unit *= 1024
	}
	return niceMax(peak/unit) * unit
}

// niceMax rounds a peak up so the graph's scale label is a round number and
// the curve doesn't hug the top
func niceMax(peak float64) float64 {
	if peak <= 0 {
		return 1
	}
	exp := math.Pow(10, math.Floor(math.Log10(peak)))
	for _, step := range []float64{1, 2, 2.5, 5, 10} {
		if step*exp >= peak*1.05 {
			return step * exp
		}
	}
	return 10 * exp
}
