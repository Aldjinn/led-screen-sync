# led-screen-sync

`led-screen-sync` is a fast, efficient Windows tool that detects the most used color on your screen and syncs it to a Home Assistant-controlled LED strip. It features a Windows system tray icon for start/stop and on/off control, optional logging and screenshot export, and is highly configurable via YAML.

## Features

- Detects the most frequent color on your screen (ignoring near-black/white)
- Sends color updates to Home Assistant as RGB values
- System tray icon with Start, Stop, Turn On, Turn Off, and Quit
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
- `EXPORT_JSON`: If `true`, writes a JSON log of the top detected colors for each cycle to `colorlog.json`.
- `EXPORT_SCREENSHOT`: If `true`, saves a screenshot of the analyzed screen as `screenshot.png` each cycle.
- `COLOR_CHANGE_THRESHOLD`: The minimum color distance (0-441) required to trigger a color update. Lower values make the LED more sensitive to small color changes.
- `UPDATE_INTERVAL_MS`: How often (in milliseconds) the screen is analyzed and the LED color is updated.
- `LOG_LEVEL`: Controls the verbosity of log output. Use `debug` for development, `info` for normal use, or higher levels to reduce output.

## Building Locally

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

1. Edit `led-screen-sync.yaml` with your Home Assistant details.
2. Run the tool:

   ```bash
   ./led-screen-sync.exe
   ```

3. Use the tray icon to Start/Stop syncing, or Turn On/Off the LED strip.

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
