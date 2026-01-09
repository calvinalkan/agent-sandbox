package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"

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
	// ErrInvalidCmdFlag is returned when a --cmd flag value is malformed.
	ErrInvalidCmdFlag = errors.New("invalid --cmd format: expected KEY=VALUE")
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
	flags.StringArray("cmd", nil, "Command wrapper override (KEY=VALUE, repeatable)")

	_ = env // Will be used when implementing sandbox

	return &Command{
		Flags:   flags,
		Usage:   "exec [flags] <command> [args]",
		Short:   "Run command in sandbox",
		Long:    "Run a command inside the bubblewrap sandbox with configured filesystem access.",
		Aliases: []string{},
		Exec: func(_ context.Context, _ io.Reader, _, stderr io.Writer, args []string) error {
			err := checkPlatformPrerequisites()
			if err != nil {
				return err
			}

			if len(args) == 0 {
				return ErrNoCommand
			}

			// Apply CLI flags to config (highest priority)
			if cfg != nil {
				err = applyExecFlags(cfg, flags)
				if err != nil {
					return err
				}
			}

			fprintln(stderr, "exec command not yet implemented")

			return nil
		},
	}
}

// applyExecFlags applies CLI flag overrides to the config.
// Only flags that were explicitly set override config values.
func applyExecFlags(cfg *Config, flags *flag.FlagSet) error {
	if flags.Changed("network") {
		val, _ := flags.GetBool("network")
		cfg.Network = &val
	}

	if flags.Changed("docker") {
		val, _ := flags.GetBool("docker")
		cfg.Docker = &val
	}

	// Append CLI paths to config paths
	if flags.Changed("ro") {
		vals, _ := flags.GetStringArray("ro")
		cfg.Filesystem.Ro = append(cfg.Filesystem.Ro, vals...)
	}

	if flags.Changed("rw") {
		vals, _ := flags.GetStringArray("rw")
		cfg.Filesystem.Rw = append(cfg.Filesystem.Rw, vals...)
	}

	if flags.Changed("exclude") {
		vals, _ := flags.GetStringArray("exclude")
		cfg.Filesystem.Exclude = append(cfg.Filesystem.Exclude, vals...)
	}

	// Parse and apply --cmd flags
	if flags.Changed("cmd") {
		vals, _ := flags.GetStringArray("cmd")

		err := applyCmdFlags(cfg, vals)
		if err != nil {
			return err
		}
	}

	return nil
}

// applyCmdFlags parses and applies --cmd KEY=VALUE flags to the config.
// Supports repeated flags and comma-separated values within a single flag.
func applyCmdFlags(cfg *Config, vals []string) error {
	if cfg.Commands == nil {
		cfg.Commands = make(map[string]CommandRule)
	}

	for _, v := range vals {
		// Handle comma-separated values: --cmd git=true,rm=false
		pairs := strings.SplitSeq(v, ",")

		for pair := range pairs {
			key, value, ok := strings.Cut(pair, "=")
			if !ok {
				return fmt.Errorf("%w: %q", ErrInvalidCmdFlag, pair)
			}

			key = strings.TrimSpace(key)
			value = strings.TrimSpace(value)

			if key == "" {
				return fmt.Errorf("%w: empty key in %q", ErrInvalidCmdFlag, pair)
			}

			cfg.Commands[key] = parseCmdValue(value)
		}
	}

	return nil
}

// parseCmdValue parses a command wrapper value string into a CommandRule.
// Accepts: "true", "false", "@preset", or a script path.
func parseCmdValue(value string) CommandRule {
	switch value {
	case "true":
		return CommandRule{Kind: CommandRuleRaw}
	case "false":
		return CommandRule{Kind: CommandRuleBlock}
	default:
		if strings.HasPrefix(value, "@") {
			return CommandRule{Kind: CommandRulePreset, Value: value}
		}

		return CommandRule{Kind: CommandRuleScript, Value: value}
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
