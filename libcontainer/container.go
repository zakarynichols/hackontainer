package libcontainer

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
	ID                   string            `json:"id"`
	Pid                  int               `json:"pid"`
	Bundle               string            `json:"bundle"`
	Status               Status            `json:"status"`
	Created              time.Time         `json:"created"`
	Annotations          map[string]string `json:"annotations,omitempty"`
	OCIVersion           string            `json:"ociVersion"`
	InitProcessStartTime uint64            `json:"initProcessStartTime,omitempty"`
}

type procState struct {
	Pid   int
	State byte
}

func getProcState(pid int) (*procState, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := ioutil.ReadFile(statPath)
	if err != nil {
		return nil, err
	}

	// Parse /proc/[pid]/stat
	// Format: pid (comm) state ...
	// The comm field is in parentheses and may contain spaces, so we find the last )
	idx := strings.LastIndex(string(data), ")")
	if idx < 0 {
		return nil, fmt.Errorf("invalid /proc/stat format")
	}

	// After ), we have: state pid ...
	parts := strings.Split(string(data[idx+2:]), " ")
	if len(parts) < 4 {
		return nil, fmt.Errorf("invalid /proc/stat format")
	}

	state := parts[0][0] // First char is state (R, S, D, Z, T, X, etc)

	return &procState{
		Pid:   pid,
		State: state,
	}, nil
}

func getProcessStartTime(pid int) (uint64, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := ioutil.ReadFile(statPath)
	if err != nil {
		return 0, err
	}

	idx := strings.LastIndex(string(data), ")")
	if idx < 0 {
		return 0, fmt.Errorf("invalid /proc/stat format")
	}

	parts := strings.Split(string(data[idx+2:]), " ")
	if len(parts) < 22 {
		return 0, fmt.Errorf("invalid /proc/stat format")
	}

	startTime, err := strconv.ParseUint(parts[21], 10, 64)
	if err != nil {
		return 0, err
	}

	return startTime, nil
}

type linuxContainer struct {
	id          string
	root        string
	config      *config.Config
	bundle      string
	initProcess parentProcess
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

	// Check if we have an in-memory initProcess (like runc does)
	// This is more reliable than just reading from disk
	if c.initProcess != nil && state.Status == Running {
		pid := c.initProcess.pid()
		startTime, err := c.initProcess.startTime()
		if err != nil {
			state.Status = Stopped
		} else if startTime != state.InitProcessStartTime && state.InitProcessStartTime != 0 {
			state.Status = Stopped
		} else {
			procStat, err := getProcState(pid)
			if err != nil {
				state.Status = Stopped
			} else if procStat.State == 'Z' || procStat.State == 'X' {
				state.Status = Stopped
			}
		}
	} else if state.Status == Running && state.Pid > 0 {
		// Fallback: check if process exists using /proc
		// First check if /proc/[pid] exists - this is more reliable than Kill in some namespace scenarios
		procPath := fmt.Sprintf("/proc/%d", state.Pid)
		if _, err := os.Stat(procPath); err != nil {
			// Process doesn't exist
			state.Status = Stopped
		} else {
			// Process exists, check state
			procStat, err := getProcState(state.Pid)
			if err != nil {
				state.Status = Stopped
			} else if procStat.State == 'Z' || procStat.State == 'X' {
				state.Status = Stopped
			}
		}
	}

	return state, nil
}

func (c *linuxContainer) Start() error {
	state, err := c.State()
	if err != nil {
		return err
	}

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

	process, err := newInitProcess(c)
	if err != nil {
		return fmt.Errorf("failed to create init process: %w", err)
	}

	if err := process.start(); err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	// Store initProcess in memory for reliable state checking (like runc)
	c.initProcess = process

	// Get process start time
	startTime, err := process.startTime()
	if err != nil {
		startTime = 0
	}

	// Update state atomically after successful process start
	state.Status = Running
	state.Pid = process.pid()
	state.InitProcessStartTime = startTime
	if err := c.saveState(state); err != nil {
		_ = process.terminate()
		return fmt.Errorf("failed to save container state after start: %w", err)
	}

	return nil
}

// InitProcess creates and starts the init process for container initialization
func (c *linuxContainer) InitProcess() error {
	process, err := newInitProcess(c)
	if err != nil {
		return fmt.Errorf("failed to create init process: %w", err)
	}

	if err := process.start(); err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	// This should not be reached in normal operation as the init process will exec
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
	// OCI spec: delete MUST generate an error if container is not stopped
	state, err := c.State()
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to get container state: %w", err)
	}
	if state != nil && state.Status == Running {
		return fmt.Errorf("cannot delete a container that is running")
	}

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

	// OCI spec: kill MUST generate an error if container is neither created nor running
	if state.Status != Running && state.Status != Created {
		return fmt.Errorf("cannot signal a container that is not running or created")
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
