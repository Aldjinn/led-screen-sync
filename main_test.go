package main

import (
	"context"
	"encoding/json"
	"image"
	imgcolor "image/color"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func solid(w, h int, c imgcolor.RGBA) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetRGBA(x, y, c)
		}
	}
	return img
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
	if d := colorDistance(RGB{0, 0, 0}, RGB{255, 0, 0}); d != 255 {
		t.Errorf("unexpected color distance: %v", d)
	}
	// Maximum distance is the documented 441.
	if d := colorDistance(RGB{0, 0, 0}, RGB{255, 255, 255}); d < 441 || d > 442 {
		t.Errorf("max distance should be ~441, got %v", d)
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

func TestDownscale(t *testing.T) {
	resized := downscale(image.NewRGBA(image.Rect(0, 0, 100, 100)))
	if resized.Bounds().Dx() != 10 || resized.Bounds().Dy() != 10 {
		t.Errorf("unexpected downscale size: %v", resized.Bounds())
	}
	tiny := downscale(image.NewRGBA(image.Rect(0, 0, 3, 3)))
	if tiny.Bounds().Dx() != 1 || tiny.Bounds().Dy() != 1 {
		t.Errorf("tiny image should clamp to 1x1, got %v", tiny.Bounds())
	}
}

func TestDetectColor_ReturnsTrueAverageNotBucketFloor(t *testing.T) {
	// 123,234,56 would be floored to 112,224,48 by the old quantizer.
	img := solid(10, 10, imgcolor.RGBA{123, 234, 56, 255})
	got := mostFrequentColor(img)
	if got != (RGB{123, 234, 56}) {
		t.Errorf("got %+v, want exact color", got)
	}
}

func TestDetectColor_Dominant(t *testing.T) {
	img := solid(10, 10, imgcolor.RGBA{200, 30, 30, 255}) // red everywhere...
	for y := 0; y < 10; y++ {                             // ...except 30% blue
		for x := 0; x < 3; x++ {
			img.SetRGBA(x, y, imgcolor.RGBA{30, 30, 200, 255})
		}
	}
	if got := detectColor(img, "dominant"); got != (RGB{200, 30, 30}) {
		t.Errorf("dominant: got %+v", got)
	}
}

func TestDetectColor_IgnoresBlackAndWhite(t *testing.T) {
	img := solid(10, 10, imgcolor.RGBA{0, 0, 0, 255}) // mostly black
	for x := 0; x < 2; x++ {
		img.SetRGBA(x, 0, imgcolor.RGBA{200, 30, 30, 255})
	}
	if got := detectColor(img, "dominant"); got != (RGB{200, 30, 30}) {
		t.Errorf("got %+v, want the only non-black color", got)
	}
}

func TestDetectColor_AllBlackFallsBack(t *testing.T) {
	if got := detectColor(solid(10, 10, imgcolor.RGBA{0, 0, 0, 255}), "dominant"); got != (RGB{0, 0, 0}) {
		t.Errorf("got %+v", got)
	}
}

func TestDetectColor_VibrantPrefersSaturated(t *testing.T) {
	img := solid(10, 10, imgcolor.RGBA{120, 120, 130, 255}) // dull gray-blue, 60%
	for y := 0; y < 10; y++ {
		for x := 0; x < 4; x++ {
			img.SetRGBA(x, y, imgcolor.RGBA{220, 20, 20, 255}) // vivid red, 40%
		}
	}
	if got := detectColor(img, "dominant"); got != (RGB{120, 120, 130}) {
		t.Errorf("dominant: got %+v", got)
	}
	if got := detectColor(img, "vibrant"); got != (RGB{220, 20, 20}) {
		t.Errorf("vibrant: got %+v", got)
	}
}

func TestDetectColor_Average(t *testing.T) {
	img := solid(10, 10, imgcolor.RGBA{200, 40, 40, 255})
	for y := 0; y < 10; y++ {
		for x := 0; x < 5; x++ {
			img.SetRGBA(x, y, imgcolor.RGBA{40, 40, 200, 255})
		}
	}
	got := detectColor(img, "average")
	if got != (RGB{120, 40, 120}) {
		t.Errorf("average: got %+v", got)
	}
}

func TestTopColors(t *testing.T) {
	img := solid(10, 10, imgcolor.RGBA{200, 30, 30, 255})
	for x := 0; x < 3; x++ {
		img.SetRGBA(x, 0, imgcolor.RGBA{30, 30, 200, 255})
	}
	top := topColors(img, 5)
	if len(top) != 2 || top[0].Color != (RGB{200, 30, 30}) || top[0].Count != 97 {
		t.Errorf("unexpected top colors: %+v", top)
	}
	if len(topColors(img, 1)) != 1 {
		t.Error("topN not respected")
	}
}

func TestBrightnessFor(t *testing.T) {
	if b := brightnessFor(RGB{10, 10, 10}, false, 200, 10); b != 200 {
		t.Errorf("fixed: got %d", b)
	}
	if b := brightnessFor(RGB{255, 0, 0}, true, 255, 10); b != 255 {
		t.Errorf("auto bright: got %d", b)
	}
	if b := brightnessFor(RGB{20, 5, 5}, true, 255, 40); b != 40 {
		t.Errorf("auto dark should clamp to min: got %d", b)
	}
	if b := brightnessFor(RGB{128, 0, 0}, true, 200, 10); b != 100 {
		t.Errorf("auto should scale by max brightness: got %d", b)
	}
}

func TestBackoff(t *testing.T) {
	if d := backoff(100, 0, 1000); d != 100 {
		t.Errorf("got %v", d)
	}
	if d := backoff(100, 2, 1000); d != 400 {
		t.Errorf("got %v", d)
	}
	if d := backoff(100, 30, 1000); d != 1000 {
		t.Errorf("should cap at limit, got %v", d)
	}
}

func TestAppendColorLog(t *testing.T) {
	path := filepath.Join(t.TempDir(), "log.ndjson")
	b := image.Rect(0, 0, 10, 10)
	top := []colorCount{{RGB{255, 0, 0}, 50}}
	for i := 0; i < 3; i++ {
		if err := appendColorLog(path, b, RGB{255, 0, 0}, top, 100); err != nil {
			t.Fatal(err)
		}
	}
	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d", len(lines))
	}
	var e LogEntry
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil || e.TopColors[0].Percent != 50 || e.Detected != "#ff0000" {
		t.Errorf("bad entry %+v err=%v", e, err)
	}
}

