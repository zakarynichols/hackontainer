package libcontainer

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// CgroupManager manages cgroups for a container
type CgroupManager struct {
	root    string
	cgroups map[string]string
}

// NewCgroupManager creates a new cgroup manager
func NewCgroupManager(root string) *CgroupManager {
	// Use user session cgroup or system cgroup hierarchy
	systemRoot := "/sys/fs/cgroup"

	// Check if we can write to system cgroup
	if _, err := os.Stat(systemRoot); err != nil {
		// Fallback to /tmp/cgroup for testing
		systemRoot = "/tmp/cgroup"
		os.MkdirAll(systemRoot, 0755)
	}

	return &CgroupManager{
		root:    systemRoot,
		cgroups: make(map[string]string),
	}
}

// Setup creates and configures cgroups for the container
func (cm *CgroupManager) Setup(containerID string, config interface{}) error {
	// Try system cgroup first, fallback to tmp
	var cgroupPath string

	// Try system cgroup
	systemPath := filepath.Join("/sys/fs/cgroup", "hackontainer", containerID)
	if err := os.MkdirAll(systemPath, 0755); err == nil {
		cgroupPath = systemPath
	} else {
		// Fallback to tmp cgroup
		tmpPath := filepath.Join("/tmp/cgroup", "hackontainer", containerID)
		if err := os.MkdirAll(tmpPath, 0755); err != nil {
			return fmt.Errorf("failed to create cgroup directory: %w", err)
		}
		cgroupPath = tmpPath
	}

	// Store cgroup path for cleanup
	cm.cgroups[containerID] = cgroupPath

	// Setup basic cgroup controllers (skip if not writable)
	cm.setupControllers(cgroupPath, config)

	return nil
}

// setupControllers configures various cgroup controllers
func (cm *CgroupManager) setupControllers(path string, config interface{}) error {
	// For cgroup v2, we write to unified files
	// Setup memory limit
	if err := os.WriteFile(filepath.Join(path, "memory.max"), []byte("536870912"), 0644); err != nil {
		// Continue if this fails
	}

	// Setup CPU weight
	if err := os.WriteFile(filepath.Join(path, "cpu.weight"), []byte("100"), 0644); err != nil {
		// Continue if this fails
	}

	// Setup PID limit
	if err := os.WriteFile(filepath.Join(path, "pids.max"), []byte("100"), 0644); err != nil {
		// Continue if this fails
	}

	return nil
}

// setupMemoryController configures memory limits
func (cm *CgroupManager) setupMemoryController(path string) error {
	memoryPath := filepath.Join(path, "memory.limit_in_bytes")

	// Set reasonable memory limit (default: 512MB)
	if err := os.WriteFile(memoryPath, []byte("536870912"), 0644); err != nil {
		return err
	}

	return nil
}

// setupCpuController configures CPU limits
func (cm *CgroupManager) setupCpuController(path string) error {
	cpuPath := filepath.Join(path, "cpu.shares")

	// Set CPU shares (default: 1024)
	if err := os.WriteFile(cpuPath, []byte("1024"), 0644); err != nil {
		return err
	}

	return nil
}

// setupPidController configures PID limits
func (cm *CgroupManager) setupPidController(path string) error {
	pidPath := filepath.Join(path, "pids.max")

	// Set max PIDs (default: 100)
	if err := os.WriteFile(pidPath, []byte("100"), 0644); err != nil {
		return err
	}

	return nil
}

// setupDevicesController configures device access
func (cm *CgroupManager) setupDevicesController(path string) error {
	devicesPath := filepath.Join(path, "devices.deny")

	// Deny all devices by default
	if err := os.WriteFile(devicesPath, []byte("a"), 0644); err != nil {
		return err
	}

	// Allow essential devices
	essentialDevices := []string{
		"c 1:3 rwm",   // /dev/null
		"c 1:5 rwm",   // /dev/zero
		"c 1:7 rwm",   // /dev/full
		"c 5:0 rwm",   // /dev/tty
		"c 1:8 rwm",   // /dev/random
		"c 1:9 rwm",   // /dev/urandom
		"c 136:* rwm", // /dev/tty* devices
	}

	devicesAllowPath := filepath.Join(path, "devices.allow")
	for _, device := range essentialDevices {
		if err := os.WriteFile(devicesAllowPath, []byte(device), 0644); err != nil {
			return err
		}
	}

	return nil
}

// AddProcess adds a process PID to the cgroup
func (cm *CgroupManager) AddProcess(containerID string, pid int) error {
	cgroupPath, exists := cm.cgroups[containerID]
	if !exists {
		return fmt.Errorf("cgroup not found for container %s", containerID)
	}

	// Add PID to cgroup.procs (cgroup v2)
	procsPath := filepath.Join(cgroupPath, "cgroup.procs")
	if err := os.WriteFile(procsPath, []byte(strconv.Itoa(pid)), 0644); err != nil {
		// Log but don't fail - cgroups might not be available
		fmt.Printf("Warning: failed to add process to cgroup: %v\n", err)
	}

	return nil
}

// Destroy removes cgroup for the container
func (cm *CgroupManager) Destroy(containerID string) error {
	cgroupPath, exists := cm.cgroups[containerID]
	if !exists {
		return nil
	}

	// Remove cgroup directory
	if err := os.RemoveAll(cgroupPath); err != nil {
		return err
	}

	delete(cm.cgroups, containerID)
	return nil
}
