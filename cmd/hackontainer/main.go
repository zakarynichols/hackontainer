package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/zakarynichols/hackontainer/config"
	"github.com/zakarynichols/hackontainer/libcontainer"
)

var (
	rootDir     = "/run/hackontainer"
	rootlessVal = "auto"
)

func findCommand() string {
	commands := map[string]bool{
		"create": true, "delete": true, "run": true,
		"start": true, "state": true, "kill": true, "init": true,
	}
	for _, arg := range os.Args {
		if commands[arg] {
			return arg
		}
	}
	return ""
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Parse global flags first
	parseGlobalFlags()

	cmd := findCommand()
	if cmd == "" {
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		printUsage()
		os.Exit(1)
	}

	var err error
	switch cmd {
	case "create":
		err = runCreate()
	case "delete":
		err = runDelete()
	case "run":
		err = runRun()
	case "start":
		err = runStart()
	case "state":
		err = runState()
	case "kill":
		err = runKill()
	case "init":
		err = runInit()
	case "-h", "-help", "--help":
		printUsage()
		os.Exit(0)
	case "-v", "-version", "--version":
		fmt.Println("hackontainer version 1.0.0")
		os.Exit(0)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func parseGlobalFlags() {
	// Parse global flags - can appear before OR after the subcommand
	// os.Args format: [hackontainer [flags] command [flags] args]
	// or for init: [hackontainer --root /path init id bundle]
	i := 1
	for i < len(os.Args) {
		arg := os.Args[i]

		// Check if this is a known command (not a flag)
		if !strings.HasPrefix(arg, "-") {
			// If it's a known command, stop parsing global flags
			if arg == "create" || arg == "delete" || arg == "run" ||
				arg == "start" || arg == "state" || arg == "kill" || arg == "init" {
				break
			}
			// If it's not a known command and not a flag, treat as unknown
			// But keep going to find actual command
		}

		// Parse global flags
		if arg == "--root" && i+1 < len(os.Args) {
			rootDir = os.Args[i+1]
			i += 2
		} else if strings.HasPrefix(arg, "--root=") {
			rootDir = strings.TrimPrefix(arg, "--root=")
			i++
		} else if arg == "--rootless" && i+1 < len(os.Args) {
			rootlessVal = os.Args[i+1]
			i += 2
		} else if strings.HasPrefix(arg, "--rootless=") {
			rootlessVal = strings.TrimPrefix(arg, "--rootless=")
			i++
		} else {
			i++
		}
	}
}

func printUsage() {
	fmt.Println("Usage: hackontainer <command> [options]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  create <container-id>   create a container")
	fmt.Println("  delete <container-id>   delete a container")
	fmt.Println("  run <container-id>      create and run a container")
	fmt.Println("  start <container-id>    start a created container")
	fmt.Println("  state <container-id>    get container state")
	fmt.Println("  kill <container-id> [signal]  send signal to container")
	fmt.Println("")
	fmt.Println("Options:")
	fmt.Println("  --root <path>       root directory for container state (default: /run/hackontainer)")
	fmt.Println("  --rootless <mode>   ignore cgroup permission errors (default: auto)")
}

func findArgAfter(pos int) string {
	// Skip command name and global flags
	i := 2
	for i < len(os.Args) {
		arg := os.Args[i]
		if !strings.HasPrefix(arg, "-") {
			pos--
			if pos == 0 {
				return arg
			}
		}
		if (arg == "-b" || arg == "--bundle" || arg == "--pid-file") && i+1 < len(os.Args) {
			i += 2
			continue
		}
		if strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") {
			i++
			continue
		}
		if strings.HasPrefix(arg, "--") {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) == 1 {
				i += 2
				continue
			}
		}
		i++
	}
	return ""
}

func findFlag(flag string) string {
	for i := 2; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "-"+flag || arg == "--"+flag {
			if i+1 < len(os.Args) && !strings.HasPrefix(os.Args[i+1], "-") {
				return os.Args[i+1]
			}
		}
		if strings.HasPrefix(arg, "-"+flag+"=") {
			return strings.TrimPrefix(arg, "-"+flag+"=")
		}
		if strings.HasPrefix(arg, "--"+flag+"=") {
			return strings.TrimPrefix(arg, "--"+flag+"=")
		}
	}
	return ""
}

