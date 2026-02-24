package libcontainer

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/zakarynichols/hackontainer/config"
)

const (
	stateFilename  = "state.json"
	configFilename = "config.json"
)

type Factory interface {
	Create(id, bundle string, options ...CreateOption) (Container, error)
	Load(id string) (Container, error)
	Type() string
}

type LinuxFactory struct {
	root string
}

type CreateOption func(*LinuxFactory) error

func New(root string, options ...CreateOption) (Factory, error) {
	// Should this be defined globally and never be an empty string?
	if root == "" {
		root = "/run/hackontainer"
	}

	l := &LinuxFactory{
		root: root,
	}

	for _, opt := range options {
		if err := opt(l); err != nil {
			return nil, err
		}
	}

	if err := os.MkdirAll(root, 0700); err != nil {
		return nil, err
	}

	return l, nil
}

func (l *LinuxFactory) Create(id, bundle string, options ...CreateOption) (Container, error) {
	if bundle == "" {
		bundle = "."
	}

	// Convert bundle to absolute path to ensure consistency
	absBundle, err := filepath.Abs(bundle)
	if err != nil {
		return nil, fmt.Errorf("failed to get absolute path for bundle: %w", err)
	}

	if id == "" {
		return nil, fmt.Errorf("container ID cannot be empty")
	}

	containerRoot := filepath.Join(l.root, id)
	if err := os.MkdirAll(containerRoot, 0711); err != nil {
		return nil, err
	}

	config, err := loadContainerConfig(absBundle)
	if err != nil {
		return nil, err
	}

	if err := config.NormalizeRoot(); err != nil {
		return nil, err
	}

	if err := validateID(id); err != nil {
		return nil, err
	}

	if err := config.Validate(); err != nil {
		return nil, err
	}

	container := &linuxContainer{
		id:     id,
		root:   containerRoot,
		config: config,
		bundle: absBundle,
	}

	if err := container.createState(); err != nil {
		return nil, err
	}

	return container, nil
}

func (l *LinuxFactory) Load(id string) (Container, error) {
	if id == "" {
		return nil, fmt.Errorf("container ID cannot be empty")
	}

	containerRoot := filepath.Join(l.root, id)
	container := &linuxContainer{
		id:   id,
		root: containerRoot,
	}

	// Load state first to get bundle path
	state, err := container.State()
	if err != nil {
		return nil, err
	}

	// Load configuration from bundle
	config, err := loadContainerConfig(state.Bundle)
	if err != nil {
		return nil, err
	}

	container.config = config
	container.bundle = state.Bundle

	return container, nil
}

func (l *LinuxFactory) Type() string {
	return "libcontainer"
}

func validateID(id string) error {
	if len(id) > 1024 {
		return fmt.Errorf("container ID too long")
	}

	if filepath.Base(id) != id {
		return fmt.Errorf("invalid container ID")
	}

	return nil
}

func loadContainerConfig(bundle string) (*config.Config, error) {
	configPath := filepath.Join(bundle, configFilename)
	return config.Load(configPath)
}
