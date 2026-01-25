package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	specs "github.com/opencontainers/runtime-spec/specs-go"
)

type Config struct {
	*specs.Spec

	Rootfs string
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var spec specs.Spec
	if err := json.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config: %w", err)
	}

	return &Config{
		Spec:   &spec,
		Rootfs: filepath.Join(filepath.Dir(path), "rootfs"),
	}, nil
}

func (c *Config) Validate() error {
	return Validate(c.Spec)
}

func (c *Config) Save(path string) error {
	data, err := json.MarshalIndent(c.Spec, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	return os.WriteFile(path, data, 0644)
}
