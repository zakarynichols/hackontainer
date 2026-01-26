package libcontainer

import (
	"syscall"
)

// LinuxNamespace represents a Linux namespace
type LinuxNamespace struct {
	Type NamespaceType
	Path string
}

type NamespaceType string

const (
	CLONE_NEWNS   NamespaceType = "mount"   // Mount namespace
	CLONE_NEWUTS  NamespaceType = "uts"     // UTS namespace
	CLONE_NEWIPC  NamespaceType = "ipc"     // IPC namespace
	CLONE_NEWPID  NamespaceType = "pid"     // PID namespace
	CLONE_NEWNET  NamespaceType = "network" // Network namespace
	CLONE_NEWUSER NamespaceType = "user"    // User namespace
)

// NamespaceCloneFlags converts namespace type to syscall clone flag
func NamespaceCloneFlags(nsType NamespaceType) uintptr {
	switch nsType {
	case CLONE_NEWNS:
		return syscall.CLONE_NEWNS
	case CLONE_NEWUTS:
		return syscall.CLONE_NEWUTS
	case CLONE_NEWIPC:
		return syscall.CLONE_NEWIPC
	case CLONE_NEWPID:
		return syscall.CLONE_NEWPID
	case CLONE_NEWNET:
		return syscall.CLONE_NEWNET
	case CLONE_NEWUSER:
		return syscall.CLONE_NEWUSER
	default:
		return 0
	}
}

// GetNamespaceFlagMappings returns the namespace flag mappings for the container
func GetNamespaceFlagMappings(namespaces []LinuxNamespace) uintptr {
	var flags uintptr

	for _, ns := range namespaces {
		flags |= NamespaceCloneFlags(ns.Type)
	}

	return flags
}
