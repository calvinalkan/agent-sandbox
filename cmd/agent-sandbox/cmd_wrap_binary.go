package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	flag "github.com/spf13/pflag"
)

// PresetGit is the name of the built-in git command preset.
const PresetGit = "@git"

// Static errors for wrap-binary command.
var (
	// ErrNotInSandbox is returned when wrap-binary is called outside a sandbox.
	ErrNotInSandbox = errors.New("wrap-binary can only run inside sandbox (this is an internal command)")
	// ErrWrapBinaryNoCommand is returned when wrap-binary is called without a command.
	ErrWrapBinaryNoCommand = errors.New("wrap-binary requires command name")
	// ErrWrapBinaryMissingFlag is returned when neither --preset nor --script is provided.
	ErrWrapBinaryMissingFlag = errors.New("wrap-binary requires --preset or --script flag")
	// ErrWrapBinaryBothFlags is returned when both --preset and --script are provided.
	ErrWrapBinaryBothFlags = errors.New("wrap-binary accepts only one of --preset or --script")
	// ErrRealBinaryNotFound is returned when the real binary cannot be found.
	ErrRealBinaryNotFound = errors.New("real binary not found")
	// ErrUnknownCommandPreset is returned when an unknown command preset is specified.
	ErrUnknownCommandPreset = errors.New("unknown command preset (available: @git)")
)

// WrapBinaryCmd creates the hidden wrap-binary command.
// This command is internal and only works inside a sandbox.
func WrapBinaryCmd() *Command {
	flags := flag.NewFlagSet("wrap-binary", flag.ContinueOnError)
	flags.SetInterspersed(false) // Stop parsing at command name
	flags.BoolP("help", "h", false, "Show help")
	flags.String("preset", "", "Use built-in preset (e.g., @git)")
	flags.String("script", "", "Use custom wrapper script path")

	return &Command{
		Flags: flags,
		Usage: "wrap-binary [--preset <@name> | --script <path>] <cmd> [args...]",
		Short: "Internal wrapper command (sandbox only)",
		Long:  "Execute a wrapped command inside the sandbox. This is an internal command used by wrapper scripts.",
		Exec: func(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) error {
			// Must be inside sandbox
			if !isInsideSandbox() {
				return ErrNotInSandbox
			}

			// Parse flags
			preset, _ := flags.GetString("preset")
			script, _ := flags.GetString("script")

			// Validate flags: exactly one of --preset or --script must be set
			if preset == "" && script == "" {
				return ErrWrapBinaryMissingFlag
			}

			if preset != "" && script != "" {
				return ErrWrapBinaryBothFlags
			}

			// Remaining args should be: <cmdName> [args...]
			if len(args) == 0 {
				return ErrWrapBinaryNoCommand
			}

			cmdName := args[0]
			cmdArgs := args[1:]

			// Locate real binary via the runtime directory convention
			realBinary, err := findRealBinary(cmdName)
			if err != nil {
				return err
			}

			if preset != "" {
				return execPreset(ctx, preset, cmdName, cmdArgs, realBinary, stdin, stdout, stderr)
			}

			// Custom script: set env var for user's script and exec it
			return execCustomScript(ctx, script, cmdName, cmdArgs, realBinary, stdin, stdout, stderr)
		},
	}
}

