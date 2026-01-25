package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

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
			Name:  "console-socket",
			Usage: "path to a unix socket representing the console",
		},
		cli.BoolFlag{
			Name:  "pid-file",
			Usage: "path to a file to write the container's PID",
		},
		cli.BoolFlag{
			Name:  "no-user-ns",
			Usage: "disable user namespaces (for testing)",
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

		if pidFile := context.String("pid-file"); pidFile != "" {
			state, err := container.State()
			if err != nil {
				return fmt.Errorf("failed to get container state: %w", err)
			}
			if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", state.InitProcessPid)), 0644); err != nil {
				return fmt.Errorf("failed to write PID file: %w", err)
			}
		}

		utils.Infof("Container %s created successfully", containerID)
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
			if err := os.WriteFile(pidFile, []byte(fmt.Sprintf("%d", state.InitProcessPid)), 0644); err != nil {
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

		output := map[string]interface{}{
			"ociVersion":  "1.3.0",
			"id":          state.ID,
			"pid":         state.InitProcessPid,
			"status":      string(status),
			"bundle":      state.Bundle,
			"created":     state.Created.Format(time.RFC3339Nano),
			"annotations": state.Annotations,
		}

		if !state.Started.IsZero() {
			output["started"] = state.Started.Format(time.RFC3339Nano)
		}

		if !state.Finished.IsZero() {
			output["finished"] = state.Finished.Format(time.RFC3339Nano)
		}

		if state.ExitStatus != 0 {
			output["exitStatus"] = state.ExitStatus
		}

		json.NewEncoder(os.Stdout).Encode(output)
		return nil
	},
}
