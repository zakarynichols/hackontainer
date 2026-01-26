package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli"
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
		stateCommand,
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
	Flags: []cli.Flag{},
	Action: func(context *cli.Context) error {
		if err := checkArgs(context, 2, true); err != nil {
			return err
		}

		// Add condition to prevent duplicate container-id.
		containerID := context.Args()[0] // Should create unique container ids.
		bundle := context.Args()[1]

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
