package server

import (
	"bytes"
	"crypto/sha1"
	"image"
	"image/color"
	"image/png"
	"math"
	"sync"
)

const placeholderSize = 600

var placeholderCache sync.Map // book ID -> []byte PNG

// placeholderCover draws a deterministic gradient square for books that have
// no artwork. Podcast clients expect a raster image, so we generate one
// instead of shipping a static asset.
func placeholderCover(seed string) []byte {
	if v, ok := placeholderCache.Load(seed); ok {
		return v.([]byte)
	}
	sum := sha1.Sum([]byte(seed))
	hue := float64(sum[0])/255*360 + float64(sum[1])/255*20
	topR, topG, topB := hsv(hue, 0.55, 0.34)
	botR, botG, botB := hsv(math.Mod(hue+28, 360), 0.72, 0.10)

	img := image.NewRGBA(image.Rect(0, 0, placeholderSize, placeholderSize))
	mid := placeholderSize / 2
	for y := 0; y < placeholderSize; y++ {
		t := float64(y) / float64(placeholderSize-1)
		r := uint8(topR + (botR-topR)*t)
		g := uint8(topG + (botG-topG)*t)
		b := uint8(topB + (botB-topB)*t)
		for x := 0; x < placeholderSize; x++ {
			// A soft lighter band across the middle keeps it from looking
			// like a broken image.
			d := math.Abs(float64(x-mid)) / float64(mid)
			k := 1 - 0.18*math.Exp(-6*d*d)
			img.SetRGBA(x, y, color.RGBA{
				R: clamp8(float64(r) * k),
				G: clamp8(float64(g) * k),
				B: clamp8(float64(b) * k),
				A: 255,
			})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil
	}
	out := buf.Bytes()
	placeholderCache.Store(seed, out)
	return out
}

func clamp8(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 255:
		return 255
	default:
		return uint8(v)
	}
}

// hsv converts HSV (h in degrees, s and v in 0..1) to RGB bytes.
func hsv(h, s, v float64) (float64, float64, float64) {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	return (r + m) * 255, (g + m) * 255, (b + m) * 255
}