func runCreate() error {
	args := getArgsAfter(0)
	if len(args) != 1 {
		return fmt.Errorf("need exactly 1 argument, got %d", len(args))
	}

	containerID := args[0]
	bundle := findFlag("bundle")
	if bundle == "" {
		bundle = "."
	}
	pidFile := findFlag("pid-file")

	if _, err := os.Stat(rootDir + "/" + containerID); err == nil {
		return fmt.Errorf("container id '%s' already exists in directory %s/%s", containerID, rootDir, containerID)
	}

	factory, err := libcontainer.New(rootDir)
	if err != nil {
		return fmt.Errorf("failed to create factory: %w", err)
	}

	container, err := factory.Create(containerID, bundle)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	if pidFile != "" {
		state, err := container.State()
		if err != nil {
			return fmt.Errorf("failed to get container state: %w", err)
		}
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", state.Pid)), 0644); err != nil {
			return fmt.Errorf("failed to write PID file: %w", err)
		}
	}

	return nil
}

func runDelete() error {
	args := getArgsAfter(0)
	if len(args) != 1 {
		return fmt.Errorf("need exactly 1 argument, got %d", len(args))
	}

	containerID := args[0]

	factory, err := libcontainer.New(rootDir)
	if err != nil {
		return fmt.Errorf("failed to create factory: %w", err)
	}

	container, err := factory.Load(containerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	if err := container.Delete(); err != nil {
		return fmt.Errorf("failed to delete container: %w", err)
	}

	return nil
}

func runRun() error {
	args := getArgsAfter(0)
	if len(args) != 1 {
		return fmt.Errorf("need exactly 1 argument, got %d", len(args))
	}

	containerID := args[0]
	bundle := findFlag("bundle")
	if bundle == "" {
		bundle = "."
	}
	pidFile := findFlag("pid-file")

	factory, err := libcontainer.New(rootDir)
	if err != nil {
		return fmt.Errorf("failed to create factory: %w", err)
	}

	container, err := factory.Create(containerID, bundle)
	if err != nil {
		return fmt.Errorf("failed to create container: %w", err)
	}

	if err := container.Run(); err != nil {
		return fmt.Errorf("failed to run container: %w", err)
	}

	if pidFile != "" {
		state, err := container.State()
		if err != nil {
			return fmt.Errorf("failed to get container state: %w", err)
		}
		if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", state.Pid)), 0644); err != nil {
			return fmt.Errorf("failed to write PID file: %w", err)
		}
	}

	return nil
}

