package main

import (
	"image"
	"testing"
)

func TestQuantizeRGB(t *testing.T) {
	c := RGB{123, 234, 56}
	q := quantizeRGB(c, 16)
	if q.R%16 != 0 || q.G%16 != 0 || q.B%16 != 0 {
		t.Errorf("quantizeRGB failed: got %+v", q)
	}
}

func TestIsBlackOrWhite(t *testing.T) {
	if !isBlackOrWhite(RGB{0, 0, 0}) {
		t.Error("expected black to be true")
	}
	if !isBlackOrWhite(RGB{255, 255, 255}) {
		t.Error("expected white to be true")
	}
	if isBlackOrWhite(RGB{100, 100, 100}) {
		t.Error("expected gray to be false")
	}
}

func TestColorDistance(t *testing.T) {
	d := colorDistance(RGB{0, 0, 0}, RGB{255, 0, 0})
	if d != 255*255 {
		t.Errorf("unexpected color distance: %v", d)
	}
}

func TestColorName(t *testing.T) {
	if colorName(RGB{255, 0, 0}) != "light red" {
		t.Error("colorName failed for light red")
	}
	if colorName(RGB{0, 255, 0}) != "light green" {
		t.Error("colorName failed for light green")
	}
}

func TestHSVRoundTrip(t *testing.T) {
	r, g, b := hsToRGB(0, 100)
	h, s := rgbToHSColor(RGB{uint8(r), uint8(g), uint8(b)})
	if h < 0 || h > 360 || s < 0 || s > 100 {
		t.Errorf("unexpected hs values: h=%d s=%d", h, s)
	}
}

func TestDownscale(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	resized := downscale(img)
	if resized.Bounds().Dx() != 10 || resized.Bounds().Dy() != 10 {
		t.Errorf("unexpected downscale size: %v", resized.Bounds())
	}
}
