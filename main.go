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

	"github.com/getlantern/systray"
	"github.com/kbinani/screenshot"
	"golang.org/x/image/draw"
	"gopkg.in/yaml.v3"
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

// Ignore near-black and near-white colors
func isBlackOrWhite(c RGB) bool {
	return (c.R <= 16 && c.G <= 16 && c.B <= 16) || (c.R >= 240 && c.G >= 240 && c.B >= 240)
}

// Downscale image to 10% of original size
func downscale(img image.Image) image.Image {
	bounds := img.Bounds()
	w := bounds.Dx() / 10
	h := bounds.Dy() / 10
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	resized := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.BiLinear.Scale(resized, resized.Bounds(), img, bounds, draw.Over, nil)
	return resized
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
			if isBlackOrWhite(color) {
				continue
			}
			countMap[color]++
		}
	}
	if len(countMap) == 0 {
		// fallback: use all colors if nothing left after filtering
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

// Home Assistant entity ID
const entityID = "light.ldvsmart_indflex2m"

// Struct for Home Assistant state response
// Add RGBColor to attributes
type haState struct {
	State      string `json:"state"`
	Attributes struct {
		HSColor    []float64 `json:"hs_color"`
		RGBColor   []int     `json:"rgb_color"`
		Brightness int       `json:"brightness"`
	} `json:"attributes"`
}

// Get current LED state from Home Assistant
func getCurrentLEDState(token string) (*haState, error) {
	url := "http://192.168.1.124:8123/api/states/" + entityID
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("Failed to get LED state: %s", resp.Status)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var state haState
	if err := json.Unmarshal(body, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// Set LED state (rgb_color and brightness)
func setLEDState(r, g, b, brightness int, token string) error {
	url := "http://192.168.1.124:8123/api/services/light/turn_on"
	body := fmt.Sprintf(`{"entity_id":"%s","rgb_color":[%d,%d,%d],"brightness":%d}`, entityID, r, g, b, brightness)
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

// Calculate Euclidean distance between two RGB colors
func colorDistance(a, b RGB) float64 {
	dr := int(a.R) - int(b.R)
	dg := int(a.G) - int(b.G)
	db := int(a.B) - int(b.B)
	return (float64(dr*dr + dg*dg + db*db))
}

var (
	running          = false
	quitChan         = make(chan struct{})
	originalLEDState *haState
)

func onReady() {
	systray.SetIcon(ledIcon)
	systray.SetTitle("LED Sync")
	systray.SetTooltip("LED Screen Sync")
	// You can set a custom icon here with systray.SetIcon([]byte{})
	mStart := systray.AddMenuItem("Start", "Start color updates")
	mStop := systray.AddMenuItem("Stop", "Stop color updates")
	mQuit := systray.AddMenuItem("Quit", "Quit the app")
	mStop.Disable()

	go func() {
		for {
			select {
			case <-mStart.ClickedCh:
				if !running {
					running = true
					mStart.Disable()
					mStop.Enable()
					token := os.Getenv("HA_TOKEN")
					if token != "" {
						state, err := getCurrentLEDState(token)
						if err != nil {
							log.Printf("Failed to get current LED state: %v", err)
						} else {
							originalLEDState = state
							log.Printf("Saved original LED state: hs_color=%v, brightness=%d", state.Attributes.HSColor, state.Attributes.Brightness)
						}
					}
					go colorUpdateLoop()
				}
			case <-mStop.ClickedCh:
				if running {
					running = false
					mStart.Enable()
					mStop.Disable()
					quitChan <- struct{}{}
				}
			case <-mQuit.ClickedCh:
				systray.Quit()
				os.Exit(0)
			}
		}
	}()
}

// Helper to extract RGB from attributes (prefer rgb_color, fallback to hs_color)
func attrsToRGB(attrs struct {
	HSColor    []float64 `json:"hs_color"`
	RGBColor   []int     `json:"rgb_color"`
	Brightness int       `json:"brightness"`
}) ([3]int, bool) {
	if len(attrs.RGBColor) == 3 {
		return [3]int{attrs.RGBColor[0], attrs.RGBColor[1], attrs.RGBColor[2]}, true
	}
	if len(attrs.HSColor) == 2 {
		h := attrs.HSColor[0]
		s := attrs.HSColor[1]
		r, g, b := hsToRGB(h, s)
		return [3]int{r, g, b}, true
	}
	return [3]int{255, 255, 255}, false
}

// Convert HS to RGB (Home Assistant style)
func hsToRGB(h, s float64) (int, int, int) {
	// h: 0-360, s: 0-100
	hue := h / 360.0
	sat := s / 100.0
	v := 1.0
	var r, g, b float64
	i := int(hue * 6)
	f := hue*6 - float64(i)
	p := v * (1 - sat)
	q := v * (1 - f*sat)
	t := v * (1 - (1-f)*sat)
	switch i % 6 {
	case 0:
		r, g, b = v, t, p
	case 1:
		r, g, b = q, v, p
	case 2:
		r, g, b = p, v, t
	case 3:
		r, g, b = p, q, v
	case 4:
		r, g, b = t, p, v
	case 5:
		r, g, b = v, p, q
	}
	return int(r*255 + 0.5), int(g*255 + 0.5), int(b*255 + 0.5)
}

func colorUpdateLoop() {
	interval := 100 * time.Millisecond
	var prevColor *RGB
	colorChangeThreshold := 32.0
	for running {
		iterStart := time.Now()
		numDisplay := screenshot.NumActiveDisplays()
		if numDisplay <= 0 {
			log.Fatal("No active display found")
		}
		bounds := screenshot.GetDisplayBounds(0)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			log.Fatalf("Failed to capture screenshot: %v", err)
		}
		if os.Getenv("EXPORT_SCREENSHOT") == "true" {
			if err := saveScreenshotPNG(img, "screenshot.png"); err != nil {
				log.Printf("Failed to save screenshot: %v", err)
			}
		}
		// Downscale for fast processing
		smallImg := downscale(img)
		mostColor := mostFrequentColor(smallImg)
		fmt.Printf("Most frequent color: R:%d G:%d B:%d\n", mostColor.R, mostColor.G, mostColor.B)
		shouldCallHA := false
		if prevColor == nil {
			shouldCallHA = true
		} else {
			dist := colorDistance(mostColor, *prevColor)
			if dist >= colorChangeThreshold {
				shouldCallHA = true
			}
		}
		token := os.Getenv("HA_TOKEN")
		if token == "" {
			log.Println("HA_TOKEN environment variable not set, skipping Home Assistant call.")
		} else if shouldCallHA {
			err := setLEDState(int(mostColor.R), int(mostColor.G), int(mostColor.B), 255, token)
			if err != nil {
				log.Printf("Failed to call Home Assistant: %v", err)
			}
			prevColor = &mostColor
		} else {
			fmt.Printf("Skipped Home Assistant call (color change < threshold %.1f)\n", colorChangeThreshold)
		}
		iterEnd := time.Now()
		iterDuration := iterEnd.Sub(iterStart).Seconds()
		fmt.Printf("Iteration took %.3f seconds\n", iterDuration)
		if os.Getenv("EXPORT_JSON") == "true" {
			top := topColors(smallImg, 10)
			totalPixels := smallImg.Bounds().Dx() * smallImg.Bounds().Dy()
			if err := logTopColorsJSON("colorlog.json", smallImg.Bounds(), top, totalPixels); err != nil {
				log.Printf("Failed to log JSON: %v", err)
			}
			if err := formatJSONFile("colorlog.json"); err != nil {
				log.Printf("Failed to format JSON file: %v", err)
			}
		}
		select {
		case <-quitChan:
			return
		case <-time.After(interval):
		}
	}
}

// Config struct for YAML
type Config struct {
	Env struct {
		HA_URL                 string  `yaml:"HA_URL"`
		HA_TOKEN               string  `yaml:"HA_TOKEN"`
		LED_ENTITY             string  `yaml:"LED_ENTITY"`
		EXPORT_JSON            bool    `yaml:"EXPORT_JSON"`
		EXPORT_SCREENSHOT      bool    `yaml:"EXPORT_SCREENSHOT"`
		COLOR_CHANGE_THRESHOLD float64 `yaml:"COLOR_CHANGE_THRESHOLD"`
		UPDATE_INTERVAL_MS     int     `yaml:"UPDATE_INTERVAL_MS"`
	} `yaml:"env"`
}

var config Config

func loadConfig(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	return dec.Decode(&config)
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "********"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

func main() {
	err := loadConfig("led-screen-sync.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}
	log.Printf("Config loaded: HA_URL=%s, LED_ENTITY=%s, EXPORT_JSON=%v, EXPORT_SCREENSHOT=%v, COLOR_CHANGE_THRESHOLD=%.2f, UPDATE_INTERVAL_MS=%d, HA_TOKEN=%s",
		config.Env.HA_URL,
		config.Env.LED_ENTITY,
		config.Env.EXPORT_JSON,
		config.Env.EXPORT_SCREENSHOT,
		config.Env.COLOR_CHANGE_THRESHOLD,
		config.Env.UPDATE_INTERVAL_MS,
		maskToken(config.Env.HA_TOKEN),
	)
	systray.Run(onReady, func() {})
}
