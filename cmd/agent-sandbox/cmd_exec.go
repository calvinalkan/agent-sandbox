package main

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"runtime"

	flag "github.com/spf13/pflag"
)

// Static errors for platform prerequisites.
var (
	// ErrNoCommand is returned when exec is called without a command.
	ErrNoCommand = errors.New("no command specified")
	// ErrNotLinux is returned when running on a non-Linux platform.
	ErrNotLinux = errors.New("agent-sandbox requires Linux")
	// ErrRunningAsRoot is returned when running as root user.
	ErrRunningAsRoot = errors.New("agent-sandbox cannot run as root")
	// ErrBwrapNotFound is returned when bwrap is not in PATH.
	ErrBwrapNotFound = errors.New("bwrap not found in PATH (try installing with: sudo apt install bubblewrap)")
)

// ExecCmd creates the exec command for running commands in the sandbox.
func ExecCmd(cfg *Config, env map[string]string) *Command {
	flags := flag.NewFlagSet("exec", flag.ContinueOnError)
	flags.SetInterspersed(false) // Stop parsing at command
	flags.BoolP("help", "h", false, "Show help")
	flags.Bool("network", true, "Enable network access")
	flags.Bool("docker", false, "Enable docker socket access")
	flags.Bool("dry-run", false, "Print bwrap command without executing")
	flags.Bool("debug", false, "Print sandbox startup details to stderr")
	flags.StringArray("ro", nil, "Add read-only path")
	flags.StringArray("rw", nil, "Add read-write path")
	flags.StringArray("exclude", nil, "Add excluded path")

	_ = cfg // Will be used when implementing sandbox
	_ = env // Will be used when implementing sandbox

	return &Command{
		Flags:   flags,
		Usage:   "exec [flags] <command> [args]",
		Short:   "Run command in sandbox",
		Long:    "Run a command inside the bubblewrap sandbox with configured filesystem access.",
		Aliases: []string{},
		Exec: func(_ context.Context, _ io.Reader, _, stderr io.Writer, args []string) error {
			if err := checkPlatformPrerequisites(); err != nil {
				return err
			}

			if len(args) == 0 {
				return ErrNoCommand
			}

			fprintln(stderr, "exec command not yet implemented")

			return nil
		},
	}
}

// checkPlatformPrerequisites validates the runtime environment.
func checkPlatformPrerequisites() error {
	if runtime.GOOS != "linux" {
		return ErrNotLinux
	}

	if os.Getuid() == 0 {
		return ErrRunningAsRoot
	}

	_, err := exec.LookPath("bwrap")
	if err != nil {
		return ErrBwrapNotFound
	}

	return nil
}
