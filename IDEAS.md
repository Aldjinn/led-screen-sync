# Improvement Ideas

Based on a read-through of `main.go` and `config.go`. Items are grouped by theme and roughly ordered by value within each group. Effort: **S** = under an hour, **M** = a few hours, **L** = a day or more.

## 1. Bugs and correctness (fix first)

| # | Issue | Where | Fix | Effort |
|---|-------|-------|-----|--------|
| 1 | `UPDATE_INTERVAL_MS` and `COLOR_CHANGE_THRESHOLD` are read from config and logged, but never used. The loop hardcodes `100ms` and `32.0`. | `colorUpdateLoop` | Use `appConfig.Env.*` values. | S |
| 2 | `colorDistance` returns the **squared** distance, but the README says the threshold is 0–441 (a real Euclidean distance). A threshold of 32 currently acts like about 5.7. | `colorDistance` | Add `math.Sqrt`, or document and rename the squared value. | S |
| 3 | If `LoadConfig` fails, `logger.Fatalf` is called while `logger` is still `nil`, so the app panics with a nil pointer instead of printing the error. | `main` | Call `setupLogger()` with defaults first, or use `fmt.Fprintf(os.Stderr, ...)` plus `os.Exit(1)`. | S |
| 4 | "Start Sync" reads the token with `os.Getenv("HA_TOKEN")`, not from config. The original LED state is therefore only saved if the env var happens to be set. | `onReady` | Use `appConfig.Env.HA_TOKEN`. | S |
| 5 | `originalLEDState` is saved but **never restored** on Stop or Quit. | `onReady` | On Stop/Quit, restore the saved state (or turn the LED off, via a config option). | S |
| 6 | `running` is a plain `bool` shared between goroutines (data race). `quitChan <- struct{}{}` is unbuffered and can block the tray goroutine. | globals | Use `context.Context` with `cancel`, or `atomic.Bool`. | M |
| 7 | `logger.Fatal` on "no display" or a capture failure kills the whole app. Capture fails on lock screen, UAC prompts, RDP disconnect, display sleep and resolution change. | `colorUpdateLoop` | Log a warning, back off and retry. Keep the tray alive. | S |
| 8 | Quantization floors each channel to a multiple of 16, and the bucket's lower corner is what gets sent. Colors come out up to 15 levels too dark, and bright colors drift toward gray. | `mostFrequentColor` | Track the average of the pixels in the winning bucket, or use the bucket center. | S |
| 9 | The black/white filter runs after quantization. `<= 16` therefore means "original < 32" and `>= 240` means "original >= 240". The thresholds are asymmetric and undocumented. | `isBlackOrWhite` | Filter on the original pixel, and make the thresholds config values. | S |
| 10 | `topColors` doesn't apply the black/white filter, so the JSON log disagrees with the color actually sent. | `topColors` | Share one histogram function between both. | S |
| 11 | Brightness is hardcoded to 255, so a dark scene gives a full-brightness LED. | `setLEDState` | Derive brightness from the average luminance (with a floor), or add a config value. | S |
| 12 | Request bodies are built with `fmt.Sprintf`. | `setLEDState`, `setLEDOnOff` | Use `json.Marshal` so entity IDs can't break the JSON. | S |
| 13 | A new `http.Client{}` is created per request, with no timeout. A hung HA can stall the loop. | HA calls | Use one shared client with `Timeout: 2s` and keep-alive reuse. | S |
| 14 | `colorlog.json` is read, parsed and rewritten in full **every cycle**. At 10 Hz that is O(n²) and the file is unbounded. The checked-in file is already large. | `logTopColorsJSON` | Append NDJSON lines, or cap/rotate the file. | S |
| 15 | `EXPORT_SCREENSHOT` PNG-encodes a full-resolution frame every 100 ms. | loop | Throttle it (e.g. once per N seconds), or save only the downscaled image. | S |

## 2. Performance

