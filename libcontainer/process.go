package libcontainer

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

type parentProcess interface {
	pid() int
	start() error
	terminate() error
	wait() (*os.ProcessState, error)
	startTime() (uint64, error)
	signal(os.Signal) error
}

type initProcess struct {
	cmd       *exec.Cmd
	container *linuxContainer
}

func (p *initProcess) pid() int {
	return p.cmd.Process.Pid
}

func (p *initProcess) start() error {
	err := p.cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	return nil
}

func (p *initProcess) terminate() error {
	if p.cmd.Process == nil {
		return nil
	}

	return p.cmd.Process.Kill()
}

func (p *initProcess) wait() (*os.ProcessState, error) {
	return p.cmd.Process.Wait()
}

func (p *initProcess) startTime() (uint64, error) {
	if p.cmd.Process == nil {
		return 0, fmt.Errorf("process not started")
	}
	return getProcessStartTime(p.cmd.Process.Pid)
}

func (p *initProcess) signal(sig os.Signal) error {
	return p.cmd.Process.Signal(sig)
}

func getProcessStartTime(pid int) (uint64, error) {
	statPath := fmt.Sprintf("/proc/%d/stat", pid)
	data, err := os.ReadFile(statPath)
	if err != nil {
		return 0, err
	}

	// Parse /proc/[pid]/stat
	// Format: pid (comm) state ... start_time ...
	// Find the last ) to skip the comm field
	idx := strings.LastIndex(string(data), ")")
	if idx < 0 {
		return 0, fmt.Errorf("invalid /proc/stat format")
	}

	// After ), we have: state pid ...
	parts := strings.Split(string(data[idx+2:]), " ")
	if len(parts) < 22 {
		return 0, fmt.Errorf("invalid /proc/stat format")
	}

	// Start time is field 22 (index 21)
	startTime, err := strconv.ParseUint(parts[21], 10, 64)
	if err != nil {
		return 0, err
	}

	// Convert from clock ticks to nanoseconds (we use this for comparison)
	// Actually, for comparison purposes, we just need a consistent value
	return startTime, nil
}
