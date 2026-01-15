// Multicall dispatcher for command wrappers inside the sandbox.
//
// When a command like "git" is wrapped, the agent-sandbox binary is mounted
// over the real git binary. When git is invoked, argv[0] is "git" but the
// actual binary is agent-sandbox. The multicall dispatcher detects this
// (argv[0] != "agent-sandbox") and routes to the appropriate handler.
//
// # Runtime Layout
//
// Inside the sandbox, the runtime is mounted at /run/agent-sandbox:
//
//	/run/agent-sandbox/
//	├── agent-sandbox     # the agent-sandbox binary itself
//	├── bin/              # real binaries (e.g., bin/git)
//	└── wrappers/         # wrapper scripts or preset markers
//
// # Wrapper Files
//
// Each wrapped command has a wrapper file at wrappers/<cmd>. The content
// determines how the command is handled:
//
//   - "preset:<name>\n": Built-in preset (e.g., "preset:git\n")
//   - Otherwise: Executable script to run
//
// # Nested Sandbox Support
//
// When running a sandbox inside another sandbox, the inner sandbox mounts
// the outer's runtime at /run/agent-sandbox/outer:
//
//	┌─────────────────────────────────────────────────┐
//	│ OUTER SANDBOX                                   │
//	│   /run/agent-sandbox/                           │
//	│     ├── wrappers/git    ← outer's git wrapper   │
//	│     └── bin/git                                 │
//	│                                                 │
//	│   ┌─────────────────────────────────────────┐   │
//	│   │ INNER SANDBOX                           │   │
//	│   │   /run/agent-sandbox/                   │   │
//	│   │     ├── wrappers/rm  ← inner's wrapper  │   │
//	│   │     └── outer/       ← mount of outer   │   │
//	│   │           ├── wrappers/git              │   │
//	│   │           └── bin/git                   │   │
//	│   └─────────────────────────────────────────┘   │
//	└─────────────────────────────────────────────────┘
//
// The dispatcher searches inner first, then outer. Since inner sandboxes
// cannot override outer wrappers (filtered by filterNestedCommandRules),
// outer wrappers are effectively inherited.
//
// # Dispatch Flow
//
//  1. Read /run/agent-sandbox/wrappers/<cmd>
//  2. If content starts with "preset:" → run built-in preset
//  3. Otherwise → run as script
//  4. Repeat for /run/agent-sandbox/outer/... (nested only)
//  5. If nothing found → error

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
)

var multicallOuterRuntimeRoot = filepath.Join(agentSandboxRuntimeRoot, "outer")

func runMulticall(ctx context.Context, cmdName string, cmdArgs []string, stdin io.Reader, stdout, stderr io.Writer, env map[string]string) error {
	aliasSubcommand := gitAliasSubcommand(cmdName)

	aliasArgs := cmdArgs
	if aliasSubcommand != "" {
		aliasArgs = append([]string{aliasSubcommand}, cmdArgs...)
	}

	for _, root := range multicallRuntimeRoots() {
		wrapperPath := filepath.Join(root, "wrappers", cmdName)

		content, err := os.ReadFile(wrapperPath)
		if err == nil {
			return runWrapperFromContent(ctx, &wrapperDispatchInput{
				runtimeRoot: root,
				wrapperPath: wrapperPath,
				cmdName:     cmdName,
				cmdArgs:     cmdArgs,
				content:     content,
				stdin:       stdin,
				stdout:      stdout,
				stderr:      stderr,
				env:         env,
			})
		}

		if aliasSubcommand == "" {
			continue
		}

		wrapperPath = filepath.Join(root, "wrappers", "git")

		content, err = os.ReadFile(wrapperPath)
		if err != nil {
			continue
		}

		return runWrapperFromContent(ctx, &wrapperDispatchInput{
			runtimeRoot: root,
			wrapperPath: wrapperPath,
			cmdName:     "git",
			cmdArgs:     aliasArgs,
			content:     content,
			stdin:       stdin,
			stdout:      stdout,
			stderr:      stderr,
			env:         env,
		})
	}

	return fmt.Errorf("%s: command not available", cmdName)
}