- **Skip `image.At()` and use the pixel buffer.** `CaptureRect` returns `*image.RGBA`, so iterate `img.Pix` directly. `At().RGBA()` allocates and boxes per pixel. (S)
- **Replace the `map[RGB]int` histogram** with a flat `[16*16*16]int` array for quantization step 16. It is faster and has no allocations. (S)
- **Downscale by nearest-neighbor or a stride sample** instead of a bilinear scale of the full frame. For a most-frequent-color histogram, sampling every Nth pixel gives nearly the same result at a fraction of the cost. (S)
- **Use a GPU-friendly capture path** (DXGI Desktop Duplication) instead of GDI `BitBlt`. This is the biggest CPU win at 4K, and it also captures many fullscreen games that GDI can't. (L)
- **Skip unchanged frames.** Hash the downscaled frame (xxhash is already an indirect dependency) and skip analysis when nothing changed. (S)
- **Adaptive interval.** Slow down when the screen is static, and speed up on fast changes. (M)

## 3. Better color logic (the core of the product)

- **Most-frequent color is a weak proxy for "what the screen looks like".** Gray UI chrome or a large dark-blue background often wins over the interesting subject. Alternatives:
  - Weight pixels by saturation, so vivid colors win over dull ones.
  - Use k-means or median-cut and pick the most *prominent* cluster (frequency × saturation).
  - Send the **average color** in a perceptual space (Oklab) as a selectable mode. (M)
- **Selectable modes:** `dominant`, `average`, `vibrant`. (S once the above exists)
- **Smooth transitions.** Blend from the previous color (an exponential moving average), or send HA's `transition` parameter, to avoid harsh jumps. (S)
- **Perceptual distance for the threshold.** Use CIE Lab / Oklab ΔE instead of RGB distance. A change of 10 in blue is not the same as 10 in green. (M)
- **Saturation/vibrance boost** and **gamma/brightness curve** options, since LED strips usually look washed out. (S)
- **Letterbox/pillarbox detection.** Crop black bars before analysis, so movies don't skew toward black or gray. (M)
- **Region of interest.** Analyze only the screen edges (ambilight-style) or a configurable rectangle. (M)
- **Black-scene behavior:** turn the LED off or dim it, or hold the last color, instead of falling back to a near-black color. (S)

## 4. Multi-monitor and multi-light

- Config option `DISPLAY_INDEX` (currently hardcoded to display 0), plus "all displays combined". (S)
- **Multiple LED zones:** split the screen into left/right or top/bottom and drive several HA entities with different colors. This is the classic ambilight feel. (M–L)
- Support a **list of entities** (the same color to several lights). (S)

## 5. Faster and more reliable output

- **Rate-limit HA calls** with a minimum gap (e.g. 150 ms). Cheap Zigbee/Wi-Fi bulbs choke at 10 calls/s. (S)
- **Handle failures with backoff.** After N failed calls, pause and show an error state in the tray. (S)
- **Direct protocols** for real-time use: WLED UDP (DRGB/DNRGB), Hyperion/HyperHDR or the Philips Hue Entertainment API. Home Assistant REST adds latency. (L)
- **Home Assistant WebSocket API** for lower overhead per update than REST. (M)
- **Test the connection** on startup (`GET /api/`) and show the result in the tray tooltip. (S)

## 6. Tray UX

- Show status in the tray: running or stopped, last color, last error. Tooltip text plus a live color swatch icon. (M)
- **Pause when the PC locks, sleeps or the display turns off**, and resume automatically. Use the `WTSRegisterSessionNotification` or power events. (M)
- **Global hotkey** to toggle sync. (M)
- Menu entries: "Open config file", "Open log", "Reload config", "Start with Windows" (a toggle that writes the `Run` registry key). (M)
- **Hot-reload config** with fsnotify, or via a menu item. Changing the threshold shouldn't need a restart. (S–M)
- Menu items for the color mode and the update rate. (M)
- Replace the About dialog's Yes/No GitHub prompt with a plain "About" plus an "Open GitHub" menu item. This drops the `sqweek/dialog` dependency. (S)
- Remove the cross-platform branch in `openGitHubRepo`. The tool is Windows-only (the build, systray and capture are used on Windows), and `ShellExecute` alone is enough. (S)

