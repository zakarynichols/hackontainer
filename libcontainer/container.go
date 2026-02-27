package libcontainer

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/zakarynichols/hackontainer/config"
)

type Container interface {
	ID() string
	Status() (Status, error)
	State() (*State, error)
	Start() error
	Run() error
	InitProcess() error
	Signal(sig syscall.Signal) error
	Delete() error
}

type Status string

const (
	Created Status = "created"
	Running Status = "running"
	Stopped Status = "stopped"
)

type State struct {
	ID          string            `json:"id"`
	Pid         int               `json:"pid"`
	Bundle      string            `json:"bundle"`
	Status      Status            `json:"status"`
	Created     time.Time         `json:"created"`
	Annotations map[string]string `json:"annotations,omitempty"`
	OCIVersion  string            `json:"ociVersion"`
}

type linuxContainer struct {
	id     string
	root   string
	config *config.Config
	bundle string
}

func (c *linuxContainer) ID() string {
	return c.id
}

func (c *linuxContainer) Status() (Status, error) {
	state, err := c.State()
	if err != nil {
		return "", err
	}

	return state.Status, nil
}

func (c *linuxContainer) State() (*State, error) {
	state, err := c.loadState()
	if err != nil {
		return nil, err
	}

	// Check if process still exists for Running containers
	// This is how runc, crun, and youki handle state - check /proc
	if state.Status == Running && state.Pid > 0 {
		if err := syscall.Kill(state.Pid, 0); err != nil {
			// Process doesn't exist or is not accessible
			state.Status = Stopped
		}
	}

	return state, nil
}

func (c *linuxContainer) Start() error {
	fmt.Fprintf(os.Stderr, "DEBUG: Container.Start() called for container %s\n", c.id)

	state, err := c.State()
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Container %s current status: %s\n", c.id, state.Status)

	// OCI spec: start operation MUST only work on containers in 'created' state
	if state.Status != Created {
		switch state.Status {
		case Running:
			return fmt.Errorf("cannot start an already running container")
		case Stopped:
			return fmt.Errorf("cannot start a container that has stopped")
		default:
			return fmt.Errorf("cannot start a container in the %s state", state.Status)
		}
	}

	// Ensure process configuration is available (OCI spec requirement)
	if c.config.Process == nil || len(c.config.Process.Args) == 0 {
		return fmt.Errorf("container process not configured")
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Creating init process for container %s with args: %v\n", c.id, c.config.Process.Args)

	process, err := newInitProcess(c)
	if err != nil {
		return fmt.Errorf("failed to create init process: %w", err)
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Starting init process for container %s\n", c.id)
	if err := process.start(); err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	// Register container PID with reaper
	RegisterContainer(process.pid(), c.root)

	// Update state atomically after successful process start
	state.Status = Running
	state.Pid = process.pid()
	if err := c.saveState(state); err != nil {
		// If state save fails, try to terminate the process
		_ = process.terminate()
		return fmt.Errorf("failed to save container state after start: %w", err)
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Container started successfully, reaper runs in init process\n")

	return nil
}

// InitProcess creates and starts the init process for container initialization
func (c *linuxContainer) InitProcess() error {
	fmt.Fprintf(os.Stderr, "DEBUG: Container.InitProcess() called for container %s\n", c.id)

	fmt.Fprintf(os.Stderr, "DEBUG: About to call newInitProcess()\n")
	process, err := newInitProcess(c)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG: newInitProcess() failed: %v\n", err)
		return fmt.Errorf("failed to create init process: %w", err)
	}

	fmt.Fprintf(os.Stderr, "DEBUG: About to call process.start()\n")
	if err := process.start(); err != nil {
		fmt.Fprintf(os.Stderr, "DEBUG: process.start() failed: %v\n", err)
		return fmt.Errorf("failed to start init process: %w", err)
	}

	// This should not be reached in normal operation as the init process will exec
	fmt.Fprintf(os.Stderr, "DEBUG: process.start() returned unexpectedly - exec should have happened\n")
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
	state.Status = Stopped
	return c.saveState(state)
}

func (c *linuxContainer) Delete() error {
	statePath := filepath.Join(c.root, stateFilename)
	if err := os.Remove(statePath); err != nil && !os.IsNotExist(err) {
		return err
	}

	return os.RemoveAll(c.root)
}

func (c *linuxContainer) Signal(sig syscall.Signal) error {
	state, err := c.State()
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}

	if state.Status != Running {
		return fmt.Errorf("cannot signal a container that is not running")
	}

	if state.Pid == 0 {
		return fmt.Errorf("no process to signal")
	}

	err = syscall.Kill(state.Pid, sig)
	if err != nil {
		return fmt.Errorf("failed to send signal: %w", err)
	}

	return nil
}

func (c *linuxContainer) createState() error {
	state := &State{
		ID:          c.id,
		Pid:         0,
		Bundle:      c.bundle,
		Status:      Created,
		Created:     time.Now(),
		Annotations: make(map[string]string),
		OCIVersion:  "1.3.0",
	}

	if c.config.Spec != nil && c.config.Spec.Annotations != nil {
		state.Annotations = c.config.Spec.Annotations
	}

	return c.saveState(state)
}

func (c *linuxContainer) saveState(state *State) error {
	statePath := filepath.Join(c.root, stateFilename)
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(statePath, data, 0644)
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

	return &state, nil
}
