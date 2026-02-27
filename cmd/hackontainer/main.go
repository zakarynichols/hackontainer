package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/urfave/cli"
	"github.com/zakarynichols/hackontainer/config"
	"github.com/zakarynichols/hackontainer/libcontainer"
	"github.com/zakarynichols/hackontainer/libcontainer/utils"
)

const (
	specConfig = "config.json"
	usage      = `OCI runtime`
)

func main() {
	app := cli.NewApp()
	app.Name = "hackontainer"
	app.Usage = usage
	app.Version = "1.0.0"

	app.Flags = []cli.Flag{
		cli.BoolFlag{
			Name:  "debug",
			Usage: "enable debug logging",
		},
		cli.StringFlag{
			Name:  "log",
			Value: "",
			Usage: "set the log file to write runtime logs to (default is '/dev/stderr')",
		},
		cli.StringFlag{
			Name:  "log-format",
			Value: "text",
			Usage: "set the log format ('text' (default), or 'json')",
		},
		cli.StringFlag{
			Name:  "root",
			Value: "/run/hackontainer",
			Usage: "root directory for storage of container state (this should be located in tmpfs)",
		},
		cli.StringFlag{
			Name:  "rootless",
			Value: "auto",
			Usage: "ignore cgroup permission errors ('true', 'false', or 'auto')",
		},
	}

	app.Commands = []cli.Command{
		createCommand,
		deleteCommand,
		runCommand,
		startCommand,
		stateCommand,
		killCommand,
		initCommand,
	}

	app.Before = func(context *cli.Context) error {
		return setupLogging(context)
	}

	if err := app.Run(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func setupLogging(context *cli.Context) error {
	logConfig := &utils.LoggingConfig{
		Debug:     context.GlobalBool("debug"),
		LogFile:   context.GlobalString("log"),
		LogFormat: context.GlobalString("log-format"),
	}

	return utils.SetupLogging(logConfig)
}

func checkArgs(context *cli.Context, expected int, exact bool) error {
	if !exact && context.NArg() < expected {
		return fmt.Errorf("need at least %d arguments, got %d", expected, context.NArg())
	}
	if exact && context.NArg() != expected {
		return fmt.Errorf("need exactly %d arguments, got %d", expected, context.NArg())
	}
	return nil
}

/*
Create

create <container-id> <path-to-bundle>

This operation MUST generate an error if it is not provided a path to the
bundle and the container ID to associate with the container. If the ID
provided is not unique across all containers within the scope of the
runtime, or is not valid in any other way, the implementation MUST generate
an error and a new container MUST NOT be created. This operation MUST create
a new container.

All of the properties configured in config.json except for process MUST be
applied. process.args MUST NOT be applied until triggered by the start
operation. The remaining process properties MAY be applied by this
operation. If the runtime cannot apply a property as specified in the
configuration, it MUST generate an error and a new container MUST NOT be
created.

The runtime MAY validate config.json against this spec, either generically
or with respect to the local system capabilities, before creating the
container (step 2). Runtime callers who are interested in pre-create
validation can run bundle-validation tools before invoking the create
operation.

Any changes made to the config.json file after this operation will not have
an effect on the container.
*/
var createCommand = cli.Command{
	Name:  "create",
	Usage: "create a container",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "bundle, b",
			Value: ".",
			Usage: "path to the container's bundle directory",
		},
		cli.StringFlag{
			Name:  "pid-file",
			Usage: "path to a file to write the container's PID",
		},
	},
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, true); err != nil {
			return err
		}

		containerID := context.Args()[0]
		bundle := context.String("bundle")

		if _, err := os.Stat(context.GlobalString("root") + "/" + containerID); err == nil {
			return fmt.Errorf("container id '%s' already exists in directory %s/%s", containerID, context.GlobalString("root"), containerID)
		}

		factory, err := libcontainer.New(context.GlobalString("root"))
		if err != nil {
			return fmt.Errorf("failed to create factory: %w", err)
		}

		container, err := factory.Create(containerID, bundle)
		if err != nil {
			return fmt.Errorf("failed to create container: %w", err)
		}

		// Write pid-file if specified
		if pidFile := context.String("pid-file"); pidFile != "" {
			state, err := container.State()
			if err != nil {
				return fmt.Errorf("failed to get container state: %w", err)
			}
			if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", state.Pid)), 0644); err != nil {
				return fmt.Errorf("failed to write PID file: %w", err)
			}
		}

		utils.Infof("Container %s created successfully", container.ID())
		return nil
	},
}

var deleteCommand = cli.Command{
	Name:  "delete",
	Usage: "delete a container",
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, true); err != nil {
			return err
		}

		containerID := context.Args().First()

		factory, err := libcontainer.New(context.GlobalString("root"))
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

		utils.Infof("Container %s deleted successfully", containerID)
		return nil
	},
}