type wrapperDispatchInput struct {
	runtimeRoot string
	wrapperPath string
	cmdName     string
	cmdArgs     []string
	content     []byte
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	env         map[string]string
}

func runWrapperFromContent(ctx context.Context, input *wrapperDispatchInput) error {
	if strings.HasPrefix(string(input.content), "preset:") {
		presetName := strings.TrimPrefix(strings.TrimSpace(string(input.content)), "preset:")
		if presetName == "git" && input.cmdName == "git" {
			return runGitPreset(ctx, input.runtimeRoot, input.cmdArgs, input.stdin, input.stdout, input.stderr)
		}

		return fmt.Errorf("%s: command not available", input.cmdName)
	}

	realBinary := filepath.Join(input.runtimeRoot, "bin", input.cmdName)

	// Block-only wrappers do not mount a real binary; allow wrapper execution
	// in that case by clearing AGENT_SANDBOX_REAL when the file is missing.
	_, statErr := os.Stat(realBinary)
	if statErr != nil {
		if !os.IsNotExist(statErr) {
			return fmt.Errorf("statting %q: %w", realBinary, statErr)
		}

		realBinary = ""
	}

	return runWrapper(ctx, &wrapperRunInput{
		wrapperPath: input.wrapperPath,
		cmdName:     input.cmdName,
		realBinary:  realBinary,
		cmdArgs:     input.cmdArgs,
		stdin:       input.stdin,
		stdout:      input.stdout,
		stderr:      input.stderr,
		env:         input.env,
	})
}

func gitAliasSubcommand(cmdName string) string {
	switch cmdName {
	case "git-receive-pack":
		return "receive-pack"
	case "git-upload-pack":
		return "upload-pack"
	default:
		return ""
	}
}

type wrapperRunInput struct {
	wrapperPath string
	cmdName     string
	realBinary  string
	cmdArgs     []string
	stdin       io.Reader
	stdout      io.Writer
	stderr      io.Writer
	env         map[string]string
}

func runWrapper(ctx context.Context, config *wrapperRunInput) error {
	cmd, err := newExecCmd(config.wrapperPath, config.cmdArgs)
	if err != nil {
		return fmt.Errorf("running wrapper command %s: %w", config.cmdName, err)
	}

	cmd.Stdin = config.stdin
	cmd.Stdout = config.stdout
	cmd.Stderr = config.stderr

	cmd.Env = envMapToSlice(config.env)
	cmd.Env = append(cmd.Env,
		"AGENT_SANDBOX_CMD="+config.cmdName,
		"AGENT_SANDBOX_REAL="+config.realBinary,
	)

	err = runCommandWithContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("running wrapper command %s: %w", config.cmdName, err)
	}

	return nil
}

func runGitPreset(ctx context.Context, runtimeRoot string, cmdArgs []string, stdin io.Reader, stdout, stderr io.Writer) error {
	realBinary := filepath.Join(runtimeRoot, "bin", "git")

	_, err := os.Stat(realBinary)
	if err != nil {
		return errors.New("git: command not available")
	}

	if hasInlineAliasConfig(cmdArgs) {
		return errors.New("git alias overrides via -c/--config-env are blocked; configure aliases outside the sandbox")
	}

	subcommand, subcommandArgs := parseGitArgs(cmdArgs)

	err = isBlockedGitOperation(subcommand, subcommandArgs)
	if err != nil {
		return err
	}

	cmd, err := newExecCmd(realBinary, cmdArgs)
	if err != nil {
		return err
	}

	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err = runCommandWithContext(ctx, cmd)
	if err != nil {
		return fmt.Errorf("running git: %w", err)
	}

	return nil
}

func newExecCmd(path string, args []string) (*exec.Cmd, error) {
	if path == "" {
		return nil, errors.New("missing command path")
	}

	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("command path must be absolute: %s", path)
	}

	cmd := &exec.Cmd{
		Path: path,
		Args: append([]string{path}, args...),
	}

	return cmd, nil
}

