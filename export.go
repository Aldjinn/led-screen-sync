package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"os"
	"time"
)

const (
	colorLogFile       = "colorlog.ndjson"
	colorLogMaxBytes   = 10 << 20 // rotate at 10 MB
	screenshotInterval = 5 * time.Second
)

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
	Detected   string      `json:"detected,omitempty"`
	TopColors  []ColorStat `json:"top_colors"`
}

// appendColorLog appends one JSON object per line to filename, rotating the
// file to filename+".1" once it grows past colorLogMaxBytes.
func appendColorLog(filename string, bounds image.Rectangle, detected RGB, top []colorCount, totalPixels int) error {
	stats := make([]ColorStat, 0, len(top))
	for _, entry := range top {
		stats = append(stats, ColorStat{
			R:       entry.Color.R,
			G:       entry.Color.G,
			B:       entry.Color.B,
			Name:    colorName(entry.Color),
			Percent: float64(entry.Count) / float64(max(totalPixels, 1)) * 100,
		})
	}
	line, err := json.Marshal(LogEntry{
		Timestamp:  time.Now().Format(time.RFC3339),
		ScreenSize: fmt.Sprintf("%dx%d", bounds.Dx(), bounds.Dy()),
		Detected:   fmt.Sprintf("#%02x%02x%02x", detected.R, detected.G, detected.B),
		TopColors:  stats,
	})
	if err != nil {
		return err
	}

	if fi, err := os.Stat(filename); err == nil && fi.Size() > colorLogMaxBytes {
		_ = os.Remove(filename + ".1")
		_ = os.Rename(filename, filename+".1")
	}
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

func saveScreenshotPNG(img image.Image, filename string) error {
	f, err := os.Create(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
