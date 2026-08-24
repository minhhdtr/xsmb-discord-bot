// Package chart draws a gold price series as a PNG. Discord won't render
// SVG attachments inline, so it has to be a raster image.
package chart

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"

	"github.com/minhhdtr/xsmb-discord-bot/internal/domain"
	"github.com/minhhdtr/xsmb-discord-bot/internal/format"
)

// Layout of the rendered image, in pixels.
const (
	Width  = 920
	Height = 460

	padLeft   = 96
	padRight  = 28
	padTop    = 52
	padBottom = 48

	scale = 2 // text is drawn at twice the font's native size
)

var (
	colourBackground = color.RGBA{0x1e, 0x21, 0x26, 0xff}
	colourGrid       = color.RGBA{0x2f, 0x35, 0x3d, 0xff}
	colourAxis       = color.RGBA{0x4a, 0x54, 0x60, 0xff}
	colourText       = color.RGBA{0xc3, 0xcc, 0xd8, 0xff}
	colourFaint      = color.RGBA{0x8b, 0x96, 0xa5, 0xff}
	colourBuy        = color.RGBA{0x3f, 0xb9, 0x50, 0xff}
	colourSell       = color.RGBA{0xe2, 0x53, 0x53, 0xff}
)

// Render draws a series and returns PNG bytes.
func Render(series domain.GoldSeries) ([]byte, error) {
	if !series.Valid() {
		return nil, domain.ErrNoHistory
	}
	points := series.Points()
	if len(points) < 2 {
		return nil, fmt.Errorf("%w: need at least two days to draw a line", domain.ErrNoHistory)
	}

	// Domestic prices in triệu; a full đồng figure is 11 characters wide.
	divisor, suffix := 1.0, ""
	if series.Currency == domain.VND {
		divisor, suffix = 1e6, "TR"
	}

	img := image.NewRGBA(image.Rect(0, 0, Width, Height))
	fill(img, img.Bounds(), colourBackground)

	plot := image.Rect(padLeft, padTop, Width-padRight, Height-padBottom)
	low, high := series.Range()
	low, high = low/divisor, high/divisor
	bottom, top, step := niceBounds(low, high)

	drawGrid(img, plot, bottom, top, step, suffix)
	drawXLabels(img, plot, points)

	twoSided := series.TwoSided()
	if twoSided {
		drawSeries(img, plot, points, bottom, top, divisor, colourSell, func(p domain.GoldPoint) float64 { return p.Sell })
	}
	drawSeries(img, plot, points, bottom, top, divisor, colourBuy, func(p domain.GoldPoint) float64 { return p.Buy })

	drawHeader(img, series, points, twoSided)

	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, fmt.Errorf("encode chart: %w", err)
	}
	return out.Bytes(), nil
}

func drawHeader(img *image.RGBA, series domain.GoldSeries, points []domain.GoldPoint, twoSided bool) {
	const baseline = 18

	title := strings.ToUpper(series.Code)
	text(img, padLeft, baseline, title, colourText, scale)

	span := fmt.Sprintf("%s - %s",
		points[0].Day.Format("02/01"), points[len(points)-1].Day.Format("02/01"))
	text(img, Width-padRight-textWidth(span, scale), baseline, span, colourFaint, scale)

	// One line needs no legend, and "MUA" would be wrong for a spot price.
	if !twoSided {
		return
	}
	x := padLeft + textWidth(title, scale) + 28
	x = legend(img, x, baseline, "MUA", colourBuy)
	legend(img, x+24, baseline, "BAN", colourSell)
}

// legend draws a colour swatch with its label and returns the x just past it.
func legend(img *image.RGBA, x, baseline int, label string, col color.RGBA) int {
	const swatch = 14
	fill(img, image.Rect(x, baseline, x+swatch, baseline+swatch), col)
	x += swatch + 8
	text(img, x, baseline+(swatch-glyphHeight*scale)/2, label, colourFaint, scale)
	return x + textWidth(label, scale)
}

