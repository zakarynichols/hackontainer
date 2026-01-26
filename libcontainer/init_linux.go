package libcontainer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"golang.org/x/sys/unix"
)

// mount is a wrapper around unix.Mount for better error handling
func mount(source, target, fstype string, flags uintptr, data string) error {
	if err := unix.Mount(source, target, fstype, flags, data); err != nil {
		return &os.PathError{Op: "mount", Path: target, Err: err}
	}
	return nil
}

// unmount is a wrapper around unix.Unmount for better error handling
func unmount(target string, flags int) error {
	if err := unix.Unmount(target, flags); err != nil {
		return &os.PathError{Op: "unmount", Path: target, Err: err}
	}
	return nil
}

// prepareRoot sets up the root filesystem for container
// Following runc's prepareRoot implementation
func prepareRoot(rootfs string) error {
	// Default to slave mount, but respect config if set
	flag := unix.MS_SLAVE | unix.MS_REC

	// Apply root propagation flags if configured (skip MS_PRIVATE which is set in default)
	// This will be extended in future to use container config
	if err := mount("", "/", "", uintptr(flag), ""); err != nil {
		return fmt.Errorf("failed to make parent mount private: %w", err)
	}

	// Ensure rootfs is a mount point by bind mounting it to itself
	if err := mount(rootfs, rootfs, "bind", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to bind mount rootfs: %w", err)
	}

	return nil
}

// pivotRoot performs the pivot_root syscall to change root filesystem
// Following runc's pivotRoot implementation exactly
func pivotRoot(rootfs string) error {
	// Open old root ("/")
	oldroot, err := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open old root: %w", err)
	}
	defer unix.Close(oldroot)

	// Open new root (rootfs)
	newroot, err := unix.Open(rootfs, unix.O_DIRECTORY|unix.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open new root: %w", err)
	}
	defer unix.Close(newroot)

	// Change to the new root so that pivot_root acts on it
	if err := unix.Fchdir(newroot); err != nil {
		return fmt.Errorf("failed to fchdir to new root: %w", err)
	}

	// Perform pivot_root(".", ".")
	if err := unix.PivotRoot(".", "."); err != nil {
		return fmt.Errorf("failed to pivot_root: %w", err)
	}

	// Currently our "." is oldroot. Change to oldroot for cleanup
	if err := unix.Fchdir(oldroot); err != nil {
		return fmt.Errorf("failed to fchdir to old root: %w", err)
	}

	// Make oldroot rslave to prevent mount propagation to host
	if err := mount("", ".", "", unix.MS_SLAVE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to make old root slave: %w", err)
	}

	// Unmount old root with MNT_DETACH
	if err := unmount(".", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("failed to unmount old root: %w", err)
	}

	// Switch back to our shiny new root
	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("failed to chdir to new root: %w", err)
	}

	return nil
}

// setupRootfs sets up the container rootfs and performs pivot_root or chroot
func setupRootfs(container *linuxContainer) error {
	if err := prepareRoot(container.config.Rootfs); err != nil {
		return fmt.Errorf("failed to prepare root: %w", err)
	}

	// Change directory to rootfs before pivot_root/chroot
	if err := unix.Chdir(container.config.Rootfs); err != nil {
		return fmt.Errorf("failed to chdir to rootfs: %w", err)
	}

	// Check if we should use chroot instead of pivot_root
	// For now, always use pivot_root (can be extended to check container.config.NoPivotRoot)
	usePivotRoot := true // container.config.NoPivotRoot will be checked in future

	if usePivotRoot {
		// Perform pivot_root to jail the process
		if err := pivotRoot(container.config.Rootfs); err != nil {
			return fmt.Errorf("failed to pivot_root: %w", err)
		}
	} else {
		// Fallback to chroot (simpler but less secure)
		if err := unix.Chroot("."); err != nil {
			return fmt.Errorf("failed to chroot: %w", err)
		}
		if err := unix.Chdir("/"); err != nil {
			return fmt.Errorf("failed to chdir after chroot: %w", err)
		}
	}

	// Mount /proc inside the container
	fmt.Fprintf(os.Stderr, "DEBUG: Mounting /proc in container\n")
	if err := unix.Mount("proc", "/proc", "proc", unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_NODEV, ""); err != nil {
		return fmt.Errorf("failed to mount /proc: %w", err)
	}

	return nil
}

func newInitProcess(container *linuxContainer) (*initProcess, error) {
	args := container.config.Process.Args
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}

	processArgs := make([]string, len(args))
	copy(processArgs, args)

	// Find executable in PATH or use absolute path
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

	// Self-execution pattern like runc
	if len(os.Args) > 1 && os.Args[1] == "init" {
		// We're in the init process - set up rootfs and then exec container process
		fmt.Fprintf(os.Stderr, "DEBUG: Setting up container rootfs: %s\n", container.config.Rootfs)

		// Set up rootfs with pivot_root
		if err := setupRootfs(container); err != nil {
			return nil, fmt.Errorf("failed to setup rootfs: %w", err)
		}

		fmt.Fprintf(os.Stderr, "DEBUG: Rootfs setup complete, executing container process: %v\n", processArgs)

		// Immediately exec the container process - this replaces the current process
		fmt.Fprintf(os.Stderr, "DEBUG: About to syscall.Exec: %s with args: %v\n", processArgs[0], processArgs)

		// Check if the executable exists
		if _, err := os.Stat(processArgs[0]); os.IsNotExist(err) {
			fmt.Fprintf(os.Stderr, "DEBUG: Executable does not exist: %s\n", processArgs[0])
			return nil, fmt.Errorf("executable %s not found: %w", processArgs[0], err)
		}

		fmt.Fprintf(os.Stderr, "DEBUG: About to syscall.Exec: %s with args: %v\n", processArgs[0], processArgs)
		if cwd, err := os.Getwd(); err == nil {
			fmt.Fprintf(os.Stderr, "DEBUG: Current working directory: %s\n", cwd)
		}
		fmt.Fprintf(os.Stderr, "DEBUG: Environment: %v\n", container.config.Process.Env)

		// Use syscall.Exec to replace current process
		err := syscall.Exec(processArgs[0], processArgs, container.config.Process.Env)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG: syscall.Exec failed: %v\n", err)
			if errno, ok := err.(syscall.Errno); ok {
				fmt.Fprintf(os.Stderr, "DEBUG: Error details: errno=%d\n", errno)
			}
			return nil, fmt.Errorf("failed to exec container process %s: %w", processArgs[0], err)
		}

		// This should never be reached if exec succeeds
		return nil, fmt.Errorf("exec returned unexpectedly")
	} else {
		// Parent process - re-execute hackontainer as init
		execPath, err := os.Executable()
		if err != nil {
			execPath = os.Args[0] // fallback
		}

		// Convert bundle path to absolute path to ensure config can be found
		absBundle, err := filepath.Abs(container.bundle)
		if err != nil {
			return nil, fmt.Errorf("failed to get absolute path for bundle: %w", err)
		}

		initArgs := []string{execPath, "init", container.id, absBundle}
		cmd := &exec.Cmd{
			Path:   execPath,
			Args:   initArgs,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
			Dir:    container.config.Rootfs,
			Env:    container.config.Process.Env,
			SysProcAttr: &syscall.SysProcAttr{
				Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID,
			},
		}

		// Debug: Log what we're about to execute
		fmt.Fprintf(os.Stderr, "DEBUG: Parent re-executing as init: Path=%s, Args=%v\n", os.Args[0], initArgs)

		return &initProcess{
			cmd:       cmd,
			container: container,
		}, nil
	}
}