func runCommandWithContext(ctx context.Context, cmd *exec.Cmd) error {
	if ctx == nil {
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("running command: %w", err)
		}

		return nil
	}

	err := cmd.Start()
	if err != nil {
		return fmt.Errorf("starting command: %w", err)
	}

	done := make(chan error, 1)

	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		if err != nil {
			return fmt.Errorf("waiting for command: %w", err)
		}

		return nil
	case <-ctx.Done():
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}

		err := <-done
		if err != nil {
			return fmt.Errorf("waiting for command: %w", err)
		}

		return fmt.Errorf("command cancelled: %w", ctx.Err())
	}
}

func envMapToSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		if k == "" {
			continue
		}

		out = append(out, k+"="+v)
	}

	return out
}

// parseGitArgs extracts the git subcommand, skipping global flags like -C.
func parseGitArgs(args []string) (string, []string) {
	flagsWithValue := map[string]bool{
		"-C": true, "-c": true, "--git-dir": true, "--work-tree": true,
		"--namespace": true, "--super-prefix": true, "--config-env": true,
		"--exec-path": true, "--html-path": true, "--man-path": true,
		"--info-path": true, "--list-cmds": true, "--attr-source": true,
		"-p": false, "--paginate": false, "-P": false, "--no-pager": false,
		"--no-replace-obj": false, "--bare": false, "--literal-pathspecs": false,
		"--glob-pathspecs": false, "--noglob-pathspecs": false,
		"--icase-pathspecs": false, "--no-optional-locks": false,
	}

	i := 0
	for i < len(args) {
		arg := args[i]

		if !strings.HasPrefix(arg, "-") {
			return arg, args[i+1:]
		}

		if strings.Contains(arg, "=") {
			flagName := strings.SplitN(arg, "=", 2)[0]
			if _, known := flagsWithValue[flagName]; known || strings.HasPrefix(flagName, "--") {
				i++

				continue
			}
		}

		if takesValue, known := flagsWithValue[arg]; known && takesValue {
			i += 2

			continue
		}

		i++
	}

	return "", nil
}

func isBlockedGitOperation(subcommand string, args []string) error {
	inTemp, err := isInTempDir()
	if err != nil {
		return err
	}

	if inTemp {
		return nil
	}

	switch subcommand {
	case "checkout":
		return errors.New("git checkout blocked: can discard uncommitted changes; use 'git switch' for branches")
	case "restore":
		return errors.New("git restore blocked: discards uncommitted changes; commit or stash first")
	case "reset":
		if hasFlag(args, "--hard") {
			return errors.New("git reset --hard blocked: discards commits and changes; use 'git reset --soft' or 'git revert'")
		}
	case "clean":
		if hasFlag(args, "-f", "--force") {
			return errors.New("git clean -f blocked: deletes untracked files; review manually")
		}
	case "commit":
		if hasFlag(args, "--no-verify", "-n") {
			return errors.New("git commit --no-verify blocked: bypasses safety hooks; fix the hook issues")
		}
	case "stash":
		if len(args) > 0 {
			switch args[0] {
			case "drop":
				return errors.New("git stash drop blocked: permanently deletes stash; keep stashes or export first")
			case "clear":
				return errors.New("git stash clear blocked: deletes all stashes; export important stashes first")
			case "pop":
				return errors.New("git stash pop blocked: can cause merge conflicts that lose stash; use 'git stash apply'")
			}
		}
	case "branch":
		deleteFlag := hasFlag(args, "-d", "--delete")

		forceFlag := hasFlag(args, "-f", "--force")
		if hasFlag(args, "-D") || (deleteFlag && forceFlag) {
			return errors.New("git branch -D blocked: force deletes unmerged branch; use 'git branch -d' (safe delete)")
		}
	case "push":
		forceFlag := hasFlag(args, "--force", "-f")

		forceWithLease := hasFlag(args, "--force-with-lease")
		if forceFlag {
			if forceWithLease {
				return errors.New("git push --force blocked: use 'git push --force-with-lease' without --force/-f")
			}

			return errors.New("git push --force blocked: rewrites remote history; use 'git push --force-with-lease'")
		}
	}

	return nil
}

