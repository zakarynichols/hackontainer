package libcontainer

import (
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/zakarynichols/hackontainer/libcontainer/utils"
)

type parentProcess interface {
	pid() int
	start() error
	terminate() error
	wait() (*os.ProcessState, error)
	startTime() (uint64, error)
	signal(os.Signal) error
	externalDescriptors() []string
	setExternalDescriptors(fds []string)
	forwardChildLogs() chan error
}

type initProcess struct {
	cmd         *exec.Cmd
	container   *linuxContainer
	pipe        *os.File
	descriptors []string
}

func (p *initProcess) pid() int {
	return p.cmd.Process.Pid
}

func (p *initProcess) start() error {
	err := p.cmd.Start()
	if err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	p.container.state.InitProcessPid = p.cmd.Process.Pid
	startTime, err := startTimeToUint64(p.cmd.Process)
	if err != nil {
		utils.Errorf("failed to get start time: %v", err)
	}
	p.container.state.InitProcessStartTime = startTime
	p.container.state.Started = time.Now()

	if err := p.container.saveState(p.container.state); err != nil {
		utils.Errorf("failed to save container state: %v", err)
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

func (p *initProcess) externalDescriptors() []string {
	return p.descriptors
}

func (p *initProcess) setExternalDescriptors(fds []string) {
	p.descriptors = fds
}

func (p *initProcess) forwardChildLogs() chan error {
	ch := make(chan error, 1)
	go func() {
		close(ch)
	}()
	return ch
}

func startTimeToUint64(process *os.Process) (uint64, error) {
	return uint64(time.Now().UnixNano()), nil
}
