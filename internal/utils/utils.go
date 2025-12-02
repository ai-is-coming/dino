package utils

import (
	"image"
	"image/color"
	draw "image/draw"
	"path/filepath"
	"strings"

	"github.com/shopspring/decimal"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// Clamp bounds v into [lo, hi].
func Clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// DrawRect draws a rectangle outline with a given thickness and color on an RGBA image.
func DrawRect(img *image.RGBA, x1, y1, x2, y2 int, c color.Color, thickness int) {
	b := img.Bounds()
	set := func(x, y int) {
		if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
			return
		}
		img.Set(x, y, c)
	}
	for t := 0; t < thickness; t++ {
		// top & bottom
		for x := x1; x <= x2; x++ {
			set(x, y1+t)
			set(x, y2-t)
		}
		// left & right
		for y := y1; y <= y2; y++ {
			set(x1+t, y)
			set(x2-t, y)
		}
	}
}

// IsImageFile returns true if the file has a common image extension.
func IsImageFile(p string) bool {
	ext := strings.ToLower(filepath.Ext(p))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".bmp", ".gif", ".webp":
		return true
	default:
		return false
	}
}

// DenormalizeBbox999 converts normalized bbox coordinates (0..999 scale) given as strings
// into absolute pixel coordinates for an image of size width x height.
// It returns x1, y1, x2, y2 as integers, ensuring x1<=x2, y1<=y2 and clamped to image bounds.
func DenormalizeBbox999(x1s, y1s, x2s, y2s string, width, height int) (int, int, int, int) {
	// parse with decimal and scale by image size (0..999)
	fx1, _ := decimal.NewFromString(x1s)
	fy1, _ := decimal.NewFromString(y1s)
	fx2, _ := decimal.NewFromString(x2s)
	fy2, _ := decimal.NewFromString(y2s)
	dw := decimal.NewFromInt(int64(width))
	dh := decimal.NewFromInt(int64(height))
	thousand := decimal.NewFromInt(1000)

	x1 := int(fx1.Mul(dw).Div(thousand).IntPart())
	y1 := int(fy1.Mul(dh).Div(thousand).IntPart())
	x2 := int(fx2.Mul(dw).Div(thousand).IntPart())
	y2 := int(fy2.Mul(dh).Div(thousand).IntPart())

	// normalize ordering
	if x1 > x2 {
		x1, x2 = x2, x1
	}
	if y1 > y2 {
		y1, y2 = y2, y1
	}

	maxX := width - 1
	if maxX < 0 {
		maxX = 0
	}
	maxY := height - 1
	if maxY < 0 {
		maxY = 0
	}

	x1 = Clamp(x1, 0, maxX)
	x2 = Clamp(x2, 0, maxX)
	y1 = Clamp(y1, 0, maxY)
	y2 = Clamp(y2, 0, maxY)
	return x1, y1, x2, y2
}

// DrawLabel draws a text label near the top-left of a bounding box with a colored background.
// x,y are the top-left corner of the bbox.
func DrawLabel(img *image.RGBA, x, y int, label string, fg color.Color, bg color.Color) {
	label = strings.TrimSpace(label)
	if label == "" {
		return
	}

	face := basicfont.Face7x13
	d := &font.Drawer{
		Dst:  img,
		Src:  image.NewUniform(fg),
		Face: face,
	}

	textWidth := d.MeasureString(label).Ceil()
	m := face.Metrics()
	ascent := m.Ascent.Ceil()
	height := m.Height.Ceil()

	// Place background rectangle just above the top edge of the bbox.
	baselineY := y - 2
	top := baselineY - ascent - 2 // padding above text
	b := img.Bounds()
	if top < b.Min.Y {
		shift := b.Min.Y - top
		baselineY += shift
		top = b.Min.Y
	}
	right := x + textWidth + 4
	if right > b.Max.X {
		right = b.Max.X
	}
	bgRect := image.Rect(x, top, right, top+height+4)
	draw.Draw(img, bgRect, &image.Uniform{bg}, image.Point{}, draw.Over)

	// Draw text with a small left padding
	d.Dot = fixed.Point26_6{X: fixed.I(x + 2), Y: fixed.I(baselineY)}
	d.DrawString(label)
}
