package libcontainer

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/zakarynichols/hackontainer/config"
)

type Container interface {
	ID() string
	Status() (Status, error)
	State() (*State, error)
	Start() error
	Run() error
	Delete() error
	RefreshState() error
	Inspect() (*ContainerInfo, error)
}

type Status string

const (
	Created Status = "created"
	Running Status = "running"
	Stopped Status = "stopped"
)

type State struct {
	ID                   string            `json:"id"`
	InitProcessPid       int               `json:"init_process_pid"`
	InitProcessStartTime uint64            `json:"init_process_start_time"`
	Created              time.Time         `json:"created"`
	Started              time.Time         `json:"started,omitempty"`
	Finished             time.Time         `json:"finished,omitempty"`
	ExitStatus           int               `json:"exit_status,omitempty"`
	OOMKilled            bool              `json:"oom_killed,omitempty"`
	Error                string            `json:"error,omitempty"`
	Bundle               string            `json:"bundle"`
	Annotations          map[string]string `json:"annotations,omitempty"`
}

type linuxContainer struct {
	id     string
	root   string
	config *config.Config
	bundle string
	state  *State
}

type ContainerInfo struct {
	ID          string            `json:"id"`
	Pid         int               `json:"pid"`
	Status      string            `json:"status"`
	Bundle      string            `json:"bundle"`
	Rootfs      string            `json:"rootfs"`
	Created     time.Time         `json:"created"`
	Annotations map[string]string `json:"annotations"`
	Process     *specs.Process    `json:"process,omitempty"`
	OCIVersion  string            `json:"ociVersion"`
}

func (c *linuxContainer) ID() string {
	return c.id
}

func (c *linuxContainer) Status() (Status, error) {
	state, err := c.State()
	if err != nil {
		return "", err
	}

	if state.Finished.IsZero() {
		if !state.Started.IsZero() {
			return Running, nil
		}
		return Created, nil
	}

	return Stopped, nil
}

func (c *linuxContainer) State() (*State, error) {
	if c.state != nil {
		return c.state, nil
	}

	return c.loadState()
}

func (c *linuxContainer) Start() error {
	state, err := c.State()
	if err != nil {
		return err
	}

	if !state.Started.IsZero() {
		return fmt.Errorf("container already started")
	}

	process, err := newInitProcess(c)
	if err != nil {
		return fmt.Errorf("failed to create init process: %w", err)
	}

	if err := process.start(); err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	return nil
}

func (c *linuxContainer) Run() error {
	process, err := newInitProcess(c)
	if err != nil {
		return fmt.Errorf("failed to create init process: %w", err)
	}

	if err := process.start(); err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	_, err = process.wait()
	if err != nil {
		return err
	}

	state, err := c.State()
	if err != nil {
		return err
	}
	state.Finished = time.Now()
	return c.saveState(state)
}

func (c *linuxContainer) Delete() error {
	statePath := filepath.Join(c.root, stateFilename)
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.RemoveAll(c.root)
}

func (c *linuxContainer) RefreshState() error {
	_, err := c.loadState()
	return err
}

func (c *linuxContainer) Inspect() (*ContainerInfo, error) {
	state, err := c.State()
	if err != nil {
		return nil, err
	}

	status, err := c.Status()
	if err != nil {
		return nil, err
	}

	return &ContainerInfo{
		ID:          state.ID,
		Pid:         state.InitProcessPid,
		Status:      string(status),
		Bundle:      state.Bundle,
		Rootfs:      c.config.Rootfs,
		Created:     state.Created,
		Annotations: state.Annotations,
		Process:     c.config.Process,
		OCIVersion:  specs.Version,
	}, nil
}

func (c *linuxContainer) createState() error {
	state := &State{
		ID:          c.id,
		Created:     time.Now(),
		Bundle:      c.bundle,
		Annotations: make(map[string]string),
	}

	if c.config.Spec != nil && c.config.Spec.Annotations != nil {
		state.Annotations = c.config.Spec.Annotations
	}

	c.state = state
	return c.saveState(state)
}

func (c *linuxContainer) saveState(state *State) error {
	statePath := filepath.Join(c.root, stateFilename)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return ioutil.WriteFile(statePath, data, 0644)
}

func (c *linuxContainer) loadState() (*State, error) {
	statePath := filepath.Join(c.root, stateFilename)
	data, err := ioutil.ReadFile(statePath)
	if err != nil {
		return nil, err
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	c.state = &state
	return &state, nil
}

func (c *linuxContainer) refreshState() error {
	state, err := c.loadState()
	if err != nil {
		return err
	}
	c.state = state
	return nil
}
