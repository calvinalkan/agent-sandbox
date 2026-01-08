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

	// Create context early so config loading can be cancelled
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Load config (handles --cwd resolution internally)
	cfg, err := LoadConfig(LoadConfigInput{
		WorkDirOverride: *flagCwd,
		ConfigPath:      *flagConfig,
		Env:             env,
	})
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	// Create all commands
	commands := []*Command{
		ExecCmd(&cfg, env),
		CheckCmd(),
	}

	commandMap := make(map[string]*Command, len(commands)*2)
	for _, cmd := range commands {
		commandMap[cmd.Name()] = cmd
		for _, alias := range cmd.Aliases {
			commandMap[alias] = cmd
		}
	}

	commandAndArgs := globalFlags.Args()

	// Show help: explicit --help or bare `agent-sandbox` with no args
	if *flagHelp || len(commandAndArgs) == 0 {
		printUsage(stdout, commands)

		return 0
	}

	// Dispatch to command
	cmdName := commandAndArgs[0]

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
		cancel()
	}

	// Wait for completion, timeout, or second signal
	select {
	case <-done:
		fprintln(stderr, "Cleanup complete.")

		return 130
	case <-time.After(10 * time.Second):
		fprintln(stderr, "Cleanup timed out, forced exit.")

		return 130
	case <-sigCh:
		fprintln(stderr, "Forced exit.")

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