## 7. Configuration and security

- **Validate the config on load** and apply defaults. A missing `UPDATE_INTERVAL_MS` currently decodes to `0`. Report clear errors for a missing URL, token or entity. (S)
- **Allow env var overrides** (`HA_TOKEN`, etc.), so the token doesn't have to live in a plain-text file. (S)
- **Store the token in Windows Credential Manager** (DPAPI) instead of YAML. (M)
- **Hot-path logging:** the startup log prints config values (the token is masked, good). Log HA errors with the status code and a body snippet. (S)
- Look for the config next to the exe (`os.Executable()`), not the current working directory. Autostart usually launches in `C:\Windows\System32`, which breaks the relative path. (S)
- Write logs to a rotating file (lumberjack) in `%APPDATA%`, not just stdout. A tray app has no console. The repo already contains a `.log` file, so this is probably done via redirect. (S)
- `HA_URL` normalization: strip trailing slashes and validate the scheme. (S)

## 8. Code structure and tests

- `main.go` mixes UI, capture, color math, HA client and file logging in 720 lines. Split into packages or files: `ha/` (client), `colors/` (analysis), `capture/`, `tray/`. (M)
- Hide capture, HA and clock behind **interfaces** so the loop can be unit-tested without a display or a network. (M)
- Replace global state (`appConfig`, `logger`, `running`) with a small `App` struct. (M)
- **Tests worth adding:** `rgbToHSColor`/`hsToRGB` round trip (`hsToRGB` looks unused now, so remove it if so), threshold logic with the squared-distance fix, histogram on synthetic images (red 60%, blue 40%), config validation, an HA client against `httptest.Server`. (M)
- **Benchmarks** for `mostFrequentColor` at 1080p and 4K, to guard the performance work. (S)
- Remove dead code: `hsToRGB`, `colorName` is only used by the JSON log, and the `rgbToHSColor`/`haState.HSColor` path is only used for logging. (S)
- Use `golangci-lint` and `go vet` in CI, plus `-race` in `go test`. (S)
- CI: sign the release `.exe` (SmartScreen warnings are the biggest adoption blocker for tray tools), publish checksums, and attach the zip automatically. (M)
- A checked-in `led-screen-sync.zip`, `.exe`, `.log` and `colorlog.json` sit in the working dir. They are ignored by git, which is fine. Confirm `.gitignore` also covers `led-screen-sync.yaml`, since it holds the token. (S)

## 9. Larger feature ideas

- **Settings GUI** (a small webview or a local web page on `localhost`) with a live preview of the detected color and a threshold slider. (L)
- **Per-app profiles:** different mode or brightness depending on the foreground window (games vs. movies vs. desktop). (L)
- **Audio-reactive mode** as an alternative to screen sync. (L)
- **Scheduling:** disable sync at night, or cap brightness after a set time. (M)
- **Home Assistant integration in reverse:** expose a switch entity (via MQTT discovery) so HA automations can start or stop the sync. (M)
- **Cross-platform capture** (macOS/Linux) if there is demand. The `systray` and `screenshot` libraries already support both. (L)

## Suggested order

1. **Quick wins (about half a day):** items 1–5, 7, 8, 11–13 in section 1. These fix real bugs, and the first two make the documented config options work.
2. **Robustness:** retry on capture errors, rate limiting, pause on lock, restore the original state, config validation.
3. **Quality of output:** saturation-weighted or vibrant mode, smoothing, brightness, letterbox crop.
4. **Performance:** `Pix` iteration with a flat histogram, then consider DXGI.
5. **Structure and tests**, then larger features such as multi-zone output.
