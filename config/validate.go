package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
)

func Validate(spec *specs.Spec) error {
	if spec == nil {
		return fmt.Errorf("spec cannot be nil")
	}

	if err := validateProcess(spec.Process); err != nil {
		return fmt.Errorf("process validation failed: %w", err)
	}

	if err := validateRoot(spec.Root); err != nil {
		return fmt.Errorf("root validation failed: %w", err)
	}

	if err := validateLinux(spec); err != nil {
		return fmt.Errorf("linux validation failed: %w", err)
	}

	if err := validateMounts(spec.Mounts); err != nil {
		return fmt.Errorf("mounts validation failed: %w", err)
	}

	return nil
}

func validateProcess(process *specs.Process) error {
	if process == nil {
		return fmt.Errorf("process cannot be nil")
	}

	if len(process.Args) == 0 {
		return fmt.Errorf("process args cannot be empty")
	}

	if process.Cwd == "" {
		return fmt.Errorf("process working directory cannot be empty")
	}

	if !filepath.IsAbs(process.Cwd) {
		return fmt.Errorf("process working directory must be absolute path")
	}

	for _, env := range process.Env {
		if !strings.Contains(env, "=") {
			return fmt.Errorf("invalid environment variable format: %s", env)
		}
	}

	return nil
}

func validateRoot(root *specs.Root) error {
	if root == nil {
		return fmt.Errorf("root cannot be nil")
	}

	if root.Path == "" {
		return fmt.Errorf("root path cannot be empty")
	}

	if _, err := os.Stat(root.Path); os.IsNotExist(err) {
		return fmt.Errorf("root filesystem does not exist: %s", root.Path)
	}

	return nil
}

func validateLinux(spec *specs.Spec) error {
	if spec.Linux == nil {
		return nil
	}

	for _, ns := range spec.Linux.Namespaces {
		if ns.Type == "" {
			return fmt.Errorf("namespace type cannot be empty")
		}

		switch ns.Type {
		case specs.PIDNamespace,
			specs.NetworkNamespace,
			specs.MountNamespace,
			specs.UTSNamespace,
			specs.IPCNamespace,
			specs.UserNamespace,
			specs.CgroupNamespace:
		default:
			return fmt.Errorf("invalid namespace type: %s", ns.Type)
		}
	}

	return nil
}

func validateMounts(mounts []specs.Mount) error {
	for _, mount := range mounts {
		if mount.Destination == "" {
			return fmt.Errorf("mount destination cannot be empty")
		}

		if mount.Type == "" {
			return fmt.Errorf("mount type cannot be empty")
		}

		if !filepath.IsAbs(mount.Destination) {
			return fmt.Errorf("mount destination must be absolute path: %s", mount.Destination)
		}
	}

	return nil
}
