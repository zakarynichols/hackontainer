package libcontainer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
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
	pipe      *os.File
}

func newInitProcess(container *linuxContainer) (*initProcess, error) {
	args := container.config.Process.Args
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}

	processArgs := make([]string, len(args))
	copy(processArgs, args)

	// Find the executable in PATH or use absolute path
	execPath := processArgs[0]
	if !filepath.IsAbs(execPath) {
		path, err := exec.LookPath(execPath)
		if err != nil {
			return nil, fmt.Errorf("executable %q not found: %w", execPath, err)
		}
		execPath = path
	}

	// Create a pipe for communication between parent and child
	parentPipe, childPipe, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create pipe: %w", err)
	}

	// Prepare namespace configuration
	namespaces := []LinuxNamespace{
		{Type: CLONE_NEWNS},  // Mount namespace
		{Type: CLONE_NEWUTS}, // UTS namespace
		{Type: CLONE_NEWIPC}, // IPC namespace
		{Type: CLONE_NEWPID}, // PID namespace
		{Type: CLONE_NEWNET}, // Network namespace
	}

	// Use container-init binary for the child process
	initPath := "/home/devuser/hackontainer/container-init"

	cmd := exec.Command(initPath, "3", filepath.Join(container.bundle, "config.json"))
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags:   GetNamespaceFlagMappings(namespaces),
		Unshareflags: syscall.CLONE_NEWNS,
	}

	process := &initProcess{
		cmd:       cmd,
		container: container,
		pipe:      parentPipe,
	}

	// Set up extra files for the child process
	cmd.ExtraFiles = []*os.File{childPipe}

	return process, nil
}

func (p *initProcess) pid() int {
	return p.cmd.Process.Pid
}

func (p *initProcess) start() error {
	// Start the process
	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	// Wait for child to be ready
	var ready [1]byte
	_, err := p.pipe.Read(ready[:])
	if err != nil {
		return fmt.Errorf("failed to read ready signal from child: %w", err)
	}

	// Set up cgroups for the child process
	cgroupManager := NewCgroupManager("/sys/fs/cgroup/hackontainer")
	if err := cgroupManager.Setup(p.container.id, p.container.config); err != nil {
		return fmt.Errorf("failed to setup cgroups: %w", err)
	}

	// Add the process to cgroups
	if err := cgroupManager.AddProcess(p.container.id, p.cmd.Process.Pid); err != nil {
		return fmt.Errorf("failed to add process to cgroups: %w", err)
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
	return startTimeToUint64(p.cmd.Process)
}

func (p *initProcess) signal(sig os.Signal) error {
	return p.cmd.Process.Signal(sig)
}

func startTimeToUint64(process *os.Process) (uint64, error) {
	// This would normally read /proc/[pid]/stat to get the start time
	// For simplicity, we'll return current time
	return uint64(time.Now().UnixNano()), nil
}
