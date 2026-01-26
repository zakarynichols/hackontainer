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

	if id == "" {
		return nil, fmt.Errorf("container ID cannot be empty")
	}

	containerRoot := filepath.Join(l.root, id)
	if err := os.MkdirAll(containerRoot, 0711); err != nil {
		return nil, err
	}

	config, err := loadContainerConfig(bundle)
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
		bundle: bundle,
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

	if _, err := container.State(); err != nil {
		return nil, err
	}

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
