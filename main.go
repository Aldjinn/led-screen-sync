package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"github.com/getlantern/systray"
	"github.com/sqweek/dialog"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Build-time variables (set via -ldflags)
var (
	version = "dev"     // Will be replaced with git tag
	commit  = "unknown" // Will be replaced with git commit hash
	date    = "unknown" // Will be replaced with build date
)

const (
	configFileName = "led-screen-sync.yaml"
	repoURL        = "https://github.com/aldjinn/led-screen-sync"
	swShowNormal   = 1
)

var (
	shell32       = syscall.NewLazyDLL("shell32.dll")
	shellExecuteW = shell32.NewProc("ShellExecuteW")
)

var (
	logger     = zap.NewNop().Sugar() // replaced by setupLogger; never nil
	cfgPtr     atomic.Pointer[Config]
	configPath string // absolute path of the YAML file in use

	openSettingsOnStart bool   // -settings flag
	baseDir             string // directory for logs/screenshots (next to the config)
)

func currentConfig() *Config { return cfgPtr.Load() }

// ---------------------------------------------------------------------------
// startup helpers

func setupLogger(level string) {
	cfg := zap.NewProductionConfig()
	cfg.EncoderConfig.TimeKey = "ts"
	cfg.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	cfg.Encoding = "console"
	cfg.OutputPaths = []string{"stdout"}

	lvl := zapcore.InfoLevel
	_ = lvl.UnmarshalText([]byte(level)) // keeps Info for unknown values
	cfg.Level = zap.NewAtomicLevelAt(lvl)

	l, err := cfg.Build()
	if err != nil {
		return // keep the existing (no-op or previous) logger
	}
	logger = l.Sugar()
}

// findConfig locates the YAML file: an explicit -config path wins, then the
// directory of the executable (autostart launches with an unrelated working
// directory), then the current directory.
func findConfig(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	var candidates []string
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Join(filepath.Dir(exe), configFileName))
	}
	candidates = append(candidates, configFileName)
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return filepath.Abs(c)
		}
	}
	return "", fmt.Errorf("%s not found (looked next to the executable and in the current directory).\nCopy led-screen-sync.example.yaml to %s and edit it", configFileName, configFileName)
}

// loadAndValidate reads configPath, applies env overrides and validates.
func loadAndValidate(path string) (*Config, error) {
	cfg, err := LoadConfig(path)
	if err != nil {
		return nil, fmt.Errorf("could not read %s: %w", path, err)
	}
	cfg.applyEnvOverrides()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid configuration in %s:\n%w", path, err)
	}
	return cfg, nil
}

// fatalDialog reports a startup error. The release build has no console
// (-H=windowsgui), so a dialog is the only way the user will see it.
func fatalDialog(msg string) {
	fmt.Fprintln(os.Stderr, msg)
	dialog.Message("%s", msg).Title("LED Screen Sync").Error()
	os.Exit(1)
}

// ---------------------------------------------------------------------------
// Windows shell helpers

// openURL opens a URL in the default browser.
func openURL(target string) {
	urlPtr, _ := syscall.UTF16PtrFromString(target)
	openPtr, _ := syscall.UTF16PtrFromString("open")
	shellExecuteW.Call(
		0,
		uintptr(unsafe.Pointer(openPtr)),
		uintptr(unsafe.Pointer(urlPtr)),
		0,
		0,
		uintptr(swShowNormal),
	)
}

// showAboutDialog displays version info and offers to open the GitHub repository.
func showAboutDialog() {
	aboutText := fmt.Sprintf("LED Screen Sync\n\nVersion: %s\nCommit: %.8s\nBuilt: %s\n\nGitHub: %s\n\nWould you like to open the GitHub repository?",
		version, commit, date, repoURL)
	if dialog.Message("%s", aboutText).Title("About LED Screen Sync").YesNo() {
		openURL(repoURL)
	}
}

// ---------------------------------------------------------------------------
// tray

var (
	loop     = &syncer{}
	settings = &settingsServer{loop: loop, reload: reloadConfig}
)

// reloadConfig re-reads the YAML file and applies it, restarting the sync loop
// if it is running. On error the previous settings stay active.
func reloadConfig() error {
	cfg, err := loadAndValidate(configPath)
	if err != nil {
		return err
	}
	cfgPtr.Store(cfg)
	setupLogger(cfg.Env.LOG_LEVEL)
	logger.Info("Configuration reloaded")
	if loop.Running() {
		loop.Stop()
		loop.Start()
	}
	return nil
}

// openSettings opens the local settings page in the browser.
func openSettings() {
	u, err := settings.URL()
	if err != nil {
		logger.Errorf("Could not start the settings page: %v", err)
		go dialog.Message("Could not start the settings page:\n%v", err).Title("LED Screen Sync").Error()
		return
	}
	openURL(u)
}

func newClientFromConfig() *haClient {
	e := currentConfig().Env
	return newHAClient(e.HA_URL, e.HA_TOKEN, e.LED_ENTITY)
}

// lightCommand runs a one-off on/off call without blocking the tray loop.
func lightCommand(name string, call func(*haClient, context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		logger.Infof("Turning LED %s", name)
		if err := call(newClientFromConfig(), ctx); err != nil {
			logger.Errorf("Failed to turn %s LED: %v", name, err)
		}
	}()
}

