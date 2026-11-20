package main

import (
	"image"
	"math"
	"sort"

	"golang.org/x/image/draw"
)

type RGB struct {
	R, G, B uint8
}

// Ignore near-black and near-white colors
func isBlackOrWhite(c RGB) bool {
	return (c.R <= 16 && c.G <= 16 && c.B <= 16) || (c.R >= 240 && c.G >= 240 && c.B >= 240)
}

// colorDistance is the Euclidean distance between two RGB colors (0..441).
func colorDistance(a, b RGB) float64 {
	dr := float64(a.R) - float64(b.R)
	dg := float64(a.G) - float64(b.G)
	db := float64(a.B) - float64(b.B)
	return math.Sqrt(dr*dr + dg*dg + db*db)
}

// saturation returns the HSV saturation (0..1) of a color.
func saturation(c RGB) float64 {
	mx := max(c.R, c.G, c.B)
	mn := min(c.R, c.G, c.B)
	if mx == 0 {
		return 0
	}
	return float64(mx-mn) / float64(mx)
}

// downscale reduces the image to roughly 10% of its size. Nearest-neighbor is
// plenty for a color histogram and much cheaper than a bilinear filter.
func downscale(img image.Image) *image.RGBA {
	bounds := img.Bounds()
	w := max(bounds.Dx()/10, 1)
	h := max(bounds.Dy()/10, 1)
	resized := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.NearestNeighbor.Scale(resized, resized.Bounds(), img, bounds, draw.Src, nil)
	return resized
}

// A bucket holds every pixel whose channels share the same upper 4 bits.
// Remembering the channel sums lets us report the bucket's true average color
// instead of its (darker) lower corner.
type bucket struct {
	n       int
	r, g, b uint64
}

func (b *bucket) avg() RGB {
	if b.n == 0 {
		return RGB{}
	}
	n := uint64(b.n)
	return RGB{uint8(b.r / n), uint8(b.g / n), uint8(b.b / n)}
}

type histogram struct {
	buckets [16 * 16 * 16]bucket
	kept    int // number of pixels counted
}

// buildHistogram counts the pixels of img. With skipBW, near-black and
// near-white pixels are ignored.
func buildHistogram(img *image.RGBA, skipBW bool) *histogram {
	h := &histogram{}
	b := img.Bounds()
	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.Pix[(y-b.Min.Y)*img.Stride : (y-b.Min.Y)*img.Stride+b.Dx()*4]
		for i := 0; i+3 < len(row); i += 4 {
			r, g, bl := row[i], row[i+1], row[i+2]
			if skipBW && isBlackOrWhite(RGB{r, g, bl}) {
				continue
			}
			bk := &h.buckets[int(r>>4)<<8|int(g>>4)<<4|int(bl>>4)]
			bk.n++
			bk.r += uint64(r)
			bk.g += uint64(g)
			bk.b += uint64(bl)
			h.kept++
		}
	}
	return h
}

// filteredHistogram ignores black/white, but falls back to all pixels when
// nothing else is left (e.g. a fully black screen).
func filteredHistogram(img *image.RGBA) *histogram {
	h := buildHistogram(img, true)
	if h.kept == 0 {
		h = buildHistogram(img, false)
	}
	return h
}

// detectColor picks the representative color of img.
//
//	dominant: the most frequent color
//	vibrant:  like dominant, but saturated colors are favored over dull ones
//	average:  the mean of all (non black/white) pixels
func detectColor(img *image.RGBA, mode string) RGB {
	h := filteredHistogram(img)
	if h.kept == 0 {
		return RGB{}
	}
	if mode == "average" {
		var r, g, b uint64
		for i := range h.buckets {
			r += h.buckets[i].r
			g += h.buckets[i].g
			b += h.buckets[i].b
		}
		n := uint64(h.kept)
		return RGB{uint8(r / n), uint8(g / n), uint8(b / n)}
	}

	best := -1
	bestScore := -1.0
	for i := range h.buckets {
		bk := &h.buckets[i]
		if bk.n == 0 {
			continue
		}
		score := float64(bk.n)
		if mode == "vibrant" {
			score *= 0.2 + saturation(bk.avg())
		}
		if score > bestScore {
			bestScore = score
			best = i
		}
	}
	return h.buckets[best].avg()
}

// mostFrequentColor is detectColor in "dominant" mode.
func mostFrequentColor(img *image.RGBA) RGB {
	return detectColor(img, "dominant")
}

type colorCount struct {
	Color RGB
	Count int
}

// topColors returns the topN most frequent colors (black/white ignored).
func topColors(img *image.RGBA, topN int) []colorCount {
	h := filteredHistogram(img)
	var all []colorCount
	for i := range h.buckets {
		if h.buckets[i].n > 0 {
			all = append(all, colorCount{h.buckets[i].avg(), h.buckets[i].n})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Count > all[j].Count })
	if len(all) > topN {
		all = all[:topN]
	}
	return all
}

// brightnessFor returns the brightness (1..255) to send with color c.
func brightnessFor(c RGB, auto bool, maxB, minB int) int {
	if !auto {
		return maxB
	}
	b := int(max(c.R, c.G, c.B)) * maxB / 255
	return min(max(b, minB, 1), maxB)
}

func colorName(c RGB) string {
	r, g, b := c.R, c.G, c.B
	switch {
	case r > 200 && g < 80 && b < 80:
		return "light red"
	case r > 150 && g < 80 && b < 80:
		return "red"
	case r > 100 && r < 180 && g > 60 && g < 120 && b < 80:
		return "brown"
	case g > 200 && r > 200 && b < 100:
		return "light yellow"
	case r > 200 && g > 200 && b < 100:
		return "yellow"
	case g > 200 && r < 100 && b < 100:
		return "light green"
	case g > 150 && r < 100 && b < 100:
		return "green"
	case g > 100 && b > 100 && r < 100:
		return "teal"
	case b > 200 && r < 100 && g < 100:
		return "light blue"
	case b > 100 && r < 80 && g < 80:
		return "dark blue"
	case b > 200 && r > 200 && g < 100:
		return "pink"
	case r > 200 && g < 100 && b > 200:
		return "magenta"
	case r < 100 && g > 200 && b > 200:
		return "cyan"
	case r > 200 && g > 200 && b > 200:
		return "white"
	case r < 60 && g < 60 && b < 60:
		return "black"
	case r > 180 && g > 100 && b < 100:
		return "orange"
	case r > 180 && g > 100 && b > 100:
		return "peach"
	case r > 150 && g < 100 && b > 100:
		return "violet"
	default:
		return "unknown color"
	}
}
