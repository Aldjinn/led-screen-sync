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

Edit `led-screen-sync.yaml` to set your Home Assistant URL, token, LED entity, and options:

```yaml
env:
  HA_URL: "http://your-homeassistant:8123"
  HA_TOKEN: "your-long-lived-access-token"
  LED_ENTITY: "light.your_led_strip"
  EXPORT_JSON: false
  EXPORT_SCREENSHOT: false
  COLOR_CHANGE_THRESHOLD: 32.0
  UPDATE_INTERVAL_MS: 100
```

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
