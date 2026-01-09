package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	flag "github.com/spf13/pflag"
)

// Run is the main entry point. Returns exit code.
// sigCh can be nil if signal handling is not needed (e.g., in tests).
func Run(stdin io.Reader, stdout, stderr io.Writer, args []string, env map[string]string, sigCh <-chan os.Signal) int {
	// Find the first non-flag argument and check if it's a command
	// If not, insert "exec" to make implicit exec work with flags like --network
	args = insertExecIfNeeded(args)

	// Create fresh global flags for this invocation
	globalFlags := flag.NewFlagSet("agent-sandbox", flag.ContinueOnError)
	globalFlags.SetInterspersed(false)
	globalFlags.Usage = func() {}
	globalFlags.SetOutput(&strings.Builder{})

	flagHelp := globalFlags.BoolP("help", "h", false, "Show help")
	flagVersion := globalFlags.BoolP("version", "v", false, "Show version and exit")
	flagCwd := globalFlags.StringP("cwd", "C", "", "Run as if started in `dir`")
	flagConfig := globalFlags.String("config", "", "Use specified config `file`")

	err := globalFlags.Parse(args[1:])
	if err != nil {
		fprintError(stderr, err)
		fprintln(stderr)
		printGlobalOptions(stderr)

		return 1
	}

	// Handle --version early, before loading config
	if *flagVersion {
		if commit == "none" && date == "unknown" {
			fprintf(stdout, "agent-sandbox %s (built from source)\n", version)
		} else {
			fprintf(stdout, "agent-sandbox %s (%s, %s)\n", version, commit, date)
		}

		return 0
	}

	commandAndArgs := globalFlags.Args()

	// Show help: explicit --help or bare `agent-sandbox` with no args
	// Do this BEFORE loading config so help always works (per spec)
	if *flagHelp || len(commandAndArgs) == 0 {
		// Create minimal command list for help display (no config needed)
		commands := []*Command{
			ExecCmd(nil, nil),
			CheckCmd(),
		}

		printUsage(stdout, commands)

		return 0
	}

	// Create context early so config loading can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Create force-kill channel for signal handling
	// This is closed when a second signal or timeout requires immediate termination
	forceKillCh := make(chan struct{})
	ctx = WithForceKillCh(ctx, forceKillCh)

	// Determine if we need to load config (exec needs it, check and wrap-binary don't)
	cmdName := commandAndArgs[0]

	var cfg Config
	if cmdName == "check" || cmdName == "wrap-binary" {
		cfg = DefaultConfig()
	} else {
		// Load config for exec command
		cfg, err = LoadConfig(LoadConfigInput{
			WorkDirOverride: *flagCwd,
			ConfigPath:      *flagConfig,
			Env:             env,
		})
		if err != nil {
			fprintError(stderr, err)

			return 1
		}
	}

	// Create all commands (visible in help)
	commands := []*Command{
		ExecCmd(&cfg, env),
		CheckCmd(),
	}

	// Hidden commands (not shown in help, but still dispatchable)
	hiddenCommands := []*Command{
		WrapBinaryCmd(),
	}

	commandMap := make(map[string]*Command, len(commands)*2+len(hiddenCommands))
	for _, cmd := range commands {
		commandMap[cmd.Name()] = cmd
		for _, alias := range cmd.Aliases {
			commandMap[alias] = cmd
		}
	}

	for _, cmd := range hiddenCommands {
		commandMap[cmd.Name()] = cmd
	}

	// Dispatch to command
	cmd, ok := commandMap[cmdName]
	if !ok {
		// No command found - treat as implicit "exec"
		cmd = commandMap["exec"]
		// Don't consume cmdName, pass all args to exec
	} else {
		commandAndArgs = commandAndArgs[1:]
	}

	// Run command in goroutine so we can handle signals
	done := make(chan int, 1)

	go func() {
		done <- cmd.Run(ctx, stdin, stdout, stderr, commandAndArgs)
	}()

	// Handle nil sigCh for tests
	if sigCh == nil {
		return <-done
	}

	// Wait for completion or first signal
	select {
	case exitCode := <-done:
		return exitCode
	case <-sigCh:
		fprintln(stderr, "Interrupted, waiting up to 10s for cleanup... (Ctrl+C again to force exit)")
		cancel() // This triggers SIGTERM to the sandboxed process
	}

	// Wait for completion, timeout, or second signal
	select {
	case <-done:
		fprintln(stderr, "Cleanup complete.")

		return 130
	case <-time.After(10 * time.Second):
		fprintln(stderr, "Cleanup timed out, forced exit.")
		close(forceKillCh) // Trigger SIGKILL
		<-done             // Wait for actual termination

		return 130
	case <-sigCh:
		fprintln(stderr, "Forced exit.")
		close(forceKillCh) // Trigger SIGKILL
		<-done             // Wait for actual termination

		return 130
	}
}

