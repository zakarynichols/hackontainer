package libcontainer

import (
	"fmt"
	"golang.org/x/sys/unix"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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

		// Now execute the actual container process
		cmd := &exec.Cmd{
			Path:   processArgs[0],
			Args:   processArgs,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
			Dir:    "/", // We're now in the new root
			Env:    container.config.Process.Env,
		}

		return &initProcess{
			cmd:       cmd,
			container: container,
		}, nil
	} else {
		// Parent process - re-execute hackontainer as init
		execPath, err := os.Executable()
		if err != nil {
			execPath = os.Args[0] // fallback
		}
		initArgs := []string{execPath, "init", container.id, container.bundle}
		cmd := &exec.Cmd{
			Path:   execPath,
			Args:   initArgs,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
			Dir:    container.config.Rootfs,
			Env:    container.config.Process.Env,
			SysProcAttr: &syscall.SysProcAttr{
				Cloneflags: syscall.CLONE_NEWNS, // Mount namespace isolation
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
