package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	flag "github.com/spf13/pflag"
)

// Static errors for platform prerequisites.
var (
	// ErrNoCommand is returned when exec is called without a command.
	ErrNoCommand = errors.New("no command specified (usage: agent-sandbox <command> [args])")
	// ErrNotLinux is returned when running on a non-Linux platform.
	ErrNotLinux = errors.New("agent-sandbox requires Linux (bwrap uses Linux namespaces)")
	// ErrRunningAsRoot is returned when running as root user.
	ErrRunningAsRoot = errors.New("agent-sandbox cannot run as root (use a regular user account)")
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
		Exec: func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
			// Create debug logger (nil output means disabled)
			debugEnabled, _ := flags.GetBool("debug")

			var debug *DebugLogger
			if debugEnabled {
				debug = NewDebugLogger(stderr)
			} else {
				debug = NewDebugLogger(nil)
			}

			// 1. Platform validation
			err := checkPlatformPrerequisites()
			if err != nil {
				return err
			}

			// 2. Validate home directory early (before any path resolution)
			// This is required because @base and many presets reference home paths
			homeDir, err := GetHomeDir(env)
			if err != nil {
				return err
			}

			// 3. Validate command exists
			if len(args) == 0 {
				return ErrNoCommand
			}

			// 4. Output config loading debug info
			if cfg != nil {
				debugConfigLoading(debug, cfg)
			}

			// 5. Apply CLI flags to config (highest priority)
			if cfg != nil {
				err = applyExecFlags(cfg, flags)
				if err != nil {
					return err
				}

				// Output config merge debug info (after CLI flags applied)
				debugConfigMerge(debug, cfg, flags)
			}

			// 6. Get loaded config paths for @base preset protection
			loadedConfigPaths := getLoadedConfigPaths(cfg)

			// 7. Expand presets with context
			presetCtx := PresetContext{
				HomeDir:           homeDir,
				WorkDir:           cfg.EffectiveCwd,
				LoadedConfigPaths: loadedConfigPaths,
			}

			presetPaths, err := ExpandPresets(cfg.Filesystem.Presets, presetCtx)
			if err != nil {
				return err
			}

			// Debug: show preset expansion
			DebugPresetExpansion(debug, cfg.Filesystem.Presets, getAppliedPresets(cfg.Filesystem.Presets), getRemovedPresets(cfg.Filesystem.Presets))

			// 8. Get CLI filesystem paths (kept separate for source tracking)
			cliFilesystem := getCLIFilesystemPaths(flags)

			// 9. Resolve all paths from all layers with correct source tracking
			resolvedPaths, err := ResolvePaths(&ResolvePathsInput{
				Preset: PathLayerInput(presetPaths),
				Global: PathLayerInput{
					Ro:      cfg.GlobalFilesystem.Ro,
					Rw:      cfg.GlobalFilesystem.Rw,
					Exclude: cfg.GlobalFilesystem.Exclude,
				},
				Project: PathLayerInput{
					Ro:      cfg.ProjectFilesystem.Ro,
					Rw:      cfg.ProjectFilesystem.Rw,
					Exclude: cfg.ProjectFilesystem.Exclude,
				},
				CLI: PathLayerInput{
					Ro:      cliFilesystem.Ro,
					Rw:      cliFilesystem.Rw,
					Exclude: cliFilesystem.Exclude,
				},
				HomeDir: homeDir,
				WorkDir: cfg.EffectiveCwd,
			})
			if err != nil {
				return err
			}

			// Debug: show path resolution results
			DebugPathResolution(debug, resolvedPaths)

			// 10. Validate working directory is not excluded
			err = ValidateWorkDirNotExcluded(resolvedPaths, cfg.EffectiveCwd)
			if err != nil {
				return err
			}

			// 11. Apply specificity rules and sort by mount order
			sortedPaths := ResolveAndSort(resolvedPaths)

			// 12. Set up temp directory for exclude mounts and wrappers
			tempRes, err := SetupTempDir()
			if err != nil {
				return err
			}
			defer tempRes.Cleanup()

			// 13. Generate bwrap arguments for filesystem mounts (including excludes)
			bwrapArgs, err := BwrapArgs(sortedPaths, cfg, tempRes.EmptyFile, env)
			if err != nil {
				return err
			}

			// 14. Validate command wrapper rules
			err = validateCommandRules(cfg.Commands)
			if err != nil {
				return err
			}

			// 15. Set up command wrappers
			// Debug: show command wrapper configuration
			DebugCommandWrappers(debug, cfg.Commands)

			// Find binary locations for wrapped commands
			binPaths := make(map[string][]BinaryPath)

			for cmdName := range cfg.Commands {
				// Find binaries in PATH
				paths := BinaryLocations(cmdName, env)
				// Also check known non-PATH locations (e.g., /usr/lib/git-core/git)
				paths = append(paths, AdditionalBinaryPaths(cmdName)...)
				binPaths[cmdName] = paths
			}

			// Generate random runtime base path for sandbox
			runtimeBase := "/run/" + randomString8() + "/agent-sandbox"
			sandboxWrapBinaryPath := filepath.Join(runtimeBase, "binaries", "wrap-binary")

			// Generate wrapper scripts
			wrapperSetup := GenerateWrappers(cfg.Commands, binPaths, sandboxWrapBinaryPath)

			// Note: No cleanup needed - wrappers are injected via FDs (--ro-bind-data)

			// 16. Get self binary path for mounting into sandbox
			selfBinary, err := getSelfBinaryPath()
			if err != nil {
				return err
			}

			// 17. Add wrapper mounts to bwrap args
			bwrapArgs = AddWrapperMounts(bwrapArgs, wrapperSetup, selfBinary, runtimeBase)

			// Debug: show final bwrap arguments
			DebugBwrapArgs(debug, bwrapArgs)

			// 18. Check for dry-run mode
			dryRun, _ := flags.GetBool("dry-run")
			if dryRun {
				printDryRunOutput(stdout, bwrapArgs, args)

				return nil
			}

			// 19. Execute the command in the sandbox
			exitCode, err := ExecuteSandbox(ctx, bwrapArgs, args, env, wrapperSetup, stdin, stdout, stderr)
			if err != nil {
				return err
			}

			return NewExitCodeError(exitCode)
		},
	}
}

