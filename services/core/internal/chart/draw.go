package chart

import (
	"image"
	"image/color"
	"math"
	"strings"
)

// fill paints a rectangle, clipped to the image.
func fill(img *image.RGBA, r image.Rectangle, col color.RGBA) {
	r = r.Intersect(img.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			img.SetRGBA(x, y, col)
		}
	}
}

// dot paints a small square centred on a point, marking one day's value.
func dot(img *image.RGBA, cx, cy, radius int, col color.RGBA) {
	fill(img, image.Rect(cx-radius, cy-radius, cx+radius+1, cy+radius+1), col)
}

// line draws with Bresenham, thickened by stamping a square at each step.
func line(img *image.RGBA, x0, y0, x1, y1 int, col color.RGBA, thickness int) {
	if thickness < 1 {
		thickness = 1
	}
	half := thickness / 2

	dx := abs(x1 - x0)
	dy := -abs(y1 - y0)
	stepX, stepY := 1, 1
	if x0 > x1 {
		stepX = -1
	}
	if y0 > y1 {
		stepY = -1
	}
	err := dx + dy

	for {
		fill(img, image.Rect(x0-half, y0-half, x0+thickness-half, y0+thickness-half), col)
		if x0 == x1 && y0 == y1 {
			return
		}
		double := 2 * err
		if double >= dy {
			err += dy
			x0 += stepX
		}
		if double <= dx {
			err += dx
			y0 += stepY
		}
	}
}

// text draws with the built-in font. Characters outside it render blank.
func text(img *image.RGBA, x, y int, s string, col color.RGBA, size int) {
	if size < 1 {
		size = 1
	}
	cursor := x
	for _, r := range strings.ToUpper(s) {
		glyph, known := font[r]
		if known {
			for row := 0; row < glyphHeight; row++ {
				bits := glyph[row]
				for column := 0; column < glyphWidth; column++ {
					if bits&(1<<(glyphWidth-1-column)) == 0 {
						continue
					}
					fill(img, image.Rect(
						cursor+column*size, y+row*size,
						cursor+(column+1)*size, y+(row+1)*size), col)
				}
			}
		}
		cursor += (glyphWidth + 1) * size
	}
}

// textWidth is the pixel width text() will occupy.
func textWidth(s string, size int) int {
	if size < 1 {
		size = 1
	}
	count := len([]rune(s))
	if count == 0 {
		return 0
	}
	return count*(glyphWidth+1)*size - size
}

func abs(v int) int { return int(math.Abs(float64(v))) }