// drawGrid paints horizontal rules and their value labels.
func drawGrid(img *image.RGBA, plot image.Rectangle, bottom, top, step float64, suffix string) {
	for value := bottom; value <= top+step/2; value += step {
		y := valueToY(value, bottom, top, plot)
		fill(img, image.Rect(plot.Min.X, y, plot.Max.X, y+1), colourGrid)
		label := format.Trim(format.Decimal(value, 1)) + suffix
		text(img, plot.Min.X-10-textWidth(label, scale), y-glyphHeight*scale/2, label, colourFaint, scale)
	}
	fill(img, image.Rect(plot.Min.X, plot.Min.Y, plot.Min.X+1, plot.Max.Y), colourAxis)
	fill(img, image.Rect(plot.Min.X, plot.Max.Y, plot.Max.X, plot.Max.Y+1), colourAxis)
}

// drawXLabels writes about six dates along the bottom, evenly spaced.
func drawXLabels(img *image.RGBA, plot image.Rectangle, points []domain.GoldPoint) {
	const wanted = 6
	stride := (len(points) + wanted - 1) / wanted
	if stride < 1 {
		stride = 1
	}
	for i := 0; i < len(points); i += stride {
		x := indexToX(i, len(points), plot)
		label := points[i].Day.Format("02/01")
		text(img, x-textWidth(label, scale)/2, plot.Max.Y+12, label, colourFaint, scale)
	}
}

// drawSeries draws one line, two pixels thick, with a dot on each day.
func drawSeries(img *image.RGBA, plot image.Rectangle, points []domain.GoldPoint,
	bottom, top, divisor float64, col color.RGBA, pick func(domain.GoldPoint) float64) {

	prevX, prevY, has := 0, 0, false
	for i, p := range points {
		value := pick(p) / divisor
		if value <= 0 {
			has = false
			continue
		}
		x := indexToX(i, len(points), plot)
		y := valueToY(value, bottom, top, plot)
		if has {
			line(img, prevX, prevY, x, y, col, 2)
		}
		dot(img, x, y, 2, col)
		prevX, prevY, has = x, y, true
	}
}

func indexToX(i, count int, plot image.Rectangle) int {
	if count <= 1 {
		return plot.Min.X
	}
	span := float64(plot.Dx() - 1)
	return plot.Min.X + int(math.Round(span*float64(i)/float64(count-1)))
}

func valueToY(value, bottom, top float64, plot image.Rectangle) int {
	if top == bottom {
		return plot.Max.Y
	}
	ratio := (value - bottom) / (top - bottom)
	return plot.Max.Y - int(math.Round(ratio*float64(plot.Dy()-1)))
}

// niceBounds pads the range and snaps to a round step, so the axis reads
// 143.000 rather than 143.117.
func niceBounds(low, high float64) (bottom, top, step float64) {
	if high <= low {
		// A flat series still deserves a sensible axis.
		pad := math.Max(math.Abs(high)*0.01, 1)
		low, high = high-pad, high+pad
	}
	pad := (high - low) * 0.12
	low, high = low-pad, high+pad

	step = niceStep((high - low) / 4)
	bottom = math.Floor(low/step) * step
	top = math.Ceil(high/step) * step
	return bottom, top, step
}

// niceStep rounds a raw interval up to 1, 2, 2.5 or 5 times a power of ten.
func niceStep(raw float64) float64 {
	if raw <= 0 {
		return 1
	}
	magnitude := math.Pow(10, math.Floor(math.Log10(raw)))
	switch normalised := raw / magnitude; {
	case normalised <= 1:
		return magnitude
	case normalised <= 2:
		return 2 * magnitude
	case normalised <= 2.5:
		return 2.5 * magnitude
	case normalised <= 5:
		return 5 * magnitude
	default:
		return 10 * magnitude
	}
}