// findRealBinary locates the real binary using the path convention.
// The real binary is at ../real/<cmdName> relative to the current executable.
//
// This works because wrapper scripts exec the agent-sandbox binary at a known path
// inside the sandbox (e.g., /run/<random>/agent-sandbox/binaries/wrap-binary),
// and the real binaries are mounted at ../real/<cmdName>.
func findRealBinary(cmdName string) (string, error) {
	// Get our own executable path
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("%w: cannot determine executable path: %w", ErrRealBinaryNotFound, err)
	}

	// Resolve symlinks to get the real path
	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("%w: cannot resolve executable path: %w", ErrRealBinaryNotFound, err)
	}

	// Real binary is at ../real/<cmdName> relative to our location
	selfDir := filepath.Dir(self)
	realBinary := filepath.Join(selfDir, "..", "real", cmdName)

	// Clean the path
	realBinary = filepath.Clean(realBinary)

	// Verify it exists
	info, err := os.Stat(realBinary)
	if err != nil {
		return "", fmt.Errorf("%w: %s does not exist", ErrRealBinaryNotFound, realBinary)
	}

	// Verify it's executable
	if info.IsDir() {
		return "", fmt.Errorf("%w: %s is a directory", ErrRealBinaryNotFound, realBinary)
	}

	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("%w: %s is not executable", ErrRealBinaryNotFound, realBinary)
	}

	return realBinary, nil
}

// agentSandboxEnvVarName returns the environment variable name for a command.
// The name is AGENT_SANDBOX_<CMD> where CMD is the uppercase command name.
func agentSandboxEnvVarName(cmdName string) string {
	return "AGENT_SANDBOX_" + strings.ToUpper(cmdName)
}

// execPreset executes a command using a built-in preset.
// The preset applies rules to filter/block certain operations before executing.
func execPreset(_ context.Context, preset, cmdName string, cmdArgs []string, realBinary string, stdin io.Reader, stdout, stderr io.Writer) error {
	// Validate preset exists
	switch preset {
	case PresetGit:
		return execGitPreset(cmdName, cmdArgs, realBinary, stdin, stdout, stderr)
	default:
		return fmt.Errorf("%w: %s", ErrUnknownCommandPreset, preset)
	}
}

// execGitPreset implements the @git command preset.
// It blocks dangerous git operations per SPEC.md.
func execGitPreset(_ string, cmdArgs []string, realBinary string, stdin io.Reader, stdout, stderr io.Writer) error {
	// Parse git arguments to find the subcommand
	subcommand, subcommandArgs := parseGitArgs(cmdArgs)

	// Check if the operation is blocked
	if blocked, reason := isGitOperationBlocked(subcommand, subcommandArgs); blocked {
		fprintln(stderr, "error: "+reason)

		return ErrSilentExit
	}

	// Execute the real git binary
	return execBinary(realBinary, cmdArgs, nil, stdin, stdout, stderr)
}

// parseGitArgs parses git arguments to extract the subcommand.
// Git allows global flags before the subcommand (e.g., git -C /path push).
// Returns the subcommand and the remaining args after the subcommand.
func parseGitArgs(args []string) (string, []string) {
	// Git global flags that take a value
	flagsWithValue := map[string]bool{
		"-C":                  true,
		"-c":                  true,
		"--git-dir":           true,
		"--work-tree":         true,
		"--namespace":         true,
		"--super-prefix":      true,
		"--config-env":        true,
		"--exec-path":         true,
		"--html-path":         true,
		"--man-path":          true,
		"--info-path":         true,
		"--list-cmds":         true,
		"--attr-source":       true,
		"-p":                  true,
		"--paginate":          false, // no value
		"-P":                  false, // no value
		"--no-pager":          false,
		"--no-replace-obj":    false,
		"--bare":              false,
		"--literal-pathspecs": false,
		"--glob-pathspecs":    false,
		"--noglob-pathspecs":  false,
		"--icase-pathspecs":   false,
		"--no-optional-locks": false,
	}

	i := 0
	for i < len(args) {
		arg := args[i]

		// Not a flag - this is the subcommand
		if !strings.HasPrefix(arg, "-") {
			return arg, args[i+1:]
		}

		// Check if this flag takes a value
		// Handle --flag=value form
		if strings.Contains(arg, "=") {
			flagName := strings.SplitN(arg, "=", 2)[0]
			if _, known := flagsWithValue[flagName]; known || strings.HasPrefix(flagName, "--") {
				i++

				continue
			}
		}

		// Check if this flag takes a separate value argument
		if takesValue, known := flagsWithValue[arg]; known && takesValue {
			i += 2 // Skip flag and its value

			continue
		}

		// Flag without value, skip it
		i++
	}

	// No subcommand found
	return "", nil
}

