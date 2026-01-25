package libcontainer

import (
	"os"
	"os/exec"
)

func newInitProcess(container *linuxContainer) (*initProcess, error) {
	args := container.config.Process.Args
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}

	processArgs := make([]string, len(args))
	copy(processArgs, args)
	cmd := &exec.Cmd{
		Path:   processArgs[0],
		Args:   processArgs,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Dir:    container.config.Root.Path,
		Env:    container.config.Process.Env,
	}

	return &initProcess{
		cmd:       cmd,
		container: container,
	}, nil
}
