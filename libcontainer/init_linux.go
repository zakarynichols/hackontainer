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

func mount(source, target, fstype string, flags uintptr, data string) error {
	if err := unix.Mount(source, target, fstype, flags, data); err != nil {
		return &os.PathError{Op: "mount", Path: target, Err: err}
	}
	return nil
}

func unmount(target string, flags int) error {
	if err := unix.Unmount(target, flags); err != nil {
		return &os.PathError{Op: "unmount", Path: target, Err: err}
	}
	return nil
}

func prepareRoot(rootfs string) error {
	if err := mount("", "/", "", unix.MS_PRIVATE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to make root mount private: %w", err)
	}

	flag := unix.MS_SLAVE | unix.MS_REC
	if err := mount("", "/", "", uintptr(flag), ""); err != nil {
		return fmt.Errorf("failed to make root mount slave: %w", err)
	}

	if err := mount(rootfs, rootfs, "bind", unix.MS_BIND|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to bind mount rootfs: %w", err)
	}

	return nil
}

func pivotRoot(rootfs string) error {
	oldroot, err := unix.Open("/", unix.O_DIRECTORY|unix.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open old root: %w", err)
	}
	defer unix.Close(oldroot)

	newroot, err := unix.Open(rootfs, unix.O_DIRECTORY|unix.O_RDONLY, 0)
	if err != nil {
		return fmt.Errorf("failed to open new root: %w", err)
	}
	defer unix.Close(newroot)

	if err := unix.Fchdir(newroot); err != nil {
		return fmt.Errorf("failed to fchdir to new root: %w", err)
	}

	if err := unix.PivotRoot(".", "."); err != nil {
		return fmt.Errorf("failed to pivot_root: %w", err)
	}

	if err := unix.Fchdir(oldroot); err != nil {
		return fmt.Errorf("failed to fchdir to old root: %w", err)
	}

	if err := mount("", ".", "", unix.MS_SLAVE|unix.MS_REC, ""); err != nil {
		return fmt.Errorf("failed to make old root slave: %w", err)
	}

	if err := unmount(".", unix.MNT_DETACH); err != nil {
		return fmt.Errorf("failed to unmount old root: %w", err)
	}

	if err := unix.Chdir("/"); err != nil {
		return fmt.Errorf("failed to chdir to new root: %w", err)
	}

	return nil
}

func setupRootfs(container *linuxContainer) error {
	if err := prepareRoot(container.config.Rootfs); err != nil {
		return fmt.Errorf("failed to prepare root: %w", err)
	}

	if err := unix.Chdir(container.config.Rootfs); err != nil {
		return fmt.Errorf("failed to chdir to rootfs: %w", err)
	}

	if err := pivotRoot(container.config.Rootfs); err != nil {
		return fmt.Errorf("failed to pivot_root: %w", err)
	}

	if err := os.MkdirAll("/proc", 0755); err != nil {
		return fmt.Errorf("failed to create /proc directory: %w", err)
	}

	if err := unix.Mount("proc", "/proc", "proc", unix.MS_NOSUID|unix.MS_NOEXEC|unix.MS_NODEV, ""); err != nil {
		return fmt.Errorf("failed to mount /proc: %w", err)
	}

	return nil
}

// RunAsChild is called by main() when --child flag is detected
// This runs in the forked child process to set up and exec the container
func RunAsChild(bundle string) error {
	cfg, err := loadContainerConfig(bundle)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.NormalizeRoot(); err != nil {
		return err
	}

	container := &linuxContainer{
		config: cfg,
		bundle: bundle,
	}

	fmt.Printf(">>> [CHILD] Running in new namespaces, setting up container...\n")

	// Step 1: pivot_root
	fmt.Printf(">>> [CHILD] Calling setupRootfs (pivot_root)...\n")
	if err := setupRootfs(container); err != nil {
		return fmt.Errorf("failed to setup rootfs: %w", err)
	}
	fmt.Printf(">>> [CHILD] pivot_root completed.\n")

	// Step 2: Set hostname
	if container.config.Hostname != "" {
		fmt.Printf(">>> [CHILD] Setting hostname to: %s\n", container.config.Hostname)
		if err := unix.Sethostname([]byte(container.config.Hostname)); err != nil {
			return fmt.Errorf("failed to set hostname: %w", err)
		}
	}

	// Step 3: Resolve and exec
	args := container.config.Process.Args
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}

	execPath := args[0]
	fmt.Printf(">>> [CHILD] Resolving executable: %s\n", execPath)
	if !filepath.IsAbs(execPath) {
		containerExecPath := filepath.Join(container.config.Rootfs, execPath)
		if _, err := os.Stat(containerExecPath); err == nil {
			execPath = containerExecPath
		} else {
			pathValue := ""
			for _, env := range container.config.Process.Env {
				if strings.HasPrefix(env, "PATH=") {
					pathValue = strings.TrimPrefix(env, "PATH=")
					break
				}
			}
			if pathValue != "" {
				oldPath := os.Getenv("PATH")
				os.Setenv("PATH", pathValue)
				path, err := exec.LookPath(execPath)
				os.Setenv("PATH", oldPath)
				if err != nil {
					return fmt.Errorf("executable %q not found: %w", execPath, err)
				}
				execPath = path
			} else {
				return fmt.Errorf("no PATH set")
			}
		}
	}

	fmt.Printf(">>> [CHILD] Executing: %s %v\n", execPath, args)
	err = syscall.Exec(execPath, args, container.config.Process.Env)
	return fmt.Errorf("exec failed: %w", err)
}

/*
 * SINGLE-PROCESS PATTERN:
 *
 * PARENT: Creates exec.Cmd with --child flag, calls Start()
 * CHILD:  Detected by --child flag, runs RunAsChild() which does pivot_root + exec
 */
func newInitProcess(container *linuxContainer) (*initProcess, error) {
	// Check if we're the child after fork
	isChild := false
	for _, arg := range os.Args {
		if arg == "--child" {
			isChild = true
			break
		}
	}

	if isChild {
		// This path is actually handled in main() now
		// This function won't be called in child because main() handles --child
		return nil, fmt.Errorf("child mode should be handled in main()")
	}

	// Parent path: create exec.Cmd
	fmt.Printf(">>> [PARENT] Creating container process with namespaces...\n")
	fmt.Printf(">>> [PARENT] Namespaces: pid, net, ipc, uts, cgroup, time, mount\n")

	execPath, err := os.Executable()
	if err != nil {
		execPath = os.Args[0]
	}

	absBundle, _ := filepath.Abs(container.bundle)
	cmd := &exec.Cmd{
		Path:   execPath,
		Args:   []string{execPath, "--child", "--bundle", absBundle},
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
		Dir:    "/",
		Env:    container.config.Process.Env,
		SysProcAttr: &syscall.SysProcAttr{
			Cloneflags: syscall.CLONE_NEWNS |
				syscall.CLONE_NEWPID |
				syscall.CLONE_NEWUTS |
				syscall.CLONE_NEWNET |
				syscall.CLONE_NEWIPC |
				syscall.CLONE_NEWCGROUP |
				syscall.CLONE_NEWTIME,
		},
	}

	fmt.Printf(">>> [PARENT] Returning cmd. Parent will call cmd.Start() to fork child.\n")

	return &initProcess{
		cmd:       cmd,
		container: container,
	}, nil
}