// multicallRuntimeRoots returns the runtime directories to search for wrappers,
// in priority order: inner sandbox first, then outer (if nested).
//
// In a nested sandbox, /run/agent-sandbox/outer contains the outer sandbox's
// runtime. We search inner first so inner-specific wrappers take precedence,
// but since filterNestedCommandRules prevents inner from overriding outer
// wrappers, outer wrappers are effectively inherited.
func multicallRuntimeRoots() []string {
	roots := []string{agentSandboxRuntimeRoot}

	_, statErr := os.Stat(multicallOuterRuntimeRoot)
	if statErr == nil {
		roots = append(roots, multicallOuterRuntimeRoot)
	}

	return roots
}

func hasFlag(args []string, flags ...string) bool {
	if len(flags) == 0 {
		return false
	}

	longFlags := make(map[string]bool)
	shortFlags := make(map[string]bool)

	for _, f := range flags {
		if strings.HasPrefix(f, "--") {
			longFlags[f] = true
		} else if strings.HasPrefix(f, "-") {
			shortFlags[f] = true
		}
	}

	for _, arg := range args {
		if arg == "--" {
			break
		}

		if strings.HasPrefix(arg, "--") {
			name := arg
			if idx := strings.Index(name, "="); idx > 0 {
				name = name[:idx]
			}

			if longFlags[name] {
				return true
			}

			for target := range longFlags {
				if matchesLongFlag(name, target) {
					return true
				}
			}

			continue
		}

		if strings.HasPrefix(arg, "-") && len(arg) > 1 {
			if len(arg) == 2 {
				if shortFlags[arg] {
					return true
				}

				continue
			}

			for _, r := range arg[1:] {
				if shortFlags["-"+string(r)] {
					return true
				}
			}
		}
	}

	return false
}

func matchesLongFlag(arg, target string) bool {
	if arg == target {
		return true
	}

	if !strings.HasPrefix(arg, "--") || !strings.HasPrefix(target, "--") {
		return false
	}

	if len(arg) >= len(target) {
		return false
	}

	if !strings.HasPrefix(target, arg) {
		return false
	}

	if target == "--force-with-lease" {
		return len(arg) > len("--force")
	}

	return true
}

func hasInlineAliasConfig(args []string) bool {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			break
		}

		switch {
		case arg == "-c" || arg == "--config-env":
			if i+1 >= len(args) {
				continue
			}

			if isAliasConfigKey(args[i+1]) {
				return true
			}

			i++

			continue
		case strings.HasPrefix(arg, "-c") && len(arg) > 2 && !strings.HasPrefix(arg, "--"):
			if isAliasConfigKey(arg[2:]) {
				return true
			}

			continue
		case strings.HasPrefix(arg, "--config-env="):
			if isAliasConfigKey(strings.TrimPrefix(arg, "--config-env=")) {
				return true
			}

			continue
		}
	}

	return false
}

func isAliasConfigKey(value string) bool {
	key := value
	if idx := strings.Index(key, "="); idx >= 0 {
		key = key[:idx]
	}

	key = strings.TrimSpace(key)
	key = strings.ToLower(key)

	return strings.HasPrefix(key, "alias.")
}

// isInTempDir checks if the current working directory is inside /tmp.
//
// The sandbox normalizes the host's temp directory to /tmp (via Config.TempDir),
// so we only need to check /tmp here. Agents can still manipulate TMPDIR, but it
// doesn't matter since we check against the hardcoded /tmp path, not os.TempDir().
func isInTempDir() (bool, error) {
	pwd, err := os.Getwd()
	if err != nil {
		return false, fmt.Errorf("getting working directory: %w", err)
	}

	pwdReal, err := filepath.EvalSymlinks(pwd)
	if err != nil {
		return false, fmt.Errorf("resolving working directory: %w", err)
	}

	tmpReal, err := filepath.EvalSymlinks("/tmp")
	if err != nil {
		return false, fmt.Errorf("resolving /tmp: %w", err)
	}

	return strings.HasPrefix(pwdReal, tmpReal), nil
}
