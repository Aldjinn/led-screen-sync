package main

import (
	"os"

	"gopkg.in/yaml.v3"
)

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
	return &config, nil
}
