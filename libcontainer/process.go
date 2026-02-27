package libcontainer

import (
	"fmt"
	"os"
	"os/exec"
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
}

func (p *initProcess) pid() int {
	return p.cmd.Process.Pid
}

func (p *initProcess) start() error {
	fmt.Fprintf(os.Stderr, "DEBUG: process.start() executing: %s\n", p.cmd.Path)

	err := p.cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	fmt.Fprintf(os.Stderr, "DEBUG: process started PID=%d\n", p.cmd.Process.Pid)
	// Note: subreaper will handle reaping, no goroutine needed

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
	return uint64(time.Now().UnixNano()), nil
}
