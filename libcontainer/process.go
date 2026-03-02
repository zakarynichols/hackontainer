package libcontainer

import (
	"fmt"
	"os"
	"os/exec"
)

type parentProcess interface {
	pid() int
	start() error
	terminate() error
	wait() (*os.ProcessState, error)
	startTime() (uint64, error)
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
