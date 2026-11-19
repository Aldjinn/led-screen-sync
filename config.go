package main

import (
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config mirrors led-screen-sync.yaml.
type Config struct {
	Env EnvConfig `yaml:"env"`
}

// EnvConfig is the "env:" section of the YAML file. The field names double as
// the JSON keys used by the settings page.
type EnvConfig struct {
	HA_URL                 string  `yaml:"HA_URL"`
	HA_TOKEN               string  `yaml:"HA_TOKEN"`
	LED_ENTITY             string  `yaml:"LED_ENTITY"`
	EXPORT_JSON            bool    `yaml:"EXPORT_JSON"`
	EXPORT_SCREENSHOT      bool    `yaml:"EXPORT_SCREENSHOT"`
	COLOR_CHANGE_THRESHOLD float64 `yaml:"COLOR_CHANGE_THRESHOLD"`
	UPDATE_INTERVAL_MS     int     `yaml:"UPDATE_INTERVAL_MS"`
	LOG_LEVEL              string  `yaml:"LOG_LEVEL"`

	// Optional settings added after the initial release. All have defaults.
	DISPLAY_INDEX        int    `yaml:"DISPLAY_INDEX"`        // which monitor to analyze (0 = primary)
	COLOR_MODE           string `yaml:"COLOR_MODE"`           // dominant | vibrant | average
	BRIGHTNESS           int    `yaml:"BRIGHTNESS"`           // 1-255, fixed brightness (or the maximum in auto mode)
	AUTO_BRIGHTNESS      bool   `yaml:"AUTO_BRIGHTNESS"`      // derive brightness from the detected color
	MIN_BRIGHTNESS       int    `yaml:"MIN_BRIGHTNESS"`       // lower bound in auto mode
	TRANSITION_MS        int    `yaml:"TRANSITION_MS"`        // Home Assistant transition time per update
	MIN_CALL_INTERVAL_MS int    `yaml:"MIN_CALL_INTERVAL_MS"` // rate limit for Home Assistant calls
	KEEP_COLOR_ON_STOP   bool   `yaml:"KEEP_COLOR_ON_STOP"`   // don't restore the original LED state on Stop/Quit
}

// Defaults used when a value is omitted (or zero) in the YAML file.
const (
	defaultThreshold     = 32.0
	defaultIntervalMS    = 100
	defaultLogLevel      = "info"
	defaultColorMode     = "dominant"
	defaultBrightness    = 255
	defaultMinBrightness = 10
	defaultMinCallGapMS  = 150
)

// LoadConfig reads and parses the YAML file and fills in defaults.
// It does not validate; call Validate for that.
func LoadConfig(path string) (*Config, error) {
	var config Config
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	dec := yaml.NewDecoder(f)
	if err := dec.Decode(&config); err != nil {
		return nil, err
	}
	config.applyDefaults()
	return &config, nil
}

func (c *Config) applyDefaults() {
	e := &c.Env
	e.HA_URL = strings.TrimRight(strings.TrimSpace(e.HA_URL), "/")
	e.HA_TOKEN = strings.TrimSpace(e.HA_TOKEN)
	e.LED_ENTITY = strings.TrimSpace(e.LED_ENTITY)
	if e.COLOR_CHANGE_THRESHOLD <= 0 {
		e.COLOR_CHANGE_THRESHOLD = defaultThreshold
	}
	if e.UPDATE_INTERVAL_MS <= 0 {
		e.UPDATE_INTERVAL_MS = defaultIntervalMS
	}
	if e.LOG_LEVEL == "" {
		e.LOG_LEVEL = defaultLogLevel
	}
	e.LOG_LEVEL = strings.ToLower(e.LOG_LEVEL)
	if e.COLOR_MODE == "" {
		e.COLOR_MODE = defaultColorMode
	}
	e.COLOR_MODE = strings.ToLower(e.COLOR_MODE)
	if e.BRIGHTNESS <= 0 {
		e.BRIGHTNESS = defaultBrightness
	}
	if e.MIN_BRIGHTNESS <= 0 {
		e.MIN_BRIGHTNESS = defaultMinBrightness
	}
	if e.MIN_CALL_INTERVAL_MS <= 0 {
		e.MIN_CALL_INTERVAL_MS = defaultMinCallGapMS
	}
}

// applyEnvOverrides lets HA_URL, HA_TOKEN and LED_ENTITY be supplied through
// environment variables, so the token doesn't have to live in the YAML file.
func (c *Config) applyEnvOverrides() {
	if v := strings.TrimSpace(os.Getenv("HA_URL")); v != "" {
		c.Env.HA_URL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv("HA_TOKEN")); v != "" {
		c.Env.HA_TOKEN = v
	}
	if v := strings.TrimSpace(os.Getenv("LED_ENTITY")); v != "" {
		c.Env.LED_ENTITY = v
	}
}

// entityIDPattern matches Home Assistant entity IDs such as light.my_led_strip.
var entityIDPattern = regexp.MustCompile(`^[a-z0-9_]+\.[a-z0-9_]+$`)

var validLogLevels = map[string]bool{
	"debug": true, "info": true, "warn": true, "error": true,
	"dpanic": true, "panic": true, "fatal": true,
}

// Validate returns a descriptive error for the first invalid setting.
func (c *Config) Validate() error {
	e := c.Env
	if err := validateHAURL(e.HA_URL); err != nil {
		return err
	}
	if e.LED_ENTITY == "" {
		return fmt.Errorf("LED_ENTITY is not set")
	}
	if !entityIDPattern.MatchString(e.LED_ENTITY) {
		return fmt.Errorf("LED_ENTITY %q is not a valid entity ID (expected something like light.my_led_strip)", e.LED_ENTITY)
	}
	if !validLogLevels[e.LOG_LEVEL] {
		return fmt.Errorf("LOG_LEVEL %q is invalid (use debug, info, warn, error, dpanic, panic or fatal)", e.LOG_LEVEL)
	}
	switch e.COLOR_MODE {
	case "dominant", "vibrant", "average":
	default:
		return fmt.Errorf("COLOR_MODE %q is invalid (use dominant, vibrant or average)", e.COLOR_MODE)
	}
	if e.COLOR_CHANGE_THRESHOLD > 442 {
		return fmt.Errorf("COLOR_CHANGE_THRESHOLD must be between 0 and 441")
	}
	if e.BRIGHTNESS > 255 || e.MIN_BRIGHTNESS > 255 {
		return fmt.Errorf("BRIGHTNESS and MIN_BRIGHTNESS must be at most 255")
	}
	if e.DISPLAY_INDEX < 0 {
		return fmt.Errorf("DISPLAY_INDEX must be 0 or greater")
	}
	if e.TRANSITION_MS < 0 {
		return fmt.Errorf("TRANSITION_MS must be 0 or greater")
	}
	if e.HA_TOKEN == "" {
		return fmt.Errorf("HA_TOKEN is not set (set it in the YAML file or as an environment variable)")
	}
	return nil
}

func validateHAURL(raw string) error {
	if raw == "" {
		return fmt.Errorf("HA_URL is not set")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("HA_URL %q must look like http://host:8123", raw)
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return fmt.Errorf("HA_URL must not contain credentials, a query string or a fragment")
	}
	return nil
}