func runState() error {
	args := getArgsAfter(0)
	if len(args) != 1 {
		return fmt.Errorf("need exactly 1 argument, got %d", len(args))
	}

	containerID := args[0]

	factory, err := libcontainer.New(rootDir)
	if err != nil {
		return fmt.Errorf("failed to create factory: %w", err)
	}

	container, err := factory.Load(containerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	state, err := container.State()
	if err != nil {
		return fmt.Errorf("failed to get container state: %w", err)
	}

	status, err := container.Status()
	if err != nil {
		return fmt.Errorf("failed to get container status: %w", err)
	}

	state.Status = libcontainer.Status(status)

	json.NewEncoder(os.Stdout).Encode(state)
	return nil
}

func runStart() error {
	args := getArgsAfter(0)
	if len(args) != 1 {
		return fmt.Errorf("need exactly 1 argument, got %d", len(args))
	}

	containerID := args[0]

	factory, err := libcontainer.New(rootDir)
	if err != nil {
		return fmt.Errorf("failed to create factory: %w", err)
	}

	container, err := factory.Load(containerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	status, err := container.Status()
	if err != nil {
		return fmt.Errorf("failed to get container status: %w", err)
	}

	switch status {
	case libcontainer.Created:
		if err := container.Start(); err != nil {
			return fmt.Errorf("failed to start container: %w", err)
		}
		return nil
	case libcontainer.Stopped:
		return fmt.Errorf("cannot start a container that has stopped")
	case libcontainer.Running:
		return fmt.Errorf("cannot start an already running container")
	default:
		return fmt.Errorf("cannot start a container in the %s state", status)
	}
}

func runInit() error {
	args := getArgsAfter(0)
	if len(args) != 2 {
		return fmt.Errorf("need exactly 2 arguments, got %d", len(args))
	}

	containerID := args[0]
	bundle := args[1]

	factory, err := libcontainer.New(rootDir)
	if err != nil {
		return fmt.Errorf("failed to create factory: %w", err)
	}

	container, err := factory.Load(containerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	configPath := filepath.Join(bundle, "config.json")
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if cfg.Process == nil || len(cfg.Process.Args) == 0 {
		return fmt.Errorf("no process specified in config")
	}

	env := cfg.Process.Env
	if env == nil {
		env = os.Environ()
	}

	if err := container.InitProcess(); err != nil {
		return fmt.Errorf("failed to start init process: %w", err)
	}

	return nil
}

func runKill() error {
	args := getArgsAfter(0)
	if len(args) < 1 || len(args) > 2 {
		return fmt.Errorf("need 1 or 2 arguments, got %d", len(args))
	}

	containerID := args[0]

	factory, err := libcontainer.New(rootDir)
	if err != nil {
		return fmt.Errorf("failed to create factory: %w", err)
	}

	container, err := factory.Load(containerID)
	if err != nil {
		return fmt.Errorf("failed to load container: %w", err)
	}

	sigStr := "SIGTERM"
	if len(args) == 2 {
		sigStr = args[1]
	}

	sig, err := parseSignal(sigStr)
	if err != nil {
		return err
	}

	err = container.Signal(sig)
	if err != nil {
		return fmt.Errorf("failed to send signal: %w", err)
	}

	return nil
}

func getArgsAfter(skip int) []string {
	var args []string
	commands := map[string]bool{
		"create": true, "delete": true, "run": true,
		"start": true, "state": true, "kill": true, "init": true,
	}

	// Find the command position
	cmdPos := -1
	for i, arg := range os.Args {
		if commands[arg] {
			cmdPos = i
			break
		}
	}

	if cmdPos == -1 {
		return args
	}

	// Collect args after the command, skipping flags
	for i := cmdPos + 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if !strings.HasPrefix(arg, "-") {
			args = append(args, arg)
		} else if arg == "-b" || arg == "--bundle" || arg == "--pid-file" || arg == "--console-socket" {
			// Skip flag value
			i++
		} else if strings.HasPrefix(arg, "--") && strings.Contains(arg, "=") {
			// Skip --flag=value format
		} else if len(arg) == 2 && arg[0] == '-' {
			// Skip single dash flags (-b etc)
			i++
		}
	}
	return args
}

func parseSignal(rawSignal string) (syscall.Signal, error) {
	s, err := strconv.Atoi(rawSignal)
	if err == nil {
		return syscall.Signal(s), nil
	}

	sig := strings.ToUpper(rawSignal)
	if !strings.HasPrefix(sig, "SIG") {
		sig = "SIG" + sig
	}

	signalNum := stringToSignal(sig)
	if signalNum == 0 {
		return 0, fmt.Errorf("unknown signal %q", rawSignal)
	}

	return signalNum, nil
}

func stringToSignal(sig string) syscall.Signal {
	switch sig {
	case "SIGTERM":
		return syscall.SIGTERM
	case "SIGKILL":
		return syscall.SIGKILL
	case "SIGINT":
		return syscall.SIGINT
	case "SIGQUIT":
		return syscall.SIGQUIT
	case "SIGHUP":
		return syscall.SIGHUP
	case "SIGUSR1":
		return syscall.SIGUSR1
	case "SIGUSR2":
		return syscall.SIGUSR2
	case "SIGWINCH":
		return syscall.SIGWINCH
	default:
		return 0
	}
}
