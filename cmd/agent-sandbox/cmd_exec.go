package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"

	"github.com/calvinalkan/agent-sandbox/sandbox"
)

const (
	agentSandboxRuntimeRoot        = "/run/agent-sandbox"
	errNoCommandMessage            = "no command specified (usage: agent-sandbox <command> [args])"
	errNotLinuxMessage             = "agent-sandbox requires Linux (bwrap uses Linux namespaces)"
	errRunningAsRootMessage        = "agent-sandbox cannot run as root (use a regular user account)"
	errBwrapNotFoundMessage        = "bwrap not found in PATH (try installing with: sudo apt install bubblewrap)"
	errInvalidCommandPresetMessage = "command preset can only be used for its matching command"
	errNestedSandboxDepthMessage   = "nested sandboxes beyond one level are not supported"
)

// sandboxBinaryPath is where the agent-sandbox binary is mounted inside the sandbox.
//
// This path lives under the sandbox's /run tmpfs, so its presence is a reliable
// indicator that we're running inside an agent-sandbox-created sandbox.
var sandboxBinaryPath = filepath.Join(agentSandboxRuntimeRoot, "agent-sandbox")

// WithKillContext returns a context that carries a separate "kill context".
// When the kill context is cancelled, the sandboxed process should be sent SIGKILL.
// This enables two-stage shutdown: cancel the main context for SIGTERM,
// then cancel the kill context for SIGKILL.
func WithKillContext(ctx context.Context, killCtx context.Context) context.Context {
	return context.WithValue(ctx, killCtxKey{}, killCtx)
}

type killCtxKey struct{}

