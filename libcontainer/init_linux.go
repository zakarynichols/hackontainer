package libcontainer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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
		// Look in container rootfs first
		containerExecPath := filepath.Join(container.config.Rootfs, execPath)
		if _, err := os.Stat(containerExecPath); err == nil {
			execPath = containerExecPath
		} else {
			// Look for executable in PATH
			path, err := exec.LookPath(execPath)
			if err != nil {
				return nil, fmt.Errorf("executable %q not found in container rootfs or PATH: %w", execPath, err)
			}
			execPath = path
		}
	}

	cmd := &exec.Cmd{
		Path:   execPath,
		Args:   processArgs,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Dir:    container.config.Rootfs,
		Env:    container.config.Process.Env,
	}

	return &initProcess{
		cmd:       cmd,
		container: container,
	}, nil
}