func onReady() {
	systray.SetIcon(ledIcon)
	systray.SetTitle("LED Sync")
	setTrayStatus("Stopped")

	mStatus := systray.AddMenuItem("Status: Stopped", "Current state")
	mStatus.Disable()
	mSettings := systray.AddMenuItem("Settings...", "Open the settings page in your browser")
	systray.AddSeparator()
	mStart := systray.AddMenuItem("Start Sync", "Start color updates")
	mStop := systray.AddMenuItem("Stop Sync", "Stop color updates")
	mTurnOn := systray.AddMenuItem("Turn On", "Turn on the LED strip")
	mTurnOff := systray.AddMenuItem("Turn Off", "Turn off the LED strip")
	systray.AddSeparator()
	mOpenCfg := systray.AddMenuItem("Open Config", "Edit led-screen-sync.yaml")
	mReload := systray.AddMenuItem("Reload Config", "Re-read led-screen-sync.yaml")
	mAutostart := systray.AddMenuItem("Start with Windows", "Launch automatically at sign-in")
	systray.AddSeparator()
	mAbout := systray.AddMenuItem("About", "About LED Screen Sync")
	mQuit := systray.AddMenuItem("Quit", "Quit the app")
	mStop.Disable()
	if autostartEnabled() {
		mAutostart.Check()
	}

	// The loop can be started or stopped from the tray or the settings page,
	// so the menu follows its status instead of tracking clicks.
	loop.onStatus = func(msg string) {
		mStatus.SetTitle("Status: " + msg)
		setTrayStatus(msg)
		if msg == "Stopped" {
			mStart.Enable()
			mStop.Disable()
		} else {
			mStart.Disable()
			mStop.Enable()
		}
	}
	if openSettingsOnStart {
		go openSettings()
	}

	// Check connectivity once so a wrong URL or token shows up in the log early.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := newClientFromConfig().Ping(ctx); err != nil {
			logger.Warnf("Home Assistant check failed: %v", err)
			setTrayStatus("Home Assistant unreachable")
		} else {
			logger.Info("Home Assistant connection OK")
		}
	}()

	go func() {
		for {
			select {
			case <-mStart.ClickedCh:
				loop.Start()
			case <-mStop.ClickedCh:
				loop.Stop()
			case <-mSettings.ClickedCh:
				go openSettings()
			case <-mTurnOn.ClickedCh:
				lightCommand("on", (*haClient).TurnOn)
			case <-mTurnOff.ClickedCh:
				lightCommand("off", (*haClient).TurnOff)
			case <-mOpenCfg.ClickedCh:
				if err := exec.Command("notepad.exe", configPath).Start(); err != nil {
					logger.Errorf("Failed to open config: %v", err)
				}
			case <-mReload.ClickedCh:
				if err := reloadConfig(); err != nil {
					logger.Errorf("Reload failed, keeping previous settings: %v", err)
					go dialog.Message("%s", err.Error()).Title("LED Screen Sync").Error()
				}
			case <-mAutostart.ClickedCh:
				want := !mAutostart.Checked()
				if err := setAutostart(want); err != nil {
					logger.Errorf("Failed to change autostart: %v", err)
					break
				}
				if want {
					mAutostart.Check()
				} else {
					mAutostart.Uncheck()
				}
			case <-mAbout.ClickedCh:
				go showAboutDialog()
			case <-mQuit.ClickedCh:
				logger.Info("Exiting LED Sync app")
				loop.Stop() // restores the original LED state
				systray.Quit()
				return
			}
		}
	}()
}

func setTrayStatus(msg string) {
	systray.SetTooltip("LED Screen Sync - " + msg)
}

func maskToken(token string) string {
	if len(token) <= 8 {
		return "********"
	}
	return token[:4] + "..." + token[len(token)-4:]
}

func main() {
	showVersion := flag.Bool("v", false, "show version information")
	cfgFlag := flag.String("config", "", "path to the YAML config file")
	settingsFlag := flag.Bool("settings", false, "open the settings page at startup")
	flag.Parse()
	openSettingsOnStart = *settingsFlag

	if *showVersion {
		fmt.Printf("LED Screen Sync v%s\n", version)
		fmt.Printf("Commit: %s\n", commit)
		fmt.Printf("Built: %s\n", date)
		os.Exit(0)
	}

	setupLogger("info")

	path, err := findConfig(*cfgFlag)
	if err != nil {
		fatalDialog(err.Error())
	}
	cfg, err := loadAndValidate(path)
	if err != nil {
		fatalDialog(err.Error())
	}
	configPath = path
	baseDir = filepath.Dir(path)
	cfgPtr.Store(cfg)
	setupLogger(cfg.Env.LOG_LEVEL)

	e := cfg.Env
	logger.Infof("Starting LED Sync app")
	logger.Infof("Version: %s, Commit: %s, Built: %s", version, commit, date)
	logger.Infof("Config: %s", configPath)
	logger.Infof("Settings: HA_URL=%s LED_ENTITY=%s HA_TOKEN=%s mode=%s threshold=%.1f interval=%dms display=%d brightness=%d auto=%v",
		e.HA_URL, e.LED_ENTITY, maskToken(e.HA_TOKEN), e.COLOR_MODE, e.COLOR_CHANGE_THRESHOLD,
		e.UPDATE_INTERVAL_MS, e.DISPLAY_INDEX, e.BRIGHTNESS, e.AUTO_BRIGHTNESS)

	systray.Run(onReady, func() {})
}
