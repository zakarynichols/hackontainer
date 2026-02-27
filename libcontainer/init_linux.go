package libcontainer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	// First make the root mount private (REQUIRED for pivot_root to work)
	if err := mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to make root mount private: %w", err)
	}

	// Then change to slave to prevent mount propagation to host
	flag := unix.MS_SLAVE | unix.MS_REC
	if err := mount("", "/", "", uintptr(flag), ""); err != nil {
		return fmt.Errorf("failed to make root mount slave: %w", err)
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
	fmt.Fprintf(os.Stderr, "DEBUG: Old root unmounted successfully\n")

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
		fmt.Fprintf(os.Stderr, "DEBUG: pivot_root completed successfully\n")
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
	// First ensure /proc directory exists
	if err := os.MkdirAll("/proc", 0755); err != nil {
		return fmt.Errorf("failed to create /proc directory: %w", err)
	}

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

	// Self-execution pattern like runc - check os.Args for "init" command (accounting for flags before command)
	// os.Args can be: [hackontainer --root /path init id bundle] or [hackontainer init id bundle]
	isInit := false
	for _, arg := range os.Args {
		if arg == "init" {
			isInit = true
			break
		}
	}

	fmt.Fprintf(os.Stderr, "DEBUG CHILD: os.Args check: len=%d, os.Args=%v, isInit=%v\n", len(os.Args), os.Args, isInit)

	// Self-execution pattern like runc - check os.Args (runtime args after exec), NOT processArgs (container cmd)
	if isInit {
		// We're in the init process - set up rootfs and then exec container process
		fmt.Fprintf(os.Stderr, "DEBUG CHILD: Entered init branch\n")
		fmt.Fprintf(os.Stderr, "DEBUG CHILD: Setting up container rootfs: %s\n", container.config.Rootfs)

		// Set up rootfs with pivot_root
		if err := setupRootfs(container); err != nil {
			return nil, fmt.Errorf("failed to setup rootfs: %w", err)
		}

		// Set hostname if specified in config
		if container.config.Hostname != "" {
			fmt.Fprintf(os.Stderr, "DEBUG: Setting hostname to: %s\n", container.config.Hostname)
			if err := unix.Sethostname([]byte(container.config.Hostname)); err != nil {
				return nil, fmt.Errorf("failed to set hostname: %w", err)
			}
		}

		// Resolve executable path AFTER pivot_root, using container's environment
		// This is how runc does it - look up after rootfs is set up
		execPath := processArgs[0]
		if !filepath.IsAbs(execPath) {
			// First check if it's directly in rootfs (e.g., /bin/sh)
			containerExecPath := filepath.Join(container.config.Rootfs, execPath)
			if _, err := os.Stat(containerExecPath); err == nil {
				execPath = containerExecPath
			} else {
				// Use container's PATH from process.Env to find the executable
				containerEnv := container.config.Process.Env
				pathValue := ""
				for _, env := range containerEnv {
					if strings.HasPrefix(env, "PATH=") {
						pathValue = strings.TrimPrefix(env, "PATH=")
						break
					}
				}
				if pathValue != "" {
					// Set PATH temporarily for LookPath
					oldPath := os.Getenv("PATH")
					os.Setenv("PATH", pathValue)
					path, err := exec.LookPath(execPath)
					os.Setenv("PATH", oldPath)
					if err != nil {
						return nil, fmt.Errorf("executable %q not found in container PATH: %w", execPath, err)
					}
					execPath = path
				} else {
					return nil, fmt.Errorf("no PATH set in container environment")
				}
			}
		}

		fmt.Fprintf(os.Stderr, "DEBUG: Resolved executable path: %s\n", execPath)
		fmt.Fprintf(os.Stderr, "DEBUG: Rootfs setup complete, executing container process: %v\n", processArgs)

		// Replace the first arg with the resolved path
		processArgs[0] = execPath

		// Use syscall.Exec to replace current process
		err := syscall.Exec(execPath, processArgs, container.config.Process.Env)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG: syscall.Exec failed: %v\n", err)
			return nil, fmt.Errorf("failed to exec container process %s: %w", execPath, err)
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

		// Pass root path through to child process so it can find the container state
		// container.root is /root/containername, but we need /root for the factory to load container
		containerRoot := filepath.Dir(container.root)

		// Debug: Check rootfs accessibility
		fmt.Fprintf(os.Stderr, "DEBUG: Rootfs path: %s\n", container.config.Rootfs)
		rootfsInfo, err := os.Stat(container.config.Rootfs)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG: Rootfs stat error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "DEBUG: Rootfs is dir: %v, mode: %v\n", rootfsInfo.IsDir(), rootfsInfo.Mode())
		}

		// Debug: Check if execPath exists
		execInfo, err := os.Stat(execPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG: ExecPath stat error: %v\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "DEBUG: ExecPath exists: %s, mode: %v\n", execPath, execInfo.Mode())
		}

		initArgs := []string{execPath, "--root", containerRoot, "init", container.id, absBundle}
		cmd := &exec.Cmd{
			Path:   execPath,
			Args:   initArgs,
			Stdout: os.Stdout,
			Stderr: os.Stderr,
			Stdin:  os.Stdin,
			Dir:    container.config.Rootfs,
			Env:    container.config.Process.Env,
			SysProcAttr: &syscall.SysProcAttr{
				Cloneflags: syscall.CLONE_NEWNS | syscall.CLONE_NEWPID | syscall.CLONE_NEWUTS,
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
