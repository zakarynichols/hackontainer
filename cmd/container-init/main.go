package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/zakarynichols/hackontainer/config"
)

// ContainerInit is the init process that runs inside the container
func main() {
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <pipe-fd> <config>\n", os.Args[0])
		os.Exit(1)
	}

	pipeFD := os.Args[1]
	configPath := os.Args[2]

	// Parse pipe file descriptor
	fd, err := strconv.Atoi(pipeFD)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Invalid pipe FD: %v\n", err)
		os.Exit(1)
	}

	pipe := os.NewFile(uintptr(fd), "pipe")
	if pipe == nil {
		fmt.Fprintf(os.Stderr, "Failed to create pipe from FD\n")
		os.Exit(1)
	}

	// Load container configuration
	containerConfig, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	// Normalize root path to be absolute
	if !filepath.IsAbs(containerConfig.Root.Path) {
		bundleDir := filepath.Dir(configPath)
		containerConfig.Root.Path = filepath.Join(bundleDir, containerConfig.Root.Path)
	}

	// Debug output
	fmt.Fprintf(os.Stderr, "DEBUG: Bundle dir: %s\n", filepath.Dir(configPath))
	fmt.Fprintf(os.Stderr, "DEBUG: Rootfs path: %s\n", containerConfig.Root.Path)

	// Check if rootfs exists and is accessible
	if _, err := os.Stat(containerConfig.Root.Path); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: Cannot access rootfs %s: %v\n", containerConfig.Root.Path, err)
		os.Exit(1)
	}

	// Change working directory to rootfs FIRST, before any mount operations
	absRootPath := containerConfig.Root.Path
	if !filepath.IsAbs(absRootPath) {
		var absErr error
		absRootPath, absErr = filepath.Abs(absRootPath)
		if absErr != nil {
			fmt.Fprintf(os.Stderr, "Failed to get absolute path for rootfs: %v\n", absErr)
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stderr, "DEBUG: Changing to directory: %s\n", absRootPath)
	var chdirErr error
	chdirErr = os.Chdir(absRootPath)
	if chdirErr != nil {
		fmt.Fprintf(os.Stderr, "Failed to change to rootfs directory %s: %v\n", absRootPath, chdirErr)
		os.Exit(1)
	}

	// Prepare for exec
	args := containerConfig.Process.Args
	if len(args) == 0 {
		args = []string{"/bin/sh"}
	}

	execPath := args[0]
	if !filepath.IsAbs(execPath) {
		// Look in container rootfs
		containerExecPath := filepath.Join(absRootPath, execPath)
		if _, err := os.Stat(containerExecPath); err == nil {
			execPath = containerExecPath
		} else {
			// Try to find in PATH
			path, err := exec.LookPath(execPath)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Executable %q not found: tried %s and PATH\n", execPath, containerExecPath)
				os.Exit(1)
			}
			execPath = path
		}
	}

	// Setup hostname if specified
	if containerConfig.Spec != nil && containerConfig.Spec.Hostname != "" {
		hostname := containerConfig.Spec.Hostname
		if err := syscall.Sethostname([]byte(hostname)); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to set hostname: %v\n", err)
			os.Exit(1)
		}
	}

	// Signal parent that we're ready
	if _, err := pipe.Write([]byte{1}); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to signal parent: %v\n", err)
		os.Exit(1)
	}
	pipe.Close()

	// Now perform chroot
	if err := syscall.Chroot("."); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to chroot from %s: %v\n", absRootPath, err)
		os.Exit(1)
	}

	// Change to new root
	if err := syscall.Chdir("/"); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to chdir to new root: %v\n", err)
		os.Exit(1)
	}

	// Change to specified working directory
	if containerConfig.Process.Cwd != "" && containerConfig.Process.Cwd != "/" {
		if err := os.Chdir(containerConfig.Process.Cwd); err != nil {
			fmt.Fprintf(os.Stderr, "Failed to change directory to %s: %v\n", containerConfig.Process.Cwd, err)
			os.Exit(1)
		}
	}

	// Set environment variables
	os.Setenv("PATH", "/bin:/usr/bin:/usr/local/bin:/sbin:/usr/sbin:/usr/local/sbin")
	for _, env := range containerConfig.Process.Env {
		parts := strings.SplitN(env, "=", 2)
		if len(parts) == 2 {
			os.Setenv(parts[0], parts[1])
		}
	}

	// Execute the container process
	if err := syscall.Exec(execPath, args, os.Environ()); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to exec %s: %v\n", execPath, err)
		os.Exit(1)
	}
}