// isGitOperationBlocked checks if a git operation is blocked.
// Returns true with a reason message if blocked.
func isGitOperationBlocked(subcommand string, args []string) (bool, string) {
	switch subcommand {
	case "checkout":
		return true, "git checkout blocked: can discard uncommitted changes. Use 'git switch' for branches."

	case "restore":
		return true, "git restore blocked: discards uncommitted changes. Commit or stash first."

	case "reset":
		// Only block --hard
		if hasFlag(args, "--hard") {
			return true, "git reset --hard blocked: discards commits and changes. Use 'git reset --soft' or 'git revert'."
		}

	case "clean":
		// Only block -f/--force
		if hasFlag(args, "-f", "--force") {
			return true, "git clean -f blocked: deletes untracked files. Review manually."
		}

	case "commit":
		// Block --no-verify
		if hasFlag(args, "--no-verify", "-n") {
			return true, "git commit --no-verify blocked: bypasses safety hooks. Fix the hook issues."
		}

	case "stash":
		// Check stash subcommand
		if len(args) > 0 {
			switch args[0] {
			case "drop":
				return true, "git stash drop blocked: permanently deletes stash. Export important stashes first."
			case "clear":
				return true, "git stash clear blocked: deletes all stashes. Export important stashes first."
			case "pop":
				return true, "git stash pop blocked: can cause merge conflicts that lose stash. Use 'git stash apply'."
			}
		}

	case "branch":
		// Block -D (force delete)
		if hasFlag(args, "-D") {
			return true, "git branch -D blocked: force deletes unmerged branch. Use 'git branch -d' (safe delete)."
		}

	case "push":
		// Block --force but allow --force-with-lease
		if hasFlag(args, "--force", "-f") && !hasFlag(args, "--force-with-lease") {
			return true, "git push --force blocked: rewrites remote history. Use 'git push --force-with-lease'."
		}
	}

	return false, ""
}

// hasFlag checks if any of the given flags are present in args.
func hasFlag(args []string, flags ...string) bool {
	flagSet := make(map[string]bool, len(flags))
	for _, f := range flags {
		flagSet[f] = true
	}

	for _, arg := range args {
		// Handle --flag=value form
		if idx := strings.Index(arg, "="); idx > 0 {
			arg = arg[:idx]
		}

		if flagSet[arg] {
			return true
		}
	}

	return false
}

// execCustomScript executes a user's custom wrapper script.
// It sets the AGENT_SANDBOX_<CMD> environment variable pointing to the real binary,
// then execs the user's script.
func execCustomScript(_ context.Context, scriptPath, cmdName string, cmdArgs []string, realBinary string, stdin io.Reader, stdout, stderr io.Writer) error {
	// Build environment with the real binary path
	env := []string{
		agentSandboxEnvVarName(cmdName) + "=" + realBinary,
	}

	// Execute the user's script with the original command arguments
	return execBinary(scriptPath, cmdArgs, env, stdin, stdout, stderr)
}

// execBinary executes a binary with the given arguments and additional environment.
// If additionalEnv is provided, those variables are added to the current environment.
// The binary's stdin, stdout, and stderr are connected to the current process.
// Returns an ExitCodeError if the command exits with non-zero status.
func execBinary(binary string, args []string, additionalEnv []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command(binary, args...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	// Build environment: inherit current + add extras
	cmd.Env = os.Environ()
	cmd.Env = append(cmd.Env, additionalEnv...)

	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return NewExitCodeError(exitErr.ExitCode())
		}

		return fmt.Errorf("exec %s: %w", binary, err)
	}

	return nil
}
