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
		// Look for executable in PATH
		path, err := exec.LookPath(execPath)
		if err != nil {
			return nil, fmt.Errorf("executable %q not found: %w", execPath, err)
		}
		execPath = path
	}

	cmd := &exec.Cmd{
		Path:   execPath,
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
