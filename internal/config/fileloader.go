package config

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

func ConfigurationFromFile(file string) (*Configuration, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	defer f.Close()

	d := yaml.NewDecoder(f)

	d.KnownFields(true)

	var cfg Configuration

	err = d.Decode(&cfg)
	if err != nil {
		return nil, fmt.Errorf("read config from file %s: %w", file, err)
	}

	err = d.Decode(&map[string]any{})
	if err != io.EOF {
		return nil, fmt.Errorf("config file %s contains multiple objects", file)
	}

	return &cfg, nil
}
