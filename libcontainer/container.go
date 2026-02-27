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
	"golang.org/x/sys/unix"
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
	return c.loadState()
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

	// Update state atomically after successful process start
	state.Status = Running
	state.Pid = process.pid()
	if err := c.saveState(state); err != nil {
		// If state save fails, try to terminate the process
		_ = process.terminate()
		return fmt.Errorf("failed to save container state after start: %w", err)
	}

	// Fork a reaper process that will update state to stopped when container exits
	// This ensures the reaper outlives the parent process
	containerPid := state.Pid
	containerRoot := c.root

	// Fork a child process to act as reaper
	// Using raw syscall since unix.Fork doesn't exist in golang.org/x/sys/unix
	pid, _, errSys := unix.Syscall(unix.SYS_FORK, 0, 0, 0)
	if errSys != 0 {
		fmt.Fprintf(os.Stderr, "DEBUG: Fork failed for reaper: %v\n", errSys)
		// Continue anyway - container is running, just won't update state on exit
		return nil
	}

	if pid == 0 {
		// Child process - act as reaper
		fmt.Fprintf(os.Stderr, "DEBUG REAPER: Started, watching PID: %d, root: %s\n", containerPid, containerRoot)

		// Wait for the container process to exit
		for iteration := 0; ; iteration++ {
			// Check if the container process still exists
			err := syscall.Kill(containerPid, 0)
			if err != nil {
				// Process doesn't exist anymore, update state
				fmt.Fprintf(os.Stderr, "DEBUG REAPER: Iteration %d - Process %d exited (err: %v)\n", iteration, containerPid, err)
				break
			}
			if iteration%10 == 0 {
				fmt.Fprintf(os.Stderr, "DEBUG REAPER: Iteration %d - Process %d still running\n", iteration, containerPid)
			}
			time.Sleep(100 * time.Millisecond)
		}

		// Update state to stopped
		statePath := filepath.Join(containerRoot, stateFilename)
		fmt.Fprintf(os.Stderr, "DEBUG REAPER: Reading state from: %s\n", statePath)
		data, err := ioutil.ReadFile(statePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG REAPER: Failed to read state: %v\n", err)
			os.Exit(0)
		}

		var currentState State
		if err := json.Unmarshal(data, &currentState); err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG REAPER: Failed to unmarshal state: %v\n", err)
			os.Exit(0)
		}

		fmt.Fprintf(os.Stderr, "DEBUG REAPER: Current state: %s, updating to stopped\n", currentState.Status)
		currentState.Status = Stopped
		data, err = json.MarshalIndent(currentState, "", "  ")
		if err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG REAPER: Failed to marshal state: %v\n", err)
			os.Exit(0)
		}

		if err := ioutil.WriteFile(statePath, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG REAPER: Failed to write state: %v\n", err)
			os.Exit(0)
		}

		fmt.Fprintf(os.Stderr, "DEBUG REAPER: State updated to stopped, exiting\n")
		os.Exit(0)
	}

	// Parent process - return immediately
	fmt.Fprintf(os.Stderr, "DEBUG: Reaper forked with PID: %d\n", pid)

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

	fmt.Fprintf(os.Stderr, "DEBUG SIGNAL: container status: %s, pid: %d, signal: %v\n", state.Status, state.Pid, sig)

	if state.Status != Running {
		return fmt.Errorf("cannot signal a container that is not running")
	}

	if state.Pid == 0 {
		return fmt.Errorf("no process to signal")
	}

	err = syscall.Kill(state.Pid, sig)
	fmt.Fprintf(os.Stderr, "DEBUG SIGNAL: syscall.Kill(%d, %v) result: %v\n", state.Pid, sig, err)
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