// --- Home Assistant client -------------------------------------------------

func TestHAClient_SetColorAndRestore(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody = nil
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Write([]byte(`[]`))
	}))
	defer srv.Close()
	c := newHAClient(srv.URL+"/", "tok", `light."odd"`)

	if err := c.SetColor(context.Background(), RGB{1, 2, 3}, 77, 500); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/services/light/turn_on" || gotAuth != "Bearer tok" {
		t.Errorf("path=%s auth=%s", gotPath, gotAuth)
	}
	if gotBody["entity_id"] != `light."odd"` || gotBody["brightness"] != float64(77) || gotBody["transition"] != 0.5 {
		t.Errorf("body=%v", gotBody)
	}

	if err := c.Restore(context.Background(), &haState{State: "off"}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/services/light/turn_off" {
		t.Errorf("restore off: path=%s", gotPath)
	}
}

func TestHAClient_ErrorIncludesBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "entity not found", http.StatusNotFound)
	}))
	defer srv.Close()
	err := newHAClient(srv.URL, "t", "light.x").TurnOn(context.Background())
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "entity not found") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestHAClient_State(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"state":"on","attributes":{"rgb_color":[1,2,3],"brightness":null}}`))
	}))
	defer srv.Close()
	st, err := newHAClient(srv.URL, "t", "light.x").State(context.Background())
	if err != nil || st.State != "on" || len(st.Attributes.RGBColor) != 3 || st.Attributes.Brightness != nil {
		t.Errorf("st=%+v err=%v", st, err)
	}
}