func ExecuteSandbox(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, cfg *Config, env map[string]string, args []string, debug *DebugLogger, dryRun bool) int {
	homeDir, err := getHomeDir(env)
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	if len(args) == 0 {
		fprintError(stderr, errors.New(errNoCommandMessage))

		return 1
	}

	// Nested sandbox behavior: command wrappers are inherited from the outer
	// sandbox. Inner sandboxes may add new wrappers for commands that are not
	// already wrapped, but cannot override outer wrappers.
	if isInsideSandbox() {
		err = checkNestedSandboxDepth()
		if err != nil {
			fprintError(stderr, err)

			return 1
		}

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

	if dryRun {
		printDryRunOutput(stdout, bwrapArgs, args)

		return 0
	}

	exitCode, err := runBwrapProcess(ctx, cmd, stderr, debug)
	if err != nil {
		fprintError(stderr, err)

		return 1
	}

	return exitCode
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

func newSandbox(cfg *Config, env sandbox.Environment, debug *DebugLogger) (*sandbox.Sandbox, error) {
	if cfg == nil {
		return nil, errors.New("nil config")
	}

	selfBinary, err := getSelfBinary()
	if err != nil {
		return nil, err
	}

	mounts := make([]sandbox.Mount, 0, 32)

	// Filesystem policy mounts in precedence order.
	//
	// We intentionally keep global/project config filesystem paths separate so that
	// later config layers reliably override earlier ones, even when access levels
	// differ (e.g. global "rw" vs project "ro").
	mounts = append(mounts, mountsFromConfig(&cfg.GlobalFilesystem)...)
	mounts = append(mounts, mountsFromConfig(&cfg.ProjectFilesystem)...)
	mounts = append(mounts, mountsFromConfig(&cfg.CLIFilesystem)...)

	for _, p := range getLoadedConfigPaths(cfg) {
		mounts = append(mounts, sandbox.ROTry(p))
	}

	// Protect project config files from modification by sandboxed processes.
	//
	// These live under the (typically RW) workdir mount, so they must be
	// re-mounted read-only explicitly.
	mounts = append(mounts,
		sandbox.ROTry(filepath.Join(env.WorkDir, ".agent-sandbox.json")),
		sandbox.ROTry(filepath.Join(env.WorkDir, ".agent-sandbox.jsonc")),
	)

	runtimeRoot := filepath.Dir(sandboxBinaryPath)

	// Always mount the agent-sandbox binary at the deterministic runtime path.
	// This enables sandbox detection and provides the multicall launcher.
	mounts = append(mounts,
		sandbox.Dir(runtimeRoot, 0o111),
		sandbox.RoBind(selfBinary, sandboxBinaryPath),
	)

	// Nested sandbox support: the inner sandbox mounts a fresh /run tmpfs, which
	// would otherwise hide the outer sandbox runtime (policies/real bins).
	// Mount the outer runtime under /run/agent-sandbox/outer and let the launcher
	// fall back to it when no inner policy exists.
	if isInsideSandbox() {
		outerRuntime := filepath.Join(runtimeRoot, "outer")
		mounts = append(mounts,
			sandbox.Dir(outerRuntime),
			sandbox.RoBindTry(runtimeRoot, outerRuntime),
		)
	}

	block, wrappers, err := buildSandboxCommandRules(cfg.Commands)
	if err != nil {
		return nil, err
	}

	sbCfg := sandbox.Config{
		Network: cfg.Network,
		Docker:  cfg.Docker,
		TempDir: os.TempDir(),
		Filesystem: sandbox.Filesystem{
			Presets: effectivePresetsForCLI(cfg.Filesystem.Presets),
			Mounts:  mounts,
		},
		Commands: sandbox.Commands{
			Block:     block,
			Wrappers:  wrappers,
			Launcher:  selfBinary,
			MountPath: agentSandboxRuntimeRoot,
		},
	}

	if debug != nil && debug.Enabled() {
		sbCfg.Debugf = func(format string, args ...any) {
			debug.Logf(format, args...)
		}
	}

	sb, err := sandbox.NewWithEnvironment(&sbCfg, env)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	return sb, nil
}

func effectivePresetsForCLI(presets []string) []string {
	// CLI semantics: @all is the default baseline and user-provided presets are
	// modifications unless the user explicitly controls @all.
	if len(presets) == 0 {
		return nil
	}

	for _, raw := range presets {
		p := strings.TrimSpace(raw)
		if p == "@all" || p == "!@all" {
			return presets
		}
	}

	return append([]string{"@all"}, presets...)
}

func mountsFromConfig(fs *FilesystemConfig) []sandbox.Mount {
	out := make([]sandbox.Mount, 0, len(fs.Ro)+len(fs.Rw)+len(fs.Exclude))

	// CLI config and flags historically tolerated missing paths. Keep that behavior
	// by using the *Try variants, and rely on explicit strict mounts in presets.
	//
	// Ordering matters: the sandbox planner resolves conflicts for the same host
	// path by "last one wins". Within a single config layer we therefore emit
	// mounts from least to most restrictive so that:
	//   exclude > ro > rw.
	for _, p := range fs.Rw {
		out = append(out, sandbox.RWTry(p))
	}

	for _, p := range fs.Ro {
		out = append(out, sandbox.ROTry(p))
	}

	for _, p := range fs.Exclude {
		out = append(out, sandbox.ExcludeTry(p))
	}

	return out
}

func buildSandboxCommandRules(commands map[string]CommandRule) ([]string, map[string]sandbox.Wrapper, error) {
	if len(commands) == 0 {
		return nil, nil, nil
	}

	var block []string

	wrappers := make(map[string]sandbox.Wrapper)

	for cmdName, rule := range commands {
		switch rule.Kind {
		case CommandRuleExplicitAllow:
			continue
		case CommandRuleBlock:
			block = append(block, cmdName)
		case CommandRuleScript:
			wrappers[cmdName] = sandbox.Wrap(rule.Value)
		case CommandRulePreset:
			switch rule.Value {
			case "@git":
				// Use inline script with special content the launcher recognizes as preset
				wrappers[cmdName] = sandbox.Wrapper{InlineScript: "preset:git\n"}
			default:
				return nil, nil, fmt.Errorf("unknown command preset: %s", rule.Value)
			}
		default:
			return nil, nil, fmt.Errorf("unknown command rule kind: %v", rule.Kind)
		}
	}

	if len(block) == 0 {
		block = nil
	}

	if len(wrappers) == 0 {
		wrappers = nil
	}

	return block, wrappers, nil
}

func bwrapArgsFromCmd(args []string) []string {
	if len(args) == 0 {
		return nil
	}

	args = args[1:]
	for i, a := range args {
		if a == "--" {
			return args[:i]
		}
	}

	return args
}

// runBwrapProcess starts the bwrap process and handles shutdown signals.
func runBwrapProcess(ctx context.Context, cmd *exec.Cmd, stderr io.Writer, _ *DebugLogger) (int, error) {
	if ctx.Err() != nil {
		return 0, fmt.Errorf("context cancelled: %w", ctx.Err())
	}

	err := cmd.Start()
	if err != nil {
		return 1, fmt.Errorf("starting bwrap: %w (check if kernel supports user namespaces: sysctl kernel.unprivileged_userns_clone)", err)
	}

	killCtx, ok := ctx.Value(killCtxKey{}).(context.Context)
	if !ok {
		killCtx = context.Background()
	}

	waitDone := make(chan error, 1)

	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case waitErr := <-waitDone:
		return extractExitCode(waitErr)

	case <-ctx.Done():
		// Context cancelled - send SIGTERM for graceful shutdown.
		if cmd.Process != nil {
			err := cmd.Process.Signal(syscall.SIGTERM)
			if err != nil {
				fmt.Fprintf(stderr, "warning: failed to send SIGTERM: %v\n", err)
			}
		}

		select {
		case waitErr := <-waitDone:
			return extractExitCode(waitErr)
		case <-killCtx.Done():
			if cmd.Process != nil {
				err := cmd.Process.Kill()
				if err != nil {
					fmt.Fprintf(stderr, "warning: failed to send SIGKILL: %v\n", err)
				}
			}

			<-waitDone

			return 0, nil
		}

	case <-killCtx.Done():
		if cmd.Process != nil {
			err := cmd.Process.Kill()
			if err != nil {
				fmt.Fprintf(stderr, "warning: failed to send SIGKILL: %v\n", err)
			}
		}

		<-waitDone

		return 0, nil
	}
}

func extractExitCode(waitErr error) (int, error) {
	if waitErr != nil {
		var exitErr *exec.ExitError
		if errors.As(waitErr, &exitErr) {
			return exitErr.ExitCode(), nil
		}

		return 1, fmt.Errorf("waiting for bwrap: %w", waitErr)
	}

	return 0, nil
}

func getSelfBinary() (string, error) {
	self, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate agent-sandbox binary: %w", err)
	}

	self, err = filepath.EvalSymlinks(self)
	if err != nil {
		return "", fmt.Errorf("cannot resolve agent-sandbox binary path: %w", err)
	}

	self = filepath.Clean(self)
	if self == "" {
		return "", errors.New("cannot locate agent-sandbox binary")
	}

	info, err := os.Stat(self)
	if err != nil {
		return "", fmt.Errorf("cannot stat agent-sandbox binary %q: %w", self, err)
	}

	if info.IsDir() {
		return "", fmt.Errorf("agent-sandbox binary %q is a directory", self)
	}

	if info.Mode().Perm()&0o111 == 0 {
		return "", fmt.Errorf("agent-sandbox binary %q is not executable", self)
	}

	return self, nil
}

