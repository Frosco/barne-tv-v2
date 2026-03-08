package main

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Source struct {
	Type string `yaml:"type"`
	ID   string `yaml:"id"`
	Name string `yaml:"name"`
}

type Config struct {
	YouTubeAPIKey   string   `yaml:"youtube_api_key"`
	RefreshInterval string   `yaml:"refresh_interval"`
	Sources         []Source `yaml:"sources"`
}

func LoadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	if cfg.YouTubeAPIKey == "" {
		return nil, fmt.Errorf("youtube_api_key is required")
	}

	if cfg.RefreshInterval == "" {
		cfg.RefreshInterval = "6h"
	}

	return &cfg, nil
}