// applyExecFlags applies CLI flag overrides to the config.
// Only flags that were explicitly set override config values.
// Note: Path flags (--ro, --rw, --exclude) are NOT applied here - they are
// retrieved separately via getCLIFilesystemPaths to preserve source tracking.
func applyExecFlags(cfg *Config, flags *flag.FlagSet) error {
	if flags.Changed("network") {
		val, _ := flags.GetBool("network")
		cfg.Network = &val
	}

	if flags.Changed("docker") {
		val, _ := flags.GetBool("docker")
		cfg.Docker = &val
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

// getCLIFilesystemPaths extracts filesystem path flags from CLI.
// These are kept separate from config paths to preserve source tracking for debug output.
func getCLIFilesystemPaths(flags *flag.FlagSet) FilesystemConfig {
	var result FilesystemConfig

	if flags.Changed("ro") {
		result.Ro, _ = flags.GetStringArray("ro")
	}

	if flags.Changed("rw") {
		result.Rw, _ = flags.GetStringArray("rw")
	}

	if flags.Changed("exclude") {
		result.Exclude, _ = flags.GetStringArray("exclude")
	}

	return result
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

// ErrInvalidCommandPreset is returned when a command preset is used for the wrong command.
var ErrInvalidCommandPreset = errors.New("command preset can only be used for its matching command")

// validateCommandRules checks that command presets are used correctly.
// For example, @git can only be used with the "git" command.
func validateCommandRules(commands map[string]CommandRule) error {
	for cmdName, rule := range commands {
		if rule.Kind != CommandRulePreset {
			continue
		}

		// Extract the command name from the preset (e.g., "@git" -> "git")
		presetCmd := strings.TrimPrefix(rule.Value, "@")

		if cmdName != presetCmd {
			return fmt.Errorf("%w: %s preset can only be used for '%s' command, not '%s'",
				ErrInvalidCommandPreset, rule.Value, presetCmd, cmdName)
		}
	}

	return nil
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

// getLoadedConfigPaths returns the paths of all loaded config files.
// This is used to protect config files from modification inside the sandbox.
func getLoadedConfigPaths(cfg *Config) []string {
	if cfg == nil || cfg.LoadedConfigFiles == nil {
		return nil
	}

	paths := make([]string, 0, len(cfg.LoadedConfigFiles))
	for _, path := range cfg.LoadedConfigFiles {
		paths = append(paths, path)
	}

	return paths
}

// getAppliedPresets returns the list of presets that were applied (not negated).
func getAppliedPresets(presets []string) []string {
	// Always start with @all as the implicit default
	applied := []string{"@all"}
	seen := map[string]bool{"@all": true}

	for _, preset := range presets {
		if strings.HasPrefix(preset, "!") {
			continue // Skip negated presets
		}

		if !seen[preset] {
			seen[preset] = true
			applied = append(applied, preset)
		}
	}

	return applied
}

// getRemovedPresets returns the list of presets that were removed via negation.
func getRemovedPresets(presets []string) []string {
	var removed []string

	for _, preset := range presets {
		if after, ok := strings.CutPrefix(preset, "!"); ok {
			removed = append(removed, after)
		}
	}

	return removed
}

// randomString8 generates a random 8-byte (16 hex character) string.
// Used to create unique runtime paths for sandbox isolation.
func randomString8() string {
	bytes := make([]byte, 8)

	_, err := rand.Read(bytes)
	if err != nil {
		// Fall back to a simple timestamp-based string if crypto/rand fails
		return fmt.Sprintf("%x", os.Getpid())
	}

	return hex.EncodeToString(bytes)
}

// getSelfBinaryPath returns the absolute path to the agent-sandbox binary.
// It resolves symlinks to get the real binary path.
func getSelfBinaryPath() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrSelfBinaryNotFound, err)
	}

	// Resolve any symlinks to get the real binary path
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("%w: cannot resolve symlinks: %w", ErrSelfBinaryNotFound, err)
	}

	return self, nil
}
