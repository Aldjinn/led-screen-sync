package main

import (
	"context"
	"fmt"
	"hash/maphash"
	"image"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kbinani/screenshot"
)

// syncer owns the background capture → Home Assistant loop.
type syncer struct {
	mu       sync.Mutex
	cancel   context.CancelFunc
	done     chan struct{}
	onStatus func(string) // called from the loop goroutine; may be nil

	// Live info for the settings page.
	status   atomic.Pointer[string]
	detected atomic.Uint32 // packed color, 0 = none
	sent     atomic.Uint32
}

// packColor encodes c so that a zero value means "no color yet".
func packColor(c RGB) uint32 { return 1<<24 | uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B) }

func unpackHex(v uint32) string {
	if v == 0 {
		return ""
	}
	return fmt.Sprintf("#%06x", v&0xffffff)
}

// Snapshot is a point-in-time view of the loop for the UI.
type Snapshot struct {
	Running  bool
	Status   string
	Detected string // "#rrggbb" or ""
	Sent     string
}

func (s *syncer) Snapshot() Snapshot {
	snap := Snapshot{Running: s.Running(), Status: "Stopped"}
	if p := s.status.Load(); p != nil {
		snap.Status = *p
	}
	if snap.Running {
		snap.Detected = unpackHex(s.detected.Load())
		snap.Sent = unpackHex(s.sent.Load())
	}
	return snap
}

func (s *syncer) setStatus(msg string) {
	s.status.Store(&msg)
	if s.onStatus != nil {
		s.onStatus(msg)
	}
}

// Running reports whether the loop is active.
func (s *syncer) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cancel != nil
}

// Start launches the loop using the current config. It is a no-op if the
// loop is already running.
func (s *syncer) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		return
	}
	cfg := currentConfig()
	client := newHAClient(cfg.Env.HA_URL, cfg.Env.HA_TOKEN, cfg.Env.LED_ENTITY)
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	s.detected.Store(0)
	s.sent.Store(0)
	go func(done chan struct{}) {
		defer close(done)
		s.run(ctx, cfg, client)
	}(s.done)
}

// Stop ends the loop and waits until it has finished (including restoring
// the original LED state, if configured).
func (s *syncer) Stop() {
	s.mu.Lock()
	cancel, done := s.cancel, s.done
	s.cancel, s.done = nil, nil
	s.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel()
	<-done
}

// backoff returns base<<n capped at limit.
func backoff(base time.Duration, n int, limit time.Duration) time.Duration {
	d := base << min(n, 10)
	if d <= 0 || d > limit {
		return limit
	}
	return d
}

