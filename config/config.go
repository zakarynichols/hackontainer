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

	bundleDir := filepath.Dir(path)
	rootPath := "."
	if spec.Root != nil {
		rootPath = spec.Root.Path
		if rootPath == "" {
			rootPath = "."
		}
	}

	return &Config{
		Spec:   &spec,
		Rootfs: filepath.Join(bundleDir, rootPath),
	}, nil
}

/*
On POSIX platforms, path is either an absolute path or a relative
path to the bundle. For example, with a bundle at /to/bundle and a
root filesystem at /to/bundle/rootfs, the path value can be either
`/to/bundle/rootfs` or `rootfs`. The value SHOULD be the conventional rootfs.
*/
func (c *Config) NormalizeRoot() error {
	if c.Spec.Root == nil {
		return fmt.Errorf("root specification required")
	}

	if !filepath.IsAbs(c.Spec.Root.Path) {
		bundleDir := filepath.Dir(c.Rootfs)
		c.Spec.Root.Path = filepath.Join(bundleDir, c.Spec.Root.Path)
	}

	c.Rootfs = c.Spec.Root.Path

	return nil
}
func (c *Config) Validate() error {
	return Validate(c.Spec)
}
