package libcontainer

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// MountManager handles container mount operations
type MountManager struct {
	rootfs string
}

// NewMountManager creates a new mount manager
func NewMountManager(rootfs string) *MountManager {
	return &MountManager{
		rootfs: rootfs,
	}
}

// SetupRootfs sets up the container's root filesystem
func (mm *MountManager) SetupRootfs() error {
	// Ensure rootfs exists
	if err := os.MkdirAll(mm.rootfs, 0755); err != nil {
		return err
	}

	// Bind mount rootfs to itself
	if err := syscall.Mount(mm.rootfs, mm.rootfs, "bind", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount rootfs failed: %w", err)
	}

	// Make rootfs private
	if err := syscall.Mount("", mm.rootfs, "none", syscall.MS_PRIVATE|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("make rootfs private failed: %w", err)
	}

	return nil
}

// SetupPivotRoot performs pivot_root to change root filesystem
func (mm *MountManager) SetupPivotRoot() error {
	// First, bind mount rootfs to itself to prepare for pivot_root
	if err := syscall.Mount(mm.rootfs, mm.rootfs, "bind", syscall.MS_BIND|syscall.MS_REC, ""); err != nil {
		return fmt.Errorf("bind mount rootfs failed: %w", err)
	}

	// Create .pivot_root directory (remove if exists)
	pivotDir := filepath.Join(mm.rootfs, ".pivot_root")
	if err := os.RemoveAll(pivotDir); err != nil {
		return fmt.Errorf("failed to remove existing pivot directory: %w", err)
	}
	if err := os.Mkdir(pivotDir, 0700); err != nil {
		return fmt.Errorf("failed to create pivot directory: %w", err)
	}

	// Pivot root using chroot as fallback since we're in mount namespace
	if err := syscall.Chroot(mm.rootfs); err != nil {
		return fmt.Errorf("chroot failed: %w", err)
	}

	// Change to new root
	if err := syscall.Chdir("/"); err != nil {
		return fmt.Errorf("chdir to new root failed: %w", err)
	}

	// Remove pivot directory
	if err := os.RemoveAll("/.pivot_root"); err != nil {
		return fmt.Errorf("remove pivot directory failed: %w", err)
	}

	return nil
}

// MountProc mounts /proc filesystem
func (mm *MountManager) MountProc() error {
	procPath := filepath.Join(mm.rootfs, "proc")
	if err := os.MkdirAll(procPath, 0755); err != nil {
		return err
	}

	return syscall.Mount("proc", procPath, "proc", 0, "")
}

// MountSysfs mounts /sys filesystem
func (mm *MountManager) MountSysfs() error {
	sysPath := filepath.Join(mm.rootfs, "sys")
	if err := os.MkdirAll(sysPath, 0755); err != nil {
		return err
	}

	return syscall.Mount("sysfs", sysPath, "sysfs", 0, "")
}

// MountDevpts mounts /dev/pts filesystem
func (mm *MountManager) MountDevpts() error {
	devptsPath := filepath.Join(mm.rootfs, "dev", "pts")
	if err := os.MkdirAll(devptsPath, 0755); err != nil {
		return err
	}

	return syscall.Mount("devpts", devptsPath, "devpts", syscall.MS_NOSUID|syscall.MS_NOEXEC, "newinstance,ptmxmode=0666,mode=0620,gid=5")
}

// MountTmpfs mounts tmpfs filesystems
func (mm *MountManager) MountTmpfs() error {
	// Mount /dev/shm
	shmPath := filepath.Join(mm.rootfs, "dev", "shm")
	if err := os.MkdirAll(shmPath, 0755); err != nil {
		return err
	}
	if err := syscall.Mount("shm", shmPath, "tmpfs", 0, "size=65536k"); err != nil {
		return err
	}

	// Mount /run
	runPath := filepath.Join(mm.rootfs, "run")
	if err := os.MkdirAll(runPath, 0755); err != nil {
		return err
	}
	if err := syscall.Mount("tmpfs", runPath, "tmpfs", 0, "size=65536k"); err != nil {
		return err
	}

	return nil
}

// SetupDevices sets up essential device nodes
func (mm *MountManager) SetupDevices() error {
	devPath := filepath.Join(mm.rootfs, "dev")
	if err := os.MkdirAll(devPath, 0755); err != nil {
		return err
	}

	// Create essential device nodes
	devices := []struct {
		path string
		mode uint32
		dev  int
	}{
		{"/dev/null", syscall.S_IFCHR | 0666, int(0x1<<8 | 0x3)},
		{"/dev/zero", syscall.S_IFCHR | 0666, int(0x1<<8 | 0x5)},
		{"/dev/full", syscall.S_IFCHR | 0666, int(0x1<<8 | 0x7)},
		{"/dev/tty", syscall.S_IFCHR | 0666, int(0x5<<8 | 0x0)},
		{"/dev/urandom", syscall.S_IFCHR | 0666, int(0x1<<8 | 0x9)},
		{"/dev/random", syscall.S_IFCHR | 0666, int(0x1<<8 | 0x8)},
	}

	for _, device := range devices {
		devicePath := filepath.Join(mm.rootfs, device.path)
		if err := syscall.Mknod(devicePath, device.mode, device.dev); err != nil {
			// Device might already exist, continue
			continue
		}
		if err := os.Chmod(devicePath, os.FileMode(device.mode)&0777); err != nil {
			continue
		}
	}

	// Create /dev/console symlink to current tty
	consolePath := filepath.Join(mm.rootfs, "dev", "console")
	if err := os.Symlink("/dev/tty1", consolePath); err != nil {
		// Continue if symlink fails
	}

	return nil
}

// Cleanup unmounts all mounted filesystems
func (mm *MountManager) Cleanup() error {
	// Unmount common filesystems
	mounts := []string{
		filepath.Join(mm.rootfs, "proc"),
		filepath.Join(mm.rootfs, "sys"),
		filepath.Join(mm.rootfs, "dev", "pts"),
		filepath.Join(mm.rootfs, "dev", "shm"),
		filepath.Join(mm.rootfs, "run"),
	}

	for _, mount := range mounts {
		syscall.Unmount(mount, 2) // MNT_DETACH = 2
	}

	return nil
}
