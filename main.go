package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/getlantern/systray"
	"github.com/kbinani/screenshot"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"golang.org/x/image/draw"
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

	// Read existing log entries (if any)
	var entries []LogEntry
	if data, err := os.ReadFile(filename); err == nil && len(data) > 0 {
		if err := json.Unmarshal(data, &entries); err != nil {
			// If file is not a valid array, try to recover from NDJSON (old format)
			var lines []LogEntry
			for _, line := range bytes.Split(data, []byte{'\n'}) {
				if len(bytes.TrimSpace(line)) == 0 {
					continue
				}
				var e LogEntry
				if err := json.Unmarshal(line, &e); err == nil {
					lines = append(lines, e)
				}
			}
			entries = lines
		}
	}
	entries = append(entries, logEntry)

	// Write the updated array back to file
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(entries); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return nil
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
	url := appConfig.Env.HA_URL + "/api/states/" + appConfig.Env.LED_ENTITY
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
	url := appConfig.Env.HA_URL + "/api/services/light/turn_on"
	body := fmt.Sprintf(`{"entity_id":"%s","rgb_color":[%d,%d,%d],"brightness":%d}`,
		appConfig.Env.LED_ENTITY, r, g, b, brightness)
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

// Turn LED on or off using Home Assistant API
func setLEDOnOff(on bool) error {
	logger.Infof("Turning LED %s", map[bool]string{true: "on", false: "off"}[on])
	urlPath := "/api/services/light/turn_on"
	if !on {
		urlPath = "/api/services/light/turn_off"
	}

	url := appConfig.Env.HA_URL + urlPath
	body := fmt.Sprintf(`{"entity_id":"%s"}`, appConfig.Env.LED_ENTITY)
	req, err := http.NewRequest("POST", url, bytes.NewBuffer([]byte(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+appConfig.Env.HA_TOKEN)
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("Home Assistant on/off call failed: %s", resp.Status)
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
	appConfig        *Config
	logger           *zap.SugaredLogger
)

func setupLogger() {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.Encoding = "console"
	cfg.OutputPaths = []string{"stdout"}

	level := zapcore.InfoLevel
	switch appConfig.Env.LOG_LEVEL {
	case "debug":
		level = zapcore.DebugLevel
	case "warn":
		level = zapcore.WarnLevel
	case "error":
		level = zapcore.ErrorLevel
	case "dpanic":
		level = zapcore.DPanicLevel
	case "panic":
		level = zapcore.PanicLevel
	case "fatal":
		level = zapcore.FatalLevel
	}
	cfg.Level = zap.NewAtomicLevelAt(level)

	l, err := cfg.Build()
	if err != nil {
		panic(err)
	}
	logger = l.Sugar()
}

func onReady() {
	logger.Infof("Starting LED Sync app")
	systray.SetIcon(ledIcon)
	systray.SetTitle("LED Sync")
	systray.SetTooltip("LED Screen Sync")
	// You can set a custom icon here with systray.SetIcon([]byte{})
	mStart := systray.AddMenuItem("Start Sync", "Start color updates")
	mStop := systray.AddMenuItem("Stop Sync", "Stop color updates")
	mTurnOn := systray.AddMenuItem("Turn On", "Turn on the LED strip")
	mTurnOff := systray.AddMenuItem("Turn Off", "Turn off the LED strip")
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
							logger.Errorf("Failed to get current LED state: %v", err)
						} else {
							originalLEDState = state
							logger.Infof("Saved original LED state: hs_color=%v, brightness=%d", state.Attributes.HSColor, state.Attributes.Brightness)
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
			case <-mTurnOn.ClickedCh:
				go func() {
					err := setLEDOnOff(true)
					if err != nil {
						logger.Errorf("Failed to turn on LED: %v", err)
					}
				}()
			case <-mTurnOff.ClickedCh:
				go func() {
					err := setLEDOnOff(false)
					if err != nil {
						logger.Errorf("Failed to turn off LED: %v", err)
					}
				}()
			case <-mQuit.ClickedCh:
				logger.Infof("Exiting LED Sync app")
				systray.Quit()
				os.Exit(0)
			}
		}
	}()
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
			logger.Fatal("No active display found")
		}
		bounds := screenshot.GetDisplayBounds(0)
		img, err := screenshot.CaptureRect(bounds)
		if err != nil {
			logger.Fatalf("Failed to capture screenshot: %v", err)
		}
		if appConfig.Env.EXPORT_SCREENSHOT {
			if err := saveScreenshotPNG(img, "screenshot.png"); err != nil {
				logger.Warnf("Failed to save screenshot: %v", err)
			}
		}
		// Downscale for fast processing
		smallImg := downscale(img)
		mostColor := mostFrequentColor(smallImg)
		logger.Debugf("Most frequent color: R:%d G:%d B:%d", mostColor.R, mostColor.G, mostColor.B)
		shouldCallHA := false
		if prevColor == nil {
			shouldCallHA = true
		} else {
			dist := colorDistance(mostColor, *prevColor)
			if dist >= colorChangeThreshold {
				shouldCallHA = true
			}
		}
		token := appConfig.Env.HA_TOKEN
		if token == "" {
			logger.Warn("HA_TOKEN not set in config, skipping Home Assistant call.")
		} else if shouldCallHA {
			err := setLEDState(int(mostColor.R), int(mostColor.G), int(mostColor.B), 255, token)
			if err != nil {
				logger.Warnf("Failed to call Home Assistant: %v", err)
			}
			prevColor = &mostColor
		} else {
			logger.Debugf("Skipped Home Assistant call (color change < threshold %.1f)", colorChangeThreshold)
		}
		iterEnd := time.Now()
		iterDuration := iterEnd.Sub(iterStart).Seconds()
		logger.Debugf("Iteration took %.3f seconds", iterDuration)
		if appConfig.Env.EXPORT_JSON {
			top := topColors(smallImg, 10)
			totalPixels := smallImg.Bounds().Dx() * smallImg.Bounds().Dy()
			if err := logTopColorsJSON("colorlog.json", smallImg.Bounds(), top, totalPixels); err != nil {
				logger.Warnf("Failed to log JSON: %v", err)
			}
		}
		select {
		case <-quitChan:
			return
		case <-time.After(interval):
		}
	}
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "********"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

func main() {
	var err error
	appConfig, err = LoadConfig("led-screen-sync.yaml")
	if err != nil {
		logger.Fatalf("Failed to load config: %v", err)
	}
	setupLogger()
	logger.Infof("Config loaded: HA_URL=%s, LED_ENTITY=%s, EXPORT_JSON=%v, EXPORT_SCREENSHOT=%v, COLOR_CHANGE_THRESHOLD=%.2f, UPDATE_INTERVAL_MS=%d, HA_TOKEN=%s",
		appConfig.Env.HA_URL,
		appConfig.Env.LED_ENTITY,
		appConfig.Env.EXPORT_JSON,
		appConfig.Env.EXPORT_SCREENSHOT,
		appConfig.Env.COLOR_CHANGE_THRESHOLD,
		appConfig.Env.UPDATE_INTERVAL_MS,
		maskToken(appConfig.Env.HA_TOKEN),
	)
	systray.Run(onReady, func() {})
}
