# led-screen-sync

`led-screen-sync` is a fast, efficient Windows tool that detects the most used color on your screen and syncs it to a Home Assistant-controlled LED strip. It features a Windows system tray icon for start/stop and on/off control, optional logging and screenshot export, and is highly configurable via YAML.

## Features

- Detects the most frequent color on your screen (ignoring near-black/white)
- Sends color updates to Home Assistant as RGB values
- **Settings page**: tray menu → *Settings...* opens a local page in your browser with a live color preview, sliders for sensitivity/fade/brightness, presets (Calm/Balanced/Reactive), a light picker and Start/Stop. Changes are saved to the YAML file (comments are kept) and applied immediately
- System tray icon with Start, Stop, Turn On, Turn Off, Open/Reload Config, "Start with Windows" and Quit, plus a live status tooltip
- Restores the LED's previous state when you stop syncing (can be disabled)
- Color modes (dominant, vibrant, average), optional auto brightness and fade transitions
- Resilient: retries with backoff when the screen can't be captured (lock screen, UAC) or Home Assistant is unreachable
- Optional JSON logging and screenshot export
- All configuration via `led-screen-sync.yaml`
- Fast, efficient, and easy to maintain

## Configuration

All options are set in `led-screen-sync.yaml` under the `env:` section. Here is an example and explanation of each option:

```yaml
env:
  HA_URL: "http://your-homeassistant:8123"         # Home Assistant API base URL (no trailing slash)
  HA_TOKEN: "your-long-lived-access-token"         # Home Assistant long-lived access token
  LED_ENTITY: "light.your_led_strip"               # Entity ID of your LED strip in Home Assistant
  EXPORT_JSON: false                                # If true, writes color statistics to colorlog.json
  EXPORT_SCREENSHOT: false                          # If true, saves a screenshot as screenshot.png each cycle
  COLOR_CHANGE_THRESHOLD: 32.0                      # Minimum color distance to trigger an update (higher = less sensitive)
  UPDATE_INTERVAL_MS: 100                           # How often to check the screen and update (milliseconds)
  LOG_LEVEL: "info"                                # Log level: debug, info, warn, error, dpanic, panic, fatal
```

**Option details:**

- `HA_URL`: The base URL of your Home Assistant instance (e.g., `http://192.168.1.2:8123`).
- `HA_TOKEN`: Your Home Assistant long-lived access token (see Home Assistant profile > Long-Lived Access Tokens).
- `LED_ENTITY`: The entity ID of your LED strip in Home Assistant (e.g., `light.my_led_strip`).
- `EXPORT_JSON`: If `true`, appends the top detected colors of each new frame to `colorlog.ndjson` (one JSON object per line, rotated at 10 MB).
- `EXPORT_SCREENSHOT`: If `true`, saves a screenshot of the analyzed screen as `screenshot.png` (at most every 5 seconds).
- `COLOR_CHANGE_THRESHOLD`: The minimum color distance (0-441) required to trigger a color update. Lower values make the LED more sensitive to small color changes. Default: 32.
- `UPDATE_INTERVAL_MS`: How often (in milliseconds) the screen is analyzed and the LED color is updated. Default: 100.
- `LOG_LEVEL`: Controls the verbosity of log output. Use `debug` for development, `info` for normal use, or higher levels to reduce output.

Optional settings (all have defaults, see `led-screen-sync.example.yaml`):

- `DISPLAY_INDEX`: Which monitor to analyze (0 = primary).
- `COLOR_MODE`: `dominant` (most frequent color), `vibrant` (favors saturated colors) or `average`.
- `BRIGHTNESS` / `AUTO_BRIGHTNESS` / `MIN_BRIGHTNESS`: Fixed brightness (1-255), or derive it from the detected color so dark scenes dim the LED.
- `TRANSITION_MS`: Fade time per update, passed to Home Assistant.
- `MIN_CALL_INTERVAL_MS`: Minimum gap between Home Assistant calls (default 150).
- `KEEP_COLOR_ON_STOP`: Leave the last color on the LED instead of restoring the previous state.

`HA_URL`, `HA_TOKEN` and `LED_ENTITY` may also be set as environment variables, which override the YAML values. The config file is looked up next to `led-screen-sync.exe` first, then in the current directory; use `-config <path>` to choose another file. Use **Reload Config** in the tray menu to apply changes without restarting.

## Settings page

Right-click the tray icon and choose **Settings...** (or start the exe with `-settings`). The page is served by the app itself on `127.0.0.1` with a random port and a per-run access token, so it is only reachable from your own PC and only through the link the app opens. It lets you:

- watch the detected color and the color sent to the LED in real time,
- start/stop syncing and turn the light on/off,
- find your lights in Home Assistant and test the connection,
- tune sensitivity, fade time, update rate, color mode and brightness (or pick a preset),
- save: the YAML file is updated in place (comments and unknown keys are kept, the previous file is saved as `led-screen-sync.yaml.bak`) and the new settings are applied right away - a running sync restarts automatically.

The access token is never sent to the page. Leave the token field empty to keep the saved one.

## Installation

### Download Pre-built Binary

Download the latest release from the [releases page](../../releases) and run `led-screen-sync.exe` directly.

### Building Locally

1. Install Go (1.24+ recommended): <https://golang.org/dl/>
2. Clone this repository:

   ```bash
   git clone <repo-url>
   cd led-screen-sync
   ```

3. Download dependencies:

   ```bash
   go mod tidy
   ```

4. Build the executable:

   ```bash
   go build -ldflags -H=windowsgui -o led-screen-sync.exe
   ```

5. Run directly:

   ```bash
   go run .
   ```

## Running

1. Copy `led-screen-sync.example.yaml` to `led-screen-sync.yaml` (next to the exe) and fill in your Home Assistant details.
2. Run the tool:

   ```bash
   ./led-screen-sync.exe
   ```

3. Use the tray icon to Start/Stop syncing, or Turn On/Off the LED strip. Configuration errors are shown in a dialog at startup.

## Testing

Run all unit tests:

```bash
go test ./...
```

## Requirements

- Windows OS (uses Windows screenshot APIs)
- Go 1.24 or newer
- Home Assistant with an accessible API and a compatible LED entity

## License

MIT