func (s *syncer) run(ctx context.Context, cfg *Config, client *haClient) {
	e := cfg.Env
	interval := time.Duration(e.UPDATE_INTERVAL_MS) * time.Millisecond
	minGap := time.Duration(e.MIN_CALL_INTERVAL_MS) * time.Millisecond

	// Remember the LED state so it can be restored when we stop.
	var original *haState
	if st, err := client.State(ctx); err != nil {
		logger.Warnf("Could not read the current LED state (it won't be restored): %v", err)
	} else {
		original = st
		logger.Infof("Saved original LED state: state=%s rgb=%v", st.State, st.Attributes.RGBColor)
	}
	defer func() {
		defer s.setStatus("Stopped")
		if original == nil || e.KEEP_COLOR_ON_STOP {
			return
		}
		rctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := client.Restore(rctx, original); err != nil {
			logger.Warnf("Failed to restore the original LED state: %v", err)
		} else {
			logger.Info("Restored original LED state")
		}
	}()

	var (
		prev        *RGB // last color successfully sent
		cached      RGB  // color detected from the last distinct frame
		seed        = maphash.MakeSeed()
		lastHash    uint64
		haveHash    bool
		lastCall    time.Time
		nextAllowed time.Time // HA backoff after failures
		lastShot    time.Time
		capFailures int
		haFailures  int
		warnedIdx   bool
	)
	s.setStatus("Running")
	logger.Infof("Sync started (interval=%v, threshold=%.1f, mode=%s)", interval, e.COLOR_CHANGE_THRESHOLD, e.COLOR_MODE)

	for {
		wait := interval

		// --- capture -------------------------------------------------------
		n := screenshot.NumActiveDisplays()
		idx := e.DISPLAY_INDEX
		if idx >= n {
			if !warnedIdx && n > 0 {
				logger.Warnf("DISPLAY_INDEX %d not available (%d displays); using display 0", idx, n)
				warnedIdx = true
			}
			idx = 0
		}
		var img *image.RGBA
		if n > 0 {
			img = captureDisplay(idx)
		}
		if img == nil {
			capFailures++
			if capFailures == 1 || capFailures%20 == 0 {
				logger.Warnf("Screen capture failed (attempt %d); retrying", capFailures)
			}
			s.setStatus("Waiting for screen")
			wait = backoff(interval, capFailures, 5*time.Second)
		} else {
			if capFailures > 0 {
				logger.Info("Screen capture recovered")
				capFailures = 0
				s.setStatus("Running")
			}

			if e.EXPORT_SCREENSHOT && time.Since(lastShot) >= screenshotInterval {
				if err := saveScreenshotPNG(img, filepath.Join(baseDir, "screenshot.png")); err != nil {
					logger.Warnf("Failed to save screenshot: %v", err)
				}
				lastShot = time.Now()
			}

			// --- analysis (skipped when the frame didn't change) -------------
			small := downscale(img)
			h := maphash.Bytes(seed, small.Pix)
			if !haveHash || h != lastHash {
				cached = detectColor(small, e.COLOR_MODE)
				lastHash, haveHash = h, true
				logger.Debugf("Detected color: R:%d G:%d B:%d", cached.R, cached.G, cached.B)

				if e.EXPORT_JSON {
					top := topColors(small, 10)
					total := small.Bounds().Dx() * small.Bounds().Dy()
					if err := appendColorLog(filepath.Join(baseDir, colorLogFile), small.Bounds(), cached, top, total); err != nil {
						logger.Warnf("Failed to write color log: %v", err)
					}
				}
			}

			s.detected.Store(packColor(cached))

			// --- send to Home Assistant -------------------------------------
			should := prev == nil || colorDistance(cached, *prev) >= e.COLOR_CHANGE_THRESHOLD
			now := time.Now()
			switch {
			case !should:
				logger.Debugf("Skipped update (change below threshold %.1f)", e.COLOR_CHANGE_THRESHOLD)
			case now.Before(nextAllowed) || now.Sub(lastCall) < minGap:
				logger.Debugf("Skipped update (rate limit / backoff)")
			default:
				brightness := brightnessFor(cached, e.AUTO_BRIGHTNESS, e.BRIGHTNESS, e.MIN_BRIGHTNESS)
				err := client.SetColor(ctx, cached, brightness, e.TRANSITION_MS)
				lastCall = now
				switch {
				case ctx.Err() != nil:
					return
				case err != nil:
					haFailures++
					if haFailures == 1 || haFailures%30 == 0 {
						logger.Warnf("Home Assistant call failed (attempt %d): %v", haFailures, err)
					}
					nextAllowed = now.Add(backoff(time.Second, haFailures-1, 30*time.Second))
					s.setStatus("Home Assistant error")
				default:
					if haFailures > 0 {
						logger.Info("Home Assistant connection recovered")
						haFailures = 0
						s.setStatus("Running")
					}
					c := cached
					prev = &c
					s.sent.Store(packColor(c))
				}
			}
		}

		select {
		case <-ctx.Done():
			logger.Info("Sync stopped")
			return
		case <-time.After(wait):
		}
	}
}

// captureDisplay grabs one display, returning nil on any failure (lock
// screen, UAC prompt, RDP disconnect, display sleep, ...).
func captureDisplay(idx int) *image.RGBA {
	img, err := screenshot.CaptureRect(screenshot.GetDisplayBounds(idx))
	if err != nil || img == nil {
		logger.Debugf("Capture error: %v", err)
		return nil
	}
	return img
}