// validateCommandRules checks that command presets are used correctly.
// For example, @git can only be used with the "git" command.
func validateCommandRules(commands map[string]CommandRule) error {
	for cmdName, rule := range commands {
		if rule.Kind != CommandRulePreset {
			continue
		}

		presetCmd := strings.TrimPrefix(rule.Value, "@")
		if cmdName != presetCmd {
			return fmt.Errorf("%s: %s preset can only be used for '%s' command, not '%s'",
				errInvalidCommandPresetMessage, rule.Value, presetCmd, cmdName)
		}
	}

	return nil
}

// printDryRunOutput formats and prints the bwrap command for dry-run mode.
// The output is shell-compatible and can be copy-pasted to run manually.
func printDryRunOutput(output io.Writer, bwrapArgs []string, command []string) {
	fprintf(output, "bwrap \\\n")

	for _, arg := range bwrapArgs {
		fprintf(output, "  %s \\\n", shellQuoteIfNeeded(arg))
	}

	fprintf(output, "  --")

	for _, arg := range command {
		fprintf(output, " %s", shellQuoteIfNeeded(arg))
	}

	fprintln(output)
}

// shellQuoteIfNeeded returns the string quoted if it contains special characters,
// otherwise returns it unchanged. This makes the output shell-safe.
func shellQuoteIfNeeded(str string) string {
	for _, c := range str {
		if !isShellSafeChar(c) {
			escaped := strings.ReplaceAll(str, "'", "'\"'\"'")

			return "'" + escaped + "'"
		}
	}

	return str
}

