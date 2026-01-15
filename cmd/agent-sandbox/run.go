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
	"time"

	"github.com/calvinalkan/agent-sandbox/sandbox"
	flag "github.com/spf13/pflag"
)

const (
	// agentSandboxExecutableName is the canonical name of the agent-sandbox binary.
	//
	// Inside the sandbox we rely on argv0-based dispatch: wrapped commands execute
	// the agent-sandbox ELF launcher but argv0 is the wrapped command name (e.g.
	// "git"), while normal CLI usage uses argv0 == "agent-sandbox".
	agentSandboxExecutableName = "agent-sandbox"

	// exitCodeSIGINT is the exit code when the process is interrupted by SIGINT (128 + 2).
	exitCodeSIGINT = 130

	// cleanupTimeout is how long to wait for graceful shutdown before force-killing.
	cleanupTimeout = 10 * time.Second
)

// Run is the main entry point that isolates the entire logic from global state like stdin/stdout/stderr and env.
// Returns exit code.
// sigCh can be nil if signal handling is not needed (e.g., in tests).
func Run(stdin io.Reader, stdout, stderr io.Writer, args []string, env map[string]string, sigCh <-chan os.Signal) int {
	if len(args) > 0 {
		invoked := filepath.Base(args[0])
		if invoked != agentSandboxExecutableName && isInsideSandbox() && isWrappedCommandName(invoked) {
			err := runMulticall(context.Background(), invoked, args[1:], stdin, stdout, stderr, env)
			if err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					// Pass through errors from scripts without
					// printing to stderr again.
					return exitErr.ExitCode()
				}

				fprintError(stderr, err)

				return 1
			}

			return 0
		}
	}

	err := checkPlatformPrerequisites()
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	flags := flag.NewFlagSet(agentSandboxExecutableName, flag.ContinueOnError)
	// Stop parsing at first non-flag (the command),
	// important, oterhwise we can't find where the "real" command begins.
	flags.SetInterspersed(false)
	flags.Usage = func() {}
	flags.SetOutput(&strings.Builder{})

	flagHelp := flags.BoolP("help", "h", false, "Show help")
	flagVersion := flags.BoolP("version", "v", false, "Show version and exit")
	flagCheck := flags.Bool("check", false, "Check if running inside sandbox and exit")

	flagCwd := flags.StringP("cwd", "C", "", "Run as if started in `dir`")
	flagConfig := flags.StringP("config", "c", "", "Use specified config `file`")

	flags.Bool("network", true, "Enable network access")
	flags.Bool("docker", false, "Enable docker socket access")
	flags.Bool("dry-run", false, "Print bwrap command without executing")
	flags.Bool("debug", false, "Print sandbox startup details to stderr")
	flags.StringArray("ro", nil, "Add read-only path")
	flags.StringArray("rw", nil, "Add read-write path")
	flags.StringArray("exclude", nil, "Add excluded path")
	flags.StringArray("cmd", nil, "Command wrapper override (KEY=VALUE, repeatable)")

	err := flags.Parse(args[1:])
	if err != nil {
		fprintError(stderr, err)
		fprintln(stderr)
		printUsage(stderr)

		return 1
	}

	if *flagVersion {
		fprintf(stdout, "%s\n", formatVersion())

		return 0
	}

	if *flagCheck {
		inside := isInsideSandbox()
		if inside {
			fprintln(stdout, "inside sandbox")

			return 0
		}

		fprintln(stdout, "outside sandbox")

		return 1
	}

	commandAndArgs := flags.Args()

	if *flagHelp || len(commandAndArgs) == 0 {
		printUsage(stdout)

		return 0
	}

	cfg, err := LoadConfig(LoadConfigInput{
		WorkDirOverride: *flagCwd,
		ConfigPath:      *flagConfig,
		EnvVars:         env,
		CLIFlags:        flags,
	})
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	// Create nested contexts for two-stage shutdown:
	// - termCtx cancellation triggers SIGTERM (graceful shutdown)
	// - killCtx cancellation triggers SIGKILL (force kill)
	killCtx, kill := context.WithCancel(context.Background())
	defer kill()

	termCtx, terminate := context.WithCancel(killCtx)
	defer terminate()

	ctx := WithKillContext(termCtx, killCtx)

	done := make(chan int, 1)

	go func() {
		done <- runSandbox(ctx, stdin, stdout, stderr, &cfg, env, flags, commandAndArgs)
	}()

	if sigCh == nil {
		return <-done
	}

	select {
	case exitCode := <-done:
		return exitCode
	case <-sigCh:
		fprintln(stderr, "Interrupted, waiting up to 10s for cleanup... (Ctrl+C again to force exit)")
		terminate()
	}

	select {
	case <-done:
		fprintln(stderr, "Cleanup complete.")

		return exitCodeSIGINT
	case <-time.After(cleanupTimeout):
		fprintln(stderr, "Cleanup timed out, forced exit.")
		kill()
		<-done

		return exitCodeSIGINT
	case <-sigCh:
		fprintln(stderr, "Forced exit.")
		kill()
		<-done

		return exitCodeSIGINT
	}
}

