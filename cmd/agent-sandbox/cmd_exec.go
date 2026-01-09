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
	// ErrHomeNotFound is returned when the home directory cannot be determined.
	ErrHomeNotFound = errors.New("cannot determine home directory")
	// ErrHomeNotDir is returned when HOME points to a file instead of a directory.
	ErrHomeNotDir = errors.New("home directory is not a directory")
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

	return &Command{
		Flags:   flags,
		Usage:   "exec [flags] <command> [args]",
		Short:   "Run command in sandbox",
		Long:    "Run a command inside the bubblewrap sandbox with configured filesystem access.",
		Aliases: []string{},
		Exec: func(_ context.Context, _ io.Reader, stdout, stderr io.Writer, args []string) error {
			err := checkPlatformPrerequisites()
			if err != nil {
				return err
			}

			// Validate home directory early (before any path resolution)
			// This is required because @base and many presets reference home paths
			homeDir, err := GetHomeDir(env)
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

			// Expand presets with context
			presetCtx := PresetContext{
				HomeDir: homeDir,
				WorkDir: cfg.EffectiveCwd,
				// LoadedConfigPaths: would be set by config loading in full implementation
			}

			presetPaths, err := ExpandPresets(cfg.Filesystem.Presets, presetCtx)
			if err != nil {
				return err
			}

			// Resolve all paths
			resolvedPaths, err := ResolvePaths(&ResolvePathsInput{
				Preset:  PathLayerInput(presetPaths),
				Global:  PathLayerInput{}, // global config paths handled by config loading
				Project: PathLayerInput{}, // project config paths handled by config loading
				CLI: PathLayerInput{
					Ro:      cfg.Filesystem.Ro,
					Rw:      cfg.Filesystem.Rw,
					Exclude: cfg.Filesystem.Exclude,
				},
				HomeDir: homeDir,
				WorkDir: cfg.EffectiveCwd,
			})
			if err != nil {
				return err
			}

			// Validate working directory is not excluded
			err = ValidateWorkDirNotExcluded(resolvedPaths, cfg.EffectiveCwd)
			if err != nil {
				return err
			}

			// Apply specificity rules and sort by mount order
			sortedPaths := ResolveAndSort(resolvedPaths)

			// Generate bwrap arguments
			bwrapArgs, err := BwrapArgs(sortedPaths, cfg)
			if err != nil {
				return err
			}

			// Check for dry-run mode
			dryRun, _ := flags.GetBool("dry-run")
			if dryRun {
				printDryRunOutput(stdout, bwrapArgs, args)

				return nil
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

// GetHomeDir returns the home directory, validating that it exists and is a directory.
// It first checks the env map (respects container overrides), then falls back to os.UserHomeDir().
func GetHomeDir(env map[string]string) (string, error) {
	// Try env first (respect container overrides)
	if home := env["HOME"]; home != "" {
		info, err := os.Stat(home)
		if err != nil {
			return "", fmt.Errorf("%w: %s (from $HOME) does not exist: %w", ErrHomeNotFound, home, err)
		}

		if !info.IsDir() {
			return "", fmt.Errorf("%w: %s (from $HOME)", ErrHomeNotDir, home)
		}

		return home, nil
	}

	// Fall back to os.UserHomeDir()
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("%w: %w (set $HOME environment variable)", ErrHomeNotFound, err)
	}

	// Verify the fallback home exists and is a directory
	info, err := os.Stat(home)
	if err != nil {
		return "", fmt.Errorf("%w: %s does not exist: %w", ErrHomeNotFound, home, err)
	}

	if !info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrHomeNotDir, home)
	}

	return home, nil
}

// printDryRunOutput formats and prints the bwrap command for dry-run mode.
// The output is shell-compatible and can be copy-pasted to run manually.
func printDryRunOutput(output io.Writer, bwrapArgs []string, command []string) {
	// Print bwrap with arguments using line continuation for readability
	fprintf(output, "bwrap \\\n")

	for _, arg := range bwrapArgs {
		fprintf(output, "  %s \\\n", shellQuoteIfNeeded(arg))
	}

	// Print command separator and user command
	fprintf(output, "  --")

	for _, arg := range command {
		fprintf(output, " %s", shellQuoteIfNeeded(arg))
	}

	fprintln(output)
}

// shellQuoteIfNeeded returns the string quoted if it contains special characters,
// otherwise returns it unchanged. This makes the output shell-safe.
func shellQuoteIfNeeded(str string) string {
	// Check if the string needs quoting
	for _, c := range str {
		if !isShellSafeChar(c) {
			// Use single quotes for safety, escaping any existing single quotes
			escaped := strings.ReplaceAll(str, "'", "'\"'\"'")

			return "'" + escaped + "'"
		}
	}

	return str
}

// isShellSafeChar returns true if the character doesn't need quoting in shell.
func isShellSafeChar(c rune) bool {
	// Safe characters: alphanumeric, dash, underscore, dot, forward slash, colon, equals
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '-' || c == '_' || c == '.' || c == '/' || c == ':' || c == '='
}