func fprintln(output io.Writer, a ...any) {
	_, _ = fmt.Fprintln(output, a...)
}

func fprintf(output io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(output, format, a...)
}

// ANSI color codes for terminal output.
const (
	colorRed   = "\033[31m"
	colorReset = "\033[0m"
)

// fprintError prints an error message with optional red coloring for TTY.
func fprintError(output io.Writer, err error) {
	if IsTerminal() {
		fprintln(output, colorRed+"error:"+colorReset, err)
	} else {
		fprintln(output, "error:", err)
	}
}

const globalOptionsHelp = `  -h, --help             Show help
  -v, --version          Show version and exit
  -C, --cwd <dir>        Run as if started in <dir>
      --config <file>    Use specified config file`

func printGlobalOptions(output io.Writer) {
	fprintln(output, "Usage: agent-sandbox [flags] <command> [args]")
	fprintln(output)
	fprintln(output, "Global flags:")
	fprintln(output, globalOptionsHelp)
	fprintln(output)
	fprintln(output, "Run 'agent-sandbox --help' for a list of commands.")
}

func printUsage(output io.Writer, commands []*Command) {
	fprintln(output, "agent-sandbox - filesystem sandbox for agentic coding workflows")
	fprintln(output)
	fprintln(output, "Usage: agent-sandbox [flags] <command> [args]")
	fprintln(output)
	fprintln(output, "Flags:")
	fprintln(output, globalOptionsHelp)
	fprintln(output)
	fprintln(output, "Commands:")

	for _, cmd := range commands {
		fprintln(output, cmd.HelpLine())
	}

	fprintln(output)
	fprintln(output, "Run 'agent-sandbox <command> --help' for more information on a command.")
}

// isTerminal is a function variable that returns true if stdin is a terminal.
// It can be overridden in tests to control TTY behavior.
var isTerminal = func() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return (stat.Mode() & os.ModeCharDevice) != 0
}

// IsTerminal returns true if stdin is a terminal.
func IsTerminal() bool {
	return isTerminal()
}

// execFlags are flags that belong to the exec command, not global.
var execFlags = map[string]bool{
	"network": true, "docker": true, "dry-run": true, "debug": true,
	"ro": true, "rw": true, "exclude": true, "cmd": true,
}

// insertExecIfNeeded scans args to find where to insert "exec" for implicit exec mode.
// It inserts "exec" before the first exec flag or non-flag command argument.
// Examples:
//   - agent-sandbox echo hello → agent-sandbox exec echo hello
//   - agent-sandbox --network=false echo hello → agent-sandbox exec --network=false echo hello
//   - agent-sandbox --cwd /foo echo hello → agent-sandbox --cwd /foo exec echo hello
func insertExecIfNeeded(args []string) []string {
	if len(args) < 2 {
		return args
	}

	// Find position where we should insert "exec"
	// This is either:
	// 1. Before the first exec flag (like --network)
	// 2. Before the first non-flag that's not a command and not a flag value
	insertPos := -1

	for i := 1; i < len(args); i++ {
		arg := args[i]

		if strings.HasPrefix(arg, "-") {
			// It's a flag - check if it's an exec flag
			if isExecFlag(arg) {
				insertPos = i

				break
			}
			// It's a global flag, continue
			continue
		}

		// Non-flag argument
		// Check if previous arg was a flag that takes a value
		if i > 1 && needsValue(args[i-1]) {
			continue // This is a flag value, keep looking
		}

		// Found a command or argument
		if arg == "check" || arg == "exec" || arg == "wrap-binary" {
			return args // Already has explicit command
		}

		insertPos = i

		break
	}

	if insertPos == -1 {
		// No insertion point found
		return args
	}

	// Insert "exec" at the found position
	result := make([]string, 0, len(args)+1)
	result = append(result, args[:insertPos]...)
	result = append(result, "exec")
	result = append(result, args[insertPos:]...)

	return result
}

// isExecFlag checks if a flag string is an exec command flag.
func isExecFlag(flagStr string) bool {
	// Strip leading dashes
	name := strings.TrimLeft(flagStr, "-")
	// Handle --flag=value form
	name, _, _ = strings.Cut(name, "=")

	return execFlags[name]
}

// needsValue returns true if the flag requires a following value argument.
func needsValue(flagStr string) bool {
	// Handle --flag=value (doesn't need separate value)
	if strings.Contains(flagStr, "=") {
		return false
	}

	// Strip leading dashes
	name := strings.TrimLeft(flagStr, "-")

	// Global flags that need values
	if name == "cwd" || name == "C" || name == "config" || name == "c" {
		return true
	}

	// Exec flags that need values
	if name == "ro" || name == "rw" || name == "exclude" || name == "cmd" {
		return true
	}

	return false
}