var runCommand = cli.Command{
	Name:  "run",
	Usage: "create and run a container",
	Flags: []cli.Flag{
		cli.StringFlag{
			Name:  "bundle, b",
			Value: ".",
			Usage: "path to the container's bundle directory",
		},
		cli.StringFlag{
			Name:  "console-socket",
			Usage: "path to a unix socket representing the console",
		},
		cli.StringFlag{
			Name:  "pid-file",
			Usage: "path to a file to write the container's PID",
		},
	},
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, true); err != nil {
			return err
		}

		containerID := context.Args().First()
		bundle := context.String("bundle")

		factory, err := libcontainer.New(context.GlobalString("root"))
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

		if pidFile := context.String("pid-file"); pidFile != "" {
			state, err := container.State()
			if err != nil {
				return fmt.Errorf("failed to get container state: %w", err)
			}
			if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", state.Pid)), 0644); err != nil {
				return fmt.Errorf("failed to write PID file: %w", err)
			}
		}

		utils.Infof("Container %s started successfully", containerID)
		return nil
	},
}

var stateCommand = cli.Command{
	Name:  "state",
	Usage: "get the state of a container",
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, true); err != nil {
			return err
		}

		containerID := context.Args().First()

		factory, err := libcontainer.New(context.GlobalString("root"))
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
	},
}

/*
Start

start <container-id>

This operation MUST generate an error if it is not provided the container ID.
Attempting to start a container that is not created MUST have no effect on the container
and MUST generate an error. This operation MUST run the user-specified program as
specified by process. This operation MUST generate an error if process was not set.
*/
var startCommand = cli.Command{
	Name:        "start",
	Usage:       "executes the user defined process in a created container",
	ArgsUsage:   "start container",
	Description: `The start command executes the user defined process in a created container.`,
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 1, true); err != nil {
			return err
		}

		containerID := context.Args().First()

		factory, err := libcontainer.New(context.GlobalString("root"))
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

		// OCI spec: start must only work on containers in 'created' state
		switch status {
		case libcontainer.Created:
			if err := container.Start(); err != nil {
				return fmt.Errorf("failed to start container: %w", err)
			}
			utils.Infof("Container %s started successfully", containerID)

			return nil
		case libcontainer.Stopped:
			return fmt.Errorf("cannot start a container that has stopped")
		case libcontainer.Running:
			return fmt.Errorf("cannot start an already running container")
		default:
			return fmt.Errorf("cannot start a container in the %s state", status)
		}
	},
}

/*
Init

init <container-id> <path-to-bundle>

This is a special command that is used internally by the runtime.
It sets up the container environment including namespaces, mounts, and then
executes the container process. This command is not meant to be called directly
by users.
*/
var initCommand = cli.Command{
	Name:  "init",
	Usage: "initialize the container process",
	Action: func(context *cli.Context) error {
		fmt.Println("DEBUG: init command action started, args:", os.Args)

		if err := checkArgs(context, 2, true); err != nil {
			return err
		}

		containerID := context.Args()[0]
		bundle := context.Args()[1]

		fmt.Println("DEBUG: init - containerID=" + containerID + ", bundle=" + bundle)

		factory, err := libcontainer.New(context.GlobalString("root"))
		if err != nil {
			return fmt.Errorf("failed to create factory: %w", err)
		}

		container, err := factory.Load(containerID)
		if err != nil {
			return fmt.Errorf("failed to load container: %w", err)
		}

		// Load config to get process args
		configPath := filepath.Join(bundle, "config.json")
		config, err := config.Load(configPath)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		if config.Process == nil || len(config.Process.Args) == 0 {
			return fmt.Errorf("no process specified in config")
		}

		// Set up environment variables
		env := config.Process.Env
		if env == nil {
			env = os.Environ()
		}

		// In init mode - this is the re-executed process
		fmt.Fprintf(os.Stderr, "DEBUG: Init command called for container %s\n", containerID)
		fmt.Fprintf(os.Stderr, "DEBUG: Loading container from factory\n")

		// Call the container's init process method (this will exec the container process)
		fmt.Fprintf(os.Stderr, "DEBUG: About to call container.InitProcess()\n")
		if err := container.InitProcess(); err != nil {
			fmt.Fprintf(os.Stderr, "DEBUG: container.InitProcess() failed: %v\n", err)
			return fmt.Errorf("failed to start init process: %w", err)
		}

		// This should never be reached as the init process will exec
		return nil
	},
}

var killCommand = cli.Command{
	Name:  "kill",
	Usage: "kill sends the specified signal (default: SIGTERM) to the container's init process",
	ArgsUsage: `<container-id> [signal]

Where "<container-id>" is the name for the instance of the container and
"[signal]" is the signal to be sent to the init process.

EXAMPLE:
For example, if the container id is "ubuntu01" the following will send a "KILL"
signal to the init process of the "ubuntu01" container:

       # hackontainer kill ubuntu01 KILL`,
	Action: func(context *cli.Context) error {
		if context.NArg() < 1 || context.NArg() > 2 {
			return fmt.Errorf("need 1 or 2 arguments, got %d", context.NArg())
		}

		containerID := context.Args()[0]

		factory, err := libcontainer.New(context.GlobalString("root"))
		if err != nil {
			return fmt.Errorf("failed to create factory: %w", err)
		}

		container, err := factory.Load(containerID)
		if err != nil {
			return fmt.Errorf("failed to load container: %w", err)
		}

		sigstr := context.Args().Get(1)
		if sigstr == "" {
			sigstr = "SIGTERM"
		}

		sig, err := parseSignal(sigstr)
		if err != nil {
			return err
		}

		err = container.Signal(sig)
		if err != nil {
			return fmt.Errorf("failed to send signal: %w", err)
		}

		utils.Infof("Signal %s sent to container %s", sigstr, containerID)
		return nil
	},
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