// isShellSafeChar returns true if the character doesn't need quoting in shell.
func isShellSafeChar(c rune) bool {
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

func withAgentSandboxOnPath(env map[string]string) map[string]string {
	if env == nil {
		return nil
	}

	out := make(map[string]string, len(env)+1)
	maps.Copy(out, env)

	sandboxDir := filepath.Dir(sandboxBinaryPath)

	pathVar := out["PATH"]
	if pathVar == "" {
		out["PATH"] = sandboxDir

		return out
	}

	if slices.Contains(strings.Split(pathVar, ":"), sandboxDir) {
		return out
	}

	out["PATH"] = sandboxDir + ":" + pathVar

	return out
}

func filterNestedCommandRules(commands map[string]CommandRule) map[string]CommandRule {
	if len(commands) == 0 {
		return commands
	}

	runtimeRoot := filepath.Dir(sandboxBinaryPath)

	out := make(map[string]CommandRule, len(commands))
	for cmdName, rule := range commands {
		if hasWrapperMarker(runtimeRoot, cmdName) {
			continue
		}

		out[cmdName] = rule
	}

	if len(out) == 0 {
		return nil
	}

	return out
}

func hasWrapperMarker(runtimeRoot, cmdName string) bool {
	if cmdName == "" || strings.Contains(cmdName, "/") {
		return false
	}

	_, policyErr := os.Stat(filepath.Join(runtimeRoot, "policies", cmdName))
	if policyErr == nil {
		return true
	}

	_, presetErr := os.Stat(filepath.Join(runtimeRoot, "presets", cmdName))
	if presetErr == nil {
		return true
	}

	return false
}

// isInsideSandbox checks if the current process is running inside an agent-sandbox
// sandbox by testing for the presence of the sandbox-mounted agent-sandbox binary.
func isInsideSandbox() bool {
	_, err := os.Stat(sandboxBinaryPath)

	return err == nil
}

func checkNestedSandboxDepth() error {
	if !isInsideSandbox() {
		return nil
	}

	// The launcher only searches the current runtime and one outer runtime.
	// A third-level sandbox would skip top-level policies, so block it here.
	outerBinary := filepath.Join(filepath.Dir(sandboxBinaryPath), "outer", "agent-sandbox")
	_, err := os.Stat(outerBinary)
	if err == nil {
		return errors.New(errNestedSandboxDepthMessage)
	}

	if os.IsNotExist(err) {
		return nil
	}

	return fmt.Errorf("check nested sandbox depth: %w", err)
}

func isWrappedCommandName(cmdName string) bool {
	runtimeRoot := filepath.Dir(sandboxBinaryPath)
	for _, root := range []string{runtimeRoot, filepath.Join(runtimeRoot, "outer")} {
		if hasWrapperMarker(root, cmdName) {
			return true
		}
	}

	return false
}
