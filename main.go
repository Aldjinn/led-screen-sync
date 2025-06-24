package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/go-toast/toast"
	"github.com/kbinani/screenshot"
)

type RGB struct {
	R, G, B uint8
}

// Quantize an RGB color to reduce the number of unique colors (e.g., to the nearest 16)
func quantizeRGB(c RGB, step uint8) RGB {
	return RGB{
		R: (c.R / step) * step,
		G: (c.G / step) * step,
		B: (c.B / step) * step,
	}
}

func mostFrequentColor(img image.Image) RGB {
	countMap := make(map[RGB]int)
	bounds := img.Bounds()
	quantStep := uint8(16) // quantize to nearest 16
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			cr := uint8(r >> 8)
			cg := uint8(g >> 8)
			cb := uint8(b >> 8)
			color := quantizeRGB(RGB{cr, cg, cb}, quantStep)
			countMap[color]++
		}
	}
	var maxCount int
	var mostColor RGB

	// Find the color with the highest count
	for col, cnt := range countMap {
		if cnt > maxCount {
			maxCount = cnt
			mostColor = col
		}
	}
	return mostColor
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

func topColors(img image.Image, topN int) []struct {
	Color RGB
	Count int
} {
	countMap := make(map[RGB]int)
	bounds := img.Bounds()
	total := 0
	quantStep := uint8(16)
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			cr := uint8(r >> 8)
			cg := uint8(g >> 8)
			cb := uint8(b >> 8)
			color := quantizeRGB(RGB{cr, cg, cb}, quantStep)
			countMap[color]++
			total++
		}
	}
	// Sort colors by count
	type kv struct {
		Color RGB
		Count int
	}
	var sorted []kv
	for k, v := range countMap {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Count > sorted[j].Count
	})
	if len(sorted) > topN {
		sorted = sorted[:topN]
	}
	// Convert []kv to []struct{Color colorful.Color; Count int}
	result := make([]struct {
		Color RGB
		Count int
	}, len(sorted))
	for i, v := range sorted {
		result[i] = struct {
			Color RGB
			Count int
		}{v.Color, v.Count}
	}
	return result
}

type ColorStat struct {
	R       uint8   `json:"r"`
	G       uint8   `json:"g"`
	B       uint8   `json:"b"`
	Name    string  `json:"name"`
	Percent float64 `json:"percent"`
}

type LogEntry struct {
	Timestamp  string      `json:"timestamp"`
	ScreenSize string      `json:"screen_size"`
	TopColors  []ColorStat `json:"top_colors"`
}

func logTopColorsJSON(filename string, bounds image.Rectangle, top []struct {
	Color RGB
	Count int
}, totalPixels int) error {
	var stats []ColorStat
	for _, entry := range top {
		stats = append(stats, ColorStat{
			R:       entry.Color.R,
			G:       entry.Color.G,
			B:       entry.Color.B,
			Name:    colorName(entry.Color),
			Percent: float64(entry.Count) / float64(totalPixels) * 100,
		})
	}
	logEntry := LogEntry{
		Timestamp:  time.Now().Format(time.RFC3339),
		ScreenSize: fmt.Sprintf("%dx%d", bounds.Dx(), bounds.Dy()),
		TopColors:  stats,
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	return enc.Encode(logEntry)
}

func formatJSONFile(filename string) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		return err
	}
	var entries []json.RawMessage
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var entry json.RawMessage
		err := dec.Decode(&entry)
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			return err
		}
		entries = append(entries, entry)
	}
	pretty, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, pretty, 0644)
}

func saveScreenshotPNG(img image.Image, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// Convert RGB to HSV and then to Home Assistant hs_color (hue, saturation)
func rgbToHSColor(c RGB) (int, int) {
	r := float64(c.R) / 255.0
	g := float64(c.G) / 255.0
	b := float64(c.B) / 255.0
	max := r
	if g > max {
		max = g
	}
	if b > max {
		max = b
	}
	min := r
	if g < min {
		min = g
	}
	if b < min {
		min = b
	}
	delta := max - min
	var h, s float64
	if delta == 0 {
		h = 0
	} else if max == r {
		h = 60 * ((g - b) / delta)
		if h < 0 {
			h += 360
		}
	} else if max == g {
		h = 60 * (((b - r) / delta) + 2)
	} else {
		h = 60 * (((r - g) / delta) + 4)
	}
	if max == 0 {
		s = 0
	} else {
		s = delta / max * 100
	}
	return int(h + 0.5), int(s + 0.5)
}

func callHomeAssistantHSColor(h, s int, token string) error {
	url := "http://192.168.1.124:8123/api/services/light/turn_on"
	body := fmt.Sprintf(`{"entity_id":"light.ldvsmart_indflex2m","hs_color":[%d,%d],"brightness":255}`, h, s)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Home Assistant call failed: %s", resp.Status)
	}
	return nil
}

func notifyMostColor(c RGB, h, s int) {
	notification := toast.Notification{
		AppID:   "LED Screen Sync",
		Title:   "Most Used Color",
		Message: fmt.Sprintf("R:%d G:%d B:%d (%s)\nhs_color: [%d, %d]", c.R, c.G, c.B, colorName(c), h, s),
	}
	err := notification.Push()
	if err != nil {
		log.Printf("Failed to send notification: %v", err)
	}
}

func main() {
	interval := 10 * time.Second
	for {
		numDisplay := screenshot.NumActiveDisplays()
		if numDisplay <= 0 {
			log.Fatal("No active display found")
		}
		bounds := screenshot.GetDisplayBounds(0)
		log.Printf("Screenshot size: %dx%d", bounds.Dx(), bounds.Dy())
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			log.Fatalf("Failed to capture screenshot: %v", err)
		}

		if os.Getenv("EXPORT_SCREENSHOT") == "true" {
			if err := saveScreenshotPNG(img, "screenshot.png"); err != nil {
				log.Printf("Failed to save screenshot: %v", err)
			}
		}

		mostColor := mostFrequentColor(img)
		fmt.Printf("Most frequent color: R:%d G:%d B:%d (%s)\n",
			mostColor.R, mostColor.G, mostColor.B, colorName(mostColor))
		h, s := rgbToHSColor(mostColor)
		notifyMostColor(mostColor, h, s)
		fmt.Printf("Calling Home Assistant with hs_color: [%d, %d]\n", h, s)
		token := os.Getenv("HA_TOKEN")
		if token == "" {
			log.Println("HA_TOKEN environment variable not set, skipping Home Assistant call.")
		} else {
			err := callHomeAssistantHSColor(h, s, token)
			if err != nil {
				log.Printf("Failed to call Home Assistant: %v", err)
			}
		}

		top := topColors(img, 10)
		totalPixels := bounds.Dx() * bounds.Dy()
		fmt.Println("Top 10 colors:")
		for _, entry := range top {
			percent := float64(entry.Count) / float64(totalPixels) * 100
			fmt.Printf("R:%d G:%d B:%d (%s): %.2f%%\n",
				entry.Color.R, entry.Color.G, entry.Color.B, colorName(entry.Color), percent)
		}

		if os.Getenv("EXPORT_JSON") == "true" {
			if err := logTopColorsJSON("colorlog.json", bounds, top, totalPixels); err != nil {
				log.Printf("Failed to log JSON: %v", err)
			}
			if err := formatJSONFile("colorlog.json"); err != nil {
				log.Printf("Failed to format JSON file: %v", err)
			}
		}

		time.Sleep(interval)
	}
}