// runSandbox executes a command inside the sandbox.
func runSandbox(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, cfg *Config, env map[string]string, flags *flag.FlagSet, args []string) int {
	debugEnabled, _ := flags.GetBool("debug")

	var debug *DebugLogger
	if debugEnabled {
		debug = NewDebugLogger(stderr)
		debugVersion(debug)
	}

	homeDir, err := getHomeDir(env)
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	if len(args) == 0 {
		fprintError(stderr, errors.New(errNoCommandMessage))

		return 1
	}

	debugConfigLoading(debug, cfg)
	debugConfigMerge(debug, cfg, flags)

	// Nested sandbox behavior: command wrappers are inherited from the outer
	// sandbox. Inner sandboxes may add new wrappers for commands that are not
	// already wrapped, but cannot override outer wrappers.
	if isInsideSandbox() {
		cfg.Commands = filterNestedCommandRules(cfg.Commands)
	}

	err = validateCommandRules(cfg.Commands)
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	sandboxEnv := sandbox.Environment{
		HomeDir: homeDir,
		WorkDir: cfg.EffectiveCwd,
		HostEnv: withAgentSandboxOnPath(env),
	}

	if debug != nil && debug.Enabled() {
		debug.Phase("sandbox")
	}

	sb, err := newSandbox(cfg, sandboxEnv, debug)
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	cmd, cleanup, err := sb.Command(ctx, args)
	if err != nil {
		if cleanup != nil {
			cleanupErr := cleanup()
			if cleanupErr != nil {
				fmt.Fprintf(stderr, "warning: cleanup failed: %v\n", cleanupErr)
			}
		}

		fprintError(stderr, err)

		return 1
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	if cleanup != nil {
		defer func() {
			cleanupErr := cleanup()
			if cleanupErr != nil {
				fmt.Fprintf(stderr, "warning: could not cleanup sandbox resources: %v\n", cleanupErr)
			}
		}()
	}

	bwrapArgs := bwrapArgsFromCmd(cmd.Args)

	DebugCommandWrappers(debug, cfg.Commands)
	DebugBwrapArgs(debug, bwrapArgs)

	if debug != nil && debug.Enabled() {
		debug.Phase("process")
		debug.Logf("starting")
	}

	dryRun, _ := flags.GetBool("dry-run")
	if dryRun {
		printDryRunOutput(stdout, bwrapArgs, args)

		return 0
	}

	exitCode, err := runSandboxedCommand(ctx, cmd, stderr, debug)
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	return exitCode
}

const usageHelp = `agent-sandbox - filesystem sandbox for agentic coding workflows

Usage: agent-sandbox [flags] <command> [args]

Flags:
  -h, --help             Show help
  -v, --version          Show version and exit
      --check            Check if running inside sandbox and exit
  -C, --cwd <dir>        Run as if started in <dir>
  -c, --config <file>    Use specified config file
      --network          Enable network access (default: true)
      --docker           Enable docker socket access
      --dry-run          Print bwrap command without executing
      --debug            Print sandbox startup details to stderr
      --ro <path>        Add read-only path (repeatable)
      --rw <path>        Add read-write path (repeatable)
      --exclude <path>   Exclude path from sandbox (repeatable)
      --cmd <key=value>  Command wrapper override (repeatable)

Examples:
  agent-sandbox echo hello
  agent-sandbox --network=false bash
  agent-sandbox --ro /data --rw /tmp/out my-script.sh
  agent-sandbox --check`

func printUsage(output io.Writer) {
	fprintln(output, usageHelp)
}

func fprintln(out io.Writer, a ...any) {
	_, _ = fmt.Fprintln(out, a...)
}

func fprintf(out io.Writer, format string, a ...any) {
	_, _ = fmt.Fprintf(out, format, a...)
}

func fprintError(out io.Writer, err error) {
	if isTerminal() {
		fprintln(out, "\033[31magent-sandbox: error:\033[0m", err)
	} else {
		fprintln(out, "agent-sandbox: error:", err)
	}
}

func formatVersion() string {
	if version == "source" {
		return fmt.Sprintf("agent-sandbox (built from source, %s)", date)
	}

	return fmt.Sprintf("agent-sandbox %s (%s, %s)", version, commit, date)
}

// isTerminal returns true if stdin is a terminal.
func isTerminal() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}

	return (stat.Mode() & os.ModeCharDevice) != 0
}

func getHomeDir(env map[string]string) (string, error) {
	// Escape hatch for our env abstraction (os.UserHomeDir() checks $HOME aswell)
	if home := env["HOME"]; home != "" {
		return home, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("determining home directory: %w", err)
	}

	return home, nil
}

func isWrappedCommandName(cmdName string) bool {
	if cmdName == "" || strings.Contains(cmdName, "/") {
		return false
	}

	runtimeRoot := filepath.Dir(sandboxBinaryPath)
	for _, root := range []string{runtimeRoot, filepath.Join(runtimeRoot, "outer")} {
		_, policyErr := os.Stat(filepath.Join(root, "policies", cmdName))
		if policyErr == nil {
			return true
		}

		_, presetErr := os.Stat(filepath.Join(root, "presets", cmdName))
		if presetErr == nil {
			return true
		}
	}

	return false
}
